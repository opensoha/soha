package copilot

import (
	context "context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	db "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	workflowrepo "github.com/opensoha/soha/internal/repository/workflow"
)

func checkpointTestKeys(t *testing.T) keyring.Ring {
	t.Helper()
	key, err := keyring.NewKey("checkpoint-test", "task115-private-checkpoint-key", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestPrivateNativeCheckpointSealsEveryCopyAndBindsRun(t *testing.T) {
	r := New(nil, checkpointTestKeys(t))
	run := domain.AgentRun{ID: "run-1", CreatedBy: "owner", SessionID: "session-1"}
	raw := json.RawMessage(`{"Messages":[{"Content":"PRIVATE_NATIVE_MARKER","ToolCalls":[{"name":"k8s.pods.exec","arguments":{"argv":["private"]}}]}],"pending":[{"toolName":"k8s.pods.exec"}],"tasks":[{"input":{"integer":9007199254740993}}]}`)
	sealed, err := r.encodeNativeCheckpoint(run, raw)
	if err != nil || strings.Contains(string(sealed), "PRIVATE_NATIVE_MARKER") {
		t.Fatal("checkpoint leaked", err)
	}
	opened, err := r.decodeNativeCheckpoint(run, sealed)
	if err != nil || string(opened) != string(raw) {
		t.Fatal("checkpoint changed", err)
	}
	other := run
	other.ID = "other"
	if _, err := r.decodeNativeCheckpoint(other, sealed); err == nil {
		t.Fatal("checkpoint transplanted")
	}
	if _, err := New(nil).decodeNativeCheckpoint(run, sealed); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := New(nil).encodeNativeCheckpoint(run, raw); err == nil {
		t.Fatal("plaintext fallback")
	}
	if _, err := r.decodeNativeCheckpoint(run, raw); err == nil {
		t.Fatal("unsealed protected checkpoint accepted")
	}
	if _, err := r.decodeNativeCheckpoint(run, []byte(`{"Messages":[{"Content":"about k8s.pods.exec"}]}`)); err != nil {
		t.Fatal("ordinary legacy text was rejected", err)
	}
	for _, name := range []string{"k8s_pods_exec", "k8s_resources_conditional_update"} {
		encoded, _ := json.Marshal(map[string]any{"Messages": []any{map[string]any{"ToolCalls": []any{map[string]any{"Name": name, "Arguments": map[string]any{"private": "PRIVATE_NATIVE_MARKER"}}}}}})
		if _, err := New(nil).encodeNativeCheckpoint(run, encoded); err == nil {
			t.Fatal("model function alias allowed plaintext fallback")
		}
		if _, err := r.decodeNativeCheckpoint(run, encoded); err == nil {
			t.Fatal("unsealed model function alias accepted")
		}
	}
}

func TestPrivateDurableStorageWithPostgres(t *testing.T) {
	_, store, ctx := newCopilotPostgresTestRepository(t)
	keys := checkpointTestKeys(t)
	repo := New(store.DB(), keys)
	workflows := workflowrepo.New(store.DB(), keys)
	now := time.Now().UTC()
	owner := "private-storage-" + uuid.NewString()
	session, err := repo.CreateSession(ctx, domain.Session{ID: uuid.NewString(), CreatedBy: owner, Title: "Private storage test", Metadata: map[string]any{}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	var workflowRun domainworkflow.Run
	t.Run("workflow_intent_prepared_and_revision_ciphertext", func(t *testing.T) {
		privateDurableStorageWithPostgresPhase1(t, &store, &ctx, &keys, &workflows, &now, &owner, &err, &workflowRun)
	})
	t.Run("workflow_revision_cas_retains_sealed_state", func(t *testing.T) { privateDurableStorageWithPostgresPhase2(t, &store, &ctx, &workflows, &workflowRun) })
	run := domain.AgentRun{ID: "agent:" + uuid.NewString(), ProviderID: "internal", ProviderKind: "native", CapabilityID: "general", SessionID: session.ID, CreatedBy: owner, Status: "queued", Input: map[string]any{}, Output: map[string]any{}, CallbackToken: uuid.NewString(), TimeoutSeconds: 600, QueuedAt: now, CreatedAt: now, UpdatedAt: now}
	_, err = repo.CreateNativeRun(ctx, run, domain.Message{ID: run.ID + ":request", SessionID: session.ID, Role: "user", Content: "Synthetic private test", Metadata: map[string]any{}, CreatedAt: now}, "original-native", "request-hash")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repo.ClaimNativeRun(ctx, "private-test-worker")
	if err != nil || lease.Run.ID != run.ID {
		t.Fatal("native claim failed", err)
	}
	checkpoint := json.RawMessage(`{"budgetTokens":100,"Messages":[{"Content":"PRIVATE_PG_MARKER"}],"Pending":[{"Call":{"name":"k8s.pods.exec","arguments":{"argv":["PRIVATE_PG_MARKER"]}}}],"Tasks":[{"Input":{"private":"PRIVATE_PG_MARKER"}}]}`)
	t.Run("native_checkpoint_and_journal_ciphertext", func(t *testing.T) {
		privateDurableStorageWithPostgresPhase3(t, &store, &ctx, &keys, &repo, &owner, &err, &run, &lease, &checkpoint)
	})
	t.Run("native_public_observation_copies_never_store_private_input_or_output", func(t *testing.T) {
		privateDurableStorageWithPostgresPhase4(t, &store, &ctx, &keys, &repo, &now, &owner, &session, &run)
	})
	t.Run("native_resume_reseals_without_losing_budget", func(t *testing.T) {
		privateDurableStorageWithPostgresPhase5(t, &store, &ctx, &repo, &owner, &err, &run, &lease, &checkpoint)
	})
	t.Run("native_missing_key_and_corruption_rejected", func(t *testing.T) { privateDurableStorageWithPostgresPhase6(t, &store, &ctx, &repo, &owner, &run) })
	t.Run("native_personal_cleanup_lookup_is_read_only_and_task_bound", func(t *testing.T) { privateDurableStorageWithPostgresPhase7(t, &store, &ctx, &repo) })
}

func privateDurableStorageWithPostgresPhase1(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedKeys *keyring.Ring, sharedWorkflows **workflowrepo.Repository, sharedNow *time.Time, sharedOwner *string, sharedErr *error, sharedWorkflowRun *domainworkflow.Run) {

	call := domainai.ToolInvocationRequest{ToolName: "k8s.resources.conditional.update", Input: map[string]any{"content": "PRIVATE_PG_MARKER"}}
	intent := domainworkflow.CapabilityIntent{ActorID: (*sharedOwner), Digest: "original-digest", PlanVersion: 1, Deadline: (*sharedNow).Add(time.Hour), Input: domainai.CapabilityTaskInput{IdempotencyKey: "private-original", Plan: domainai.CapabilityPlan{Goal: "Private storage", Steps: []domainai.CapabilityPlanStep{{ID: "step-1", Call: sohaapi.CapabilityCall{ToolName: call.ToolName, Input: call.Input}}}}}}
	(*sharedWorkflowRun), (*sharedErr) = (*sharedWorkflows).CreateCapabilityRun((*sharedCtx), domainworkflow.Run{ID: uuid.NewString(), Scope: domainworkflow.ScopeCapabilityTask, Status: "failed", WorkflowName: "Private storage", Steps: []domainworkflow.Step{}, NodeRuns: []domainworkflow.NodeRun{{NodeID: "step-1", PreparedCall: &call}}, Metadata: map[string]any{"capabilityIntent": intent, "capabilityRevisions": []domainworkflow.CapabilityRevision{{Intent: intent, Nodes: []domainworkflow.NodeRun{{NodeID: "old-step", PreparedCall: &call}}, Version: 1}}}, CreatedAt: (*sharedNow).Format(time.RFC3339), UpdatedAt: (*sharedNow).Format(time.RFC3339)})
	if (*sharedErr) != nil {
		t.Fatal((*sharedErr))
	}
	var raw string
	if err := (*sharedStore).DB().Raw(`SELECT metadata::text FROM workflow_runs WHERE id=?`, (*sharedWorkflowRun).ID).Row().Scan(&raw); err != nil || strings.Contains(raw, "PRIVATE_PG_MARKER") {
		t.Fatal("private workflow state leaked", err)
	}
	loaded, err := workflowrepo.New((*sharedStore).DB(), (*sharedKeys)).Get((*sharedCtx), (*sharedWorkflowRun).ID)
	if err != nil || loaded.NodeRuns[0].PreparedCall.Input["content"] != "PRIVATE_PG_MARKER" {
		t.Fatal("workflow restart lost prepared input", err)
	}
	revisions, err := domainworkflow.CapabilityRevisionsFrom(loaded)
	if err != nil || revisions[0].Nodes[0].PreparedCall.Input["content"] != "PRIVATE_PG_MARKER" {
		t.Fatal("revision input lost", err)
	}
}

func privateDurableStorageWithPostgresPhase2(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedWorkflows **workflowrepo.Repository, sharedWorkflowRun *domainworkflow.Run) {

	updated, err := (*sharedWorkflows).ReviseCapabilityRun((*sharedCtx), (*sharedWorkflowRun).ID, (*sharedWorkflowRun).Version, func(run domainworkflow.Run) (domainworkflow.Run, error) { return run, nil })
	if err != nil || updated.Version <= (*sharedWorkflowRun).Version {
		t.Fatal("revision CAS failed", err)
	}
	var raw string
	if err := (*sharedStore).DB().Raw(`SELECT metadata::text FROM workflow_runs WHERE id=?`, (*sharedWorkflowRun).ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "PRIVATE_PG_MARKER") {
		t.Fatal("revision wrote plaintext")
	}
	if _, err := workflowrepo.New((*sharedStore).DB()).Get((*sharedCtx), (*sharedWorkflowRun).ID); err == nil {
		t.Fatal("workflow key loss accepted")
	}
}

func privateDurableStorageWithPostgresPhase3(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedKeys *keyring.Ring, sharedRepo **Repository, sharedOwner *string, sharedErr *error, sharedRun *domain.AgentRun, sharedLease *domain.NativeLease, sharedCheckpoint *json.RawMessage) {

	(*sharedLease), (*sharedErr) = (*sharedRepo).AdvanceNativeRun((*sharedCtx), (*sharedLease), domain.NativeAdvance{Run: (*sharedLease).Run, Phase: "tools", Checkpoint: (*sharedCheckpoint)})
	if (*sharedErr) != nil {
		t.Fatal((*sharedErr))
	}
	var raw string
	if err := (*sharedStore).DB().Raw(`SELECT native_checkpoint::text FROM ai_agent_runs WHERE id=?`, (*sharedRun).ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "PRIVATE_PG_MARKER") {
		t.Fatal("current checkpoint leaked")
	}
	var journal []string
	if err := (*sharedStore).DB().Raw(`SELECT checkpoint::text FROM ai_agent_run_steps WHERE run_id=?`, (*sharedRun).ID).Scan(&journal).Error; err != nil || len(journal) != 1 || strings.Contains(strings.Join(journal, ""), "PRIVATE_PG_MARKER") {
		t.Fatal("checkpoint journal leaked", err)
	}
	loaded, err := New((*sharedStore).DB(), (*sharedKeys)).ReadNativeLease((*sharedCtx), (*sharedOwner), (*sharedRun).ID)
	if err != nil || !strings.Contains(string(loaded.Checkpoint), "PRIVATE_PG_MARKER") {
		t.Fatal("native restart lost protected input", err)
	}
}

func privateDurableStorageWithPostgresPhase4(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedKeys *keyring.Ring, sharedRepo **Repository, sharedNow *time.Time, sharedOwner *string, sharedSession *domain.Session, sharedRun *domain.AgentRun) {

	const marker = "PRIVATE_PUBLIC_COPY_MARKER"
	projected, err := appaigateway.ProtectedWorkbenchExecution(domain.ToolExecution{ID: "projected-tool", ToolName: "k8s.pods.exec", Status: "success", Input: map[string]any{"execution": map[string]any{"argv": []string{marker}}}, Output: map[string]any{"receipt": map[string]any{"id": "original-execution", "status": "succeeded"}, "outputState": "text", "output": map[string]any{"stdout": marker}}})
	if err != nil {
		t.Fatal(err)
	}
	copyRun := (*sharedRun)
	copyRun.ID = (*sharedRun).ID + ":projection"
	_, err = (*sharedRepo).CreateNativeRun((*sharedCtx), copyRun, domain.Message{ID: copyRun.ID + ":request", SessionID: (*sharedSession).ID, Role: "user", Content: "Synthetic projection test", Metadata: map[string]any{}, CreatedAt: (*sharedNow)}, "projection-native", "projection-hash")
	if err != nil {
		t.Fatal(err)
	}
	copyLease, err := (*sharedRepo).ClaimNativeRun((*sharedCtx), "projection-worker")
	if err != nil || copyLease.Run.ID != copyRun.ID {
		t.Fatal("projection claim failed", err)
	}
	copyLease.Run.Status = "completed"
	copyLease.Run.ToolExecutions = []domain.ToolExecution{projected}
	privateCheckpoint, _ := json.Marshal(map[string]any{"pending": []any{map[string]any{"toolName": "k8s.pods.exec", "input": map[string]any{"argv": []string{marker}}}}, "messages": []any{map[string]any{"content": marker}}})
	event := domain.WorkbenchStreamEvent{Type: "tool.completed", ToolCall: &domain.WorkbenchToolCall{ToolName: projected.ToolName, InputPreview: projected.Input, OutputPreview: projected.Output}}
	message := domain.Message{ID: copyRun.ID + ":reply", SessionID: (*sharedSession).ID, Role: "assistant", Content: "Process receipt available", Metadata: map[string]any{"toolExecutions": []domain.ToolExecution{projected}}, CreatedAt: (*sharedNow)}
	_, err = (*sharedRepo).AdvanceNativeRun((*sharedCtx), copyLease, domain.NativeAdvance{Run: copyLease.Run, Phase: "completed", Checkpoint: privateCheckpoint, Events: []domain.WorkbenchStreamEvent{event}, FinalMessage: &message})
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := (*sharedStore).DB().Raw(`SELECT native_checkpoint::text||tool_executions::text||output::text||(SELECT string_agg(payload::text,'') FROM ai_agent_run_events WHERE run_id=?)||(SELECT metadata::text FROM ai_messages WHERE id=?) FROM ai_agent_runs WHERE id=?`, copyRun.ID, message.ID, copyRun.ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, marker) {
		t.Fatal("private copy reached a public observation column")
	}
	loaded, err := New((*sharedStore).DB(), (*sharedKeys)).ReadNativeLease((*sharedCtx), (*sharedOwner), copyRun.ID)
	if err != nil || !strings.Contains(string(loaded.Checkpoint), marker) {
		t.Fatal("worker could not restore exact sealed input", err)
	}
	public, _ := json.Marshal(loaded.Run)
	if strings.Contains(string(public), marker) || !strings.Contains(string(public), "original-execution") {
		t.Fatal("read projection exposed private input or lost receipt")
	}
}

func privateDurableStorageWithPostgresPhase5(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedRepo **Repository, sharedOwner *string, sharedErr *error, sharedRun *domain.AgentRun, sharedLease *domain.NativeLease, sharedCheckpoint *json.RawMessage) {

	(*sharedLease), (*sharedErr) = (*sharedRepo).AdvanceNativeRun((*sharedCtx), (*sharedLease), domain.NativeAdvance{Run: (*sharedLease).Run, Phase: "paused", StopReason: "waiting_input", Checkpoint: (*sharedCheckpoint), Release: true})
	if (*sharedErr) != nil {
		t.Fatal((*sharedErr))
	}
	if _, err := (*sharedRepo).ResumeNativeRun((*sharedCtx), (*sharedOwner), (*sharedRun).ID, "PRIVATE_RESUME_MARKER"); err != nil {
		t.Fatal(err)
	}
	loaded, err := (*sharedRepo).ReadNativeLease((*sharedCtx), (*sharedOwner), (*sharedRun).ID)
	if err != nil {
		t.Fatal(err)
	}
	var cp map[string]any
	if json.Unmarshal(loaded.Checkpoint, &cp) != nil || cp["budgetTokens"] != float64(100) || cp["userInput"] != "PRIVATE_RESUME_MARKER" {
		t.Fatal("resume changed checkpoint")
	}
	var raw string
	if err := (*sharedStore).DB().Raw(`SELECT native_checkpoint::text FROM ai_agent_runs WHERE id=?`, (*sharedRun).ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "PRIVATE_") {
		t.Fatal("resume wrote plaintext")
	}
}

func privateDurableStorageWithPostgresPhase6(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedRepo **Repository, sharedOwner *string, sharedRun *domain.AgentRun) {

	if _, err := New((*sharedStore).DB()).ReadNativeLease((*sharedCtx), (*sharedOwner), (*sharedRun).ID); err == nil {
		t.Fatal("missing native key accepted")
	}
	if err := (*sharedStore).DB().Exec(`UPDATE ai_agent_runs SET native_checkpoint=jsonb_set(native_checkpoint,'{ciphertext}','"corrupt"') WHERE id=?`, (*sharedRun).ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (*sharedRepo).ReadNativeLease((*sharedCtx), (*sharedOwner), (*sharedRun).ID); err == nil {
		t.Fatal("corrupt checkpoint accepted")
	}
}

func privateDurableStorageWithPostgresPhase7(t *testing.T, sharedStore **db.Store, sharedCtx *context.Context, sharedRepo **Repository) {

	owner := "private-cleanup-" + uuid.NewString()
	taskID := domain.PersonalTaskID("agent:" + uuid.NewString())
	if _, err := (*sharedRepo).FindPersonalTaskEnvironment((*sharedCtx), owner, taskID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("unbound environment: %v", err)
	}
	var count int64
	if err := (*sharedStore).DB().Raw(`SELECT count(*) FROM ai_personal_assistants WHERE created_by=?`, owner).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("lookup created an assistant: %d %v", count, err)
	}
	fixture := personalEnvironmentFixtureWithRepository(t, (*sharedRepo), (*sharedStore).DB(), (*sharedCtx))
	fixture.t = t
	view := fixture.wake(t, owner)
	if err := (*sharedStore).DB().Exec(`UPDATE ai_personal_environments SET active_task_id=? WHERE assistant_id=? AND created_by=?`, taskID, view.AssistantID, owner).Error; err != nil {
		t.Fatal(err)
	}
	item, err := (*sharedRepo).FindPersonalTaskEnvironment((*sharedCtx), owner, taskID)
	if err != nil || item.AssistantID != view.AssistantID || item.ActiveTaskID != taskID {
		t.Fatalf("original lease missing: %v", err)
	}
	for _, pair := range [][2]string{{"other-owner", taskID}, {owner, "other-task"}} {
		if _, err := (*sharedRepo).FindPersonalTaskEnvironment((*sharedCtx), pair[0], pair[1]); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("foreign lease returned: %v", err)
		}
	}
	if _, err := (*sharedRepo).FindPersonalTaskEnvironment((*sharedCtx), "", taskID); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("empty owner accepted: %v", err)
	}
}
