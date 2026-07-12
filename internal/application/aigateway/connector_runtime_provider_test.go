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
	server := newConnectorRuntimeTestServer(t, runtimeToken, &invoked)
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
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke},
		},
	}), audit, repo)
	service.SetCapabilityProviders(provider)

	manifest, err := service.Capabilities(context.Background(), testPrincipal("developer"), domainaigateway.ManifestRequest{})
	if err != nil {
		t.Fatalf("Capabilities returned error: %v", err)
	}
	assertConnectorRuntimeManifest(t, manifest)

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
	assertConnectorRuntimeInvocation(t, result, invoked, runtimeToken)
	assertConnectorRuntimeAudit(t, repo, audit, runtimeToken)
}

func newConnectorRuntimeTestServer(t *testing.T, runtimeToken string, invoked *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest":
			writeConnectorRuntimeManifest(t, w, r, runtimeToken)
		case "/actions/feishu.message.send_text":
			invokeConnectorRuntimeAction(t, w, r, runtimeToken, invoked)
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeConnectorRuntimeManifest(t *testing.T, w http.ResponseWriter, r *http.Request, runtimeToken string) {
	t.Helper()
	if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
		t.Fatalf("manifest request missing bearer token: %q", r.Header.Get("Authorization"))
	}
	writeJSON(t, w, map[string]any{
		"id": "feishu", "name": "Feishu Connector", "description": "Feishu connector runtime.",
		"actions": []map[string]any{{
			"name": "feishu.message.send_text", "description": "Send a text message.",
			"inputSchema": map[string]any{"type": "object", "required": []string{"receiveIdType", "receiveId", "text"}},
		}},
	})
}

func invokeConnectorRuntimeAction(t *testing.T, w http.ResponseWriter, r *http.Request, runtimeToken string, invoked *bool) {
	t.Helper()
	if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+runtimeToken {
		t.Fatalf("invalid action request: method=%s authorization=%q", r.Method, r.Header.Get("Authorization"))
	}
	var input map[string]any
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		t.Fatalf("decode action input: %v", err)
	}
	if input["text"] != "hello" || input["receiveId"] != "chat-1" {
		t.Fatalf("unexpected action input: %#v", input)
	}
	*invoked = true
	writeJSON(t, w, map[string]any{"ok": true, "output": map[string]any{"messageId": "msg-1", "status": "sent"}})
}

func assertConnectorRuntimeManifest(t *testing.T, manifest domainaigateway.Manifest) {
	t.Helper()
	if len(manifest.Tools) != 1 || manifest.Tools[0].Name != "feishu.message.send_text" {
		t.Fatalf("connector manifest tools: %#v", manifest.Tools)
	}
	tool := manifest.Tools[0]
	if tool.RiskLevel != domainaigateway.RiskLevelMutate || tool.PermissionKeys[0] != appaccess.PermAIGatewayInvoke {
		t.Fatalf("connector tool policy: %#v", tool)
	}
}

func assertConnectorRuntimeInvocation(t *testing.T, result domainaigateway.ToolInvocationResult, invoked bool, runtimeToken string) {
	t.Helper()
	if !invoked {
		t.Fatal("connector runtime action was not invoked")
	}
	output := mustValueAs[map[string]any](t, result.Output)
	if output["messageId"] != "msg-1" || output["status"] != "sent" {
		t.Fatalf("connector output: %#v", output)
	}
	if result.RelatedIDs["pluginId"] != "opensoha.feishu" || result.RelatedIDs["connectorId"] != "feishu" || result.RelatedIDs["actionName"] != "feishu.message.send_text" {
		t.Fatalf("connector related ids: %#v", result.RelatedIDs)
	}
	for key, value := range result.RelatedIDs {
		if strings.Contains(strings.ToLower(key), "token") || strings.Contains(strings.ToLower(key), "secret") || value == runtimeToken {
			t.Fatalf("related ids leaked credential: %#v", result.RelatedIDs)
		}
	}
}

func assertConnectorRuntimeAudit(t *testing.T, repo *memoryGatewayRepository, audit *captureAuditRecorder, runtimeToken string) {
	t.Helper()
	if len(repo.auditLogs) != 1 || repo.auditLogs[0].ToolName != "feishu.message.send_text" || repo.auditLogs[0].Result != "success" {
		t.Fatalf("connector audit log: %#v", repo.auditLogs)
	}
	related := mustValueAs[map[string]any](t, repo.auditLogs[0].Metadata["relatedIds"])
	if related["pluginId"] != "opensoha.feishu" || related["connectorId"] != "feishu" || related["actionName"] != "feishu.message.send_text" {
		t.Fatalf("audit related ids: %#v", related)
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
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{
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
