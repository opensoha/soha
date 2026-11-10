package workflow

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/policy"
	"testing"
)

type explicitWorkflowReadAuthorizer struct{}

func (explicitWorkflowReadAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: request.PermissionKey == appaccess.PermDeliveryWorkflowsView && request.Delivery.ApplicationID == "app"}, nil
}
func TestWorkflowListAndGetSendExplicitReadPermission(t *testing.T) {
	service := &Service{repo: &stubWorkflowRepository{items: []domainworkflow.Run{{ID: "run", ApplicationID: "app"}}}, apps: &stubWorkflowApps{}, authorizer: explicitWorkflowReadAuthorizer{}, permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"reader": {appaccess.PermDeliveryWorkflowsView, appaccess.PermDeliveryApplicationEnvView}}})}
	principal := domainidentity.Principal{Roles: []string{"reader"}}
	items, err := service.List(context.Background(), principal, "app", "", 50)
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %+v %v", items, err)
	}
	if _, err = service.Get(context.Background(), principal, "run"); err != nil {
		t.Fatal(err)
	}
}

type batchScopeGrantReader struct{ items []domainscopegrant.Record }

func (r batchScopeGrantReader) List(context.Context) ([]domainscopegrant.Record, error) {
	return r.items, nil
}

func TestPermissionOnlyBatchTargetRetainsEnvironmentAndReadWriteScopes(t *testing.T) {
	resolver := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{
		"writer": {appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryWorkflowsView, appaccess.PermDeliveryApplicationEnvView, "delivery.application-environments.update"},
		"reader": {appaccess.PermDeliveryWorkflowsView, appaccess.PermDeliveryApplicationEnvView},
	}})
	grants := batchScopeGrantReader{items: []domainscopegrant.Record{
		{SubjectType: "user", SubjectID: "writer", ScopeType: "delivery", BusinessLineID: "retail", ApplicationIDs: []string{"app"}, EnvironmentIDs: []string{"dev"}, Role: "writer", Enabled: true},
		{SubjectType: "user", SubjectID: "reader", ScopeType: "delivery", BusinessLineID: "retail", ApplicationIDs: []string{"app"}, EnvironmentIDs: []string{"dev"}, Role: "reader", Enabled: true},
	}}
	authorizer := appaccess.New(policy.NewEngine(), nil, grants, nil)
	authorizer.SetPermissionResolver(resolver)
	service := &Service{apps: &stubWorkflowApps{}, authorizer: authorizer, permissions: resolver, catalog: &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{
		{ID: "dev-binding", ApplicationID: "app", BusinessLineID: "retail", EnvironmentKey: "dev"},
		{ID: "prod-binding", ApplicationID: "app", BusinessLineID: "retail", EnvironmentKey: "prod"},
	}}}
	target := domainworkflow.DeliveryTargetInput{ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "dev-binding", Action: "config_update", TemplateCandidateID: "candidate"}
	for _, role := range []string{"writer", "reader"} {
		principal := domainidentity.Principal{UserID: role, Roles: []string{role}}
		if err := service.authorizeDeliveryTarget(context.Background(), principal, target, domainaccess.ActionView); err != nil {
			t.Fatal(err)
		}
		err := service.authorizeDeliveryTarget(context.Background(), principal, target, domainaccess.ActionTrigger)
		if role == "writer" && err != nil {
			t.Fatalf("permission-only adoption: %v", err)
		}
		if role == "reader" && !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("reader adoption: %v", err)
		}
		outside := target
		outside.ApplicationEnvironmentID = "prod-binding"
		if err := service.authorizeDeliveryTarget(context.Background(), principal, outside, domainaccess.ActionView); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("outside environment: %v", err)
		}
	}
}
