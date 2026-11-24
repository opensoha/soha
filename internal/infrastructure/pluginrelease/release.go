// Package pluginrelease verifies and stages signed GitHub plugin version packages.
package pluginrelease

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"encoding/pem"
	"errors"
	"io"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	contract "github.com/opensoha/soha/contracts/plugins"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/infrastructure/githubplugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	manifestName      = "plugin.manifest.json"
	attestationName   = "soha-plugin-attestation.json"
	signatureName     = "soha-plugin-attestation.sig"
	PublisherFilename = "soha-plugin-publisher.pem"
	maxDocument       = 1 << 20
	maxPublisherPEM   = 4096
	maxAsset          = 256 << 20
)

var (
	errReleaseRequired = apperrors.NewBusiness(apperrors.ErrConflict, "plugin_repository_signed_release_required",
		"Publish a signed GitHub Release for this commit and preview again.", "请为此提交发布带签名的 GitHub Release 后重新预览。")
	errPublisher = apperrors.NewBusiness(apperrors.ErrConflict, "plugin_repository_publisher_untrusted",
		"The release publisher does not match the trusted Ed25519 key.", "发布者与受信的 Ed25519 公钥不匹配。")
	errPublisherInvalid = apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_publisher_invalid",
		"The signed release has no valid Ed25519 publisher key.", "签名发行版没有有效的 Ed25519 发布者公钥。")
	errSignature = apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_signature_invalid",
		"The release signature is invalid.", "发行版签名无效。")
	errInventory = apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_inventory_invalid",
		"The signed release identity or artifact inventory does not match the pinned manifest.", "签名发行版的身份或制品清单与固定清单不匹配。")
	errPlatform = apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_platform_unsupported",
		"The signed release has no backend artifact for this host platform.", "签名发行版没有适用于本机平台的后端制品。")
	errArtifact = apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_artifact_invalid",
		"The release artifact failed byte length, SHA-256 or Web archive verification.", "发行版制品未通过字节长度、SHA-256 或 Web 归档验证。")
)

type releaseSource interface {
	MatchingReleases(context.Context, domainplugin.PluginRepositoryPreviewSource, string) ([]githubplugin.Release, error)
	CopyReleaseAsset(context.Context, githubplugin.Release, string, int64, io.Writer) error
}

// Store downloads public release assets into the configured protected package store.
type Store struct {
	client releaseSource
	root   string
}

// New creates a store for the given client and package root.
func New(client *githubplugin.Client, packageRoot string) *Store {
	return &Store{client: client, root: packageRoot}
}

type packageRelease struct {
	release     githubplugin.Release
	attestation *sohaapi.PluginReleaseAttestation
	fingerprint string
	documents   map[string][]byte
	artifacts   []sohaapi.PluginReleaseArtifact
}

// Inspect verifies release evidence and the complete inventory without downloading
// or persisting the executable artifacts. The candidate key can verify a signature
// but only the caller can grant publisher trust.
func (store *Store) Inspect(
	ctx context.Context, source domainplugin.PluginRepositoryPreviewSource, manifest domainplugin.PluginManifest,
	rawManifest []byte, trustedFingerprint string, trustedPublicKey []byte,
) (*sohaapi.PluginReleaseAttestation, string, bool, error) {
	release, err := store.inspect(ctx, source, manifest, rawManifest, trustedFingerprint, trustedPublicKey)
	if release == nil || err != nil {
		return nil, "", false, err
	}
	return release.attestation, release.fingerprint, true, nil
}

func (store *Store) inspect(
	ctx context.Context, source domainplugin.PluginRepositoryPreviewSource, manifest domainplugin.PluginManifest,
	rawManifest []byte, trustedFingerprint string, trustedPublicKey []byte,
) (*packageRelease, error) {
	releases, err := store.client.MatchingReleases(ctx, source, manifest.Version)
	if err != nil {
		return nil, err
	}
	for _, release := range releases {
		_, attested := release.AssetSize(attestationName)
		_, signed := release.AssetSize(signatureName)
		if attested && signed {
			return store.verify(ctx, release, source, manifest, rawManifest, trustedFingerprint, trustedPublicKey)
		}
	}
	return nil, nil
}

func (store *Store) verify(
	ctx context.Context, release githubplugin.Release, source domainplugin.PluginRepositoryPreviewSource,
	manifest domainplugin.PluginManifest, rawManifest []byte, trustedFingerprint string, trustedPublicKey []byte,
) (*packageRelease, error) {
	if size, _ := release.AssetSize(signatureName); size != ed25519.SignatureSize {
		return nil, errSignature
	}
	publicKey, publisherPEM, err := store.publisher(ctx, release, trustedPublicKey)
	if err != nil {
		return nil, err
	}
	fingerprint := digestBytes(publicKey)
	if trustedFingerprint != "" && fingerprint != trustedFingerprint {
		return nil, errPublisher
	}
	attestationBytes, err := store.assetBytes(ctx, release, attestationName, maxDocument)
	if err != nil {
		if invalidAssetLength(err) {
			return nil, errInventory
		}
		return nil, err
	}
	signature, err := store.assetBytes(ctx, release, signatureName, ed25519.SignatureSize)
	if err != nil {
		if invalidAssetLength(err) {
			return nil, errSignature
		}
		return nil, err
	}
	if !ed25519.Verify(publicKey, attestationBytes, signature) {
		return nil, errSignature
	}
	attestation, err := verifyAttestation(source, manifest, rawManifest, attestationBytes)
	if err != nil {
		return nil, err
	}
	artifacts, err := packageArtifacts(attestation.Components, runtime.GOOS+"/"+runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	for _, asset := range artifacts {
		if size, found := release.AssetSize(asset.Name); !found || size != asset.ByteLength {
			return nil, errInventory
		}
	}
	return &packageRelease{
		release: release, attestation: attestation, fingerprint: fingerprint, artifacts: artifacts,
		documents: map[string][]byte{
			manifestName: rawManifest, attestationName: attestationBytes, signatureName: signature, PublisherFilename: publisherPEM,
		},
	}, nil
}

func (store *Store) assetBytes(ctx context.Context, release githubplugin.Release, name string, limit int64) ([]byte, error) {
	var output bytes.Buffer
	if err := store.client.CopyReleaseAsset(ctx, release, name, limit, &output); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (store *Store) publisher(
	ctx context.Context, release githubplugin.Release, trustedKey []byte,
) (ed25519.PublicKey, []byte, error) {
	if len(trustedKey) != 0 && len(trustedKey) != ed25519.PublicKeySize {
		return nil, nil, errPublisher
	}
	key := ed25519.PublicKey(trustedKey)
	if _, published := release.AssetSize(PublisherFilename); published {
		raw, err := store.assetBytes(ctx, release, PublisherFilename, maxPublisherPEM)
		if err != nil {
			if invalidAssetLength(err) {
				return nil, nil, errPublisherInvalid
			}
			return nil, nil, err
		}
		candidate, err := parsePublisher(raw)
		if err != nil {
			return nil, nil, err
		}
		if len(key) != 0 && !key.Equal(candidate) {
			return nil, nil, errPublisher
		}
		key = candidate
	}
	if len(key) == 0 {
		return nil, nil, errPublisherInvalid
	}
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, nil, errPublisherInvalid
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func parsePublisher(raw []byte) (ed25519.PublicKey, error) {
	raw = bytes.TrimSpace(raw)
	block, rest := pem.Decode(raw)
	if !bytes.HasPrefix(raw, []byte("-----BEGIN PUBLIC KEY-----")) || bytes.Count(raw, []byte("-----BEGIN ")) != 1 ||
		block == nil || block.Type != "PUBLIC KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errPublisherInvalid
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	publicKey, ok := key.(ed25519.PublicKey)
	if err != nil || !ok || len(publicKey) != ed25519.PublicKeySize {
		return nil, errPublisherInvalid
	}
	return publicKey, nil
}

type releaseSchemas struct {
	manifest    *jsonschema.Schema
	attestation *jsonschema.Schema
}

var schemasForRelease = sync.OnceValues(func() (releaseSchemas, error) {
	const prefix = "https://contracts.opensoha.dev/plugins/"
	const manifest = prefix + "plugin-manifest.schema.json"
	const attestation = prefix + "plugin-release-attestation.schema.json"
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	// The repository distribution path excludes companion packs. Disable the
	// companion schema reference so compilation uses only the embedded files.
	var manifestDocument map[string]any
	if err := jsonv2.Unmarshal(contract.ManifestSchema(), &manifestDocument); err != nil {
		return releaseSchemas{}, err
	}
	properties, ok := manifestDocument["properties"].(map[string]any)
	if !ok {
		return releaseSchemas{}, errors.New("plugin manifest schema properties are missing")
	}
	properties["companionPack"] = false
	if err := compiler.AddResource(manifest, manifestDocument); err != nil {
		return releaseSchemas{}, err
	}
	attestationDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(contract.ReleaseAttestationSchema()))
	if err != nil {
		return releaseSchemas{}, err
	}
	if err := compiler.AddResource(attestation, attestationDocument); err != nil {
		return releaseSchemas{}, err
	}
	manifestSchema, err := compiler.Compile(manifest)
	if err != nil {
		return releaseSchemas{}, err
	}
	attestationSchema, err := compiler.Compile(attestation)
	return releaseSchemas{manifest: manifestSchema, attestation: attestationSchema}, err
})

func verifyAttestation(
	source domainplugin.PluginRepositoryPreviewSource, manifest domainplugin.PluginManifest, rawManifest, rawAttestation []byte,
) (*sohaapi.PluginReleaseAttestation, error) {
	schemas, err := schemasForRelease()
	if err != nil {
		return nil, err
	}
	var attestation sohaapi.PluginReleaseAttestation
	var pinned domainplugin.PluginManifest
	if !decodeReleaseDocument(rawAttestation, schemas.attestation, &attestation) ||
		!decodeReleaseDocument(rawManifest, schemas.manifest, &pinned) {
		return nil, errInventory
	}
	digest := digestBytes(rawManifest)
	components := sohaapi.PluginPackageComponents{}
	if pinned.Components != nil {
		components = *pinned.Components
	}
	if attestation.Repository != source.CanonicalSource || attestation.CommitSha != source.CommitSha ||
		attestation.RawManifestSha256 != digest || source.RawManifestChecksum != "sha256:"+digest ||
		attestation.PluginID != pinned.ID || attestation.Version != pinned.Version ||
		pinned.ID != manifest.ID || pinned.Version != manifest.Version ||
		runtimeMode(pinned) != runtimeMode(manifest) ||
		!reflect.DeepEqual(pinned.Components, manifest.Components) || !reflect.DeepEqual(components, attestation.Components) {
		return nil, errInventory
	}
	return &attestation, nil
}

func decodeReleaseDocument(raw []byte, schema *jsonschema.Schema, output any) bool {
	var value any
	// jsonv2 rejects duplicate members and invalid UTF-8 at any nesting level.
	return len(raw) <= maxDocument && jsonv2.Unmarshal(raw, &value) == nil &&
		schema.Validate(value) == nil && jsonv2.Unmarshal(raw, output) == nil
}

func runtimeMode(manifest domainplugin.PluginManifest) string {
	if manifest.Runtime == nil {
		return ""
	}
	return string(manifest.Runtime.Mode)
}

func packageArtifacts(components sohaapi.PluginPackageComponents, platform string) ([]sohaapi.PluginReleaseArtifact, error) {
	seen := map[string]bool{manifestName: true, attestationName: true, signatureName: true, PublisherFilename: true}
	check := func(asset sohaapi.PluginReleaseArtifact) bool {
		name := strings.ToLower(asset.Name)
		if seen[name] || asset.ByteLength > maxAsset {
			return false
		}
		seen[name] = true
		return true
	}
	assets := make([]sohaapi.PluginReleaseArtifact, 0, 2)
	if components.Backend != nil {
		for _, asset := range components.Backend.Platforms {
			if !check(asset) {
				return nil, errInventory
			}
		}
		binary, ok := components.Backend.Platforms[platform]
		if !ok {
			return nil, errPlatform
		}
		assets = append(assets, binary)
	}
	if components.Web != nil {
		if !check(*components.Web) {
			return nil, errInventory
		}
		assets = append(assets, *components.Web)
	}
	return assets, nil
}

func digestBytes(input []byte) string {
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:])
}

func invalidAssetLength(err error) bool {
	return errors.Is(err, githubplugin.ErrAssetLengthMismatch) || errors.Is(err, githubplugin.ErrResponseTooLarge)
}
