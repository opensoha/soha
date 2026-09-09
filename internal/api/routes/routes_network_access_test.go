package routes

import (
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
)

func TestRegisterNetworkAccessRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerNetworkAccessRoutes(router.Group("/api/v1"), Dependencies{NetworkAccess: &apiHandlers.NetworkAccessHandler{}})
	registered := routeMethodPaths(router.Routes())
	for _, route := range []string{
		"GET /api/v1/network-access/devices",
		"GET /api/v1/network-access/devices/:deviceID",
		"PUT /api/v1/network-access/devices/:deviceID",
		"GET /api/v1/network-access/connection-options",
		"GET /api/v1/network-access/sites",
		"POST /api/v1/network-access/sites",
		"GET /api/v1/network-access/sites/:siteID",
		"PUT /api/v1/network-access/sites/:siteID",
		"DELETE /api/v1/network-access/sites/:siteID",
		"GET /api/v1/network-access/spaces",
		"POST /api/v1/network-access/spaces",
		"GET /api/v1/network-access/spaces/:spaceID",
		"PUT /api/v1/network-access/spaces/:spaceID",
		"DELETE /api/v1/network-access/spaces/:spaceID",
		"GET /api/v1/network-access/resources",
		"POST /api/v1/network-access/resources",
		"GET /api/v1/network-access/resources/:resourceID",
		"PUT /api/v1/network-access/resources/:resourceID",
		"DELETE /api/v1/network-access/resources/:resourceID",
		"GET /api/v1/network-access/gateways",
		"POST /api/v1/network-access/gateways",
		"GET /api/v1/network-access/gateways/:gatewayID",
		"PUT /api/v1/network-access/gateways/:gatewayID",
		"GET /api/v1/network-access/mihomo-profiles",
		"POST /api/v1/network-access/mihomo-profiles",
		"GET /api/v1/network-access/mihomo-profiles/:profileID",
		"PUT /api/v1/network-access/mihomo-profiles/:profileID",
		"DELETE /api/v1/network-access/mihomo-profiles/:profileID",
		"GET /api/v1/network-access/nas-bindings",
		"POST /api/v1/network-access/nas-bindings",
		"GET /api/v1/network-access/nas-bindings/:bindingID",
		"PUT /api/v1/network-access/nas-bindings/:bindingID",
		"DELETE /api/v1/network-access/nas-bindings/:bindingID",
		"GET /api/v1/network-access/site-profile-bindings",
		"POST /api/v1/network-access/site-profile-bindings",
		"GET /api/v1/network-access/site-profile-bindings/:bindingID",
		"PUT /api/v1/network-access/site-profile-bindings/:bindingID",
		"DELETE /api/v1/network-access/site-profile-bindings/:bindingID",
		"GET /api/v1/network-access/enrollments",
		"POST /api/v1/network-access/enrollments",
		"GET /api/v1/network-access/enrollments/:enrollmentID",
		"POST /api/v1/network-access/enrollments/:enrollmentID/revoke",
		"GET /api/v1/network-access/sessions",
		"GET /api/v1/network-access/telemetry/summary",
		"GET /api/v1/network-access/sessions/:sessionID",
		"POST /api/v1/network-access/sessions/:sessionID/actions/plan",
		"POST /api/v1/network-access/sessions/:sessionID/actions/execute",
		"GET /api/v1/network-access/access-grants",
		"POST /api/v1/network-access/access-grants",
		"GET /api/v1/network-access/access-grants/:grantID",
		"POST /api/v1/network-access/access-grants/:grantID/revoke",
		"GET /api/v1/network-access/policies",
		"POST /api/v1/network-access/policies",
		"POST /api/v1/network-access/policies/compile",
		"GET /api/v1/network-access/policies/:policyID",
		"PUT /api/v1/network-access/policies/:policyID",
		"DELETE /api/v1/network-access/policies/:policyID",
		"GET /api/v1/network-access/policy/snapshot",
		"POST /api/v1/network-access/policy/preview",
		"POST /api/v1/network-access/conflicts/analyze",
	} {
		if !slices.Contains(registered, route) {
			t.Fatalf("missing route %s", route)
		}
	}
}
