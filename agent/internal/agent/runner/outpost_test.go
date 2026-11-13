package runner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"go.uber.org/zap"
)

func TestOutpostRuntimeRequiresSignedMonotonicUnexpiredConfig(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newOutpostRuntime(base64.StdEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	config := signedOutpostConfig(t, privateKey, now, 1)
	if err := runtime.apply(config, "test-key", now); err != nil {
		t.Fatalf("apply valid config: %v", err)
	}

	if err := runtime.apply(config, "test-key", now); err == nil {
		t.Fatal("replayed configuration lease accepted")
	}
	renewed := signedOutpostConfig(t, privateKey, now.Add(30*time.Minute), 1)
	if err := runtime.apply(renewed, "test-key", now.Add(30*time.Minute)); err != nil {
		t.Fatalf("renew same-version configuration lease: %v", err)
	}
	tampered := signedOutpostConfig(t, privateKey, now, 2)
	tampered.Routes[0].ProviderID = "tampered"
	if err := runtime.apply(tampered, "test-key", now); err == nil {
		t.Fatal("tampered configuration accepted")
	}
	expired := signedOutpostConfig(t, privateKey, now.Add(-2*time.Hour), 2)
	if err := runtime.apply(expired, "test-key", now); err == nil {
		t.Fatal("expired configuration accepted")
	}

	_, route, skip, err := runtime.route("app.example.com:443", "/public/health", now)
	if err != nil || route.ProviderID != "provider-1" || !skip {
		t.Fatalf("signed skip route = %#v, %v, %v", route, skip, err)
	}
	if _, _, _, err := runtime.route("app.example.com", "/private", now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired last-known-good configuration served traffic")
	}
}

func TestRunnerOutpostCheckUsesCoreAndCleansHeaders(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/identity/outposts/outpost-1/check" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"decision": "allow", "statusCode": 204,
			"headers": map[string]string{"X-Auth-Request-User": "alice", "Set-Cookie": "unsafe", "X-Soha-Projects": "project-1", "X-Soha-Tags": "tag-1", "X-Soha-Email": "a@example.com\r\nInjected: yes"},
		}})
	}))
	defer server.Close()

	r := New(cfgpkg.ControlPlaneConfig{BaseURL: server.URL, BearerToken: "token", Outpost: cfgpkg.OutpostConfig{Enabled: true}}, zap.NewNop())
	runtime, err := newOutpostRuntime(base64.StdEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := runtime.apply(signedOutpostConfig(t, privateKey, now, 1), "test-key", now); err != nil {
		t.Fatal(err)
	}
	r.outpost = runtime
	result, err := r.CheckOutpostAccess(context.Background(), ForwardAuthRequest{Method: "GET", OriginalURL: "/private", RequestHost: "app.example.com", RequestPath: "/private"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if result.Headers["X-Auth-Request-User"] != "alice" || result.Headers["Set-Cookie"] != "" || result.Headers["X-Soha-Projects"] != "project-1" || result.Headers["X-Soha-Tags"] != "tag-1" {
		t.Fatalf("headers not cleaned: %#v", result.Headers)
	}
	if result.Headers["X-Soha-Email"] != "a@example.comInjected: yes" {
		t.Fatalf("CRLF not removed: %#v", result.Headers)
	}
}

func TestRunnerClaimsAndHeartbeatsOutpostConfiguration(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	config := signedOutpostConfig(t, privateKey, now, 1)
	heartbeats := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/v1/identity/outposts/runtime/claim":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": config})
		case "/api/v1/identity/outposts/outpost-1/heartbeat":
			var heartbeat sohaapi.IdentityOutpostHeartbeatRequest
			if err := json.NewDecoder(req.Body).Decode(&heartbeat); err != nil || heartbeat.ConfigurationExpiresAt == nil || !heartbeat.ConfigurationExpiresAt.Equal(config.ExpiresAt.Truncate(time.Second)) || heartbeat.RuntimeVersion == "" {
				t.Errorf("missing applied lease or runtime version: %#v, %v", heartbeat, err)
			}
			heartbeats++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"accepted": true, "desiredConfigurationVersion": 1}})
		case "/api/v1/identity/outposts/outpost-1/events":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	r := New(cfgpkg.ControlPlaneConfig{
		BaseURL: server.URL, BearerToken: "token",
		Outpost: cfgpkg.OutpostConfig{Enabled: true, AgentID: "agent-1", ProtocolVersion: "v1", TrustKeyID: "test-key", TrustPublicKey: base64.StdEncoding.EncodeToString(publicKey)},
	}, zap.NewNop())
	r.outpost, err = newOutpostRuntime(r.cfg.Outpost.TrustPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	r.claimOutpostConfig(context.Background())
	if status := r.OutpostStatus(); !status.Ready || status.ConfigurationVersion != 1 {
		t.Fatalf("status = %#v", status)
	}
	r.heartbeatOutpost(context.Background())
	if heartbeats != 1 || r.OutpostStatus().LastHeartbeatAt == "" {
		t.Fatalf("heartbeats = %d, status = %#v", heartbeats, r.OutpostStatus())
	}
}

func TestRunnerReportsRejectedConfigurationAndRecovers(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	config := signedOutpostConfig(t, privateKey, time.Now().UTC(), 1)
	config.Signature = "invalid"
	var heartbeat sohaapi.IdentityOutpostHeartbeatRequest
	events := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/v1/identity/outposts/runtime/claim":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": config})
		case "/api/v1/identity/outposts/outpost-1/heartbeat":
			if err := json.NewDecoder(req.Body).Decode(&heartbeat); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"accepted": true, "desiredConfigurationVersion": 0}})
		case "/api/v1/identity/outposts/outpost-1/events":
			events++
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	r := New(cfgpkg.ControlPlaneConfig{BaseURL: server.URL, BearerToken: "test-token", Outpost: cfgpkg.OutpostConfig{Enabled: true, AgentID: "agent-1", ProtocolVersion: "v1", TrustKeyID: "test-key", TrustPublicKey: base64.StdEncoding.EncodeToString(publicKey)}}, zap.NewNop())
	r.outpost, err = newOutpostRuntime(r.cfg.Outpost.TrustPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	r.claimOutpostConfig(context.Background())
	r.heartbeatOutpost(context.Background())
	if heartbeat.Status != sohaapi.IdentityOutpostHeartbeatRequestStatusUnavailable || heartbeat.ErrorCode != "configuration_rejected" || heartbeat.ConfigurationVersion != 0 || heartbeat.ConfigurationExpiresAt != nil {
		t.Fatalf("rejected configuration reported as applied: %#v", heartbeat)
	}
	if _, _, _, err := r.outpost.route("app.example.com", "/private", time.Now()); err == nil {
		t.Fatal("unsigned configuration authorized access")
	}
	config = signedOutpostConfig(t, privateKey, time.Now().UTC(), 1)
	r.claimOutpostConfig(context.Background())
	config = signedOutpostConfig(t, privateKey, time.Now().UTC().Add(time.Second), 1)
	r.claimOutpostConfig(context.Background())
	if !r.OutpostStatus().Ready || r.OutpostStatus().ErrorCode != "" || events != 1 {
		t.Fatalf("recovery or same-version event suppression failed: status=%#v, events=%d", r.OutpostStatus(), events)
	}
}

func signedOutpostConfig(t *testing.T, privateKey ed25519.PrivateKey, issuedAt time.Time, version int64) sohaapi.IdentityOutpostRuntimeConfig {
	t.Helper()
	config := sohaapi.IdentityOutpostRuntimeConfig{
		OutpostID: "outpost-1", ProtocolVersion: "v1", ConfigurationVersion: version,
		IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour), KeyID: "test-key", CheckURL: "/identity/outposts/outpost-1/check",
		Routes: []sohaapi.IdentityOutpostRoute{{Host: "app.example.com", PathPrefix: "/", ProviderID: "provider-1", SkipPaths: []string{"/public"}}},
	}
	config.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, outpostConfigPayload(config)))
	return config
}
