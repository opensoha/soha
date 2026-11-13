package runner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"go.uber.org/zap"
)

func managedOutpostFixture(t *testing.T) (*outpostRuntime, cfgpkg.OutpostConfig, ed25519.PrivateKey) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := cfgpkg.OutpostConfig{Enabled: true, AgentID: "agent-1", InstanceID: "pod-1", RuntimeControlEnabled: true, StateDir: filepath.Join(t.TempDir(), "state"), TrustKeyID: "test-key", TrustPublicKey: base64.StdEncoding.EncodeToString(pub)}
	r, err := newConfiguredOutpostRuntime(cfg, "https://core.example")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.state.close)
	return r, cfg, key
}

func signedOutpostControl(key ed25519.PrivateKey, r *outpostRuntime, at time.Time, generation int64, action string) *sohaapi.IdentityOutpostRuntimeControl {
	c := &sohaapi.IdentityOutpostRuntimeControl{OutpostID: "outpost-1", ProtocolVersion: "v1", InstanceID: r.instanceID, ProcessEpoch: r.processEpoch, Generation: generation, Action: sohaapi.IdentityOutpostRuntimeControlAction(action), IssuedAt: at, ExpiresAt: at.Add(30 * time.Second), KeyID: "test-key"}
	c.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, outpostControlPayload(*c)))
	return c
}

func TestOutpostControlSigningPayloadOrder(t *testing.T) {
	at := time.Date(2026, 11, 7, 8, 9, 10, 0, time.FixedZone("offset", 8*3600))
	c := sohaapi.IdentityOutpostRuntimeControl{OutpostID: "o", ProtocolVersion: "v1", InstanceID: "i", ProcessEpoch: "p", Generation: 2, Action: "drain", IssuedAt: at, ExpiresAt: at.Add(time.Second), KeyID: "k", Signature: "excluded"}
	want := `{"outpostId":"o","protocolVersion":"v1","instanceId":"i","processEpoch":"p","generation":2,"action":"drain","issuedAt":"2026-11-07T00:09:10Z","expiresAt":"2026-11-07T00:09:11Z","keyId":"k"}`
	if got := string(outpostControlPayload(c)); got != want {
		t.Fatalf("payload=%s", got)
	}
}

func TestOutpostControlDrainCountsExistingChecksAndBlocksNew(t *testing.T) {
	runtime, _, key := managedOutpostFixture(t)
	now := time.Now().Add(-time.Second)
	config := signedOutpostConfig(t, key, now.Add(-time.Minute), 1)
	config.RuntimeControl = signedOutpostControl(key, runtime, now, 1, "serve")
	if err := runtime.apply(config, "test-key", now); err != nil {
		t.Fatal(err)
	}
	oldServe := config.RuntimeControl
	deadline := runtime.leaseDeadline
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"decision":"allow","statusCode":204}}`))
	}))
	defer server.Close()
	r := New(cfgpkg.ControlPlaneConfig{BaseURL: server.URL, Outpost: cfgpkg.OutpostConfig{Enabled: true}}, zap.NewNop())
	r.outpost = runtime
	done := make(chan error, 1)
	go func() {
		_, err := r.CheckOutpostAccess(context.Background(), ForwardAuthRequest{RequestHost: "app.example.com", RequestPath: "/private"})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("check did not enter")
	}
	config.RuntimeControl = signedOutpostControl(key, runtime, now.Add(time.Millisecond), 1, "drain")
	if err := runtime.apply(config, "test-key", now.Add(time.Millisecond)); err != nil {
		close(release)
		t.Fatal(err)
	}
	status := runtime.status(now.Add(time.Millisecond))
	if status.AcceptingRequests || status.ActiveChecks != 1 || status.AppliedControlGeneration != 1 {
		close(release)
		t.Fatalf("drain status=%#v", status)
	}
	for _, path := range []string{"/private", "/public/health"} {
		result, err := r.CheckOutpostAccess(context.Background(), ForwardAuthRequest{RequestHost: "app.example.com", RequestPath: path})
		if err == nil || result.StatusCode != 503 {
			close(release)
			t.Fatalf("drained path %s accepted", path)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("existing check did not finish: %v", err)
	}
	if runtime.status(now).ActiveChecks != 0 {
		t.Fatal("check count leaked")
	}
	config.RuntimeControl = signedOutpostControl(key, runtime, now.Add(2*time.Millisecond), 1, "serve")
	if err := runtime.apply(config, "test-key", now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if !runtime.leaseDeadline.Equal(deadline) {
		t.Fatal("control renewed configuration lease")
	}
	result, err := r.CheckOutpostAccess(context.Background(), ForwardAuthRequest{RequestHost: "app.example.com", RequestPath: "/public/health"})
	if err != nil || result.Decision != "allow" {
		t.Fatalf("serve did not reopen: %#v %v", result, err)
	}
	if err := runtime.beginCheck(now.Add(31 * time.Second)); err == nil {
		runtime.finishCheck()
		t.Fatal("expired control admitted check")
	}
	config.RuntimeControl = oldServe
	if err := runtime.apply(config, "test-key", now.Add(3*time.Millisecond)); err == nil {
		t.Fatal("old same-generation serve replay accepted")
	}

}

func TestOutpostControlRejectsUntrustedOrReplayedInstructions(t *testing.T) {
	for _, name := range []string{"missing", "bad signature", "unknown key", "wrong outpost", "wrong instance", "wrong epoch", "wrong protocol", "old generation", "old issuedAt", "same issuedAt changed action", "expired", "future", "long lease", "invalid action", "replayed"} {
		t.Run(name, func(t *testing.T) {
			r, _, key := managedOutpostFixture(t)
			now := time.Now().UTC()
			config := signedOutpostConfig(t, key, now.Add(-time.Minute), 1)
			config.RuntimeControl = signedOutpostControl(key, r, now.Add(-2*time.Second), 2, "serve")
			if err := r.apply(config, "test-key", now); err != nil {
				t.Fatal(err)
			}
			c := signedOutpostControl(key, r, now.Add(-time.Second), 2, "serve")
			switch name {
			case "missing":
				c = nil
			case "unknown key":
				c.KeyID = "unknown"
			case "wrong outpost":
				c.OutpostID = "other"
			case "wrong instance":
				c.InstanceID = "other"
			case "wrong epoch":
				c.ProcessEpoch = "old"
			case "wrong protocol":
				c.ProtocolVersion = "v2"
			case "old generation":
				c.Generation = 1
			case "old issuedAt":
				c.Generation = 3
				c.IssuedAt = now.Add(-3 * time.Second)
				c.ExpiresAt = c.IssuedAt.Add(30 * time.Second)
			case "same issuedAt changed action":
				c.Action = "drain"
				c.IssuedAt = now.Add(-2 * time.Second)
				c.ExpiresAt = c.IssuedAt.Add(30 * time.Second)
			case "expired":
				c.IssuedAt = now.Add(-31 * time.Second)
				c.ExpiresAt = now.Add(-time.Second)
			case "future":
				c.IssuedAt = now.Add(time.Second)
			case "long lease":
				c.ExpiresAt = c.IssuedAt.Add(31 * time.Second)
			case "invalid action":
				c.Action = "other"
			case "replayed":
				c = config.RuntimeControl
			}
			if c != nil {
				c.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, outpostControlPayload(*c)))
				if name == "bad signature" {
					c.Signature = "bad"
				}
			}
			config.RuntimeControl = c
			if err := r.apply(config, "test-key", now); err == nil {
				t.Fatal("invalid control accepted")
			}
			if status := r.status(now); status.AcceptingRequests || status.AppliedControlGeneration != 2 {
				t.Fatalf("invalid control changed actual ACK/gate: %#v", status)
			}
		})
	}
}

func TestOutpostControlRestartAndStickyState(t *testing.T) {
	r, cfg, key := managedOutpostFixture(t)
	now := time.Now().Add(-time.Second)
	config := signedOutpostConfig(t, key, now.Add(-time.Minute), 1)
	config.RuntimeControl = signedOutpostControl(key, r, now, 4, "drain")
	if err := r.apply(config, "test-key", now); err != nil {
		t.Fatal(err)
	}
	oldEpoch := r.processEpoch
	r.state.close()
	disabled := cfg
	disabled.RuntimeControlEnabled = false
	if unexpected, err := newConfiguredOutpostRuntime(disabled, "https://core.example"); err == nil {
		unexpected.state.close()
		t.Fatal("managed marker bypassed by disabled local flag")
	}
	next, err := newConfiguredOutpostRuntime(cfg, "https://core.example")
	if err != nil {
		t.Fatal(err)
	}
	defer next.state.close()
	if next.processEpoch == oldEpoch || next.status(now).AppliedControlGeneration != 0 || next.status(now).AcceptingRequests {
		t.Fatal("restart restored old control or process epoch")
	}
	// A new configuration lease still cannot authorize an old-process control.
	renewed := signedOutpostConfig(t, key, now.Add(time.Millisecond), 1)
	renewed.RuntimeControl = config.RuntimeControl
	if err := next.apply(renewed, "test-key", now.Add(time.Millisecond)); err == nil {
		t.Fatal("old process control accepted after restart")
	}
	renewed.RuntimeControl = signedOutpostControl(key, next, now.Add(2*time.Millisecond), 3, "serve")
	if err := next.apply(renewed, "test-key", now.Add(2*time.Millisecond)); err == nil {
		t.Fatal("control generation floor lost after restart")
	}
	renewed.RuntimeControl = signedOutpostControl(key, next, now.Add(3*time.Millisecond), 5, "serve")
	if err := next.apply(renewed, "test-key", now.Add(3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if !next.status(now.Add(3 * time.Millisecond)).AcceptingRequests {
		t.Fatal("fresh process-bound serve did not recover")
	}
	other := cfg
	other.InstanceID = "pod-2"
	independent, err := newConfiguredOutpostRuntime(other, "https://core.example")
	if err != nil {
		t.Fatal(err)
	}
	independent.state.close()
}

func TestOutpostControlMigratesLegacyFloorAndPersistsBeforeACK(t *testing.T) {
	r, cfg, key := managedOutpostFixture(t)
	r.state.close()
	// Use another identity whose only durable state is the A01 format.
	cfg.AgentID = "legacy-agent"
	now := time.Now().Add(-time.Second)
	initial := signedOutpostConfig(t, key, now.Add(-time.Minute), 5)
	legacy, _, err := openOutpostState(cfg.StateDir, "https://core.example", cfg.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.save(newOutpostConfigFloor(initial)); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(cfg.StateDir, legacy.name)
	legacy.close()
	reader, _, err := openOutpostStateWithLock(cfg.StateDir, "https://core.example", cfg.AgentID, true)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	r, err = newConfiguredOutpostRuntime(cfg, "https://core.example")
	if err != nil {
		t.Fatal(err)
	}
	defer r.state.close()
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatal("legacy floor was deleted", err)
	}
	if r.floor.Version != 5 || !r.floor.Managed {
		t.Fatal("legacy floor not migrated")
	}
	config := signedOutpostConfig(t, key, now, 5)
	config.RuntimeControl = signedOutpostControl(key, r, now, 1, "serve")
	if err := r.apply(config, "test-key", now); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(cfg.StateDir, r.state.name)
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	config.RuntimeControl = signedOutpostControl(key, r, now.Add(time.Millisecond), 2, "drain")
	if err := r.apply(config, "test-key", now.Add(time.Millisecond)); err == nil {
		t.Fatal("control persisted ACK despite failed write")
	}
	if status := r.status(now); status.AcceptingRequests || status.AppliedControlGeneration != 1 {
		t.Fatalf("failed write ACK/gate=%#v", status)
	}
	// A failed fsync/rename may already have advanced disk state. Never overwrite it from stale memory.
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	config.RuntimeControl = signedOutpostControl(key, r, now.Add(2*time.Millisecond), 3, "serve")
	if err := r.apply(config, "test-key", now.Add(2*time.Millisecond)); err == nil {
		t.Fatal("uncertain durable state recovered without restart")
	}
}

func TestOutpostControlClaimAndHeartbeatReportActualState(t *testing.T) {
	runtime, cfg, key := managedOutpostFixture(t)
	var claim sohaapi.IdentityOutpostClaimRequest
	var heartbeat sohaapi.IdentityOutpostHeartbeatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/v1/identity/outposts/runtime/claim":
			if err := json.NewDecoder(req.Body).Decode(&claim); err != nil {
				t.Error(err)
			}
			now := time.Now().UTC().Add(-time.Millisecond)
			config := signedOutpostConfig(t, key, now, 1)
			config.RuntimeControl = signedOutpostControl(key, runtime, now, 7, "drain")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": config})
		case "/api/v1/identity/outposts/outpost-1/heartbeat":
			if err := json.NewDecoder(req.Body).Decode(&heartbeat); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"data":{"accepted":true,"desiredConfigurationVersion":1}}`))
		default:
			w.WriteHeader(204)
		}
	}))
	defer server.Close()
	r := New(cfgpkg.ControlPlaneConfig{BaseURL: server.URL, Outpost: cfg}, zap.NewNop())
	r.outpost = runtime
	r.claimOutpostConfig(context.Background())
	r.heartbeatOutpost(context.Background())
	if claim.InstanceID != runtime.instanceID || claim.ProcessEpoch != runtime.processEpoch || claim.SupportedRuntimeControlVersion != "v1" {
		t.Fatalf("claim metadata=%#v", claim)
	}
	if heartbeat.InstanceID != runtime.instanceID || heartbeat.ProcessEpoch != runtime.processEpoch || heartbeat.AcceptingRequests == nil || *heartbeat.AcceptingRequests || heartbeat.ActiveChecks == nil || *heartbeat.ActiveChecks != 0 || heartbeat.AppliedControlGeneration == nil || *heartbeat.AppliedControlGeneration != 7 {
		t.Fatalf("heartbeat=%#v", heartbeat)
	}
}

func TestOutpostControlCancellationReleasesActiveCheck(t *testing.T) {
	runtime, _, key := managedOutpostFixture(t)
	now := time.Now().Add(-time.Second)
	config := signedOutpostConfig(t, key, now, 1)
	config.RuntimeControl = signedOutpostControl(key, runtime, now, 1, "serve")
	if err := runtime.apply(config, "test-key", now); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		close(entered)
		select {
		case <-req.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	r := New(cfgpkg.ControlPlaneConfig{BaseURL: server.URL, Outpost: cfgpkg.OutpostConfig{Enabled: true}}, zap.NewNop())
	r.outpost = runtime
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.CheckOutpostAccess(ctx, ForwardAuthRequest{RequestHost: "app.example.com", RequestPath: "/private"})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("check did not enter")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled check allowed")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled check did not finish")
	}
	if runtime.status(time.Now()).ActiveChecks != 0 {
		t.Fatal("cancelled check leaked active count")
	}
}

func TestOutpostControlStickyBeforeFirstClaim(t *testing.T) {
	r, cfg, _ := managedOutpostFixture(t)
	r.state.close()
	cfg.RuntimeControlEnabled = false
	if unexpected, err := newConfiguredOutpostRuntime(cfg, "https://core.example"); err == nil {
		unexpected.state.close()
		t.Fatal("unclaimed managed marker was bypassed")
	}
}

func TestOutpostControlConfigurationExpiryStillClosesGate(t *testing.T) {
	r, _, key := managedOutpostFixture(t)
	now := time.Now()
	config := signedOutpostConfig(t, key, now, 1)
	config.ExpiresAt = now.Add(2 * time.Second)
	config.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, outpostConfigPayload(config)))
	config.RuntimeControl = signedOutpostControl(key, r, now, 1, "serve")
	if err := r.apply(config, "test-key", now); err != nil {
		t.Fatal(err)
	}
	if status := r.status(now.Add(3 * time.Second)); status.AcceptingRequests || status.ErrorCode != "configuration_expired" {
		t.Fatalf("control extended expired configuration: %#v", status)
	}
}
