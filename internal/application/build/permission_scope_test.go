package build

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type explicitReadAuthorizer struct{}

func (explicitReadAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: request.PermissionKey == appaccess.PermDeliveryApplicationsView && request.Delivery.ApplicationID == "app" && request.Delivery.BusinessLineID == "retail"}, nil
}
func TestPermissionKeyOnlyReadsDoNotGrantTrigger(t *testing.T) {
	service := &Service{apps: buildAppFake{app: domainapp.App{ID: "app", BusinessLineID: "retail"}}, authorizer: explicitReadAuthorizer{}}
	principal := domainidentity.Principal{UserID: "reader"}
	for _, action := range []domainaccess.Action{domainaccess.ActionView, domainaccess.ActionList} {
		if err := service.authorize(context.Background(), principal, action, "app"); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	action := domainaccess.ActionTrigger
	if err := service.authorize(context.Background(), principal, action, "app"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("reader trigger: %v", err)
	}
}
