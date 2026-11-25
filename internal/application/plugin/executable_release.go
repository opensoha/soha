package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sync"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ExecutableReleaseStore interface {
	Inspect(context.Context, domainplugin.PluginRepositoryPreviewSource, domainplugin.PluginManifest, []byte, string, []byte) (*sohaapi.PluginReleaseAttestation, string, bool, error)
	Stage(context.Context, domainplugin.PluginRepositoryPreviewSource, domainplugin.PluginManifest, []byte, string, []byte, string) (string, error)
	RemovePlugin(context.Context, string) error
	VerifyLocal(context.Context, domainplugin.PluginRepositoryPreviewSource, domainplugin.PluginManifest, string, string) error
}

type ExecutableRuntime interface {
	Switch(context.Context, domainplugin.InstalledPlugin, func() error) error
	Disable(context.Context, string, func() error) error
	WaitRetired(context.Context, string) error
	Ready(pluginID, version string) bool
}

type StorageDriverReferences interface {
	WithStorageDriverReferences(context.Context, string, func([][]sohaapi.SystemIntegrationConfigurationField) error) error
	StorageDriverReferenceCount(context.Context, string) (int, error)
}

type executablePlugins struct {
	assets     ExecutableReleaseStore
	runtime    ExecutableRuntime
	references StorageDriverReferences
	keys       map[string]string
	mu         sync.Mutex
}

// SetExecutablePlugins runs during bootstrap before the service accepts requests.
func (s *Service) SetExecutablePlugins(assets ExecutableReleaseStore, runtime ExecutableRuntime, references StorageDriverReferences, keys map[string]string) {
	s.executable = &executablePlugins{assets: assets, runtime: runtime, references: references, keys: keys}
}

const (
	trustConfigured   = "configured"
	trustCatalog      = "catalog"
	trustInstalled    = "installed"
	trustAcknowledged = "acknowledged"
)

var publisherFingerprintPattern = regexp.MustCompile("^[0-9a-f]{64}$")

func executableManifest(manifest domainplugin.PluginManifest) bool {
	return manifest.Runtime != nil && manifest.Runtime.Mode == "executable-package-v1"
}

func storageDriverID(manifest domainplugin.PluginManifest) string {
	if manifest.Components != nil && manifest.Components.Backend != nil {
		return manifest.Components.Backend.DriverID
	}
	return ""
}

func (claims repositoryPreviewClaims) previewSource() domainplugin.PluginRepositoryPreviewSource {
	return domainplugin.PluginRepositoryPreviewSource{
		Kind: string(domainplugin.SourceKindGitHubRepo), CanonicalSource: claims.Repository,
		PinnedRef: claims.Ref, CommitSha: claims.Commit, RawManifestChecksum: claims.Checksum, Official: claims.MarketplaceOfficial,
	}
}

func repositoryVersionMatches(item domainplugin.InstalledPlugin, claims repositoryPreviewClaims) bool {
	if item.Version != claims.Version || item.SourceInfo.RawManifestChecksum != claims.Checksum {
		return false
	}
	return !executableManifest(item.Manifest) || (item.SourceInfo.CommitSha == claims.Commit &&
		item.SourceInfo.PublisherKeyFingerprint == claims.PublisherFingerprint && item.SourceInfo.ReleaseAttestationDigest == claims.ReleaseDigest)
}

type executablePublisher struct {
	fingerprint string
	key         []byte
	trust       string
}

func publisherFromKey(encoded, trust string) executablePublisher {
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return executablePublisher{}
	}
	sum := sha256.Sum256(raw)
	return executablePublisher{key: raw, fingerprint: hex.EncodeToString(sum[:]), trust: trust}
}

func (s *Service) executablePublishers(repository string, official bool, catalog []string) []executablePublisher {
	var publishers []executablePublisher
	for _, encoded := range catalog {
		if key := publisherFromKey(encoded, trustCatalog); key.fingerprint != "" {
			publishers = append(publishers, key)
		}
	}
	if !official {
		if key := publisherFromKey(s.executable.keys[repository], trustConfigured); key.fingerprint != "" {
			publishers = append(publishers, key)
		}
	}
	return publishers
}

func (s *Service) previewExecutable(ctx context.Context, claims *repositoryPreviewClaims, preview *domainplugin.PluginRepositoryPreview,
	current domainplugin.InstalledPlugin, resolved ResolvedManifest, catalog []string, acknowledged string) error {
	preview.PreviewToken = "preview-only"
	preview.SignatureStatus = "not_provided"
	preview.InstallEligibility = "signed-release-required"
	if s.executable == nil {
		return nil
	}
	publishers, err := s.previewPublishers(*claims, current, catalog, acknowledged)
	if err != nil {
		return err
	}
	var lastErr error
	for _, publisher := range publishers {
		attestation, fingerprint, matched, err := s.executable.assets.Inspect(ctx, preview.SourceInfo, resolved.Manifest,
			resolved.rawManifest, publisher.fingerprint, publisher.key)
		if err != nil {
			lastErr = err
			continue
		}
		if !matched {
			continue
		}
		if attestation == nil || !publisherFingerprintPattern.MatchString(fingerprint) ||
			(publisher.fingerprint != "" && fingerprint != publisher.fingerprint) ||
			(acknowledged != "" && fingerprint != acknowledged) {
			return publisherUntrusted()
		}
		preview.ReleaseAttestation, preview.PublisherKeyFingerprint = attestation, fingerprint
		if publisher.trust == "" {
			preview.InstallEligibility, preview.SignatureStatus = "publisher-trust-required", "untrusted"
			return nil
		}
		raw, err := json.Marshal(attestation)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		claims.PublisherFingerprint, claims.ReleaseDigest, claims.PublisherTrust = fingerprint, hex.EncodeToString(sum[:]), publisher.trust
		preview.PreviewToken = ""
		preview.InstallEligibility, preview.SignatureStatus = "confirmable", "verified"
		return nil
	}
	return lastErr
}

func (s *Service) previewPublishers(claims repositoryPreviewClaims, current domainplugin.InstalledPlugin, catalog []string, acknowledged string) ([]executablePublisher, error) {
	publishers := s.executablePublishers(claims.Repository, claims.MarketplaceOfficial, catalog)
	if claims.MarketplaceOfficial {
		if acknowledged != "" {
			return nil, publisherUntrusted()
		}
		if len(publishers) == 0 {
			publishers = []executablePublisher{{}}
		}
	} else if claims.InstalledAt.IsZero() {
		if len(publishers) == 0 {
			publishers = []executablePublisher{{fingerprint: acknowledged}}
			if acknowledged != "" {
				publishers[0].trust = trustAcknowledged
			}
		}
	} else if fingerprint := current.SourceInfo.PublisherKeyFingerprint; fingerprint != "" {
		var pinned executablePublisher
		for _, publisher := range publishers {
			if publisher.fingerprint == fingerprint {
				pinned = publisher
				break
			}
		}
		pinned.fingerprint, pinned.trust = fingerprint, trustInstalled
		publishers = []executablePublisher{pinned}
	} else {
		return nil, repositorySourceConflict()
	}
	return publishers, nil
}

func (s *Service) executableConfirmationKey(ctx context.Context, actor domainidentity.Principal, claims repositoryPreviewClaims) ([]byte, error) {
	var catalog []string
	if claims.MarketplaceOfficial {
		entry, err := s.marketplaceRepositoryRegistration(ctx, claims.MarketplaceSourceID, claims.MarketplaceURL, claims.ID, claims.Repository)
		if err != nil || !entry.SourceInfo.Official {
			return nil, repositoryPreviewInvalid()
		}
		catalog = entry.PublisherKeys
	}
	publishers := s.executablePublishers(claims.Repository, claims.MarketplaceOfficial, catalog)
	for _, publisher := range publishers {
		if claims.PublisherFingerprint == publisher.fingerprint && (claims.PublisherTrust == publisher.trust || claims.PublisherTrust == trustInstalled) {
			return publisher.key, nil
		}
	}
	if claims.MarketplaceOfficial {
		return nil, publisherUntrusted()
	}
	switch claims.PublisherTrust {
	case trustAcknowledged:
		return nil, s.authorizeRepository(ctx, actor, appaccess.PermPluginManage)
	case trustInstalled:
		current, err := s.repo.GetInstalled(ctx, claims.ID)
		if err == nil && samePluginSource(current.SourceInfo, claims.sourceInfo(claims.Checksum)) &&
			current.SourceInfo.PublisherKeyFingerprint == claims.PublisherFingerprint {
			return nil, nil
		}
	}
	return nil, publisherUntrusted()
}

func (s *Service) prepareExecutable(ctx context.Context, actor domainidentity.Principal, claims repositoryPreviewClaims, resolved *ResolvedManifest) error {
	if s.executable == nil || !publisherFingerprintPattern.MatchString(claims.PublisherFingerprint) ||
		!publisherFingerprintPattern.MatchString(claims.ReleaseDigest) {
		return repositoryPreviewInvalid()
	}
	key, err := s.executableConfirmationKey(ctx, actor, claims)
	if err != nil {
		return err
	}
	if _, err := s.executable.assets.Stage(ctx, claims.previewSource(), resolved.Manifest, resolved.rawManifest,
		claims.PublisherFingerprint, key, claims.ReleaseDigest); err != nil {
		return err
	}
	resolved.SignatureStatus = "verified"
	return nil
}

func publisherUntrusted() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "plugin_repository_publisher_untrusted",
		"The publisher key does not match the trusted key.", "发布者密钥与可信密钥不匹配。")
}
