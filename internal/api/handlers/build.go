package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type BuildService interface {
	List(context.Context, domainidentity.Principal, domainbuild.Filter) ([]domainbuild.Record, error)
	Get(context.Context, domainidentity.Principal, string) (domainbuild.Record, error)
	Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error)
}

type BuildHandler struct {
	service BuildService
}

func NewBuildHandler(service BuildService) *BuildHandler {
	return &BuildHandler{service: service}
}

func (h *BuildHandler) ListBuilds(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.List(c.Request.Context(), principal, domainbuild.Filter{
		ApplicationID: c.Query("applicationId"),
		Limit:         parseLimit(c.Query("limit"), 50),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *BuildHandler) GetBuild(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Get(c.Request.Context(), principal, c.Param("buildID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *BuildHandler) TriggerBuild(c *gin.Context) {
	var req dto.TriggerBuildRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid build trigger payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Trigger(c.Request.Context(), principal, domainbuild.TriggerInput{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		ServiceID:                req.ServiceID,
		RepositoryID:             req.RepositoryID,
		BuildSourceID:            req.BuildSourceID,
		RefType:                  req.RefType,
		RefName:                  req.RefName,
		ImageTag:                 req.ImageTag,
		BuildArgs:                req.BuildArgs,
		Variables:                req.Variables,
		ResolvedCommit:           req.ResolvedCommit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}
