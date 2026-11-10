package release

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type explicitReadAuthorizer struct{}

func (explicitReadAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: request.PermissionKey == appaccess.PermDeliveryReleasesView && request.Delivery.ApplicationID == "app" && request.Delivery.BusinessLineID == "retail"}, nil
}
func TestPermissionKeyOnlyReadsDoNotGrantTrigger(t *testing.T) {
	service := &Service{authorizer: explicitReadAuthorizer{}, resolver: &stubReleaseResolver{}}
	principal := domainidentity.Principal{UserID: "reader"}
	for _, action := range []domainaccess.Action{domainaccess.ActionView, domainaccess.ActionList} {
		if err := service.authorize(context.Background(), principal, "", "", "Release", "app", "retail", "", "app", action); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	action := domainaccess.ActionTrigger
	if err := service.authorize(context.Background(), principal, "", "", "Release", "app", "retail", "", "app", action); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("reader trigger: %v", err)
	}
}
