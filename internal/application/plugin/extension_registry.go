package plugin

import (
	"slices"
	"sync"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

type ExtensionRegistry struct {
	mu      sync.RWMutex
	records map[string][]domainplugin.ExtensionRecord
}

func NewExtensionRegistry() *ExtensionRegistry {
	return &ExtensionRegistry{records: map[string][]domainplugin.ExtensionRecord{}}
}

func (r *ExtensionRegistry) RegisterPlugin(item domainplugin.InstalledPlugin, configured bool) {
	if r == nil {
		return
	}
	records := extensionRecordsFromManifest(item, configured)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(records) == 0 {
		delete(r.records, item.ID)
		return
	}
	r.records[item.ID] = records
}

func (r *ExtensionRegistry) UnregisterPlugin(pluginID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, pluginID)
}

func (r *ExtensionRegistry) List(scope string) []domainplugin.ExtensionRecord {
	if r == nil {
		return []domainplugin.ExtensionRecord{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []domainplugin.ExtensionRecord{}
	for _, records := range r.records {
		for _, record := range records {
			if scope == "" || record.Scope == scope || scope == "runtime" {
				out = append(out, record)
			}
		}
	}
	slices.SortFunc(out, func(a, b domainplugin.ExtensionRecord) int {
		if a.Scope != b.Scope {
			if a.Scope < b.Scope {
				return -1
			}
			return 1
		}
		if a.Point != b.Point {
			if a.Point < b.Point {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out
}

func extensionRecordsFromManifest(item domainplugin.InstalledPlugin, configured bool) []domainplugin.ExtensionRecord {
	points := item.Manifest.ExtensionPoints
	if points == nil {
		return []domainplugin.ExtensionRecord{}
	}
	runtimeMode := "manifest-only"
	if item.Manifest.Runtime != nil && item.Manifest.Runtime.Mode != "" {
		runtimeMode = string(item.Manifest.Runtime.Mode)
	}
	records := []domainplugin.ExtensionRecord{}
	add := func(scope, point string, contributions []sohaapi.PluginExtensionContribution) {
		for _, contribution := range contributions {
			records = append(records, extensionRecord(item, configured, runtimeMode, scope, point, contribution))
		}
	}
	if points.Auth != nil {
		add("auth", "auth.sources", points.Auth.Sources)
		add("auth", "auth.profileMappers", points.Auth.ProfileMappers)
		add("auth", "auth.directorySync", points.Auth.DirectorySync)
	}
	if points.Identity != nil {
		add("identity", "identity.applicationTemplates", points.Identity.ApplicationTemplates)
		add("identity", "identity.providerTemplates", points.Identity.ProviderTemplates)
	}
	if points.UI != nil {
		add("ui", "ui.menus", points.UI.Menus)
		add("ui", "ui.settingsForms", points.UI.SettingsForms)
		add("ui", "ui.statusCards", points.UI.StatusCards)
		add("ui", "ui.detailPanels", points.UI.DetailPanels)
		add("ui", "ui.actionButtons", points.UI.ActionButtons)
	}
	if points.AI != nil {
		add("ai", "ai.agentProviders", points.AI.AgentProviders)
		add("ai", "ai.modelProviders", points.AI.ModelProviders)
		add("ai", "ai.toolProviders", points.AI.ToolProviders)
		add("ai", "ai.skillPacks", points.AI.SkillPacks)
		add("ai", "ai.mcpPresets", points.AI.MCPPresets)
		add("ai", "ai.analysisWorkflows", points.AI.AnalysisWorkflows)
		add("ai", "ai.artifactRenderers", points.AI.ArtifactRenderers)
	}
	if points.Resource != nil {
		add("resource", "resource.tags", points.Resource.Tags)
		add("resource", "resource.actions", points.Resource.Actions)
		add("resource", "resource.tabs", points.Resource.Tabs)
		add("resource", "resource.diagnostics", points.Resource.Diagnostics)
	}
	if points.Metrics != nil {
		add("metrics", "metrics.providers", points.Metrics.Providers)
		add("metrics", "metrics.definitions", points.Metrics.Definitions)
		add("metrics", "metrics.panels", points.Metrics.Panels)
		add("metrics", "metrics.enrichers", points.Metrics.Enrichers)
	}
	if points.Alerts != nil {
		add("alerts", "alert.notificationChannels", points.Alerts.NotificationChannels)
		add("alerts", "alert.receivers", points.Alerts.Receivers)
		add("alerts", "alert.enrichers", points.Alerts.Enrichers)
		add("alerts", "alert.escalationProviders", points.Alerts.EscalationProviders)
		add("alerts", "alert.silenceAdapters", points.Alerts.SilenceAdapters)
	}
	if points.Delivery != nil {
		add("delivery", "delivery.buildProviders", points.Delivery.BuildProviders)
		add("delivery", "delivery.scanProviders", points.Delivery.ScanProviders)
		add("delivery", "delivery.releaseGates", points.Delivery.ReleaseGates)
		add("delivery", "delivery.deployStrategies", points.Delivery.DeployStrategies)
		add("delivery", "delivery.artifactStores", points.Delivery.ArtifactStores)
	}
	if points.Gateway != nil {
		add("gateway", "gateway.tools", points.Gateway.Tools)
		add("gateway", "gateway.resources", points.Gateway.Resources)
		add("gateway", "gateway.prompts", points.Gateway.Prompts)
		add("gateway", "gateway.policies", points.Gateway.Policies)
	}
	return records
}

func extensionRecord(
	item domainplugin.InstalledPlugin,
	configured bool,
	runtimeMode string,
	scope string,
	point string,
	contribution sohaapi.PluginExtensionContribution,
) domainplugin.ExtensionRecord {
	return domainplugin.ExtensionRecord{
		ID:             contribution.ID,
		PluginID:       item.ID,
		PluginName:     item.Name,
		PluginVersion:  item.Version,
		Point:          point,
		Scope:          scope,
		Label:          contribution.Label,
		Description:    contribution.Description,
		ActionRef:      contribution.ActionRef,
		ResourceKinds:  append([]string(nil), contribution.ResourceKinds...),
		PermissionKeys: append([]string(nil), contribution.PermissionKeys...),
		RuntimeMode:    runtimeMode,
		Status:         item.Status,
		Configured:     configured,
		Metadata:       contribution.Metadata,
	}
}
