package routes

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	appaccess "github.com/opensoha/soha/internal/application/access"
)

func TestRepositoryPluginSecuritySurface(t *testing.T) {
	for _, path := range []string{"/api/v1/plugins/repository/preview", "/api/v1/plugins/repository/refs"} {
		if !isReadOnlyPOST(http.MethodPost, path) {
			t.Fatalf("%s must be classified as read-only", path)
		}
	}
	for path, permission := range map[string]string{
		"/api/v1/plugins/repository/install":           appaccess.PermPluginInstall,
		"/api/v1/plugins/:pluginID/repository/upgrade": appaccess.PermPluginUpgrade,
	} {
		entry, ok := nonPlatformMutationSecuritySurface(http.MethodPost, path)
		if !ok || entry.PermissionKey != permission || !entry.AuditRequired {
			t.Fatalf("%s must have permission %s and audit coverage, got %#v", path, permission, entry)
		}
	}
}

func TestRegisterPluginRoutesPreservesExtensionCenterAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	registerPluginRoutes(router.Group("/api/v1"), Dependencies{Plugins: &apiHandlers.PluginHandler{}})

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range []string{
		"GET /api/v1/plugins/marketplace",
		"GET /api/v1/plugins/marketplace/:pluginID",
		"GET /api/v1/plugins/installed",
		"GET /api/v1/plugins/updates",
		"POST /api/v1/plugins/install",
		"POST /api/v1/plugins/repository/refs",
		"POST /api/v1/plugins/repository/preview",
		"POST /api/v1/plugins/repository/install",
		"GET /api/v1/plugins/:pluginID",
		"DELETE /api/v1/plugins/:pluginID",
		"GET /api/v1/plugins/:pluginID/manifest",
		"POST /api/v1/plugins/:pluginID/enable",
		"POST /api/v1/plugins/:pluginID/disable",
		"POST /api/v1/plugins/:pluginID/upgrade",
		"POST /api/v1/plugins/:pluginID/repository/upgrade",
		"PUT /api/v1/plugins/:pluginID/config",
		"POST /api/v1/plugins/:pluginID/activate",
		"POST /api/v1/plugins/:pluginID/rollback",
		"GET /api/v1/plugins/:pluginID/assets/*assetPath",
		"GET /api/v1/extensions/runtime",
		"GET /api/v1/extensions/resource",
		"GET /api/v1/extensions/metrics",
		"GET /api/v1/extensions/alerts",
		"GET /api/v1/extensions/ai",
		"GET /api/v1/extensions/auth",
		"GET /api/v1/extensions/identity",
		"GET /api/v1/extensions/ui",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing extension center API route %s", route)
		}
	}
}

func TestCompanionRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerCompanionRoutes(router.Group("/api/v1"), Dependencies{
		Companion: apiHandlers.NewCompanionHandler(nil),
	})
	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"GET /api/v1/companion/profile",
		"POST /api/v1/companion/interactions",
		"POST /api/v1/companion/profile/reset",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing companion API route %s", route)
		}
	}
}
