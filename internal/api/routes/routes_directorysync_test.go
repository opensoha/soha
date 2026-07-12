package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	directorysynchandler "github.com/opensoha/soha/internal/api/handlers/directorysync"
)

func TestRegisterDirectorySyncRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerDirectorySyncRoutes(router.Group("/api/v1"), Dependencies{DirectorySync: &directorysynchandler.Handler{}})
	registered := map[string]bool{}
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, expected := range []string{
		"GET /api/v1/access/directory-connections",
		"POST /api/v1/access/directory-connections/:connectionID/validate",
		"POST /api/v1/access/directory-connections/:connectionID/sync/preview",
		"POST /api/v1/access/directory-connections/:connectionID/sync",
		"POST /api/v1/access/directory-connections/:connectionID/runs/:runID/cancel",
		"GET /api/v1/access/directory-runs/:runID",
		"GET /api/v1/access/directory-conflicts",
	} {
		if !registered[expected] {
			t.Fatalf("missing route %s", expected)
		}
	}
}
