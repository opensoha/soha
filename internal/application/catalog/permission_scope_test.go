package catalog

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/policy"
	"testing"
)

func TestPermissionOnlyBindingReadPreservesEnvironmentScope(t *testing.T) {
	resolver := appaccess.NewPermissionResolver(stubCatalogRolePermissionReader{matrix: map[string][]string{"custom-reader": {appaccess.PermDeliveryApplicationEnvView}}})
	auth := appaccess.New(policy.NewEngine(), nil, stubScopeGrantReader{items: []domainscopegrant.Record{{SubjectType: "user", SubjectID: "reader", ScopeType: "delivery", BusinessLineID: "retail", ApplicationIDs: []string{"app"}, EnvironmentIDs: []string{"test"}, Role: "custom-reader", Enabled: true}}}, nil)
	auth.SetPermissionResolver(resolver)
	repo := &stubCatalogRepository{applicationEnvironments: map[string]domaincatalog.ApplicationEnvironment{
		"allowed": {ID: "allowed", ApplicationID: "app", BusinessLineID: "retail", EnvironmentKey: "test"},
		"outside": {ID: "outside", ApplicationID: "app", BusinessLineID: "retail", EnvironmentKey: "prod"},
	}}
	service := New(repo, auth, nil, resolver, nil, nil)
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"custom-reader"}}
	if _, err := service.GetApplicationEnvironment(context.Background(), principal, "allowed"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetApplicationEnvironment(context.Background(), principal, "outside"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("outside environment: %v", err)
	}
	items, err := service.ListApplicationEnvironments(context.Background(), principal)
	if err != nil || len(items) != 1 || items[0].ID != "allowed" {
		t.Fatalf("scoped list: %+v %v", items, err)
	}
}
