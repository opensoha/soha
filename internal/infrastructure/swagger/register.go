package swagger

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func Register(router gin.IRoutes, enabled bool, path string) {
	if !enabled {
		return
	}
	router.GET(path, func(c *gin.Context) {
		suffix := strings.TrimSpace(c.Param("any"))
		if suffix == "" || suffix == "/" {
			c.Redirect(http.StatusTemporaryRedirect, strings.TrimSuffix(c.Request.URL.Path, "/")+"/openapi.json")
			return
		}
		c.JSON(http.StatusOK, openAPISpec())
	})
}

func openAPISpec() gin.H {
	return gin.H{
		"openapi": "3.0.3",
		"info": gin.H{
			"title":       "soha API",
			"version":     "0.1.2",
			"description": "soha platform console API surface",
		},
		"paths": gin.H{
			"/api/v1/auth/login":                                 gin.H{"post": gin.H{"summary": "Password login"}},
			"/api/v1/auth/refresh":                               gin.H{"post": gin.H{"summary": "Refresh session"}},
			"/api/v1/auth/me":                                    gin.H{"get": gin.H{"summary": "Current user"}},
			"/api/v1/auth/providers":                             gin.H{"get": gin.H{"summary": "List enabled login providers"}},
			"/api/v1/auth/login/{providerID}/start":              gin.H{"get": gin.H{"summary": "Begin third-party login"}},
			"/api/v1/auth/login/{providerID}/callback":           gin.H{"get": gin.H{"summary": "Handle third-party login callback"}},
			"/api/v1/clusters":                                   gin.H{"get": gin.H{"summary": "List clusters"}, "post": gin.H{"summary": "Create cluster"}},
			"/api/v1/clusters/{clusterID}/detail":                gin.H{"get": gin.H{"summary": "Cluster detail"}},
			"/api/v1/clusters/{clusterID}/workloads/pods":        gin.H{"get": gin.H{"summary": "List pods"}},
			"/api/v1/clusters/{clusterID}/workloads/deployments": gin.H{"get": gin.H{"summary": "List deployments"}},
			"/api/v1/clusters/{clusterID}/workloads/deployments/{deploymentName}/metrics": gin.H{"get": gin.H{"summary": "Deployment metrics"}},
			"/api/v1/clusters/{clusterID}/network/services/{serviceName}/metrics":         gin.H{"get": gin.H{"summary": "Service metrics"}},
			"/api/v1/alerts":                                            gin.H{"get": gin.H{"summary": "List alerts"}},
			"/api/v1/alerts/{alertID}":                                  gin.H{"get": gin.H{"summary": "Get alert detail"}},
			"/api/v1/alerts/{alertID}/acknowledge":                      gin.H{"post": gin.H{"summary": "Acknowledge alert"}},
			"/api/v1/alerts/{alertID}/ownership":                        gin.H{"put": gin.H{"summary": "Update alert ownership"}},
			"/api/v1/alert-silences":                                    gin.H{"get": gin.H{"summary": "List alert silences"}, "post": gin.H{"summary": "Create alert silence"}},
			"/api/v1/alert-delivery-logs":                               gin.H{"get": gin.H{"summary": "List alert delivery logs"}},
			"/api/v1/applications":                                      gin.H{"get": gin.H{"summary": "List applications"}, "post": gin.H{"summary": "Create application"}},
			"/api/v1/applications/{applicationID}/services":             gin.H{"get": gin.H{"summary": "List application services"}, "post": gin.H{"summary": "Create application service"}},
			"/api/v1/applications/{applicationID}/services/{serviceID}": gin.H{"get": gin.H{"summary": "Get application service"}, "put": gin.H{"summary": "Update application service"}, "delete": gin.H{"summary": "Delete application service"}},
			"/api/v1/builds":                                            gin.H{"get": gin.H{"summary": "List build records"}},
			"/api/v1/builds/trigger":                                    gin.H{"post": gin.H{"summary": "Trigger build"}},
			"/api/v1/releases":                                          gin.H{"get": gin.H{"summary": "List releases"}},
			"/api/v1/releases/trigger":                                  gin.H{"post": gin.H{"summary": "Trigger release"}},
			"/api/v1/settings/identity":                                 gin.H{"get": gin.H{"summary": "Login settings"}},
			"/api/v1/settings/identity/providers":                       gin.H{"put": gin.H{"summary": "Update login providers"}},
			"/api/v1/mcp/capabilities":                                  gin.H{"get": gin.H{"summary": "List MCP capabilities"}},
		},
	}
}
