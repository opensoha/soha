package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deliveryProviderFixture struct {
	DeliveryCapabilityService
	workflow        domainworkflow.DeliveryWorkflow
	batch           domainworkflow.DeliveryBatch
	saves, creates  int
	reads           int
	changedWorkflow *domainworkflow.DeliveryWorkflow
	hostID          string
	changedHostID   string
}

func (f *deliveryProviderFixture) AssessDeliveryBatch(ctx context.Context, _ domainidentity.Principal, input sohaapi.DeliveryBatchAssessmentInput) (sohaapi.DeliveryBatchAssessment, error) {
	if _, err := f.scopedBatch(ctx, f.batch); err != nil {
		return sohaapi.DeliveryBatchAssessment{}, err
	}
	now := time.Now().UTC()
	return sohaapi.DeliveryBatchAssessment{BatchID: input.BatchID, TargetID: input.TargetID, ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "dev", Verdict: "satisfied", Summary: "fresh deployed target satisfies the requested conditions", Evidence: []sohaapi.CapabilityEvidence{{Kind: "runtime_inventory", Source: "fixture.runtime", ObservedAt: now, DataThrough: &now, Summary: "expected revision is healthy"}}, Access: []sohaapi.DeliveryAccessResult{}}, nil
}

func TestDeliveryAssessmentUsesGovernedReadCapabilityWithoutTaskOrApproval(t *testing.T) {
	service, repo, fixture, provider := deliveryCapabilityFixture(t)
	fixture.batch = domainworkflow.DeliveryBatch{ID: "batch", InvocationScopes: []map[string]string{{"applicationId": "app", "serviceId": "svc", "applicationEnvironmentId": "dev", "hostId": "host"}}}
	tool := provider.tools[len(provider.tools)-1]
	if tool.Name != "delivery.batches.assess" || !tool.ProducesAssessment || tool.RequiresApproval || tool.Execution.Mode != "sync" || tool.Execution.TaskKind != "" {
		t.Fatalf("assessment misclassified: %+v", tool)
	}
	input := domainaigateway.ToolInvocationRequest{ToolName: tool.Name, CapabilityVersion: "1", Input: map[string]any{"batchId": "batch", "targetId": "web"}}
	result, err := service.InvokeTool(context.Background(), testPrincipal("admin"), input)
	if err != nil || result.RequiresApproval || result.Task != nil || len(repo.approvalRequests) != 0 {
		t.Fatalf("read assessment created work: %+v %v", result, err)
	}
	assessment := visibleCapabilityAssessment(tool, result.Output)
	if assessment == nil || assessment.Verdict != "satisfied" {
		t.Fatalf("assessment evidence lost: %+v", assessment)
	}
	fixture.batch.PartialView = true
	if _, err := service.InvokeTool(context.Background(), testPrincipal("admin"), input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("partial batch allowed: %v", err)
	}
}

func (f *deliveryProviderFixture) ResolveDeliveryScopes(ctx context.Context, _ domainidentity.Principal, definition domainworkflow.DeliveryWorkflowDefinition) ([]map[string]string, error) {
	var scopes []map[string]string
	for _, target := range definition.Targets {
		scope := map[string]string{"applicationId": target.ApplicationID, "serviceId": target.ServiceID}
		if target.ApplicationEnvironmentID != "" {
			scope["applicationEnvironmentId"] = target.ApplicationEnvironmentID
		}
		if target.ReleaseTargetID != "" {
			scope["releaseTargetId"] = target.ReleaseTargetID
		}
		if f.hostID != "" {
			scope["hostId"] = f.hostID
		}
		scopes = append(scopes, scope)
	}
	return scopes, domainworkflow.CheckDeliveryScopes(ctx, scopes)
}
func (f *deliveryProviderFixture) scopedWorkflow(ctx context.Context, item domainworkflow.DeliveryWorkflow) (domainworkflow.DeliveryWorkflow, error) {
	var err error
	item.InvocationScopes, err = f.ResolveDeliveryScopes(ctx, domainidentity.Principal{}, item.Definition)
	return item, err
}
func (f *deliveryProviderFixture) scopedBatch(ctx context.Context, item domainworkflow.DeliveryBatch) (domainworkflow.DeliveryBatch, error) {
	var err error
	if item.InvocationScopes == nil {
		item.InvocationScopes, err = f.ResolveDeliveryScopes(ctx, domainidentity.Principal{}, item.Definition)
	}
	if err == nil {
		err = domainworkflow.CheckDeliveryScopes(ctx, item.InvocationScopes)
	}
	return item, err
}

func (f *deliveryProviderFixture) FindDeliveryWorkflowCreation(ctx context.Context, _ domainidentity.Principal, _ domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflow, error) {
	if f.saves == 0 {
		return domainworkflow.DeliveryWorkflow{}, apperrors.ErrNotFound
	}
	return f.scopedWorkflow(ctx, f.workflow)
}
func (f *deliveryProviderFixture) PrepareDeliveryWorkflow(_ context.Context, _ domainidentity.Principal, _ string, input domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflowInput, error) {
	return input, nil
}
func (f *deliveryProviderFixture) SaveDeliveryWorkflow(ctx context.Context, _ domainidentity.Principal, _ string, input domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflow, error) {
	if _, err := f.ResolveDeliveryScopes(ctx, domainidentity.Principal{}, input.Definition); err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	f.saves++
	f.workflow = domainworkflow.DeliveryWorkflow{ID: "workflow-1", Version: 1, Definition: input.Definition}
	return f.scopedWorkflow(ctx, f.workflow)
}
func (f *deliveryProviderFixture) GetDeliveryWorkflow(ctx context.Context, _ domainidentity.Principal, _ string) (domainworkflow.DeliveryWorkflow, error) {
	f.reads++
	if f.reads > 1 && f.changedWorkflow != nil {
		return f.scopedWorkflow(ctx, *f.changedWorkflow)
	}
	return f.scopedWorkflow(ctx, f.workflow)
}
func (f *deliveryProviderFixture) PrepareDeliveryBatch(_ context.Context, _ domainidentity.Principal, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryWorkflowDefinition, error) {
	if input.WorkflowID != f.workflow.ID || input.WorkflowVersion != f.workflow.Version {
		return domainworkflow.DeliveryWorkflowDefinition{}, apperrors.ErrConflict
	}
	return f.workflow.Definition, nil
}
func (f *deliveryProviderFixture) CreateDeliveryBatch(ctx context.Context, _ domainidentity.Principal, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error) {
	if f.changedHostID != "" {
		f.hostID = f.changedHostID
	}
	if _, err := f.ResolveDeliveryScopes(ctx, domainidentity.Principal{}, f.workflow.Definition); err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	f.creates++
	f.batch = domainworkflow.DeliveryBatch{ID: "batch-1", Status: "running", WorkflowID: input.WorkflowID, WorkflowVersion: input.WorkflowVersion, Definition: f.workflow.Definition}
	return f.scopedBatch(ctx, f.batch)
}
func (f *deliveryProviderFixture) FindDeliveryBatch(ctx context.Context, _ domainidentity.Principal, _ domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error) {
	if f.creates == 0 {
		return domainworkflow.DeliveryBatch{}, apperrors.ErrNotFound
	}
	return f.scopedBatch(ctx, f.batch)
}
func (f *deliveryProviderFixture) GetDeliveryBatch(ctx context.Context, _ domainidentity.Principal, _ string) (domainworkflow.DeliveryBatch, error) {
	return f.scopedBatch(ctx, f.batch)
}
func (f *deliveryProviderFixture) CancelDeliveryBatch(ctx context.Context, _ domainidentity.Principal, _ string, _ string) (domainworkflow.DeliveryBatch, error) {
	if _, err := f.scopedBatch(ctx, f.batch); err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	f.batch.Status = "canceling"
	return f.scopedBatch(ctx, f.batch)
}

func deliveryCapabilityFixture(t *testing.T) (*Service, *memoryGatewayRepository, *deliveryProviderFixture, *deliveryCapabilityProvider) {
	t.Helper()
	fixture := &deliveryProviderFixture{}
	provider, err := NewDeliveryCapabilityProvider(fixture)
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryGatewayRepository{}
	service := newDeliveryGatewayTestService(repo)
	service.SetCapabilityProviders(provider)
	deliveryProvider, ok := provider.(*deliveryCapabilityProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *deliveryCapabilityProvider", provider)
	}
	return service, repo, fixture, deliveryProvider
}
func deliveryWorkflowInput() map[string]any {
	return map[string]any{"idempotencyKey": "workflow-create-1", "definition": map[string]any{"name": "two apps", "targets": []any{
		map[string]any{"id": "a", "applicationId": "app-a", "serviceId": "service-a", "action": "build"},
		map[string]any{"id": "b", "applicationId": "app-b", "serviceId": "service-b", "action": "build"},
	}}}
}
func TestDeliveryCapabilitiesBindWorkflowAndRecoverBatch(t *testing.T) {
	service, repo, fixture, provider := deliveryCapabilityFixture(t)
	// Source permissions are still checked by the real service; this fixture
	// isolates Gateway approval, typed binding and restart behavior.
	principal := testPrincipal("admin")
	workflowCall := domainaigateway.ToolInvocationRequest{ToolName: "delivery.workflows.create", CapabilityVersion: "1", Input: deliveryWorkflowInput()}
	held, err := service.InvokeTool(context.Background(), principal, workflowCall)
	if err != nil || !held.RequiresApproval || fixture.saves != 0 {
		t.Fatalf("write escaped approval: %+v %v", held, err)
	}
	approved, err := service.ApproveApprovalRequest(context.Background(), principal, repo.approvalRequests[0].ID, domainaigateway.ApprovalDecisionInput{})
	if err != nil || approved.Invocation == nil || fixture.saves != 1 {
		t.Fatalf("create receipt missing: %+v %v", approved, err)
	}
	step := domainaigateway.CapabilityPlanStep{ID: "deliver", DependsOn: []string{"workflow"}, Call: sohaapi.CapabilityCall{ToolName: "delivery.batches.create", CapabilityVersion: "1", Input: map[string]any{"idempotencyKey": "batch-create-1"}}, Bindings: []domainaigateway.CapabilityInputBinding{{InputPath: "/workflowId", StepID: "workflow", OutputPath: "/id"}, {InputPath: "/workflowVersion", StepID: "workflow", OutputPath: "/version"}}}
	plan := domainaigateway.CapabilityPlan{Goal: "create a delivery batch", Steps: []domainaigateway.CapabilityPlanStep{{ID: "workflow", Call: sohaapi.CapabilityCall{ToolName: workflowCall.ToolName, CapabilityVersion: "1", Input: workflowCall.Input}}, step}}
	if !validPlannedCapabilityInput(provider.tools[2], step) {
		t.Fatal("canonical oneOf input rejected pending workflow bindings")
	}
	resolved, err := service.ResolveCapabilityStep(step, plan, map[string]domainaigateway.ToolInvocationResult{"workflow": *approved.Invocation})
	if err != nil || resolved.Input["workflowId"] != "workflow-1" {
		t.Fatalf("binding failed: %+v %v", resolved, err)
	}
	held, err = service.InvokeTool(context.Background(), principal, resolved)
	if err != nil || !held.RequiresApproval || fixture.creates != 0 {
		t.Fatalf("batch escaped approval: %+v %v", held, err)
	}
	approved, err = service.ApproveApprovalRequest(context.Background(), principal, repo.approvalRequests[len(repo.approvalRequests)-1].ID, domainaigateway.ApprovalDecisionInput{})
	if err != nil || approved.Invocation == nil || approved.Invocation.Task == nil || approved.Invocation.Task.ID != "batch-1" {
		t.Fatalf("batch reference missing: %+v %v", approved, err)
	}
	verifyDeliveryCapabilityRecovery(t, repo, fixture, provider, resolved)
}

func verifyDeliveryCapabilityRecovery(t *testing.T, repo *memoryGatewayRepository, fixture *deliveryProviderFixture, provider *deliveryCapabilityProvider, resolved domainaigateway.ToolInvocationRequest) {
	t.Helper()
	principal := testPrincipal("admin")
	recreated := newDeliveryGatewayTestService(repo)
	recreated.SetCapabilityProviders(provider)
	recovered, err := recreated.recoverCapabilityCall(context.Background(), principal, resolved)
	if err != nil || recovered == nil || recovered.Task == nil || fixture.creates != 1 {
		t.Fatalf("recovery repeated dispatch: %+v %v", recovered, err)
	}
	raw, _ := json.Marshal(fixture.batch)
	var persisted map[string]any
	_ = json.Unmarshal(raw, &persisted)
	if ref := provider.TaskReference(provider.tools[2], persisted, persisted); ref == nil || ref.Terminal {
		t.Fatal("persisted approval lost its asynchronous reference")
	}
	fixture.batch.Status = "canceling"
	if ref := provider.TaskReference(provider.tools[4], fixture.batch, fixture.batch); ref == nil || ref.Terminal {
		t.Fatal("cancellation request became terminal")
	}
	fixture.batch.PartialView = true
	if _, _, err := provider.InvokeTool(context.Background(), principal, provider.tools[3], map[string]any{"batchId": "batch-1"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("partial batch became a complete result: %v", err)
	}
}

func TestDeliveryPendingBindingsWithholdInputsUntilResolved(t *testing.T) {
	service, _, fixture, provider := deliveryCapabilityFixture(t)
	pending := domainaigateway.CapabilityPlanStep{ID: "deliver", Call: sohaapi.CapabilityCall{ToolName: "delivery.batches.create", CapabilityVersion: "1", Input: map[string]any{"idempotencyKey": "batch-create-1"}}, Bindings: []domainaigateway.CapabilityInputBinding{{InputPath: "/workflowId", StepID: "workflow", OutputPath: "/id"}}}
	run := domainworkflow.Run{ID: "goal", Scope: domainworkflow.ScopeCapabilityTask, Status: "queued", Metadata: map[string]any{"capabilityIntent": domainworkflow.CapabilityIntent{ActorID: "user-1", Digest: "digest", Deadline: time.Now().Add(time.Hour), Input: domainaigateway.CapabilityTaskInput{Plan: domainaigateway.CapabilityPlan{Goal: "deliver", Steps: []domainaigateway.CapabilityPlanStep{pending}}}}}, NodeRuns: []domainworkflow.NodeRun{{NodeID: "deliver", Status: "pending"}}}
	visible, err := service.VisibleCapabilityTask(context.Background(), testPrincipal("admin"), run)
	if err != nil || len(visible.Plan.Steps[0].Call.Input) != 0 || visible.Nodes[0].Summary == "" {
		t.Fatalf("unbound input was resolved or exposed: %+v %v", visible, err)
	}
	if fixture.creates != 0 || fixture.saves != 0 {
		t.Fatal("view executed effects")
	}
	fixture.batch = domainworkflow.DeliveryBatch{ID: "batch-1", Status: "running", Definition: domainworkflow.DeliveryWorkflowDefinition{Targets: []domainworkflow.DeliveryTargetInput{{ApplicationID: "app-a", ServiceID: "service-a"}}}}
	run.NodeRuns[0].PreparedCall = &domainaigateway.ToolInvocationRequest{ToolName: provider.tools[3].Name, CapabilityVersion: "1", Input: map[string]any{"batchId": "batch-1"}}
	if _, err := service.VisibleCapabilityTask(context.Background(), testPrincipal("admin"), run); err != nil {
		t.Fatalf("resolved target cannot be viewed: %v", err)
	}
}

func newDeliveryGatewayTestService(repo *memoryGatewayRepository) *Service {
	return newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"admin": {appaccess.PermAIGatewayManage, appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayView, appaccess.PermDeliveryWorkflowsView, appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationsUpdate},
	}}), nil, repo)
}

func TestRegisteredProviderCannotOverrideEarlierCatalogOwner(t *testing.T) {
	_, _, _, provider := deliveryCapabilityFixture(t)
	override := &scopeProviderFixture{testCapabilityProvider: testCapabilityProvider{tools: []domainaigateway.ToolCapability{{Name: "docker.operations.get"}}}}
	registry := newCapabilityRegistry(BuiltinCapabilityProvider{}, provider, override)
	tool, _ := registry.ToolByName("docker.operations.get")
	if _, _, handled, err := registry.InvokeTool(context.Background(), testPrincipal("admin"), tool, nil); handled || err != nil || override.calls != 0 {
		t.Fatalf("duplicate provider replaced the catalog owner: %v %v", handled, err)
	}
}

func TestDeliveryReadCannotExposeTargetsChangedAfterAuthorization(t *testing.T) {
	service, _, fixture, _ := deliveryCapabilityFixture(t)
	fixture.workflow = domainworkflow.DeliveryWorkflow{ID: "workflow-1", Definition: domainworkflow.DeliveryWorkflowDefinition{Targets: []domainworkflow.DeliveryTargetInput{{ApplicationID: "app-a", ServiceID: "service-a"}}}}
	fixture.changedWorkflow = &domainworkflow.DeliveryWorkflow{ID: "workflow-1", Definition: domainworkflow.DeliveryWorkflowDefinition{Targets: []domainworkflow.DeliveryTargetInput{{ApplicationID: "app-b", ServiceID: "service-b"}}}}
	result, err := service.InvokeTool(context.Background(), testPrincipal("admin"), domainaigateway.ToolInvocationRequest{ToolName: "delivery.workflows.get", CapabilityVersion: "1", Input: map[string]any{"workflowId": "workflow-1"}})
	if !errors.Is(err, apperrors.ErrConflict) || result.Output != nil {
		t.Fatalf("changed target escaped its scope: %+v %v", result, err)
	}
}

func TestStoppedGoalRecoversSynchronousCreationWithoutRepeatingIt(t *testing.T) {
	service, repo, fixture, provider := deliveryCapabilityFixture(t)
	service.approvals = capabilityApprovalRepositoryFixture{repo}
	principal := testPrincipal("admin")
	call := domainaigateway.ToolInvocationRequest{ToolName: "delivery.workflows.create", CapabilityVersion: "1", Input: deliveryWorkflowInput()}
	if _, _, err := provider.InvokeTool(context.Background(), principal, provider.tools[0], call.Input); err != nil {
		t.Fatal(err)
	}
	node, err := service.recoverCapabilityNodeOnStop(context.Background(), principal, domainworkflow.NodeRun{NodeID: "create", Status: "blocked", PreparedCall: &call, DispatchAttempted: true})
	if err != nil || node.Status != "canceled" || node.Invocation == nil || fixture.saves != 1 {
		t.Fatalf("recovered creation was repeated or lost: %+v %v", node, err)
	}
}

func TestDeliveryBatchRejectsPhysicalTargetChangeBeforeEnqueue(t *testing.T) {
	_, _, fixture, provider := deliveryCapabilityFixture(t)
	fixture.workflow = domainworkflow.DeliveryWorkflow{ID: "workflow", Version: 1, Definition: domainworkflow.DeliveryWorkflowDefinition{Targets: []domainworkflow.DeliveryTargetInput{{ID: "api", ApplicationID: "app", ServiceID: "service", Action: "deploy"}}}}
	fixture.hostID = "approved-host"
	tool := provider.tools[2]
	input := map[string]any{"workflowId": "workflow", "workflowVersion": 1, "idempotencyKey": "physical-target-test"}
	scopes, err := provider.ToolInvocationScopes(context.Background(), testPrincipal("admin"), tool, input)
	if err != nil || scopes[0]["hostId"] != "approved-host" {
		t.Fatalf("physical resource omitted: %+v %v", scopes, err)
	}
	for i := range scopes {
		scopes[i], err = mergeCapabilityScope(capabilityGatewayScope(tool, input), scopes[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.WithValue(context.Background(), capabilityScopeContextKey{}, capabilityScopeContext{tool: tool.Name, scopes: scopes})
	fixture.changedHostID = "unapproved-host"
	if _, _, err := provider.InvokeTool(ctx, testPrincipal("admin"), tool, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("retarget accepted: %v", err)
	}
	if fixture.creates != 0 {
		t.Fatal("retarget was detected only after enqueue")
	}
}
