package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
)

type manifestRolloutExecutor interface {
	ExecuteManifestTask(context.Context, sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error)
}

func registerManifestRolloutRoutes(platform *gin.RouterGroup, client manifestRolloutExecutor, clusterID string, actions actionPolicy) {
	handle := func(expected sohaapi.ManifestTaskAction) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Header("Cache-Control", "no-store")
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
			var input sohaapi.ManifestExecutionTaskPayload
			decoder := json.NewDecoder(c.Request.Body)
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Action != expected || input.ClusterID != clusterID || !resourceruntime.IsRolloutTask(input) || (expected == sohaapi.ManifestTaskActionObserve && input.RolloutControl != nil) {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid rollout task input")
				return
			}
			item, err := client.ExecuteManifestTask(c.Request.Context(), input)
			if err != nil {
				apiresponse.Error(c, http.StatusConflict, "conflict", "rollout does not match the expected deployment or control state")
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		}
	}
	platform.POST("/manifests/rollout/observe", handle(sohaapi.ManifestTaskActionObserve))
	platform.POST("/manifests/rollout/control", actions.Require(actionPlatformCustomResourcesApply), handle(sohaapi.ManifestTaskActionRolloutControl))
}
