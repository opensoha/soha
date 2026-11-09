package bootstrap

import (
	"context"
	"errors"
	app "github.com/opensoha/soha/internal/application/access"
	access "github.com/opensoha/soha/internal/domain/access"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type radiusRoles struct{}

func (radiusRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"radius": {"network_access.nas_bindings.view", "network_access.nas_bindings.create", "network_access.nas_bindings.update"}}, nil
}

type radiusScope struct {
	request access.Request
	allow   bool
	calls   int
}

func (s *radiusScope) Authorize(_ context.Context, r access.Request) (access.Decision, error) {
	s.request = r
	s.calls++
	return access.Decision{Allowed: s.allow}, nil
}
func TestRadiusAuthorityNASOnlyPermissionsAndSiteScope(t *testing.T) {
	scope := &radiusScope{allow: true}
	a := radiusAuthority{permissions: app.NewPermissionResolver(radiusRoles{}), scope: scope}
	p := identity.Principal{UserID: "user", Roles: []string{"radius"}}
	for _, action := range []string{"view", "create", "update"} {
		if e := a.AuthorizeRADIUS(context.Background(), p, "site", "binding", action); e != nil {
			t.Fatal(e)
		}
		if scope.request.PermissionKey != "network_access.nas_bindings."+action || scope.request.Resource.Kind != "NetworkNASBinding" || scope.request.Resource.Labels["network.siteId"] != "site" {
			t.Fatal("scope/source permission drift")
		}
	}
	scope.allow = false
	if !errors.Is(a.AuthorizeRADIUS(context.Background(), p, "other", "binding", "update"), apperrors.ErrAccessDenied) {
		t.Fatal("foreign site allowed")
	}
	before := scope.calls
	if e := a.AuthorizeRADIUS(context.Background(), identity.Principal{UserID: "user", Roles: []string{"no-grant"}}, "site", "binding", "create"); !errors.Is(e, apperrors.ErrAccessDenied) || scope.calls != before {
		t.Fatal("missing permission reached scope")
	}
}
