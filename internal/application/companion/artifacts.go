package companion

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const maxProvenanceBytes = int64(1 << 20)

type PackageFetcher interface {
	Fetch(context.Context, string, int64) (io.ReadCloser, error)
}

type PackageStore interface {
	Install(context.Context, domaincompanion.PackageDescriptor, domaincompanion.PackManifest, io.Reader) (string, error)
	Open(string, string) (io.ReadCloser, error)
}

type ArtifactOptions struct {
	MaxPackageBytes   int64
	AllowLive2D       bool
	RequireSignature  bool
	TrustedPublicKeys map[string]ed25519.PublicKey
}

type ArtifactService struct {
	repo    domaincompanion.Repository
	fetcher PackageFetcher
	store   PackageStore
	options ArtifactOptions
	now     func() time.Time
}

func NewArtifactService(repo domaincompanion.Repository, fetcher PackageFetcher, store PackageStore, options ArtifactOptions) (*ArtifactService, error) {
	if repo == nil || fetcher == nil || store == nil || options.MaxPackageBytes <= 0 {
		return nil, fmt.Errorf("%w: companion artifact dependencies are required", apperrors.ErrInvalidArgument)
	}
	return &ArtifactService{repo: repo, fetcher: fetcher, store: store, options: options, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *ArtifactService) Install(ctx context.Context, manifest domainplugin.PluginManifest, descriptor domainplugin.PluginPackageDescriptor) (domainplugin.PluginArtifactRecord, error) {
	if err := s.validateManifest(manifest); err != nil {
		return domainplugin.PluginArtifactRecord{}, err
	}
	if descriptor.SizeBytes <= 0 || descriptor.SizeBytes > s.options.MaxPackageBytes || strings.TrimSpace(descriptor.Sha256) == "" || strings.TrimSpace(descriptor.URL) == "" {
		return domainplugin.PluginArtifactRecord{}, fmt.Errorf("%w: invalid companion package descriptor", apperrors.ErrInvalidArgument)
	}
	signatureStatus, err := verifyPackageSignature(descriptor, s.options.TrustedPublicKeys, s.options.RequireSignature)
	if err != nil {
		return domainplugin.PluginArtifactRecord{}, err
	}
	provenanceStatus, err := s.verifyProvenance(ctx, descriptor)
	if err != nil {
		return domainplugin.PluginArtifactRecord{}, err
	}
	source, err := s.fetcher.Fetch(ctx, descriptor.URL, s.options.MaxPackageBytes)
	if err != nil {
		return domainplugin.PluginArtifactRecord{}, err
	}
	storageDigest, installErr := s.store.Install(ctx, descriptor, *manifest.CompanionPack, source)
	closeErr := source.Close()
	if installErr != nil {
		return domainplugin.PluginArtifactRecord{}, installErr
	}
	if closeErr != nil {
		return domainplugin.PluginArtifactRecord{}, fmt.Errorf("close companion package response: %w", closeErr)
	}
	installedAt := s.now()
	artifact, err := s.repo.PutArtifact(ctx, domaincompanion.Artifact{
		PluginID: manifest.ID, Version: manifest.Version, SHA256: descriptor.Sha256,
		SizeBytes: descriptor.SizeBytes, ChecksumStatus: "verified", SignatureStatus: signatureStatus,
		ProvenanceStatus: provenanceStatus, StorageDigest: storageDigest,
		Assets: manifest.CompanionPack.Assets, Pack: *manifest.CompanionPack, InstalledAt: installedAt,
	})
	if err != nil {
		return domainplugin.PluginArtifactRecord{}, err
	}
	return artifactRecord(artifact), nil
}

func (s *ArtifactService) Activate(ctx context.Context, pluginID, version string) (domainplugin.PluginArtifactRecord, error) {
	if strings.TrimSpace(version) == "" {
		items, err := s.repo.ListArtifacts(ctx, pluginID)
		if err != nil {
			return domainplugin.PluginArtifactRecord{}, err
		}
		if len(items) == 0 {
			return domainplugin.PluginArtifactRecord{}, fmt.Errorf("%w: companion artifact not found", apperrors.ErrNotFound)
		}
		version = items[0].Version
	}
	item, err := s.repo.ActivateArtifact(ctx, pluginID, version, s.now())
	if err != nil {
		return domainplugin.PluginArtifactRecord{}, err
	}
	return artifactRecord(item), nil
}

func (s *ArtifactService) Rollback(ctx context.Context, pluginID, version string) (domainplugin.PluginArtifactRecord, error) {
	items, err := s.repo.ListArtifacts(ctx, pluginID)
	if err != nil {
		return domainplugin.PluginArtifactRecord{}, err
	}
	if strings.TrimSpace(version) == "" {
		for _, item := range items {
			if !item.Active {
				version = item.Version
				break
			}
		}
	}
	if strings.TrimSpace(version) == "" {
		return domainplugin.PluginArtifactRecord{}, fmt.Errorf("%w: no retained companion version is available", apperrors.ErrConflict)
	}
	return s.Activate(ctx, pluginID, version)
}

func (s *ArtifactService) Remove(ctx context.Context, pluginID string) error {
	return s.repo.RetireArtifacts(ctx, pluginID, s.now())
}

func (s *ArtifactService) OpenAsset(ctx context.Context, pluginID, assetPath string) (io.ReadCloser, string, string, error) {
	artifact, err := s.repo.GetActiveArtifact(ctx, pluginID)
	if err != nil {
		return nil, "", "", err
	}
	for _, asset := range artifact.Assets {
		if asset.Path != assetPath {
			continue
		}
		file, err := s.store.Open(artifact.StorageDigest, assetPath)
		if err != nil {
			return nil, "", "", err
		}
		return file, string(asset.ContentType), asset.Sha256, nil
	}
	return nil, "", "", fmt.Errorf("%w: companion asset is not declared", apperrors.ErrNotFound)
}

func (s *ArtifactService) Records(ctx context.Context, pluginID string) ([]domainplugin.PluginArtifactRecord, error) {
	items, err := s.repo.ListArtifacts(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	records := make([]domainplugin.PluginArtifactRecord, 0, len(items))
	for _, item := range items {
		records = append(records, artifactRecord(item))
	}
	return records, nil
}

func (s *ArtifactService) ActivePack(ctx context.Context, pluginID string) (domaincompanion.PackManifest, error) {
	item, err := s.repo.GetActiveArtifact(ctx, pluginID)
	if err != nil {
		return domaincompanion.PackManifest{}, err
	}
	return item.Pack, nil
}

func (s *ArtifactService) validateManifest(manifest domainplugin.PluginManifest) error {
	if manifest.Type != "companion-pack" || manifest.CompanionPack == nil {
		return fmt.Errorf("%w: companion package manifest is required", apperrors.ErrInvalidArgument)
	}
	if !bool(manifest.CompanionPack.License.RedistributionAllowed) {
		return fmt.Errorf("%w: companion pack license must allow redistribution", apperrors.ErrInvalidArgument)
	}
	if manifest.CompanionPack.Renderer == "live2d-cubism" && !s.options.AllowLive2D {
		return fmt.Errorf("%w: Live2D is disabled until SDK and model distribution licenses are configured", apperrors.ErrUnsupportedOperation)
	}
	if manifest.Runtime != nil && manifest.Runtime.Mode != "manifest-only" {
		return fmt.Errorf("%w: companion packs must use manifest-only runtime", apperrors.ErrInvalidArgument)
	}
	if len(manifest.CompanionPack.Assets) == 0 || len(manifest.CompanionPack.Assets) > 512 {
		return fmt.Errorf("%w: companion pack assets are required", apperrors.ErrInvalidArgument)
	}
	entryFound := false
	seen := map[string]struct{}{}
	for _, asset := range manifest.CompanionPack.Assets {
		if _, exists := seen[asset.Path]; exists {
			return fmt.Errorf("%w: duplicate companion asset path", apperrors.ErrInvalidArgument)
		}
		seen[asset.Path] = struct{}{}
		entryFound = entryFound || asset.Path == manifest.CompanionPack.EntryAsset
	}
	if !entryFound {
		return fmt.Errorf("%w: companion entry asset must be declared", apperrors.ErrInvalidArgument)
	}
	return nil
}

type provenanceStatement struct {
	SubjectSHA256 string `json:"subjectSha256"`
	BuilderID     string `json:"builderId"`
	SourceURI     string `json:"sourceUri"`
	SourceCommit  string `json:"sourceCommit"`
	BuiltAt       string `json:"builtAt"`
	SigningKeyID  string `json:"signingKeyId"`
	Signature     string `json:"signature"`
}

func (s *ArtifactService) verifyProvenance(ctx context.Context, descriptor domainplugin.PluginPackageDescriptor) (string, error) {
	if strings.TrimSpace(descriptor.ProvenanceURL) == "" {
		if s.options.RequireSignature {
			return "not_provided", fmt.Errorf("%w: companion provenance is required", apperrors.ErrInvalidArgument)
		}
		return "not_provided", nil
	}
	statement, err := s.readProvenanceStatement(ctx, descriptor.ProvenanceURL)
	if err != nil {
		return "invalid", err
	}
	if err := validateProvenanceSubject(statement, descriptor.Sha256); err != nil {
		return "invalid", err
	}
	if err := validateProvenanceSource(statement.SourceURI); err != nil {
		return "invalid", err
	}
	if err := validateProvenanceBuildTime(statement.BuiltAt, s.now()); err != nil {
		return "invalid", err
	}
	if err := verifyProvenanceSignature(statement, s.options.TrustedPublicKeys); err != nil {
		return "invalid", err
	}
	return "verified", nil
}

func (s *ArtifactService) readProvenanceStatement(ctx context.Context, provenanceURL string) (provenanceStatement, error) {
	reader, err := s.fetcher.Fetch(ctx, provenanceURL, maxProvenanceBytes)
	if err != nil {
		return provenanceStatement{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, maxProvenanceBytes+1))
	closeErr := reader.Close()
	if readErr != nil || int64(len(raw)) > maxProvenanceBytes {
		return provenanceStatement{}, fmt.Errorf("%w: invalid companion provenance document", apperrors.ErrInvalidArgument)
	}
	if closeErr != nil {
		return provenanceStatement{}, fmt.Errorf("close companion provenance response: %w", closeErr)
	}
	var statement provenanceStatement
	if err := json.Unmarshal(raw, &statement); err != nil {
		return provenanceStatement{}, fmt.Errorf("%w: decode companion provenance", apperrors.ErrInvalidArgument)
	}
	return statement, nil
}

func validateProvenanceSubject(statement provenanceStatement, packageSHA256 string) error {
	if statement.SubjectSHA256 != packageSHA256 || statement.BuilderID == "" || statement.SourceURI == "" || statement.SourceCommit == "" || statement.BuiltAt == "" {
		return fmt.Errorf("%w: incomplete companion provenance", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateProvenanceSource(rawSourceURI string) error {
	sourceURI, err := url.Parse(rawSourceURI)
	if err != nil || sourceURI.Scheme != "https" || sourceURI.Host == "" || sourceURI.User != nil || sourceURI.Fragment != "" {
		return fmt.Errorf("%w: companion provenance source must be credential-free HTTPS", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateProvenanceBuildTime(rawBuiltAt string, now time.Time) error {
	builtAt, err := time.Parse(time.RFC3339, rawBuiltAt)
	if err != nil || builtAt.After(now.Add(5*time.Minute)) {
		return fmt.Errorf("%w: invalid companion provenance build time", apperrors.ErrInvalidArgument)
	}
	return nil
}

func verifyProvenanceSignature(statement provenanceStatement, trusted map[string]ed25519.PublicKey) error {
	key, ok := trusted[statement.SigningKeyID]
	if !ok {
		return fmt.Errorf("%w: untrusted companion provenance key", apperrors.ErrInvalidArgument)
	}
	signature, err := decodeSignature(statement.Signature)
	if err != nil {
		return err
	}
	payload := strings.Join([]string{statement.SubjectSHA256, statement.BuilderID, statement.SourceURI, statement.SourceCommit, statement.BuiltAt}, "\n")
	if !ed25519.Verify(key, []byte(payload), signature) {
		return fmt.Errorf("%w: invalid companion provenance signature", apperrors.ErrInvalidArgument)
	}
	return nil
}

func verifyPackageSignature(descriptor domainplugin.PluginPackageDescriptor, trusted map[string]ed25519.PublicKey, required bool) (string, error) {
	if strings.TrimSpace(descriptor.Signature) == "" {
		if required {
			return "not_provided", fmt.Errorf("%w: companion package signature is required", apperrors.ErrInvalidArgument)
		}
		return "not_provided", nil
	}
	if descriptor.SignatureAlgorithm != "ed25519" || strings.TrimSpace(descriptor.SigningKeyID) == "" {
		return "invalid", fmt.Errorf("%w: companion package signature metadata is incomplete", apperrors.ErrInvalidArgument)
	}
	key, ok := trusted[descriptor.SigningKeyID]
	if !ok {
		return "untrusted", fmt.Errorf("%w: companion package signing key is not trusted", apperrors.ErrInvalidArgument)
	}
	signature, err := decodeSignature(descriptor.Signature)
	if err != nil {
		return "invalid", err
	}
	if !ed25519.Verify(key, []byte(descriptor.Sha256), signature) {
		return "invalid", fmt.Errorf("%w: invalid companion package signature", apperrors.ErrInvalidArgument)
	}
	return "verified", nil
}

func decodeSignature(value string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(value))
	}
	if err != nil || len(raw) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: companion signature must be base64-encoded Ed25519 data", apperrors.ErrInvalidArgument)
	}
	return raw, nil
}

func artifactRecord(item domaincompanion.Artifact) domainplugin.PluginArtifactRecord {
	installedAt := item.InstalledAt
	return domainplugin.PluginArtifactRecord{
		Version: item.Version, Sha256: item.SHA256, SizeBytes: item.SizeBytes,
		ChecksumStatus:   sohaapi.PluginArtifactRecordChecksumStatus(item.ChecksumStatus),
		SignatureStatus:  sohaapi.PluginArtifactRecordSignatureStatus(item.SignatureStatus),
		ProvenanceStatus: sohaapi.PluginArtifactRecordProvenanceStatus(item.ProvenanceStatus),
		Active:           item.Active, InstalledAt: &installedAt,
	}
}

func ActiveVersion(records []domainplugin.PluginArtifactRecord) string {
	for _, record := range records {
		if record.Active {
			return record.Version
		}
	}
	return ""
}

func ReplaceActive(records []domainplugin.PluginArtifactRecord, active domainplugin.PluginArtifactRecord) []domainplugin.PluginArtifactRecord {
	found := false
	for index := range records {
		records[index].Active = records[index].Version == active.Version
		if records[index].Version == active.Version {
			records[index] = active
			found = true
		}
	}
	if !found {
		records = append(records, active)
	}
	slices.SortFunc(records, func(a, b domainplugin.PluginArtifactRecord) int { return strings.Compare(b.Version, a.Version) })
	return records
}
