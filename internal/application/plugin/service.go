package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const (
	currentSohaVersion  = "0.1.0"
	statusInstalled     = "installed"
	statusPendingConfig = "pending_config"
	statusEnabled       = "enabled"
	statusDisabled      = "disabled"
	statusFailed        = "failed"
	statusDeprecated    = "deprecated"
)

var supportedPluginTypes = []string{
	"skill",
	"skill-pack",
	"mcp-preset",
	"connector",
	"ai-provider-adapter",
	"agent-profile",
	"gateway-policy-pack",
	"diagnostic",
	"resource-extension",
	"metric-extension",
	"observability-provider",
	"notification-channel",
	"identity-template",
	"ui-extension",
	"companion-pack",
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type CompanionArtifactLifecycle interface {
	Install(context.Context, domainplugin.PluginManifest, domainplugin.PluginPackageDescriptor) (domainplugin.PluginArtifactRecord, error)
	Activate(context.Context, string, string) (domainplugin.PluginArtifactRecord, error)
	Rollback(context.Context, string, string) (domainplugin.PluginArtifactRecord, error)
	Remove(context.Context, string) error
	OpenAsset(context.Context, string, string) (io.ReadCloser, string, string, error)
	Records(context.Context, string) ([]domainplugin.PluginArtifactRecord, error)
	ActivePack(context.Context, string) (domainplugin.CompanionPackManifest, error)
}

type Service struct {
	repo          domainplugin.Repository
	permissions   *appaccess.PermissionResolver
	audit         AuditRecorder
	marketplaceMu sync.RWMutex
	marketplace   MarketplaceProvider
	extensions    *ExtensionRegistry
	companions    CompanionArtifactLifecycle
	adHocProvider func(string) (MarketplaceProvider, error)
}

type Option func(*Service)

func New(repo domainplugin.Repository, permissions *appaccess.PermissionResolver, audit AuditRecorder) *Service {
	return NewWithOptions(repo, permissions, audit)
}

func NewWithOptions(repo domainplugin.Repository, permissions *appaccess.PermissionResolver, audit AuditRecorder, options ...Option) *Service {
	s := &Service{
		repo:        repo,
		permissions: permissions,
		audit:       audit,
		marketplace: NewCompositeMarketplaceProvider(),
		extensions:  NewExtensionRegistry(),
	}
	s.adHocProvider = func(marketplaceURL string) (MarketplaceProvider, error) {
		return NewAdHocRemoteMarketplaceProvider(MarketplaceSource{ID: "ad-hoc", URL: marketplaceURL})
	}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s
}

func WithMarketplaceProvider(provider MarketplaceProvider) Option {
	return func(s *Service) {
		if provider != nil {
			s.marketplace = provider
		}
	}
}

func WithExtensionRegistry(registry *ExtensionRegistry) Option {
	return func(s *Service) {
		if registry != nil {
			s.extensions = registry
		}
	}
}

func WithCompanionArtifacts(lifecycle CompanionArtifactLifecycle) Option {
	return func(s *Service) {
		s.companions = lifecycle
	}
}

func (s *Service) ListMarketplace(ctx context.Context, principal domainidentity.Principal, filter domainplugin.MarketplaceFilter) ([]domainplugin.MarketplacePlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginView); err != nil {
		return nil, err
	}
	provider, err := s.providerFor(filter.MarketplaceURL)
	if err != nil {
		return nil, err
	}
	installed, err := s.repo.ListInstalled(ctx)
	if err != nil {
		return nil, err
	}
	installedIDs := map[string]bool{}
	for _, item := range installed {
		installedIDs[item.ID] = true
	}
	marketplaceItems, err := provider.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	items := make([]domainplugin.MarketplacePlugin, 0, len(marketplaceItems))
	for _, item := range marketplaceItems {
		item.Installed = installedIDs[item.ID]
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) GetMarketplace(ctx context.Context, principal domainidentity.Principal, ref domainplugin.PluginVersionRef) (domainplugin.MarketplacePlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginView); err != nil {
		return domainplugin.MarketplacePlugin{}, err
	}
	provider, err := s.providerFor(ref.MarketplaceURL)
	if err != nil {
		return domainplugin.MarketplacePlugin{}, err
	}
	item, err := provider.Get(ctx, ref)
	if err != nil {
		return domainplugin.MarketplacePlugin{}, err
	}
	if _, err := s.repo.GetInstalled(ctx, item.ID); err == nil {
		item.Installed = true
	}
	return item, nil
}

func (s *Service) ListInstalled(ctx context.Context, principal domainidentity.Principal) ([]domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginView); err != nil {
		return nil, err
	}
	items, err := s.repo.ListInstalled(ctx)
	if err != nil {
		return nil, err
	}
	for index := range items {
		if err := s.hydrateCompanionArtifacts(ctx, &items[index]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) GetInstalled(ctx context.Context, principal domainidentity.Principal, pluginID string) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginView); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := s.hydrateCompanionArtifacts(ctx, &item); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	return item, nil
}

func (s *Service) GetManifest(ctx context.Context, principal domainidentity.Principal, pluginID string) (domainplugin.PluginManifest, error) {
	item, err := s.GetInstalled(ctx, principal, pluginID)
	if err != nil {
		return domainplugin.PluginManifest{}, err
	}
	return item.Manifest, nil
}

func (s *Service) Install(ctx context.Context, principal domainidentity.Principal, input domainplugin.PluginInstallRequest) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginInstall); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	resolved, err := s.resolveInstallManifest(ctx, input)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	manifest := resolved.Manifest
	if err := validateManifest(manifest); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	checksum, checksumStatus, err := checksumEvidence(manifest, input.ExpectedChecksum, resolved)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	var artifactRecords []domainplugin.PluginArtifactRecord
	activeVersion := ""
	if manifest.Type == "companion-pack" {
		if s.companions == nil {
			return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: companion artifact runtime is unavailable", apperrors.ErrUnsupportedOperation)
		}
		descriptor := input.Package
		if descriptor == nil {
			descriptor = resolved.Package
		}
		if descriptor == nil {
			return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: companion package descriptor is required", apperrors.ErrInvalidArgument)
		}
		if _, err := s.companions.Install(ctx, manifest, *descriptor); err != nil {
			return domainplugin.InstalledPlugin{}, err
		}
		if input.Enable {
			if _, err := s.companions.Activate(ctx, manifest.ID, manifest.Version); err != nil {
				return domainplugin.InstalledPlugin{}, err
			}
		}
		artifactRecords, err = s.companions.Records(ctx, manifest.ID)
		if err != nil {
			return domainplugin.InstalledPlugin{}, err
		}
		activeVersion = activeArtifactVersion(artifactRecords)
	}
	now := time.Now().UTC()
	status := statusDisabled
	var enabledAt *time.Time
	configured := manifestConfigReady(manifest, map[string]string{})
	if !configured {
		status = statusPendingConfig
	} else if input.Enable {
		status = statusEnabled
		enabledAt = &now
	}
	item := domainplugin.InstalledPlugin{
		ID:                   manifest.ID,
		Name:                 manifest.Name,
		Version:              manifest.Version,
		Publisher:            manifest.Publisher,
		Type:                 manifest.Type,
		Status:               status,
		Source:               firstNonEmpty(resolved.Source, input.Source, "direct-manifest"),
		Manifest:             manifest,
		ChecksumStatus:       checksumStatus,
		SignatureStatus:      firstNonEmpty(resolved.SignatureStatus, integrityStatus(manifest)),
		ActiveVersion:        activeVersion,
		Artifacts:            artifactRecords,
		RequestedPermissions: manifest.Permissions,
		ConfiguredSecretRefs: map[string]string{},
		InstalledBy:          firstNonEmpty(principal.UserID, principal.UserName, "system"),
		InstalledAt:          now,
		UpdatedAt:            now,
		EnabledAt:            enabledAt,
		Metadata: map[string]any{
			"manifestChecksum": checksum,
			"permissionModel":  "requested-only",
			"sourceId":         resolved.SourceID,
			"marketplaceUrl":   resolved.MarketplaceURL,
			"configured":       configured,
		},
	}
	item, err = s.repo.UpsertInstalled(ctx, item)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	s.reconcileItem(item)
	s.recordAudit(ctx, principal, "install", item, "installed plugin manifest snapshot")
	return item, nil
}

func (s *Service) Enable(ctx context.Context, principal domainidentity.Principal, pluginID string) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginLifecycle); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	now := time.Now().UTC()
	if manifestConfigReady(item.Manifest, item.ConfiguredSecretRefs) {
		item.Status = statusEnabled
		item.EnabledAt = &now
		item.DisabledAt = nil
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["configured"] = true
		delete(item.Metadata, "reconcileError")
	} else {
		item.Status = statusPendingConfig
		item.EnabledAt = nil
		item.DisabledAt = nil
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["configured"] = false
		item.Metadata["reconcileError"] = "required secret refs are missing"
	}
	item.UpdatedAt = now
	item, err = s.repo.UpsertInstalled(ctx, item)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	s.reconcileItem(item)
	s.recordAudit(ctx, principal, "enable", item, "enabled plugin")
	return item, nil
}

func (s *Service) Disable(ctx context.Context, principal domainidentity.Principal, pluginID string) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginLifecycle); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	now := time.Now().UTC()
	item.Status = statusDisabled
	item.DisabledAt = &now
	item.EnabledAt = nil
	item.UpdatedAt = now
	item, err = s.repo.UpsertInstalled(ctx, item)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	s.extensions.UnregisterPlugin(item.ID)
	s.recordAudit(ctx, principal, "disable", item, "disabled plugin")
	return item, nil
}

func (s *Service) Upgrade(ctx context.Context, principal domainidentity.Principal, pluginID string, input domainplugin.PluginInstallRequest) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginUpgrade); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	current, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if input.PluginID == "" {
		input.PluginID = pluginID
	}
	resolved, err := s.resolveInstallManifest(ctx, input)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	manifest := resolved.Manifest
	if strings.TrimSpace(manifest.ID) != current.ID {
		return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: upgraded manifest id must match installed plugin id", apperrors.ErrInvalidArgument)
	}
	if err := validateManifest(manifest); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if (current.Type == "companion-pack" || manifest.Type == "companion-pack") && current.Type != manifest.Type {
		return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: companion plugin type cannot change during upgrade", apperrors.ErrInvalidArgument)
	}
	checksum, checksumStatus, err := checksumEvidence(manifest, input.ExpectedChecksum, resolved)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if manifest.Type == "companion-pack" {
		if s.companions == nil {
			return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: companion artifact runtime is unavailable", apperrors.ErrUnsupportedOperation)
		}
		descriptor := input.Package
		if descriptor == nil {
			descriptor = resolved.Package
		}
		if descriptor == nil {
			return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: companion package descriptor is required", apperrors.ErrInvalidArgument)
		}
		if _, err := s.companions.Install(ctx, manifest, *descriptor); err != nil {
			return domainplugin.InstalledPlugin{}, err
		}
		if current.Status == statusEnabled {
			if _, err := s.companions.Activate(ctx, manifest.ID, manifest.Version); err != nil {
				return domainplugin.InstalledPlugin{}, err
			}
		}
	}
	current.Name = manifest.Name
	current.Version = manifest.Version
	current.Publisher = manifest.Publisher
	current.Type = manifest.Type
	current.Source = firstNonEmpty(resolved.Source, input.Source, current.Source)
	current.Manifest = manifest
	current.ChecksumStatus = checksumStatus
	current.SignatureStatus = firstNonEmpty(resolved.SignatureStatus, integrityStatus(manifest))
	current.RequestedPermissions = manifest.Permissions
	current.UpdatedAt = time.Now().UTC()
	if current.Metadata == nil {
		current.Metadata = map[string]any{}
	}
	current.Metadata["manifestChecksum"] = checksum
	current.Metadata["permissionModel"] = "requested-only"
	current.Metadata["sourceId"] = resolved.SourceID
	current.Metadata["marketplaceUrl"] = resolved.MarketplaceURL
	current.Metadata["configured"] = manifestConfigReady(manifest, current.ConfiguredSecretRefs)
	if current.Status == statusEnabled && !manifestConfigReady(manifest, current.ConfiguredSecretRefs) {
		current.Status = statusPendingConfig
		current.EnabledAt = nil
		current.Metadata["reconcileError"] = "required secret refs are missing"
	}
	item, err := s.repo.UpsertInstalled(ctx, current)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := s.hydrateCompanionArtifacts(ctx, &item); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	s.reconcileItem(item)
	s.recordAudit(ctx, principal, "upgrade", item, "upgraded plugin manifest snapshot")
	return item, nil
}

func (s *Service) Configure(ctx context.Context, principal domainidentity.Principal, pluginID string, input domainplugin.PluginConfigRequest) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginConfigure); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if len(input.SecretRefs) > 0 {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginConfigureSecrets); err != nil {
			return domainplugin.InstalledPlugin{}, err
		}
	}
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if input.SecretRefs != nil {
		item.ConfiguredSecretRefs = normalizeStringMap(input.SecretRefs)
	}
	if input.Metadata != nil {
		item.Metadata = normalizeMetadata(input.Metadata)
		item.Metadata["permissionModel"] = "requested-only"
	}
	now := time.Now().UTC()
	if input.Enabled != nil {
		if *input.Enabled {
			if manifestConfigReady(item.Manifest, item.ConfiguredSecretRefs) {
				item.Status = statusEnabled
				item.EnabledAt = &now
				item.DisabledAt = nil
				if item.Metadata == nil {
					item.Metadata = map[string]any{}
				}
				item.Metadata["configured"] = true
				delete(item.Metadata, "reconcileError")
			} else {
				item.Status = statusPendingConfig
				item.EnabledAt = nil
				item.DisabledAt = nil
				if item.Metadata == nil {
					item.Metadata = map[string]any{}
				}
				item.Metadata["configured"] = false
				item.Metadata["reconcileError"] = "required secret refs are missing"
			}
		} else {
			item.Status = statusDisabled
			item.DisabledAt = &now
			item.EnabledAt = nil
		}
	}
	if item.Metadata != nil {
		item.Metadata["configured"] = manifestConfigReady(item.Manifest, item.ConfiguredSecretRefs)
	}
	item.UpdatedAt = now
	item, err = s.repo.UpsertInstalled(ctx, item)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	s.reconcileItem(item)
	s.recordAudit(ctx, principal, "configure", item, "configured plugin")
	return item, nil
}

func (s *Service) Remove(ctx context.Context, principal domainidentity.Principal, pluginID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginRemove); err != nil {
		return err
	}
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteInstalled(ctx, pluginID); err != nil {
		return err
	}
	if item.Type == "companion-pack" && s.companions != nil {
		if err := s.companions.Remove(ctx, pluginID); err != nil {
			return err
		}
	}
	s.extensions.UnregisterPlugin(pluginID)
	s.recordAudit(ctx, principal, "remove", item, "removed plugin")
	return nil
}

func (s *Service) ActivateCompanion(ctx context.Context, principal domainidentity.Principal, pluginID, idempotencyKey string, input domainplugin.CompanionActivationRequest) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginLifecycle); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if item.Type != "companion-pack" || s.companions == nil {
		return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: companion artifact runtime is unavailable", apperrors.ErrUnsupportedOperation)
	}
	active, err := s.companions.Activate(ctx, pluginID, input.Version)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	return s.publishActiveCompanion(ctx, principal, item, active, "activate")
}

func (s *Service) RollbackCompanion(ctx context.Context, principal domainidentity.Principal, pluginID, idempotencyKey string, input domainplugin.CompanionRollbackRequest) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginLifecycle); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if strings.TrimSpace(input.Version) == "" {
		return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: rollback version is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if item.Type != "companion-pack" || s.companions == nil {
		return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: companion artifact runtime is unavailable", apperrors.ErrUnsupportedOperation)
	}
	active, err := s.companions.Rollback(ctx, pluginID, input.Version)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	return s.publishActiveCompanion(ctx, principal, item, active, "rollback")
}

func (s *Service) OpenCompanionAsset(ctx context.Context, principal domainidentity.Principal, pluginID, assetPath string) (io.ReadCloser, string, string, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginView); err != nil {
		return nil, "", "", err
	}
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return nil, "", "", err
	}
	if item.Type != "companion-pack" || item.Status != statusEnabled || s.companions == nil {
		return nil, "", "", fmt.Errorf("%w: active companion pack not found", apperrors.ErrNotFound)
	}
	return s.companions.OpenAsset(ctx, pluginID, assetPath)
}

func (s *Service) publishActiveCompanion(ctx context.Context, principal domainidentity.Principal, item domainplugin.InstalledPlugin, active domainplugin.PluginArtifactRecord, action string) (domainplugin.InstalledPlugin, error) {
	pack, err := s.companions.ActivePack(ctx, item.ID)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	now := time.Now().UTC()
	item.Version = active.Version
	item.Manifest.Version = active.Version
	item.Manifest.CompanionPack = &pack
	item.Status = statusEnabled
	item.EnabledAt = &now
	item.DisabledAt = nil
	item.ChecksumStatus = string(active.ChecksumStatus)
	item.SignatureStatus = string(active.SignatureStatus)
	item.UpdatedAt = now
	item, err = s.repo.UpsertInstalled(ctx, item)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := s.hydrateCompanionArtifacts(ctx, &item); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	s.reconcileItem(item)
	summary := "activated companion pack"
	if action == "rollback" {
		summary = "rolled back companion pack"
	}
	s.recordAudit(ctx, principal, action, item, summary)
	return item, nil
}

func (s *Service) hydrateCompanionArtifacts(ctx context.Context, item *domainplugin.InstalledPlugin) error {
	if item == nil || item.Type != "companion-pack" {
		return nil
	}
	if s.companions == nil {
		return fmt.Errorf("%w: companion artifact runtime is unavailable", apperrors.ErrUnsupportedOperation)
	}
	records, err := s.companions.Records(ctx, item.ID)
	if err != nil {
		return err
	}
	item.Artifacts = records
	item.ActiveVersion = activeArtifactVersion(records)
	return nil
}

func activeArtifactVersion(records []domainplugin.PluginArtifactRecord) string {
	for _, record := range records {
		if record.Active {
			return record.Version
		}
	}
	return ""
}

func validateIdempotencyKey(value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 128 {
		return fmt.Errorf("%w: Idempotency-Key must contain 8 to 128 characters", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) ListExtensions(ctx context.Context, principal domainidentity.Principal, scope string) ([]domainplugin.ExtensionRecord, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPlatformExtensionsView); err != nil {
		return nil, err
	}
	return s.extensions.List(scope), nil
}

func (s *Service) Reconcile(ctx context.Context) error {
	items, err := s.repo.ListInstalled(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		s.reconcileItem(item)
	}
	return nil
}

func (s *Service) reconcileItem(item domainplugin.InstalledPlugin) {
	if item.Status != statusEnabled {
		s.extensions.UnregisterPlugin(item.ID)
		return
	}
	s.extensions.RegisterPlugin(item, manifestConfigReady(item.Manifest, item.ConfiguredSecretRefs))
}

func (s *Service) resolveInstallManifest(ctx context.Context, input domainplugin.PluginInstallRequest) (ResolvedManifest, error) {
	if input.Manifest != nil {
		manifest := *input.Manifest
		return ResolvedManifest{
			Manifest:        manifest,
			Integrity:       manifest.Integrity,
			ChecksumStatus:  "not_provided",
			SignatureStatus: integrityStatus(manifest),
			Source:          firstNonEmpty(input.Source, "direct-manifest"),
			SourceID:        input.SourceID,
			MarketplaceURL:  input.MarketplaceURL,
			Package:         input.Package,
		}, nil
	}
	pluginID := strings.TrimSpace(input.PluginID)
	if pluginID == "" {
		return ResolvedManifest{}, fmt.Errorf("%w: pluginId or manifest is required", apperrors.ErrInvalidArgument)
	}
	provider, err := s.providerFor(input.MarketplaceURL)
	if err != nil {
		return ResolvedManifest{}, err
	}
	return provider.FetchManifest(ctx, domainplugin.PluginVersionRef{
		PluginID:       pluginID,
		Version:        input.Version,
		SourceID:       input.SourceID,
		MarketplaceURL: input.MarketplaceURL,
	})
}

func (s *Service) providerFor(marketplaceURL string) (MarketplaceProvider, error) {
	marketplaceURL = strings.TrimSpace(marketplaceURL)
	s.marketplaceMu.RLock()
	marketplace := s.marketplace
	s.marketplaceMu.RUnlock()
	if marketplaceURL == "" {
		return marketplace, nil
	}
	if provider, ok := configuredMarketplaceProviderForURL(marketplace, marketplaceURL); ok {
		return provider, nil
	}
	if s.adHocProvider == nil {
		return nil, fmt.Errorf("%w: ad-hoc marketplace urls are not enabled", apperrors.ErrInvalidArgument)
	}
	return s.adHocProvider(marketplaceURL)
}

// ReconfigureMarketplace atomically publishes a fully constructed provider.
func (s *Service) ReconfigureMarketplace(provider MarketplaceProvider) error {
	if provider == nil {
		return fmt.Errorf("%w: marketplace provider is required", apperrors.ErrInvalidArgument)
	}
	s.marketplaceMu.Lock()
	s.marketplace = provider
	s.marketplaceMu.Unlock()
	return nil
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, action string, item domainplugin.InstalledPlugin, summary string) {
	if s.audit == nil {
		return
	}
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		ResourceKind:  "Plugin",
		ResourceName:  item.Name,
		Action:        "plugin." + action,
		Result:        "success",
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"pluginId":         item.ID,
			"pluginVersion":    item.Version,
			"pluginType":       item.Type,
			"permissionModel":  "requested-only",
			"requestedPerms":   item.RequestedPermissions,
			"checksumStatus":   item.ChecksumStatus,
			"signatureStatus":  item.SignatureStatus,
			"manifestSnapshot": item.Manifest,
		},
	})
}

func validateManifest(manifest domainplugin.PluginManifest) error {
	if strings.TrimSpace(manifest.ID) == "" {
		return fmt.Errorf("%w: plugin id is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("%w: plugin name is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("%w: plugin version is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(manifest.Publisher) == "" {
		return fmt.Errorf("%w: plugin publisher is required", apperrors.ErrInvalidArgument)
	}
	if !slices.Contains(supportedPluginTypes, strings.TrimSpace(manifest.Type)) {
		return fmt.Errorf("%w: unsupported plugin type %q", apperrors.ErrInvalidArgument, manifest.Type)
	}
	if manifest.Type == "companion-pack" {
		if manifest.CompanionPack == nil {
			return fmt.Errorf("%w: companionPack is required for companion-pack plugins", apperrors.ErrInvalidArgument)
		}
		if manifest.Runtime != nil && manifest.Runtime.Mode != "manifest-only" {
			return fmt.Errorf("%w: companion packs must use manifest-only runtime", apperrors.ErrInvalidArgument)
		}
	} else if manifest.CompanionPack != nil {
		return fmt.Errorf("%w: companionPack is only valid for companion-pack plugins", apperrors.ErrInvalidArgument)
	}
	if err := validateCompatibility(manifest.Compatibility); err != nil {
		return err
	}
	if err := validateRuntime(manifest.Runtime); err != nil {
		return err
	}
	if err := validateObservabilityProviders(manifest); err != nil {
		return err
	}
	if err := validateExtensionPointIDs(manifest); err != nil {
		return err
	}
	return nil
}

func manifestChecksum(manifest domainplugin.PluginManifest, expected string) (string, string, error) {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(raw)
	sum := "sha256:" + hex.EncodeToString(hash[:])
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return sum, "not_provided", nil
	}
	if expected != sum {
		return sum, "mismatch", fmt.Errorf("%w: manifest checksum mismatch", apperrors.ErrInvalidArgument)
	}
	return sum, "verified", nil
}

func checksumEvidence(manifest domainplugin.PluginManifest, expected string, resolved ResolvedManifest) (string, string, error) {
	if strings.TrimSpace(expected) != "" {
		return manifestChecksum(manifest, expected)
	}
	sum, status, err := manifestChecksum(manifest, "")
	if err != nil {
		return "", "", err
	}
	if resolved.Integrity != nil && strings.TrimSpace(resolved.Integrity.Checksum) != "" {
		sum = strings.TrimSpace(resolved.Integrity.Checksum)
	}
	if resolved.ChecksumStatus != "" && resolved.ChecksumStatus != "not_provided" {
		status = resolved.ChecksumStatus
	}
	return sum, status, nil
}

func integrityStatus(manifest domainplugin.PluginManifest) string {
	if manifest.Integrity == nil {
		return "not_provided"
	}
	if manifest.Integrity.Verified {
		return "verified"
	}
	return firstNonEmpty(manifest.Integrity.Status, "declared")
}

func validateCompatibility(compatibility *domainplugin.PluginCompatibility) error {
	if compatibility == nil || strings.TrimSpace(compatibility.Soha) == "" {
		return nil
	}
	fields := strings.Fields(compatibility.Soha)
	if len(fields) == 0 {
		fields = []string{compatibility.Soha}
	}
	for _, field := range fields {
		if strings.HasPrefix(field, ">=") {
			minVersion := strings.TrimPrefix(field, ">=")
			if compareSemver(currentSohaVersion, minVersion) < 0 {
				return fmt.Errorf("%w: plugin requires soha %s", apperrors.ErrInvalidArgument, compatibility.Soha)
			}
		}
	}
	return nil
}

func validateRuntime(runtime *domainplugin.PluginRuntimeSpec) error {
	if runtime == nil {
		return nil
	}
	switch runtime.Mode {
	case "", "manifest-only", "external-http", "managed-container":
		return nil
	default:
		return fmt.Errorf("%w: unsupported plugin runtime mode %q", apperrors.ErrInvalidArgument, runtime.Mode)
	}
}

func validateObservabilityProviders(manifest domainplugin.PluginManifest) error {
	if manifest.ExtensionPoints == nil || manifest.ExtensionPoints.Observability == nil || len(manifest.ExtensionPoints.Observability.Providers) == 0 {
		return nil
	}
	if manifest.Runtime == nil || manifest.Runtime.Mode != "external-http" && manifest.Runtime.Mode != "managed-container" {
		return fmt.Errorf("%w: observability providers require an executable runtime", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(manifest.Runtime.Endpoint) == "" || strings.TrimSpace(manifest.Runtime.ActionPath) == "" {
		return fmt.Errorf("%w: observability provider endpoint and actionPath are required", apperrors.ErrInvalidArgument)
	}
	seen := map[string]struct{}{}
	for _, provider := range manifest.ExtensionPoints.Observability.Providers {
		key := strings.ToLower(strings.TrimSpace(provider.ProviderKey))
		if key == "" || strings.TrimSpace(provider.DisplayName) == "" || provider.ProtocolVersion != "v1" || len(provider.Signals) == 0 || len(provider.Capabilities) == 0 {
			return fmt.Errorf("%w: incomplete observability provider contribution", apperrors.ErrInvalidArgument)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate observability provider key %q", apperrors.ErrInvalidArgument, key)
		}
		seen[key] = struct{}{}
		for _, capability := range provider.Capabilities {
			if strings.TrimSpace(provider.ActionRefs[capability]) == "" {
				return fmt.Errorf("%w: observability capability %q requires an actionRef", apperrors.ErrInvalidArgument, capability)
			}
			if capability == "logs.query" && !slices.Contains(provider.Signals, "logs") {
				return fmt.Errorf("%w: logs.query requires the logs signal", apperrors.ErrInvalidArgument)
			}
		}
	}
	return nil
}

func validateExtensionPointIDs(manifest domainplugin.PluginManifest) error {
	item := domainplugin.InstalledPlugin{
		ID:       manifest.ID,
		Name:     manifest.Name,
		Version:  manifest.Version,
		Manifest: manifest,
		Status:   statusEnabled,
	}
	for _, record := range extensionRecordsFromManifest(item, true) {
		if strings.TrimSpace(record.ID) == "" {
			return fmt.Errorf("%w: extension point %s contribution id is required", apperrors.ErrInvalidArgument, record.Point)
		}
	}
	return nil
}

func manifestConfigReady(manifest domainplugin.PluginManifest, secretRefs map[string]string) bool {
	if manifest.Secrets == nil {
		return true
	}
	for _, requirement := range manifest.Secrets.Required {
		if !requirement.Required {
			continue
		}
		name := strings.TrimSpace(requirement.Name)
		if name == "" {
			continue
		}
		if firstNonEmpty(secretRefs[name], requirement.SecretRef) == "" {
			return false
		}
	}
	return true
}

func matchesMarketplaceFilter(item domainplugin.MarketplacePlugin, filter domainplugin.MarketplaceFilter) bool {
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	if query != "" {
		haystack := strings.ToLower(strings.Join([]string{item.ID, item.Name, item.Publisher, item.Type, item.Summary}, " "))
		if !strings.Contains(haystack, query) {
			return false
		}
	}
	if filter.Type != "" && item.Type != filter.Type {
		return false
	}
	if filter.Publisher != "" && item.Publisher != filter.Publisher {
		return false
	}
	if filter.SourceID != "" && item.SourceID != filter.SourceID {
		return false
	}
	if filter.Version != "" && !marketplaceVersionMatches(item, filter.Version) {
		return false
	}
	return true
}

func compareSemver(left, right string) int {
	leftParts := parseSemver(left)
	rightParts := parseSemver(right)
	for i := 0; i < len(leftParts); i++ {
		if leftParts[i] < rightParts[i] {
			return -1
		}
		if leftParts[i] > rightParts[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(value string) [3]int {
	var out [3]int
	value = strings.Trim(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	for i := range out {
		if i >= len(parts) {
			break
		}
		part := parts[i]
		for j, r := range part {
			if r < '0' || r > '9' {
				part = part[:j]
				break
			}
		}
		var parsed int
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			parsed = parsed*10 + int(r-'0')
		}
		switch i {
		case 0:
			out[0] = parsed
		case 1:
			out[1] = parsed
		case 2:
			out[2] = parsed
		}
	}
	return out
}

func normalizeStringMap(values map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func normalizeMetadata(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
