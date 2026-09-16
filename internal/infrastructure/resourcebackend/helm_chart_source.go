package resourcebackend

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/registry"
	repov1 "helm.sh/helm/v4/pkg/repo/v1"
	"oras.land/oras-go/v2/registry/remote/auth"
	"sigs.k8s.io/yaml"
)

// HelmChartLoader fetches chart packages without reading host registry credentials
// or running Helm plugins. Cluster access and mutations are separate adapters.
type HelmChartLoader struct{}

func (HelmChartLoader) LoadChart(ctx context.Context, source sohaapi.DeploymentTemplateHelmSource, credentials map[string]string) (sohaapi.HelmChartInspection, []byte, error) {
	var result sohaapi.HelmChartInspection
	if err := helmrelease.ValidateSource(source); err != nil {
		return result, nil, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, transport, err := helmChartHTTPClient(ctx, credentials["CHART_CA_CERT"])
	if err != nil {
		return result, nil, err
	}
	defer transport.CloseIdleConnections()
	var content []byte
	if strings.HasPrefix(source.RepositoryURL, "oci://") {
		content, err = pullOCIChart(client, source, credentials)
	} else {
		content, err = pullHTTPSChart(ctx, client, source, credentials)
	}
	if err != nil {
		// Provider failures can contain authenticated URLs or upstream response bodies.
		return result, nil, fmt.Errorf("%w: Helm chart download failed; verify source, credentials, TLS and the size limit", apperrors.ErrClusterUnready)
	}
	if err := helmrelease.ValidateChartArchive(content); err != nil {
		return result, nil, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	result.Digest = helmrelease.SHA256(content)
	if source.Digest != "" && source.Digest != result.Digest {
		return result, nil, fmt.Errorf("%w: Helm chart content changed from the selected digest", apperrors.ErrConflict)
	}
	loaded, err := loader.LoadArchive(bytes.NewReader(content))
	if err != nil {
		return result, nil, fmt.Errorf("%w: invalid Helm chart package", apperrors.ErrInvalidArgument)
	}
	accessor, err := chart.NewAccessor(loaded)
	if err != nil {
		return result, nil, fmt.Errorf("%w: unsupported Helm chart format", apperrors.ErrInvalidArgument)
	}
	result.Name = accessor.Name()
	result.Version, _ = accessor.MetadataAsMap()["Version"].(string)
	if result.Name != source.Chart || result.Version != source.Version {
		return result, nil, fmt.Errorf("%w: Helm chart name or version differs from its source reference", apperrors.ErrConflict)
	}
	values, err := json.Marshal(accessor.Values())
	if err != nil {
		return result, nil, fmt.Errorf("%w: invalid Helm default values", apperrors.ErrInvalidArgument)
	}
	if err := json.Unmarshal(values, &result.DefaultValues); err != nil {
		return result, nil, fmt.Errorf("%w: unsupported Helm default values", apperrors.ErrInvalidArgument)
	}
	result.HasValuesSchema = len(accessor.Schema()) > 0
	if result.HasValuesSchema {
		if err := json.Unmarshal(accessor.Schema(), &result.ValuesSchema); err != nil {
			return result, nil, fmt.Errorf("%w: invalid Helm values schema", apperrors.ErrInvalidArgument)
		}
	}
	return result, content, nil
}

func helmChartHTTPClient(ctx context.Context, certificate string) (*http.Client, *http.Transport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, nil, fmt.Errorf("%w: default HTTP transport is unavailable", apperrors.ErrClusterUnready)
	}
	transport := base.Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if certificate != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(certificate)) {
			return nil, nil, fmt.Errorf("%w: invalid Helm chart CA certificate", apperrors.ErrInvalidArgument)
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	client := &http.Client{Transport: helmChartTransport{ctx: ctx, base: transport}, Timeout: 30 * time.Second}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("helm chart redirect is not permitted")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	}
	return client, transport, nil
}

type helmChartTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t helmChartTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || req.URL.User != nil {
		return nil, errors.New("helm chart transport requires HTTPS")
	}
	// Helm OCI Pull uses a background context; bind every request to this load.
	response, err := t.base.RoundTrip(req.Clone(t.ctx))
	if err != nil {
		return nil, err
	}
	if response.ContentLength > helmrelease.MaxChartArchiveBytes {
		_ = response.Body.Close()
		return nil, errors.New("helm chart response exceeds the download limit")
	}
	response.Body = &helmChartBody{ReadCloser: response.Body, remaining: helmrelease.MaxChartArchiveBytes}
	return response, nil
}

type helmChartBody struct {
	io.ReadCloser
	remaining int64
}

func (b *helmChartBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		var extra [1]byte
		if n, err := b.ReadCloser.Read(extra[:]); n == 0 {
			return 0, err
		}
		return 0, errors.New("helm chart response exceeds the download limit")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}

func pullHTTPSChart(ctx context.Context, client *http.Client, source sohaapi.DeploymentTemplateHelmSource, credentials map[string]string) ([]byte, error) {
	base, err := url.Parse(strings.TrimRight(source.RepositoryURL, "/") + "/")
	if err != nil {
		return nil, err
	}
	indexBytes, err := readHelmChartURL(ctx, client, base.ResolveReference(&url.URL{Path: "index.yaml"}), base, credentials)
	if err != nil {
		return nil, err
	}
	var index repov1.IndexFile
	if err := yaml.Unmarshal(indexBytes, &index); err != nil {
		return nil, errors.New("invalid Helm repository index")
	}
	for _, entry := range index.Entries[source.Chart] {
		if entry.Metadata == nil || entry.Version != source.Version || len(entry.URLs) == 0 {
			continue
		}
		location, err := url.Parse(entry.URLs[0])
		if err != nil {
			return nil, errors.New("invalid Helm chart location")
		}
		content, err := readHelmChartURL(ctx, client, base.ResolveReference(location), base, credentials)
		if err == nil && entry.Digest != "" && strings.TrimPrefix(entry.Digest, "sha256:") != strings.TrimPrefix(helmrelease.SHA256(content), "sha256:") {
			return nil, errors.New("helm repository index digest mismatch")
		}
		return content, err
	}
	return nil, errors.New("exact Helm chart version is not present in the repository")
}

func readHelmChartURL(ctx context.Context, client *http.Client, location, origin *url.URL, credentials map[string]string) ([]byte, error) {
	if location.Scheme != "https" || location.User != nil || location.Fragment != "" {
		return nil, errors.New("invalid Helm chart location")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, err
	}
	if location.Host == origin.Host && (credentials["CHART_USERNAME"] != "" || credentials["CHART_PASSWORD"] != "") {
		req.SetBasicAuth(credentials["CHART_USERNAME"], credentials["CHART_PASSWORD"])
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("helm chart server rejected the download")
	}
	return io.ReadAll(response.Body)
}

func pullOCIChart(client *http.Client, source sohaapi.DeploymentTemplateHelmSource, credentials map[string]string) ([]byte, error) {
	root, err := os.MkdirTemp("", "soha-helm-chart-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(root) }()
	u, err := url.Parse(source.RepositoryURL)
	if err != nil {
		return nil, err
	}
	authorizer := auth.Client{Client: client, Credential: func(_ context.Context, host string) (auth.Credential, error) {
		if host != u.Host {
			return auth.EmptyCredential, nil
		}
		return auth.Credential{Username: credentials["CHART_USERNAME"], Password: credentials["CHART_PASSWORD"]}, nil
	}}
	runtime, err := registry.NewClient(registry.ClientOptHTTPClient(client), registry.ClientOptAuthorizer(authorizer), registry.ClientOptCredentialsFile(filepath.Join(root, "credentials.json")))
	if err != nil {
		return nil, err
	}
	ref := strings.TrimSuffix(strings.TrimPrefix(source.RepositoryURL, "oci://"), "/") + "/" + source.Chart + ":" + strings.ReplaceAll(source.Version, "+", "_")
	result, err := runtime.Pull(ref)
	if err != nil || result == nil || result.Chart == nil {
		return nil, errors.New("OCI Helm chart pull failed")
	}
	return result.Chart.Data, nil
}
