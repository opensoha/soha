package observability

import (
	"context"
	"sort"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

const logsQueryCapability = "logs.query"

type PluginProviderStore interface {
	ListInstalled(context.Context) ([]domainplugin.InstalledPlugin, error)
}

type resolvedProvider struct {
	definition sohaapi.ObservabilityProviderDefinition
	runtime    *sohaapi.PluginRuntimeSpec
	actionRefs map[string]string
}

type ProviderRuntime struct {
	ProviderKey     string
	ProtocolVersion string
	Runtime         sohaapi.PluginRuntimeSpec
	Action          string
}

func (s *Service) ListProviders(ctx context.Context, principal domainidentity.Principal) ([]sohaapi.ObservabilityProviderDefinition, error) {
	if err := appaccess.AuthorizeAnyRuntimePermission(ctx, s.permissions, principal, appaccess.PermObserveMonitoringView, appaccess.PermObserveLogDataSourcesView); err != nil {
		return nil, err
	}
	providers, err := s.providerCatalog(ctx)
	if err != nil {
		return nil, err
	}
	var dataSources []domainobservability.DataSource
	if s.dataSources != nil {
		dataSources, err = s.dataSources.ListDataSources(ctx)
		if err != nil {
			return nil, err
		}
	}
	items := make([]sohaapi.ObservabilityProviderDefinition, 0, len(providers))
	for _, provider := range providers {
		definition := provider.definition
		definition.Status, definition.StatusReason = s.providerExecutionStatus(provider)
		definition.Configured, definition.RuntimeStatus, definition.RuntimeStatusReason, definition.LastValidatedAt = providerRuntimeStatus(definition, dataSources)
		items = append(items, definition)
	}
	return items, nil
}

func providerRuntimeStatus(provider sohaapi.ObservabilityProviderDefinition, dataSources []domainobservability.DataSource) (bool, sohaapi.ObservabilityProviderRuntimeStatus, string, *time.Time) {
	if provider.Status == sohaapi.ObservabilityProviderStatusUnsupported {
		return false, sohaapi.ObservabilityProviderRuntimeStatusUnsupported, provider.StatusReason, nil
	}
	healthy, failed, unknown := 0, 0, 0
	var lastValidatedAt *time.Time
	for _, item := range dataSources {
		if !item.Enabled || providerKeyForDataSource(item) != provider.ProviderKey {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.ValidationStatus)) {
		case "success":
			healthy++
		case "error":
			failed++
		default:
			unknown++
		}
		if item.LastValidatedAt != nil && (lastValidatedAt == nil || item.LastValidatedAt.After(*lastValidatedAt)) {
			value := item.LastValidatedAt.UTC()
			lastValidatedAt = &value
		}
	}
	configured := healthy+failed+unknown > 0
	switch {
	case !configured:
		return false, sohaapi.ObservabilityProviderRuntimeStatusUnconfigured, "未配置已启用的数据源", nil
	case failed > 0 && healthy > 0:
		return true, sohaapi.ObservabilityProviderRuntimeStatusDegraded, "部分数据源最近校验失败", lastValidatedAt
	case failed > 0:
		return true, sohaapi.ObservabilityProviderRuntimeStatusFailed, "数据源最近校验失败", lastValidatedAt
	case unknown > 0 && healthy > 0:
		return true, sohaapi.ObservabilityProviderRuntimeStatusDegraded, "部分数据源尚未校验", lastValidatedAt
	case unknown > 0:
		return true, sohaapi.ObservabilityProviderRuntimeStatusUnknown, "数据源尚未校验", lastValidatedAt
	default:
		return true, sohaapi.ObservabilityProviderRuntimeStatusHealthy, "", lastValidatedAt
	}
}

func providerKeyForDataSource(item domainobservability.DataSource) string {
	key := strings.ToLower(strings.TrimSpace(item.BackendType))
	if key == "es" {
		return "elasticsearch"
	}
	return key
}

func (s *Service) resolveProvider(ctx context.Context, providerKey string) (resolvedProvider, bool, error) {
	providerKey = strings.ToLower(strings.TrimSpace(providerKey))
	if providerKey == "es" {
		providerKey = "elasticsearch"
	}
	providers, err := s.providerCatalog(ctx)
	if err != nil {
		return resolvedProvider{}, false, err
	}
	for _, provider := range providers {
		if provider.definition.ProviderKey == providerKey {
			return provider, true, nil
		}
	}
	return resolvedProvider{}, false, nil
}

func (s *Service) providerCatalog(ctx context.Context) ([]resolvedProvider, error) {
	providers := builtInProviders()
	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		seen[provider.definition.ProviderKey] = struct{}{}
	}
	if s.plugins == nil {
		return providers, nil
	}
	installed, err := s.plugins.ListInstalled(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(installed, func(i, j int) bool { return installed[i].ID < installed[j].ID })
	for _, plugin := range installed {
		if plugin.Status != "enabled" || plugin.Manifest.ExtensionPoints == nil || plugin.Manifest.ExtensionPoints.Observability == nil || plugin.Manifest.Runtime == nil {
			continue
		}
		runtimeMode, ok := providerRuntimeMode(plugin.Manifest.Runtime.Mode)
		if !ok {
			continue
		}
		for _, contribution := range plugin.Manifest.ExtensionPoints.Observability.Providers {
			key := strings.ToLower(strings.TrimSpace(contribution.ProviderKey))
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			signals := make([]sohaapi.ObservabilityProviderSignal, 0, len(contribution.Signals))
			for _, signal := range contribution.Signals {
				signals = append(signals, sohaapi.ObservabilityProviderSignal(signal))
			}
			providers = append(providers, resolvedProvider{
				definition: sohaapi.ObservabilityProviderDefinition{
					ProviderKey: key, DisplayName: contribution.DisplayName, Description: contribution.Description,
					ProtocolVersion: contribution.ProtocolVersion, Signals: signals, Capabilities: append([]string(nil), contribution.Capabilities...),
					RuntimeMode: runtimeMode, BuiltIn: false, PluginID: plugin.ID, ConfigSchemaRef: contribution.ConfigSchemaRef,
				},
				runtime: plugin.Manifest.Runtime, actionRefs: cloneStrings(contribution.ActionRefs),
			})
			seen[key] = struct{}{}
		}
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].definition.ProviderKey < providers[j].definition.ProviderKey })
	return providers, nil
}

func builtInProviders() []resolvedProvider {
	definition := func(key, name string, signals []sohaapi.ObservabilityProviderSignal, capabilities ...string) resolvedProvider {
		return resolvedProvider{definition: sohaapi.ObservabilityProviderDefinition{
			ProviderKey: key, DisplayName: name, ProtocolVersion: "v1", Signals: signals, Capabilities: capabilities,
			RuntimeMode: sohaapi.ObservabilityProviderRuntimeModeBuiltin, BuiltIn: true,
			Status: sohaapi.ObservabilityProviderStatusSupported,
		}}
	}
	return []resolvedProvider{
		definition("clickhouse", "ClickHouse", []sohaapi.ObservabilityProviderSignal{sohaapi.ObservabilityProviderSignalLogs}, logsQueryCapability, "logs.correlate"),
		definition("elasticsearch", "Elasticsearch", []sohaapi.ObservabilityProviderSignal{sohaapi.ObservabilityProviderSignalLogs}, logsQueryCapability, "logs.correlate"),
		definition("jaeger", "Jaeger", []sohaapi.ObservabilityProviderSignal{sohaapi.ObservabilityProviderSignalTraces}, "traces.query"),
		definition("loki", "Grafana Loki", []sohaapi.ObservabilityProviderSignal{sohaapi.ObservabilityProviderSignalLogs}, logsQueryCapability, "logs.correlate"),
		definition("prometheus", "Prometheus", []sohaapi.ObservabilityProviderSignal{sohaapi.ObservabilityProviderSignalMetrics}, "metrics.range-query"),
		definition("skywalking", "Apache SkyWalking", []sohaapi.ObservabilityProviderSignal{sohaapi.ObservabilityProviderSignalTraces}, "traces.query"),
	}
}

func (s *Service) providerExecutionStatus(provider resolvedProvider) (sohaapi.ObservabilityProviderStatus, string) {
	if provider.definition.BuiltIn {
		return sohaapi.ObservabilityProviderStatusSupported, ""
	}
	executable := 0
	for _, capability := range provider.definition.Capabilities {
		if capability == logsQueryCapability && s.externalLogs != nil {
			if _, ok := provider.runtimeFor(capability); ok {
				executable++
			}
		}
	}
	switch {
	case executable == 0:
		return sohaapi.ObservabilityProviderStatusUnsupported, "当前 Core 未注册该 Provider 的可执行能力"
	case executable < len(provider.definition.Capabilities):
		return sohaapi.ObservabilityProviderStatusDegraded, "部分声明能力尚无可执行适配器"
	default:
		return sohaapi.ObservabilityProviderStatusSupported, ""
	}
}

func providerRuntimeMode(mode sohaapi.PluginRuntimeSpecMode) (sohaapi.ObservabilityProviderRuntimeMode, bool) {
	switch mode {
	case sohaapi.ExternalHTTP:
		return sohaapi.ObservabilityProviderRuntimeModeExternalHTTP, true
	case sohaapi.ManagedContainer:
		return sohaapi.ObservabilityProviderRuntimeModeManagedContainer, true
	default:
		return "", false
	}
}

func hasCapability(provider resolvedProvider, capability string) bool {
	for _, item := range provider.definition.Capabilities {
		if item == capability {
			return true
		}
	}
	return false
}

func hasSignal(provider resolvedProvider, signal sohaapi.ObservabilityProviderSignal) bool {
	for _, item := range provider.definition.Signals {
		if item == signal {
			return true
		}
	}
	return false
}

func supportsLogQuery(provider resolvedProvider) bool {
	return hasSignal(provider, sohaapi.ObservabilityProviderSignalLogs) && hasCapability(provider, logsQueryCapability)
}

func (provider resolvedProvider) runtimeFor(capability string) (ProviderRuntime, bool) {
	if provider.runtime == nil {
		return ProviderRuntime{}, false
	}
	action := strings.TrimSpace(provider.actionRefs[capability])
	if action == "" {
		return ProviderRuntime{}, false
	}
	return ProviderRuntime{
		ProviderKey: provider.definition.ProviderKey, ProtocolVersion: provider.definition.ProtocolVersion,
		Runtime: *provider.runtime, Action: action,
	}, true
}

func cloneStrings(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
