package access

import (
	"context"
	"testing"

	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainmenu "github.com/kubecrux/kubecrux/internal/domain/menu"
)

type stubPolicyCatalogReader struct {
	roles []domainaccess.RoleRecord
}

func (s stubPolicyCatalogReader) ListPolicies(context.Context) ([]domainaccess.Policy, error) {
	return nil, nil
}

func (s stubPolicyCatalogReader) ListRoles(context.Context) ([]domainaccess.RoleRecord, error) {
	return s.roles, nil
}

type stubRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

type stubVisibleMenuReader struct{}

func (stubVisibleMenuReader) ListVisible(context.Context, domainidentity.Principal) ([]domainmenu.Record, error) {
	return []domainmenu.Record{
		{ID: "dashboard", Path: "/"},
		{ID: "access", Path: "/access"},
	}, nil
}

func TestPermissionSnapshotUsesPersistedRolePermissionKeys(t *testing.T) {
	SetRolePermissionMatrix(nil)
	catalog := NewCatalog(nil, stubPolicyCatalogReader{}, nil, stubVisibleMenuReader{}, NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"custom-role": {PermAccessUsersView, PermAccessScopeGrantsManage},
		},
	}))

	snapshot, err := catalog.PermissionSnapshot(context.Background(), domainidentity.Principal{
		Roles: []string{"custom-role"},
	})
	if err != nil {
		t.Fatalf("PermissionSnapshot returned error: %v", err)
	}
	if len(snapshot.PermissionKeys) != 2 {
		t.Fatalf("PermissionKeys = %v, want 2 entries", snapshot.PermissionKeys)
	}
	if !HasPermission([]string{"custom-role"}, PermAccessScopeGrantsManage) {
		t.Fatalf("HasPermission should use runtime role permission matrix for custom roles")
	}
	if len(snapshot.VisibleMenuIDs) != 2 {
		t.Fatalf("VisibleMenuIDs = %v, want 2 entries", snapshot.VisibleMenuIDs)
	}
}

func TestCatalogListUsersRequiresAccessUsersViewPermission(t *testing.T) {
	SetRolePermissionMatrix(nil)
	catalog := NewCatalog(nil, stubPolicyCatalogReader{}, nil, nil, NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"custom-role": {PermAccessRolesView},
		},
	}))

	_, err := catalog.ListUsers(context.Background(), domainidentity.Principal{Roles: []string{"custom-role"}})
	if err == nil {
		t.Fatalf("ListUsers error = nil, want access denied")
	}
}

func TestPermissionSnapshotFailsClosedWithoutRuntimeResolver(t *testing.T) {
	SetRolePermissionMatrix(nil)
	catalog := NewCatalog(nil, stubPolicyCatalogReader{}, nil, nil, nil)

	_, err := catalog.PermissionSnapshot(context.Background(), domainidentity.Principal{
		Roles: []string{"admin"},
	})
	if err == nil {
		t.Fatalf("PermissionSnapshot error = nil, want runtime resolver failure")
	}
}

func TestCatalogListUsersFailsClosedWithoutRuntimeResolver(t *testing.T) {
	SetRolePermissionMatrix(nil)
	catalog := NewCatalog(nil, stubPolicyCatalogReader{}, nil, nil, nil)

	_, err := catalog.ListUsers(context.Background(), domainidentity.Principal{Roles: []string{"admin"}})
	if err == nil {
		t.Fatalf("ListUsers error = nil, want runtime resolver failure")
	}
}

func TestDefaultRolePermissionsIncludeWorkspaceEntryPermissions(t *testing.T) {
	SetRolePermissionMatrix(nil)

	if !HasPermission([]string{"developer"}, PermWorkspaceApplicationView) {
		t.Fatalf("developer role should include %s", PermWorkspaceApplicationView)
	}
	if !HasPermission([]string{"developer"}, PermWorkspaceResourceView) {
		t.Fatalf("developer role should include %s", PermWorkspaceResourceView)
	}
	if !HasPermission([]string{"auditor"}, PermWorkspaceResourceView) {
		t.Fatalf("auditor role should include %s", PermWorkspaceResourceView)
	}
	if HasPermission([]string{"auditor"}, PermWorkspaceApplicationView) {
		t.Fatalf("auditor role should not include %s", PermWorkspaceApplicationView)
	}
}
