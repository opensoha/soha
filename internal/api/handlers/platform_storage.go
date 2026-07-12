package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func (h *persistentVolumeClaimResourceHandler) ListPersistentVolumeClaims(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListPersistentVolumeClaims(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *persistentVolumeClaimResourceHandler) GetPersistentVolumeClaimDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetPersistentVolumeClaimDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *persistentVolumeClaimResourceHandler) CreatePersistentVolumeClaim(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "PersistentVolumeClaim")
}
func (h *persistentVolumeResourceHandler) ListPersistentVolumes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListPersistentVolumes(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *persistentVolumeResourceHandler) GetPersistentVolumeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetPersistentVolumeDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *persistentVolumeResourceHandler) CreatePersistentVolume(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "PersistentVolume")
}
func (h *storageClassResourceHandler) ListStorageClasses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListStorageClasses(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *storageClassResourceHandler) GetStorageClassDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetStorageClassDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *storageClassResourceHandler) CreateStorageClass(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "StorageClass")
}
