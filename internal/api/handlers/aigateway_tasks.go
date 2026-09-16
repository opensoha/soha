package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type AIGatewayPlanService interface {
	ValidateCapabilityPlan(context.Context, domainidentity.Principal, domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityPlanValidation, error)
}

type AIGatewayTaskService interface {
	CreateCapabilityTask(context.Context, domainidentity.Principal, domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityTask, error)
	GetCapabilityTask(context.Context, domainidentity.Principal, string) (domainaigateway.CapabilityTask, error)
	ListCapabilityTasks(context.Context, domainidentity.Principal, int) ([]domainaigateway.CapabilityTask, error)
	CancelCapabilityTask(context.Context, domainidentity.Principal, string) (domainaigateway.CapabilityTask, error)
	ResumeCapabilityTask(context.Context, domainidentity.Principal, string, domainaigateway.CapabilityTaskRevisionInput) (domainaigateway.CapabilityTask, error)
	GetCapabilityTaskRevision(context.Context, domainidentity.Principal, string, int) (domainaigateway.CapabilityTask, error)
}

type aiGatewayTaskHandler struct {
	plans AIGatewayPlanService
	tasks AIGatewayTaskService
}

func capabilityTaskRequest(c *gin.Context) (domainaigateway.CapabilityTaskInput, bool) {
	var input domainaigateway.CapabilityTaskInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if c.ShouldBindJSON(&input) != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid capability task payload")
		return input, false
	}
	input.AIClientID = firstNonEmpty(input.AIClientID, firstHeaderValue(c, "X-Soha-AI-Client-ID", "X-AI-Client-ID"))
	input.SkillID = firstNonEmpty(input.SkillID, firstHeaderValue(c, "X-Soha-Skill-ID", "X-Skill-ID"))
	return input, true
}

func (h *aiGatewayTaskHandler) ValidateCapabilityPlan(c *gin.Context) {
	if h.plans == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "capability plan validation is unavailable")
		return
	}
	input, ok := capabilityTaskRequest(c)
	if !ok {
		return
	}
	item, err := h.plans.ValidateCapabilityPlan(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *aiGatewayTaskHandler) taskAvailable(c *gin.Context) bool {
	if h.tasks == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "capability tasks are unavailable")
		return false
	}
	return true
}

func (h *aiGatewayTaskHandler) CreateCapabilityTask(c *gin.Context) {
	if !h.taskAvailable(c) {
		return
	}
	input, ok := capabilityTaskRequest(c)
	if !ok {
		return
	}
	item, err := h.tasks.CreateCapabilityTask(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *aiGatewayTaskHandler) GetCapabilityTask(c *gin.Context) {
	if !h.taskAvailable(c) {
		return
	}
	var item domainaigateway.CapabilityTask
	var err error
	if value := c.Query("planVersion"); value != "" {
		version, parseErr := strconv.Atoi(value)
		if parseErr != nil || version < 1 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "planVersion must be positive")
			return
		}
		item, err = h.tasks.GetCapabilityTaskRevision(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskId"), version)
	} else {
		item, err = h.tasks.GetCapabilityTask(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskId"))
	}
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *aiGatewayTaskHandler) ResumeCapabilityTask(c *gin.Context) {
	if !h.taskAvailable(c) {
		return
	}
	var input domainaigateway.CapabilityTaskRevisionInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if c.ShouldBindJSON(&input) != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid capability revision payload")
		return
	}
	item, err := h.tasks.ResumeCapabilityTask(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskId"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *aiGatewayTaskHandler) CancelCapabilityTask(c *gin.Context) {
	if !h.taskAvailable(c) {
		return
	}
	item, err := h.tasks.CancelCapabilityTask(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskId"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *aiGatewayTaskHandler) ListCapabilityTasks(c *gin.Context) {
	if !h.taskAvailable(c) {
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit < 1 || limit > 100 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "limit must be between 1 and 100")
		return
	}
	items, err := h.tasks.ListCapabilityTasks(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
