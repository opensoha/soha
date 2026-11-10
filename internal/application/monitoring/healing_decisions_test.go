package monitoring

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type healingDecisionStore struct {
	HealingRunRepository
	mu  sync.Mutex
	run domainalert.HealingRun
}

func (r *healingDecisionStore) GetHealingRun(context.Context, string) (domainalert.HealingRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.run
	item.Result = map[string]any{}
	for k, v := range r.run.Result {
		item.Result[k] = v
	}
	return item, nil
}
func (r *healingDecisionStore) CompareAndSwapHealingRun(_ context.Context, before, after domainalert.HealingRun) (domainalert.HealingRun, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.run
	won := current.ExecutionGeneration == before.ExecutionGeneration && current.ApprovalStatus == before.ApprovalStatus && current.SubmissionStatus == before.SubmissionStatus && current.WorkflowRunID == before.WorkflowRunID && current.UpdatedAt.Equal(before.UpdatedAt)
	if won {
		after.UpdatedAt = time.Now().UTC()
		r.run = after
		current = after
	}
	current.Result = map[string]any{}
	for k, v := range r.run.Result {
		current.Result[k] = v
	}
	return current, won, nil
}

type healingPolicyReader struct {
	HealingPolicyRepository
	policy domainalert.HealingPolicy
}

func (r *healingPolicyReader) GetHealingPolicy(context.Context, string) (domainalert.HealingPolicy, error) {
	return r.policy, nil
}

type healingWorkflowFake struct {
	calls         atomic.Int32
	mu            sync.Mutex
	run           domainworkflow.Run
	missing, fail bool
}

func (w *healingWorkflowFake) ExecuteSystemDAG(_ context.Context, _ domainidentity.Principal, app, _, _ string, _ map[string]any, input domainworkflow.Input, _ map[string]any) (domainworkflow.Run, error) {
	w.calls.Add(1)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return domainworkflow.Run{}, errors.New("private-provider-error")
	}
	w.run = domainworkflow.Run{ID: input.RequestedRunID, ApplicationID: app, Status: "queued"}
	return w.run, nil
}
func (w *healingWorkflowFake) GetSystemRun(context.Context, string) (domainworkflow.Run, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.missing || w.run.ID == "" {
		return domainworkflow.Run{}, apperrors.ErrNotFound
	}
	return w.run, nil
}

func healingDecisionFixture(t *testing.T) (*Service, *healingDecisionStore, *healingPolicyReader, *healingWorkflowFake, domainidentity.Principal) {
	t.Helper()
	event := domainalert.AlertEvent{ID: "event:owned", ClusterID: "cluster:owned", Namespace: "owned", Labels: map[string]string{"workload": "owned"}}
	base := &stubMonitoringCompatRepository{alertEvents: map[string]domainalert.AlertEvent{event.ID: event}}
	service := serviceWithCompatRepository(base)
	service.permissions = monitoringCompatPermissions(appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "approve"), appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "reject"), appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "retry"))
	policy := &healingPolicyReader{policy: domainalert.HealingPolicy{ID: "policy:owned", Name: "Owned", Enabled: true, Definition: map[string]any{"owned": true}}}
	digest, err := healingIntent(policy.policy, event)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	repo := &healingDecisionStore{run: domainalert.HealingRun{ID: "healing-run:owned", EventID: event.ID, PolicyID: policy.policy.ID, Status: "pending_approval", ApprovalStatus: "pending", SubmissionStatus: "not_submitted", ExecutionGeneration: 1, IntentDigest: digest, Result: map[string]any{}, CreatedAt: now, UpdatedAt: now}}
	workflow := &healingWorkflowFake{}
	service.healingPolicies, service.healingRuns, service.workflow = policy, repo, workflow
	return service, repo, policy, workflow, domainidentity.Principal{UserID: "actor", UserName: "Actor", Roles: []string{"ops"}}
}

func TestHealingConcurrentApprovalOnlyDispatchesOriginalGeneration(t *testing.T) {
	s, repo, _, workflow, actor := healingDecisionFixture(t)
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			if _, err := s.ApproveHealingRun(t.Context(), actor, "healing-run:owned", "Approved"); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if workflow.calls.Load() != 1 {
		t.Fatalf("dispatch count=%d", workflow.calls.Load())
	}
	run, _ := repo.GetHealingRun(t.Context(), "healing-run:owned")
	if run.SubmissionStatus != "accepted" || run.WorkflowRunID == "" {
		t.Fatalf("missing original fence: %+v", run)
	}
	if _, err := s.RejectHealingRun(t.Context(), actor, run.ID, "late"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("late reject=%v", err)
	}
	if _, err := s.RetryHealingRun(t.Context(), actor, run.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("active retry=%v", err)
	}
}

func TestHealingUnknownCannotResubmitOrRetry(t *testing.T) {
	s, repo, _, workflow, actor := healingDecisionFixture(t)
	workflow.fail = true
	first, err := s.ApproveHealingRun(t.Context(), actor, "healing-run:owned", "")
	if err != nil || first.SubmissionStatus != "unknown" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := s.ApproveHealingRun(t.Context(), actor, first.ID, "")
	if err != nil || second.WorkflowRunID != first.WorkflowRunID || workflow.calls.Load() != 1 {
		t.Fatalf("duplicate=%+v err=%v calls=%d", second, err, workflow.calls.Load())
	}
	if _, err := s.RetryHealingRun(t.Context(), actor, first.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unknown retry=%v", err)
	}
	stored, _ := repo.GetHealingRun(t.Context(), first.ID)
	if len(stored.Result) != 2 || stored.Result["executionState"] != "unknown" {
		t.Fatalf("raw error persisted: %+v", stored.Result)
	}
}

func TestHealingIntentChangeRequiresNewPendingGeneration(t *testing.T) {
	s, repo, policy, workflow, actor := healingDecisionFixture(t)
	policy.policy.Definition["owned"] = false
	if _, err := s.ApproveHealingRun(t.Context(), actor, "healing-run:owned", ""); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale intent=%v", err)
	}
	if workflow.calls.Load() != 0 {
		t.Fatal("changed intent dispatched")
	}
	rejected, err := s.RejectHealingRun(t.Context(), actor, "healing-run:owned", "")
	if err != nil {
		t.Fatal(err)
	}
	retried, err := s.RetryHealingRun(t.Context(), actor, rejected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ExecutionGeneration != 2 || retried.ApprovalStatus != "pending" || retried.IntentDigest == rejected.IntentDigest {
		t.Fatalf("new generation=%+v", retried)
	}
	// An old completion cannot overwrite that new pending decision.
	if _, won, err := repo.CompareAndSwapHealingRun(t.Context(), rejected, rejected); err != nil || won {
		t.Fatalf("stale completion won=%t err=%v", won, err)
	}
	if _, err := s.ApproveHealingRun(t.Context(), actor, retried.ID, ""); err != nil {
		t.Fatal(err)
	}
	if workflow.calls.Load() != 1 {
		t.Fatal("new generation not dispatched once")
	}
}

func TestHealingRetryRequiresKnownFailedOriginalWorkflow(t *testing.T) {
	s, _, _, workflow, actor := healingDecisionFixture(t)
	first, err := s.ApproveHealingRun(t.Context(), actor, "healing-run:owned", "")
	if err != nil {
		t.Fatal(err)
	}
	workflow.mu.Lock()
	workflow.run.Status = "succeeded"
	workflow.mu.Unlock()
	if _, err := s.RetryHealingRun(t.Context(), actor, first.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("succeeded retry=%v", err)
	}
	workflow.mu.Lock()
	workflow.run.Status = "failed"
	workflow.mu.Unlock()
	retried, err := s.RetryHealingRun(t.Context(), actor, first.ID)
	if err != nil || retried.ExecutionGeneration != 2 {
		t.Fatalf("known failed retry=%+v err=%v", retried, err)
	}
}
