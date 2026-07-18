package agentharness

import (
	"strings"
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

func TestValidateProviderDefinitionRuntimeAndSecretRefs(t *testing.T) {
	validProvider := ProviderDefinition{
		SchemaVersion:   "opensoha.dev/agent-provider-definition/v1",
		ID:              "provider-1",
		Kind:            "agent",
		PluginID:        "agent.provider-1",
		PluginVersion:   "1.0.0",
		ProviderVersion: "1.0.0",
		AdapterProtocol: "opensoha.agent-provider/v1",
		Capabilities:    []string{"run"},
		Runtime:         RuntimeDefinition{Kind: "cli", Command: "provider"},
	}
	tests := []struct {
		name        string
		mutate      func(*ProviderDefinition)
		wantErrText string
	}{
		{name: "cli"},
		{name: "container", mutate: func(provider *ProviderDefinition) {
			provider.Runtime = RuntimeDefinition{Kind: "container", Image: "example/provider:1.0.0"}
		}},
		{name: "remote HTTPS", mutate: func(provider *ProviderDefinition) {
			provider.Runtime = RuntimeDefinition{Kind: "remote", Endpoint: "https://provider.example.test/api"}
		}},
		{name: "remote user info", mutate: func(provider *ProviderDefinition) {
			provider.Runtime = RuntimeDefinition{Kind: "remote", Endpoint: "https://user:pass@provider.example.test"}
		}, wantErrText: "HTTPS URL without user info"},
		{name: "missing container image", mutate: func(provider *ProviderDefinition) {
			provider.Runtime = RuntimeDefinition{Kind: "container"}
		}, wantErrText: "container provider image is required"},
		{name: "unsupported runtime", mutate: func(provider *ProviderDefinition) {
			provider.Runtime = RuntimeDefinition{Kind: "process"}
		}, wantErrText: "unsupported provider runtime"},
		{name: "valid secret ref", mutate: func(provider *ProviderDefinition) {
			provider.SecretRefs = []string{"secret:provider/token"}
		}},
		{name: "inline secret", mutate: func(provider *ProviderDefinition) {
			provider.SecretRefs = []string{"plain-text-token"}
		}, wantErrText: "bounded secret refs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := validProvider
			if test.mutate != nil {
				test.mutate(&provider)
			}
			err := validateProviderDefinition(provider)
			if test.wantErrText == "" {
				if err != nil {
					t.Fatalf("validateProviderDefinition() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErrText) {
				t.Fatalf("validateProviderDefinition() error = %v, want text %q", err, test.wantErrText)
			}
		})
	}
}
