package routes

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	contractsopenapi "github.com/opensoha/soha-contracts/openapi"
)

type openAPIRouteOperation struct {
	Capability json.RawMessage `json:"x-soha-capability"`
}

type openAPIRoutePathItem struct {
	Get    *openAPIRouteOperation `json:"get"`
	Post   *openAPIRouteOperation `json:"post"`
	Put    *openAPIRouteOperation `json:"put"`
	Patch  *openAPIRouteOperation `json:"patch"`
	Delete *openAPIRouteOperation `json:"delete"`
}

func TestOpenAPICapabilityOperationsHaveLiveRoutes(t *testing.T) {
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

	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerProtectedRoutes(router.Group(document.Servers[0].URL), allRoutesEnabledConfig(), routeTestDependencies())
	liveRoutes := make(map[string]struct{})
	for _, route := range router.Routes() {
		liveRoutes[route.Method+" "+normalizeRoutePattern(route.Path)] = struct{}{}
	}

	checked := 0
	for path, pathItem := range document.Paths {
		for method, operation := range map[string]*openAPIRouteOperation{
			"GET": pathItem.Get, "POST": pathItem.Post, "PUT": pathItem.Put, "PATCH": pathItem.Patch, "DELETE": pathItem.Delete,
		} {
			if operation == nil || len(operation.Capability) == 0 {
				continue
			}
			checked++
			want := method + " " + normalizeRoutePattern(strings.TrimSuffix(document.Servers[0].URL, "/")+path)
			if _, ok := liveRoutes[want]; !ok {
				t.Errorf("OpenAPI capability operation has no live route: %s", want)
			}
		}
	}
	if checked == 0 {
		t.Fatal("OpenAPI has no x-soha-capability operations")
	}
}

func normalizeRoutePattern(path string) string {
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, ":") || (strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")) {
			segments[index] = ":param"
		}
	}
	return strings.Join(segments, "/")
}
