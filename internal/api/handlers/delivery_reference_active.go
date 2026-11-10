package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DeliveryReferenceActiveService interface {
	ListEnvironmentServiceReferences(context.Context, domainidentity.Principal, string, string, int) (domaindelivery.ServiceReferencePage, error)
	GetEnvironmentServiceReference(context.Context, domainidentity.Principal, string, string) (domaindelivery.ServiceReference, error)
	PutEnvironmentServiceReference(context.Context, domainidentity.Principal, string, string, domaindelivery.ServiceReferenceInput) (domaindelivery.ServiceReference, error)
	DeleteEnvironmentServiceReference(context.Context, domainidentity.Principal, string, string, domaindelivery.ServiceReferenceDeleteInput) error
	ResolveEnvironmentServiceReferenceTarget(context.Context, domainidentity.Principal, string, string, string, string, int64) (domaindelivery.ServiceReferenceResolvedTarget, error)
}

type DeliveryReferenceImpactService interface {
	ListEnvironmentServiceReferenceConsumers(context.Context, domainidentity.Principal, string, string, string, int) (domaindelivery.ServiceReferenceConsumerPage, error)
	GetDeliveryPlanServiceReferenceImpact(context.Context, domainidentity.Principal, string, string, int) (domaindelivery.DeliveryPlanServiceReferenceImpact, error)
}

func referenceNoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}

func (h *DeliveryHandler) referenceActiveService(c *gin.Context) (DeliveryReferenceActiveService, bool) {
	referenceNoStore(c)
	service, ok := h.applications.(DeliveryReferenceActiveService)
	if !ok {
		writeError(c, fmt.Errorf("%w: active service references are unavailable", apperrors.ErrUnsupportedOperation))
	}
	return service, ok
}

func (h *DeliveryHandler) referenceImpactService(c *gin.Context) (DeliveryReferenceImpactService, bool) {
	referenceNoStore(c)
	service, ok := h.applications.(DeliveryReferenceImpactService)
	if !ok {
		writeError(c, fmt.Errorf("%w: service reference impact is unavailable", apperrors.ErrUnsupportedOperation))
	}
	return service, ok
}

func referenceQueryKeys(c *gin.Context, allowed ...string) bool {
	keys := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		keys[key] = true
	}
	for key, values := range c.Request.URL.Query() {
		if !keys[key] || len(values) != 1 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid service reference query")
			return false
		}
	}
	return true
}

func referencePageLimit(c *gin.Context) (int, bool) {
	if _, provided := c.Request.URL.Query()["limit"]; !provided {
		return 50, true
	}
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil || limit < 1 || limit > 100 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "page limit must be from 1 to 100")
		return 0, false
	}
	return limit, true
}

func (h *DeliveryHandler) ListEnvironmentServiceReferences(c *gin.Context) {
	service, ok := h.referenceActiveService(c)
	if !ok || !referenceQueryKeys(c, "limit", "cursor") {
		return
	}
	limit, ok := referencePageLimit(c)
	if !ok {
		return
	}
	page, err := service.ListEnvironmentServiceReferences(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Query("cursor"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *DeliveryHandler) GetEnvironmentServiceReference(c *gin.Context) {
	service, ok := h.referenceActiveService(c)
	if !ok || !referenceQueryKeys(c) {
		return
	}
	item, err := service.GetEnvironmentServiceReference(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Param("serviceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) PutEnvironmentServiceReference(c *gin.Context) {
	service, ok := h.referenceActiveService(c)
	if !ok || !referenceQueryKeys(c) {
		return
	}
	var request struct {
		domaindelivery.ServiceReferenceInput
		ExpectedReferenceID json.RawMessage `json:"expectedReferenceId"`
	}
	if !decodeReferenceCandidatePayload(c, &request) {
		return
	}
	input := request.ServiceReferenceInput
	if len(request.ExpectedReferenceID) > 0 {
		if input.ExpectedRevision == nil || *input.ExpectedRevision == 0 || string(request.ExpectedReferenceID) == "null" || json.Unmarshal(request.ExpectedReferenceID, &input.ExpectedReferenceID) != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "reference identity is required only when updating an active reference")
			return
		}
	}
	item, err := service.PutEnvironmentServiceReference(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Param("serviceID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) DeleteEnvironmentServiceReference(c *gin.Context) {
	service, ok := h.referenceActiveService(c)
	if !ok || !referenceQueryKeys(c) {
		return
	}
	var input domaindelivery.ServiceReferenceDeleteInput
	if !decodeReferenceCandidatePayload(c, &input) {
		return
	}
	if err := service.DeleteEnvironmentServiceReference(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Param("serviceID"), input); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *DeliveryHandler) ResolveEnvironmentServiceReferenceTarget(c *gin.Context) {
	service, ok := h.referenceActiveService(c)
	if !ok || !referenceQueryKeys(c, "expectedReferenceId", "expectedRevision") {
		return
	}
	revision, err := strconv.ParseInt(c.Query("expectedRevision"), 10, 64)
	if err != nil || revision < 1 || !validReferenceQueryID(c.Query("expectedReferenceId")) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "reference identity and positive revision are required")
		return
	}
	item, err := service.ResolveEnvironmentServiceReferenceTarget(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Param("serviceID"), c.Param("targetID"), c.Query("expectedReferenceId"), revision)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func validReferenceQueryID(id string) bool {
	if len(id) < 1 || len(id) > 200 {
		return false
	}
	for _, r := range id {
		if r <= ' ' || r == 127 {
			return false
		}
	}
	return true
}

func (h *DeliveryHandler) ListEnvironmentServiceReferenceConsumers(c *gin.Context) {
	service, ok := h.referenceImpactService(c)
	if !ok || !referenceQueryKeys(c, "serviceId", "limit", "cursor") {
		return
	}
	if !validReferenceQueryID(c.Query("serviceId")) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "serviceId is required")
		return
	}
	limit, ok := referencePageLimit(c)
	if !ok {
		return
	}
	page, err := service.ListEnvironmentServiceReferenceConsumers(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), c.Query("serviceId"), c.Query("cursor"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *DeliveryHandler) GetDeliveryPlanServiceReferenceImpact(c *gin.Context) {
	service, ok := h.referenceImpactService(c)
	if !ok || !referenceQueryKeys(c, "limit", "cursor") {
		return
	}
	limit, ok := referencePageLimit(c)
	if !ok {
		return
	}
	item, err := service.GetDeliveryPlanServiceReferenceImpact(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("planID"), c.Query("cursor"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}
