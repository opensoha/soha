package observability

import (
	"context"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

type providerPluginStore []domainplugin.InstalledPlugin

func (s providerPluginStore) ListInstalled(context.Context) ([]domainplugin.InstalledPlugin, error) {
	return s, nil
}

type recordingExternalLogs struct{ calls int }

func (r *recordingExternalLogs) ValidateConfig(ProviderRuntime, map[string]any) error { return nil }
func (r *recordingExternalLogs) Search(_ context.Context, _ ProviderRuntime, sourceID string, _ map[string]any, _ telemetry.LogSearchQuery) (telemetry.LogSearchResult, error) {
	r.calls++
	return telemetry.LogSearchResult{SourceID: sourceID, Records: []telemetry.LogRecord{{Timestamp: time.Now().UTC(), Message: "external"}}}, nil
}

func TestPluginProviderJoinsCatalogAndExecutesDurableLogs(t *testing.T) {
	plugin := domainplugin.InstalledPlugin{
		ID: "community.logs", Name: "Community Logs", Version: "0.1.0", Status: "enabled",
		Manifest: domainplugin.PluginManifest{
			Runtime: &domainplugin.PluginRuntimeSpec{Mode: sohaapi.ManagedContainer, Endpoint: "http://provider", ActionPath: "/v1/{action}"},
			ExtensionPoints: &domainplugin.PluginExtensionPoints{Observability: &sohaapi.PluginObservabilityExtensions{Providers: []sohaapi.PluginObservabilityProvider{{
				ProviderKey: "community-logs", DisplayName: "Community Logs", ProtocolVersion: "v1",
				Signals: []sohaapi.PluginObservabilityProviderSignals{sohaapi.Logs}, Capabilities: []string{logsQueryCapability},
				ActionRefs: map[string]string{logsQueryCapability: "logs.query"},
			}}}},
		},
	}
	store := &memoryDataSources{items: map[string]domainobservability.DataSource{"source-1": {
		ID: "source-1", Name: "external", SourceKind: dataSourceKindLogs, BackendType: "community-logs", Enabled: true,
		QueryBudget: map[string]any{"timeoutSeconds": 5, "maxRangeSeconds": 3600, "maxEntries": 100},
	}}}
	external := &recordingExternalLogs{}
	service := &Service{
		dataSources: store, permissions: appaccess.NewPermissionResolver(observabilityRoleReader{"reader": {appaccess.PermObserveLogDataSourcesView}}),
		logs: &recordingLogRegistry{}, externalLogs: external, plugins: providerPluginStore{plugin}, now: time.Now,
	}
	input := sohaapi.ObservabilityDataSourceInput{
		Name: "external", BackendType: sohaapi.ObservabilityDataSourceBackendTypeProvider, ProviderKey: "community-logs", Enabled: true,
		Config: sohaapi.ObservabilityLogDataSourceConfig{Endpoint: "http://provider", Configuration: []sohaapi.SystemIntegrationConfigurationField{{Key: "dataset", Value: "apps"}}},
	}
	normalized, err := service.normalizeInput(context.Background(), input, "")
	if err != nil || normalized.BackendType != "community-logs" {
		t.Fatalf("normalizeInput() = %#v, %v", normalized, err)
	}
	input.Config.Configuration[0].Key = "INVALID"
	if _, err := service.normalizeInput(context.Background(), input, ""); err == nil {
		t.Fatal("normalizeInput() should reject invalid provider configuration keys")
	}
	input.Config.Configuration[0] = sohaapi.SystemIntegrationConfigurationField{Key: "dataset", Value: "apps"}
	plugin.Manifest.ExtensionPoints.Observability.Providers[0].Signals = []sohaapi.PluginObservabilityProviderSignals{sohaapi.Metrics}
	service.plugins = providerPluginStore{plugin}
	if _, err := service.normalizeInput(context.Background(), input, ""); err == nil {
		t.Fatal("normalizeInput() should reject a log capability without the logs signal")
	}
	plugin.Manifest.ExtensionPoints.Observability.Providers[0].Signals = []sohaapi.PluginObservabilityProviderSignals{sohaapi.Logs}
	service.plugins = providerPluginStore{plugin}

	providers, err := service.providerCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, provider := range providers {
		found = found || provider.definition.ProviderKey == "community-logs" && !provider.definition.BuiltIn
	}
	if !found {
		t.Fatalf("provider catalog = %#v", providers)
	}
	selector := domainresource.LogSourceSelector{Namespace: "default"}
	page, err := service.QueryDurableLogs(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"reader"}}, "cluster-1", domainresource.LogQuery{Selector: &selector, Limit: 1})
	if err != nil {
		t.Fatalf("QueryDurableLogs() error = %v", err)
	}
	if external.calls != 1 || len(page.Entries) != 1 || page.Entries[0].Message != "external" {
		t.Fatalf("external calls/page = %d %#v", external.calls, page)
	}
}

func TestLegacyElasticsearchProviderAlias(t *testing.T) {
	provider, found, err := (&Service{}).resolveProvider(context.Background(), "es")
	if err != nil || !found || provider.definition.ProviderKey != "elasticsearch" {
		t.Fatalf("resolveProvider(es) = %#v, %t, %v", provider.definition, found, err)
	}
}

func TestProviderCatalogUsesSignalNeutralViewPermission(t *testing.T) {
	service := &Service{permissions: appaccess.NewPermissionResolver(observabilityRoleReader{"observer": {appaccess.PermObserveMonitoringView}})}
	items, err := service.ListProviders(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"observer"}})
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	if len(items) == 0 {
		t.Fatal("ListProviders() returned no built-in providers")
	}
	for _, item := range items {
		if item.Status != sohaapi.ObservabilityProviderStatusSupported {
			t.Fatalf("built-in provider status = %#v", item)
		}
	}
}

func TestProviderCatalogAllowsDataSourceViewPermission(t *testing.T) {
	service := &Service{permissions: appaccess.NewPermissionResolver(observabilityRoleReader{"data-source-reader": {appaccess.PermObserveLogDataSourcesView}})}
	items, err := service.ListProviders(context.Background(), domainidentity.Principal{UserID: "user-1", Roles: []string{"data-source-reader"}})
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	if len(items) == 0 {
		t.Fatal("ListProviders() returned no built-in providers")
	}
}

func TestPluginProviderExecutionStatusIsTruthful(t *testing.T) {
	runtime := sohaapi.PluginRuntimeSpec{Mode: sohaapi.ExternalHTTP, Endpoint: "http://provider", ActionPath: "/v1/{action}"}
	provider := resolvedProvider{
		definition: sohaapi.ObservabilityProviderDefinition{
			Capabilities: []string{logsQueryCapability}, RuntimeMode: sohaapi.ObservabilityProviderRuntimeModeExternalHTTP,
		},
		runtime: &runtime, actionRefs: map[string]string{logsQueryCapability: "logs-query"},
	}
	service := &Service{externalLogs: &recordingExternalLogs{}}
	if status, _ := service.providerExecutionStatus(provider); status != sohaapi.ObservabilityProviderStatusSupported {
		t.Fatalf("status = %s", status)
	}
	provider.definition.Capabilities = append(provider.definition.Capabilities, "traces.query")
	if status, _ := service.providerExecutionStatus(provider); status != sohaapi.ObservabilityProviderStatusDegraded {
		t.Fatalf("status = %s", status)
	}
	provider.definition.Capabilities = []string{"traces.query"}
	if status, _ := service.providerExecutionStatus(provider); status != sohaapi.ObservabilityProviderStatusUnsupported {
		t.Fatalf("status = %s", status)
	}
}
