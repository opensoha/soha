package aigateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type executionAuthorizationProvider struct {
	*scopeProviderFixture
	authorization domain.ExecutionAuthorization
}

func (p *executionAuthorizationProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domain.ToolCapability, input map[string]any) (any, map[string]any, error) {
	value, ok := domain.ExecutionAuthorizationFrom(ctx)
	if !ok {
		panic("execution provenance missing")
	}
	// Model the domain queue's persisted JSON, including numeric conversions.
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(encoded, &p.authorization); err != nil {
		return nil, nil, err
	}
	return p.scopeProviderFixture.InvokeTool(ctx, principal, tool, input)
}

func TestQueuedGatewayAuthorizationRechecksWithoutRateConsumption(t *testing.T) {
	for _, scenario := range []string{"unchanged", "grant", "policy", "scope", "version", "actor", "approval-required", "dry-run"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, scopeProvider, call := capabilityScopesFixture()
			provider := &executionAuthorizationProvider{scopeProviderFixture: scopeProvider}
			service.SetCapabilityProviders(provider)
			policy := scopePolicy("original", "allow")
			policy.Conditions = map[string]any{"rateLimit": map[string]any{"maxCallsPerMinute": 1}}
			repo.accessPolicies = []domain.AccessPolicy{policy}
			actor := testPrincipal("developer")
			if _, err := service.InvokeTool(context.Background(), actor, call); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(repo.rateLimitCounters)
			switch scenario {
			case "grant":
				repo.toolGrants = []domain.ToolGrant{{ID: "one-app", SubjectType: "user", SubjectID: actor.UserID, ToolName: call.ToolName, Effect: "allow", ResourceScopes: map[string]any{"applicationIds": []string{"app-a"}}}}
			case "policy":
				repo.accessPolicies = append(repo.accessPolicies, scopePolicy("deny-second", "deny", "app-b"))
			case "scope":
				provider.scopes[1]["applicationId"] = "other"
			case "version":
				provider.tools[0].Version = "2"
			case "actor":
				actor.UserID = "other"
			case "approval-required":
				repo.accessPolicies[0].ApprovalPolicy = map[string]any{"strategy": "require_approval"}
			case "dry-run":
				repo.accessPolicies[0].ApprovalPolicy = map[string]any{"strategy": "dry_run_only"}
			}
			for range 2 {
				err := service.CheckExecutionAuthorization(context.Background(), actor, provider.authorization)
				if (err == nil) != (scenario == "unchanged") {
					t.Fatalf("queued authorization %s: %v", scenario, err)
				}
			}
			after, _ := json.Marshal(repo.rateLimitCounters)
			if string(before) != string(after) || len(repo.approvalRequests) != 0 || provider.calls != 1 {
				t.Fatal("authorization check consumed a call, approval or provider mutation")
			}
		})
	}
}

func TestQueuedGatewayAuthorizationRequiresOriginalValidApproval(t *testing.T) {
	for _, scenario := range []string{"valid", "revoked", "expired", "changed-policy", "changed-input"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, scoped, call := capabilityScopesFixture()
			scoped.tools[0].RequiresApproval = true
			provider := &executionAuthorizationProvider{scopeProviderFixture: scoped}
			service.SetCapabilityProviders(provider)
			actor := testPrincipal("developer")
			if _, err := service.InvokeTool(context.Background(), actor, call); err != nil {
				t.Fatal(err)
			}
			if _, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), repo.approvalRequests[0].ID, domain.ApprovalDecisionInput{}); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "revoked":
				repo.approvalRequests[0].Status = "canceled"
			case "expired":
				past := time.Now().Add(-time.Minute)
				repo.approvalRequests[0].ExpiresAt = &past
			case "changed-policy":
				policy := scopePolicy("new-policy", "allow")
				policy.ApprovalPolicy = map[string]any{"strategy": "require_approval", "requiredApprovals": 2}
				repo.accessPolicies = []domain.AccessPolicy{policy}
			case "changed-input":
				repo.approvalRequests[0].ToolInput = map[string]any{"applicationId": "other"}
			}
			err := service.CheckExecutionAuthorization(context.Background(), actor, provider.authorization)
			if (err == nil) != (scenario == "valid") || provider.calls != 1 || len(repo.approvalRequests) != 1 {
				t.Fatalf("queued approval %s: %v calls=%d", scenario, err, provider.calls)
			}
		})
	}
}
