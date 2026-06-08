package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
)

func (h *MonitoringHandler) ListRules(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListRules(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) GetRule(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetRule(c.Request.Context(), principal, c.Param("ruleID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) CreateRule(c *gin.Context) {
	var req dto.AlertRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert rule payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateRule(c.Request.Context(), principal, domainalert.AlertRuleInput{
		ID:                   req.ID,
		Name:                 req.Name,
		RuleType:             req.RuleType,
		DatasourceSelector:   req.DatasourceSelector,
		QuerySpec:            req.QuerySpec,
		ThresholdSpec:        req.ThresholdSpec,
		ForSeconds:           req.ForSeconds,
		GroupBy:              req.GroupBy,
		Labels:               req.Labels,
		Annotations:          req.Annotations,
		NotificationPolicyID: req.NotificationPolicyID,
		HealingPolicyIDs:     req.HealingPolicyIDs,
		Enabled:              req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MonitoringHandler) UpdateRule(c *gin.Context) {
	var req dto.AlertRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert rule payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateRule(c.Request.Context(), principal, c.Param("ruleID"), domainalert.AlertRuleInput{
		ID:                   req.ID,
		Name:                 req.Name,
		RuleType:             req.RuleType,
		DatasourceSelector:   req.DatasourceSelector,
		QuerySpec:            req.QuerySpec,
		ThresholdSpec:        req.ThresholdSpec,
		ForSeconds:           req.ForSeconds,
		GroupBy:              req.GroupBy,
		Labels:               req.Labels,
		Annotations:          req.Annotations,
		NotificationPolicyID: req.NotificationPolicyID,
		HealingPolicyIDs:     req.HealingPolicyIDs,
		Enabled:              req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) TestRule(c *gin.Context) {
	var req dto.AlertRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert rule payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.TestRule(c.Request.Context(), principal, domainalert.AlertRuleInput{
		ID:                   req.ID,
		Name:                 req.Name,
		RuleType:             req.RuleType,
		DatasourceSelector:   req.DatasourceSelector,
		QuerySpec:            req.QuerySpec,
		ThresholdSpec:        req.ThresholdSpec,
		ForSeconds:           req.ForSeconds,
		GroupBy:              req.GroupBy,
		Labels:               req.Labels,
		Annotations:          req.Annotations,
		NotificationPolicyID: req.NotificationPolicyID,
		HealingPolicyIDs:     req.HealingPolicyIDs,
		Enabled:              req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListRuleRuns(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListRuleRuns(c.Request.Context(), principal, domainalert.AlertRuleRunFilter{
		RuleID: c.Query("ruleId"),
		Limit:  parseLimit(c.Query("limit"), 20),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) ListEvents(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListEvents(c.Request.Context(), principal, domainalert.AlertEventFilter{
		Status:    c.Query("status"),
		RuleID:    c.Query("ruleId"),
		ClusterID: c.Query("clusterId"),
		Limit:     parseLimit(c.Query("limit"), 50),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) GetEvent(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetEvent(c.Request.Context(), principal, c.Param("eventID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) AcknowledgeEvent(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.AcknowledgeEvent(c.Request.Context(), principal, c.Param("eventID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ResolveEvent(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.ResolveEvent(c.Request.Context(), principal, c.Param("eventID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) HealEvent(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.HealEvent(c.Request.Context(), principal, c.Param("eventID"), c.Query("policyId"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MonitoringHandler) GetHealingRun(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetHealingRun(c.Request.Context(), principal, c.Param("runID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ApproveHealingRun(c *gin.Context) {
	var req dto.WorkflowApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid healing approval payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.ApproveHealingRun(c.Request.Context(), principal, c.Param("runID"), req.Comment)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) RejectHealingRun(c *gin.Context) {
	var req dto.WorkflowApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid healing approval payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RejectHealingRun(c.Request.Context(), principal, c.Param("runID"), req.Comment)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) RetryHealingRun(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RetryHealingRun(c.Request.Context(), principal, c.Param("runID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListNotificationPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListNotificationPolicies(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateNotificationPolicy(c *gin.Context) {
	var req dto.NotificationPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid notification policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateNotificationPolicy(c.Request.Context(), principal, domainalert.NotificationPolicyInput{
		ID:              req.ID,
		Name:            req.Name,
		Matchers:        req.Matchers,
		ProcessorChain:  req.ProcessorChain,
		ChannelRefs:     req.ChannelRefs,
		OnCallRef:       req.OnCallRef,
		SendResolved:    req.SendResolved,
		CooldownSeconds: req.CooldownSeconds,
		Enabled:         req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MonitoringHandler) UpdateNotificationPolicy(c *gin.Context) {
	var req dto.NotificationPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid notification policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateNotificationPolicy(c.Request.Context(), principal, c.Param("policyID"), domainalert.NotificationPolicyInput{
		ID:              req.ID,
		Name:            req.Name,
		Matchers:        req.Matchers,
		ProcessorChain:  req.ProcessorChain,
		ChannelRefs:     req.ChannelRefs,
		OnCallRef:       req.OnCallRef,
		SendResolved:    req.SendResolved,
		CooldownSeconds: req.CooldownSeconds,
		Enabled:         req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) PreviewNotificationPolicy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.PreviewNotificationPolicy(c.Request.Context(), principal, c.Param("policyID"), c.Query("eventId"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) ListNotificationTemplates(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListNotificationTemplates(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateNotificationTemplate(c *gin.Context) {
	var req dto.NotificationTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid notification template payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateNotificationTemplate(c.Request.Context(), principal, domainalert.NotificationTemplateInput{
		ID:            req.ID,
		Name:          req.Name,
		TemplateType:  req.TemplateType,
		ContentType:   req.ContentType,
		BodyTemplate:  req.BodyTemplate,
		Headers:       req.Headers,
		QueryParams:   req.QueryParams,
		SamplePayload: req.SamplePayload,
		Enabled:       req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MonitoringHandler) UpdateNotificationTemplate(c *gin.Context) {
	var req dto.NotificationTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid notification template payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateNotificationTemplate(c.Request.Context(), principal, c.Param("templateID"), domainalert.NotificationTemplateInput{
		ID:            req.ID,
		Name:          req.Name,
		TemplateType:  req.TemplateType,
		ContentType:   req.ContentType,
		BodyTemplate:  req.BodyTemplate,
		Headers:       req.Headers,
		QueryParams:   req.QueryParams,
		SamplePayload: req.SamplePayload,
		Enabled:       req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListHealingPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListHealingPolicies(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateHealingPolicy(c *gin.Context) {
	var req dto.HealingPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid healing policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateHealingPolicy(c.Request.Context(), principal, domainalert.HealingPolicyInput{
		ID:                  req.ID,
		Name:                req.Name,
		TriggerMode:         req.TriggerMode,
		WorkflowTemplateID:  req.WorkflowTemplateID,
		ApprovalPolicyRef:   req.ApprovalPolicyRef,
		CooldownSeconds:     req.CooldownSeconds,
		ConcurrencyKey:      req.ConcurrencyKey,
		SafetyWindowSeconds: req.SafetyWindowSeconds,
		Definition:          req.Definition,
		Enabled:             req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MonitoringHandler) UpdateHealingPolicy(c *gin.Context) {
	var req dto.HealingPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid healing policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateHealingPolicy(c.Request.Context(), principal, c.Param("policyID"), domainalert.HealingPolicyInput{
		ID:                  req.ID,
		Name:                req.Name,
		TriggerMode:         req.TriggerMode,
		WorkflowTemplateID:  req.WorkflowTemplateID,
		ApprovalPolicyRef:   req.ApprovalPolicyRef,
		CooldownSeconds:     req.CooldownSeconds,
		ConcurrencyKey:      req.ConcurrencyKey,
		SafetyWindowSeconds: req.SafetyWindowSeconds,
		Definition:          req.Definition,
		Enabled:             req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListHealingRuns(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListHealingRuns(c.Request.Context(), principal, domainalert.HealingRunFilter{
		PolicyID: c.Query("policyId"),
		EventID:  c.Query("eventId"),
		Status:   c.Query("status"),
		Limit:    parseLimit(c.Query("limit"), 50),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) ListOnCallSchedules(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListOnCallSchedules(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateOnCallSchedule(c *gin.Context) {
	var req dto.OnCallScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oncall schedule payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateOnCallSchedule(c.Request.Context(), principal, domainalert.OnCallScheduleInput{
		ID:          req.ID,
		Name:        req.Name,
		TimeZone:    req.TimeZone,
		Description: req.Description,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MonitoringHandler) UpdateOnCallSchedule(c *gin.Context) {
	var req dto.OnCallScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oncall schedule payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateOnCallSchedule(c.Request.Context(), principal, c.Param("scheduleID"), domainalert.OnCallScheduleInput{
		ID:          req.ID,
		Name:        req.Name,
		TimeZone:    req.TimeZone,
		Description: req.Description,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListOnCallRotations(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListOnCallRotations(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateOnCallRotation(c *gin.Context) {
	var req dto.OnCallRotationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oncall rotation payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateOnCallRotation(c.Request.Context(), principal, domainalert.OnCallRotationInput{
		ID:             req.ID,
		ScheduleID:     req.ScheduleID,
		Name:           req.Name,
		Participants:   req.Participants,
		RotationConfig: req.RotationConfig,
		Enabled:        req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MonitoringHandler) UpdateOnCallRotation(c *gin.Context) {
	var req dto.OnCallRotationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oncall rotation payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateOnCallRotation(c.Request.Context(), principal, c.Param("rotationID"), domainalert.OnCallRotationInput{
		ID:             req.ID,
		ScheduleID:     req.ScheduleID,
		Name:           req.Name,
		Participants:   req.Participants,
		RotationConfig: req.RotationConfig,
		Enabled:        req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListOnCallEscalationPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListOnCallEscalationPolicies(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateOnCallEscalationPolicy(c *gin.Context) {
	var req dto.OnCallEscalationPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oncall escalation policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateOnCallEscalationPolicy(c.Request.Context(), principal, domainalert.OnCallEscalationPolicyInput{
		ID:      req.ID,
		Name:    req.Name,
		Steps:   req.Steps,
		Enabled: req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MonitoringHandler) UpdateOnCallEscalationPolicy(c *gin.Context) {
	var req dto.OnCallEscalationPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oncall escalation policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateOnCallEscalationPolicy(c.Request.Context(), principal, c.Param("policyID"), domainalert.OnCallEscalationPolicyInput{
		ID:      req.ID,
		Name:    req.Name,
		Steps:   req.Steps,
		Enabled: req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListOnCallAssignmentRules(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListOnCallAssignmentRules(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateOnCallAssignmentRule(c *gin.Context) {
	var req dto.OnCallAssignmentRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oncall assignment rule payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateOnCallAssignmentRule(c.Request.Context(), principal, domainalert.OnCallAssignmentRuleInput{
		ID:              req.ID,
		Name:            req.Name,
		IntegrationID:   req.IntegrationID,
		IntegrationType: req.IntegrationType,
		BusinessLineID:  req.BusinessLineID,
		AlertCategory:   req.AlertCategory,
		AlertName:       req.AlertName,
		Severity:        req.Severity,
		Service:         req.Service,
		Role:            req.Role,
		Matchers:        req.Matchers,
		TargetType:      req.TargetType,
		TargetRef:       req.TargetRef,
		RouteOrder:      req.RouteOrder,
		GroupBy:         req.GroupBy,
		Priority:        req.Priority,
		Enabled:         req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MonitoringHandler) UpdateOnCallAssignmentRule(c *gin.Context) {
	var req dto.OnCallAssignmentRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oncall assignment rule payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	ruleID := firstNonEmptyParam(c.Param("ruleID"), c.Param("routeID"))
	item, err := h.service.UpdateOnCallAssignmentRule(c.Request.Context(), principal, ruleID, domainalert.OnCallAssignmentRuleInput{
		ID:              req.ID,
		Name:            req.Name,
		IntegrationID:   req.IntegrationID,
		IntegrationType: req.IntegrationType,
		BusinessLineID:  req.BusinessLineID,
		AlertCategory:   req.AlertCategory,
		AlertName:       req.AlertName,
		Severity:        req.Severity,
		Service:         req.Service,
		Role:            req.Role,
		Matchers:        req.Matchers,
		TargetType:      req.TargetType,
		TargetRef:       req.TargetRef,
		RouteOrder:      req.RouteOrder,
		GroupBy:         req.GroupBy,
		Priority:        req.Priority,
		Enabled:         req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func firstNonEmptyParam(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (h *MonitoringHandler) GetCurrentOnCall(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetCurrentOnCall(c.Request.Context(), principal, c.Query("ref"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ResolveOnCall(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.ResolveOnCall(c.Request.Context(), principal, domainalert.OnCallResolveInput{
		AlertID:         c.Query("alertId"),
		IntegrationID:   c.Query("integrationId"),
		IntegrationType: c.Query("integrationType"),
		BusinessLineID:  c.Query("businessLineId"),
		AlertCategory:   c.Query("alertCategory"),
		AlertName:       c.Query("alertName"),
		Severity:        c.Query("severity"),
		Service:         c.Query("service"),
		Role:            c.Query("role"),
		ClusterID:       c.Query("clusterId"),
		Namespace:       c.Query("namespace"),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListOnCallTasks(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListOnCallTasks(c.Request.Context(), principal, parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
