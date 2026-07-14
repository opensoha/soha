package agentharness

import (
	"testing"
	"time"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

type extensionSourceStub struct {
	records []domainplugin.ExtensionRecord
}

func (s extensionSourceStub) List(string) []domainplugin.ExtensionRecord { return s.records }

func TestProviderReconcilerBuildsVersionedCatalogFromEnabledConfiguredExtensions(t *testing.T) {
	source := extensionSourceStub{records: []domainplugin.ExtensionRecord{
		{ID: "hermes", PluginID: "agent.hermes", PluginVersion: "1.2.0", Point: "ai.agentProviders", Scope: "ai", Label: "Hermes", Status: "enabled", Configured: true, RuntimeMode: "cli", Metadata: map[string]any{
			"adapterProtocol": "opensoha.agent-provider.cli/v1", "capabilities": []any{"root_cause"},
			"runtime": map[string]any{"kind": "cli", "command": "hermes", "args": []any{"chat", "-Q"}},
		}},
		{ID: "disabled", PluginID: "agent.disabled", PluginVersion: "1.0.0", Point: "ai.agentProviders", Scope: "ai", Status: "disabled", Configured: true},
	}}
	reconciler := NewProviderReconciler(source)
	catalog, changed, err := reconciler.Reconcile(ProviderCatalog{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || catalog.Revision != 1 || len(catalog.Providers) != 1 || catalog.Providers[0].ID != "hermes" {
		t.Fatalf("catalog = %#v changed=%v", catalog, changed)
	}
	unchanged, changed, err := reconciler.Reconcile(catalog, time.Now())
	if err != nil || changed || unchanged.Revision != catalog.Revision {
		t.Fatalf("unchanged reconcile = %#v changed=%v err=%v", unchanged, changed, err)
	}
}

func TestProviderReconcilerFailsClosedForExecutableWithoutProtocol(t *testing.T) {
	reconciler := NewProviderReconciler(extensionSourceStub{records: []domainplugin.ExtensionRecord{
		{
			ID: "unsafe", PluginID: "unsafe", PluginVersion: "1", Point: "ai.agentProviders", Scope: "ai", Status: "enabled", Configured: true,
			Metadata: map[string]any{"capabilities": []any{"run"}, "runtime": map[string]any{"kind": "cli", "command": "unsafe"}},
		},
	}})
	if _, _, err := reconciler.Reconcile(ProviderCatalog{}, time.Now()); err == nil {
		t.Fatal("Reconcile() accepted provider without adapter protocol")
	}
}

func TestProviderReconcilerRejectsRemoteProviderOverPlainHTTP(t *testing.T) {
	reconciler := NewProviderReconciler(extensionSourceStub{records: []domainplugin.ExtensionRecord{{
		ID: "remote", PluginID: "agent.remote", PluginVersion: "1", Point: "ai.agentProviders", Scope: "ai", Status: "enabled", Configured: true,
		Metadata: map[string]any{"adapterProtocol": "remote/v1", "capabilities": []any{"run"}, "runtime": map[string]any{"kind": "remote", "endpoint": "http://agent.example.test"}},
	}}})
	if _, _, err := reconciler.Reconcile(ProviderCatalog{}, time.Now()); err == nil {
		t.Fatal("Reconcile() accepted insecure remote endpoint")
	}
}
