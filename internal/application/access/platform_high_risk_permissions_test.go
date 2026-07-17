package access

import "testing"

func TestDefaultRolePermissionsForHighRiskKubernetesResources(t *testing.T) {
	SetRolePermissionMatrix(nil)
	permissions := []string{
		PermPlatformRBACManage,
		PermPlatformRBACEscalate,
		PermPlatformRBACBind,
		PermPlatformNamespacesManage,
		PermPlatformCRDsManage,
		PermPlatformAdmissionManage,
		PermPlatformClusterResourcesManage,
	}
	for _, permission := range permissions {
		if !HasPermission([]string{"admin"}, permission) {
			t.Errorf("admin should include %s", permission)
		}
		for _, role := range []string{"ops", "developer", "tester", "readonly", "auditor"} {
			if HasPermission([]string{role}, permission) {
				t.Errorf("%s must not receive high-risk permission %s by default", role, permission)
			}
		}
	}
}
