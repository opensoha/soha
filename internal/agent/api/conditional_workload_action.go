package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

type conditionalWorkloadActionClient interface {
	ExecuteWorkloadActionConditional(context.Context, string, string, string, string, contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error)
}

func workloadActionPolicy(kind, action string) string {
	switch kind + "/" + action {
	case "Deployment/restart":
		return actionPlatformDeploymentRestart
	case "Deployment/scale":
		return actionPlatformDeploymentScale
	case "Deployment/rollback":
		return actionPlatformDeploymentRollback
	case "StatefulSet/restart":
		return actionPlatformStatefulSetRestart
	case "StatefulSet/scale":
		return actionPlatformStatefulSetScale
	case "DaemonSet/restart":
		return actionPlatformDaemonSetRestart
	default:
		return ""
	}
}

func registerConditionalWorkloadActionRoute(platform *gin.RouterGroup, client conditionalWorkloadActionClient, actions actionPolicy) {
	platform.POST("/preconditions-v1/resources/:kind/:name/actions/:action", func(c *gin.Context) {
		action := workloadActionPolicy(c.Param("kind"), c.Param("action"))
		if action == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "unsupported workload action")
			c.Abort()
			return
		}
		actions.Require(action)(c)
	}, func(c *gin.Context) {
		var input contractresource.ConditionalWorkloadActionInput
		if !decodeConditionalResource(c, &input) {
			return
		}
		if err := input.Validate(c.Param("kind"), c.Param("action")); err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
			return
		}
		item, err := client.ExecuteWorkloadActionConditional(c.Request.Context(), c.Query("namespace"), c.Param("kind"), c.Param("name"), c.Param("action"), input)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, http.StatusOK, item)
	})
}
