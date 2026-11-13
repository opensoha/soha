package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

func registerPlatformInventoryRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	platform.GET("/summary", func(c *gin.Context) {
		apiresponse.Item(c, http.StatusOK, client.Summary(c.Request.Context()))
	})
	platform.GET("/namespaces", func(c *gin.Context) {
		items, err := client.ListNamespaces(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/infrastructure/nodes", func(c *gin.Context) {
		items, err := client.ListNodes(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/infrastructure/nodes/:name/detail", func(c *gin.Context) {
		item, err := client.GetNodeDetail(c.Request.Context(), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/events", func(c *gin.Context) {
		namespace := c.Query("namespace")
		limit := parseLimit(c.Query("limit"), 20)
		items, err := client.ListClusterEvents(c.Request.Context(), namespace, limit)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
}
