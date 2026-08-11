package routes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	contractsopenapi "github.com/opensoha/soha-contracts/openapi"
)

const platformRouteSurfacePath = "testdata/platform_route_surface.json"

type platformRouteSurfaceEntry struct {
	Method         string `json:"method"`
	Path           string `json:"path"`
	Classification string `json:"classification"`
}

type platformRouteSurfaceSnapshot struct {
	Version int                         `json:"version"`
	Routes  []platformRouteSurfaceEntry `json:"routes"`
}

func TestPlatformRouteSurfaceMatchesReviewedSnapshot(t *testing.T) {
	current := currentPlatformRouteSurface(t)
	if os.Getenv("UPDATE_PLATFORM_ROUTE_SURFACE") == "1" {
		payload, err := json.MarshalIndent(platformRouteSurfaceSnapshot{Version: 1, Routes: current}, "", "  ")
		if err != nil {
			t.Fatalf("encode platform route surface: %v", err)
		}
		if err := os.WriteFile(filepath.Clean(platformRouteSurfacePath), append(payload, '\n'), 0o644); err != nil {
			t.Fatalf("write platform route surface: %v", err)
		}
		t.Skip("updated platform route surface")
	}

	payload, err := os.ReadFile(filepath.Clean(platformRouteSurfacePath))
	if err != nil {
		t.Fatalf("read platform route surface: %v", err)
	}
	var snapshot platformRouteSurfaceSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("decode platform route surface: %v", err)
	}
	if snapshot.Version != 1 || len(snapshot.Routes) == 0 {
		t.Fatal("platform route surface snapshot is empty or unsupported")
	}
	if !reflect.DeepEqual(snapshot.Routes, current) {
		t.Fatalf("platform route surface changed; review the public/internal classification and run UPDATE_PLATFORM_ROUTE_SURFACE=1 go test ./internal/api/routes -run TestPlatformRouteSurfaceMatchesReviewedSnapshot")
	}
}

func currentPlatformRouteSurface(t *testing.T) []platformRouteSurfaceEntry {
	t.Helper()
	publicRoutes := publicOpenAPIPlatformRoutes(t)
	streamRoutes := map[string]struct{}{
		"GET /api/v1/clusters/:param/logs/stream":                       {},
		"GET /api/v1/clusters/:param/workloads/pods/:param/logs/stream": {},
		"GET /api/v1/clusters/:param/workloads/pods/:param/terminal":    {},
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerProtectedRoutes(router.Group("/api/v1"), allRoutesEnabledConfig(), routeTestDependencies())
	entries := make([]platformRouteSurfaceEntry, 0, len(router.Routes()))
	for _, route := range router.Routes() {
		if route.Path != "/api/v1/clusters" && !strings.HasPrefix(route.Path, "/api/v1/clusters/") {
			continue
		}
		classification := "internal"
		key := route.Method + " " + normalizeRoutePattern(route.Path)
		if _, ok := publicRoutes[key]; ok {
			classification = "public"
			delete(publicRoutes, key)
		} else if _, ok := streamRoutes[key]; ok {
			classification = "stream-only"
			delete(streamRoutes, key)
		}
		entries = append(entries, platformRouteSurfaceEntry{
			Method:         route.Method,
			Path:           route.Path,
			Classification: classification,
		})
	}
	if len(publicRoutes) > 0 {
		missing := make([]string, 0, len(publicRoutes))
		for route := range publicRoutes {
			missing = append(missing, route)
		}
		sort.Strings(missing)
		t.Fatalf("public platform OpenAPI operations have no live route: %s", strings.Join(missing, ", "))
	}
	if len(streamRoutes) > 0 {
		missing := make([]string, 0, len(streamRoutes))
		for route := range streamRoutes {
			missing = append(missing, route)
		}
		sort.Strings(missing)
		t.Fatalf("reviewed platform stream routes have no live route: %s", strings.Join(missing, ", "))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path == entries[j].Path {
			return entries[i].Method < entries[j].Method
		}
		return entries[i].Path < entries[j].Path
	})
	return entries
}

func publicOpenAPIPlatformRoutes(t *testing.T) map[string]struct{} {
	t.Helper()
	var document struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		Paths map[string]openAPIRoutePathItem `json:"paths"`
	}
	if err := json.Unmarshal(contractsopenapi.JSON(), &document); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	if len(document.Servers) == 0 {
		t.Fatal("OpenAPI has no server base path")
	}

	basePath := strings.TrimSuffix(document.Servers[0].URL, "/")
	routes := make(map[string]struct{})
	for path, pathItem := range document.Paths {
		if path != "/clusters" && !strings.HasPrefix(path, "/clusters/") {
			continue
		}
		for method, operation := range map[string]*openAPIRouteOperation{
			"GET": pathItem.Get, "POST": pathItem.Post, "PUT": pathItem.Put, "PATCH": pathItem.Patch, "DELETE": pathItem.Delete,
		} {
			if operation != nil {
				if strings.TrimSpace(operation.OperationID) == "" {
					t.Fatalf("public platform OpenAPI operation has no operationId: %s %s", method, path)
				}
				routes[method+" "+normalizeRoutePattern(basePath+path)] = struct{}{}
			}
		}
	}
	return routes
}
