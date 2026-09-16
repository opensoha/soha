package aigateway

import (
	"context"
	"errors"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

type scopeProviderFixture struct {
	testCapabilityProvider
	scopes            []map[string]string
	calls, recoveries int
}

func (p *scopeProviderFixture) ToolInvocationScopes(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) ([]map[string]string, error) {
	return p.scopes, nil
}
func (p *scopeProviderFixture) InvokeTool(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) (any, map[string]any, error) {
	p.calls++
	return map[string]any{"id": "batch-1", "firstPrivate": "one", "secondPrivate": "two"}, nil, nil
}
func (p *scopeProviderFixture) RecoverTool(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) (any, bool, error) {
	p.recoveries++
	return map[string]any{"id": "batch-1"}, true, nil
}
func capabilityScopesFixture() (*Service, *memoryGatewayRepository, *scopeProviderFixture, domainaigateway.ToolInvocationRequest) {
	repo := &memoryGatewayRepository{}
	service := contractTestService(repo, nil)
	provider := &scopeProviderFixture{scopes: []map[string]string{{"applicationId": "app-a"}, {"applicationId": "app-b"}}, testCapabilityProvider: testCapabilityProvider{tools: []domainaigateway.ToolCapability{{Name: "test.batch", Version: "1", RiskLevel: "read", Execution: &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}, InputSchema: gatewayObjectSchema(nil, map[string]any{"applicationId": gatewayStringSchema("Optional scope hint.")})}}}}
	service.SetCapabilityProviders(provider)
	return service, repo, provider, domainaigateway.ToolInvocationRequest{ToolName: "test.batch", CapabilityVersion: "1", Input: map[string]any{}}
}
func scopePolicy(id, effect string, scopes ...string) domainaigateway.AccessPolicy {
	p := domainaigateway.AccessPolicy{ID: id, Enabled: true, SubjectType: "role", SubjectID: "developer", Effect: effect, ToolPatterns: []string{"test.batch"}}
	if len(scopes) > 0 {
		p.ResourceScopes = map[string]any{"applicationIds": scopes}
	}
	return p
}
func TestCapabilityScopesRequireEveryTargetGrantAndPolicy(t *testing.T) {
	for _, scenario := range []string{"grant", "policy", "hint", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, provider, call := capabilityScopesFixture()
			switch scenario {
			case "grant":
				repo.toolGrants = []domainaigateway.ToolGrant{{ID: "one-app", SubjectType: "user", SubjectID: "user-1", ToolName: "test.batch", Effect: "allow", ResourceScopes: map[string]any{"applicationIds": []string{"app-a"}}}}
			case "policy":
				repo.accessPolicies = []domainaigateway.AccessPolicy{scopePolicy("allowed", "allow"), scopePolicy("denied", "deny", "app-b")}
			case "hint":
				call.Input["applicationId"] = "app-a"
			case "empty":
				provider.scopes = nil
			}
			if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), call); err == nil || provider.calls != 0 {
				t.Fatalf("unauthorized scope executed: calls=%d err=%v", provider.calls, err)
			}
		})
	}
}
func TestCapabilityScopesCombineRedactionAndCountOneInvocation(t *testing.T) {
	service, repo, provider, call := capabilityScopesFixture()
	a, b := scopePolicy("app-a", "allow", "app-a"), scopePolicy("app-b", "allow", "app-b")
	a.Conditions = map[string]any{"outputRedactionPolicy": map[string]any{"mode": "sanitize", "fields": []any{"firstPrivate"}}}
	b.Conditions = map[string]any{"outputRedactionPolicy": map[string]any{"mode": "sanitize", "fields": []any{"secondPrivate"}}}
	limit := scopePolicy("limit", "allow")
	limit.Conditions = map[string]any{"rateLimit": map[string]any{"maxCallsPerMinute": 1}}
	repo.accessPolicies = []domainaigateway.AccessPolicy{a, b, limit}
	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), call)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("output type = %T, want map[string]any", result.Output)
	}
	if output["firstPrivate"] != "[REDACTED]" || output["secondPrivate"] != "[REDACTED]" {
		t.Fatalf("scope-specific redaction was skipped: %+v", output)
	}
	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), call); err == nil || provider.calls != 1 {
		t.Fatalf("limit was bypassed: calls=%d err=%v", provider.calls, err)
	}
	for _, counter := range repo.rateLimitCounters {
		if counter.Count != 2 {
			t.Fatalf("one invocation counted once per target: count=%d", counter.Count)
		}
	}
}
func TestCapabilityScopesRecheckApprovalRecoveryAndHistory(t *testing.T) {
	service, repo, provider, call := capabilityScopesFixture()
	provider.tools[0].RiskLevel = domainaigateway.RiskLevelExecute
	provider.tools[0].RequiresApproval = true
	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), call)
	if err != nil || !held.RequiresApproval || len(repo.approvalRequests) != 1 {
		t.Fatalf("missing approval: %+v %v", held, err)
	}
	repo.accessPolicies = []domainaigateway.AccessPolicy{scopePolicy("allowed", "allow"), scopePolicy("new-denial", "deny", "app-b")}
	if _, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), repo.approvalRequests[0].ID, domainaigateway.ApprovalDecisionInput{}); !errors.Is(err, apperrors.ErrAccessDenied) || provider.calls != 0 {
		t.Fatalf("approval ignored second target denial: %v", err)
	}
	repo.toolGrants = []domainaigateway.ToolGrant{{ID: "first-only", SubjectType: "user", SubjectID: "user-1", ToolName: call.ToolName, Effect: "allow", ResourceScopes: map[string]any{"applicationIds": []string{"app-a"}}}}
	if _, err := service.recoverCapabilityCall(context.Background(), testPrincipal("developer"), call); !errors.Is(err, apperrors.ErrAccessDenied) || provider.recoveries != 0 {
		t.Fatalf("recovery ignored second target denial: %v", err)
	}
	repo.toolGrants = nil
	plan := domainaigateway.CapabilityPlan{Goal: "inspect batch", Steps: []domainaigateway.CapabilityPlanStep{{ID: "batch", Call: sohaapi.CapabilityCall{ToolName: call.ToolName, CapabilityVersion: "1", Input: call.Input}}}}
	run := domainworkflow.Run{ID: "goal", Scope: domainworkflow.ScopeCapabilityTask, Status: "completed", Metadata: map[string]any{"capabilityIntent": domainworkflow.CapabilityIntent{ActorID: "user-1", Digest: "digest", Deadline: time.Now().Add(time.Hour), Input: domainaigateway.CapabilityTaskInput{Plan: plan}}}, NodeRuns: []domainworkflow.NodeRun{}}
	if _, err := service.VisibleCapabilityTask(context.Background(), testPrincipal("developer"), run); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("history ignored second target denial: %v", err)
	}
}

func TestCapabilityInputPolicyCannotRetargetExecution(t *testing.T) {
	service, repo, provider, call := capabilityScopesFixture()
	provider.scopes = []map[string]string{{"applicationId": "app-a"}}
	call.Input["applicationId"] = "app-a"
	policy := scopePolicy("sanitize", "allow")
	policy.Conditions = map[string]any{"redactionPolicy": map[string]any{"mode": "sanitize", "fields": []any{"applicationId"}}}
	repo.accessPolicies = []domainaigateway.AccessPolicy{policy}
	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), call); !errors.Is(err, apperrors.ErrAccessDenied) || provider.calls != 0 {
		t.Fatalf("redaction retargeted execution: %v calls=%d", err, provider.calls)
	}
	// Legacy static capabilities must also preserve the scope used by authorization.
	service.SetCapabilityProviders(testCapabilityProvider{tools: provider.tools, invoke: func(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) (any, map[string]any, error) {
		provider.calls++
		return nil, nil, nil
	}})
	if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), call); !errors.Is(err, apperrors.ErrAccessDenied) || provider.calls != 0 {
		t.Fatalf("static scope was retargeted: %v calls=%d", err, provider.calls)
	}
}

func TestCapabilityApprovalFreezesEveryResolvedTarget(t *testing.T) {
	for _, change := range []string{"reordered", "retargeted", "removed"} {
		t.Run(change, func(t *testing.T) {
			service, repo, provider, call := capabilityScopesFixture()
			provider.tools[0].RiskLevel = domainaigateway.RiskLevelExecute
			provider.tools[0].RequiresApproval = true
			if _, err := service.InvokeTool(context.Background(), testPrincipal("developer"), call); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "reordered":
				provider.scopes[0], provider.scopes[1] = provider.scopes[1], provider.scopes[0]
			case "retargeted":
				provider.scopes[1]["applicationId"] = "app-c"
			case "removed":
				provider.scopes = provider.scopes[:1]
			}
			_, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), repo.approvalRequests[0].ID, domainaigateway.ApprovalDecisionInput{})
			if change == "reordered" {
				if err != nil || provider.calls != 1 {
					t.Fatalf("scope order changed authorization: %v", err)
				}
			} else if !errors.Is(err, apperrors.ErrConflict) || provider.calls != 0 {
				t.Fatalf("changed target executed: calls=%d err=%v", provider.calls, err)
			}
		})
	}
}
