package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"go.uber.org/zap"
)

func TestSecretLeaseRedemptionInjectsAndRedacts(t *testing.T) {
	const secretValue = "plaintext-from-store"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/v1/runner/secret-leases/lease-1/redeem" {
			t.Fatalf("request = %s %s", req.Method, req.URL.Path)
		}
		if req.Header.Get("Authorization") != "Bearer runner-token" || req.Header.Get("X-Soha-Secret-Lease-Token") != "lease-token" || req.Header.Get("X-Soha-Agent-ID") != "agent-1" {
			t.Fatalf("invalid lease redemption headers")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"leaseId": "lease-1", "expiresAt": time.Now().UTC().Add(time.Minute), "values": map[string]string{"REGISTRY_TOKEN": secretValue},
		}})
	}))
	defer server.Close()

	runner := New(cfgpkg.ControlPlaneConfig{BaseURL: server.URL, BearerToken: "runner-token"}, zap.NewNop())
	ctx, err := runner.redeemSecretLease(context.Background(), &sohaapi.SecretLeaseGrant{ID: "lease-1", Token: "lease-token", ExpiresAt: time.Now().UTC().Add(time.Minute)}, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REGISTRY_TOKEN", "stale-host-value")
	logs, err := runCommand(ctx, "", "/bin/sh", "-lc", `printf '%s' "$REGISTRY_TOKEN"`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), secretValue) {
		t.Fatalf("secret was not injected into child environment: %#v", logs)
	}
	redacted := redactResolvedSecretValues(ctx, map[string]any{"logs": logs, "nested": map[string]any{"value": secretValue}})
	encoded, _ := json.Marshal(redacted)
	if strings.Contains(string(encoded), secretValue) || !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("secret was not redacted: %s", encoded)
	}
}

func TestValidSecretEnvironmentAliasRejectsExecutionOverrides(t *testing.T) {
	for _, alias := range []string{"PATH", "BASH_ENV", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES"} {
		if validSecretEnvironmentAlias(alias) {
			t.Errorf("validSecretEnvironmentAlias(%q) = true", alias)
		}
	}
	if !validSecretEnvironmentAlias("REGISTRY_TOKEN") {
		t.Error("validSecretEnvironmentAlias(REGISTRY_TOKEN) = false")
	}
}

func TestSecretLeaseRedactsRegistryCredentialFragments(t *testing.T) {
	auth := base64.StdEncoding.EncodeToString([]byte("user:encoded-password"))
	value := `{"auths":{"registry.example":{"auth":"` + auth + `"},"other.example":{"password":"plain-password","identitytoken":"identity-token"}}}`
	ctx := context.WithValue(context.Background(), secretValuesContextKey{}, map[string]string{"REGISTRY_AUTH": value})
	logs := []string{value, "Basic " + auth, "user:encoded-password", "encoded-password plain-password identity-token"}
	result, _ := json.Marshal(redactResolvedSecretValues(ctx, map[string]any{"logs": logs}))
	for _, secret := range []string{auth, "encoded-password", "plain-password", "identity-token"} {
		if strings.Contains(string(result), secret) {
			t.Fatalf("registry credential fragment was not redacted: %s", result)
		}
	}
	if !strings.Contains(string(result), "[REDACTED]") {
		t.Fatalf("redaction marker missing: %s", result)
	}
}
