package observabilityprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appobservability "github.com/opensoha/soha/internal/application/observability"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

func TestLogClientSearchUsesVersionedActionProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/actions/logs.query" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var payload struct {
			ProtocolVersion string            `json:"protocolVersion"`
			ProviderKey     string            `json:"providerKey"`
			Credentials     map[string]string `json:"credentials"`
			Config          map[string]any    `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ProtocolVersion != "v1" || payload.ProviderKey != "test-logs" || payload.Credentials["token"] != "secret" || payload.Config["credentials"] != nil {
			t.Fatalf("payload = %#v", payload)
		}
		_ = json.NewEncoder(w).Encode(telemetry.LogSearchResult{Records: []telemetry.LogRecord{{Timestamp: time.Unix(1, 0).UTC(), Message: "ok"}}})
	}))
	defer server.Close()

	runtime := appobservability.ProviderRuntime{ProviderKey: "test-logs", ProtocolVersion: "v1", Action: "logs.query", Runtime: sohaapi.PluginRuntimeSpec{
		Mode: sohaapi.ManagedContainer, Endpoint: server.URL, ActionPath: "/v1/actions/{action}",
	}}
	result, err := NewLogClient().Search(context.Background(), runtime, "source-1", map[string]any{"endpoint": "unused", "credentials": map[string]string{"token": "secret"}}, telemetry.LogSearchQuery{Limit: 1})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.SourceID != "source-1" || len(result.Records) != 1 || result.Records[0].Message != "ok" {
		t.Fatalf("result = %#v", result)
	}
}

func TestLogClientRejectsPrivateExternalEndpoint(t *testing.T) {
	err := NewLogClient().ValidateConfig(appobservability.ProviderRuntime{ProtocolVersion: "v1", Action: "logs.query", Runtime: sohaapi.PluginRuntimeSpec{
		Mode: sohaapi.ExternalHTTP, Endpoint: "https://127.0.0.1:8080", ActionPath: "/{action}",
	}}, nil)
	if err == nil {
		t.Fatal("ValidateConfig() should reject a private external endpoint")
	}
}
