package app

import (
	"context"
	"errors"
	access "github.com/opensoha/soha/internal/application/access"
	da "github.com/opensoha/soha/internal/domain/access"
	app "github.com/opensoha/soha/internal/domain/application"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type mappedImportRepo struct {
	captureAppRepository
	service app.Service
}

func (r *mappedImportRepo) GetService(_ context.Context, appID, id string) (app.Service, error) {
	if r.service.ID != id || r.service.ApplicationID != appID {
		return app.Service{}, apperrors.ErrNotFound
	}
	return r.service, nil
}

type mappedImportRoles struct{}

func (mappedImportRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"editor": {access.ManagedActionPermission(access.PermDeliveryApplicationServicesManage, "update")}, "creator": {access.ManagedActionPermission(access.PermDeliveryApplicationServicesManage, "create")}}, nil
}
func TestExistingServiceImportRequiresActualServiceUpdateScope(t *testing.T) {
	repo := &mappedImportRepo{captureAppRepository: captureAppRepository{items: map[string]app.App{"app": {ID: "app", Group: "payments", BusinessLineID: "finance"}}}, service: app.Service{ID: "logical", ApplicationID: "app", Key: "real", Name: "Actual", Enabled: true}}
	auth := &captureAppAuthorizer{allowed: true}
	s := New(repo, nil, auth, nil, nil)
	s.SetPermissionResolver(access.NewPermissionResolver(mappedImportRoles{}))
	input := []app.ServiceInput{{ID: "logical", Key: "forged", Name: "Forged"}}
	err := s.AuthorizeServiceImport(context.Background(), di.Principal{Roles: []string{"creator"}}, "app", app.UpsertInput{}, input)
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("create-only mapping=%v", err)
	}
	err = s.AuthorizeServiceImport(context.Background(), di.Principal{Roles: []string{"editor"}}, "app", app.UpsertInput{}, input)
	if err != nil {
		t.Fatal(err)
	}
	last := auth.requests[len(auth.requests)-1]
	if last.Action != da.ActionUpdate || last.Resource.Name != "Actual" || last.Delivery.ApplicationID != "app" || last.Delivery.BusinessLineID != "finance" {
		t.Fatalf("scope=%+v", last)
	}
	repo.service.ApplicationID = "foreign"
	if err = s.AuthorizeServiceImport(context.Background(), di.Principal{Roles: []string{"editor"}}, "app", app.UpsertInput{}, input); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("foreign=%v", err)
	}
	repo.service.ApplicationID = "app"
	repo.service.Enabled = false
	if err = s.AuthorizeServiceImport(context.Background(), di.Principal{Roles: []string{"editor"}}, "app", app.UpsertInput{}, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("disabled=%v", err)
	}
}
