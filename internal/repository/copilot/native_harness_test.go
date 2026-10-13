package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	workflowrepo "github.com/opensoha/soha/internal/repository/workflow"
)

func TestNativeRunWithPostgres(t *testing.T) {
	repo, store, ctx := newCopilotPostgresTestRepository(t)
	now := time.Now().UTC()
	owner := "native-test-" + uuid.NewString()
	session, err := repo.CreateSession(ctx, domain.Session{ID: uuid.NewString(), Title: "Native test", CreatedBy: owner, Metadata: map[string]any{}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	candidate := func() domain.AgentRun {
		return domain.AgentRun{ID: "agent:" + uuid.NewString(), ProviderID: "internal", ProviderKind: "native", CapabilityID: "general", SessionID: session.ID, CreatedBy: owner, Status: "queued", Input: map[string]any{}, Output: map[string]any{}, CallbackToken: uuid.NewString(), TimeoutSeconds: 600, QueuedAt: now, CreatedAt: now, UpdatedAt: now}
	}
	create := func(key string) domain.AgentRun {
		t.Helper()
		run := candidate()
		saved, err := repo.CreateNativeRun(ctx, run, domain.Message{ID: run.ID + ":request", SessionID: session.ID, Role: "user", Content: "Test", Metadata: map[string]any{}, CreatedAt: now}, key, "request-hash")
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	run := create("first")
	t.Run("idempotent request", func(t *testing.T) {
		nativeRunIdempotentRequest(t, repo, store, ctx, create, candidate, run, session, owner)
	})
	t.Run("owner isolation", func(t *testing.T) { nativeRunOwnerIsolation(t, repo, ctx, owner, run) })
	t.Run("request recovery", func(t *testing.T) { nativeRunRequestRecovery(t, repo, store, ctx, owner, session.ID, run) })
	var lease domain.NativeLease
	var parent domain.NativeTaskParent
	t.Run("lease fencing", func(t *testing.T) { nativeRunLeaseFencing(t, repo, store, ctx, run, &lease) })
	t.Run("durable events", func(t *testing.T) { nativeRunDurableEvents(t, repo, ctx, owner, run, &lease) })
	t.Run("model context journal", func(t *testing.T) { nativeRunModelContextJournal(t, repo, store, ctx, create) })
	t.Run("parent child registration", func(t *testing.T) {
		nativeRunParentChildRegistration(t, repo, store, ctx, owner, session, now, run, create, &lease, &parent)
	})
	t.Run("cancel fence", func(t *testing.T) { nativeRunCancelFence(t, repo, ctx, owner, run, &lease, parent) })
	t.Run("failure stop fence", func(t *testing.T) { nativeRunFailureStopFence(t, repo, ctx, owner, session, create) })
	t.Run("unknown outcome no replay", func(t *testing.T) { nativeRunUnknownOutcomeNoReplay(t, repo, ctx, owner, create) })
	t.Run("input resume gate", func(t *testing.T) { nativeRunInputResumeGate(t, repo, ctx, owner, create) })
	t.Run("atomic result", func(t *testing.T) { nativeRunAtomicResult(t, repo, store, ctx, session, create) })
	t.Run("shared tool budget", func(t *testing.T) { nativeRunSharedToolBudget(t, repo, ctx, owner, create) })
}

func nativeRunModelContextJournal(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, create func(string) domain.AgentRun) {
	t.Helper()
	run := create("model-context-journal")
	lease, err := repo.ClaimNativeRun(ctx, "context-journal-worker")
	if err != nil || lease.Run.ID != run.ID {
		t.Fatalf("journal claim: %v", err)
	}
	stale := lease
	for index := 1; index <= 2; index++ {
		checkpoint := map[string]any{"modelContext": map[string]any{"id": fmt.Sprintf("%s:model:%d", run.ID, index), "requestHash": fmt.Sprintf("request-%d", index), "snapshot": map[string]any{"contentHash": fmt.Sprintf("source-%d", index)}}}
		raw, err := json.Marshal(checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		lease, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "model", Checkpoint: raw})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.AdvanceNativeRun(ctx, stale, domain.NativeAdvance{Run: stale.Run, Phase: "model", Checkpoint: json.RawMessage(`{"modelContext":{"id":"stale"}}`)}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale context revision accepted: %v", err)
	}
	var checkpoints []struct{ Checkpoint []byte }
	if err := store.DB().Raw(`SELECT checkpoint FROM ai_agent_run_steps WHERE run_id=? ORDER BY sequence`, run.ID).Scan(&checkpoints).Error; err != nil || len(checkpoints) != 2 {
		t.Fatalf("context journal entries=%d: %v", len(checkpoints), err)
	}
	for index, entry := range checkpoints {
		var saved struct {
			ModelContext struct {
				ID       string
				Snapshot struct{ ContentHash string }
			}
		}
		if err := json.Unmarshal(entry.Checkpoint, &saved); err != nil || saved.ModelContext.ID != fmt.Sprintf("%s:model:%d", run.ID, index+1) || saved.ModelContext.Snapshot.ContentHash != fmt.Sprintf("source-%d", index+1) {
			t.Fatalf("later checkpoint overwrote original context: %v", err)
		}
	}
	completed := lease.Run
	completed.Status = "completed"
	if _, err := repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: completed, Phase: "completed", Checkpoint: lease.Checkpoint}); err != nil {
		t.Fatal(err)
	}
}

func nativeRunSharedToolBudget(t *testing.T, repo *Repository, ctx context.Context, owner string, create func(string) domain.AgentRun) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	run := create("shared-budget")
	result := repo.db.WithContext(ctx).Exec(`UPDATE ai_agent_runs SET status='running',claimed_by_agent_id='budget-worker',native_version=1,native_fence=1,native_lease_until=now()+interval '30 seconds' WHERE id=? AND status='queued'`, run.ID)
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("prepare budget lease: %v", result.Error)
	}
	lease, err := repo.ReadNativeLease(ctx, owner, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := repo.ReserveNativeToolCall(ctx, lease, domain.NativeWorkToolCallLimit)
	if err != nil || usage.WorkCalls != 1 {
		t.Fatalf("root attempt not persisted: %+v %v", usage, err)
	}
	parent := domain.NativeTaskParent{RunID: run.ID, OwnerID: owner, SessionID: run.SessionID, RequestHash: lease.RequestHash, Deadline: run.QueuedAt.Add(time.Duration(run.TimeoutSeconds) * time.Second)}
	providerFailure := errors.New("owner invocation failed")
	err = repo.WithNativeTaskParent(ctx, parent, false, func(callCtx context.Context) error {
		if err := repo.ReserveNativeChildToolCall(callCtx, parent, false); err != nil {
			return err
		}
		return providerFailure
	})
	if !errors.Is(err, providerFailure) {
		t.Fatalf("child reservation did not complete under the stop lock: %v", err)
	}
	current, err := repo.ReadNativeLease(ctx, owner, run.ID)
	if err != nil || current.ToolUsage.WorkCalls != 2 {
		t.Fatalf("rolled-back owner error refunded an attempt: %+v %v", current.ToolUsage, err)
	}
	nativeRunConcurrentBudgetReservations(t, repo, ctx, parent)
	lease, err = repo.ReadNativeLease(ctx, owner, run.ID)
	if err != nil || lease.ToolUsage.WorkCalls != domain.NativeWorkToolCallLimit {
		t.Fatalf("shared work ceiling changed: %+v %v", lease.ToolUsage, err)
	}
	stale := lease
	stale.Fence--
	if _, err := repo.ReserveNativeToolCall(ctx, stale, domain.NativeWorkToolCallLimit); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale root reserved work: %v", err)
	}
	lease, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "tools", Checkpoint: json.RawMessage(`{"usage":{"toolCalls":0}}`)})
	if err != nil || lease.ToolUsage.WorkCalls != domain.NativeWorkToolCallLimit {
		t.Fatalf("checkpoint replaced the shared counter: %+v %v", lease.ToolUsage, err)
	}
	nativeRunCancellationBudget(t, repo, ctx, owner, run.ID, parent)
}

func nativeRunCancellationBudget(t *testing.T, repo *Repository, ctx context.Context, owner, runID string, parent domain.NativeTaskParent) {
	t.Helper()
	if _, err := repo.RequestNativeCancel(ctx, owner, runID); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := repo.WithNativeTaskParent(ctx, parent, false, func(context.Context) error { called = true; return nil }); !errors.Is(err, apperrors.ErrConflict) || called {
		t.Fatalf("stopped parent dispatched work: %v called=%v", err, called)
	}
	for attempt := 0; attempt < domain.NativeCleanupToolCallLimit; attempt++ {
		if err := repo.WithNativeTaskParent(ctx, parent, true, func(callCtx context.Context) error { return repo.ReserveNativeChildToolCall(callCtx, parent, true) }); err != nil {
			t.Fatalf("work exhaustion prevented cleanup: %v", err)
		}
	}
	if err := repo.WithNativeTaskParent(ctx, parent, true, func(callCtx context.Context) error { return repo.ReserveNativeChildToolCall(callCtx, parent, true) }); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("cleanup reserve was unbounded: %v", err)
	}
	current, err := repo.ReadNativeLease(ctx, owner, runID)
	if err != nil || current.ToolUsage.WorkCalls != domain.NativeWorkToolCallLimit || current.ToolUsage.CleanupCalls != domain.NativeCleanupToolCallLimit {
		t.Fatalf("durable total ceiling changed: %+v %v", current.ToolUsage, err)
	}
}

func nativeRunConcurrentBudgetReservations(t *testing.T, repo *Repository, ctx context.Context, parent domain.NativeTaskParent) {
	t.Helper()
	results := make(chan error, 12)
	for worker := 0; worker < 4; worker++ {
		go func() {
			for attempt := 0; attempt < 3; attempt++ {
				results <- repo.WithNativeTaskParent(ctx, parent, false, func(callCtx context.Context) error { return repo.ReserveNativeChildToolCall(callCtx, parent, false) })
			}
		}()
	}
	accepted := 0
	for result := 0; result < 12; result++ {
		err := <-results
		if err == nil {
			accepted++
		} else if !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("concurrent reservation failed unexpectedly: %v", err)
		}
	}
	if accepted != domain.NativeWorkToolCallLimit-2 {
		t.Fatalf("concurrent callers overspent or lost allowance: %d", accepted)
	}
}

func nativeRunIdempotentRequest(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, create func(string) domain.AgentRun, candidate func() domain.AgentRun, run domain.AgentRun, session domain.Session, owner string) {
	if again := create("first"); again.ID != run.ID {
		t.Fatal("duplicate run")
	}
	other := candidate()
	if _, err := repo.CreateNativeRun(ctx, other, domain.Message{}, "first", "changed-hash"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed request accepted: %v", err)
	}
	var count int64
	if err := store.DB().Raw(`SELECT count(*) FROM ai_messages WHERE session_id=?`, session.ID).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate user message %d %v", count, err)
	}

}

func nativeRunOwnerIsolation(t *testing.T, repo *Repository, ctx context.Context, owner string, run domain.AgentRun) {
	if _, err := repo.ReadNativeLease(ctx, owner+"-other", run.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross-owner read: %v", err)
	}
	if _, err := repo.ClaimAgentRun(ctx, domain.AgentRunClaimInput{AgentID: "legacy", ProviderIDs: []string{"internal"}, Kinds: []string{"native"}}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("external owner claimed Native: %v", err)
	}

}

func nativeRunRequestRecovery(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, owner, session string, run domain.AgentRun) {
	t.Helper()
	for _, test := range []struct{ user, session, key, want string }{
		{owner, session, "first", run.ID},
		{owner + "-other", session, "first", ""},
		{owner, session + "-other", "first", ""},
		{owner, session, "missing", ""},
	} {
		items, err := repo.ListAgentRuns(ctx, domain.AgentRunFilter{CreatedBy: test.user, SessionID: test.session, NativeRequestID: test.key, Limit: 1})
		if err != nil || (test.want == "" && len(items) != 0) || (test.want != "" && (len(items) != 1 || items[0].ID != test.want)) {
			t.Fatalf("request recovery user=%s key=%s: %d runs, %v", test.user, test.key, len(items), err)
		}
	}
	// A read does not create a second request message or advance any execution lease.
	var state struct{ Messages, Fence, Version int64 }
	if err := store.DB().Raw(`SELECT (SELECT count(*) FROM ai_messages WHERE session_id=?) AS messages,native_fence AS fence,native_version AS version FROM ai_agent_runs WHERE id=?`, session, run.ID).Scan(&state).Error; err != nil || state.Messages != 1 || state.Fence != 0 || state.Version != 0 {
		t.Fatalf("lookup changed durable state: %+v %v", state, err)
	}
}

func nativeRunLeaseFencing(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, run domain.AgentRun, leasePtr *domain.NativeLease) {
	lease := *leasePtr
	defer func() { *leasePtr = lease }()

	first, err := repo.ClaimNativeRun(ctx, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.RenewNativeLease(ctx, first); err != nil || !ok {
		t.Fatalf("renew: %v %v", ok, err)
	}
	if _, err := repo.ClaimNativeRun(ctx, "worker-2"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("double claim: %v", err)
	}
	if err := store.DB().Exec(`UPDATE ai_agent_runs SET native_lease_until=now()-interval '1 second' WHERE id=?`, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	lease, err = repo.ClaimNativeRun(ctx, "worker-2")
	if err != nil || lease.Fence <= first.Fence {
		t.Fatalf("reclaim: %+v %v", lease, err)
	}
	if _, err := repo.AdvanceNativeRun(ctx, first, domain.NativeAdvance{Run: first.Run, Phase: "model", Checkpoint: []byte(`{}`), Events: []domain.WorkbenchStreamEvent{{Type: "message.delta", ContentDelta: "stale"}}}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}

}

func nativeRunDurableEvents(t *testing.T, repo *Repository, ctx context.Context, owner string, run domain.AgentRun, leasePtr *domain.NativeLease) {
	lease := *leasePtr
	defer func() { *leasePtr = lease }()

	var err error
	lease, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "tools", Checkpoint: []byte(`{"counter":1}`), Events: []domain.WorkbenchStreamEvent{{Type: "message.delta", Role: "assistant", ContentDelta: "progress"}}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListNativeEvents(ctx, owner, run.ID, 0)
	if err != nil || len(page.Items) != 1 || page.NextSequence != 1 || page.Items[0].ContentDelta != "progress" {
		t.Fatalf("durable page: %+v %v", page, err)
	}
	if page, err = repo.ListNativeEvents(ctx, owner, run.ID, 20); err != nil || !page.ResyncRequired {
		t.Fatalf("invalid cursor: %+v %v", page, err)
	}
	loaded, err := repo.ReadNativeLease(ctx, owner, run.ID)
	if err != nil || string(loaded.Checkpoint) != `{"counter": 1}` {
		t.Fatalf("checkpoint: %s %v", loaded.Checkpoint, err)
	}

}

func nativeRunParentChildRegistration(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, owner string, session domain.Session, now time.Time, run domain.AgentRun, create func(string) domain.AgentRun, leasePtr *domain.NativeLease, parentPtr *domain.NativeTaskParent) {
	lease := *leasePtr
	defer func() { *leasePtr = lease }()

	parent := *parentPtr
	defer func() { *parentPtr = parent }()

	childID := uuid.NewString()
	key := "native-child-" + childID
	register := func(txCtx context.Context, bound domain.NativeTaskParent) error {
		parent = bound
		_, err := workflowrepo.New(store.DB()).CreateCapabilityRun(txCtx, domainworkflow.Run{ID: childID, Scope: domainworkflow.ScopeCapabilityTask, Status: "queued", WorkflowName: "guard proof", CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339), Metadata: map[string]any{"capabilityIntent": domainworkflow.CapabilityIntent{Input: domainaigateway.CapabilityTaskInput{IdempotencyKey: key}, ActorID: owner, Digest: "frozen-plan", NativeParent: &bound, PlanVersion: 1, Deadline: bound.Deadline}}})
		return err
	}
	if err := repo.WithNativeTaskCreation(ctx, lease, func(txCtx context.Context, bound domain.NativeTaskParent) error {
		if err := register(txCtx, bound); err != nil {
			return err
		}
		return apperrors.ErrConflict
	}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("registration failure: %v", err)
	}
	var count int64
	if err := store.DB().Raw(`SELECT count(*) FROM workflow_runs WHERE id=?`, childID).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("child survived registration rollback: %d %v", count, err)
	}
	if err := repo.WithNativeTaskCreation(ctx, lease, register); err != nil {
		t.Fatalf("atomic child registration: %v", err)
	}
	if reason, err := repo.NativeTaskStopReason(ctx, parent); err != nil || reason != "" {
		t.Fatalf("current parent blocked: %s %v", reason, err)
	}
	if err := repo.WithNativeTaskParent(ctx, parent, false, func(boundedCtx context.Context) error {
		deadline, ok := boundedCtx.Deadline()
		if !ok || deadline.After(parent.Deadline) {
			t.Fatal("dispatch exceeded the immutable parent deadline")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	oldLease := lease
	if err := store.DB().Exec(`UPDATE ai_agent_runs SET native_fence=native_fence+1,native_version=native_version+1 WHERE id=?`, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.WithNativeTaskCreation(ctx, oldLease, register); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale parent registered a child: %v", err)
	}
	if reason, err := repo.NativeTaskStopReason(ctx, parent); err != nil || reason != "" {
		t.Fatalf("takeover invalidated the original child identity: %s %v", reason, err)
	}
	var err error
	lease, err = repo.ReadNativeLease(ctx, owner, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []domain.NativeTaskParent{
		{RunID: parent.RunID, OwnerID: owner + "other", SessionID: parent.SessionID, RequestHash: parent.RequestHash, Deadline: parent.Deadline},
		{RunID: parent.RunID, OwnerID: owner, SessionID: "other", RequestHash: parent.RequestHash, Deadline: parent.Deadline},
		{RunID: parent.RunID, OwnerID: owner, SessionID: parent.SessionID, RequestHash: "changed", Deadline: parent.Deadline},
		{RunID: parent.RunID, OwnerID: owner, SessionID: parent.SessionID, RequestHash: parent.RequestHash, Deadline: parent.Deadline.Add(time.Second)},
	} {
		called := false
		if err := repo.WithNativeTaskParent(ctx, bad, true, func(context.Context) error { called = true; return nil }); !errors.Is(err, apperrors.ErrConflict) || called {
			t.Fatalf("forged parent allowed a child control: %+v %v", bad, err)
		}
	}

}

func nativeRunCancelFence(t *testing.T, repo *Repository, ctx context.Context, owner string, run domain.AgentRun, leasePtr *domain.NativeLease, parent domain.NativeTaskParent) {
	lease := *leasePtr
	defer func() { *leasePtr = lease }()

	assertNativeCancelSerialization(t, repo, ctx, parent, owner, run.ID)
	if _, err := repo.RequestNativeCancel(ctx, owner, run.ID); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.RenewNativeLease(ctx, lease); err != nil || ok {
		t.Fatalf("cancel renewal: %v %v", ok, err)
	}
	called := false
	if err := repo.WithNativeTaskParent(ctx, parent, false, func(context.Context) error { called = true; return nil }); !errors.Is(err, apperrors.ErrConflict) || called {
		t.Fatalf("canceled parent allowed dispatch or approval: %v", err)
	}
	if err := repo.WithNativeTaskParent(ctx, parent, true, func(cleanupCtx context.Context) error {
		deadline, bounded := cleanupCtx.Deadline()
		if !bounded || deadline.After(time.Now().Add(30*time.Second)) {
			return errors.New("cancellation reconciliation has no request limit")
		}
		called = true
		return nil
	}); err != nil || !called {
		t.Fatalf("cancellation reconciliation was blocked: %v", err)
	}
	if _, err := repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "tools", Checkpoint: []byte(`{}`)}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("dispatch after cancel: %v", err)
	}
	var err error
	lease, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "cancelling", StopReason: "user_cancelled", Checkpoint: []byte(`{"tasks":[{"state":"observing"}]}`), Release: true})
	if err != nil || lease.Run.Status != "running" || lease.Phase != "cancelling" {
		t.Fatalf("cancel reconciliation could not persist without a false terminal: %+v %v", lease, err)
	}
	canceled := lease.Run
	canceled.Status = "canceled"
	lease, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: canceled, Phase: "canceled", StopReason: "user_cancelled", Checkpoint: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}

}

func nativeRunFailureStopFence(t *testing.T, repo *Repository, ctx context.Context, owner string, session domain.Session, create func(string) domain.AgentRun) {
	other := create("failure-stop")
	lease, err := repo.ClaimNativeRun(ctx, "failure-worker")
	if err != nil || lease.Run.ID != other.ID || lease.RequestHash != "request-hash" {
		t.Fatalf("claim: %+v %v", lease, err)
	}
	parent := domain.NativeTaskParent{RunID: other.ID, OwnerID: owner, SessionID: session.ID, RequestHash: lease.RequestHash, Deadline: other.QueuedAt.Add(600 * time.Second)}
	lease, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "cancelling", StopReason: "policy_denied", Checkpoint: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if reason, err := repo.NativeTaskStopReason(ctx, parent); err != nil || reason != "failure" {
		t.Fatalf("failure did not stop the child: %q %v", reason, err)
	}
	called := false
	if err := repo.WithNativeTaskParent(ctx, parent, false, func(context.Context) error { called = true; return nil }); !errors.Is(err, apperrors.ErrConflict) || called {
		t.Fatalf("failure cleanup allowed primary dispatch: %v", err)
	}
	if err := repo.WithNativeTaskCreation(ctx, lease, func(context.Context, domain.NativeTaskParent) error { called = true; return nil }); !errors.Is(err, apperrors.ErrConflict) || called {
		t.Fatalf("failure cleanup registered a new child: %v", err)
	}
	lease.Run.Status = "failed"
	if _, err := repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "failed", StopReason: "policy_denied", Checkpoint: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}

}

func nativeRunUnknownOutcomeNoReplay(t *testing.T, repo *Repository, ctx context.Context, owner string, create func(string) domain.AgentRun) {
	other := create("unknown")
	lease, err := repo.ClaimNativeRun(ctx, "worker-3")
	if err != nil || lease.Run.ID != other.ID {
		t.Fatalf("claim: %+v %v", lease, err)
	}
	_, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "paused", StopReason: "unknown_outcome", Release: true, Checkpoint: []byte(`{"pending":[{"state":"dispatching"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ResumeNativeRun(ctx, owner, other.ID, "retry"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unknown write replay: %v", err)
	}
	if _, err = repo.ClaimNativeRun(ctx, "worker-4"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("paused run reclaimed: %v", err)
	}

}

func nativeRunInputResumeGate(t *testing.T, repo *Repository, ctx context.Context, owner string, create func(string) domain.AgentRun) {
	other := create("input-resume")
	lease, err := repo.ClaimNativeRun(ctx, "resume-worker")
	if err != nil || lease.Run.ID != other.ID {
		t.Fatalf("claim: %+v %v", lease, err)
	}
	lease.Run.Output["pendingQuestion"] = "Which namespace?"
	lease, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "paused", StopReason: "policy_denied", Release: true, Checkpoint: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ResumeNativeRun(ctx, owner, other.ID, "answer"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("non-input pause was resumed: %v", err)
	}
	lease, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "paused", StopReason: "waiting_input", Release: true, Checkpoint: []byte(`{"usage":{"tokens":10},"budgetTokens":100}`)})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := repo.ResumeNativeRun(ctx, owner, other.ID, "payments")
	if err != nil || resumed.ID != other.ID || resumed.Phase != "preparing" || resumed.StopReason != "" || resumed.Output["pendingQuestion"] != nil {
		t.Fatalf("wrong resumed projection: %+v %v", resumed, err)
	}
	loaded, err := repo.ReadNativeLease(ctx, owner, other.ID)
	var checkpoint map[string]any
	if err != nil || json.Unmarshal(loaded.Checkpoint, &checkpoint) != nil || checkpoint["userInput"] != "payments" || checkpoint["budgetTokens"] != float64(100) {
		t.Fatalf("resume reset budget or input: %s %v", loaded.Checkpoint, err)
	}
	if _, err = repo.RequestNativeCancel(ctx, owner, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ResumeNativeRun(ctx, owner, other.ID, "late answer"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("canceled run accepted input: %v", err)
	}
	lease, err = repo.ClaimNativeRun(ctx, "resume-cancel-worker")
	if err != nil {
		t.Fatal(err)
	}
	lease.Run.Status = "canceled"
	if _, err = repo.AdvanceNativeRun(ctx, lease, domain.NativeAdvance{Run: lease.Run, Phase: "canceled", StopReason: "user_cancelled", Checkpoint: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}

}

func nativeRunAtomicResult(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context, session domain.Session, create func(string) domain.AgentRun) {
	other := create("result")
	lease, err := repo.ClaimNativeRun(ctx, "worker-5")
	if err != nil || lease.Run.ID != other.ID {
		t.Fatalf("claim: %+v %v", lease, err)
	}
	final := lease.Run
	final.Status = "completed"
	message := domain.Message{ID: final.ID + ":reply", SessionID: session.ID, Role: "assistant", Content: "Verified text reply", Metadata: map[string]any{}}
	advance := domain.NativeAdvance{Run: final, Phase: "completed", Checkpoint: []byte(`{}`), FinalMessage: &message, Events: []domain.WorkbenchStreamEvent{{Type: "message.done", Role: "assistant", Content: message.Content}}}
	stale := lease
	stale.Version--
	if _, err := repo.AdvanceNativeRun(ctx, stale, advance); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale completion: %v", err)
	}
	var count int64
	if err := store.DB().Raw(`SELECT count(*) FROM ai_agent_run_events WHERE run_id=?`, final.ID).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("rolled-back events survived")
	}
	if _, err := repo.AdvanceNativeRun(ctx, lease, advance); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdvanceNativeRun(ctx, lease, advance); err == nil {
		t.Fatal("terminal result replayed")
	}
	if err := store.DB().Raw(`SELECT count(*) FROM ai_messages WHERE id=?`, message.ID).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate reply: %d %v", count, err)
	}

}

func assertNativeCancelSerialization(t *testing.T, repo *Repository, ctx context.Context, parent domain.NativeTaskParent, owner, runID string) {
	t.Helper()
	entered, release, dispatchDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	lockCtx, finish := context.WithTimeout(ctx, 5*time.Second)
	defer finish()
	go func() {
		dispatchDone <- repo.WithNativeTaskParent(lockCtx, parent, false, func(context.Context) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-lockCtx.Done():
				return lockCtx.Err()
			}
		})
	}()
	select {
	case <-entered:
	case <-lockCtx.Done():
		t.Fatal("parent dispatch lock was not acquired")
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, blockedErr := repo.RequestNativeCancel(cancelCtx, owner, runID)
	deadlineErr := cancelCtx.Err()
	cancel()
	close(release)
	if err := <-dispatchDone; err != nil {
		t.Fatalf("serialized dispatch failed: %v", err)
	}
	if blockedErr == nil || !errors.Is(deadlineErr, context.DeadlineExceeded) {
		t.Fatalf("cancel crossed an active dispatch/approval fence: %v %v", blockedErr, deadlineErr)
	}
}
