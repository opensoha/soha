package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
)

func TestRegisterPlatformConfigurationDetailRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerPlatformConfigurationRoutes(router.Group("/api/v1/platform"), &k8sagent.Client{}, actionPolicy{})
	registered := map[string]struct{}{}
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, path := range []string{
		"/api/v1/platform/configuration/hpas/:name/detail",
		"/api/v1/platform/configuration/poddisruptionbudgets/:name/detail",
		"/api/v1/platform/configuration/mutatingwebhookconfigurations/:name/detail",
		"/api/v1/platform/configuration/validatingwebhookconfigurations/:name/detail",
		"/api/v1/platform/configuration/resourcequotas/:name/detail",
		"/api/v1/platform/configuration/limitranges/:name/detail",
	} {
		if _, ok := registered[http.MethodGet+" "+path]; !ok {
			t.Errorf("GET %s is not registered", path)
		}
	}
}
