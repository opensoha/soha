package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	"go.uber.org/zap"
)

func TestPersonalEnvironmentPreflightHTTPReal(t *testing.T) {
	if os.Getenv("SOHA_TEST_PERSONAL_DOCKER") != "1" {
		t.Skip("requires owned disposable Docker validation")
	}
	cfg := personalTestConfig()
	cfg.Security.AllowedActions = []string{actionPersonalEnvironmentPreflight}
	server := New(cfg, zap.NewNop(), nil, nil)
	request := contractenv.PreflightRequest{SchemaVersion: contractenv.PreflightRequestVersion, ID: uuid.NewString(), Quota: personalTestRequest("inspect").Lease.Quota}
	t.Run("quota_receipt_before_personal_data", func(t *testing.T) {
		data, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/personal-environments/preflight", strings.NewReader(string(data)))
		r.Header.Set("Authorization", "Bearer personal-fixture-token")
		response := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(response, r)
		if response.Code != http.StatusOK {
			t.Fatalf("preflight returned %d: %s", response.Code, response.Body.String())
		}
		var result struct {
			Data contractenv.PreflightResult `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if err := result.Data.ValidateFor(request); err != nil {
			t.Fatal(err)
		}
	})
}
