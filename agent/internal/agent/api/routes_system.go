package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-agent/internal/agent/buildinfo"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	runnerpkg "github.com/opensoha/soha-agent/internal/agent/runner"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

func registerSystemRoutes(
	router *gin.Engine,
	cfg cfgpkg.Config,
	client *k8sagent.Client,
	runtime RuntimeTaskController,
) {
	registerActionDiagnostics(router, cfg, client)
	router.GET("/healthz", func(c *gin.Context) {
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET(fmt.Sprintf("%s/healthz", cfg.HTTP.BasePath), func(c *gin.Context) {
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})
	readyHandler := func(c *gin.Context) {
		if cfg.ControlPlane.Outpost.Enabled {
			outpost, ok := runtime.(interface {
				OutpostStatus() runnerpkg.OutpostStatus
			})
			if !ok || !outpost.OutpostStatus().Ready {
				apiresponse.JSON(c, http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
				return
			}
		}
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ready"})
	}
	router.GET("/readyz", readyHandler)
	router.GET(fmt.Sprintf("%s/readyz", cfg.HTTP.BasePath), readyHandler)
	buildInfoHandler := func(c *gin.Context) {
		apiresponse.Item(c, http.StatusOK, buildinfo.Current())
	}
	router.GET("/version", buildInfoHandler)
	router.GET(fmt.Sprintf("%s/version", cfg.HTTP.BasePath), buildInfoHandler)
	router.GET(fmt.Sprintf("%s/build-info", cfg.HTTP.BasePath), buildInfoHandler)
	router.GET(
		fmt.Sprintf("%s/diagnostics", cfg.HTTP.BasePath),
		authAnyMiddleware(cfg.Auth.BearerToken, cfg.ControlPlane.BearerToken),
		func(c *gin.Context) {
			apiresponse.Item(c, http.StatusOK, buildDiagnosticsView(cfg, client != nil, runtime))
		},
	)
}
