package aigateway

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
)

type fakeGatewaySecretResolver struct {
	value        string
	pinCalls     int
	resolveCalls int
}

func (r *fakeGatewaySecretResolver) PinReferences(_ context.Context, _ domainidentity.Principal, refs map[string]string, target domainsecret.Target) ([]domainsecret.Reference, error) {
	r.pinCalls++
	result := make([]domainsecret.Reference, 0, len(refs))
	for alias := range refs {
		result = append(result, domainsecret.Reference{Alias: alias, SecretID: "secret-1", Version: 7, URI: "soha://secrets/secret-1/versions/7"})
	}
	if target != (domainsecret.Target{Type: "capability", Ref: "custom.echo"}) {
		return nil, fmt.Errorf("unexpected target: %#v", target)
	}
	return result, nil
}

func (r *fakeGatewaySecretResolver) ResolvePinnedReferences(_ context.Context, _ domainidentity.Principal, refs []domainsecret.Reference, _ domainsecret.Target) (map[string]string, error) {
	r.resolveCalls++
	if len(refs) != 1 || refs[0].Version != 7 {
		return nil, fmt.Errorf("unexpected pinned refs: %#v", refs)
	}
	return map[string]string{"TOKEN": r.value}, nil
}

func secretRefTestProvider(t *testing.T, invoked *bool, expected string) testCapabilityProvider {
	t.Helper()
	return testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{{Name: "custom.echo", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}}},
		invoke: func(ctx context.Context, _ domainidentity.Principal, _ domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
			*invoked = true
			if _, exists := input["TOKEN"]; exists {
				t.Fatal("resolved secret must not be merged into auditable tool input")
			}
			if got := domainsecret.ValuesFromContext(ctx)["TOKEN"]; got != expected {
				t.Fatalf("resolved secret = %q, want %q", got, expected)
			}
			return map[string]any{"ok": true}, nil, nil
		},
	}
}

func TestInvokeToolResolvesSecretRefsOnlyAtExecutionBoundary(t *testing.T) {
	repo := &memoryGatewayRepository{}
	resolver := &fakeGatewaySecretResolver{value: "runtime-secret"}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"developer": {appaccess.PermAIGatewayInvoke},
	}}), nil, repo)
	service.secrets = resolver
	invoked := false
	service.SetCapabilityProviders(secretRefTestProvider(t, &invoked, resolver.value))

	_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "custom.echo",
		Input:    map[string]any{"message": "hello"},
		SecretRefs: map[string]string{
			"TOKEN": "soha://secrets/secret-1",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if !invoked || resolver.pinCalls != 2 || resolver.resolveCalls != 1 {
		t.Fatalf("unexpected execution state: invoked=%v pin=%d resolve=%d", invoked, resolver.pinCalls, resolver.resolveCalls)
	}
	if strings.Contains(fmt.Sprint(repo.auditLogs), resolver.value) {
		t.Fatal("Gateway audit leaked resolved secret")
	}
}

func TestApprovalPinsSecretRefsWithoutResolvingUntilReplay(t *testing.T) {
	repo := &memoryGatewayRepository{accessPolicies: []domainaigateway.AccessPolicy{{
		ID: "policy-secret-approval", Enabled: true, SubjectType: "role", SubjectID: "developer", Effect: "allow",
		ToolPatterns: []string{"custom.echo"}, ApprovalPolicy: map[string]any{"strategy": "require_approval"},
	}}}
	resolver := &fakeGatewaySecretResolver{value: "approval-secret"}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"developer": {appaccess.PermAIGatewayInvoke},
		"admin":     {appaccess.PermAIGatewayManage, appaccess.PermAIGatewayInvoke},
	}}), nil, repo)
	service.secrets = resolver
	invoked := false
	service.SetCapabilityProviders(secretRefTestProvider(t, &invoked, resolver.value))

	held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
		ToolName: "custom.echo",
		Input:    map[string]any{"message": "hello"},
		SecretRefs: map[string]string{
			"TOKEN": "soha://secrets/secret-1",
		},
	})
	if err != nil {
		t.Fatalf("InvokeTool returned error: %v", err)
	}
	if invoked || resolver.resolveCalls != 0 || len(repo.approvalRequests) != 1 {
		t.Fatalf("secret resolved before approval: invoked=%v resolve=%d approvals=%d", invoked, resolver.resolveCalls, len(repo.approvalRequests))
	}
	request := repo.approvalRequests[0]
	if request.SecretRefs["TOKEN"] != "soha://secrets/secret-1/versions/7" || strings.Contains(fmt.Sprint(request.ToolInput), resolver.value) {
		t.Fatalf("approval did not persist only pinned refs: %#v", request)
	}

	decision, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId"), domainaigateway.ApprovalDecisionInput{Comment: "ok"})
	if err != nil {
		t.Fatalf("ApproveApprovalRequest returned error: %v", err)
	}
	if decision.Invocation == nil || !invoked || resolver.resolveCalls != 1 {
		t.Fatalf("approved replay did not resolve exactly once: %#v invoked=%v resolve=%d", decision, invoked, resolver.resolveCalls)
	}
}
