package sohacli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func TestHostRuntimeConfigStagesSecretAndMasksResponse(t *testing.T) {
	const value = "synthetic-hosted-secret"
	t.Setenv("APP_TOKEN", value)
	update, err := readHostEnvUpdate([]string{"APP_TOKEN"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readHostEnvUpdate([]string{"APP_TOKEN=" + value}, nil, true); err == nil || strings.Contains(err.Error(), value) {
		t.Fatal("invalid name exposed the secret")
	}
	if _, err := readHostEnvUpdate([]string{"APP_TOKEN"}, nil, false); err == nil {
		t.Fatal("accepted secret update without confirmation")
	}
	var reject atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPut || req.URL.Path != "/api/v1/app-hosting/apps/app_1/runtime-config" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body sohaapi.UpdateHostedAppRuntimeConfigRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || len(body.SetEnvironment) != 1 ||
			body.SetEnvironment[0].Name != "APP_TOKEN" || body.SetEnvironment[0].Value != value {
			t.Error("request did not contain the expected write-only value")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if reject.Load() {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_argument","message":"`+value+`"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"environmentKeys":["APP_TOKEN"],"dataWarningBytes":209715200}}`)
	}))
	defer server.Close()
	client := &hostClient{sdk: sohaapi.NewClient(server.URL)}
	result, err := hostRuntimeConfig(t.Context(), client, "app_1", update)
	encoded, _ := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), "APP_TOKEN") || strings.Contains(string(encoded), value) {
		t.Fatal("unexpected masked result")
	}
	reject.Store(true)
	if _, err := hostRuntimeConfig(t.Context(), client, "app_1", update); err == nil || strings.Contains(err.Error(), value) {
		t.Fatal("remote error exposed the write-only value")
	}
}
