package networkproxyruntime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func readinessRunner(enabled bool) *Runner {
	until := time.Now().Add(time.Minute)
	desired := domain.Configuration{Revision: 1, Enabled: enabled, ContentHash: "content", ValidUntil: until}
	e := newEngine(Config{Engine: domain.EngineMihomo}, "")
	e.desired = desired
	return &Runner{engine: e, config: Config{PollInterval: 10 * time.Second}, enabled: enabled, validUntil: until, appliedRevision: 1, seenRevision: 1, attemptReported: true, lastAttempt: &domain.Applied{Revision: 1, Status: "applied", ReadbackHash: desired.Digest()}}
}

func readinessStatus(h *runtimeHealth) int {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return w.Code
}

func TestReadinessSeparatesDisabledAndNonPublicationFromTraffic(t *testing.T) {
	r := readinessRunner(false)
	r.publishReadiness(false, time.Now())
	if readinessStatus(&r.health) != http.StatusOK || r.enabled || r.ingress != nil {
		t.Fatal("disabled acknowledged configuration is not independently ready")
	}
	r = readinessRunner(true)
	r.publishReadiness(true, time.Now())
	if readinessStatus(&r.health) != http.StatusOK || r.ingress != nil {
		t.Fatal("non-publication process readiness depends on traffic ingress")
	}
	r.publishReadiness(false, time.Now())
	if readinessStatus(&r.health) != 503 {
		t.Fatal("unhealthy enabled engine is ready")
	}
	r = readinessRunner(false)
	r.attemptReported = false
	r.publishReadiness(false, time.Now())
	if readinessStatus(&r.health) != 503 {
		t.Fatal("unacknowledged configuration is ready")
	}
}

func TestReadinessExpiresWithoutRunnerMutationAndRejectsDrain(t *testing.T) {
	r := readinessRunner(false)
	r.validUntil = time.Now().Add(60 * time.Millisecond)
	r.publishReadiness(false, time.Now())
	if readinessStatus(&r.health) != 200 {
		t.Fatal("fresh snapshot unavailable")
	}
	time.Sleep(80 * time.Millisecond)
	if readinessStatus(&r.health) != 503 {
		t.Fatal("expired snapshot remained ready")
	}
	r = readinessRunner(false)
	r.config.ManagedDeployment = true
	r.deployment.grant = &api.NetworkProxyDeploymentRuntimeControl{AcceptingRequests: true, ExpiresAt: time.Now().Add(-time.Second)}
	r.publishReadiness(false, time.Now())
	if readinessStatus(&r.health) != 503 {
		t.Fatal("expired control lease is ready")
	}
	r.deployment.grant.ExpiresAt = time.Now().Add(time.Minute)
	r.deployment.grant.AcceptingRequests = false
	r.publishReadiness(false, time.Now())
	if readinessStatus(&r.health) != 503 {
		t.Fatal("draining runtime is ready")
	}
	r.deployment.grant.AcceptingRequests = true
	r.publishReadiness(false, time.Now())
	if readinessStatus(&r.health) != 200 {
		t.Fatal("valid disabled managed runtime is not ready")
	}
	r.suspended = true
	r.publishReadiness(false, time.Now())
	if readinessStatus(&r.health) != 503 {
		t.Fatal("suspended runtime is ready")
	}
}

func TestHealthListenerIsBoundedMetadataFreeAndOwned(t *testing.T) {
	h := runtimeHealth{}
	if err := h.start(""); err != nil || h.listener != nil {
		t.Fatal("legacy enabled health listener")
	}
	if err := h.start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + h.listener.Addr().String()
	defer h.close()
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 30; j++ {
				h.snapshot.Store(&readinessSnapshot{ready: true, until: time.Now().Add(time.Second)})
				_ = readinessStatus(&h)
				h.invalidate()
			}
		}()
	}
	workers.Wait()
	for _, path := range []string{"/healthz", "/readyz"} {
		response, err := http.Get(endpoint + path)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if len(raw) != 0 {
			t.Fatal("health endpoint exposed metadata")
		}
		if path == "/healthz" && response.StatusCode != 200 {
			t.Fatal("liveness unavailable")
		}
	}
	response, err := http.Post(endpoint+"/healthz", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 405 {
		t.Fatal("health accepted mutation")
	}
	response, err = http.Get(endpoint + "/configs")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("controller path exposed")
	}
	h.close()
	if response, err := http.Get(endpoint + "/healthz"); err == nil {
		_ = response.Body.Close()
		t.Fatal("owned health listener survived close")
	}
}

func TestReadinessTracksOwnedEngineHealth(t *testing.T) {
	config, _ := lifecycleFixture(t, "")
	e := newEngine(config, "fixture-controller-secret")
	defer e.stop()
	desired := lifecycleDesired(1, 7890)
	result := e.apply(context.Background(), desired)
	if result.Status != "applied" {
		t.Fatalf("fixture apply %+v", result)
	}
	r := &Runner{engine: e, enabled: true, validUntil: desired.ValidUntil, appliedRevision: 1, seenRevision: 1, lastAttempt: &result, attemptReported: true}
	r.ensureRunning(context.Background())
	sample, _ := r.observe(context.Background())
	r.publishReadiness(sample.Health == "healthy", time.Now())
	if readinessStatus(&r.health) != 200 {
		t.Fatal("verified owned engine is not ready")
	}
	if err := e.process.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-e.wait:
		e.wait <- err
	case <-time.After(3 * time.Second):
		t.Fatal("owned engine did not exit")
	}
	r.ensureRunning(context.Background())
	if readinessStatus(&r.health) != 503 {
		t.Fatal("exited owned engine remained ready")
	}
}
