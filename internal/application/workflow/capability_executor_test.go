package workflow

import (
	"context"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type capabilityExecutorRepository struct{ *deliveryExecutorRepository }

func (r *capabilityExecutorRepository) ReviseCapabilityRun(_ context.Context, id string, version int64, revise func(domainworkflow.Run) (domainworkflow.Run, error)) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.run.ID != id || r.run.Version != version || !capabilityNodeTerminal(r.run.Status) {
		return r.run, apperrors.ErrConflict
	}
	run, err := revise(r.run)
	if err != nil {
		return r.run, err
	}
	run.Version++
	run.FencingToken++
	run.Status, run.StopReason, run.StopSummary, run.LeaseOwner = "queued", "", "", ""
	r.run = run
	return r.run, nil
}

func (r *capabilityExecutorRepository) CreateCapabilityRun(_ context.Context, run domainworkflow.Run) (domainworkflow.Run, error) {
	r.run = run
	r.run.Version = 1
	return r.run, nil
}

func (r *capabilityExecutorRepository) ListCapabilityRuns(context.Context, string, int) ([]domainworkflow.Run, error) {
	return []domainworkflow.Run{r.run}, nil
}

func (r *capabilityExecutorRepository) UpdateCapabilityNode(_ context.Context, expected domainworkflow.Run, id string, advance func(domainworkflow.Run, domainworkflow.NodeRun) (domainworkflow.NodeRun, error)) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !sameDeliveryLease(expected, r.run) {
		return r.run, apperrors.ErrConflict
	}
	for i, node := range r.run.NodeRuns {
		if node.NodeID == id {
			updated, err := advance(r.run, node)
			if err != nil {
				return r.run, err
			}
			r.run.NodeRuns[i] = updated
			r.run.Version++
			return r.run, nil
		}
	}
	return r.run, apperrors.ErrNotFound
}

type capabilityFixtureProvider struct {
	calls   []string
	verdict string
	revoked bool
}

func (*capabilityFixtureProvider) Resources() []domainaigateway.ResourceCapability { return nil }
func (*capabilityFixtureProvider) Prompts() []domainaigateway.PromptCapability     { return nil }
func (*capabilityFixtureProvider) Skills() []domainaigateway.SkillCapability       { return nil }
func (*capabilityFixtureProvider) Tools() []domainaigateway.ToolCapability {
	return []domainaigateway.ToolCapability{
		{Name: "fixture.resource.get", Version: "1", RiskLevel: "read", Execution: &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}, InputSchema: map[string]any{"type": "object"}, OutputSemantics: []domainaigateway.CapabilityValueSemantic{{Path: "/id", Kind: "fixture.resource"}}},
		{Name: "fixture.resource.assess", Version: "1", RiskLevel: "read", Execution: &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}, ProducesAssessment: true, InputSchema: map[string]any{"type": "object", "required": []string{"resourceId"}, "properties": map[string]any{"resourceId": map[string]any{"type": "string"}}}, InputSemantics: []domainaigateway.CapabilityValueSemantic{{Path: "/resourceId", Kind: "fixture.resource"}}},
	}
}

func (p *capabilityFixtureProvider) InvokeTool(_ context.Context, _ domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	p.calls = append(p.calls, tool.Name)
	if tool.Name == "fixture.resource.get" {
		return map[string]any{"id": "resource-1"}, nil, nil
	}
	if input["resourceId"] != "resource-1" {
		return nil, nil, apperrors.ErrInvalidArgument
	}
	if p.verdict == "" {
		return map[string]any{"status": "success"}, nil, nil
	}
	return domainaigateway.CapabilityAssessment{Verdict: sohaapi.CapabilityAssessmentVerdict(p.verdict), Summary: "domain verification", Evidence: []domainaigateway.CapabilityEvidence{{Kind: "fixture", Source: "fixture.resource", ObservedAt: time.Now().UTC(), Summary: "resource observed"}}}, nil, nil
}

func (p *capabilityFixtureProvider) CurrentExecutionPrincipal(_ context.Context, id, token string) (domainidentity.Principal, error) {
	if p.revoked || id != "actor" || token != "frozen-token" {
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	return domainidentity.Principal{UserID: id, Roles: []string{"capability-test"}, AccessTokenID: token}, nil
}

func newCapabilityExecutorFixture(t *testing.T, verdict string) (*Service, *capabilityExecutorRepository, *capabilityFixtureProvider) {
	t.Helper()
	provider := &capabilityFixtureProvider{verdict: verdict}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"capability-test": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke}}})
	gateway := appaigateway.NewWithDeps(appaigateway.ServiceDeps{Permissions: permissions})
	gateway.SetCapabilityProviders(provider)
	repo := &capabilityExecutorRepository{&deliveryExecutorRepository{deliveryServiceRepository: &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}}}
	service := New(repo, nil, nil, permissions, nil, nil, nil, nil)
	service.SetCapabilityRuntime(gateway, provider)
	input := domainaigateway.CapabilityTaskInput{IdempotencyKey: "fixture-goal", Plan: domainaigateway.CapabilityPlan{Goal: "verify an unfamiliar resource", VerificationSteps: []string{"assess"}, Steps: []domainaigateway.CapabilityPlanStep{
		{ID: "get", Call: sohaapi.CapabilityCall{ToolName: "fixture.resource.get", CapabilityVersion: "1", Input: map[string]any{}}},
		{ID: "assess", Call: sohaapi.CapabilityCall{ToolName: "fixture.resource.assess", CapabilityVersion: "1", Input: map[string]any{}}, DependsOn: []string{"get"}, Bindings: []sohaapi.CapabilityInputBinding{{InputPath: "/resourceId", StepID: "get", OutputPath: "/id"}}},
	}}}
	principal, _ := provider.CurrentExecutionPrincipal(context.Background(), "actor", "frozen-token")
	if _, err := service.CreateCapabilityTask(context.Background(), principal, input); err != nil {
		t.Fatal(err)
	}
	return service, repo, provider
}

func tickCapabilityFixture(t *testing.T, service *Service, repo *capabilityExecutorRepository) {
	t.Helper()
	run, err := repo.ClaimManagedRun(context.Background(), "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service.runCapabilityTick(context.Background(), run)
}

func TestCapabilityWorkflowUsesBindingsAndEvidenceAcrossRestart(t *testing.T) {
	for verdict, status := range map[string]string{"satisfied": "completed", "unsatisfied": "failed", "inconclusive": "inconclusive", "": "inconclusive"} {
		t.Run("verdict-"+verdict, func(t *testing.T) {
			service, repo, provider := newCapabilityExecutorFixture(t, verdict)
			tickCapabilityFixture(t, service, repo)
			if len(provider.calls) != 1 || repo.run.Status != "running" {
				t.Fatalf("first step did not persist: %v %+v", provider.calls, repo.run)
			}
			// Recreate the worker owner; the only continuation is the stored Run.
			restarted := New(repo, nil, nil, service.permissions, nil, nil, nil, nil)
			restarted.SetCapabilityRuntime(service.capabilityRuntime, provider)
			tickCapabilityFixture(t, restarted, repo)
			if len(provider.calls) != 2 || repo.run.Status != status {
				t.Fatalf("goal status without expected evidence: calls=%v status=%s nodes=%+v", provider.calls, repo.run.Status, repo.run.NodeRuns)
			}
		})
	}
}

func TestCapabilityWorkflowStopsBeforeDispatchOnCancelOrRevocation(t *testing.T) {
	for _, scenario := range []string{"cancel", "revoked", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, provider := newCapabilityExecutorFixture(t, "satisfied")
			switch scenario {
			case "cancel":
				if _, err := repo.StopManagedRun(context.Background(), repo.run.ID, "user", "stop"); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				provider.revoked = true
			case "deadline":
				intent, _ := domainworkflow.CapabilityIntentFrom(repo.run)
				intent.Deadline = time.Now().Add(-time.Second)
				repo.run.Metadata["capabilityIntent"] = intent
			}
			tickCapabilityFixture(t, service, repo)
			if len(provider.calls) != 0 || repo.run.Status == "completed" || repo.run.Status == "running" {
				t.Fatalf("stopped task dispatched: calls=%v status=%s", provider.calls, repo.run.Status)
			}
		})
	}
}
