package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DeliveryReferenceCandidateService interface {
	ListEnvironmentServiceReferenceCandidates(context.Context, domainidentity.Principal, string, string, int) (domaindelivery.ServiceReferenceCandidatePage, error)
	GetEnvironmentServiceReferenceCandidate(context.Context, domainidentity.Principal, string, string) (domaindelivery.ServiceReferenceCandidate, error)
	PutEnvironmentServiceReferenceCandidate(context.Context, domainidentity.Principal, string, string, domaindelivery.ServiceReferenceCandidateInput) (domaindelivery.ServiceReferenceCandidate, error)
	DeleteEnvironmentServiceReferenceCandidate(context.Context, domainidentity.Principal, string, string, domaindelivery.ServiceReferenceCandidateDeleteInput) error
}

func (h *DeliveryHandler) ListEnvironmentServiceReferenceCandidates(c *gin.Context) {
	service, ok := h.referenceCandidateService(c)
	if !ok {
		return
	}
	query := c.Request.URL.Query()
	for key, values := range query {
		if (key != "limit" && key != "cursor") || len(values) != 1 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid candidate metadata query")
			return
		}
	}
	limit := 50
	if value, provided := query["limit"]; provided {
		var err error
		limit, err = strconv.Atoi(value[0])
		if err != nil || limit < 1 || limit > 100 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "candidate page limit must be from 1 to 100")
			return
		}
	}
	page, err := service.ListEnvironmentServiceReferenceCandidates(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Query("cursor"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *DeliveryHandler) referenceCandidateService(c *gin.Context) (DeliveryReferenceCandidateService, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	service, ok := h.applications.(DeliveryReferenceCandidateService)
	if !ok {
		writeError(c, fmt.Errorf("%w: service reference candidates are unavailable", apperrors.ErrUnsupportedOperation))
	}
	return service, ok
}

func (h *DeliveryHandler) GetEnvironmentServiceReferenceCandidate(c *gin.Context) {
	service, ok := h.referenceCandidateService(c)
	if !ok {
		return
	}
	item, err := service.GetEnvironmentServiceReferenceCandidate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Param("serviceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) PutEnvironmentServiceReferenceCandidate(c *gin.Context) {
	service, ok := h.referenceCandidateService(c)
	if !ok {
		return
	}
	var input domaindelivery.ServiceReferenceCandidateInput
	if !decodeReferenceCandidatePayload(c, &input) {
		return
	}
	item, err := service.PutEnvironmentServiceReferenceCandidate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Param("serviceID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) DeleteEnvironmentServiceReferenceCandidate(c *gin.Context) {
	service, ok := h.referenceCandidateService(c)
	if !ok {
		return
	}
	var input domaindelivery.ServiceReferenceCandidateDeleteInput
	if !decodeReferenceCandidatePayload(c, &input) {
		return
	}
	if err := service.DeleteEnvironmentServiceReferenceCandidate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Param("serviceID"), input); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func decodeReferenceCandidatePayload(c *gin.Context, input any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid service reference candidate payload")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "expected one service reference candidate payload")
		return false
	}
	return true
}
