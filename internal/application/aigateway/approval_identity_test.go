package aigateway

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type approvalPrincipalFunc func(context.Context, string) (domainidentity.Principal, error)

func (f approvalPrincipalFunc) CurrentPrincipal(ctx context.Context, id string) (domainidentity.Principal, error) {
	return f(ctx, id)
}

func TestApprovalReplayRevalidatesActorAndArguments(t *testing.T) {
	for _, scenario := range []string{"unchanged", "role-revoked", "account-disabled", "identity-unavailable", "arguments-changed"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &memoryGatewayRepository{}
			delivery := &fakeDeliveryService{workflowRunID: "workflow-1"}
			service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
				"admin":     {appaccess.PermAIGatewayManage},
				"developer": {appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger},
			}}), nil, repo)
			service.SetDeliveryServices(&fakeApplicationService{}, delivery)
			held, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{
				ToolName: "delivery.actions.trigger", Input: map[string]any{"applicationId": "app-1", "action": "build", "applicationEnvironmentId": "binding-1"},
			})
			if err != nil {
				t.Fatal(err)
			}
			service.identity = approvalPrincipalFunc(func(_ context.Context, id string) (domainidentity.Principal, error) {
				if id != "user-1" {
					t.Fatalf("wrong actor: %s", id)
				}
				if scenario == "account-disabled" {
					return domainidentity.Principal{}, apperrors.ErrUnauthorized
				}
				actor := testPrincipal("developer")
				if scenario == "role-revoked" {
					actor.Roles = nil
				}
				return actor, nil
			})
			if scenario == "identity-unavailable" {
				service.identity = nil
			}
			if scenario == "arguments-changed" {
				repo.accessPolicies = []domainaigateway.AccessPolicy{{ID: "new-policy", Enabled: true, SubjectType: "role", SubjectID: "developer", Effect: "allow", ToolPatterns: []string{"delivery.actions.trigger"}, Conditions: map[string]any{"redactionPolicy": map[string]any{"mode": "sanitize", "fields": []any{"applicationId"}}}}}
			}
			id := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
			decision, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), id, domainaigateway.ApprovalDecisionInput{})
			if scenario == "unchanged" {
				if err != nil || !delivery.triggered || decision.Request.Status != "executed" {
					t.Fatalf("expected execution: %v, %s", err, decision.Request.Status)
				}
			} else if err == nil || delivery.triggered || decision.Request.Status != "failed" {
				t.Fatalf("unsafe replay: error=%v triggered=%v status=%s", err, delivery.triggered, decision.Request.Status)
			}
			_, err = service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), id, domainaigateway.ApprovalDecisionInput{})
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("duplicate approval accepted: %v", err)
			}
		})
	}
}

func TestRequestToolApprovalNeverExecutesPermittedChange(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"developer": {appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsCreate},
	}}), nil, repo)
	// No owning service is installed: a direct invocation would fail, not return an approval.
	result, err := service.RequestToolApproval(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "delivery.applications.create", Input: map[string]any{"name": "Review me", "key": "review-me"}})
	if err != nil || result.Result != "pending_approval" || len(repo.approvalRequests) != 1 {
		t.Fatalf("expected a pending request without execution: %v, %#v", err, result)
	}
	if repo.approvalRequests[0].ToolInput["key"] != "review-me" {
		t.Fatal("approval did not pin arguments")
	}
}
