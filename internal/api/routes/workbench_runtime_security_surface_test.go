package routes

import (
	"net/http"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
)

func TestWorkbenchRuntimeMutationSecuritySurface(t *testing.T) {
	settingsUpdate := appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")
	for _, tc := range []struct {
		method, path, kind, action, permission string
	}{
		{http.MethodPut, "/api/v1/settings/ai/workbench-runtime-profiles/:profileId", "WorkbenchRuntimeProfile", "publish", settingsUpdate},
		{http.MethodPut, "/api/v1/settings/ai/workbench-runtime-assets/:kind/:assetId/:version", "WorkbenchRuntimeAsset", "update", settingsUpdate},
		{http.MethodPost, "/api/v1/settings/ai/workbench-runtime-assets/mcp/:assetId/:version/probe", "WorkbenchRuntimeAsset", "probe", settingsUpdate},
		{http.MethodPut, "/api/v1/copilot/sessions/:sessionID/runtime-profile", "WorkbenchRuntimeProfile", "select", appaccess.PermObserveAIChatUse},
	} {
		t.Run(tc.action, func(t *testing.T) {
			entry, ok := nonPlatformMutationSecuritySurface(tc.method, tc.path)
			if !ok || entry.ResourceKind != tc.kind || entry.Action != tc.action || entry.PermissionKey != tc.permission {
				t.Fatalf("%s %s security surface = %#v, want %s %s %s", tc.method, tc.path, entry, tc.kind, tc.action, tc.permission)
			}
		})
	}
}
