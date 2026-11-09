package runner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"go.uber.org/zap"
)

func TestOutpostDurableFloorRejectsReplayAfterRestart(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cfg := cfgpkg.OutpostConfig{AgentID: "agent-1", StateDir: filepath.Join(t.TempDir(), "state"), TrustKeyID: "test-key", TrustPublicKey: base64.StdEncoding.EncodeToString(pub)}
	open := func() *outpostRuntime {
		t.Helper()
		r, err := newConfiguredOutpostRuntime(cfg, "https://core.example")
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	now := time.Now().UTC()
	initial := signedOutpostConfig(t, priv, now, 2)
	r := open()
	if err := r.apply(initial, "test-key", now); err != nil {
		t.Fatal(err)
	}
	if _, err := newConfiguredOutpostRuntime(cfg, "https://core.example"); err == nil {
		t.Fatal("second runtime stole state lock")
	}
	other := cfg
	other.AgentID = "agent-2"
	independent, err := newConfiguredOutpostRuntime(other, "https://core.example")
	if err != nil {
		t.Fatal(err)
	}
	independent.state.close()
	r.state.close()
	r = open()
	defer r.state.close()
	if r.status(now).Ready {
		t.Fatal("restart restored routing without a fresh signed claim")
	}
	for _, name := range []string{"replay", "lower version", "old issuedAt", "different content", "expired", "unknown key"} {
		t.Run(name, func(t *testing.T) {
			candidate := signedOutpostConfig(t, priv, now.Add(time.Minute), 2)
			switch name {
			case "replay":
				candidate = initial
			case "lower version":
				candidate.ConfigurationVersion = 1
			case "old issuedAt":
				candidate.IssuedAt = now.Add(-time.Second)
				candidate.ConfigurationVersion = 3
			case "different content":
				candidate.Routes[0].ProviderID = "different"
			case "expired":
				candidate.ExpiresAt = now
			case "unknown key":
				candidate.KeyID = "unknown"
			}
			candidate.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, outpostConfigPayload(candidate)))
			if err := r.apply(candidate, "test-key", now.Add(time.Minute)); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	renewal := signedOutpostConfig(t, priv, now.Add(time.Minute), 2)
	if err := r.apply(renewal, "test-key", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !r.status(now.Add(time.Minute)).Ready {
		t.Fatal("fresh renewal failed to recover")
	}
	next := signedOutpostConfig(t, priv, now.Add(2*time.Minute), 3)
	next.Routes[0].ProviderID = "provider-2"
	next.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, outpostConfigPayload(next)))
	if err := r.apply(next, "test-key", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("higher version content update rejected: %v", err)
	}
}

func TestOutpostTrustKeyRotationUsesLocalAnchors(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)
	cfg := cfgpkg.OutpostConfig{AgentID: "agent", StateDir: filepath.Join(t.TempDir(), "state"), TrustKeyID: "test-key", TrustPublicKey: base64.StdEncoding.EncodeToString(pub), TrustPublicKeys: map[string]string{"next-key": base64.StdEncoding.EncodeToString(pub2)}}
	r, err := newConfiguredOutpostRuntime(cfg, "https://core.example")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := r.apply(signedOutpostConfig(t, priv, now, 1), "ignored", now); err != nil {
		t.Fatal(err)
	}
	next := signedOutpostConfig(t, priv2, now.Add(time.Minute), 1)
	next.KeyID = "next-key"
	next.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv2, outpostConfigPayload(next)))
	if err := r.apply(next, "ignored", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	r.state.close()
	cfg.TrustKeyID, cfg.TrustPublicKey = "", "" // Remove the old anchor after overlap.
	r, err = newConfiguredOutpostRuntime(cfg, "https://core.example")
	if err != nil {
		t.Fatal(err)
	}
	defer r.state.close()
	if err := r.apply(signedOutpostConfig(t, priv, now.Add(2*time.Minute), 2), "test-key", now.Add(2*time.Minute)); err == nil {
		t.Fatal("removed signing key still trusted")
	}
}

func TestOutpostPersistenceFailureClosesExistingRoutes(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cfg := cfgpkg.OutpostConfig{AgentID: "agent", StateDir: filepath.Join(t.TempDir(), "state"), TrustKeyID: "test-key", TrustPublicKey: base64.StdEncoding.EncodeToString(pub)}
	r, err := newConfiguredOutpostRuntime(cfg, "https://core.example")
	if err != nil {
		t.Fatal(err)
	}
	defer r.state.close()
	now := time.Now().UTC()
	if err := r.apply(signedOutpostConfig(t, priv, now, 1), "test-key", now); err != nil {
		t.Fatal(err)
	}
	// A directory at the destination deterministically prevents atomic replacement.
	statePath := filepath.Join(cfg.StateDir, r.state.name)
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := r.apply(signedOutpostConfig(t, priv, now.Add(time.Minute), 2), "test-key", now.Add(time.Minute)); err == nil {
		t.Fatal("failed persistence accepted")
	}
	if r.floor.Version != 1 || r.status(now).Ready {
		t.Fatal("failed persistence advanced or remained ready")
	}
	if _, _, _, err := r.route("app.example.com", "/private", now); err == nil {
		t.Fatal("route authorized after persistence failure")
	}
}

func TestOutpostCorruptStateFailsStartup(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	cfg := cfgpkg.OutpostConfig{AgentID: "agent", StateDir: filepath.Join(t.TempDir(), "state"), TrustKeyID: "test-key", TrustPublicKey: base64.StdEncoding.EncodeToString(pub)}
	r, err := newConfiguredOutpostRuntime(cfg, "https://core.example")
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(cfg.StateDir, r.state.name)
	r.state.close()
	if err := os.WriteFile(statePath, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newConfiguredOutpostRuntime(cfg, "https://core.example"); err == nil {
		t.Fatal("corrupt floor reset on restart")
	}
}

func TestOutpostOnlineCheckFailsClosedWithValidLease(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	var unavailable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/v1/identity/outposts/outpost-1/events" {
			w.WriteHeader(204)
			return
		}
		if unavailable.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"decision":"allow","statusCode":204}}`))
	}))
	defer server.Close()
	r := New(cfgpkg.ControlPlaneConfig{BaseURL: server.URL, BearerToken: "test-token", Outpost: cfgpkg.OutpostConfig{Enabled: true}}, zap.NewNop())
	runtime, err := newOutpostRuntime(base64.StdEncoding.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	r.outpost = runtime
	now := time.Now()
	if err := runtime.apply(signedOutpostConfig(t, priv, now, 1), "test-key", now); err != nil {
		t.Fatal(err)
	}
	request := ForwardAuthRequest{Method: "GET", RequestHost: "app.example.com", RequestPath: "/private"}
	if _, err := r.CheckOutpostAccess(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	unavailable.Store(true)
	result, err := r.CheckOutpostAccess(context.Background(), request)
	if err == nil || result.Decision != "deny" || result.StatusCode != 503 {
		t.Fatalf("offline check allowed: %#v, %v", result, err)
	}
	// A monotonic local deadline is independently authoritative, even if wall expiry is still future.
	runtime.mu.Lock()
	runtime.leaseDeadline = now.Add(-time.Second)
	runtime.mu.Unlock()
	if _, _, _, err := runtime.route("app.example.com", "/private", now); err == nil {
		t.Fatal("local deadline failed to expire routing")
	}
}

func TestOutpostClockSkewCannotExtendSignedLeaseDuration(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	runtime, err := newOutpostRuntime(base64.StdEncoding.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	config := signedOutpostConfig(t, priv, now.Add(30*time.Second), 1)
	if err := runtime.apply(config, "test-key", now); err != nil {
		t.Fatal(err)
	}
	if runtime.leaseDeadline.Sub(now) > config.ExpiresAt.Sub(config.IssuedAt) {
		t.Fatal("clock skew extended signed lease duration")
	}
}
