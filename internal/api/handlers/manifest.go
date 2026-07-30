package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

type ManifestService interface {
	List(context.Context, domainidentity.Principal, domainmanifest.Filter) (domainmanifest.Page, error)
	Get(context.Context, domainidentity.Principal, string) (domainmanifest.Package, error)
	Create(context.Context, domainidentity.Principal, domainmanifest.Input) (domainmanifest.Package, error)
	Update(context.Context, domainidentity.Principal, string, domainmanifest.Input) (domainmanifest.Package, error)
	Delete(context.Context, domainidentity.Principal, string) error
	Publish(context.Context, domainidentity.Principal, string, string) (domainmanifest.Package, error)
	ListRevisions(context.Context, domainidentity.Principal, string) ([]domainmanifest.Revision, error)
}

type ManifestHandler struct{ service ManifestService }

func NewManifestHandler(service ManifestService) *ManifestHandler {
	return &ManifestHandler{service: service}
}

func (h *ManifestHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))
	items, err := h.service.List(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domainmanifest.Filter{ApplicationID: c.Query("applicationId"), ClusterID: c.Query("clusterId"), Namespace: c.Query("namespace"), Search: c.Query("search"), Page: page, PageSize: pageSize, Limit: limit})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, items)
}

func (h *ManifestHandler) Get(c *gin.Context) {
	item, err := h.service.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ManifestHandler) Create(c *gin.Context) {
	var input domainmanifest.Input
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest package payload")
		return
	}
	item, err := h.service.Create(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *ManifestHandler) Update(c *gin.Context) {
	var input domainmanifest.Input
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest package payload")
		return
	}
	item, err := h.service.Update(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ManifestHandler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *ManifestHandler) Publish(c *gin.Context) {
	var request struct {
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid publish payload")
		return
	}
	item, err := h.service.Publish(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"), request.Note)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ManifestHandler) ListRevisions(c *gin.Context) {
	items, err := h.service.ListRevisions(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
