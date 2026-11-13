package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type buildpacksCapabilityReader interface {
	BuildpacksCapability(context.Context, string) sohaapi.BuildpacksCapability
}

type cancelRuntimeTaskRequest struct {
	Reason string `json:"reason"`
}

func registerRuntimeRoutes(
	router *gin.Engine,
	cfg cfgpkg.Config,
	runtime RuntimeTaskController,
	actions actionPolicy,
) {
	if runtime == nil {
		return
	}
	runtimeGroup := router.Group(fmt.Sprintf("%s/runtime", cfg.HTTP.BasePath))
	runtimeGroup.Use(authAnyMiddleware(cfg.Auth.BearerToken, cfg.ControlPlane.BearerToken))
	{
		if reader, ok := runtime.(buildpacksCapabilityReader); ok {
			runtimeGroup.GET("/buildpacks/capability", func(c *gin.Context) {
				apiresponse.Item(c, http.StatusOK, reader.BuildpacksCapability(c.Request.Context(), c.Query("applicationId")))
			})
		}
		runtimeGroup.GET("/execution-tasks", func(c *gin.Context) {
			apiresponse.Items(c, http.StatusOK, runtime.ListActiveTasks())
		})
		runtimeGroup.GET("/execution-tasks/:taskID", func(c *gin.Context) {
			item, ok := runtime.GetActiveTask(c.Param("taskID"))
			if !ok {
				apiresponse.Error(c, http.StatusNotFound, "not_found", "runtime execution task not found")
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		runtimeGroup.POST(
			"/execution-tasks/:taskID/cancel",
			actions.Require(actionRuntimeExecutionTaskCancel),
			func(c *gin.Context) {
				var req cancelRuntimeTaskRequest
				_ = c.ShouldBindJSON(&req)
				if !runtime.CancelActiveTask(c.Param("taskID"), req.Reason) {
					apiresponse.Error(c, http.StatusNotFound, "not_found", "runtime execution task not found")
					return
				}
				apiresponse.JSON(c, http.StatusAccepted, gin.H{"status": "canceling"})
			},
		)
		if metrics, ok := runtime.(RuntimeMetricsController); ok {
			runtimeGroup.GET("/metrics", func(c *gin.Context) {
				apiresponse.Item(c, http.StatusOK, metrics.MetricsSnapshot())
			})
		}
	}
}
