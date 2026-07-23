package announcement

import (
	"context"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type announcementRolePermissions struct {
	permissions map[string][]string
}

func (r announcementRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.permissions, nil
}

func TestAuthorizeInboxAcceptsPortalViewPermission(t *testing.T) {
	resolver := appaccess.NewPermissionResolver(announcementRolePermissions{
		permissions: map[string][]string{
			"portal-user": {appaccess.PermIdentityPortalView},
		},
	})
	service := &Service{permissions: resolver}

	if err := service.authorizeInbox(context.Background(), domainidentity.Principal{Roles: []string{"portal-user"}}); err != nil {
		t.Fatalf("authorizeInbox() error = %v", err)
	}
}

func TestAuthorizeInboxRejectsPrincipalWithoutReadPermission(t *testing.T) {
	resolver := appaccess.NewPermissionResolver(announcementRolePermissions{
		permissions: map[string][]string{
			"viewer": {appaccess.PermIdentityApplicationsView},
		},
	})
	service := &Service{permissions: resolver}

	if err := service.authorizeInbox(context.Background(), domainidentity.Principal{Roles: []string{"viewer"}}); err == nil {
		t.Fatal("authorizeInbox() error = nil, want access denied")
	}
}
