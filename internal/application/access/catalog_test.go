package access

import (
	"context"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmenu "github.com/opensoha/soha/internal/domain/menu"
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

func TestDefaultRolePermissionsDeliveryWorkbenchRoles(t *testing.T) {
	SetRolePermissionMatrix(nil)

	for _, permission := range []string{
		PermWorkspaceApplicationView,
		PermDeliveryApplicationsView,
		PermDeliveryApplicationServicesView,
		PermDeliveryApplicationEnvView,
		PermDeliveryReleaseBundlesView,
		PermDeliveryExecutionTasksView,
	} {
		if !HasPermission([]string{"tester"}, permission) {
			t.Fatalf("tester role should include %s", permission)
		}
	}

	for _, permission := range []string{
		PermWorkspaceApplicationView,
		PermDeliveryApplicationsView,
		PermDeliveryApplicationServicesView,
		PermDeliveryApplicationEnvView,
		PermDeliveryReleaseBundlesView,
		PermDeliveryExecutionTasksView,
		PermDeliveryReleaseBoardView,
		PermDeliveryWorkflowsView,
		PermDeliveryReleasesView,
	} {
		if !HasPermission([]string{"readonly"}, permission) {
			t.Fatalf("readonly role should include %s", permission)
		}
	}

	for _, role := range []string{"tester", "readonly"} {
		for _, permission := range []string{
			PermDeliveryApplicationsCreate,
			PermDeliveryApplicationsUpdate,
			PermDeliveryApplicationsDelete,
			PermDeliveryApplicationServicesManage,
			PermDeliveryApplicationEnvManage,
			PermDeliveryBuildTemplatesManage,
			PermDeliveryWorkflowTemplatesManage,
			PermDeliveryRegistriesManage,
			PermDeliveryBuildsTrigger,
			PermDeliveryWorkflowsTrigger,
			PermDeliveryReleasesTrigger,
			PermPlatformDeploymentRollback,
		} {
			if HasPermission([]string{role}, permission) {
				t.Fatalf("%s role should not include %s", role, permission)
			}
		}
	}
}

func TestDefaultRolePermissionsAIGateway(t *testing.T) {
	SetRolePermissionMatrix(nil)

	for _, permission := range []string{PermAIGatewayView, PermAIGatewayInvoke, PermAIGatewayManage} {
		for _, role := range []string{"admin", "ops"} {
			if !HasPermission([]string{role}, permission) {
				t.Fatalf("%s role should include %s", role, permission)
			}
		}
	}
	for _, permission := range []string{PermAIGatewayView, PermAIGatewayInvoke} {
		if !HasPermission([]string{"developer"}, permission) {
			t.Fatalf("developer role should include %s", permission)
		}
	}
	if HasPermission([]string{"developer"}, PermAIGatewayManage) {
		t.Fatalf("developer role should not include %s", PermAIGatewayManage)
	}
	if !HasPermission([]string{"readonly"}, PermAIGatewayView) {
		t.Fatalf("readonly role should include %s", PermAIGatewayView)
	}
	for _, permission := range []string{PermAIGatewayInvoke, PermAIGatewayManage} {
		if HasPermission([]string{"readonly"}, permission) {
			t.Fatalf("readonly role should not include %s", permission)
		}
	}
	for _, permission := range []string{PermAIGatewayView, PermAIGatewayInvoke, PermAIGatewayManage} {
		if HasPermission([]string{"auditor"}, permission) {
			t.Fatalf("auditor role should not include %s", permission)
		}
	}
}

func TestDefaultRolePermissionsAIGatewayRelay(t *testing.T) {
	SetRolePermissionMatrix(nil)

	for _, permission := range []string{PermAIGatewayRelayView, PermAIGatewayRelayInvoke, PermAIGatewayRelayManage} {
		for _, role := range []string{"admin", "ops"} {
			if !HasPermission([]string{role}, permission) {
				t.Fatalf("%s role should include %s", role, permission)
			}
		}
	}
	for _, permission := range []string{PermAIGatewayRelayView, PermAIGatewayRelayInvoke} {
		if !HasPermission([]string{"developer"}, permission) {
			t.Fatalf("developer role should include %s", permission)
		}
	}
	if HasPermission([]string{"developer"}, PermAIGatewayRelayManage) {
		t.Fatalf("developer role should not include %s", PermAIGatewayRelayManage)
	}
	if !HasPermission([]string{"readonly"}, PermAIGatewayRelayView) {
		t.Fatalf("readonly role should include %s", PermAIGatewayRelayView)
	}
	for _, permission := range []string{PermAIGatewayRelayInvoke, PermAIGatewayRelayManage} {
		if HasPermission([]string{"readonly"}, permission) {
			t.Fatalf("readonly role should not include %s", permission)
		}
	}
	for _, permission := range []string{PermAIGatewayRelayView, PermAIGatewayRelayInvoke, PermAIGatewayRelayManage} {
		if HasPermission([]string{"auditor"}, permission) {
			t.Fatalf("auditor role should not include %s", permission)
		}
	}
}

func TestDefaultRolePermissionsPlugins(t *testing.T) {
	SetRolePermissionMatrix(nil)

	for _, permission := range []string{PermPluginView, PermPluginInstall, PermPluginManage, PermPluginConfigureSecrets} {
		if !HasPermission([]string{"admin"}, permission) {
			t.Fatalf("admin role should include %s", permission)
		}
	}
	for _, permission := range []string{PermPluginView, PermPluginInstall, PermPluginManage, PermPluginConfigureSecrets} {
		if !HasPermission([]string{"ops"}, permission) {
			t.Fatalf("ops role should include %s", permission)
		}
	}
	for _, role := range []string{"developer", "readonly", "auditor"} {
		if !HasPermission([]string{role}, PermPluginView) {
			t.Fatalf("%s role should include %s", role, PermPluginView)
		}
		for _, permission := range []string{PermPluginInstall, PermPluginManage, PermPluginConfigureSecrets} {
			if HasPermission([]string{role}, permission) {
				t.Fatalf("%s role should not include %s", role, permission)
			}
		}
	}
}

func TestDefaultRolePermissionsVirtualizationViewGrants(t *testing.T) {
	SetRolePermissionMatrix(nil)

	virtualizationPermissions := []string{
		PermVirtualizationOverviewView,
		PermVirtualizationVMsView,
		PermVirtualizationClustersView,
		PermVirtualizationImagesView,
		PermVirtualizationFlavorsView,
		PermVirtualizationOperationsView,
		PermVirtualizationSyncView,
	}
	for _, permission := range virtualizationPermissions {
		if !HasPermission([]string{"admin"}, permission) {
			t.Fatalf("admin role should include %s", permission)
		}
		if !HasPermission([]string{"ops"}, permission) {
			t.Fatalf("ops role should include %s", permission)
		}
		for _, role := range []string{"developer", "readonly", "auditor"} {
			if HasPermission([]string{role}, permission) {
				t.Fatalf("%s role should not include %s", role, permission)
			}
		}
	}
}

func TestDefaultRolePermissionsVirtualizationManageGrants(t *testing.T) {
	SetRolePermissionMatrix(nil)

	adminOnlyPermissions := []string{
		PermVirtualizationClustersManage,
		PermVirtualizationFlavorsManage,
		PermVirtualizationOperationsManage,
	}
	for _, permission := range adminOnlyPermissions {
		if !HasPermission([]string{"admin"}, permission) {
			t.Fatalf("admin role should include %s", permission)
		}
		for _, role := range []string{"ops", "developer", "readonly", "auditor"} {
			if HasPermission([]string{role}, permission) {
				t.Fatalf("%s role should not include %s", role, permission)
			}
		}
	}

	for _, permission := range []string{PermVirtualizationVMsManage, PermVirtualizationSyncManage} {
		if !HasPermission([]string{"admin"}, permission) {
			t.Fatalf("admin role should include %s", permission)
		}
		if !HasPermission([]string{"ops"}, permission) {
			t.Fatalf("ops role should include %s", permission)
		}
		for _, role := range []string{"developer", "readonly", "auditor"} {
			if HasPermission([]string{role}, permission) {
				t.Fatalf("%s role should not include %s", role, permission)
			}
		}
	}
}
