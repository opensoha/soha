package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

func registerObservedWorkloadDeleteRoute(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	platform.DELETE("/resources/observed", actions.Require(actionPlatformResourcesDelete), func(c *gin.Context) {
		var req contractresource.ObservedWorkloadDeleteRequest
		if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Namespace) == "" || strings.TrimSpace(req.Name) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace, kind, name, and expectedUid are required")
			return
		}
		if err := client.DeleteObservedWorkload(c.Request.Context(), req.Namespace, req.Kind, req.Name, req.ExpectedUID); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}
