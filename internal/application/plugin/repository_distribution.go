package plugin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	repositoryPreviewLifetime = 15 * time.Minute
	maxRepositoryPreviewToken = 8192
	repositoryPreviewPurpose  = "soha.plugin.repository.preview.v1"
	repositoryPreviewScope    = "default/default"
)

var repositoryCommitSHA = regexp.MustCompile("^[0-9a-f]{40}$")

type RepositoryManifestSource interface {
	Resolve(ctx context.Context, repositoryURL, ref string) (canonicalSource, pinnedRef, commitSHA string, err error)
	FetchManifest(ctx context.Context, canonicalSource, commitSHA string) ([]byte, error)
}

type repositoryDistribution struct {
	source RepositoryManifestSource
	key    []byte
	now    func() time.Time
}

// WithRepositoryDistribution binds previews to this Core installation.
func WithRepositoryDistribution(source RepositoryManifestSource, key []byte) Option {
	return func(s *Service) {
		if source != nil && len(key) >= 32 {
			s.repositories = &repositoryDistribution{source: source, key: bytes.Clone(key), now: time.Now}
		}
	}
}

type repositoryPreviewClaims struct {
	Caller              string
	Scope               string
	Repository          string
	Ref                 string
	Commit              string
	ID                  string
	Version             string
	Checksum            string
	Expires             int64
	InstalledAt         time.Time
	UpdatedAt           time.Time
	MarketplaceSourceID string `json:",omitempty"`
	MarketplaceURL      string `json:",omitempty"`
	MarketplaceOfficial bool   `json:",omitempty"`
}

func (s *Service) PreviewRepository(ctx context.Context, actor domainidentity.Principal, input domainplugin.PluginRepositoryPreviewRequest) (domainplugin.PluginRepositoryPreview, error) {
	if err := s.authorizeRepository(ctx, actor, appaccess.PermPluginInstall); err != nil {
		return domainplugin.PluginRepositoryPreview{}, err
	}
	claims := repositoryPreviewClaims{Caller: actor.UserID, Scope: repositoryPreviewScope, ID: input.PluginID}
	if input.MarketplaceSourceID != "" {
		if input.PluginID == "" || input.RepositoryURL == "" {
			return domainplugin.PluginRepositoryPreview{}, apperrors.ErrInvalidArgument
		}
		entry, err := s.marketplaceRepositoryRegistration(ctx, input.MarketplaceSourceID, "", input.PluginID, input.RepositoryURL)
		if errors.Is(err, apperrors.ErrConflict) {
			return domainplugin.PluginRepositoryPreview{}, errMarketplaceRepositoryMismatch
		}
		if err != nil {
			return domainplugin.PluginRepositoryPreview{}, err
		}
		claims.MarketplaceSourceID, claims.MarketplaceURL, claims.MarketplaceOfficial = entry.SourceID, entry.SourceURL, entry.SourceInfo.Official
	}
	repo, ref, commit, err := s.repositories.source.Resolve(ctx, input.RepositoryURL, input.Ref)
	if err != nil {
		return domainplugin.PluginRepositoryPreview{}, err
	}
	if claims.MarketplaceSourceID != "" && repo != input.RepositoryURL {
		return domainplugin.PluginRepositoryPreview{}, errMarketplaceRepositoryMismatch
	}
	claims.Repository, claims.Ref, claims.Commit = repo, ref, commit
	resolved, err := s.repositories.fetch(ctx, claims)
	if err != nil {
		return domainplugin.PluginRepositoryPreview{}, err
	}
	claims.ID, claims.Version, claims.Checksum = resolved.Manifest.ID, resolved.Manifest.Version, resolved.RawChecksum
	current, err := s.repo.GetInstalled(ctx, claims.ID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return domainplugin.PluginRepositoryPreview{}, err
	}
	if repositoryInstalled(current) && samePluginSource(current.SourceInfo, resolved.SourceInfo) {
		claims.InstalledAt, claims.UpdatedAt = current.InstalledAt, current.UpdatedAt
	}
	claims.Expires = s.repositories.now().Add(repositoryPreviewLifetime).Unix()
	token, err := s.repositories.sign(claims)
	if err != nil {
		return domainplugin.PluginRepositoryPreview{}, err
	}
	return domainplugin.PluginRepositoryPreview{
		Manifest: resolved.Manifest, PreviewToken: token, ExpiresAt: time.Unix(claims.Expires, 0).UTC(),
		ChecksumStatus: resolved.ChecksumStatus, SignatureStatus: resolved.SignatureStatus,
		SourceInfo: domainplugin.PluginRepositoryPreviewSource{
			Kind: string(domainplugin.SourceKindGitHubRepo), CanonicalSource: repo, PinnedRef: ref,
			CommitSha: commit, RawManifestChecksum: claims.Checksum, Official: claims.MarketplaceOfficial,
		},
	}, nil
}

func validateRepositoryReleaseVersion(ref, manifestVersion string) error {
	tag, ok := strings.CutPrefix(ref, "refs/tags/")
	if !ok {
		return nil
	}
	release, err := repositorySemver(tag)
	if err != nil {
		return nil
	}
	manifest, err := repositorySemver(manifestVersion)
	if err != nil || manifest.String() != release.String() {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_release_version_mismatch",
			"The release tag and plugin manifest declare different versions.", "发布标签与插件清单声明的版本不一致。")
	}
	return nil
}

func (s *Service) InstallRepository(ctx context.Context, actor domainidentity.Principal, input domainplugin.PluginRepositoryInstallRequest) (domainplugin.InstalledPlugin, error) {
	if err := s.authorizeRepository(ctx, actor, appaccess.PermPluginInstall); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	claims, err := s.repositories.verify(input.PreviewToken, actor.UserID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := s.checkRepositoryMarketplaceBinding(ctx, claims); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	resolved, err := s.repositories.fetch(ctx, claims)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	return s.installResolved(ctx, actor, domainplugin.PluginInstallRequest{Enable: input.Enable}, resolved)
}

func (s *Service) UpgradeRepository(ctx context.Context, actor domainidentity.Principal, pluginID string, input domainplugin.PluginRepositoryUpgradeRequest) (domainplugin.InstalledPlugin, error) {
	if err := s.authorizeRepository(ctx, actor, appaccess.PermPluginUpgrade); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	claims, err := s.repositories.verify(input.PreviewToken, actor.UserID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	current, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if !repositoryInstalled(current) || claims.ID != pluginID || !samePluginSource(current.SourceInfo, claims.sourceInfo(claims.Checksum)) {
		return domainplugin.InstalledPlugin{}, repositorySourceConflict()
	}
	if err := s.checkRepositoryMarketplaceBinding(ctx, claims); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if !current.InstalledAt.Equal(claims.InstalledAt) || !current.UpdatedAt.Equal(claims.UpdatedAt) {
		if current.Version == claims.Version && current.SourceInfo.RawManifestChecksum == claims.Checksum {
			return current, nil
		}
		return domainplugin.InstalledPlugin{}, repositoryPreviewInvalid()
	}
	resolved, err := s.repositories.fetch(ctx, claims)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if permissionsIncreased(current.RequestedPermissions, resolved.Manifest.Permissions) && !input.ConfirmPermissionIncrease {
		return domainplugin.InstalledPlugin{}, apperrors.NewBusiness(apperrors.ErrConflict, "plugin_permission_increase", "Review and confirm the additional plugin permissions.", "请审阅并确认新增插件权限。")
	}
	if current.Version == claims.Version {
		if current.SourceInfo.RawManifestChecksum == claims.Checksum {
			return current, nil
		}
		return domainplugin.InstalledPlugin{}, apperrors.NewBusiness(apperrors.ErrConflict, "plugin_version_immutable", "The installed version is bound to different manifest bytes. Select a new version.", "当前版本已绑定其他清单内容，请选择新版本。")
	}
	return s.upgradeResolved(ctx, actor, current, domainplugin.PluginInstallRequest{}, resolved)
}

func (s *Service) checkRepositoryMarketplaceBinding(ctx context.Context, claims repositoryPreviewClaims) error {
	if claims.MarketplaceSourceID == "" {
		return nil
	}
	entry, err := s.marketplaceRepositoryRegistration(ctx, claims.MarketplaceSourceID, claims.MarketplaceURL, claims.ID, claims.Repository)
	if errors.Is(err, apperrors.ErrNotFound) {
		return repositorySourceConflict()
	}
	if err != nil {
		return err
	}
	if entry.SourceInfo.Official != claims.MarketplaceOfficial {
		return repositorySourceConflict()
	}
	return nil
}

func (s *Service) authorizeRepository(ctx context.Context, actor domainidentity.Principal, permission string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, actor, permission); err != nil {
		return err
	}
	if actor.UserID == "" {
		return apperrors.ErrUnauthorized
	}
	if s.repositories == nil {
		return apperrors.ErrUnsupportedOperation
	}
	return nil
}

func (d *repositoryDistribution) sign(claims repositoryPreviewClaims) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(d.mac(raw))
	if len(token) > maxRepositoryPreviewToken {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_preview_too_large", "The plugin identifier or version exceeds the repository preview limit.", "插件标识或版本超出仓库预览长度限制。")
	}
	return token, nil
}

func (d *repositoryDistribution) verify(token, caller string) (repositoryPreviewClaims, error) {
	var claims repositoryPreviewClaims
	if len(token) > maxRepositoryPreviewToken {
		return claims, repositoryPreviewInvalid()
	}
	payload, signature, ok := strings.Cut(token, ".")
	if !ok {
		return claims, repositoryPreviewInvalid()
	}
	raw, rawErr := base64.RawURLEncoding.Strict().DecodeString(payload)
	mac, macErr := base64.RawURLEncoding.Strict().DecodeString(signature)
	if rawErr != nil || macErr != nil || !hmac.Equal(mac, d.mac(raw)) || json.Unmarshal(raw, &claims) != nil ||
		claims.Caller != caller || claims.Scope != repositoryPreviewScope || d.now().Unix() >= claims.Expires {
		return repositoryPreviewClaims{}, repositoryPreviewInvalid()
	}
	return claims, nil
}

func (d *repositoryDistribution) mac(raw []byte) []byte {
	mac := hmac.New(sha256.New, d.key)
	// This installation-scoped key signs only this version of the repository preview.
	_, _ = mac.Write([]byte(repositoryPreviewPurpose + "\x00"))
	_, _ = mac.Write(raw)
	return mac.Sum(nil)
}

func (d *repositoryDistribution) fetch(ctx context.Context, claims repositoryPreviewClaims) (ResolvedManifest, error) {
	if !repositoryCommitSHA.MatchString(claims.Commit) {
		return ResolvedManifest{}, repositoryPreviewInvalid()
	}
	raw, err := d.source.FetchManifest(ctx, claims.Repository, claims.Commit)
	if err != nil {
		return ResolvedManifest{}, err
	}
	checksum := checksumBytes(raw)
	if claims.Checksum != "" && checksum != claims.Checksum {
		return ResolvedManifest{}, repositoryPreviewInvalid()
	}
	manifest, err := decodeRepositoryManifest(raw)
	if err != nil {
		return ResolvedManifest{}, err
	}
	if (claims.ID != "" && manifest.ID != claims.ID) || (claims.Version != "" && manifest.Version != claims.Version) {
		if claims.Checksum == "" && claims.MarketplaceSourceID != "" {
			return ResolvedManifest{}, errMarketplaceRepositoryMismatch
		}
		return ResolvedManifest{}, repositoryPreviewInvalid()
	}
	if err := validateRepositoryReleaseVersion(claims.Ref, manifest.Version); err != nil {
		return ResolvedManifest{}, err
	}
	source := string(domainplugin.SourceKindGitHubRepo)
	if claims.MarketplaceSourceID != "" {
		source = "marketplace:" + manifest.ID
	}
	return ResolvedManifest{
		Manifest: manifest, RawChecksum: checksum, ChecksumStatus: "verified",
		SignatureStatus: integrityStatus(manifest), Source: source, SourceID: claims.MarketplaceSourceID,
		MarketplaceURL: claims.MarketplaceURL, SourceInfo: claims.sourceInfo(checksum),
	}, nil
}

func (claims repositoryPreviewClaims) sourceInfo(checksum string) *domainplugin.PluginSourceInfo {
	info := &domainplugin.PluginSourceInfo{
		Kind: domainplugin.SourceKindGitHubRepo, CanonicalSource: claims.Repository,
		PinnedRef: claims.Ref, CommitSha: claims.Commit, RawManifestChecksum: checksum,
	}
	if claims.MarketplaceSourceID != "" {
		info.Kind, info.CanonicalSource = domainplugin.SourceKindMarketplace, claims.MarketplaceURL
		info.SourceID, info.RepositoryURL, info.Official = claims.MarketplaceSourceID, claims.Repository, claims.MarketplaceOfficial
	}
	return info
}

func repositoryPreviewInvalid() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "plugin_preview_invalid", "The repository preview is no longer valid. Preview the plugin again.", "仓库预览已失效，请重新预览插件。")
}

func repositorySourceConflict() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "plugin_source_conflict", "This plugin is bound to its installed source. Use a same-source upgrade or remove it before changing the source.", "插件已绑定安装来源。请使用同来源升级，或移除后再切换来源。")
}

func repositoryInstalled(item domainplugin.InstalledPlugin) bool {
	return item.SourceInfo != nil && (item.SourceInfo.Kind == domainplugin.SourceKindGitHubRepo ||
		(item.SourceInfo.Kind == domainplugin.SourceKindMarketplace && item.SourceInfo.RepositoryURL != ""))
}

func samePluginSource(a, b *domainplugin.PluginSourceInfo) bool {
	return a != nil && b != nil && a.Kind == b.Kind && a.CanonicalSource == b.CanonicalSource &&
		a.SourceID == b.SourceID && a.RepositoryURL == b.RepositoryURL
}

func permissionsIncreased(current, next *domainplugin.PluginPermissionRequest) bool {
	if next == nil {
		return false
	}
	if current == nil {
		return len(next.Required) > 0 || len(next.Domain) > 0
	}
	return permissionAdded(current.Required, next.Required) || permissionAdded(current.Domain, next.Domain)
}

func permissionAdded(current, next []string) bool {
	return slices.ContainsFunc(next, func(key string) bool { return !slices.Contains(current, key) })
}
