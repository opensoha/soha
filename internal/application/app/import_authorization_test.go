package app

import (
	"context"
	"errors"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestServiceImportUsesExistingApplicationScope(t *testing.T) {
	repo := &captureAppRepository{items: map[string]domainapp.App{"app": {ID: "app", Name: "Private", Key: "private", BusinessLineID: "finance", Group: "payments"}}}
	for _, allow := range []bool{true, false} {
		authorizer := &captureAppAuthorizer{allowed: allow}
		service := New(repo, nil, authorizer, nil, nil)
		err := service.AuthorizeServiceImport(context.Background(), domainidentity.Principal{}, "app", domainapp.UpsertInput{Key: "forged", BusinessLineID: "public"}, []domainapp.ServiceInput{{Key: "api", Name: "API"}})
		if !allow && !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("denial=%v", err)
		}
		if allow && err != nil {
			t.Fatal(err)
		}
		request := authorizer.requests[0]
		if request.Action != domainaccess.ActionUpdate || request.Delivery.ApplicationID != "app" || request.Delivery.BusinessLineID != "finance" || request.Delivery.ApplicationGroup != "payments" || request.Resource.Kind != "ApplicationService" {
			t.Fatalf("scope=%+v", request)
		}
	}
}
