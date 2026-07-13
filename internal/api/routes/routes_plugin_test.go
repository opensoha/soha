package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
)

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
		"POST /api/v1/plugins/install",
		"GET /api/v1/plugins/:pluginID",
		"DELETE /api/v1/plugins/:pluginID",
		"GET /api/v1/plugins/:pluginID/manifest",
		"POST /api/v1/plugins/:pluginID/enable",
		"POST /api/v1/plugins/:pluginID/disable",
		"POST /api/v1/plugins/:pluginID/upgrade",
		"PUT /api/v1/plugins/:pluginID/config",
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
