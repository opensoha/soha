package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"go.uber.org/zap"
)

func TestExecutionRunnerFakeControlPlaneCompletesWithCallbackRetryAndMetrics(t *testing.T) {
	task := ExecutionTask{
		ID:                       "task-1",
		ApplicationID:            "app-1",
		ApplicationEnvironmentID: "env-1",
		TaskKind:                 "build",
		ProviderKind:             "ci_agent_runner",
		Status:                   "queued",
		CallbackToken:            "callback-token",
		Payload: map[string]any{
			"commands": []any{"printf done"},
		},
	}

	var mu sync.Mutex
	claimCount := 0
	completedAttempts := 0
	statuses := make([]string, 0, 4)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/delivery/execution-tasks/claim":
			if got := r.Header.Get("Authorization"); got != "Bearer runner-token" {
				t.Fatalf("unexpected claim authorization header %q", got)
			}
			mu.Lock()
			defer mu.Unlock()
			claimCount++
			if claimCount == 1 {
				return jsonResponse(t, http.StatusAccepted, map[string]any{"data": task}), nil
			}
			return jsonResponse(t, http.StatusAccepted, map[string]any{"data": ExecutionTask{}}), nil
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/delivery/execution-tasks/task-1/runner-status":
			next := task
			next.Status = "running"
			return jsonResponse(t, http.StatusOK, map[string]any{"data": next}), nil
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/delivery/execution-callbacks":
			var req callbackRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode callback request: %v", err)
			}
			mu.Lock()
			statuses = append(statuses, req.Status)
			if req.Status == "completed" {
				completedAttempts++
				if completedAttempts == 1 {
					mu.Unlock()
					return jsonResponse(t, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": "transient callback failure"}}), nil
				}
			}
			mu.Unlock()
			next := task
			next.Status = req.Status
			return jsonResponse(t, http.StatusAccepted, map[string]any{"data": next}), nil
		default:
			t.Fatalf("unexpected fake control-plane request %s %s", r.Method, r.URL.Path)
			return jsonResponse(t, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "not found"}}), nil
		}
	})

	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:        "http://control-plane",
		BearerToken:    "runner-token",
		ProviderKinds:  []string{"ci_agent_runner"},
		WorkspaceRoot:  filepath.Join(t.TempDir(), "workspace"),
		DefaultTimeout: 5 * time.Second,
		CallbackRetry: cfgpkg.CallbackRetryConfig{
			MaxAttempts: 2,
			Backoff:     time.Millisecond,
		},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}

	claimed, ok := runner.claim(context.Background())
	if !ok {
		t.Fatal("expected fake control plane to return a task")
	}
	runner.execute(context.Background(), claimed)

	mu.Lock()
	defer mu.Unlock()
	if completedAttempts != 2 {
		t.Fatalf("completed callback attempts = %d, want 2", completedAttempts)
	}
	if !containsStatus(statuses, "running") || !containsStatus(statuses, "completed") {
		t.Fatalf("expected running and completed callbacks, got %v", statuses)
	}
	metrics := runner.MetricsSnapshot()
	if metrics.Execution.Claims != 1 || metrics.Execution.Completed != 1 || metrics.Execution.CallbackFailures != 1 || metrics.Execution.FinalCallbacks != 1 {
		t.Fatalf("unexpected execution metrics: %#v", metrics.Execution)
	}
}

func TestRunnerNoContentClaimsAreIdle(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/delivery/execution-tasks/claim", "/api/v1/docker/operations/claim":
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Status:     http.StatusText(http.StatusNoContent),
				Header:     make(http.Header),
				Body:       http.NoBody,
				Request:    r,
			}, nil
		default:
			t.Fatalf("unexpected fake control-plane request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})

	executionRunner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:       "http://control-plane",
		BearerToken:   "runner-token",
		ProviderKinds: []string{"ci_agent_runner"},
	}, zap.NewNop())
	executionRunner.httpClient = &http.Client{Transport: transport}
	if task, ok := executionRunner.claim(context.Background()); ok || task.ID != "" {
		t.Fatalf("execution claim = %#v, %t, want idle miss", task, ok)
	}
	if metrics := executionRunner.MetricsSnapshot().Execution; metrics.Claims != 0 || metrics.ClaimMisses != 1 {
		t.Fatalf("execution claim metrics = %#v, want one miss", metrics)
	}

	dockerRunner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:     "http://control-plane",
		BearerToken: "runner-token",
		Docker: cfgpkg.DockerRunnerConfig{
			OperationKinds: []string{"host_sync"},
		},
	}, zap.NewNop())
	dockerRunner.httpClient = &http.Client{Transport: transport}
	if operation, ok := dockerRunner.claimDockerOperation(context.Background()); ok || operation.ID != "" {
		t.Fatalf("Docker claim = %#v, %t, want idle miss", operation, ok)
	}
	if metrics := dockerRunner.MetricsSnapshot().Docker; metrics.Claims != 0 || metrics.ClaimMisses != 1 {
		t.Fatalf("Docker claim metrics = %#v, want one miss", metrics)
	}
}

func TestDockerOnlyRunnerDoesNotPollExecutionTasks(t *testing.T) {
	dockerClaimed := make(chan struct{}, 1)
	var executionClaims atomic.Int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/docker/operations/claim":
			select {
			case dockerClaimed <- struct{}{}:
			default:
			}
		case "/api/v1/delivery/execution-tasks/claim":
			executionClaims.Add(1)
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Status:     http.StatusText(http.StatusNoContent),
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    r,
		}, nil
	})

	runner := New(cfgpkg.ControlPlaneConfig{
		Enabled: true, BaseURL: "http://control-plane", BearerToken: "host-runtime-token", PollInterval: time.Millisecond,
		Docker: cfgpkg.DockerRunnerConfig{Enabled: true, WorkerID: "host-1", HostIDs: []string{"host-1"}, OperationKinds: []string{"host_sync"}},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)

	select {
	case <-dockerClaimed:
	case <-time.After(time.Second):
		t.Fatal("Docker-only runner did not poll Docker operations")
	}
	cancel()
	if got := executionClaims.Load(); got != 0 {
		t.Fatalf("execution task claims = %d, want 0", got)
	}
}

func TestExecutionRunnerTimeoutSendsFinalStateWithDetachedContext(t *testing.T) {
	task := ExecutionTask{
		ID:            "task-timeout",
		ApplicationID: "app-timeout",
		TaskKind:      "build",
		ProviderKind:  "ci_agent_runner",
		Status:        "queued",
		CallbackToken: "callback-token",
		Payload: map[string]any{
			"commands": []any{"sleep 1"},
			"timeout":  "50ms",
		},
	}

	var mu sync.Mutex
	statuses := make([]string, 0, 2)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/delivery/execution-callbacks":
			var req callbackRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode callback request: %v", err)
			}
			mu.Lock()
			statuses = append(statuses, req.Status)
			mu.Unlock()
			next := task
			next.Status = req.Status
			return jsonResponse(t, http.StatusAccepted, map[string]any{"data": next}), nil
		default:
			t.Fatalf("unexpected fake control-plane request %s %s", r.Method, r.URL.Path)
			return jsonResponse(t, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "not found"}}), nil
		}
	})

	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:       "http://control-plane",
		BearerToken:   "runner-token",
		WorkspaceRoot: filepath.Join(t.TempDir(), "workspace"),
		CallbackRetry: cfgpkg.CallbackRetryConfig{
			MaxAttempts: 2,
			Backoff:     time.Millisecond,
		},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}

	runner.execute(context.Background(), task)

	mu.Lock()
	defer mu.Unlock()
	if !containsStatus(statuses, "callback_timeout") {
		t.Fatalf("expected callback_timeout final callback, got %v", statuses)
	}
	metrics := runner.MetricsSnapshot()
	if metrics.Execution.TimedOut != 1 || metrics.Execution.FinalCallbacks != 1 {
		t.Fatalf("unexpected timeout metrics: %#v", metrics.Execution)
	}
}

func TestRunnerExecutionSlotsRespectMaxConcurrency(t *testing.T) {
	runner := New(cfgpkg.ControlPlaneConfig{MaxConcurrency: 1}, zap.NewNop())
	if !runner.tryAcquireExecutionSlot() {
		t.Fatal("first execution slot acquire failed")
	}
	if runner.tryAcquireExecutionSlot() {
		t.Fatal("second execution slot acquire succeeded, want max concurrency guard")
	}
	runner.releaseExecutionSlot()
	if !runner.tryAcquireExecutionSlot() {
		t.Fatal("execution slot acquire after release failed")
	}
	runner.releaseExecutionSlot()
}

func TestCommandFingerprintIsStableAndDoesNotExposeCommand(t *testing.T) {
	command := "echo token=secret"
	first := commandFingerprint(command)
	second := commandFingerprint("  " + command + "  ")
	if first == "" || first != second {
		t.Fatalf("command fingerprint is not stable: %q %q", first, second)
	}
	if strings.Contains(first, command) || strings.Contains(first, "secret") {
		t.Fatalf("command fingerprint exposed command content: %q", first)
	}
}

func containsStatus(statuses []string, status string) bool {
	for _, item := range statuses {
		if strings.TrimSpace(item) == status {
			return true
		}
	}
	return false
}

func jsonResponse(t *testing.T, status int, payload any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fake control-plane response: %v", err)
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(raw))),
	}
}

func TestAgentRuntimeLoopRunsTwoTasksWithinSharedConcurrencyLimit(t *testing.T) {
	runner := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://control.test", BearerToken: "token", MaxConcurrency: 2, PollInterval: time.Millisecond}, zap.NewNop())
	var claims atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	runner.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/claim") {
			claims.Add(1)
			return jsonResponse(t, http.StatusAccepted, map[string]any{"data": AgentRun{ID: "run", ProviderID: "hermes", CapabilityID: "general", CallbackToken: "token"}}), nil
		}
		started <- struct{}{}
		<-release
		return jsonResponse(t, http.StatusOK, map[string]any{"data": AgentRun{ID: "run", Status: "canceled"}}), nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(release)
	go runner.agentRuntimeLoop(ctx)
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("second agent run could not start while first was active")
		}
	}
	time.Sleep(20 * time.Millisecond)
	if got := claims.Load(); got != 2 {
		t.Fatalf("claimed %d runs with only two execution slots", got)
	}
	cancel()
}
