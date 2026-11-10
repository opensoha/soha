package app

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/policy"
	"testing"
)

type permissionOnlyRoleReader struct{}

func (permissionOnlyRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"custom-reader": {appaccess.PermDeliveryApplicationsView}, "ops": {appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationsCreate, appaccess.PermDeliveryApplicationsUpdate}, "viewer": {appaccess.PermDeliveryApplicationsView}}, nil
}

func TestPermissionOnlyApplicationReadPreservesScope(t *testing.T) {
	resolver := appaccess.NewPermissionResolver(permissionOnlyRoleReader{})
	auth := appaccess.New(policy.NewEngine(), nil, appScopeGrantReader{items: []domainscopegrant.Record{{SubjectType: "user", SubjectID: "reader", ScopeType: "delivery", BusinessLineID: "retail", ApplicationIDs: []string{"allowed"}, Role: "custom-reader", Enabled: true}}}, nil)
	auth.SetPermissionResolver(resolver)
	repo := &captureAppRepository{items: map[string]domainapp.App{"allowed": {ID: "allowed", BusinessLineID: "retail"}, "outside": {ID: "outside", BusinessLineID: "retail"}}}
	service := New(repo, nil, auth, nil, nil)
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"custom-reader"}}
	if _, err := service.Get(context.Background(), principal, "allowed"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), principal, "outside"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("outside app: %v", err)
	}
	if _, err := service.Update(context.Background(), principal, "allowed", validAppInput("allowed")); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("reader mutation: %v", err)
	}
}
