package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domaindelivery "github.com/kubecrux/kubecrux/internal/domain/delivery"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
)

type DeliveryService interface {
	GetApplicationDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationDetail, error)
	GetApplicationEnvironmentDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationEnvironmentDetail, error)
	ListReleaseBoard(context.Context, domainidentity.Principal) ([]domaindelivery.ReleaseBoardEntry, error)
	ListTargetCandidates(context.Context, domainidentity.Principal, string, string, string) ([]domaindelivery.TargetCandidate, error)
}

type DeliveryHandler struct {
	service DeliveryService
}

func NewDeliveryHandler(service DeliveryService) *DeliveryHandler {
	return &DeliveryHandler{service: service}
}

func (h *DeliveryHandler) GetApplicationDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApplicationDetail(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetApplicationEnvironmentDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApplicationEnvironmentDetail(c.Request.Context(), principal, c.Param("applicationEnvironmentID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) ListReleaseBoard(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListReleaseBoard(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListTargetCandidates(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListTargetCandidates(c.Request.Context(), principal, c.Query("clusterId"), c.Query("namespace"), c.Query("search"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
