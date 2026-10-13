package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativeTaskOwnerFixture struct {
	store             *nativeLoopStore
	task              domainaigateway.CapabilityTask
	input             domainaigateway.CapabilityTaskInput
	creates, cancels  int
	finds             int
	lostCreateReceipt bool
	denyLookup        bool
}

func (f *nativeTaskOwnerFixture) CreateNativeCapabilityTask(_ context.Context, lease domaincopilot.NativeLease, input domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityTask, error) {
	var cp nativeCheckpoint
	if err := json.Unmarshal(f.store.lease.Checkpoint, &cp); err != nil || len(cp.Tasks) != 1 || cp.Tasks[0].State != "dispatching" || cp.Tasks[0].Digest != nativeTaskDigest(input) || lease.Version != f.store.lease.Version {
		return domainaigateway.CapabilityTask{}, errors.New("task was dispatched before the durable input and fence")
	}
	f.creates++
	f.input = input
	f.task = domainaigateway.CapabilityTask{ID: "child-1", CreatedBy: lease.Run.CreatedBy, CreatedAt: time.Now().UTC(), PlanVersion: 1, Plan: input.Plan, Status: "waiting_approval"}
	if f.lostCreateReceipt {
		return domainaigateway.CapabilityTask{}, context.DeadlineExceeded
	}
	return f.task, nil
}

func (f *nativeTaskOwnerFixture) FindNativeCapabilityTask(_ context.Context, principal domainidentity.Principal, parent domaincopilot.NativeTaskParent, key, digest string) (domainaigateway.CapabilityTask, error) {
	f.finds++
	if f.denyLookup {
		return domainaigateway.CapabilityTask{}, apperrors.ErrAccessDenied
	}
	if f.task.ID == "" {
		return f.task, apperrors.ErrNotFound
	}
	if principal.UserID != parent.OwnerID || parent.RequestHash != f.store.lease.RequestHash || key != f.input.IdempotencyKey || digest != nativeTaskDigest(f.input) {
		return domainaigateway.CapabilityTask{}, apperrors.ErrConflict
	}
	return f.task, nil
}

func (f *nativeTaskOwnerFixture) CancelCapabilityTask(_ context.Context, _ domainidentity.Principal, id string) (domainaigateway.CapabilityTask, error) {
	if f.store.lease.Phase != "cancelling" || id != f.task.ID {
		return f.task, errors.New("cancellation ran before the parent stop fence")
	}
	f.cancels++
	f.task.Status = "canceling"
	return f.task, nil
}

func (f *nativeTaskOwnerFixture) GetCapabilityTask(_ context.Context, principal domainidentity.Principal, id string) (domainaigateway.CapabilityTask, error) {
	if f.denyLookup || id != f.task.ID || principal.UserID != f.task.CreatedBy {
		return domainaigateway.CapabilityTask{}, apperrors.ErrAccessDenied
	}
	return f.task, nil
}

func (g *nativeLoopGateway) ValidateCapabilityPlan(_ context.Context, _ domainidentity.Principal, input domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityPlanValidation, error) {
	return domainaigateway.CapabilityPlanValidation{Valid: true, Digest: nativeTaskDigest(input)}, nil
}

func nativeTaskFixtureTools() []domainaigateway.ToolCapability {
	read := &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}
	return []domainaigateway.ToolCapability{
		{Name: "test.plan", Version: "1", RiskLevel: "read", Execution: read, InputSchema: map[string]any{"type": "object"}},
		{Name: "test.write", Version: "1", RiskLevel: "write", Execution: &domainaigateway.ToolExecutionContract{Mode: "async", Idempotent: true, RecoveryMode: "original-call", IdempotencyKeyField: "idempotencyKey", StatusTool: "test.status", CancelTool: "test.cancel", Checks: []domainaigateway.CapabilityCheckReference{{ToolName: "test.plan", CapabilityVersion: "1", Purpose: "precondition"}, {ToolName: "test.assess", CapabilityVersion: "1", Purpose: "verification"}}}, InputSchema: map[string]any{"type": "object"}},
		{Name: "test.status", Version: "1", RiskLevel: "read", Execution: read, InputSchema: map[string]any{"type": "object"}},
		{Name: "test.cancel", Version: "1", RiskLevel: "write", InputSchema: map[string]any{"type": "object"}},
		{Name: "test.assess", Version: "1", RiskLevel: "read", Execution: read, ProducesAssessment: true, InputSchema: map[string]any{"type": "object"}},
	}
}

func nativeTaskFixturePlan() domainaigateway.CapabilityPlan {
	return domainaigateway.CapabilityPlan{Goal: "Apply and verify the actual write receipt", VerificationSteps: []string{"assess"}, Steps: []domainaigateway.CapabilityPlanStep{
		{ID: "plan"},
		{ID: "write", DependsOn: []string{"plan"}},
		{ID: "assess", DependsOn: []string{"write"}, Bindings: []domainaigateway.CapabilityInputBinding{{StepID: "write", OutputPath: "/operationId", InputPath: "/afterOperationId"}}},
	}}
}

func TestNativeTaskRecoversLostCreationReceiptBeforeCancellation(t *testing.T) {
	service, store, gateway, owner := nativeGovernedTaskFixture(t)
	owner.lostCreateReceipt = true
	gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		return appaigateway.WorkbenchRelayResponse{Usage: map[string]float64{"totalTokens": 10}, ToolCalls: []appaigateway.WorkbenchToolCall{{ID: "request", Name: "harness_request_task", Arguments: nativeTaskFixtureArguments(t)}}}, nil
	}
	for i := 0; i < 2; i++ {
		if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || store.lease.Phase != "cancelling" || owner.creates != 1 || owner.finds == 0 || len(store.final) != 0 {
			t.Fatalf("lost submission was not reconciled safely: phase=%s creates=%d finds=%d err=%v", store.lease.Phase, owner.creates, owner.finds, err)
		}
	}
	owner.task.Status = "canceled"
	if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || store.lease.Run.Status != "failed" || len(store.final) != 1 || owner.creates != 1 {
		t.Fatalf("failed attempt did not wait for confirmed child stop: status=%s err=%v", store.lease.Run.Status, err)
	}
}

func TestNativeTaskInputPinsServerKeysScopeChecksAndRemainingBudget(t *testing.T) {
	service, store, gateway, owner := nativeGovernedTaskFixture(t)
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	store.lease.Run.Scope = domaincopilot.SessionScope{ClusterID: "cluster", Namespace: "payments"}
	firstCP, secondCP := initialNativeCheckpoint(store.lease.Run), initialNativeCheckpoint(store.lease.Run)
	first, err := service.prepareNativeTask(t.Context(), principal, store.lease, &firstCP, nativeTaskFixtureArguments(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.prepareNativeTask(t.Context(), principal, store.lease, &secondCP, nativeTaskFixtureArguments(t))
	if err != nil || first.Input.IdempotencyKey != second.Input.IdempotencyKey || first.Digest != second.Digest || len(first.Input.ScopeConstraints) != 2 || firstCP.Usage.Steps != 3 || firstCP.Usage.ToolCalls != 0 {
		t.Fatalf("unstable key/scope/reservation: first=%+v second=%+v err=%v", first, second, err)
	}
	if key, ok := first.Input.Plan.Steps[1].Call.Input["idempotencyKey"].(string); !ok || !strings.HasPrefix(key, "native:") || len(key) > 128 {
		t.Fatalf("domain key was not server-owned and bounded: %v", key)
	}
	for _, scenario := range []string{"budget", "identity_field", "missing_precondition", "unrelated_verification", "changed_cancel", "legacy", "missing_request_hash"} {
		t.Run(scenario, func(t *testing.T) {
			assertNativeTaskPreparationRejected(t, scenario, service, store, gateway, owner, principal)
		})
	}
}

func nativeTaskFixtureArguments(t *testing.T) json.RawMessage {
	t.Helper()
	plan := nativeTaskFixturePlan()
	for i, name := range []string{"test.plan", "test.write", "test.assess"} {
		plan.Steps[i].Call.ToolName, plan.Steps[i].Call.CapabilityVersion, plan.Steps[i].Call.Input = name, "1", map[string]any{}
	}
	data, err := json.Marshal(map[string]any{"plan": plan})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func nativeGovernedTaskFixture(t *testing.T) (*Service, *nativeLoopStore, *nativeLoopGateway, *nativeTaskOwnerFixture) {
	t.Helper()
	service, store, gateway, _ := nativeLoopFixture(t)
	owner := &nativeTaskOwnerFixture{store: store}
	service.nativeTasks = owner
	gateway.catalog = nativeTaskFixtureTools()
	_, catalog, err := service.freezeNativeCatalog(t.Context(), domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}, "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.lease.Run.Input["_sohaNativeCatalog"] = catalog
	store.lease.Run.ToolBindings = appendNativeTaskBindings(nil, catalog.Tasks)
	return service, store, gateway, owner
}

func TestNativeTaskWaitsForOriginalApprovalAndFreshAssessmentAcrossRecovery(t *testing.T) {
	service, store, gateway, owner := nativeGovernedTaskFixture(t)
	models := 0
	gateway.model = func(_ context.Context, request appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		models++
		response := appaigateway.WorkbenchRelayResponse{Usage: map[string]float64{"totalTokens": 10}}
		if models == 1 {
			response.ToolCalls = []appaigateway.WorkbenchToolCall{{ID: "request", Name: "harness_request_task", Arguments: nativeTaskFixtureArguments(t)}}
		} else {
			last := request.Messages[len(request.Messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, `"goalVerified":true`) {
				t.Fatal("model received submission instead of verified domain evidence")
			}
			response.Content = "The domain verified the change."
		}
		return response, nil
	}
	if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || store.lease.Phase != "waiting_approval" || len(store.final) != 0 || owner.creates != 1 {
		t.Fatalf("approval was bypassed: phase=%s creates=%d err=%v", store.lease.Phase, owner.creates, err)
	}
	store.lease.Fence++ // Core restart takes over the same parent and child.
	if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || owner.creates != 1 || owner.finds != 1 || models != 1 {
		t.Fatalf("recovery resubmitted or resumed model early: creates=%d finds=%d models=%d err=%v", owner.creates, owner.finds, models, err)
	}
	owner.task.Status = "completed"
	owner.task.Assessment = &domainaigateway.CapabilityAssessment{Verdict: "satisfied", Evidence: []domainaigateway.CapabilityEvidence{{Kind: "domain", Source: "test.assess", ObservedAt: time.Now(), Summary: "Actual operation verified"}}}
	if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || store.lease.Run.Status != "completed" || len(store.final) != 1 || models != 2 || owner.creates != 1 {
		t.Fatalf("verified completion failed: status=%s creates=%d models=%d err=%v", store.lease.Run.Status, owner.creates, models, err)
	}
}

func TestNativeTaskCancellationRequiresDomainTerminalConfirmation(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "authorization_revoked"}[revoked], func(t *testing.T) {
			service, store, gateway, owner := nativeGovernedTaskFixture(t)
			gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
				return appaigateway.WorkbenchRelayResponse{Usage: map[string]float64{"totalTokens": 10}, ToolCalls: []appaigateway.WorkbenchToolCall{{ID: "request", Name: "harness_request_task", Arguments: nativeTaskFixtureArguments(t)}}}, nil
			}
			if err := service.processNativeRun(t.Context(), store, store.lease); err != nil {
				t.Fatal(err)
			}
			store.lease.CancelRequested = true
			owner.denyLookup = revoked
			if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || store.lease.Phase != "cancelling" || store.lease.Run.Status != "running" || len(store.final) != 0 {
				t.Fatalf("cancel acknowledgement became a false terminal: %+v err=%v", store.lease, err)
			}
			if revoked {
				if owner.cancels != 0 {
					t.Fatal("revoked authorization was bypassed for cancellation")
				}
				return
			}
			owner.task.Status = "canceled"
			if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || store.lease.Run.Status != "canceled" || len(store.final) != 1 || owner.creates != 1 {
				t.Fatalf("confirmed cancellation failed: status=%s err=%v", store.lease.Run.Status, err)
			}
		})
	}
}

func TestNativeTaskDoesNotRedispatchUnknownSubmission(t *testing.T) {
	service, store, _, owner := nativeGovernedTaskFixture(t)
	cp := initialNativeCheckpoint(store.lease.Run)
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	child, err := service.prepareNativeTask(t.Context(), principal, store.lease, &cp, nativeTaskFixtureArguments(t))
	if err != nil {
		t.Fatal(err)
	}
	child.State = "dispatching"
	cp.Tasks = []nativeTaskCheckpoint{child}
	cp.Pending = []nativeCall{{ToolName: "harness.request_task", TaskKey: child.Input.IdempotencyKey, Call: appaigateway.WorkbenchToolCall{ID: "unknown"}}}
	store.lease.Checkpoint, _ = json.Marshal(cp)
	if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || owner.creates != 0 || store.lease.Phase != "paused" || store.lease.Run.Status != "running" {
		t.Fatalf("unknown submission was replayed: creates=%d phase=%s err=%v", owner.creates, store.lease.Phase, err)
	}
}

func TestNativeTaskRejectsUnfreshOrUnconfirmedVerification(t *testing.T) {
	now := time.Now()
	for _, scenario := range []string{"fresh", "old", "future", "incomplete", "unsatisfied", "child_running", "child_unknown", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			task := domainaigateway.CapabilityTask{Status: "completed", CreatedAt: now.Add(-time.Minute), Assessment: &domainaigateway.CapabilityAssessment{Verdict: "satisfied", Evidence: []domainaigateway.CapabilityEvidence{{Kind: "domain", Source: "owner", ObservedAt: now, Summary: "Verified"}}}}
			switch scenario {
			case "old":
				task.Assessment.Evidence[0].ObservedAt = now.Add(-6 * time.Minute)
			case "future":
				task.Assessment.Evidence[0].ObservedAt = now.Add(time.Minute)
			case "incomplete":
				task.Assessment.Evidence[0].Incomplete = true
			case "unsatisfied":
				task.Assessment.Verdict = "unsatisfied"
			case "child_running":
				task.Nodes = []domainaigateway.CapabilityTaskNode{{Invocation: &domainaigateway.ToolInvocationResult{Task: &domainaigateway.CapabilityTaskRef{Terminal: false}}}}
			case "child_unknown":
				task.Nodes = []domainaigateway.CapabilityTaskNode{{Invocation: &domainaigateway.ToolInvocationResult{Task: &domainaigateway.CapabilityTaskRef{ID: "domain-1", Terminal: true, Outcome: "unknown"}}}}
			case "missing":
				task.Assessment = nil
			}
			_, err := nativeTaskVerifiedThrough(task, now)
			if (err == nil) != (scenario == "fresh") {
				t.Fatalf("verification accepted=%t scenario=%s err=%v", err == nil, scenario, err)
			}
		})
	}
}

func TestNativeTaskEvidenceRechecksCurrentOwnerForHistoryRunsAndEvents(t *testing.T) {
	service, store, _, owner := nativeGovernedTaskFixture(t)
	owner.task = domainaigateway.CapabilityTask{ID: "task-1", CreatedBy: "reader", Status: "completed"}
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	message := domaincopilot.Message{Role: "assistant", Content: "Private domain result", Metadata: map[string]any{"capabilityTaskIds": []string{"task-1"}}}
	store.lease.Run.Output["capabilityTaskIds"] = []string{"task-1"}
	store.lease.Run.Output["answer"] = message.Content
	if _, err := service.validateMessageKnowledge(t.Context(), principal, message); err != nil {
		t.Fatal(err)
	}
	owner.denyLookup = true
	if history, withheld := service.groundedHistory(t.Context(), principal, []domaincopilot.Message{message}); !withheld || len(history) != 0 {
		t.Fatal("revoked task evidence entered the next model history")
	}
	if visible := service.visibleKnowledgeMessage(t.Context(), principal, message); strings.Contains(visible.Content, "Private") {
		t.Fatal("revoked task answer remained visible")
	}
	if visible := service.visibleKnowledgeAgentRun(t.Context(), principal, store.lease.Run); strings.Contains(stringValue(visible.Output["answer"]), "Private") {
		t.Fatal("revoked task run remained visible")
	}
	page := domaincopilot.NativeEventPage{NextSequence: 7, Items: []domaincopilot.WorkbenchStreamEvent{{Type: "message.done", Sequence: 7, Content: message.Content}}}
	if visible := service.visibleKnowledgeEvents(t.Context(), principal, store.lease.Run, page); visible.NextSequence != 7 || len(visible.Items) != 1 || visible.Items[0].Type != "error" {
		t.Fatal("revoked task events were leaked or lost their cursor")
	}
}

func TestNativeTaskCatalogUsesActualDockerDefinitionsAndFreezesRecoveryControls(t *testing.T) {
	service, store, gateway, _ := nativeGovernedTaskFixture(t)
	gateway.catalog = appaigateway.BuiltinCapabilityProvider{}.Tools()
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	_, snapshot, err := service.freezeNativeCatalog(t.Context(), principal, "session", nil, nil)
	if err != nil || snapshot.Tasks["docker.projects.deploy.trigger"].Version != "1" || snapshot.Tasks["docker.operations.cancel"].Version != "1" {
		t.Fatalf("actual governed Docker chain is unavailable: %v", err)
	}
	store.lease.Run.Input["_sohaNativeCatalog"] = snapshot
	result, err := service.discoverNativeTasks(t.Context(), principal, store.lease.Run, json.RawMessage(`{"query":"docker.projects.deploy.trigger"}`))
	items, _ := result["items"].([]domainaigateway.ToolCapability)
	if err != nil || len(items) != 1 || items[0].Name != "docker.projects.deploy.trigger" || items[0].Execution.CancelTool != "docker.operations.cancel" || len(items[0].Execution.Checks) != 2 {
		t.Fatalf("model could not discover the real governed contract: %+v %v", result, err)
	}
	for i := range gateway.catalog {
		if gateway.catalog[i].Name == "docker.operations.cancel" {
			gateway.catalog[i].Version = "2"
		}
	}
	if _, err := service.currentNativeTaskCatalog(t.Context(), principal, store.lease.Run); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("changed recovery control exceeded frozen catalog: %v", err)
	}
	_, snapshot, err = service.freezeNativeCatalog(t.Context(), principal, "session", nil, nil, []string{"harness.request_task"})
	if err != nil || len(snapshot.Tasks) != 0 {
		t.Fatalf("disabled task control was advertised: %+v %v", snapshot.Tasks, err)
	}
}

func assertNativeTaskPreparationRejected(t *testing.T, scenario string, service *Service, store *nativeLoopStore, gateway *nativeLoopGateway, owner *nativeTaskOwnerFixture, principal domainidentity.Principal) {
	t.Helper()
	cp := initialNativeCheckpoint(store.lease.Run)
	lease := store.lease
	raw := nativeTaskFixtureArguments(t)
	var request map[string]any
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	var planned struct {
		Plan domainaigateway.CapabilityPlan `json:"plan"`
	}
	if err := json.Unmarshal(raw, &planned); err != nil {
		t.Fatal(err)
	}
	switch scenario {
	case "budget":
		cp.Usage.ToolCalls = cp.Budget.MaxToolCalls - 2
	case "identity_field":
		request["aiClientId"] = "forged"
	case "missing_precondition":
		planned.Plan.Steps[1].DependsOn = nil
		request["plan"] = planned.Plan
	case "unrelated_verification":
		planned.Plan.Steps[2].Bindings = nil
		request["plan"] = planned.Plan
	case "changed_cancel":
		gateway.catalog[3].Version = "2"
		defer func() { gateway.catalog[3].Version = "1" }()
	case "legacy":
		delete(lease.Run.Input, "_sohaNativeCatalog")
		defer func() {
			_, catalog, _ := service.freezeNativeCatalog(t.Context(), principal, "session", nil, nil)
			lease.Run.Input["_sohaNativeCatalog"] = catalog
		}()
	case "missing_request_hash":
		lease.RequestHash = ""
	}
	raw, _ = json.Marshal(request)
	if _, err := service.prepareNativeTask(t.Context(), principal, lease, &cp, raw); err == nil || owner.creates != 0 {
		t.Fatalf("invalid Native task accepted: scenario=%s creates=%d err=%v", scenario, owner.creates, err)
	}
}
