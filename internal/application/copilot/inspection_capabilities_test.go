package copilot

import (
	"context"
	"errors"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type inspectionRuntimeFixture struct {
	principal       domainidentity.Principal
	principalError  error
	token           string
	inputs          []domainai.CapabilityTaskInput
	validationError error
}

func (f *inspectionRuntimeFixture) CurrentPrincipal(context.Context, string) (domainidentity.Principal, error) {
	return f.principal, f.principalError
}
func (f *inspectionRuntimeFixture) CurrentExecutionPrincipal(_ context.Context, _ string, token string) (domainidentity.Principal, error) {
	f.token = token
	return f.principal, f.principalError
}
func (f *inspectionRuntimeFixture) ValidateCapabilityPlan(context.Context, domainidentity.Principal, domainai.CapabilityTaskInput) (domainai.CapabilityPlanValidation, error) {
	return domainai.CapabilityPlanValidation{Valid: f.validationError == nil}, f.validationError
}
func (f *inspectionRuntimeFixture) MaterializeRegisteredCapabilityPlan(_ context.Context, _ domainidentity.Principal, input domainai.CapabilityTaskInput) (domainai.CapabilityTaskInput, error) {
	return input, nil
}
func (f *inspectionRuntimeFixture) CreateCapabilityTask(_ context.Context, _ domainidentity.Principal, input domainai.CapabilityTaskInput) (domainai.CapabilityTask, error) {
	f.inputs = append(f.inputs, input)
	return domainai.CapabilityTask{ID: "original-goal"}, nil
}

func TestInspectionHandoffUsesCurrentCreatorAndOriginalExecutionToken(t *testing.T) {
	service, _ := newInspectionAuthzTestService(map[string][]string{"operator": {appaccess.PermObserveAIInspectionRun}})
	fixture := &inspectionRuntimeFixture{principal: domainidentity.Principal{UserID: "creator", Roles: []string{"operator"}}}
	service.agentPrincipals = fixture
	service.SetInspectionCapabilityRuntime(fixture, fixture)
	task := domain.InspectionTask{ID: "registered", CreatedBy: "creator", ExecutionTokenID: "original-token", Revision: 4, InspectionCapability: domain.InspectionCapability{CapabilityPlan: &sohaapi.CapabilityPlan{Goal: "Verify deployment"}, Trigger: &sohaapi.WorkbenchInspectionTrigger{Kind: "schedule"}, AIClientID: "client", SkillID: "skill"}}
	run := domain.InspectionRun{ID: "receipt", TaskID: task.ID, Status: "queued", Report: map[string]any{"registrationRevision": 4}}
	result, err := service.handoffInspectionRun(context.Background(), task, run)
	if err != nil || result.Status != "handed_off" || result.Report["capabilityTaskId"] != "original-goal" {
		t.Fatalf("handoff: %+v %v", result, err)
	}
	if fixture.token != "original-token" || len(fixture.inputs) != 1 || fixture.inputs[0].IdempotencyKey != "receipt" || fixture.inputs[0].AIClientID != "client" || fixture.inputs[0].SkillID != "skill" {
		t.Fatalf("execution context lost: %+v", fixture)
	}
	for _, scenario := range []string{"revoked-token", "removed-role", "different-subject"} {
		fixture.principal = domainidentity.Principal{UserID: "creator", Roles: []string{"operator"}}
		fixture.principalError = nil
		switch scenario {
		case "revoked-token":
			fixture.principalError = apperrors.ErrUnauthorized
		case "removed-role":
			fixture.principal.Roles = nil
		case "different-subject":
			fixture.principal.UserID = "other"
		}
		if _, err := service.handoffInspectionRun(context.Background(), task, run); err == nil {
			t.Fatalf("%s allowed handoff", scenario)
		}
		if len(fixture.inputs) != 1 {
			t.Fatal("denied identity dispatched")
		}
	}
	service.agentPrincipals = nil
	if _, err := service.handoffInspectionRun(context.Background(), task, run); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("missing resolver not rejected: %v", err)
	}
}

func TestInspectionRegistrationRejectsAmbiguousTriggerAndMissingPlan(t *testing.T) {
	service, _ := newInspectionAuthzTestService(map[string][]string{"operator": {appaccess.PermObserveAIInspectionRun}})
	fixture := &inspectionRuntimeFixture{}
	service.SetInspectionCapabilityRuntime(fixture, fixture)
	principal := domainidentity.Principal{UserID: "creator", Roles: []string{"operator"}}
	for _, scenario := range []string{"valid", "missing-plan", "missing-trigger", "legacy-checks", "zero-interval", "unsupported", "schedule-alert-selector"} {
		input := domain.InspectionTaskInput{IntervalMinutes: 5, InspectionCapability: domain.InspectionCapability{CapabilityPlan: &sohaapi.CapabilityPlan{}, Trigger: &sohaapi.WorkbenchInspectionTrigger{Kind: "schedule"}}}
		switch scenario {
		case "missing-plan":
			input.CapabilityPlan = nil
		case "missing-trigger":
			input.Trigger = nil
		case "legacy-checks":
			input.Checks = []string{"cluster_health"}
		case "zero-interval":
			input.IntervalMinutes = 0
		case "unsupported":
			input.Trigger.Kind = "webhook"
		case "schedule-alert-selector":
			input.Trigger.AlertRuleID = "rule"
		}
		err := service.validateInspectionRegistration(context.Background(), principal, &input)
		if (err == nil) != (scenario == "valid") {
			t.Fatalf("%s: %v", scenario, err)
		}
	}
}

type inspectionRegistrationFixture struct {
	*inspectionAuthzTestRepository
	task domain.InspectionTask
}

func (f *inspectionRegistrationFixture) GetInspectionTask(context.Context, string, string) (domain.InspectionTask, error) {
	return f.task, nil
}
func (f *inspectionRegistrationFixture) CreateInspectionTask(_ context.Context, task domain.InspectionTask) (domain.InspectionTask, error) {
	f.task = task
	return task, nil
}
func (f *inspectionRegistrationFixture) UpdateInspectionTask(_ context.Context, _, _ string, input domain.InspectionTaskInput) (domain.InspectionTask, error) {
	f.task.InspectionCapability = input.InspectionCapability
	f.task.Checks = input.Checks
	f.task.Enabled = input.Enabled
	f.task.Revision++
	return f.task, nil
}

func TestInspectionCapabilityCRUDPreservesDisabledEditsAndDoesNotAddLegacyChecks(t *testing.T) {
	service, repo := newInspectionAuthzTestService(map[string][]string{"operator": {appaccess.PermObserveAIInspectionRun, appaccess.ManagedActionPermission(appaccess.PermObserveAIInspectionManage, "create"), appaccess.ManagedActionPermission(appaccess.PermObserveAIInspectionManage, "update")}})
	store := &inspectionRegistrationFixture{inspectionAuthzTestRepository: repo}
	service.inspectionTasks = store
	runtime := &inspectionRuntimeFixture{}
	service.SetInspectionCapabilityRuntime(runtime, runtime)
	principal := domainidentity.Principal{UserID: "creator", Roles: []string{"operator"}}
	input := domain.InspectionTaskInput{ID: "registration", Title: "Check deployment", IntervalMinutes: 1, InspectionCapability: domain.InspectionCapability{CapabilityPlan: &sohaapi.CapabilityPlan{Goal: "original"}, Trigger: &sohaapi.WorkbenchInspectionTrigger{Kind: "schedule"}}}
	created, err := service.CreateInspectionTask(context.Background(), principal, input, "en-US")
	if err != nil || len(created.Checks) != 0 || created.IntervalMinutes != 1 {
		t.Fatalf("create: %+v %v", created, err)
	}
	input.ExpectedRevision = created.Revision
	input.CapabilityPlan = &sohaapi.CapabilityPlan{Goal: "edited while disabled"}
	updated, err := service.UpdateInspectionTask(context.Background(), principal, created.ID, input, "en-US")
	if err != nil || updated.CapabilityPlan.Goal != "edited while disabled" || len(updated.Checks) != 0 {
		t.Fatalf("disabled edit: %+v %v", updated, err)
	}
	runtime.validationError = apperrors.ErrAccessDenied
	input.ExpectedRevision = updated.Revision
	input.CapabilityPlan = &sohaapi.CapabilityPlan{Goal: "denied edit"}
	if _, err := service.UpdateInspectionTask(context.Background(), principal, created.ID, input, "en-US"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("changed disabled plan bypassed validation: %v", err)
	}
	input.CapabilityPlan = nil
	updated, err = service.UpdateInspectionTask(context.Background(), principal, created.ID, input, "en-US")
	if err != nil || updated.Enabled || updated.CapabilityPlan.Goal != "edited while disabled" {
		t.Fatalf("disable after tool permission removed: %+v %v", updated, err)
	}
}
