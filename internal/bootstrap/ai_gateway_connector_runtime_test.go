package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

type connectorRuntimeRoleReader struct {
	matrix map[string][]string
}

func (r connectorRuntimeRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.matrix, nil
}

func TestRegisterAIGatewayConnectorRuntimesAppendsConfiguredProvider(t *testing.T) {
	const runtimeToken = "runtime-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manifest" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
			t.Fatalf("manifest request missing bearer token: %q", r.Header.Get("Authorization"))
		}
		writeBootstrapJSON(t, w, map[string]any{
			"id": "feishu",
			"actions": []map[string]any{
				{
					"name":        "feishu.message.send_text",
					"description": "Send a text message.",
					"inputSchema": map[string]any{"type": "object"},
				},
			},
		})
	}))
	defer server.Close()

	service := newBootstrapGatewayService(appaccess.NewPermissionResolver(connectorRuntimeRoleReader{
		matrix: map[string][]string{
			"developer": {
				appaccess.PermAIGatewayView,
				appaccess.PermAIGatewayInvoke,
				appaccess.PermDeliveryApplicationsView,
			},
		},
	}))
	err := registerAIGatewayConnectorRuntimes(context.Background(), service, cfgpkg.AIGatewayConfig{
		ConnectorRuntime: cfgpkg.AIGatewayConnectorRuntimeConfig{
			Endpoint: server.URL,
			Token:    runtimeToken,
			PluginID: "opensoha.feishu",
		},
	})
	if err != nil {
		t.Fatalf("registerAIGatewayConnectorRuntimes returned error: %v", err)
	}

	manifest, err := service.Capabilities(context.Background(), connectorRuntimePrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if !bootstrapHasTool(manifest.Tools, "feishu.message.send_text") {
		t.Fatalf("expected connector runtime tool in manifest, got %#v", manifest.Tools)
	}
	if !bootstrapHasTool(manifest.Tools, "delivery.applications.list") {
		t.Fatalf("expected default AI Gateway tools to remain, got %#v", manifest.Tools)
	}
}

func TestRegisterAIGatewayConnectorRuntimesReturnsDiscoveryError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "runtime unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	service := newBootstrapGatewayService(appaccess.NewPermissionResolver(connectorRuntimeRoleReader{}))
	err := registerAIGatewayConnectorRuntimes(context.Background(), service, cfgpkg.AIGatewayConfig{
		ConnectorRuntimes: []cfgpkg.AIGatewayConnectorRuntimeConfig{
			{Endpoint: server.URL},
		},
	})
	if err == nil {
		t.Fatal("registerAIGatewayConnectorRuntimes error = nil, want discovery error")
	}
}

func newBootstrapGatewayService(permissions *appaccess.PermissionResolver) *appaigateway.Service {
	return appaigateway.NewWithDeps(appaigateway.ServiceDeps{
		Permissions: permissions,
	})
}

func connectorRuntimePrincipal(role string) domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "user-1",
		UserName: "User One",
		Roles:    []string{role},
	}
}

func bootstrapHasTool(tools []domainaigateway.ToolCapability, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func writeBootstrapJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}
