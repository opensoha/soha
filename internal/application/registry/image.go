package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/distribution/reference"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainregistry "github.com/opensoha/soha/internal/domain/registry"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/netguard"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

func (s *Service) ValidateBuildImage(ctx context.Context, principal domainidentity.Principal, id, image string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryRegistriesView); err != nil {
		return err
	}
	_, _, err := s.imageConnection(ctx, id, image)
	return err
}

func (s *Service) imageConnection(ctx context.Context, id, image string) (domainregistry.Connection, reference.Named, error) {
	name, err := reference.ParseNormalizedNamed(image)
	if err != nil || name.String() != image {
		return domainregistry.Connection{}, nil, fmt.Errorf("%w: image must have an explicit registry and repository", apperrors.ErrInvalidArgument)
	}
	items, err := s.repo.List(ctx, 10000)
	if err != nil {
		return domainregistry.Connection{}, nil, err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		endpoint, err := registryEndpoint(item.Endpoint, item.Insecure)
		if err != nil {
			return item, nil, err
		}
		namespace := strings.Trim(item.Namespace, "/")
		if endpoint.Host != reference.Domain(name) || namespace != "" && !strings.HasPrefix(reference.Path(name), namespace+"/") {
			return item, nil, fmt.Errorf("%w: image does not belong to its registry connection", apperrors.ErrAccessDenied)
		}
		return item, name, nil
	}
	return domainregistry.Connection{}, nil, apperrors.ErrNotFound
}

// VerifyBuildImage fetches the immutable manifest and hashes the actual bytes.
// A CI report or registry response header alone is not completion evidence.
func (s *Service) VerifyBuildImage(ctx context.Context, id, image, digest string) error {
	item, name, err := s.imageConnection(ctx, id, image)
	if err != nil {
		return err
	}
	if len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		return apperrors.ErrInvalidArgument
	}
	if _, err := hex.DecodeString(digest[7:]); err != nil {
		return apperrors.ErrInvalidArgument
	}
	client, closeClient, err := registryImageClient(ctx, item)
	if err != nil {
		return err
	}
	defer closeClient()
	secret, err := s.decryptImageCredential(item.Secret)
	if err != nil {
		return err
	}
	repository, err := remote.NewRepository(reference.TrimNamed(name).String())
	if err != nil {
		return err
	}
	endpoint, _ := registryEndpoint(item.Endpoint, item.Insecure)
	repository.PlainHTTP = endpoint.Scheme == "http"
	repository.Client = &auth.Client{Client: client, Credential: auth.StaticCredential(repository.Reference.Registry, auth.Credential{Username: item.Username, Password: secret})}
	descriptor, content, err := repository.FetchReference(ctx, digest)
	if err != nil {
		return fmt.Errorf("%w: registry manifest could not be verified", apperrors.ErrClusterUnready)
	}
	defer func() { _ = content.Close() }()
	data, err := io.ReadAll(io.LimitReader(content, 4<<20+1))
	if err != nil || len(data) > 4<<20 || descriptor.Size != int64(len(data)) || descriptor.Digest.String() != digest {
		return fmt.Errorf("%w: invalid registry manifest", apperrors.ErrConflict)
	}
	sum := sha256.Sum256(data)
	if "sha256:"+hex.EncodeToString(sum[:]) != digest {
		return fmt.Errorf("%w: registry manifest digest mismatch", apperrors.ErrConflict)
	}
	return validateImageManifest(descriptor.MediaType, data)
}

func validateImageManifest(mediaType string, data []byte) error {
	var manifest struct {
		SchemaVersion int                  `json:"schemaVersion"`
		MediaType     string               `json:"mediaType"`
		ArtifactType  string               `json:"artifactType"`
		Config        ocispec.Descriptor   `json:"config"`
		Layers        []ocispec.Descriptor `json:"layers"`
		Manifests     []ocispec.Descriptor `json:"manifests"`
	}
	invalid := fmt.Errorf("%w: registry artifact is not an image manifest", apperrors.ErrConflict)
	if json.Unmarshal(data, &manifest) != nil || manifest.SchemaVersion != 2 || manifest.ArtifactType != "" || manifest.MediaType != "" && manifest.MediaType != mediaType {
		return invalid
	}
	var descriptors []ocispec.Descriptor
	switch mediaType {
	case ocispec.MediaTypeImageManifest, "application/vnd.docker.distribution.manifest.v2+json":
		if manifest.Config.MediaType != ocispec.MediaTypeImageConfig && manifest.Config.MediaType != "application/vnd.docker.container.image.v1+json" {
			return invalid
		}
		descriptors = append(manifest.Layers, manifest.Config)
	case ocispec.MediaTypeImageIndex, "application/vnd.docker.distribution.manifest.list.v2+json":
		if len(manifest.Manifests) == 0 {
			return invalid
		}
		for _, descriptor := range manifest.Manifests {
			if descriptor.MediaType != ocispec.MediaTypeImageManifest && descriptor.MediaType != "application/vnd.docker.distribution.manifest.v2+json" {
				return invalid
			}
		}
		descriptors = manifest.Manifests
	default:
		return invalid
	}
	for _, descriptor := range descriptors {
		if descriptor.Digest.Validate() != nil || descriptor.Size < 0 || descriptor.MediaType == "" {
			return invalid
		}
	}
	return nil
}

func (s *Service) decryptImageCredential(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !secretcrypto.Encrypted(value) {
		return "", fmt.Errorf("%w: registry credential must be encrypted before use", apperrors.ErrInvalidArgument)
	}
	if s.credentialKeys.Active().ID() != "" {
		return secretcrypto.DecryptStringWithKeyring(s.credentialKeys, value)
	}
	return secretcrypto.DecryptString(s.credentialKey, value)
}

func registryEndpoint(value string, insecure bool) (*url.URL, error) {
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Scheme != "https" && (!insecure || u.Scheme != "http") {
		return nil, fmt.Errorf("%w: registry endpoint must be an explicit HTTP(S) origin", apperrors.ErrInvalidArgument)
	}
	return u, nil
}

type registryImageTransport map[string]http.RoundTripper

func (t registryImageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport := t[request.URL.Scheme+"://"+request.URL.Host]
	if transport == nil || request.URL.User != nil {
		return nil, fmt.Errorf("registry authentication endpoint is not allowed")
	}
	return transport.RoundTrip(request)
}

func registryImageClient(ctx context.Context, item domainregistry.Connection) (*http.Client, func(), error) {
	var prefixes []netip.Prefix
	if value, _ := item.Metadata["allowedCIDRs"].(string); value != "" {
		for _, text := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
			prefix, err := netip.ParsePrefix(text)
			if err != nil || len(prefixes) >= 32 {
				return nil, nil, apperrors.ErrInvalidArgument
			}
			prefixes = append(prefixes, prefix.Masked())
		}
	}
	endpoints := []string{item.Endpoint}
	if value, _ := item.Metadata["authEndpoint"].(string); value != "" {
		endpoints = append(endpoints, value)
	}
	ca, _ := item.Metadata["caCertificate"].(string)
	transport := registryImageTransport{}
	closeClients := func() {
		for _, value := range transport {
			if t, ok := value.(*http.Transport); ok {
				t.CloseIdleConnections()
			}
		}
	}
	for _, value := range endpoints {
		u, err := registryEndpoint(value, item.Insecure)
		if err != nil {
			closeClients()
			return nil, nil, err
		}
		port := u.Port()
		if port == "" {
			port = "443"
			if u.Scheme == "http" {
				port = "80"
			}
		}
		client, err := netguard.PinnedHTTPSClient(ctx, u.Hostname(), port, prefixes, ca)
		if err != nil {
			closeClients()
			return nil, nil, err
		}
		transport[u.Scheme+"://"+u.Host] = client.Transport
	}
	return &http.Client{Transport: transport, Timeout: 20e9, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, closeClients, nil
}
