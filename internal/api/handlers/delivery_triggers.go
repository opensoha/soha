package handlers

import (
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type DeliveryTriggerService interface {
	List(context.Context, domainidentity.Principal, string, string, int, int) ([]domain.Trigger, error)
	Get(context.Context, domainidentity.Principal, string) (domain.Trigger, error)
	Save(context.Context, domainidentity.Principal, string, domain.Input) (domain.Trigger, error)
	Events(context.Context, domainidentity.Principal, string, int, int) ([]domain.Event, error)
	ReceiveWebhook(context.Context, string, string, string, string, []byte) (domain.Event, error)
}

type DeliveryTriggerHandler struct{ service DeliveryTriggerService }

// Strip generated union unmarshallers so DisallowUnknownFields also covers
// configuration credentials and nested schedules at the HTTP boundary.
type deliveryTriggerInputFields domain.Input
type deliveryTriggerScheduleFields domain.Schedule

func NewDeliveryTriggerHandler(service DeliveryTriggerService) *DeliveryTriggerHandler {
	return &DeliveryTriggerHandler{service: service}
}

func (h *DeliveryTriggerHandler) List(c *gin.Context) {
	offset, limit, ok := templateSourcePagination(c)
	if !ok {
		return
	}
	items, err := h.service.List(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Query("targetKind"), c.Query("targetId"), offset, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, items)
}

func (h *DeliveryTriggerHandler) Get(c *gin.Context) {
	item, err := h.service.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("triggerID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryTriggerHandler) Save(c *gin.Context) {
	var request struct {
		deliveryTriggerInputFields
		Schedule *deliveryTriggerScheduleFields `json:"schedule,omitempty"`
	}
	if !decodeDeliveryDocumentRequest(c, &request, 64<<10) {
		return
	}
	input := domain.Input(request.deliveryTriggerInputFields)
	if request.Schedule != nil {
		schedule := domain.Schedule(*request.Schedule)
		input.Schedule = &schedule
	}
	item, err := h.service.Save(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("triggerID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if c.Param("triggerID") == "" {
		status = http.StatusCreated
	}
	apiresponse.Item(c, status, item)
}

func (h *DeliveryTriggerHandler) Events(c *gin.Context) {
	offset, limit, ok := templateSourcePagination(c)
	if !ok {
		return
	}
	items, err := h.service.Events(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("triggerID"), offset, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, items)
}

func (h *DeliveryTriggerHandler) Webhook(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	for _, header := range []string{"webhook-id", "webhook-timestamp", "webhook-signature"} {
		if len(c.Request.Header.Values(header)) != 1 {
			apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "a single signed webhook credential is required")
			return
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "webhook body must be at most 1 MiB")
		return
	}
	item, err := h.service.ReceiveWebhook(c.Request.Context(), c.Param("triggerID"), c.GetHeader("webhook-id"), c.GetHeader("webhook-timestamp"), c.GetHeader("webhook-signature"), body)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}
