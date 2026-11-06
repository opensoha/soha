package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"go.uber.org/zap"
)

type heldHelmExecutor struct{ started, cancelSeen, finish chan struct{} }

func (e heldHelmExecutor) ExecuteHelmDelivery(ctx context.Context, _ sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	close(e.started)
	<-ctx.Done()
	close(e.cancelSeen)
	<-e.finish
	return sohaapi.HelmExecutionTaskResult{Stopped: true, Revision: 2, Status: "failed"}, ctx.Err()
}

func TestHelmCancelWaitsForNativeStop(t *testing.T) {
	payload := sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: sohaapi.HelmDeliverySnapshot{Operation: sohaapi.HelmDeliverySnapshotOperationUpgrade, ClusterID: "one", Namespace: "dev", ReleaseName: "app", DeliveryPlanID: "plan", ApplicationID: "app", ApplicationEnvironmentID: "env", ServiceID: "svc", TargetID: "target"}}
	for _, beforeStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "running", true: "before start"}[beforeStart], func(t *testing.T) {
			task := ExecutionTask{ID: "helm", TaskKind: "helm_apply", ProviderKind: "helm_agent.one", CallbackToken: "attempt", Payload: map[string]any{"helm": payload}}
			r := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://core", AgentID: "agent", BearerToken: "general", CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1}}, zap.NewNop())
			e := heldHelmExecutor{make(chan struct{}), make(chan struct{}), make(chan struct{})}
			r.SetHelmExecutor(e, "one", "cluster-token", []string{"platform.helm_releases.values_update"})
			var mu sync.Mutex
			statuses := []string{}
			r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var callback callbackRequest
				if err := json.NewDecoder(req.Body).Decode(&callback); err != nil {
					return nil, err
				}
				mu.Lock()
				statuses = append(statuses, callback.Status)
				mu.Unlock()
				next := task
				next.Status = callback.Status
				if beforeStart && callback.Status == "running" {
					next.Status = "canceling"
				}
				return jsonResponse(t, http.StatusOK, map[string]any{"data": next}), nil
			})}
			done := make(chan struct{})
			go func() { defer close(done); r.execute(context.Background(), task) }()
			if !beforeStart {
				select {
				case <-e.started:
				case <-time.After(time.Second):
					t.Fatal("native operation did not start")
				}
				if !r.CancelActiveTask(task.ID, "stop") {
					t.Fatal("Helm task was not registered")
				}
				select {
				case <-e.cancelSeen:
				case <-time.After(time.Second):
					t.Fatal("native operation did not receive cancel")
				}
				select {
				case <-done:
					t.Fatal("cancel acknowledged while native operation was still running")
				default:
				}
				mu.Lock()
				premature := containsStatus(statuses, "canceled") || containsStatus(statuses, "completed")
				mu.Unlock()
				if premature {
					t.Fatal("premature terminal callback")
				}
				close(e.finish)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("stopped operation did not finish")
			}
			if beforeStart {
				select {
				case <-e.started:
					t.Fatal("native operation ran after canceling start")
				default:
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if !containsStatus(statuses, "canceled") || containsStatus(statuses, "completed") {
				t.Fatalf("callbacks: %v", statuses)
			}
			if len(r.ListActiveTasks()) != 0 {
				t.Fatal("active Helm task leaked")
			}
		})
	}
}

func TestHelmClaimUsesOnlyItsClusterAndCredential(t *testing.T) {
	r := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://core", AgentID: "agent", BearerToken: "general", ProviderKinds: []string{"ci_agent_runner"}}, zap.NewNop())
	r.SetHelmExecutor(heldHelmExecutor{}, "one", "cluster-token", nil)
	r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var claim claimRequest
		if err := json.NewDecoder(req.Body).Decode(&claim); err != nil {
			return nil, err
		}
		if req.Header.Get("Authorization") != "Bearer cluster-token" || len(claim.ProviderKinds) != 1 || claim.ProviderKinds[0] != "helm_agent.one" {
			t.Fatalf("claim did not use cluster credential/provider")
		}
		return jsonResponse(t, http.StatusAccepted, map[string]any{"data": ExecutionTask{ID: "helm"}}), nil
	})}
	if task, ok := r.claim(context.Background()); !ok || task.ID != "helm" {
		t.Fatal("Helm claim failed")
	}
}

func TestHelmApplyRequiresExplicitOperationAction(t *testing.T) {
	payload := sohaapi.HelmExecutionTaskPayload{
		Action: sohaapi.Apply,
		Snapshot: sohaapi.HelmDeliverySnapshot{
			Operation: sohaapi.HelmDeliverySnapshotOperationRollback, ClusterID: "one", Namespace: "dev", ReleaseName: "app", DeliveryPlanID: "plan", ApplicationID: "app", ApplicationEnvironmentID: "env", ServiceID: "svc", TargetID: "target",
		},
	}
	task := ExecutionTask{ID: "helm", TaskKind: "helm_apply", ProviderKind: "helm_agent.one", CallbackToken: "attempt", Payload: map[string]any{"helm": payload}}
	r := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://core", AgentID: "agent", BearerToken: "general", CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1}}, zap.NewNop())
	r.SetHelmExecutor(heldHelmExecutor{}, "one", "cluster-token", []string{"platform.helm_releases.values_update"})
	var statuses []string
	r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var callback callbackRequest
		if err := json.NewDecoder(req.Body).Decode(&callback); err != nil {
			return nil, err
		}
		statuses = append(statuses, callback.Status)
		return jsonResponse(t, http.StatusOK, map[string]any{"data": task}), nil
	})}
	r.execute(context.Background(), task)
	if len(statuses) != 1 || statuses[0] != "failed" {
		t.Fatalf("unconfigured rollback was not stopped before execution: %v", statuses)
	}
}
