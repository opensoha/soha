package plugin

import (
	"context"
	"encoding/json"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"testing"
)

func TestAgentProviderEndpointConfigurationGatesRegistration(t *testing.T) {
	var manifest domainplugin.PluginManifest
	if err := json.Unmarshal([]byte(`{"id":"plugin","name":"Agent","version":"1","publisher":"test","type":"ai-provider-adapter","extensionPoints":{"ai":{"agentProviders":[{"id":"agent","metadata":{"runtime":{"kind":"remote","endpointConfigKey":"endpoint"}}}]}}}`), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"", "http://example.test", "https://user:pass@example.test", "https://example.test?token=secret"} {
		if manifestConfigReady(manifest, nil, map[string]any{"endpoint": endpoint}) {
			t.Errorf("unsafe/missing endpoint accepted: %q", endpoint)
		}
	}
	configuration := map[string]any{"endpoint": "https://hermes.example.test", "other": "do-not-publish"}
	if !manifestConfigReady(manifest, nil, configuration) {
		t.Fatal("valid endpoint is not configurable")
	}
	records := extensionRecordsFromManifest(domainplugin.InstalledPlugin{ID: "plugin", Manifest: manifest, Metadata: configuration, Status: "enabled"}, true)
	runtime, _ := records[0].Metadata["runtime"].(map[string]any)
	if runtime["endpoint"] != "https://hermes.example.test" {
		t.Fatal("configured endpoint was not projected")
	}
	if _, ok := records[0].Metadata["other"]; ok {
		t.Fatal("unrelated configuration leaked")
	}
	original, _ := manifest.ExtensionPoints.AI.AgentProviders[0].Metadata["runtime"].(map[string]any)
	if _, ok := original["endpoint"]; ok {
		t.Fatal("manifest was mutated")
	}
}

func TestAgentPluginInstallConfigureAndDisable(t *testing.T) {
	service := New(newMemoryPluginRepo(), appaccess.NewPermissionResolver(stubRolePermissions{
		"manager": {appaccess.PermPluginInstall, appaccess.PermPluginView, appaccess.PermPluginManage, appaccess.PermPlatformExtensionsView},
	}), nil)
	principal := domainidentity.Principal{Roles: []string{"manager"}}
	var manifest domainplugin.PluginManifest
	if err := json.Unmarshal([]byte(`{"id":"opensoha.hermes-api","name":"Hermes Chat","version":"0.1.0","publisher":"opensoha","type":"ai-provider-adapter","runtime":{"mode":"manifest-only"},"extensionPoints":{"ai":{"agentProviders":[{"id":"hermes-api","metadata":{"runtime":{"kind":"remote","endpointConfigKey":"endpoint"}}}]}}}`), &manifest); err != nil {
		t.Fatal(err)
	}
	item, err := service.Install(context.Background(), principal, domainplugin.PluginInstallRequest{Manifest: &manifest, Enable: true})
	if err != nil || item.Status != statusPendingConfig {
		t.Fatalf("install = %s, %v", item.Status, err)
	}
	for _, endpoint := range []string{"https://hermes.example.test", ""} {
		item, err = service.Configure(context.Background(), principal, manifest.ID, domainplugin.PluginConfigRequest{Enabled: boolPtr(true), Metadata: map[string]any{"endpoint": endpoint}})
		if err != nil {
			t.Fatal(err)
		}
		extensions, err := service.ListExtensions(context.Background(), principal, "ai")
		if err != nil {
			t.Fatal(err)
		}
		if endpoint != "" {
			if item.Status != statusEnabled || len(extensions) != 1 {
				t.Fatalf("configured plugin = %s, %v", item.Status, extensions)
			}
		} else if item.Status != statusPendingConfig || len(extensions) != 0 {
			t.Fatalf("unconfigured plugin stayed active = %s, %v", item.Status, extensions)
		}
	}
}
