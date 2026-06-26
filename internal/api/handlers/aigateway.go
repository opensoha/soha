package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type AIGatewayService interface {
	Capabilities(context.Context, domainidentity.Principal, domainaigateway.ManifestRequest) (domainaigateway.Manifest, error)
	InvokeTool(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error)
	ReadResource(context.Context, domainidentity.Principal, domainaigateway.ResourceReadRequest) (domainaigateway.ResourceReadResult, error)
	GetPrompt(context.Context, domainidentity.Principal, domainaigateway.PromptGetRequest) (domainaigateway.PromptGetResult, error)
	ListPersonalAccessTokens(context.Context, domainidentity.Principal, domainaigateway.PersonalAccessTokenListRequest) ([]domainaigateway.PersonalAccessToken, error)
	CreatePersonalAccessToken(context.Context, domainidentity.Principal, domainaigateway.PersonalAccessTokenInput) (domainaigateway.CreatedPersonalAccessToken, error)
	RevokePersonalAccessToken(context.Context, domainidentity.Principal, string) error
	RotatePersonalAccessToken(context.Context, domainidentity.Principal, string, domainaigateway.TokenRotationInput) (domainaigateway.CreatedPersonalAccessToken, error)
	ListServiceAccounts(context.Context, domainidentity.Principal) ([]domainaigateway.ServiceAccount, error)
	CreateServiceAccount(context.Context, domainidentity.Principal, domainaigateway.ServiceAccountInput) (domainaigateway.ServiceAccount, error)
	ListServiceAccountTokens(context.Context, domainidentity.Principal) ([]domainaigateway.ServiceAccountToken, error)
	CreateServiceAccountToken(context.Context, domainidentity.Principal, string, domainaigateway.ServiceAccountTokenInput) (domainaigateway.CreatedServiceAccountToken, error)
	RevokeServiceAccountToken(context.Context, domainidentity.Principal, string) error
	RotateServiceAccountToken(context.Context, domainidentity.Principal, string, domainaigateway.TokenRotationInput) (domainaigateway.CreatedServiceAccountToken, error)
	ListAIClients(context.Context, domainidentity.Principal) ([]domainaigateway.AIClient, error)
	CreateAIClient(context.Context, domainidentity.Principal, domainaigateway.AIClientInput) (domainaigateway.AIClient, error)
	UpdateAIClient(context.Context, domainidentity.Principal, string, domainaigateway.AIClientInput) (domainaigateway.AIClient, error)
	ListToolGrants(context.Context, domainidentity.Principal, domainaigateway.ToolGrantFilter) ([]domainaigateway.ToolGrant, error)
	CreateToolGrant(context.Context, domainidentity.Principal, domainaigateway.ToolGrantInput) (domainaigateway.ToolGrant, error)
	DeleteToolGrant(context.Context, domainidentity.Principal, string) error
	ListAccessPolicies(context.Context, domainidentity.Principal, domainaigateway.AccessPolicyFilter) ([]domainaigateway.AccessPolicy, error)
	CreateAccessPolicy(context.Context, domainidentity.Principal, domainaigateway.AccessPolicyInput) (domainaigateway.AccessPolicy, error)
	UpdateAccessPolicy(context.Context, domainidentity.Principal, string, domainaigateway.AccessPolicyInput) (domainaigateway.AccessPolicy, error)
	DeleteAccessPolicy(context.Context, domainidentity.Principal, string) error
	GovernanceStatus(context.Context, domainidentity.Principal, domainaigateway.GovernanceStatusRequest) (domainaigateway.GovernanceStatus, error)
	ListSkillBindings(context.Context, domainidentity.Principal, domainaigateway.SkillBindingFilter) ([]domainaigateway.SkillBinding, error)
	CreateSkillBinding(context.Context, domainidentity.Principal, domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error)
	UpdateSkillBinding(context.Context, domainidentity.Principal, string, domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error)
	DeleteSkillBinding(context.Context, domainidentity.Principal, string) error
	ListAuditLogs(context.Context, domainidentity.Principal, domainaigateway.AuditLogFilter) ([]domainaigateway.AuditLog, error)
	ListApprovalRequests(context.Context, domainidentity.Principal, domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error)
	GetApprovalTimeline(context.Context, domainidentity.Principal, string) (domainaigateway.ApprovalTimeline, error)
	ApproveApprovalRequest(context.Context, domainidentity.Principal, string, domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error)
	RejectApprovalRequest(context.Context, domainidentity.Principal, string, domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error)
	CancelApprovalRequest(context.Context, domainidentity.Principal, string, domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error)
	ListLLMUpstreams(context.Context, domainidentity.Principal, domainaigateway.LLMUpstreamFilter) ([]domainaigateway.LLMUpstream, error)
	CreateLLMUpstream(context.Context, domainidentity.Principal, domainaigateway.LLMUpstreamInput) (domainaigateway.LLMUpstream, error)
	UpdateLLMUpstream(context.Context, domainidentity.Principal, string, domainaigateway.LLMUpstreamInput) (domainaigateway.LLMUpstream, error)
	TestLLMUpstream(context.Context, domainidentity.Principal, string) (domainaigateway.LLMUpstreamTestResult, error)
	RunLLMRelayHealthChecks(context.Context, domainidentity.Principal) (domainaigateway.LLMRelayHealthCheckRun, error)
	ListLLMModelRoutes(context.Context, domainidentity.Principal, domainaigateway.LLMModelRouteFilter) ([]domainaigateway.LLMModelRoute, error)
	CreateLLMModelRoute(context.Context, domainidentity.Principal, domainaigateway.LLMModelRouteInput) (domainaigateway.LLMModelRoute, error)
	UpdateLLMModelRoute(context.Context, domainidentity.Principal, string, domainaigateway.LLMModelRouteInput) (domainaigateway.LLMModelRoute, error)
	DeleteLLMModelRoute(context.Context, domainidentity.Principal, string) error
	ListLLMCallLogs(context.Context, domainidentity.Principal, domainaigateway.LLMCallLogFilter) ([]domainaigateway.LLMCallLog, error)
	LLMRelayMetrics(context.Context, domainidentity.Principal) (domainaigateway.LLMRelayMetrics, error)
	LLMRelayCacheStats(context.Context, domainidentity.Principal, domainaigateway.LLMRelayCacheStatsRequest) (domainaigateway.LLMRelayCacheStats, error)
	PurgeLLMRelayCache(context.Context, domainidentity.Principal, domainaigateway.LLMRelayCachePurgeRequest) (domainaigateway.LLMRelayCachePurgeResult, error)
	LLMRelayMaxRequestBodyBytes() int64
	RelayLLMHTTP(context.Context, domainidentity.Principal, domainidentity.AccessContext, appaigateway.LLMRelayHTTPRequest, http.ResponseWriter) error
	RelayLLMWebSocket(context.Context, domainidentity.Principal, domainidentity.AccessContext, appaigateway.LLMRelayHTTPRequest, http.ResponseWriter, *http.Request) error
}

type AIGatewayHandler struct {
	service AIGatewayService
}

const maxAIGatewayGovernanceWindowHours = 168

func NewAIGatewayHandler(service AIGatewayService) *AIGatewayHandler {
	return &AIGatewayHandler{service: service}
}

func (h *AIGatewayHandler) Capabilities(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	accessCtx := apiMiddleware.AccessContextFromContext(c)
	item, err := h.service.Capabilities(c.Request.Context(), principal, domainaigateway.ManifestRequest{
		AIClientID:   firstNonEmpty(c.Query("aiClientId"), firstHeaderValue(c, "X-Soha-AI-Client-ID", "X-AI-Client-ID")),
		AIClientName: firstNonEmpty(c.Query("aiClientName"), firstHeaderValue(c, "X-Soha-AI-Client", "X-AI-Client")),
		SkillID:      firstNonEmpty(c.Query("skillId"), firstHeaderValue(c, "X-Soha-Skill-ID", "X-Skill-ID")),
		TokenID:      accessCtx.TokenID,
		TokenKind:    accessCtx.TokenKind,
		SessionID:    accessCtx.SessionID,
		SubjectType:  accessCtx.SubjectType,
		SubjectID:    accessCtx.SubjectID,
		Source:       firstNonEmpty(c.Query("source"), firstHeaderValue(c, "X-Soha-Source", "X-Source")),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) InvokeTool(c *gin.Context) {
	var req domainaigateway.ToolInvocationRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI Gateway tool invocation payload")
		return
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}
	req.ToolName = firstNonEmpty(req.ToolName, c.Param("toolName"))
	req.AIClientID = firstNonEmpty(req.AIClientID, firstHeaderValue(c, "X-Soha-AI-Client-ID", "X-AI-Client-ID"))
	req.AIClientName = firstNonEmpty(req.AIClientName, firstHeaderValue(c, "X-Soha-AI-Client", "X-AI-Client"))
	req.SkillID = firstNonEmpty(req.SkillID, firstHeaderValue(c, "X-Soha-Skill-ID", "X-Skill-ID"))
	req.RequestID = firstNonEmpty(req.RequestID, c.GetString("request_id"))
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.InvokeTool(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) ReadResource(c *gin.Context) {
	var req domainaigateway.ResourceReadRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI Gateway resource read payload")
		return
	}
	if req.Context == nil {
		req.Context = map[string]any{}
	}
	req.Name = firstNonEmpty(req.Name, req.URI, c.Query("name"), c.Query("uri"))
	req.URI = firstNonEmpty(req.URI, req.Name)
	req.AIClientID = firstNonEmpty(req.AIClientID, firstHeaderValue(c, "X-Soha-AI-Client-ID", "X-AI-Client-ID"))
	req.AIClientName = firstNonEmpty(req.AIClientName, firstHeaderValue(c, "X-Soha-AI-Client", "X-AI-Client"))
	req.SkillID = firstNonEmpty(req.SkillID, firstHeaderValue(c, "X-Soha-Skill-ID", "X-Skill-ID"))
	req.RequestID = firstNonEmpty(req.RequestID, c.GetString("request_id"))
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.ReadResource(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) GetPrompt(c *gin.Context) {
	var req domainaigateway.PromptGetRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI Gateway prompt payload")
		return
	}
	if req.Arguments == nil {
		req.Arguments = map[string]any{}
	}
	if req.Context == nil {
		req.Context = map[string]any{}
	}
	req.Name = firstNonEmpty(req.Name, c.Query("name"))
	req.AIClientID = firstNonEmpty(req.AIClientID, firstHeaderValue(c, "X-Soha-AI-Client-ID", "X-AI-Client-ID"))
	req.AIClientName = firstNonEmpty(req.AIClientName, firstHeaderValue(c, "X-Soha-AI-Client", "X-AI-Client"))
	req.SkillID = firstNonEmpty(req.SkillID, firstHeaderValue(c, "X-Soha-Skill-ID", "X-Skill-ID"))
	req.RequestID = firstNonEmpty(req.RequestID, c.GetString("request_id"))
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetPrompt(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) ListPersonalAccessTokens(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListPersonalAccessTokens(c.Request.Context(), principal, domainaigateway.PersonalAccessTokenListRequest{
		Scope:  c.Query("scope"),
		UserID: c.Query("userId"),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) CreatePersonalAccessToken(c *gin.Context) {
	var req domainaigateway.PersonalAccessTokenInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal access token payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreatePersonalAccessToken(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) RevokePersonalAccessToken(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.RevokePersonalAccessToken(c.Request.Context(), principal, c.Param("tokenID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AIGatewayHandler) RotatePersonalAccessToken(c *gin.Context) {
	var req domainaigateway.TokenRotationInput
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal access token rotation payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RotatePersonalAccessToken(c.Request.Context(), principal, c.Param("tokenID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) ListServiceAccounts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListServiceAccounts(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) CreateServiceAccount(c *gin.Context) {
	var req domainaigateway.ServiceAccountInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid service account payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateServiceAccount(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) ListServiceAccountTokens(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListServiceAccountTokens(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) CreateServiceAccountToken(c *gin.Context) {
	var req domainaigateway.ServiceAccountTokenInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid service account token payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateServiceAccountToken(c.Request.Context(), principal, c.Param("serviceAccountID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) RevokeServiceAccountToken(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.RevokeServiceAccountToken(c.Request.Context(), principal, c.Param("tokenID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AIGatewayHandler) RotateServiceAccountToken(c *gin.Context) {
	var req domainaigateway.TokenRotationInput
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid service account token rotation payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RotateServiceAccountToken(c.Request.Context(), principal, c.Param("tokenID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) ListAIClients(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAIClients(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) CreateAIClient(c *gin.Context) {
	var req domainaigateway.AIClientInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI client payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateAIClient(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) UpdateAIClient(c *gin.Context) {
	var req domainaigateway.AIClientInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI client payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateAIClient(c.Request.Context(), principal, c.Param("clientID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) ListToolGrants(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListToolGrants(c.Request.Context(), principal, domainaigateway.ToolGrantFilter{
		SubjectType:    c.Query("subjectType"),
		SubjectID:      c.Query("subjectId"),
		AIClientID:     c.Query("aiClientId"),
		ToolName:       c.Query("toolName"),
		IncludeExpired: c.Query("includeExpired") == "true",
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) CreateToolGrant(c *gin.Context) {
	var req domainaigateway.ToolGrantInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid MCP tool grant payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateToolGrant(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) DeleteToolGrant(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteToolGrant(c.Request.Context(), principal, c.Param("grantID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AIGatewayHandler) ListAccessPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAccessPolicies(c.Request.Context(), principal, domainaigateway.AccessPolicyFilter{
		SubjectType:     c.Query("subjectType"),
		SubjectID:       c.Query("subjectId"),
		AIClientID:      c.Query("aiClientId"),
		Effect:          c.Query("effect"),
		IncludeDisabled: c.Query("includeDisabled") == "true",
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) CreateAccessPolicy(c *gin.Context) {
	var req domainaigateway.AccessPolicyInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI access policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateAccessPolicy(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) UpdateAccessPolicy(c *gin.Context) {
	var req domainaigateway.AccessPolicyInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI access policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateAccessPolicy(c.Request.Context(), principal, c.Param("policyID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) DeleteAccessPolicy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteAccessPolicy(c.Request.Context(), principal, c.Param("policyID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AIGatewayHandler) GovernanceStatus(c *gin.Context) {
	windowHours, err := parseAIGatewayWindowHours(c.Query("windowHours"), 0)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GovernanceStatus(c.Request.Context(), principal, domainaigateway.GovernanceStatusRequest{WindowHours: windowHours})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) ListSkillBindings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListSkillBindings(c.Request.Context(), principal, domainaigateway.SkillBindingFilter{
		SubjectType:     c.Query("subjectType"),
		SubjectID:       c.Query("subjectId"),
		AIClientID:      c.Query("aiClientId"),
		SkillID:         c.Query("skillId"),
		IncludeDisabled: c.Query("includeDisabled") == "true",
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) CreateSkillBinding(c *gin.Context) {
	var req domainaigateway.SkillBindingInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI skill binding payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateSkillBinding(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) UpdateSkillBinding(c *gin.Context) {
	var req domainaigateway.SkillBindingInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI skill binding payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateSkillBinding(c.Request.Context(), principal, c.Param("bindingID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) DeleteSkillBinding(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteSkillBinding(c.Request.Context(), principal, c.Param("bindingID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AIGatewayHandler) ListAuditLogs(c *gin.Context) {
	filter, err := parseAIGatewayAuditLogFilter(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAuditLogs(c.Request.Context(), principal, filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) ListApprovalRequests(c *gin.Context) {
	filter, err := parseAIGatewayApprovalRequestFilter(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListApprovalRequests(c.Request.Context(), principal, filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) GetApprovalTimeline(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApprovalTimeline(c.Request.Context(), principal, c.Param("requestID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) ApproveApprovalRequest(c *gin.Context) {
	h.decideApprovalRequest(c, "approve")
}

func (h *AIGatewayHandler) RejectApprovalRequest(c *gin.Context) {
	h.decideApprovalRequest(c, "reject")
}

func (h *AIGatewayHandler) CancelApprovalRequest(c *gin.Context) {
	h.decideApprovalRequest(c, "cancel")
}

func (h *AIGatewayHandler) ListLLMUpstreams(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListLLMUpstreams(c.Request.Context(), principal, domainaigateway.LLMUpstreamFilter{
		ProviderKind: c.Query("providerKind"),
		Status:       c.Query("status"),
		IncludeAll:   parseBoolQuery(c.Query("includeAll")),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) CreateLLMUpstream(c *gin.Context) {
	var req domainaigateway.LLMUpstreamInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid LLM upstream payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateLLMUpstream(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) UpdateLLMUpstream(c *gin.Context) {
	var req domainaigateway.LLMUpstreamInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid LLM upstream payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateLLMUpstream(c.Request.Context(), principal, c.Param("upstreamID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) TestLLMUpstream(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.TestLLMUpstream(c.Request.Context(), principal, c.Param("upstreamID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) RunLLMRelayHealthChecks(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RunLLMRelayHealthChecks(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) ListLLMModelRoutes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListLLMModelRoutes(c.Request.Context(), principal, domainaigateway.LLMModelRouteFilter{
		PublicModel:     c.Query("publicModel"),
		ProviderKind:    c.Query("providerKind"),
		UpstreamID:      c.Query("upstreamId"),
		RouteGroup:      c.Query("routeGroup"),
		IncludeDisabled: parseBoolQuery(c.Query("includeDisabled")),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) CreateLLMModelRoute(c *gin.Context) {
	var req domainaigateway.LLMModelRouteInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid LLM model route payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateLLMModelRoute(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AIGatewayHandler) UpdateLLMModelRoute(c *gin.Context) {
	var req domainaigateway.LLMModelRouteInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid LLM model route payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateLLMModelRoute(c.Request.Context(), principal, c.Param("routeID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) DeleteLLMModelRoute(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteLLMModelRoute(c.Request.Context(), principal, c.Param("routeID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AIGatewayHandler) ListLLMCallLogs(c *gin.Context) {
	filter, err := parseLLMCallLogFilter(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListLLMCallLogs(c.Request.Context(), principal, filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AIGatewayHandler) LLMRelayMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.LLMRelayMetrics(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) LLMRelayCacheStats(c *gin.Context) {
	windowHours, err := parseAIGatewayWindowHours(c.Query("windowHours"), 24)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.LLMRelayCacheStats(c.Request.Context(), principal, domainaigateway.LLMRelayCacheStatsRequest{
		WindowHours: windowHours,
		PublicModel: c.Query("publicModel"),
		UpstreamID:  c.Query("upstreamId"),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) PurgeLLMRelayCache(c *gin.Context) {
	var req domainaigateway.LLMRelayCachePurgeRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid LLM relay cache purge payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.PurgeLLMRelayCache(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AIGatewayHandler) RelayOpenAIModels(c *gin.Context) {
	h.relayLLM(c, "openai", "models")
}

func (h *AIGatewayHandler) RelayOpenAIChatCompletions(c *gin.Context) {
	h.relayLLM(c, "openai", "chat/completions")
}

func (h *AIGatewayHandler) RelayOpenAIResponses(c *gin.Context) {
	h.relayLLM(c, "openai", "responses")
}

func (h *AIGatewayHandler) RelayOpenAIEmbeddings(c *gin.Context) {
	h.relayLLM(c, "openai", "embeddings")
}

func (h *AIGatewayHandler) RelayOpenAIImageGenerations(c *gin.Context) {
	h.relayLLM(c, "openai", "images/generations")
}

func (h *AIGatewayHandler) RelayOpenAIImageEdits(c *gin.Context) {
	h.relayLLM(c, "openai", "images/edits")
}

func (h *AIGatewayHandler) RelayOpenAIImageVariations(c *gin.Context) {
	h.relayLLM(c, "openai", "images/variations")
}

func (h *AIGatewayHandler) RelayOpenAIAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "openai", "audio/speech")
}

func (h *AIGatewayHandler) RelayOpenAIAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "openai", "audio/transcriptions")
}

func (h *AIGatewayHandler) RelayOpenAIAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "openai", "audio/translations")
}

func (h *AIGatewayHandler) RelayOpenAIRealtime(c *gin.Context) {
	h.relayLLMWebSocket(c, "openai", "realtime")
}

func (h *AIGatewayHandler) RelayDeepSeekModels(c *gin.Context) {
	h.relayLLM(c, "deepseek", "models")
}

func (h *AIGatewayHandler) RelayDeepSeekChatCompletions(c *gin.Context) {
	h.relayLLM(c, "deepseek", "chat/completions")
}

func (h *AIGatewayHandler) RelayDeepSeekResponses(c *gin.Context) {
	h.relayLLM(c, "deepseek", "responses")
}

func (h *AIGatewayHandler) RelayDeepSeekEmbeddings(c *gin.Context) {
	h.relayLLM(c, "deepseek", "embeddings")
}

func (h *AIGatewayHandler) RelayDeepSeekImageGenerations(c *gin.Context) {
	h.relayLLM(c, "deepseek", "images/generations")
}

func (h *AIGatewayHandler) RelayDeepSeekImageEdits(c *gin.Context) {
	h.relayLLM(c, "deepseek", "images/edits")
}

func (h *AIGatewayHandler) RelayDeepSeekImageVariations(c *gin.Context) {
	h.relayLLM(c, "deepseek", "images/variations")
}

func (h *AIGatewayHandler) RelayDeepSeekAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "deepseek", "audio/speech")
}

func (h *AIGatewayHandler) RelayDeepSeekAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "deepseek", "audio/transcriptions")
}

func (h *AIGatewayHandler) RelayDeepSeekAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "deepseek", "audio/translations")
}

func (h *AIGatewayHandler) RelayQwenModels(c *gin.Context) {
	h.relayLLM(c, "qwen", "models")
}

func (h *AIGatewayHandler) RelayQwenChatCompletions(c *gin.Context) {
	h.relayLLM(c, "qwen", "chat/completions")
}

func (h *AIGatewayHandler) RelayQwenResponses(c *gin.Context) {
	h.relayLLM(c, "qwen", "responses")
}

func (h *AIGatewayHandler) RelayQwenEmbeddings(c *gin.Context) {
	h.relayLLM(c, "qwen", "embeddings")
}

func (h *AIGatewayHandler) RelayQwenImageGenerations(c *gin.Context) {
	h.relayLLM(c, "qwen", "images/generations")
}

func (h *AIGatewayHandler) RelayQwenImageEdits(c *gin.Context) {
	h.relayLLM(c, "qwen", "images/edits")
}

func (h *AIGatewayHandler) RelayQwenImageVariations(c *gin.Context) {
	h.relayLLM(c, "qwen", "images/variations")
}

func (h *AIGatewayHandler) RelayQwenAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "qwen", "audio/speech")
}

func (h *AIGatewayHandler) RelayQwenAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "qwen", "audio/transcriptions")
}

func (h *AIGatewayHandler) RelayQwenAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "qwen", "audio/translations")
}

func (h *AIGatewayHandler) RelayOpenRouterModels(c *gin.Context) {
	h.relayLLM(c, "openrouter", "models")
}

func (h *AIGatewayHandler) RelayOpenRouterChatCompletions(c *gin.Context) {
	h.relayLLM(c, "openrouter", "chat/completions")
}

func (h *AIGatewayHandler) RelayOpenRouterResponses(c *gin.Context) {
	h.relayLLM(c, "openrouter", "responses")
}

func (h *AIGatewayHandler) RelayOpenRouterEmbeddings(c *gin.Context) {
	h.relayLLM(c, "openrouter", "embeddings")
}

func (h *AIGatewayHandler) RelayOpenRouterImageGenerations(c *gin.Context) {
	h.relayLLM(c, "openrouter", "images/generations")
}

func (h *AIGatewayHandler) RelayOpenRouterImageEdits(c *gin.Context) {
	h.relayLLM(c, "openrouter", "images/edits")
}

func (h *AIGatewayHandler) RelayOpenRouterImageVariations(c *gin.Context) {
	h.relayLLM(c, "openrouter", "images/variations")
}

func (h *AIGatewayHandler) RelayOpenRouterAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "openrouter", "audio/speech")
}

func (h *AIGatewayHandler) RelayOpenRouterAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "openrouter", "audio/transcriptions")
}

func (h *AIGatewayHandler) RelayOpenRouterAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "openrouter", "audio/translations")
}

func (h *AIGatewayHandler) RelayAzureOpenAIModels(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "models")
}

func (h *AIGatewayHandler) RelayAzureOpenAIChatCompletions(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "chat/completions")
}

func (h *AIGatewayHandler) RelayAzureOpenAIResponses(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "responses")
}

func (h *AIGatewayHandler) RelayAzureOpenAIEmbeddings(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "embeddings")
}

func (h *AIGatewayHandler) RelayAzureOpenAIImageGenerations(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "images/generations")
}

func (h *AIGatewayHandler) RelayAzureOpenAIImageEdits(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "images/edits")
}

func (h *AIGatewayHandler) RelayAzureOpenAIImageVariations(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "images/variations")
}

func (h *AIGatewayHandler) RelayAzureOpenAIAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "audio/speech")
}

func (h *AIGatewayHandler) RelayAzureOpenAIAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "audio/transcriptions")
}

func (h *AIGatewayHandler) RelayAzureOpenAIAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "audio/translations")
}

func (h *AIGatewayHandler) RelayGeminiModels(c *gin.Context) {
	h.relayLLM(c, "gemini", "models")
}

func (h *AIGatewayHandler) RelayGeminiInteractions(c *gin.Context) {
	h.relayLLM(c, "gemini", "interactions")
}

func (h *AIGatewayHandler) RelayGeminiModelAction(c *gin.Context) {
	pathModel, endpoint, ok := parseGeminiRelayModelAction(c.Param("modelAction"))
	if !ok {
		relayNativeError(c, http.StatusBadRequest, "invalid_argument", "unsupported Gemini relay model action")
		return
	}
	h.relayLLMWithPathModel(c, "gemini", endpoint, pathModel)
}

func (h *AIGatewayHandler) RelayCohereRerank(c *gin.Context) {
	h.relayLLM(c, "cohere", "rerank")
}

func (h *AIGatewayHandler) RelayAnthropicModels(c *gin.Context) {
	h.relayLLM(c, "anthropic", "models")
}

func (h *AIGatewayHandler) RelayAnthropicMessages(c *gin.Context) {
	h.relayLLM(c, "anthropic", "messages")
}

func (h *AIGatewayHandler) relayLLM(c *gin.Context, providerKind, endpoint string) {
	h.relayLLMWithPathModel(c, providerKind, endpoint, "")
}

func (h *AIGatewayHandler) relayLLMWebSocket(c *gin.Context, providerKind, endpoint string) {
	principal := apiMiddleware.PrincipalFromContext(c)
	accessCtx := apiMiddleware.AccessContextFromContext(c)
	err := h.service.RelayLLMWebSocket(c.Request.Context(), principal, accessCtx, appaigateway.LLMRelayHTTPRequest{
		ProviderKind: providerKind,
		Endpoint:     endpoint,
		QueryModel:   c.Query("model"),
		Method:       c.Request.Method,
		Headers:      c.Request.Header.Clone(),
		RequestID:    c.GetString("request_id"),
		SourceIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	}, c.Writer, c.Request)
	if err != nil {
		relayNativeError(c, apiStatusCode(err), apiErrorCode(err), err.Error())
	}
}

func (h *AIGatewayHandler) relayLLMWithPathModel(c *gin.Context, providerKind, endpoint, pathModel string) {
	var body []byte
	if c.Request.Body != nil {
		limited := http.MaxBytesReader(c.Writer, c.Request.Body, h.service.LLMRelayMaxRequestBodyBytes())
		var err error
		body, err = io.ReadAll(limited)
		if err != nil {
			relayNativeError(c, http.StatusRequestEntityTooLarge, "request_too_large", "relay request body is too large")
			return
		}
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	accessCtx := apiMiddleware.AccessContextFromContext(c)
	err := h.service.RelayLLMHTTP(c.Request.Context(), principal, accessCtx, appaigateway.LLMRelayHTTPRequest{
		ProviderKind: providerKind,
		Endpoint:     endpoint,
		PathModel:    pathModel,
		Method:       c.Request.Method,
		Headers:      c.Request.Header.Clone(),
		Body:         body,
		RequestID:    c.GetString("request_id"),
		SourceIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	}, c.Writer)
	if err != nil {
		relayNativeError(c, apiStatusCode(err), apiErrorCode(err), err.Error())
	}
}

func parseGeminiRelayModelAction(modelAction string) (string, string, bool) {
	modelAction = strings.TrimPrefix(strings.TrimSpace(modelAction), "/")
	separator := strings.LastIndex(modelAction, ":")
	if separator <= 0 || separator == len(modelAction)-1 {
		return "", "", false
	}
	model := strings.TrimSpace(modelAction[:separator])
	action := strings.TrimSpace(modelAction[separator+1:])
	switch action {
	case "generateContent", "streamGenerateContent":
		return model, action, true
	default:
		return "", "", false
	}
}

func (h *AIGatewayHandler) decideApprovalRequest(c *gin.Context, action string) {
	var req domainaigateway.ApprovalDecisionInput
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid AI Gateway approval decision payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	var (
		item domainaigateway.ApprovalDecisionResult
		err  error
	)
	switch action {
	case "approve":
		item, err = h.service.ApproveApprovalRequest(c.Request.Context(), principal, c.Param("requestID"), req)
	case "reject":
		item, err = h.service.RejectApprovalRequest(c.Request.Context(), principal, c.Param("requestID"), req)
	case "cancel":
		item, err = h.service.CancelApprovalRequest(c.Request.Context(), principal, c.Param("requestID"), req)
	default:
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "unsupported approval decision")
		return
	}
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func parseAIGatewayApprovalRequestFilter(c *gin.Context) (domainaigateway.ApprovalRequestFilter, error) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return domainaigateway.ApprovalRequestFilter{}, errors.New("invalid approval request limit")
		}
		limit = parsed
	}
	from, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("from"), c.Query("startTime"), c.Query("createdAtFrom")))
	if err != nil {
		return domainaigateway.ApprovalRequestFilter{}, err
	}
	to, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("to"), c.Query("endTime"), c.Query("createdAtTo")))
	if err != nil {
		return domainaigateway.ApprovalRequestFilter{}, err
	}
	return domainaigateway.ApprovalRequestFilter{
		ID:         firstNonEmpty(c.Query("id"), c.Query("requestID"), c.Query("approvalRequestId")),
		Status:     c.Query("status"),
		ActorType:  c.Query("actorType"),
		ActorID:    firstNonEmpty(c.Query("actorId"), c.Query("actor")),
		AIClientID: c.Query("aiClientId"),
		SkillID:    c.Query("skillId"),
		ToolName:   c.Query("toolName"),
		RiskLevel:  domainaigateway.RiskLevel(c.Query("riskLevel")),
		Strategy:   c.Query("strategy"),
		From:       from,
		To:         to,
		Limit:      limit,
	}, nil
}

func parseAIGatewayAuditLogFilter(c *gin.Context) (domainaigateway.AuditLogFilter, error) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return domainaigateway.AuditLogFilter{}, errors.New("invalid audit limit")
		}
		limit = parsed
	}
	from, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("from"), c.Query("startTime"), c.Query("createdAtFrom")))
	if err != nil {
		return domainaigateway.AuditLogFilter{}, err
	}
	to, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("to"), c.Query("endTime"), c.Query("createdAtTo")))
	if err != nil {
		return domainaigateway.AuditLogFilter{}, err
	}
	return domainaigateway.AuditLogFilter{
		ActorType:         c.Query("actorType"),
		ActorID:           firstNonEmpty(c.Query("actorId"), c.Query("actor")),
		AIClientID:        c.Query("aiClientId"),
		SkillID:           c.Query("skillId"),
		ToolName:          c.Query("toolName"),
		ApprovalRequestID: firstNonEmpty(c.Query("approvalRequestId"), c.Query("approvalRequestID")),
		RiskLevel:         domainaigateway.RiskLevel(c.Query("riskLevel")),
		Result:            c.Query("result"),
		Action:            c.Query("action"),
		From:              from,
		To:                to,
		Limit:             limit,
	}, nil
}

func parseAIGatewayAuditTime(value string) (*time.Time, error) {
	value = firstNonEmpty(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("invalid audit time; use RFC3339")
	}
	return &parsed, nil
}

func parseAIGatewayWindowHours(raw string, defaultValue int) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("windowHours must be an integer")
	}
	if parsed != 0 && (parsed < 1 || parsed > maxAIGatewayGovernanceWindowHours) {
		return 0, errors.New("windowHours must be 0 or between 1 and 168")
	}
	return parsed, nil
}

func parseLLMCallLogFilter(c *gin.Context) (domainaigateway.LLMCallLogFilter, error) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return domainaigateway.LLMCallLogFilter{}, errors.New("invalid model call log limit")
		}
		limit = parsed
	}
	from, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("from"), c.Query("startTime"), c.Query("createdAtFrom")))
	if err != nil {
		return domainaigateway.LLMCallLogFilter{}, err
	}
	to, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("to"), c.Query("endTime"), c.Query("createdAtTo")))
	if err != nil {
		return domainaigateway.LLMCallLogFilter{}, err
	}
	return domainaigateway.LLMCallLogFilter{
		ActorType:    c.Query("actorType"),
		ActorID:      firstNonEmpty(c.Query("actorId"), c.Query("actor")),
		TokenID:      firstNonEmpty(c.Query("tokenId"), c.Query("tokenID")),
		TokenKind:    c.Query("tokenKind"),
		AIClientID:   c.Query("aiClientId"),
		PublicModel:  c.Query("publicModel"),
		UpstreamID:   c.Query("upstreamId"),
		ProviderKind: c.Query("providerKind"),
		Status:       c.Query("status"),
		Endpoint:     c.Query("endpoint"),
		CacheStatus:  c.Query("cacheStatus"),
		From:         from,
		To:           to,
		Limit:        limit,
	}, nil
}

func parseBoolQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func apiStatusCode(err error) int {
	return apierrors.StatusCode(err)
}

func apiErrorCode(err error) string {
	return apierrors.Code(err)
}

func relayNativeError(c *gin.Context, status int, code, message string) {
	if status == http.StatusInternalServerError {
		message = "internal server error"
	}
	if strings.TrimSpace(code) == "" {
		code = "error"
	}
	if strings.TrimSpace(message) == "" {
		message = code
	}
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    code,
			"code":    code,
		},
	})
}

func firstHeaderValue(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := c.GetHeader(name); value != "" {
			return value
		}
	}
	return ""
}
