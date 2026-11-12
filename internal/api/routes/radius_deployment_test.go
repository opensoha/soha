package routes

import "testing"

func TestRadiusMutationSecuritySurface(t *testing.T) {
	for _, tc := range []struct{ method, path, action string }{{"POST", "/api/v1/network-access/radius-pools", "create"}, {"PUT", "/api/v1/network-access/radius-pools/:poolID/configuration", "update"}, {"POST", "/api/v1/network-access/radius-pools/:poolID/control", "update"}} {
		e, ok := nonPlatformMutationSecuritySurface(tc.method, tc.path)
		if !ok || !e.ScopeRequired || e.ResourceKind != "NetworkRADIUSPool" || e.PermissionKey != "network_access.nas_bindings."+tc.action {
			t.Fatalf("wrong mutation coverage: %+v", e)
		}
	}
}
