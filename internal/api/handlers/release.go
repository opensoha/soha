package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
)

type ReleaseService interface {
	List(context.Context, domainidentity.Principal, domainrelease.Filter) ([]domainrelease.Record, error)
	Trigger(context.Context, domainidentity.Principal, domainrelease.TriggerInput) (domainrelease.Record, error)
}

type ReleaseHandler struct {
	service ReleaseService
}

func NewReleaseHandler(service ReleaseService) *ReleaseHandler {
	return &ReleaseHandler{service: service}
}

func (h *ReleaseHandler) ListReleases(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.List(c.Request.Context(), principal, domainrelease.Filter{
		ApplicationID: c.Query("applicationId"),
		ClusterID:     c.Query("clusterId"),
		Limit:         parseLimit(c.Query("limit"), 50),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ReleaseHandler) TriggerRelease(c *gin.Context) {
	var req dto.TriggerReleaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid release trigger payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Trigger(c.Request.Context(), principal, domainrelease.TriggerInput{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		ReleaseBundleID:          req.ReleaseBundleID,
		ClusterID:                req.ClusterID,
		Namespace:                req.Namespace,
		DeploymentName:           req.DeploymentName,
		ContainerName:            req.ContainerName,
		Image:                    req.Image,
		ImageTag:                 req.ImageTag,
		ReleaseName:              req.ReleaseName,
		ActionKind:               req.ActionKind,
		WorkflowRunID:            req.WorkflowRunID,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}
