package proxydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
)

type executorTransport func(*http.Request) (*http.Response, error)

func (f executorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func executorResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func testExecutorClient(t *testing.T, transport executorTransport) *HTTPClient {
	t.Helper()
	client, err := NewHTTPClient("http://localhost", "exec", strings.Repeat("x", 32))
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = transport
	return client
}

func TestRateLimitDelay(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, test := range []struct {
		name, header   string
		fallback, want time.Duration
	}{
		{"seconds", "7", time.Second, 7 * time.Second},
		{"date", now.Add(9 * time.Second).Format(http.TimeFormat), time.Second, 9 * time.Second},
		{"zero", "0", time.Second, time.Second},
		{"negative", "-5", time.Second, time.Second},
		{"large", "9223372036854775807", time.Second, time.Minute},
		{"old date", now.Add(-time.Hour).Format(http.TimeFormat), time.Second, time.Second},
		{"far date", now.Add(time.Hour).Format(http.TimeFormat), time.Second, time.Minute},
		{"missing", "", 4 * time.Second, 4 * time.Second},
		{"invalid", "garbage", 8 * time.Second, 8 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := rateLimitDelay(test.header, now, test.fallback); got != test.want {
				t.Fatalf("delay=%v want=%v", got, test.want)
			}
		})
	}
}

func TestHTTP429ClaimStatusCallbackRecoverWithoutWorkerExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slot := domain.Slot{ID: "slot", ProxyID: "edge", ExecutorID: "exec", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", MaxReplicas: 1, Members: []domain.Member{{InstanceID: "edge-0", RuntimeID: "p0", Ordinal: 0}}}
	command := domain.Command{ProxyID: "edge", Slot: slot, Generation: 1, OwnerEpoch: 1, Action: "start", Replicas: 1, Deadline: time.Now().Add(time.Minute)}
	task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), ClaimedByAgentID: "exec", Status: "running", CallbackToken: "callback", Payload: map[string]any{"proxyDeployment": command}, Result: map[string]any{"proxyDeploymentDrain": domain.DrainStatus{Instances: 1, CountKnown: true, Ready: true}}}
	calls := map[string]int{}
	firstBody := map[string]string{}
	completed := false
	client := testExecutorClient(t, func(req *http.Request) (*http.Response, error) {
		path := req.URL.Path
		raw, _ := io.ReadAll(req.Body)
		calls[path]++
		if calls[path] == 1 {
			firstBody[path] = string(raw)
			return executorResponse(http.StatusTooManyRequests, "do not log"), nil
		}
		if calls[path] == 2 && string(raw) != firstBody[path] {
			t.Fatalf("retry changed %s body", path)
		}
		if strings.HasSuffix(path, "/callback") {
			var input delivery.ExecutionCallbackInput
			if err := json.Unmarshal(raw, &input); err != nil {
				t.Fatal(err)
			}
			if input.Status == "completed" {
				completed = true
				cancel()
			}
		}
		raw, _ = json.Marshal(map[string]any{"data": task})
		return executorResponse(http.StatusOK, string(raw)), nil
	})
	waits := 0
	client.retryWait = func(ctx context.Context, delay time.Duration) bool { waits++; return ctx.Err() == nil }
	runtime := &fakeScale{initial: domain.Observation{Replicas: 1, DesiredReplicas: 1, ReadyReplicas: 1}}
	worker, err := NewWorker(client, runtime, "exec", []domain.Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("worker exited: %v", err)
	}
	if !completed || waits != 3 || runtime.writes != 0 {
		t.Fatalf("completed=%v retries=%d writes=%d", completed, waits, runtime.writes)
	}
}

func TestHTTP429RetryWaitHonorsCancellationAndDeadline(t *testing.T) {
	for _, cancellation := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancel"}[cancellation], func(t *testing.T) {
			calls := 0
			var cancel context.CancelFunc
			ctx, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer stop()
			if cancellation {
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
			}
			client := testExecutorClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				response := executorResponse(http.StatusTooManyRequests, "")
				response.Header.Set("Retry-After", "60")
				if cancellation {
					cancel()
				}
				return response, nil
			})
			start := time.Now()
			_, err := client.Claim(ctx)
			expected := context.DeadlineExceeded
			if cancellation {
				expected = context.Canceled
			}
			if !errors.Is(err, expected) || calls != 1 || time.Since(start) > time.Second {
				t.Fatalf("err=%v calls=%d elapsed=%v", err, calls, time.Since(start))
			}
		})
	}
}

func TestHTTPDoesNotReplayUnknownOrAuthoritativeErrors(t *testing.T) {
	for _, code := range []int{0, http.StatusInternalServerError, http.StatusConflict, http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			calls := 0
			client := testExecutorClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				if code == 0 {
					return nil, context.DeadlineExceeded
				}
				return executorResponse(code, "private response body"), nil
			})
			client.retryWait = func(context.Context, time.Duration) bool { t.Fatal("non-429 retried"); return false }
			_, err := client.Callback(context.Background(), delivery.ExecutionCallbackInput{CallbackToken: "private"})
			if err == nil || calls != 1 || strings.Contains(err.Error(), "private") {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

type closeTrackingBody struct{ closed bool }

func (*closeTrackingBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *closeTrackingBody) Close() error           { b.closed = true; return nil }
func TestHTTP429ClosesBodyBeforeBoundedBackoff(t *testing.T) {
	body := &closeTrackingBody{}
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := testExecutorClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		response := executorResponse(http.StatusTooManyRequests, "")
		response.Body = body
		return response, nil
	})
	client.retryWait = func(_ context.Context, delay time.Duration) bool {
		if !body.closed {
			t.Fatal("429 body open during wait")
		}
		want := min(time.Second*time.Duration(1<<min(calls-1, 6)), time.Minute)
		if delay != want {
			t.Fatalf("call=%d delay=%v want=%v", calls, delay, want)
		}
		if calls == 9 {
			cancel()
			return false
		}
		body.closed = false
		return true
	}
	_, err := client.Claim(ctx)
	if !errors.Is(err, context.Canceled) || calls != 9 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestHTTP429BeforeScaleCannotReuseExpiredStopProof(t *testing.T) {
	now := time.Now()
	slot, _, task := confirmTask(now)
	until := now.Add(time.Minute)
	task.Result = map[string]any{"proxyDeploymentDrain": domain.DrainStatus{Instances: 2, CountKnown: true, StopRequired: true, ConfirmUntil: &until}}
	statusCalls := 0
	client := testExecutorClient(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/status") {
			statusCalls++
			if statusCalls == 2 {
				return executorResponse(http.StatusTooManyRequests, ""), nil
			}
		}
		task.Status = "running"
		raw, _ := json.Marshal(map[string]any{"data": task})
		return executorResponse(http.StatusOK, string(raw)), nil
	})
	client.retryWait = func(context.Context, time.Duration) bool { until = time.Now().Add(-time.Second); return true }
	runtime := &fakeScale{initial: domain.Observation{Replicas: 2, DesiredReplicas: 2}}
	worker, err := NewWorker(client, runtime, "exec", []domain.Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	worker.Execute(context.Background(), task)
	if runtime.writes != 0 || statusCalls != 3 {
		t.Fatalf("writes=%d status calls=%d", runtime.writes, statusCalls)
	}
}

func TestHTTP429HeartbeatExhaustedDeadlineNeverScales(t *testing.T) {
	slot, command, task := confirmTask(time.Now())
	command.Deadline = time.Now().Add(30 * time.Millisecond)
	task.Payload = map[string]any{"proxyDeployment": command}
	callbacks := 0
	client := testExecutorClient(t, func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		var input delivery.ExecutionCallbackInput
		_ = json.Unmarshal(raw, &input)
		callbacks++
		if input.Status == "running" {
			response := executorResponse(http.StatusTooManyRequests, "")
			response.Header.Set("Retry-After", "60")
			return response, nil
		}
		raw, _ = json.Marshal(map[string]any{"data": task})
		return executorResponse(http.StatusOK, string(raw)), nil
	})
	runtime := &fakeScale{initial: domain.Observation{Replicas: 2, DesiredReplicas: 2}}
	worker, err := NewWorker(client, runtime, "exec", []domain.Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	worker.Execute(context.Background(), task)
	if runtime.writes != 0 || callbacks != 2 {
		t.Fatalf("writes=%d callbacks=%d", runtime.writes, callbacks)
	}
}
