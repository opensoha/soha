package routes

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	directoryhandler "github.com/opensoha/soha/internal/api/handlers/directorysync"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type directoryOriginalRoles struct{ keys []string }

func (r directoryOriginalRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"owned": r.keys}, nil
}

func TestDirectoryOriginalLookupRequiresCurrentViewAndSyncIntersection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"view-only", "sync-only", "key-ceiling", "both"} {
		t.Run(scenario, func(t *testing.T) {
			keys := []string{appaccess.PermAccessDirectoryView, appaccess.PermAccessDirectorySync}
			principal := domainidentity.Principal{UserID: "actor", Roles: []string{"owned"}, AccessTokenID: "trusted-token"}
			if scenario == "view-only" {
				keys = keys[:1]
			}
			if scenario == "sync-only" {
				keys = keys[1:]
			}
			if scenario == "key-ceiling" {
				principal.PermissionKeys = []string{appaccess.PermAccessDirectoryView}
			}
			h := directoryhandler.New(nil, nil, nil)
			h.SetPermissionResolver(appaccess.NewPermissionResolver(directoryOriginalRoles{keys}))
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("principal", principal); c.Next() })
			registerDirectorySyncRoutes(router.Group("/api/v1"), Dependencies{DirectorySync: h})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/access/directory-connections/owned/sync-requests/1d397734-d897-484d-9009-21924e904080", nil))
			want := 403
			if scenario == "both" {
				want = 501
			}
			if recorder.Code != want {
				t.Fatalf("lookup current permission intersection: %s status=%d want=%d", scenario, recorder.Code, want)
			}
		})
	}
}
