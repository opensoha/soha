package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
)

func TestAgentHelmMutationsDelegateToAgent(t *testing.T) {
	var seen []string
	server := newAgentHelmTestServer(t, &seen)
	defer server.Close()

	service := New(Dependencies{
		Agents:      testAgentClients(agentinfra.NewRegistry(0)),
		Connections: stubConnectionResolver{connection: agentConnection(server.URL)},
		Authorizer:  allowAllResourceAuthorizer{},
		Audit:       noopResourceAuditRecorder{},
	})
	principal := domainidentity.Principal{UserID: "user-1"}

	installed, err := service.Helm().InstallHelmChart(context.Background(), principal, "agent-cluster", domainresource.HelmChartInstallInput{
		RepositoryURL:  "https://charts.example",
		ChartName:      "nginx",
		Version:        "1.2.3",
		ReleaseName:    "edge",
		Namespace:      "platform",
		TimeoutSeconds: 60,
	})
	if err != nil {
		t.Fatalf("InstallHelmChart() error = %v", err)
	}
	if installed.Name != "edge" || installed.Revision != "1" {
		t.Fatalf("installed = %#v, want edge revision 1", installed)
	}

	values, err := service.Helm().UpdateHelmReleaseValues(context.Background(), principal, "agent-cluster", "platform", "edge", "replicaCount: 2\n")
	if err != nil {
		t.Fatalf("UpdateHelmReleaseValues() error = %v", err)
	}
	if values.Revision != "2" || !values.Editable {
		t.Fatalf("values = %#v, want revision 2 editable", values)
	}

	if err := service.Helm().DeleteHelmRelease(context.Background(), principal, "agent-cluster", "platform", "edge"); err != nil {
		t.Fatalf("DeleteHelmRelease() error = %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("request count = %d, want 3: %#v", len(seen), seen)
	}
}

func newAgentHelmTestServer(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.String())
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/platform/helm/charts/install":
			var req domainresource.HelmChartInstallInput
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode install request: %v", err)
			}
			if req.ReleaseName != "edge" || req.Namespace != "platform" || req.ChartName != "nginx" || req.RepositoryURL != "https://charts.example" {
				t.Fatalf("unexpected install request: %#v", req)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"name":         "edge",
				"namespace":    "platform",
				"revision":     "1",
				"status":       "deployed",
				"chartName":    "nginx",
				"chartVersion": "1.2.3",
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/platform/helm/releases/edge/values":
			if r.URL.Query().Get("namespace") != "platform" {
				t.Fatalf("unexpected values query: %s", r.URL.RawQuery)
			}
			var req struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode values request: %v", err)
			}
			if req.Content != "replicaCount: 2\n" {
				t.Fatalf("values content = %q, want replicaCount", req.Content)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"name":        "edge",
				"namespace":   "platform",
				"revision":    "2",
				"content":     req.Content,
				"original":    req.Content,
				"editable":    true,
				"diffEnabled": true,
			}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/platform/helm/releases/edge":
			if r.URL.Query().Get("namespace") != "platform" {
				t.Fatalf("unexpected delete query: %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
}
