package api

import (
	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
	"net/http"
)

func registerPlatformStatefulSetHistoryRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	platform.GET("/workloads/statefulsets/:name/rollouts", func(c *gin.Context) {
		items, err := client.ListStatefulSetRolloutHistory(c.Request.Context(), c.DefaultQuery("namespace", "default"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/workloads/statefulsets/:name/rollouts/:revisionName/yaml", func(c *gin.Context) {
		item, err := client.GetStatefulSetRevisionYAML(c.Request.Context(), c.DefaultQuery("namespace", "default"), c.Param("name"), c.Param("revisionName"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}
