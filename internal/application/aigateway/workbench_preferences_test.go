package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestWorkbenchReasoningPreferencesReachUpstream(t *testing.T) {
	for _, endpoint := range []string{"chat/completions", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload["model"] != "gpt-upstream" {
					t.Errorf("wrong model: %v", payload["model"])
				}
				if _, ok := payload["temperature"]; ok {
					t.Error("reasoning models must not receive fixed temperature")
				}
				effort := payload["reasoning_effort"]
				if endpoint == "responses" {
					reasoning, _ := payload["reasoning"].(map[string]any)
					effort = reasoning["effort"]
				}
				if effort != "high" {
					t.Errorf("effort = %v", effort)
				}
				w.Header().Set("Content-Type", "application/json")
				if endpoint == "responses" {
					_, _ = io.WriteString(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)
				} else {
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
				}
			}))
			defer upstream.Close()
			repo := relayRepoForUpstream(t, upstream.URL, "openai", "test-key")
			repo.routes[0].Metadata = map[string]any{"reasoningEfforts": []string{"low", "medium", "high"}}
			service := newRelayRuntimeTestService(repo)
			options, err := service.ListWorkbenchModels(context.Background(), relayWorkbenchTestPrincipal(), endpoint)
			if err != nil || len(options) != 1 || len(options[0].ReasoningEfforts) != 3 {
				t.Fatalf("catalog: %v %v", options, err)
			}
			_, err = service.InvokeWorkbenchModel(context.Background(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{PublicModel: "gpt-public", Endpoint: endpoint, ReasoningEffort: "high", Messages: []WorkbenchRelayMessage{{Role: "user", Content: "hello"}}})
			if err != nil {
				t.Fatal(err)
			}
			repo.routes[0].Metadata = nil
			_, err = service.InvokeWorkbenchModelStream(context.Background(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{PublicModel: "gpt-public", Endpoint: endpoint, ReasoningEffort: "high"}, nil)
			if err == nil {
				t.Fatal("unsupported effort accepted")
			}
			repo.routes[0].Enabled = false
			options, err = service.ListWorkbenchModels(context.Background(), relayWorkbenchTestPrincipal(), endpoint)
			if err != nil || len(options) != 0 {
				t.Fatalf("disabled route exposed: %v %v", options, err)
			}
		})
	}
}

func TestResolveWorkbenchModelOptionUsesPinnedRoute(t *testing.T) {
	repo := relayRepoForUpstream(t, "https://upstream.example", "openai", "test-key")
	repo.routes[0].Metadata = map[string]any{"reasoningEfforts": []string{"low"}}
	service := newRelayRuntimeTestService(repo)
	editorPermission := appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")
	service.permissions = appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"editor": {editorPermission},
	}})
	principal := testPrincipal("editor")
	input := WorkbenchRelayRequest{RouteID: repo.routes[0].ID, Endpoint: "chat/completions"}
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayView}
	if _, err := service.ResolveWorkbenchModelOption(t.Context(), principal, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("unauthorized actor resolved profile model: %v", err)
	}
	principal.PermissionKeys = []string{editorPermission}
	if option, err := service.ResolveWorkbenchModelOption(t.Context(), principal, input); err != nil || option.PublicModel != "gpt-public" || len(option.ReasoningEfforts) != 1 || option.ReasoningEfforts[0] != "low" {
		t.Fatalf("resolve supported effort on pinned route: %+v, %v", option, err)
	}
}
