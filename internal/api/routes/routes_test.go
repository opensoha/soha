package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
)

func TestRegisterPlatformRoutesKeepsCoreOperationalSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerPlatformRoutes(group, Dependencies{Platform: &apiHandlers.PlatformHandler{}})

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range []string{
		"GET /api/v1/clusters/:clusterID/workloads/pods",
		"PUT /api/v1/clusters/:clusterID/workloads/pods/:podName/yaml",
		"POST /api/v1/clusters/:clusterID/workloads/deployments/restart",
		"GET /api/v1/clusters/:clusterID/network/services",
		"GET /api/v1/clusters/:clusterID/network/topology",
		"GET /api/v1/clusters/:clusterID/network/gatewayclasses",
		"GET /api/v1/clusters/:clusterID/network/gateways",
		"GET /api/v1/clusters/:clusterID/helm/releases/:releaseName/values",
		"POST /api/v1/clusters/:clusterID/helm/charts/install",
		"GET /api/v1/clusters/:clusterID/extensions/crds/:crdName/resources",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}

	for _, route := range []string{
		"GET /api/v1/clusters/:clusterID/network/httproutes",
		"GET /api/v1/clusters/:clusterID/network/backendtlspolicies",
		"GET /api/v1/clusters/:clusterID/network/grpcroutes",
		"GET /api/v1/clusters/:clusterID/network/referencegrants",
		"GET /api/v1/clusters/:clusterID/network/httproutes/:name/yaml",
		"PUT /api/v1/clusters/:clusterID/network/httproutes/:name/yaml",
		"DELETE /api/v1/clusters/:clusterID/network/httproutes/:name",
		"GET /api/v1/clusters/:clusterID/network/backendtlspolicies/:name/yaml",
		"PUT /api/v1/clusters/:clusterID/network/backendtlspolicies/:name/yaml",
		"DELETE /api/v1/clusters/:clusterID/network/backendtlspolicies/:name",
		"GET /api/v1/clusters/:clusterID/network/grpcroutes/:name/yaml",
		"PUT /api/v1/clusters/:clusterID/network/grpcroutes/:name/yaml",
		"DELETE /api/v1/clusters/:clusterID/network/grpcroutes/:name",
		"GET /api/v1/clusters/:clusterID/network/referencegrants/:name/yaml",
		"PUT /api/v1/clusters/:clusterID/network/referencegrants/:name/yaml",
		"DELETE /api/v1/clusters/:clusterID/network/referencegrants/:name",
	} {
		if _, ok := registered[route]; ok {
			t.Fatalf("raw Gateway API route should not be exposed: %s", route)
		}
	}
}
