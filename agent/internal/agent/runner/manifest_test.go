package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"go.uber.org/zap"
)

func TestDecodeManifestPayloadRequiresStableIdentity(t *testing.T) {
	payload, err := decodeManifestPayload(map[string]any{
		"action": "apply", "packageId": "package-1", "generation": float64(4),
		"idempotencyKey": "manifest:deployment-1:4:apply",
	})
	if err != nil {
		t.Fatalf("decodeManifestPayload() error = %v", err)
	}
	if payload.PackageID != "package-1" || payload.Generation != 4 {
		t.Fatalf("payload = %#v", payload)
	}
	if _, err := decodeManifestPayload(map[string]any{"action": "apply", "generation": float64(1)}); err == nil {
		t.Fatal("decodeManifestPayload() error = nil, want missing identity rejection")
	}
}

func TestPublicManifestErrorDoesNotExposeProviderDetails(t *testing.T) {
	secret := "token=super-secret kubeconfig=/private/config"
	if got := publicManifestError(errors.New(secret)); got != "manifest execution failed" {
		t.Fatalf("publicManifestError() = %q, want generic public error", got)
	}
}

type blockingManifestExecutor struct {
	started   chan struct{}
	stopError error
}

func (executor blockingManifestExecutor) ExecuteManifestTask(ctx context.Context, _ sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	close(executor.started)
	<-ctx.Done()
	if executor.stopError != nil {
		return sohaapi.ManifestExecutionTaskResult{}, executor.stopError
	}
	return sohaapi.ManifestExecutionTaskResult{}, ctx.Err()
}

func TestManifestExecutionIsCancelableThroughRuntimeAndControlPlane(t *testing.T) {
	for _, source := range []string{"runtime API", "control plane", "timeout", "stop unconfirmed", "rollout stop unconfirmed"} {
		t.Run(source, func(t *testing.T) {
			task := ExecutionTask{ID: "task-1", CallbackToken: "attempt-1", TaskKind: "manifest_apply", Status: "running", Payload: map[string]any{"action": "apply", "packageId": "package-1", "generation": 1, "idempotencyKey": "key-1"}}
			var remoteCanceled atomic.Bool
			var mu sync.Mutex
			statuses := []string{}
			runner := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://control-plane", DefaultTimeout: 5 * time.Second, CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1}}, zap.NewNop())
			if source == "timeout" {
				runner.cfg.DefaultTimeout = 25 * time.Millisecond
			}
			runner.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				next := task
				if req.Method == http.MethodPost {
					var callback callbackRequest
					if err := json.NewDecoder(req.Body).Decode(&callback); err != nil {
						return nil, err
					}
					mu.Lock()
					statuses = append(statuses, callback.Status)
					mu.Unlock()
					next.Status = callback.Status
				}
				if remoteCanceled.Load() {
					next.Status = "canceled"
				}
				return jsonResponse(t, http.StatusOK, map[string]any{"data": next}), nil
			})}
			started, done := make(chan struct{}), make(chan struct{})
			executor := blockingManifestExecutor{started: started}
			if source == "stop unconfirmed" {
				executor.stopError = resourceruntime.ErrArgoStopUnconfirmed
			}
			if source == "rollout stop unconfirmed" {
				executor.stopError = resourceruntime.ErrRolloutStopUnconfirmed
			}
			runner.manifestExecutor = executor
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			go func() { defer close(done); runner.execute(ctx, task) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("manifest did not start")
			}
			if (source == "runtime API" || source == "stop unconfirmed" || source == "rollout stop unconfirmed") && !runner.CancelActiveTask(task.ID, "stop") {
				t.Fatal("manifest was absent from active tasks")
			}
			if source == "control plane" {
				remoteCanceled.Store(true)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("manifest kept executing after cancellation")
			}
			if len(runner.ListActiveTasks()) != 0 {
				t.Fatal("active manifest task leaked")
			}
			want := "canceled"
			if source == "stop unconfirmed" || source == "rollout stop unconfirmed" {
				want = "failed"
			}
			if source == "timeout" {
				want = "callback_timeout"
			}
			mu.Lock()
			defer mu.Unlock()
			if !containsStatus(statuses, want) || containsStatus(statuses, "completed") {
				t.Fatalf("callbacks = %v, want %s", statuses, want)
			}
		})
	}
}

func TestManifestClaimAdvertisesProtectedProtocol(t *testing.T) {
	runner := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://control-plane"}, zap.NewNop())
	runner.manifestClusterID = "cluster-1"
	runner.manifestExecutor = blockingManifestExecutor{}
	called := false
	runner.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		var claim claimRequest
		if err := json.NewDecoder(req.Body).Decode(&claim); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(claim.ProviderKinds, "manifest_root_cr_v1.cluster-1") || !slices.Contains(claim.ProviderKinds, "manifest_agent_v3.cluster-1") || !slices.Contains(claim.ProviderKinds, "manifest_agent.cluster-1") {
			t.Errorf("claim providers = %v", claim.ProviderKinds)
		}
		return jsonResponse(t, http.StatusNoContent, nil), nil
	})}
	runner.claim(t.Context())
	if !called {
		t.Fatal("no manifest claim")
	}
}

func TestManifestProtocolCannotDropOrDowngradeRootIdentity(t *testing.T) {
	for _, mode := range []string{"protected", "missing-root", "legacy-root", "wrong-cluster", "native"} {
		t.Run(mode, func(t *testing.T) {
			payload := sohaapi.ManifestExecutionTaskPayload{ClusterID: "cluster-1", RootResource: &sohaapi.ManifestRootResource{UID: "root"}}
			provider := resourceruntime.RootCRAgentProviderPrefix + "cluster-1"
			switch mode {
			case "missing-root":
				payload.RootResource = nil
			case "legacy-root":
				provider = "manifest_agent_v3.cluster-1"
			case "wrong-cluster":
				provider = resourceruntime.RootCRAgentProviderPrefix + "cluster-2"
			case "native":
				provider, payload.RootResource = "manifest_agent_v3.cluster-1", nil
			}
			err := validateManifestProtocol(provider, "cluster-1", payload)
			valid := mode == "protected" || mode == "native"
			if valid && err != nil || !valid && err == nil {
				t.Fatalf("protocol boundary: mode=%s error=%v", mode, err)
			}
		})
	}
}

func TestInitializationManifestCannotUseLegacyProvider(t *testing.T) {
	payload := sohaapi.ManifestExecutionTaskPayload{Action: "apply", InitializationPlanID: "first", ClusterID: "one"}
	for _, provider := range []string{"manifest_agent.one", "manifest_agent_v3.one", "manifest_root_cr_v1.one", "manifest_initialize_v1.two"} {
		if validateManifestProtocol(provider, "one", payload) == nil {
			t.Fatalf("unsafe initialization provider %s", provider)
		}
	}
	if err := validateManifestProtocol("manifest_initialize_v1.one", "one", payload); err != nil {
		t.Fatal(err)
	}
	payload.InitializationPlanID = ""
	if validateManifestProtocol("manifest_initialize_v1.one", "one", payload) == nil {
		t.Fatal("dedicated provider accepted missing mode")
	}
}
