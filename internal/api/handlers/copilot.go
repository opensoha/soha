package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmcp "github.com/opensoha/soha/internal/domain/mcp"
)

type CopilotService interface {
	ListSessions(context.Context, domainidentity.Principal) ([]domaincopilot.Session, error)
	GetSession(context.Context, domainidentity.Principal, string) (domaincopilot.Session, error)
	CreateSession(context.Context, domainidentity.Principal, string, string, string, map[string]any, map[string]any, string, []string, string) (domaincopilot.Session, error)
	UpdateSession(context.Context, domainidentity.Principal, string, string, string, string, string, string, map[string]any, map[string]any, string, map[string]any, []string, bool) (domaincopilot.Session, error)
	DeleteSession(context.Context, domainidentity.Principal, string) error
	ListMessages(context.Context, domainidentity.Principal, string) ([]domaincopilot.Message, error)
	SendMessage(context.Context, domainidentity.Principal, string, string, string) (domaincopilot.SessionMessageEnvelope, error)
	StreamMessage(context.Context, domainidentity.Principal, string, domaincopilot.WorkbenchSendMessageInput, string) (domaincopilot.WorkbenchStreamResult, error)
	RecordGlobalAssistantEvent(context.Context, domainidentity.Principal, domaincopilot.WorkbenchGlobalAssistantEventInput) error
	Insights(context.Context, domainidentity.Principal, string) ([]domaincopilot.Insight, error)
	ListDataSourceCapabilities(context.Context, domainidentity.Principal) ([]domainmcp.Adapter, error)
	GetWorkbenchCatalog(context.Context, domainidentity.Principal) (domaincopilot.WorkbenchCatalog, error)
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
	DeleteAutomationPolicy(context.Context, domainidentity.Principal, string) error
	ListRootCauseRuns(context.Context, domainidentity.Principal, domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error)
	ListAnalysisRuns(context.Context, domainidentity.Principal, domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error)
	GetRootCauseRun(context.Context, domainidentity.Principal, string) (domaincopilot.RootCauseRun, error)
	RunRootCauseAnalysis(context.Context, domainidentity.Principal, domaincopilot.RootCauseRunInput, string) (domaincopilot.RootCauseRun, error)
	RunSessionAnalysis(context.Context, domainidentity.Principal, string, domaincopilot.RootCauseRunInput, string) (domaincopilot.SessionMessageEnvelope, error)
	ListAgentProviders(context.Context, domainidentity.Principal) ([]domaincopilot.AgentProvider, error)
	ListAgentRuns(context.Context, domainidentity.Principal) ([]domaincopilot.AgentRun, error)
	CancelAgentRun(context.Context, domainidentity.Principal, string) (domaincopilot.AgentRun, error)
	ClaimAgentRun(context.Context, domaincopilot.AgentRunClaimInput) (domaincopilot.AgentRun, error)
	RecordAgentRunCallback(context.Context, domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error)
	RecordAgentToolCall(context.Context, domaincopilot.AgentToolCallInput) (domaincopilot.AgentToolCallResult, error)
	ListInspectionTasks(context.Context, domainidentity.Principal) ([]domaincopilot.InspectionTask, error)
	CreateInspectionTask(context.Context, domainidentity.Principal, domaincopilot.InspectionTaskInput, string) (domaincopilot.InspectionTask, error)
	UpdateInspectionTask(context.Context, domainidentity.Principal, string, domaincopilot.InspectionTaskInput, string) (domaincopilot.InspectionTask, error)
	DeleteInspectionTask(context.Context, domainidentity.Principal, string) error
	ListInspectionRuns(context.Context, domainidentity.Principal, domaincopilot.InspectionRunFilter) ([]domaincopilot.InspectionRun, error)
	ExecuteInspectionTask(context.Context, domainidentity.Principal, string, string) (domaincopilot.InspectionRun, error)
	CreateSessionFromInspectionRun(context.Context, domainidentity.Principal, string, string) (domaincopilot.Session, error)
	CreateInspectionTaskFromSession(context.Context, domainidentity.Principal, string, domaincopilot.InspectionTaskInput, string) (domaincopilot.InspectionTask, error)
}

type CopilotHandler struct {
	service     CopilotService
	runnerToken string
}

func NewCopilotHandler(service CopilotService, runnerToken ...string) *CopilotHandler {
	token := ""
	if len(runnerToken) > 0 {
		token = runnerToken[0]
	}
	return &CopilotHandler{service: service, runnerToken: token}
}

func (h *CopilotHandler) ListInsights(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.Insights(c.Request.Context(), principal, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) ListDataSourceCapabilities(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListDataSourceCapabilities(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) GetWorkbenchCatalog(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetWorkbenchCatalog(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CopilotHandler) ListAgentProviders(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAgentProviders(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) ListAgentRuns(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAgentRuns(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CopilotHandler) CancelAgentRun(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CancelAgentRun(c.Request.Context(), principal, c.Param("runID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *CopilotHandler) ClaimAgentRun(c *gin.Context) {
	if !authorizeAIAgentRunner(c, h.runnerToken) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid ai agent runner token")
		return
	}
	var req dto.AgentRunClaimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid ai agent run claim payload")
		return
	}
	item, err := h.service.ClaimAgentRun(c.Request.Context(), domaincopilot.AgentRunClaimInput{
		AgentID:     req.AgentID,
		ProviderIDs: req.ProviderIDs,
		Kinds:       req.Kinds,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *CopilotHandler) RecordAgentRunCallback(c *gin.Context) {
	if !authorizeAIAgentRunner(c, h.runnerToken) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid ai agent runner token")
		return
	}
	var req dto.AgentRunCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid ai agent run callback payload")
		return
	}
	item, err := h.service.RecordAgentRunCallback(c.Request.Context(), domaincopilot.AgentRunCallbackInput{
		RunID:             req.RunID,
		CallbackToken:     req.CallbackToken,
		AgentID:           req.AgentID,
		Status:            req.Status,
		Payload:           req.Payload,
		Events:            req.Events,
		ToolExecutions:    req.ToolExecutions,
		AnalysisArtifacts: req.AnalysisArtifacts,
		ExternalRunID:     req.ExternalRunID,
		ErrorMessage:      req.ErrorMessage,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *CopilotHandler) RecordAgentToolCall(c *gin.Context) {
	if !authorizeAIAgentRunner(c, h.runnerToken) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid ai agent runner token")
		return
	}
	var req dto.AgentToolCallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid ai agent tool call payload")
		return
	}
	item, err := h.service.RecordAgentToolCall(c.Request.Context(), domaincopilot.AgentToolCallInput{
		RunID:         req.RunID,
		CallbackToken: req.CallbackToken,
		AgentID:       req.AgentID,
		ToolBindingID: req.ToolBindingID,
		AdapterID:     req.AdapterID,
		ToolName:      req.ToolName,
		Input:         req.Input,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
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
		AnalysisKinds:      req.AnalysisKinds,
		AgentProviderID:    req.AgentProviderID,
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
		AnalysisKinds:      req.AnalysisKinds,
		AgentProviderID:    req.AgentProviderID,
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

func (h *CopilotHandler) DeleteAutomationPolicy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteAutomationPolicy(c.Request.Context(), principal, c.Param("policyID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
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

func (h *CopilotHandler) GetSession(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetSession(c.Request.Context(), principal, c.Param("sessionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CopilotHandler) CreateSession(c *gin.Context) {
	var req dto.CreateCopilotSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid copilot session payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	scope := req.Scope
	if scope == nil {
		scope = map[string]any{}
	}
	if req.AlertID != "" {
		scope["alertId"] = req.AlertID
	}
	if req.Workload != "" {
		scope["workload"] = req.Workload
	}
	item, err := h.service.CreateSession(c.Request.Context(), principal, req.Title, req.Mode, req.AgentProviderID, scope, req.PinnedContext, req.Source, req.Tags, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CopilotHandler) UpdateSession(c *gin.Context) {
	var req dto.UpdateCopilotSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid copilot session patch payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateSession(c.Request.Context(), principal, c.Param("sessionID"), req.Title, req.Mode, req.AgentProviderID, req.Status, req.Summary, req.Scope, req.PinnedContext, req.Source, req.Toolset, req.Tags, req.Archived)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CopilotHandler) RecordGlobalAssistantEvent(c *gin.Context) {
	var req dto.WorkbenchGlobalAssistantOpenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid global assistant event payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.RecordGlobalAssistantEvent(c.Request.Context(), principal, domaincopilot.WorkbenchGlobalAssistantEventInput{
		Action:           req.Action,
		LaunchContext:    req.LaunchContext,
		SelectionContext: req.SelectionContext,
		Prompt:           req.Prompt,
		SessionID:        req.SessionID,
		Source:           req.Source,
	}); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusAccepted, map[string]any{"ok": true})
}

func (h *CopilotHandler) DeleteSession(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteSession(c.Request.Context(), principal, c.Param("sessionID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusNoContent, nil)
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
	apiresponse.Item(c, http.StatusAccepted, items)
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

func (h *CopilotHandler) ListAnalysisRuns(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAnalysisRuns(c.Request.Context(), principal, domaincopilot.RootCauseRunFilter{
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
		Title:             req.Title,
		Kind:              req.Kind,
		SessionID:         req.SessionID,
		AnalysisProfileID: req.AnalysisProfileID,
		AgentProviderID:   req.AgentProviderID,
		TriggerType:       req.TriggerType,
		ClusterID:         req.ClusterID,
		Namespace:         req.Namespace,
		WorkloadKind:      req.WorkloadKind,
		WorkloadName:      req.WorkloadName,
		AlertID:           req.AlertID,
		TimeRangeMinutes:  req.TimeRangeMinutes,
		Question:          req.Question,
	}, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CopilotHandler) AnalyzeSession(c *gin.Context) {
	var req dto.AnalyzeSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid analyze session payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RunSessionAnalysis(c.Request.Context(), principal, c.Param("sessionID"), domaincopilot.RootCauseRunInput{
		Kind:              req.Mode,
		AnalysisProfileID: req.AnalysisProfileID,
		AgentProviderID:   req.AgentProviderID,
		TriggerType:       req.TriggerType,
		Question:          req.Question,
		ClusterID:         stringValueMap(req.Scope, "clusterId"),
		Namespace:         stringValueMap(req.Scope, "namespace"),
		WorkloadName:      stringValueMap(req.Scope, "workload"),
		AlertID:           stringValueMap(req.Scope, "alertId"),
		TimeRangeMinutes:  intValueMap(req.Scope, "timeRangeMinutes", 60),
	}, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
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

func (h *CopilotHandler) DeleteInspectionTask(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteInspectionTask(c.Request.Context(), principal, c.Param("taskID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
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

func (h *CopilotHandler) CreateSessionFromInspectionRun(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateSessionFromInspectionRun(c.Request.Context(), principal, c.Param("runID"), localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CopilotHandler) CreateInspectionTaskFromSession(c *gin.Context) {
	var req dto.CreateInspectionTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid inspection task payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateInspectionTaskFromSession(c.Request.Context(), principal, c.Param("sessionID"), domaincopilot.InspectionTaskInput{
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

func localeFromRequest(value string) string {
	if len(value) >= 2 && (value[0:2] == "zh" || value[0:2] == "ZH") {
		return "zh-CN"
	}
	return "en-US"
}

func stringValueMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	current, _ := values[key].(string)
	return current
}

func intValueMap(values map[string]any, key string, fallback int) int {
	if values == nil {
		return fallback
	}
	switch current := values[key].(type) {
	case int:
		return current
	case float64:
		return int(current)
	default:
		return fallback
	}
}
