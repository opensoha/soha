package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type TemplateSourceService interface {
	List(context.Context, domainidentity.Principal, int, int) ([]domaindocument.Source, error)
	Get(context.Context, domainidentity.Principal, string) (domaindocument.Source, error)
	Save(context.Context, domainidentity.Principal, string, domaindocument.SourceInput) (domaindocument.Source, error)
	Objects(context.Context, domainidentity.Principal, string, int, int) ([]domaindocument.Association, error)
	SourceInfo(context.Context, domainidentity.Principal, string, string, int64) (domaindocument.SourceInfo, error)
	Remove(context.Context, domainidentity.Principal, string, string, string, domaindocument.SourceRemoveInput) error
	Sync(context.Context, domainidentity.Principal, string, domaindocument.SyncInput) (domaindocument.SyncRun, error)
	Runs(context.Context, domainidentity.Principal, string, int, int) ([]domaindocument.SyncRun, error)
	Run(context.Context, domainidentity.Principal, string, string) (domaindocument.SyncRun, error)
	Apply(context.Context, domainidentity.Principal, string, string, domaindocument.SyncApplyInput) (domaindocument.SyncRun, error)
}

type TemplateSourceHandler struct{ service TemplateSourceService }

func NewTemplateSourceHandler(service TemplateSourceService) *TemplateSourceHandler {
	return &TemplateSourceHandler{service: service}
}

func (h *TemplateSourceHandler) List(c *gin.Context) {
	offset, limit, ok := templateSourcePagination(c)
	if !ok {
		return
	}
	items, err := h.service.List(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), offset, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, items)
}

func (h *TemplateSourceHandler) Get(c *gin.Context) {
	item, err := h.service.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *TemplateSourceHandler) Save(c *gin.Context) {
	var input domaindocument.SourceInput
	if !decodeDeliveryDocumentRequest(c, &input, 64<<10) {
		return
	}
	item, err := h.service.Save(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if c.Param("sourceID") == "" {
		status = http.StatusCreated
	}
	apiresponse.Item(c, status, item)
}

func (h *TemplateSourceHandler) Objects(c *gin.Context) {
	offset, limit, ok := templateSourcePagination(c)
	if !ok {
		return
	}
	items, err := h.service.Objects(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceID"), offset, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, items)
}

func (h *TemplateSourceHandler) SourceInfo(c *gin.Context) {
	version := int64(0)
	if value, supplied := c.GetQuery("version"); supplied {
		var err error
		version, err = strconv.ParseInt(value, 10, 64)
		if err != nil || version < 1 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "version must be a positive integer")
			return
		}
	}
	item, err := h.service.SourceInfo(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("kind"), c.Param("objectID"), version)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *TemplateSourceHandler) Remove(c *gin.Context) {
	var input domaindocument.SourceRemoveInput
	if !decodeDeliveryDocumentRequest(c, &input, 4096) {
		return
	}
	err := h.service.Remove(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceID"), c.Param("kind"), c.Param("objectID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *TemplateSourceHandler) Sync(c *gin.Context) {
	var input domaindocument.SyncInput
	if !decodeDeliveryDocumentRequest(c, &input, 4096) {
		return
	}
	item, err := h.service.Sync(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *TemplateSourceHandler) Runs(c *gin.Context) {
	offset, limit, ok := templateSourcePagination(c)
	if !ok {
		return
	}
	items, err := h.service.Runs(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceID"), offset, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, items)
}

func (h *TemplateSourceHandler) Run(c *gin.Context) {
	item, err := h.service.Run(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceID"), c.Param("runID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *TemplateSourceHandler) Apply(c *gin.Context) {
	var input domaindocument.SyncApplyInput
	if !decodeDeliveryDocumentRequest(c, &input, 4096) {
		return
	}
	item, err := h.service.Apply(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceID"), c.Param("runID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func templateSourcePagination(c *gin.Context) (int, int, bool) {
	offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if offsetErr != nil || limitErr != nil || offset < 0 || limit < 1 || limit > 200 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "offset must be nonnegative and limit must be between 1 and 200")
		return 0, 0, false
	}
	return offset, limit, true
}
