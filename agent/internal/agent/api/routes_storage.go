package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

func registerPlatformStorageRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	platform.GET("/storage/persistentvolumeclaims", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListPersistentVolumeClaims(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/storage/persistentvolumeclaims/:name/detail", func(c *gin.Context) {
		item, err := client.GetPersistentVolumeClaimDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/storage/persistentvolumes", func(c *gin.Context) {
		items, err := client.ListPersistentVolumes(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/storage/persistentvolumes/:name/detail", func(c *gin.Context) {
		item, err := client.GetPersistentVolumeDetail(c.Request.Context(), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/storage/storageclasses", func(c *gin.Context) {
		items, err := client.ListStorageClasses(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/storage/storageclasses/:name/detail", func(c *gin.Context) {
		item, err := client.GetStorageClassDetail(c.Request.Context(), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}
