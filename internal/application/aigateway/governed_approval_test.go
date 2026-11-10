package aigateway

import (
	"context"
	"slices"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type governedApprovalIdentity struct {
	principal domainidentity.Principal
	revoked   bool
	token     string
}

func (*governedApprovalIdentity) CurrentPrincipal(context.Context, string) (domainidentity.Principal, error) {
	return domainidentity.Principal{}, apperrors.ErrAccessDenied
}

func (f *governedApprovalIdentity) CurrentExecutionPrincipal(_ context.Context, subject, token string) (domainidentity.Principal, error) {
	f.token = token
	if f.revoked || subject != f.principal.UserID || token != f.principal.AccessTokenID {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	return f.principal, nil
}

func TestGovernedApprovalReloadsOriginalCredential(t *testing.T) {
	for _, subject := range []string{"user-1", "service_account:automation"} {
		for _, scenario := range []string{"unchanged", "revoked", "binding-changed", "permission-revoked", "permission-expanded"} {
			t.Run(subject+"/"+scenario, func(t *testing.T) { governedApprovalReloadsOriginalCredentialPhase1(t, &subject, &scenario) })
		}
	}
}

func governedApprovalReloadsOriginalCredentialPhase1(t *testing.T, sharedSubject *string, sharedScenario *string) {

	repo := &memoryGatewayRepository{aiClients: map[string]domainaigateway.AIClient{"codex": {ID: "codex", Status: "active"}}}
	delivery := &fakeDeliveryService{workflowRunID: "workflow-1"}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"admin":     {appaccess.PermAIGatewayManage},
		"developer": {appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger, appaccess.PermDeliveryApplicationsCreate},
	}}), nil, repo)
	service.SetDeliveryServices(&fakeApplicationService{}, delivery)
	actor := testPrincipal("developer")
	actor.UserID, actor.AccessTokenID = (*sharedSubject), "original-key"
	actor.PermissionKeys = []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger}
	actor.GovernedAI = &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "delivery-developer"}
	held, err := service.InvokeTool(t.Context(), actor, domainaigateway.ToolInvocationRequest{AIClientID: "codex", SkillID: "delivery-developer", ToolName: "delivery.actions.trigger", Input: map[string]any{"applicationId": "app-1", "action": "build", "applicationEnvironmentId": "binding-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.approvalRequests) != 1 || repo.approvalRequests[0].ActorTokenID != actor.AccessTokenID || len(repo.approvalRequests[0].ActorPermissionCeiling) == 0 {
		t.Fatal("approval lost credential identity or permission ceiling")
	}
	reader := &governedApprovalIdentity{principal: actor}
	service.identity = reader
	configureGovernedApprovalIdentity(reader, sharedScenario)
	id := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	decision, err := service.ApproveApprovalRequest(t.Context(), testPrincipal("admin"), id, domainaigateway.ApprovalDecisionInput{})
	if (*sharedScenario) == "unchanged" || (*sharedScenario) == "permission-expanded" {
		if err != nil || !delivery.triggered || decision.Request.Status != "executed" {
			t.Fatalf("valid bounded approval failed: %v %+v", err, decision)
		}
	} else if err == nil || delivery.triggered || decision.Request.Status != "failed" {
		t.Fatalf("unsafe approval replay: %v %+v", err, decision)
	}
	if reader.token != "original-key" {
		t.Fatal("approval did not reload its original credential")
	}
	if (*sharedScenario) == "permission-expanded" {
		bounded, err := service.currentApprovalPrincipal(t.Context(), repo.approvalRequests[0])
		if err != nil || slices.Contains(bounded.PermissionKeys, appaccess.PermDeliveryApplicationsCreate) {
			t.Fatal("approval widened its original permission ceiling")
		}
	}
}

func configureGovernedApprovalIdentity(reader *governedApprovalIdentity, sharedScenario *string) {
	switch *sharedScenario {
	case "revoked":
		reader.revoked = true
	case "binding-changed":
		reader.principal.GovernedAI = &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "other", SkillID: "delivery-developer"}
	case "permission-revoked":
		reader.principal.PermissionKeys = []string{appaccess.PermAIGatewayInvoke}
	case "permission-expanded":
		reader.principal.PermissionKeys = nil
	}
}
