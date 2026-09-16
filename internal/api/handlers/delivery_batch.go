package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type DeliveryBatchService interface {
	ListDeliveryWorkflows(context.Context, domainidentity.Principal) ([]domainworkflow.DeliveryWorkflow, error)
	GetDeliveryWorkflow(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryWorkflow, error)
	SaveDeliveryWorkflow(context.Context, domainidentity.Principal, string, domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflow, error)
	ListDeliveryBatches(context.Context, domainidentity.Principal, string, string, string, int) ([]domainworkflow.DeliveryBatch, error)
	GetDeliveryBatch(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryBatch, error)
	CreateDeliveryBatch(context.Context, domainidentity.Principal, domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error)
	CancelDeliveryBatch(context.Context, domainidentity.Principal, string, string) (domainworkflow.DeliveryBatch, error)
}
type DeliveryBatchHandler struct{ service DeliveryBatchService }

func NewDeliveryBatchHandler(service DeliveryBatchService) *DeliveryBatchHandler {
	return &DeliveryBatchHandler{service: service}
}

func (h *DeliveryBatchHandler) ListWorkflows(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.ListDeliveryWorkflows(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, item)
}

func (h *DeliveryBatchHandler) GetWorkflow(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetDeliveryWorkflow(c.Request.Context(), principal, c.Param("deliveryWorkflowID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryBatchHandler) ListBatches(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.ListDeliveryBatches(c.Request.Context(), principal, c.Query("applicationId"), c.Query("serviceId"), c.Query("workflowId"), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, item)
}

func (h *DeliveryBatchHandler) GetBatch(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetDeliveryBatch(c.Request.Context(), principal, c.Param("deliveryBatchID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryBatchHandler) CreateWorkflow(c *gin.Context) {
	h.saveWorkflow(c, "", http.StatusCreated)
}
func (h *DeliveryBatchHandler) UpdateWorkflow(c *gin.Context) {
	h.saveWorkflow(c, c.Param("deliveryWorkflowID"), http.StatusOK)
}
func (h *DeliveryBatchHandler) saveWorkflow(c *gin.Context, id string, status int) {
	var input domainworkflow.DeliveryWorkflowInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid delivery workflow payload")
		return
	}
	item, err := h.service.SaveDeliveryWorkflow(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), id, input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, status, item)
}
func (h *DeliveryBatchHandler) CreateBatch(c *gin.Context) {
	var input domainworkflow.DeliveryBatchInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid delivery batch payload")
		return
	}
	// Freezing Git refs may exceed the ordinary response write timeout.
	if err := clearResponseWriteDeadline(c); err != nil {
		writeError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	item, err := h.service.CreateDeliveryBatch(ctx, apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *DeliveryBatchHandler) CancelBatch(c *gin.Context) {
	var input struct {
		Reason string `json:"reason" binding:"max=2000"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid delivery cancellation payload")
		return
	}
	item, err := h.service.CancelDeliveryBatch(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("deliveryBatchID"), input.Reason)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
