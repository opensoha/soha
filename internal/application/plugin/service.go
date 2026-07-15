package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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
	"notification-channel",
	"identity-template",
	"ui-extension",
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Service struct {
	repo          domainplugin.Repository
	permissions   *appaccess.PermissionResolver
	audit         AuditRecorder
	marketplace   MarketplaceProvider
	extensions    *ExtensionRegistry
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
	return s.repo.ListInstalled(ctx)
}

func (s *Service) GetInstalled(ctx context.Context, principal domainidentity.Principal, pluginID string) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginView); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	return s.repo.GetInstalled(ctx, pluginID)
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
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginManage); err != nil {
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
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginManage); err != nil {
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
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginManage); err != nil {
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
	checksum, checksumStatus, err := checksumEvidence(manifest, input.ExpectedChecksum, resolved)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
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
	s.reconcileItem(item)
	s.recordAudit(ctx, principal, "upgrade", item, "upgraded plugin manifest snapshot")
	return item, nil
}

func (s *Service) Configure(ctx context.Context, principal domainidentity.Principal, pluginID string, input domainplugin.PluginConfigRequest) (domainplugin.InstalledPlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginManage); err != nil {
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
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginManage); err != nil {
		return err
	}
	item, err := s.repo.GetInstalled(ctx, pluginID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteInstalled(ctx, pluginID); err != nil {
		return err
	}
	s.extensions.UnregisterPlugin(pluginID)
	s.recordAudit(ctx, principal, "remove", item, "removed plugin")
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
	if marketplaceURL == "" {
		return s.marketplace, nil
	}
	if provider, ok := configuredMarketplaceProviderForURL(s.marketplace, marketplaceURL); ok {
		return provider, nil
	}
	if s.adHocProvider == nil {
		return nil, fmt.Errorf("%w: ad-hoc marketplace urls are not enabled", apperrors.ErrInvalidArgument)
	}
	return s.adHocProvider(marketplaceURL)
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
	if err := validateCompatibility(manifest.Compatibility); err != nil {
		return err
	}
	if err := validateRuntime(manifest.Runtime); err != nil {
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
