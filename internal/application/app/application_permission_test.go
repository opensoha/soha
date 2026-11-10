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

type actionScopeApplicationRepository struct {
	Repository
	item domainapp.App
	got  string
}

func (r *actionScopeApplicationRepository) Get(_ context.Context, id string) (domainapp.App, error) {
	r.got = id
	return r.item, nil
}

type actionScopeApplicationAuthorizer struct {
	request domainaccess.Request
	allow   bool
}

func (a *actionScopeApplicationAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.request = request
	return domainaccess.Decision{Allowed: a.allow}, nil
}

func TestApplicationPermissionRetainsCanonicalActionAndActualScope(t *testing.T) {
	for _, allow := range []bool{false, true} {
		repo := &actionScopeApplicationRepository{item: domainapp.App{ID: "app-1", Key: "api", Name: "API", BusinessLineID: "line-1", Group: "group-1"}}
		authorizer := &actionScopeApplicationAuthorizer{allow: allow}
		service := &Service{repo: repo, authorizer: authorizer}
		principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"task-controller"}}
		err := service.AuthorizeApplicationPermission(t.Context(), principal, " app-1 ", "delivery.execution-tasks.cancel")
		if allow && err != nil || !allow && !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("authorization allow=%v error=%v", allow, err)
		}
		request := authorizer.request
		if repo.got != "app-1" || request.PermissionKey != "delivery.execution-tasks.cancel" || request.Action != "" || request.Subject.UserID != principal.UserID || request.Delivery.ApplicationID != repo.item.ID || request.Delivery.BusinessLineID != repo.item.BusinessLineID || request.Delivery.ApplicationGroup != repo.item.Group {
			t.Fatal("application action authorization used a view fallback or caller-supplied scope")
		}
	}
}

func TestApplicationPermissionFailsClosedWhenAuthorizationUnavailable(t *testing.T) {
	for _, service := range []*Service{nil, {}, {repo: &actionScopeApplicationRepository{}}} {
		if err := service.AuthorizeApplicationPermission(t.Context(), domainidentity.Principal{}, "app-1", "delivery.execution-tasks.cancel"); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatal("missing application authorizer allowed an action")
		}
	}
}
