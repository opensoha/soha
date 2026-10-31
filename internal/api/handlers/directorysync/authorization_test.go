package directorysync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type directoryPermissionRoles struct{ keys []string }

func (s directoryPermissionRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"test-role": s.keys}, nil
}

func TestDirectoryManagementPermissionsRespectCredentialCeilingAndFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := []string{appaccess.PermAccessDirectoryView, appaccess.PermAccessDirectoryManage, appaccess.PermAccessDirectorySync, appaccess.PermAccessDirectoryPeopleManage, appaccess.PermAccessIdentityLinkManage}
	permissions := []string{appaccess.PermAccessDirectoryView, appaccess.PermAccessDirectorySync,
		appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, "create"),
		appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, "update"),
		appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, "delete"),
		appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, "validate"),
		appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryPeopleManage, "resolve"),
		appaccess.ManagedActionPermission(appaccess.PermAccessIdentityLinkManage, "unlink")}
	for _, permission := range permissions {
		for _, scenario := range []string{"permitted", "legacy-permitted", "view-only", "key-ceiling", "resolver-missing"} {
			t.Run(permission+"/"+scenario, func(t *testing.T) {
				keys := permissions
				principal := domainidentity.Principal{UserID: "test-user", Roles: []string{"test-role"}}
				if scenario == "legacy-permitted" {
					keys = legacy
				}
				if scenario == "view-only" {
					keys = []string{appaccess.PermAccessDirectoryView}
				}
				if scenario == "key-ceiling" {
					principal.PermissionKeys = []string{appaccess.PermAccessDirectoryView}
				}
				handler := New(nil, nil, nil)
				if scenario != "resolver-missing" {
					handler.SetPermissionResolver(appaccess.NewPermissionResolver(directoryPermissionRoles{keys}))
				}
				calls := 0
				router := gin.New()
				router.Use(func(c *gin.Context) { c.Set("principal", principal); c.Next() })
				router.POST("/directory-operation", handler.RequirePermission(permission), func(c *gin.Context) { calls++; c.Status(http.StatusNoContent) })
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/directory-operation", nil))
				allowed := scenario == "permitted" || scenario == "legacy-permitted" || permission == appaccess.PermAccessDirectoryView && scenario != "resolver-missing"
				if allowed && (recorder.Code != 204 || calls != 1) || !allowed && (recorder.Code != 403 || calls != 0) {
					t.Fatalf("allowed=%t status=%d calls=%d", allowed, recorder.Code, calls)
				}
			})
		}
	}
}
