package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
)

func (h *PlatformHandler) ListPersistentVolumeClaims(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListPersistentVolumeClaims(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetPersistentVolumeClaimDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetPersistentVolumeClaimDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) CreatePersistentVolumeClaim(c *gin.Context) {
	h.createResourceFromYAML(c, "PersistentVolumeClaim")
}
func (h *PlatformHandler) ListPersistentVolumes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListPersistentVolumes(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetPersistentVolumeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetPersistentVolumeDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) CreatePersistentVolume(c *gin.Context) {
	h.createResourceFromYAML(c, "PersistentVolume")
}
func (h *PlatformHandler) ListStorageClasses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListStorageClasses(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetStorageClassDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetStorageClassDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) CreateStorageClass(c *gin.Context) {
	h.createResourceFromYAML(c, "StorageClass")
}
