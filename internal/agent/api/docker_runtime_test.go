package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/soha/soha/internal/agent/config"
)

func TestDockerRuntimeRoutesRequireConfiguredToken(t *testing.T) {
	router := dockerRuntimeTestRouter(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/docker/runtime/logs", bytes.NewBufferString(`{}`)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDockerRuntimeRoutesRejectInvalidToken(t *testing.T) {
	router := dockerRuntimeTestRouter(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/runtime/logs", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer wrong-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDockerRuntimeRoutesAcceptAgentOrControlPlaneToken(t *testing.T) {
	router := dockerRuntimeTestRouter(cfgpkg.Config{
		HTTP:         cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth:         cfgpkg.AuthConfig{BearerToken: "agent-token"},
		ControlPlane: cfgpkg.ControlPlaneConfig{BearerToken: "control-token"},
	})

	for _, token := range []string{"agent-token", "control-token"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/runtime/logs", bytes.NewBufferString(`{}`))
		req.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code == http.StatusUnauthorized {
			t.Fatalf("token %q was rejected with status %d", token, recorder.Code)
		}
	}
}

func dockerRuntimeTestRouter(cfg cfgpkg.Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerDockerRuntimeRoutes(router, cfg)
	return router
}
