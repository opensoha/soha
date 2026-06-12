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
	statusEnabled  = "enabled"
	statusDisabled = "disabled"
)

var supportedPluginTypes = []string{
	"skill",
	"skill-pack",
	"mcp-preset",
	"connector",
	"ai-provider-adapter",
	"agent-profile",
	"gateway-policy-pack",
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Service struct {
	repo        domainplugin.Repository
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	marketplace []domainplugin.MarketplacePlugin
}

func New(repo domainplugin.Repository, permissions *appaccess.PermissionResolver, audit AuditRecorder) *Service {
	return &Service{
		repo:        repo,
		permissions: permissions,
		audit:       audit,
		marketplace: defaultMarketplace(),
	}
}

func (s *Service) ListMarketplace(ctx context.Context, principal domainidentity.Principal, filter domainplugin.MarketplaceFilter) ([]domainplugin.MarketplacePlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginView); err != nil {
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
	items := make([]domainplugin.MarketplacePlugin, 0, len(s.marketplace))
	for _, item := range s.marketplace {
		if !matchesMarketplaceFilter(item, filter) {
			continue
		}
		item.Installed = installedIDs[item.ID]
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) GetMarketplace(ctx context.Context, principal domainidentity.Principal, pluginID string) (domainplugin.MarketplacePlugin, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermPluginView); err != nil {
		return domainplugin.MarketplacePlugin{}, err
	}
	item, ok := s.marketplaceByID(pluginID)
	if !ok {
		return domainplugin.MarketplacePlugin{}, fmt.Errorf("%w: plugin not found", apperrors.ErrNotFound)
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
	manifest, source, err := s.resolveInstallManifest(input)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := validateManifest(manifest); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	checksum, checksumStatus, err := manifestChecksum(manifest, input.ExpectedChecksum)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	now := time.Now().UTC()
	status := statusDisabled
	var enabledAt *time.Time
	if input.Enable {
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
		Source:               firstNonEmpty(source, input.Source, "direct-manifest"),
		Manifest:             manifest,
		ChecksumStatus:       checksumStatus,
		SignatureStatus:      integrityStatus(manifest),
		RequestedPermissions: manifest.Permissions,
		ConfiguredSecretRefs: map[string]string{},
		InstalledBy:          firstNonEmpty(principal.UserID, principal.UserName, "system"),
		InstalledAt:          now,
		UpdatedAt:            now,
		EnabledAt:            enabledAt,
		Metadata: map[string]any{
			"manifestChecksum": checksum,
			"permissionModel":  "requested-only",
		},
	}
	item, err = s.repo.UpsertInstalled(ctx, item)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
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
	item.Status = statusEnabled
	item.EnabledAt = &now
	item.DisabledAt = nil
	item.UpdatedAt = now
	item, err = s.repo.UpsertInstalled(ctx, item)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
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
	manifest, source, err := s.resolveInstallManifest(input)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if strings.TrimSpace(manifest.ID) != current.ID {
		return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: upgraded manifest id must match installed plugin id", apperrors.ErrInvalidArgument)
	}
	if err := validateManifest(manifest); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	checksum, checksumStatus, err := manifestChecksum(manifest, input.ExpectedChecksum)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	current.Name = manifest.Name
	current.Version = manifest.Version
	current.Publisher = manifest.Publisher
	current.Type = manifest.Type
	current.Source = firstNonEmpty(source, input.Source, current.Source)
	current.Manifest = manifest
	current.ChecksumStatus = checksumStatus
	current.SignatureStatus = integrityStatus(manifest)
	current.RequestedPermissions = manifest.Permissions
	current.UpdatedAt = time.Now().UTC()
	if current.Metadata == nil {
		current.Metadata = map[string]any{}
	}
	current.Metadata["manifestChecksum"] = checksum
	current.Metadata["permissionModel"] = "requested-only"
	item, err := s.repo.UpsertInstalled(ctx, current)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
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
			item.Status = statusEnabled
			item.EnabledAt = &now
			item.DisabledAt = nil
		} else {
			item.Status = statusDisabled
			item.DisabledAt = &now
			item.EnabledAt = nil
		}
	}
	item.UpdatedAt = now
	item, err = s.repo.UpsertInstalled(ctx, item)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
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
	s.recordAudit(ctx, principal, "remove", item, "removed plugin")
	return nil
}

func (s *Service) resolveInstallManifest(input domainplugin.PluginInstallRequest) (domainplugin.PluginManifest, string, error) {
	if input.Manifest != nil {
		return *input.Manifest, firstNonEmpty(input.Source, "direct-manifest"), nil
	}
	pluginID := strings.TrimSpace(input.PluginID)
	if pluginID == "" {
		return domainplugin.PluginManifest{}, "", fmt.Errorf("%w: pluginId or manifest is required", apperrors.ErrInvalidArgument)
	}
	item, ok := s.marketplaceByID(pluginID)
	if !ok {
		return domainplugin.PluginManifest{}, "", fmt.Errorf("%w: marketplace plugin not found", apperrors.ErrNotFound)
	}
	return item.Manifest, item.Source, nil
}

func (s *Service) marketplaceByID(pluginID string) (domainplugin.MarketplacePlugin, bool) {
	pluginID = strings.TrimSpace(pluginID)
	for _, item := range s.marketplace {
		if item.ID == pluginID {
			return item, true
		}
	}
	return domainplugin.MarketplacePlugin{}, false
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

func integrityStatus(manifest domainplugin.PluginManifest) string {
	if manifest.Integrity == nil {
		return "not_provided"
	}
	if manifest.Integrity.Verified {
		return "verified"
	}
	return firstNonEmpty(manifest.Integrity.Status, "declared")
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
	return true
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

func defaultMarketplace() []domainplugin.MarketplacePlugin {
	k8sManifest := domainplugin.PluginManifest{
		ID:          "opensoha.k8s-sre-pack",
		Name:        "K8s SRE Pack",
		Version:     "0.1.0",
		Publisher:   "opensoha",
		Type:        "skill-pack",
		Description: "Read-only Kubernetes SRE skills and MCP preset references for AI Gateway workflows.",
		Assets: &domainplugin.PluginAssetSnapshot{
			Skills:     []string{"skills/ai-gateway/k8s-sre/SKILL.md"},
			MCPPresets: []string{"mcp-presets/k8s-readonly.yaml"},
		},
		Capabilities: &domainplugin.PluginCapabilityRequest{
			Tools: []string{"k8s.pods.list", "k8s.pods.logs", "k8s.events.list"},
		},
		Permissions: &domainplugin.PluginPermissionRequest{
			Required: []string{appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke},
			Domain:   []string{appaccess.PermWorkspaceResourceView},
		},
		Integrity: &domainplugin.PluginIntegrity{Status: "catalog"},
	}
	feishuManifest := domainplugin.PluginManifest{
		ID:          "opensoha.feishu",
		Name:        "Feishu Connector",
		Version:     "0.1.0",
		Publisher:   "opensoha",
		Type:        "connector",
		Description: "Feishu connector runtime capability bundle for AI Gateway actions.",
		Assets: &domainplugin.PluginAssetSnapshot{
			Connectors: []string{"connectors/feishu/connector.manifest.json"},
		},
		Capabilities: &domainplugin.PluginCapabilityRequest{
			Tools: []string{"feishu.message.send_text"},
		},
		Permissions: &domainplugin.PluginPermissionRequest{
			Required: []string{appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke},
			Domain:   []string{"connector"},
		},
		Integrity: &domainplugin.PluginIntegrity{Status: "catalog"},
	}
	return []domainplugin.MarketplacePlugin{
		{
			ID:        k8sManifest.ID,
			Name:      k8sManifest.Name,
			Version:   k8sManifest.Version,
			Publisher: k8sManifest.Publisher,
			Type:      k8sManifest.Type,
			Summary:   k8sManifest.Description,
			Source:    "marketplace:opensoha/k8s-sre-pack",
			RiskLevel: "read",
			Manifest:  k8sManifest,
		},
		{
			ID:        feishuManifest.ID,
			Name:      feishuManifest.Name,
			Version:   feishuManifest.Version,
			Publisher: feishuManifest.Publisher,
			Type:      feishuManifest.Type,
			Summary:   feishuManifest.Description,
			Source:    "marketplace:opensoha/feishu",
			RiskLevel: "mutate",
			Manifest:  feishuManifest,
		},
	}
}
