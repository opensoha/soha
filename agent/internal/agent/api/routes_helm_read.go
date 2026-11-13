package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

func registerPlatformHelmReadRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	platform.GET("/helm/releases", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListHelmReleases(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/helm/releases/:name/detail", func(c *gin.Context) {
		namespace := c.Query("namespace")
		item, err := client.GetHelmReleaseDetail(c.Request.Context(), namespace, c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/helm/releases/:name/history", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListHelmReleaseHistory(c.Request.Context(), namespace, c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/helm/releases/:name/values", func(c *gin.Context) {
		namespace := c.Query("namespace")
		revision := c.Query("revision")
		item, err := client.GetHelmReleaseValues(c.Request.Context(), namespace, c.Param("name"), revision)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/helm/releases/:name/manifest", func(c *gin.Context) {
		item, err := client.GetHelmReleaseManifest(c.Request.Context(), c.Query("namespace"), c.Param("name"), c.Query("revision"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}
