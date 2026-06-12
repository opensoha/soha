package aigateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
)

func TestConnectorRuntimeProviderDiscoversManifestAndInvokesThroughGateway(t *testing.T) {
	const runtimeToken = "runtime-secret-token"
	var invoked bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest":
			if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
				t.Fatalf("manifest request missing bearer token: %q", r.Header.Get("Authorization"))
			}
			writeJSON(t, w, map[string]any{
				"id":          "feishu",
				"name":        "Feishu Connector",
				"description": "Feishu connector runtime.",
				"actions": []map[string]any{
					{
						"name":        "feishu.message.send_text",
						"description": "Send a text message.",
						"inputSchema": map[string]any{
							"type":     "object",
							"required": []string{"receiveIdType", "receiveId", "text"},
						},
					},
				},
			})
		case "/actions/feishu.message.send_text":
			if r.Method != http.MethodPost {
				t.Fatalf("action method = %s, want POST", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
				t.Fatalf("action request missing bearer token: %q", r.Header.Get("Authorization"))
			}
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatalf("decode action input: %v", err)
			}
			if input["text"] != "hello" || input["receiveId"] != "chat-1" {
				t.Fatalf("unexpected action input: %#v", input)
			}
			invoked = true
			writeJSON(t, w, map[string]any{
				"ok": true,
				"output": map[string]any{
					"messageId": "msg-1",
					"status":    "sent",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := DiscoverConnectorRuntime(
		context.Background(),
		server.URL,
		server.Client(),
		WithConnectorRuntimeToken(runtimeToken),
		WithConnectorRuntimePluginID("opensoha.feishu"),
	)
	if err != nil {
		t.Fatalf("DiscoverConnectorRuntime returned error: %v", err)
	}

	repo := &memoryGatewayRepository{}
	audit := &captureAuditRecorder{}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke},
		},
	}), audit, repo)
	service.SetCapabilityProviders(provider)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	if len(manifest.Tools) != 1 || manifest.Tools[0].Name != "feishu.message.send_text" {
		t.Fatalf("expected connector tool in manifest, got %#v", manifest.Tools)
	}
	if manifest.Tools[0].RiskLevel != domainaigateway.RiskLevelMutate || manifest.Tools[0].PermissionKeys[0] != appaccess.PermAIGatewayInvoke {
		t.Fatalf("unexpected connector tool policy metadata: %#v", manifest.Tools[0])
	}

	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "feishu.message.send_text",
		Input: map[string]any{
			"receiveIdType": "chat_id",
			"receiveId":     "chat-1",
			"text":          "hello",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if !invoked {
		t.Fatalf("expected connector runtime action to be invoked")
	}
	output := result.Output.(map[string]any)
	if output["messageId"] != "msg-1" || output["status"] != "sent" {
		t.Fatalf("unexpected connector output: %#v", output)
	}
	if result.RelatedIDs["pluginId"] != "opensoha.feishu" || result.RelatedIDs["connectorId"] != "feishu" || result.RelatedIDs["actionName"] != "feishu.message.send_text" {
		t.Fatalf("unexpected related ids: %#v", result.RelatedIDs)
	}
	for key, value := range result.RelatedIDs {
		if strings.Contains(strings.ToLower(key), "token") || strings.Contains(strings.ToLower(key), "secret") || value == runtimeToken {
			t.Fatalf("related ids leaked runtime credential: %#v", result.RelatedIDs)
		}
	}
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].ToolName != "feishu.message.send_text" || repo.auditLogs[0].Result != "success" {
		t.Fatalf("expected Gateway audit log for connector invocation, got %#v", repo.auditLogs)
	}
	metadataRelatedIDs, ok := repo.auditLogs[0].Metadata["relatedIds"].(map[string]any)
	if !ok {
		t.Fatalf("expected connector related ids in audit metadata, got %#v", repo.auditLogs[0].Metadata)
	}
	if metadataRelatedIDs["pluginId"] != "opensoha.feishu" || metadataRelatedIDs["connectorId"] != "feishu" || metadataRelatedIDs["actionName"] != "feishu.message.send_text" {
		t.Fatalf("expected connector related ids in audit metadata, got %#v", metadataRelatedIDs)
	}
	if strings.Contains(jsonString(t, repo.auditLogs[0]), runtimeToken) || strings.Contains(jsonString(t, audit.entries), runtimeToken) {
		t.Fatalf("audit leaked runtime token: gateway=%#v generic=%#v", repo.auditLogs, audit.entries)
	}
}

func TestConnectorRuntimeProviderRejectsRuntimeActionErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest":
			writeJSON(t, w, map[string]any{
				"id": "feishu",
				"actions": []map[string]any{
					{"name": "feishu.message.send_text", "description": "Send a text message."},
				},
			})
		case "/actions/feishu.message.send_text":
			writeJSON(t, w, map[string]any{
				"ok":    false,
				"error": map[string]any{"code": "tenant_token_missing"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := DiscoverConnectorRuntime(context.Background(), server.URL, server.Client())
	if err != nil {
		t.Fatalf("DiscoverConnectorRuntime returned error: %v", err)
	}
	service := New(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		},
	}), nil, &memoryGatewayRepository{})
	service.SetCapabilityProviders(provider)

	_, err = service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "feishu.message.send_text",
	})
	if err == nil || !strings.Contains(err.Error(), "tenant_token_missing") {
		t.Fatalf("expected connector runtime error, got %v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func jsonString(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(raw)
}
