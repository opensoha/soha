package routes

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	directorysynchandler "github.com/opensoha/soha/internal/api/handlers/directorysync"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdirectory "github.com/opensoha/soha/internal/application/directorysync"
	domaindirectory "github.com/opensoha/soha/internal/domain/directorysync"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
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
		"POST /api/v1/access/directory-connections/:connectionID/sync-requests",
		"GET /api/v1/access/directory-connections/:connectionID/sync-requests/:idempotencyKey",
		"POST /api/v1/access/directory-connections/:connectionID/runs/:runID/cancel",
		"GET /api/v1/access/directory-connections/:connectionID/runtime-status",
		"GET /api/v1/access/directory-connections/:connectionID/events",
		"POST /api/v1/access/directory-connections/:connectionID/events/:eventID/retry",
		"GET /api/v1/access/directory-runs/:runID",
		"GET /api/v1/access/directory-conflicts",
	} {
		if !registered[expected] {
			t.Fatalf("missing route %s", expected)
		}
	}
}

type directoryReadOnlyRoles struct{}

func (directoryReadOnlyRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"directory-reader": {appaccess.PermAccessDirectoryView}}, nil
}

type directoryActionRoles struct{ permission string }

func (r directoryActionRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"directory-actor": {r.permission}}, nil
}

type directoryRouteLookup struct {
	directorysynchandler.Repository
}

func (*directoryRouteLookup) GetConnection(context.Context, string) (domaindirectory.Connection, domaindirectory.Policy, error) {
	return domaindirectory.Connection{}, domaindirectory.Policy{}, apperrors.ErrNotFound
}

type directoryRouteConnections struct{}

func (directoryRouteConnections) CreateConnection(context.Context, domainidentity.Principal, appdirectory.ConnectionInput) (domaindirectory.Connection, domaindirectory.Policy, error) {
	return domaindirectory.Connection{}, domaindirectory.Policy{}, apperrors.ErrInvalidArgument
}

func (directoryRouteConnections) UpdateConnection(context.Context, domainidentity.Principal, string, appdirectory.ConnectionInput) (domaindirectory.Connection, domaindirectory.Policy, error) {
	return domaindirectory.Connection{}, domaindirectory.Policy{}, apperrors.ErrInvalidArgument
}

func (directoryRouteConnections) ValidateConnection(context.Context, domainidentity.Principal, string) (domaindirectory.Connection, error) {
	return domaindirectory.Connection{}, apperrors.ErrNotFound
}

func TestDirectoryRoutesUseCanonicalPerActionPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	routes := []struct {
		method, path, action string
		allowedStatus        int
	}{
		{"POST", "/access/directory-connections", "create", 400},
		{"PUT", "/access/directory-connections/owned", "update", 400},
		// Deletion is an existing explicit unsupported API, excluded from CLI.
		{"DELETE", "/access/directory-connections/owned", "delete", 501},
		{"POST", "/access/directory-connections/owned/validate", "validate", 404},
	}
	for _, grant := range []string{"create", "update", "delete", "validate", "sync", "legacy"} {
		permission := appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, grant)
		if grant == "legacy" {
			permission = appaccess.PermAccessDirectoryManage
		}
		for _, route := range routes {
			t.Run(grant+"/"+route.action, func(t *testing.T) {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("principal", domainidentity.Principal{UserID: "actor", Roles: []string{"directory-actor"}})
					c.Next()
				})
				handler := directorysynchandler.New(&directoryRouteLookup{}, nil, nil, directoryRouteConnections{})
				handler.SetPermissionResolver(appaccess.NewPermissionResolver(directoryActionRoles{permission}))
				registerDirectorySyncRoutes(router.Group("/api/v1"), Dependencies{DirectorySync: handler})
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(route.method, "/api/v1"+route.path, strings.NewReader(`{}`))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(recorder, request)
				want := 403
				if grant == route.action || grant == "legacy" {
					want = route.allowedStatus
				}
				if recorder.Code != want {
					t.Fatalf("canonical grant %s to %s: status=%d want=%d", permission, route.action, recorder.Code, want)
				}
			})
		}
	}
}

func TestDirectoryViewCannotReachAnyManagementMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery(), func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{UserID: "reader", Roles: []string{"directory-reader"}})
		c.Next()
	})
	handler := &directorysynchandler.Handler{}
	handler.SetPermissionResolver(appaccess.NewPermissionResolver(directoryReadOnlyRoles{}))
	registerDirectorySyncRoutes(router.Group("/api/v1"), Dependencies{DirectorySync: handler})
	mutations := 0
	for _, route := range router.Routes() {
		if route.Method == "GET" {
			continue
		}
		path := route.Path
		for _, param := range []string{"connectionID", "eventID", "runID", "conflictID", "identityID", "suppressionID"} {
			path = strings.ReplaceAll(path, ":"+param, "fixture-id")
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(route.Method, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != 403 {
			t.Errorf("view grant reached mutation %s %s: %d", route.Method, route.Path, recorder.Code)
		}
		mutations++
	}
	if mutations != 13 {
		t.Fatalf("review every directory mutation: %d", mutations)
	}
}

func TestAllDirectoryManagementRoutesDenyWithoutPermissionResolver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	registerDirectorySyncRoutes(router.Group("/api/v1"), Dependencies{DirectorySync: &directorysynchandler.Handler{}})
	count := 0
	for _, route := range router.Routes() {
		path := route.Path
		for _, param := range []string{"connectionID", "eventID", "runID", "conflictID", "identityID", "suppressionID", "idempotencyKey"} {
			path = strings.ReplaceAll(path, ":"+param, "fixture-id")
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(route.Method, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != 403 {
			t.Errorf("unguarded directory entry %s %s: %d", route.Method, route.Path, recorder.Code)
		}
		count++
	}
	if count != 21 {
		t.Fatalf("review every directory management route: count=%d", count)
	}
}
