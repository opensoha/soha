package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domaincopilot "github.com/kubecrux/kubecrux/internal/domain/copilot"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainmcp "github.com/kubecrux/kubecrux/internal/domain/mcp"
)

type CopilotService interface {
	ListSessions(context.Context, domainidentity.Principal) ([]domaincopilot.Session, error)
	CreateSession(context.Context, domainidentity.Principal, string, string) (domaincopilot.Session, error)
	ListMessages(context.Context, domainidentity.Principal, string) ([]domaincopilot.Message, error)
	SendMessage(context.Context, domainidentity.Principal, string, string, string) ([]domaincopilot.Message, error)
	Insights(context.Context, string) ([]domaincopilot.Insight, error)
	ListDataSourceCapabilities(context.Context) ([]domainmcp.Adapter, error)
	ListDataSources(context.Context, domainidentity.Principal) ([]domaincopilot.DataSource, error)
	CreateDataSource(context.Context, domainidentity.Principal, domaincopilot.DataSourceInput) (domaincopilot.DataSource, error)
	UpdateDataSource(context.Context, domainidentity.Principal, string, domaincopilot.DataSourceInput) (domaincopilot.DataSource, error)
	ValidateDataSource(context.Context, domainidentity.Principal, string) (domaincopilot.DataSource, error)
	ListAnalysisProfiles(context.Context, domainidentity.Principal) ([]domaincopilot.AnalysisProfile, error)
	CreateAnalysisProfile(context.Context, domainidentity.Principal, domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfile, error)
	UpdateAnalysisProfile(context.Context, domainidentity.Principal, string, domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfile, error)
	ListAutomationPolicies(context.Context, domainidentity.Principal) ([]domaincopilot.AutomationPolicy, error)
	CreateAutomationPolicy(context.Context, domainidentity.Principal, domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicy, error)
	UpdateAutomationPolicy(context.Context, domainidentity.Principal, string, domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicy, error)
	ListRootCauseRuns(context.Context, domainidentity.Principal, domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error)
	GetRootCauseRun(context.Context, domainidentity.Principal, string) (domaincopilot.RootCauseRun, error)
	RunRootCauseAnalysis(context.Context, domainidentity.Principal, domaincopilot.RootCauseRunInput, string) (domaincopilot.RootCauseRun, error)
	ListInspectionTasks(context.Context, domainidentity.Principal) ([]domaincopilot.InspectionTask, error)
	CreateInspectionTask(context.Context, domainidentity.Principal, domaincopilot.InspectionTaskInput, string) (domaincopilot.InspectionTask, error)
	UpdateInspectionTask(context.Context, domainidentity.Principal, string, domaincopilot.InspectionTaskInput, string) (domaincopilot.InspectionTask, error)
	ListInspectionRuns(context.Context, domainidentity.Principal, domaincopilot.InspectionRunFilter) ([]domaincopilot.InspectionRun, error)
	ExecuteInspectionTask(context.Context, domainidentity.Principal, string, string) (domaincopilot.InspectionRun, error)
}

type CopilotHandler struct {
	service CopilotService
}

func NewCopilotHandler(service CopilotService) *CopilotHandler {
	return &CopilotHandler{service: service}
}

func (h *CopilotHandler) ListInsights(c *gin.Context) {
	items, err := h.service.Insights(c.Request.Context(), localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) ListDataSourceCapabilities(c *gin.Context) {
	items, err := h.service.ListDataSourceCapabilities(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) ListDataSources(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListDataSources(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) CreateDataSource(c *gin.Context) {
	var req dto.DataSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid data source payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateDataSource(c.Request.Context(), principal, domaincopilot.DataSourceInput{
		ID:              req.ID,
		Name:            req.Name,
		SourceKind:      req.SourceKind,
		BackendType:     req.BackendType,
		Enabled:         req.Enabled,
		CredentialRef:   req.CredentialRef,
		Scope:           req.Scope,
		QueryBudget:     req.QueryBudget,
		RedactionPolicy: req.RedactionPolicy,
		MCPAdapter:      req.MCPAdapter,
		Config:          req.Config,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CopilotHandler) UpdateDataSource(c *gin.Context) {
	var req dto.DataSourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid data source payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateDataSource(c.Request.Context(), principal, c.Param("dataSourceID"), domaincopilot.DataSourceInput{
		ID:              req.ID,
		Name:            req.Name,
		SourceKind:      req.SourceKind,
		BackendType:     req.BackendType,
		Enabled:         req.Enabled,
		CredentialRef:   req.CredentialRef,
		Scope:           req.Scope,
		QueryBudget:     req.QueryBudget,
		RedactionPolicy: req.RedactionPolicy,
		MCPAdapter:      req.MCPAdapter,
		Config:          req.Config,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CopilotHandler) ValidateDataSource(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.ValidateDataSource(c.Request.Context(), principal, c.Param("dataSourceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CopilotHandler) ListAnalysisProfiles(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAnalysisProfiles(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) CreateAnalysisProfile(c *gin.Context) {
	var req dto.AnalysisProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid analysis profile payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateAnalysisProfile(c.Request.Context(), principal, domaincopilot.AnalysisProfileInput{
		ID:                      req.ID,
		Name:                    req.Name,
		Mode:                    req.Mode,
		EnabledSources:          req.EnabledSources,
		EnabledPlaybooks:        req.EnabledPlaybooks,
		QueryBudgets:            req.QueryBudgets,
		OutputStyle:             req.OutputStyle,
		RemediationPolicy:       req.RemediationPolicy,
		DefaultTimeRangeMinutes: req.DefaultTimeRangeMinutes,
		TimeoutSeconds:          req.TimeoutSeconds,
		Enabled:                 req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CopilotHandler) UpdateAnalysisProfile(c *gin.Context) {
	var req dto.AnalysisProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid analysis profile payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateAnalysisProfile(c.Request.Context(), principal, c.Param("profileID"), domaincopilot.AnalysisProfileInput{
		ID:                      req.ID,
		Name:                    req.Name,
		Mode:                    req.Mode,
		EnabledSources:          req.EnabledSources,
		EnabledPlaybooks:        req.EnabledPlaybooks,
		QueryBudgets:            req.QueryBudgets,
		OutputStyle:             req.OutputStyle,
		RemediationPolicy:       req.RemediationPolicy,
		DefaultTimeRangeMinutes: req.DefaultTimeRangeMinutes,
		TimeoutSeconds:          req.TimeoutSeconds,
		Enabled:                 req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CopilotHandler) ListAutomationPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAutomationPolicies(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) CreateAutomationPolicy(c *gin.Context) {
	var req dto.AutomationPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid automation policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateAutomationPolicy(c.Request.Context(), principal, domaincopilot.AutomationPolicyInput{
		ID:                 req.ID,
		Name:               req.Name,
		Enabled:            req.Enabled,
		TriggerType:        req.TriggerType,
		TriggerConditions:  req.TriggerConditions,
		DedupWindowSeconds: req.DedupWindowSeconds,
		AnalysisProfileID:  req.AnalysisProfileID,
		RemediationPolicy:  req.RemediationPolicy,
		ApprovalPolicy:     req.ApprovalPolicy,
		CooldownSeconds:    req.CooldownSeconds,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CopilotHandler) UpdateAutomationPolicy(c *gin.Context) {
	var req dto.AutomationPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid automation policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateAutomationPolicy(c.Request.Context(), principal, c.Param("policyID"), domaincopilot.AutomationPolicyInput{
		ID:                 req.ID,
		Name:               req.Name,
		Enabled:            req.Enabled,
		TriggerType:        req.TriggerType,
		TriggerConditions:  req.TriggerConditions,
		DedupWindowSeconds: req.DedupWindowSeconds,
		AnalysisProfileID:  req.AnalysisProfileID,
		RemediationPolicy:  req.RemediationPolicy,
		ApprovalPolicy:     req.ApprovalPolicy,
		CooldownSeconds:    req.CooldownSeconds,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CopilotHandler) ListSessions(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListSessions(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) CreateSession(c *gin.Context) {
	var req dto.CreateCopilotSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid copilot session payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateSession(c.Request.Context(), principal, req.Title, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CopilotHandler) ListMessages(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListMessages(c.Request.Context(), principal, c.Param("sessionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) SendMessage(c *gin.Context) {
	var req dto.SendCopilotMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid copilot message payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.SendMessage(c.Request.Context(), principal, c.Param("sessionID"), req.Content, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusAccepted, items)
}

func (h *CopilotHandler) ListRootCauseRuns(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListRootCauseRuns(c.Request.Context(), principal, domaincopilot.RootCauseRunFilter{
		ClusterID: c.Query("clusterId"),
		AlertID:   c.Query("alertId"),
		Limit:     parseLimit(c.Query("limit"), 20),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) GetRootCauseRun(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetRootCauseRun(c.Request.Context(), principal, c.Param("runID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CopilotHandler) CreateRootCauseRun(c *gin.Context) {
	var req dto.CreateRootCauseRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid root cause analysis payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RunRootCauseAnalysis(c.Request.Context(), principal, domaincopilot.RootCauseRunInput{
		Title:            req.Title,
		ClusterID:        req.ClusterID,
		Namespace:        req.Namespace,
		WorkloadKind:     req.WorkloadKind,
		WorkloadName:     req.WorkloadName,
		AlertID:          req.AlertID,
		TimeRangeMinutes: req.TimeRangeMinutes,
		Question:         req.Question,
	}, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CopilotHandler) ListInspectionTasks(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListInspectionTasks(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) CreateInspectionTask(c *gin.Context) {
	var req dto.CreateInspectionTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid inspection task payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateInspectionTask(c.Request.Context(), principal, domaincopilot.InspectionTaskInput{
		ID:              req.ID,
		Title:           req.Title,
		ScopeType:       req.ScopeType,
		ClusterID:       req.ClusterID,
		Namespace:       req.Namespace,
		Checks:          req.Checks,
		Enabled:         req.Enabled,
		IntervalMinutes: req.IntervalMinutes,
		Metadata:        req.Metadata,
	}, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CopilotHandler) UpdateInspectionTask(c *gin.Context) {
	var req dto.CreateInspectionTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid inspection task payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateInspectionTask(c.Request.Context(), principal, c.Param("taskID"), domaincopilot.InspectionTaskInput{
		ID:              req.ID,
		Title:           req.Title,
		ScopeType:       req.ScopeType,
		ClusterID:       req.ClusterID,
		Namespace:       req.Namespace,
		Checks:          req.Checks,
		Enabled:         req.Enabled,
		IntervalMinutes: req.IntervalMinutes,
		Metadata:        req.Metadata,
	}, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CopilotHandler) ListInspectionRuns(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListInspectionRuns(c.Request.Context(), principal, domaincopilot.InspectionRunFilter{
		TaskID:     c.Query("taskId"),
		ClusterID:  c.Query("clusterId"),
		Namespace:  c.Query("namespace"),
		Check:      c.Query("check"),
		LatestOnly: c.Query("latest") == "true",
		Limit:      20,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) ExecuteInspectionTask(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.ExecuteInspectionTask(c.Request.Context(), principal, c.Param("taskID"), localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func localeFromRequest(value string) string {
	if len(value) >= 2 && (value[0:2] == "zh" || value[0:2] == "ZH") {
		return "zh-CN"
	}
	return "en-US"
}
