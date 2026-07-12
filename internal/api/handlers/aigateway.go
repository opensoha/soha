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

type AIGatewayCapabilityService interface {
	Capabilities(context.Context, domainidentity.Principal, domainaigateway.ManifestRequest) (domainaigateway.Manifest, error)
	InvokeTool(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error)
	ReadResource(context.Context, domainidentity.Principal, domainaigateway.ResourceReadRequest) (domainaigateway.ResourceReadResult, error)
	GetPrompt(context.Context, domainidentity.Principal, domainaigateway.PromptGetRequest) (domainaigateway.PromptGetResult, error)
}

type AIGatewayPersonalTokenService interface {
	ListPersonalAccessTokens(context.Context, domainidentity.Principal, domainaigateway.PersonalAccessTokenListRequest) ([]domainaigateway.PersonalAccessToken, error)
	CreatePersonalAccessToken(context.Context, domainidentity.Principal, domainaigateway.PersonalAccessTokenInput) (domainaigateway.CreatedPersonalAccessToken, error)
	RevokePersonalAccessToken(context.Context, domainidentity.Principal, string) error
	RotatePersonalAccessToken(context.Context, domainidentity.Principal, string, domainaigateway.TokenRotationInput) (domainaigateway.CreatedPersonalAccessToken, error)
}

type AIGatewayServiceAccountService interface {
	ListServiceAccounts(context.Context, domainidentity.Principal) ([]domainaigateway.ServiceAccount, error)
	CreateServiceAccount(context.Context, domainidentity.Principal, domainaigateway.ServiceAccountInput) (domainaigateway.ServiceAccount, error)
	ListServiceAccountTokens(context.Context, domainidentity.Principal) ([]domainaigateway.ServiceAccountToken, error)
	CreateServiceAccountToken(context.Context, domainidentity.Principal, string, domainaigateway.ServiceAccountTokenInput) (domainaigateway.CreatedServiceAccountToken, error)
	RevokeServiceAccountToken(context.Context, domainidentity.Principal, string) error
	RotateServiceAccountToken(context.Context, domainidentity.Principal, string, domainaigateway.TokenRotationInput) (domainaigateway.CreatedServiceAccountToken, error)
}

type AIGatewayClientService interface {
	ListAIClients(context.Context, domainidentity.Principal) ([]domainaigateway.AIClient, error)
	CreateAIClient(context.Context, domainidentity.Principal, domainaigateway.AIClientInput) (domainaigateway.AIClient, error)
	UpdateAIClient(context.Context, domainidentity.Principal, string, domainaigateway.AIClientInput) (domainaigateway.AIClient, error)
}

type AIGatewayToolGrantService interface {
	ListToolGrants(context.Context, domainidentity.Principal, domainaigateway.ToolGrantFilter) ([]domainaigateway.ToolGrant, error)
	CreateToolGrant(context.Context, domainidentity.Principal, domainaigateway.ToolGrantInput) (domainaigateway.ToolGrant, error)
	DeleteToolGrant(context.Context, domainidentity.Principal, string) error
}

type AIGatewayAccessPolicyService interface {
	ListAccessPolicies(context.Context, domainidentity.Principal, domainaigateway.AccessPolicyFilter) ([]domainaigateway.AccessPolicy, error)
	CreateAccessPolicy(context.Context, domainidentity.Principal, domainaigateway.AccessPolicyInput) (domainaigateway.AccessPolicy, error)
	UpdateAccessPolicy(context.Context, domainidentity.Principal, string, domainaigateway.AccessPolicyInput) (domainaigateway.AccessPolicy, error)
	DeleteAccessPolicy(context.Context, domainidentity.Principal, string) error
}

type AIGatewayGovernanceService interface {
	GovernanceStatus(context.Context, domainidentity.Principal, domainaigateway.GovernanceStatusRequest) (domainaigateway.GovernanceStatus, error)
	ListSkillBindings(context.Context, domainidentity.Principal, domainaigateway.SkillBindingFilter) ([]domainaigateway.SkillBinding, error)
	CreateSkillBinding(context.Context, domainidentity.Principal, domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error)
	UpdateSkillBinding(context.Context, domainidentity.Principal, string, domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error)
	DeleteSkillBinding(context.Context, domainidentity.Principal, string) error
}

type AIGatewayAuditService interface {
	ListAuditLogs(context.Context, domainidentity.Principal, domainaigateway.AuditLogFilter) ([]domainaigateway.AuditLog, error)
}

type AIGatewayApprovalService interface {
	ListApprovalRequests(context.Context, domainidentity.Principal, domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error)
	GetApprovalTimeline(context.Context, domainidentity.Principal, string) (domainaigateway.ApprovalTimeline, error)
	ApproveApprovalRequest(context.Context, domainidentity.Principal, string, domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error)
	RejectApprovalRequest(context.Context, domainidentity.Principal, string, domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error)
	CancelApprovalRequest(context.Context, domainidentity.Principal, string, domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error)
}

type AIGatewayUpstreamService interface {
	ListLLMUpstreams(context.Context, domainidentity.Principal, domainaigateway.LLMUpstreamFilter) ([]domainaigateway.LLMUpstream, error)
	CreateLLMUpstream(context.Context, domainidentity.Principal, domainaigateway.LLMUpstreamInput) (domainaigateway.LLMUpstream, error)
	UpdateLLMUpstream(context.Context, domainidentity.Principal, string, domainaigateway.LLMUpstreamInput) (domainaigateway.LLMUpstream, error)
	TestLLMUpstream(context.Context, domainidentity.Principal, string) (domainaigateway.LLMUpstreamTestResult, error)
	RunLLMRelayHealthChecks(context.Context, domainidentity.Principal) (domainaigateway.LLMRelayHealthCheckRun, error)
}

type AIGatewayModelRouteService interface {
	ListLLMModelRoutes(context.Context, domainidentity.Principal, domainaigateway.LLMModelRouteFilter) ([]domainaigateway.LLMModelRoute, error)
	CreateLLMModelRoute(context.Context, domainidentity.Principal, domainaigateway.LLMModelRouteInput) (domainaigateway.LLMModelRoute, error)
	UpdateLLMModelRoute(context.Context, domainidentity.Principal, string, domainaigateway.LLMModelRouteInput) (domainaigateway.LLMModelRoute, error)
	DeleteLLMModelRoute(context.Context, domainidentity.Principal, string) error
}

type AIGatewayRelayObservabilityService interface {
	ListLLMCallLogs(context.Context, domainidentity.Principal, domainaigateway.LLMCallLogFilter) ([]domainaigateway.LLMCallLog, error)
	LLMRelayMetrics(context.Context, domainidentity.Principal) (domainaigateway.LLMRelayMetrics, error)
	LLMRelayCacheStats(context.Context, domainidentity.Principal, domainaigateway.LLMRelayCacheStatsRequest) (domainaigateway.LLMRelayCacheStats, error)
	PurgeLLMRelayCache(context.Context, domainidentity.Principal, domainaigateway.LLMRelayCachePurgeRequest) (domainaigateway.LLMRelayCachePurgeResult, error)
}

type AIGatewayRelayService interface {
	LLMRelayMaxRequestBodyBytes() int64
	RelayLLMHTTP(context.Context, domainidentity.Principal, domainidentity.AccessContext, appaigateway.LLMRelayHTTPRequest, http.ResponseWriter) error
	RelayLLMWebSocket(context.Context, domainidentity.Principal, domainidentity.AccessContext, appaigateway.LLMRelayHTTPRequest, http.ResponseWriter, *http.Request) error
}

type AIGatewayService interface {
	AIGatewayCapabilityService
	AIGatewayPersonalTokenService
	AIGatewayServiceAccountService
	AIGatewayClientService
	AIGatewayToolGrantService
	AIGatewayAccessPolicyService
	AIGatewayGovernanceService
	AIGatewayAuditService
	AIGatewayApprovalService
	AIGatewayUpstreamService
	AIGatewayModelRouteService
	AIGatewayRelayObservabilityService
	AIGatewayRelayService
}

type AIGatewayHandler struct {
	aiGatewayCapabilityHandler
	aiGatewayPersonalTokenHandler
	aiGatewayServiceAccountHandler
	aiGatewayClientHandler
	aiGatewayToolGrantHandler
	aiGatewayAccessPolicyHandler
	aiGatewayGovernanceHandler
	aiGatewayAuditHandler
	aiGatewayApprovalHandler
	aiGatewayUpstreamHandler
	aiGatewayModelRouteHandler
	aiGatewayRelayObservabilityHandler
	aiGatewayRelayHandler
}

const maxAIGatewayGovernanceWindowHours = 168

func NewAIGatewayHandler(service AIGatewayService) *AIGatewayHandler {
	return NewAIGatewayHandlerWithServices(AIGatewayServices{
		Capabilities: service, PersonalTokens: service, ServiceAccounts: service,
		Clients: service, ToolGrants: service, AccessPolicies: service, Governance: service,
		Audit: service, Approvals: service, Upstreams: service, ModelRoutes: service,
		RelayObservability: service, Relay: service,
	})
}

type AIGatewayServices struct {
	Capabilities       AIGatewayCapabilityService
	PersonalTokens     AIGatewayPersonalTokenService
	ServiceAccounts    AIGatewayServiceAccountService
	Clients            AIGatewayClientService
	ToolGrants         AIGatewayToolGrantService
	AccessPolicies     AIGatewayAccessPolicyService
	Governance         AIGatewayGovernanceService
	Audit              AIGatewayAuditService
	Approvals          AIGatewayApprovalService
	Upstreams          AIGatewayUpstreamService
	ModelRoutes        AIGatewayModelRouteService
	RelayObservability AIGatewayRelayObservabilityService
	Relay              AIGatewayRelayService
}

type aiGatewayCapabilityHandler struct{ service AIGatewayCapabilityService }
type aiGatewayPersonalTokenHandler struct{ service AIGatewayPersonalTokenService }
type aiGatewayServiceAccountHandler struct {
	service AIGatewayServiceAccountService
}
type aiGatewayClientHandler struct{ service AIGatewayClientService }
type aiGatewayToolGrantHandler struct{ service AIGatewayToolGrantService }
type aiGatewayAccessPolicyHandler struct{ service AIGatewayAccessPolicyService }
type aiGatewayGovernanceHandler struct{ service AIGatewayGovernanceService }
type aiGatewayAuditHandler struct{ service AIGatewayAuditService }
type aiGatewayApprovalHandler struct{ service AIGatewayApprovalService }
type aiGatewayUpstreamHandler struct{ service AIGatewayUpstreamService }
type aiGatewayModelRouteHandler struct{ service AIGatewayModelRouteService }
type aiGatewayRelayObservabilityHandler struct {
	service AIGatewayRelayObservabilityService
}
type aiGatewayRelayHandler struct{ service AIGatewayRelayService }

func NewAIGatewayHandlerWithServices(services AIGatewayServices) *AIGatewayHandler {
	return &AIGatewayHandler{
		aiGatewayCapabilityHandler:         aiGatewayCapabilityHandler{service: services.Capabilities},
		aiGatewayPersonalTokenHandler:      aiGatewayPersonalTokenHandler{service: services.PersonalTokens},
		aiGatewayServiceAccountHandler:     aiGatewayServiceAccountHandler{service: services.ServiceAccounts},
		aiGatewayClientHandler:             aiGatewayClientHandler{service: services.Clients},
		aiGatewayToolGrantHandler:          aiGatewayToolGrantHandler{service: services.ToolGrants},
		aiGatewayAccessPolicyHandler:       aiGatewayAccessPolicyHandler{service: services.AccessPolicies},
		aiGatewayGovernanceHandler:         aiGatewayGovernanceHandler{service: services.Governance},
		aiGatewayAuditHandler:              aiGatewayAuditHandler{service: services.Audit},
		aiGatewayApprovalHandler:           aiGatewayApprovalHandler{service: services.Approvals},
		aiGatewayUpstreamHandler:           aiGatewayUpstreamHandler{service: services.Upstreams},
		aiGatewayModelRouteHandler:         aiGatewayModelRouteHandler{service: services.ModelRoutes},
		aiGatewayRelayObservabilityHandler: aiGatewayRelayObservabilityHandler{service: services.RelayObservability},
		aiGatewayRelayHandler:              aiGatewayRelayHandler{service: services.Relay},
	}
}

func (h *aiGatewayCapabilityHandler) Capabilities(c *gin.Context) {
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

func (h *aiGatewayCapabilityHandler) InvokeTool(c *gin.Context) {
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

func (h *aiGatewayCapabilityHandler) ReadResource(c *gin.Context) {
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

func (h *aiGatewayCapabilityHandler) GetPrompt(c *gin.Context) {
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

func (h *aiGatewayPersonalTokenHandler) ListPersonalAccessTokens(c *gin.Context) {
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

func (h *aiGatewayPersonalTokenHandler) CreatePersonalAccessToken(c *gin.Context) {
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

func (h *aiGatewayPersonalTokenHandler) RevokePersonalAccessToken(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.RevokePersonalAccessToken(c.Request.Context(), principal, c.Param("tokenID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *aiGatewayPersonalTokenHandler) RotatePersonalAccessToken(c *gin.Context) {
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

func (h *aiGatewayServiceAccountHandler) ListServiceAccounts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListServiceAccounts(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *aiGatewayServiceAccountHandler) CreateServiceAccount(c *gin.Context) {
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

func (h *aiGatewayServiceAccountHandler) ListServiceAccountTokens(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListServiceAccountTokens(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *aiGatewayServiceAccountHandler) CreateServiceAccountToken(c *gin.Context) {
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

func (h *aiGatewayServiceAccountHandler) RevokeServiceAccountToken(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.RevokeServiceAccountToken(c.Request.Context(), principal, c.Param("tokenID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *aiGatewayServiceAccountHandler) RotateServiceAccountToken(c *gin.Context) {
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

func (h *aiGatewayClientHandler) ListAIClients(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAIClients(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *aiGatewayClientHandler) CreateAIClient(c *gin.Context) {
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

func (h *aiGatewayClientHandler) UpdateAIClient(c *gin.Context) {
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

func (h *aiGatewayToolGrantHandler) ListToolGrants(c *gin.Context) {
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

func (h *aiGatewayToolGrantHandler) CreateToolGrant(c *gin.Context) {
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

func (h *aiGatewayToolGrantHandler) DeleteToolGrant(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteToolGrant(c.Request.Context(), principal, c.Param("grantID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *aiGatewayAccessPolicyHandler) ListAccessPolicies(c *gin.Context) {
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

func (h *aiGatewayAccessPolicyHandler) CreateAccessPolicy(c *gin.Context) {
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

func (h *aiGatewayAccessPolicyHandler) UpdateAccessPolicy(c *gin.Context) {
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

func (h *aiGatewayAccessPolicyHandler) DeleteAccessPolicy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteAccessPolicy(c.Request.Context(), principal, c.Param("policyID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *aiGatewayGovernanceHandler) GovernanceStatus(c *gin.Context) {
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

func (h *aiGatewayGovernanceHandler) ListSkillBindings(c *gin.Context) {
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

func (h *aiGatewayGovernanceHandler) CreateSkillBinding(c *gin.Context) {
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

func (h *aiGatewayGovernanceHandler) UpdateSkillBinding(c *gin.Context) {
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

func (h *aiGatewayGovernanceHandler) DeleteSkillBinding(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteSkillBinding(c.Request.Context(), principal, c.Param("bindingID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *aiGatewayAuditHandler) ListAuditLogs(c *gin.Context) {
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

func (h *aiGatewayApprovalHandler) ListApprovalRequests(c *gin.Context) {
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

func (h *aiGatewayApprovalHandler) GetApprovalTimeline(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApprovalTimeline(c.Request.Context(), principal, c.Param("requestID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *aiGatewayApprovalHandler) ApproveApprovalRequest(c *gin.Context) {
	h.decideApprovalRequest(c, "approve")
}

func (h *aiGatewayApprovalHandler) RejectApprovalRequest(c *gin.Context) {
	h.decideApprovalRequest(c, "reject")
}

func (h *aiGatewayApprovalHandler) CancelApprovalRequest(c *gin.Context) {
	h.decideApprovalRequest(c, "cancel")
}

func (h *aiGatewayUpstreamHandler) ListLLMUpstreams(c *gin.Context) {
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

func (h *aiGatewayUpstreamHandler) CreateLLMUpstream(c *gin.Context) {
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

func (h *aiGatewayUpstreamHandler) UpdateLLMUpstream(c *gin.Context) {
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

func (h *aiGatewayUpstreamHandler) TestLLMUpstream(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.TestLLMUpstream(c.Request.Context(), principal, c.Param("upstreamID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *aiGatewayUpstreamHandler) RunLLMRelayHealthChecks(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RunLLMRelayHealthChecks(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *aiGatewayModelRouteHandler) ListLLMModelRoutes(c *gin.Context) {
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

func (h *aiGatewayModelRouteHandler) CreateLLMModelRoute(c *gin.Context) {
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

func (h *aiGatewayModelRouteHandler) UpdateLLMModelRoute(c *gin.Context) {
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

func (h *aiGatewayModelRouteHandler) DeleteLLMModelRoute(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteLLMModelRoute(c.Request.Context(), principal, c.Param("routeID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *aiGatewayRelayObservabilityHandler) ListLLMCallLogs(c *gin.Context) {
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

func (h *aiGatewayRelayObservabilityHandler) LLMRelayMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.LLMRelayMetrics(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *aiGatewayRelayObservabilityHandler) LLMRelayCacheStats(c *gin.Context) {
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

func (h *aiGatewayRelayObservabilityHandler) PurgeLLMRelayCache(c *gin.Context) {
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

func (h *aiGatewayRelayHandler) RelayOpenAIModels(c *gin.Context) {
	h.relayLLM(c, "openai", "models")
}

func (h *aiGatewayRelayHandler) RelayOpenAIChatCompletions(c *gin.Context) {
	h.relayLLM(c, "openai", "chat/completions")
}

func (h *aiGatewayRelayHandler) RelayOpenAIResponses(c *gin.Context) {
	h.relayLLM(c, "openai", "responses")
}

func (h *aiGatewayRelayHandler) RelayOpenAIEmbeddings(c *gin.Context) {
	h.relayLLM(c, "openai", "embeddings")
}

func (h *aiGatewayRelayHandler) RelayOpenAIImageGenerations(c *gin.Context) {
	h.relayLLM(c, "openai", "images/generations")
}

func (h *aiGatewayRelayHandler) RelayOpenAIImageEdits(c *gin.Context) {
	h.relayLLM(c, "openai", "images/edits")
}

func (h *aiGatewayRelayHandler) RelayOpenAIImageVariations(c *gin.Context) {
	h.relayLLM(c, "openai", "images/variations")
}

func (h *aiGatewayRelayHandler) RelayOpenAIAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "openai", "audio/speech")
}

func (h *aiGatewayRelayHandler) RelayOpenAIAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "openai", "audio/transcriptions")
}

func (h *aiGatewayRelayHandler) RelayOpenAIAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "openai", "audio/translations")
}

func (h *aiGatewayRelayHandler) RelayOpenAIRealtime(c *gin.Context) {
	h.relayLLMWebSocket(c, "openai", "realtime")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekModels(c *gin.Context) {
	h.relayLLM(c, "deepseek", "models")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekChatCompletions(c *gin.Context) {
	h.relayLLM(c, "deepseek", "chat/completions")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekResponses(c *gin.Context) {
	h.relayLLM(c, "deepseek", "responses")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekEmbeddings(c *gin.Context) {
	h.relayLLM(c, "deepseek", "embeddings")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekImageGenerations(c *gin.Context) {
	h.relayLLM(c, "deepseek", "images/generations")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekImageEdits(c *gin.Context) {
	h.relayLLM(c, "deepseek", "images/edits")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekImageVariations(c *gin.Context) {
	h.relayLLM(c, "deepseek", "images/variations")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "deepseek", "audio/speech")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "deepseek", "audio/transcriptions")
}

func (h *aiGatewayRelayHandler) RelayDeepSeekAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "deepseek", "audio/translations")
}

func (h *aiGatewayRelayHandler) RelayQwenModels(c *gin.Context) {
	h.relayLLM(c, "qwen", "models")
}

func (h *aiGatewayRelayHandler) RelayQwenChatCompletions(c *gin.Context) {
	h.relayLLM(c, "qwen", "chat/completions")
}

func (h *aiGatewayRelayHandler) RelayQwenResponses(c *gin.Context) {
	h.relayLLM(c, "qwen", "responses")
}

func (h *aiGatewayRelayHandler) RelayQwenEmbeddings(c *gin.Context) {
	h.relayLLM(c, "qwen", "embeddings")
}

func (h *aiGatewayRelayHandler) RelayQwenImageGenerations(c *gin.Context) {
	h.relayLLM(c, "qwen", "images/generations")
}

func (h *aiGatewayRelayHandler) RelayQwenImageEdits(c *gin.Context) {
	h.relayLLM(c, "qwen", "images/edits")
}

func (h *aiGatewayRelayHandler) RelayQwenImageVariations(c *gin.Context) {
	h.relayLLM(c, "qwen", "images/variations")
}

func (h *aiGatewayRelayHandler) RelayQwenAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "qwen", "audio/speech")
}

func (h *aiGatewayRelayHandler) RelayQwenAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "qwen", "audio/transcriptions")
}

func (h *aiGatewayRelayHandler) RelayQwenAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "qwen", "audio/translations")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterModels(c *gin.Context) {
	h.relayLLM(c, "openrouter", "models")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterChatCompletions(c *gin.Context) {
	h.relayLLM(c, "openrouter", "chat/completions")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterResponses(c *gin.Context) {
	h.relayLLM(c, "openrouter", "responses")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterEmbeddings(c *gin.Context) {
	h.relayLLM(c, "openrouter", "embeddings")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterImageGenerations(c *gin.Context) {
	h.relayLLM(c, "openrouter", "images/generations")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterImageEdits(c *gin.Context) {
	h.relayLLM(c, "openrouter", "images/edits")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterImageVariations(c *gin.Context) {
	h.relayLLM(c, "openrouter", "images/variations")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "openrouter", "audio/speech")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "openrouter", "audio/transcriptions")
}

func (h *aiGatewayRelayHandler) RelayOpenRouterAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "openrouter", "audio/translations")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIModels(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "models")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIChatCompletions(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "chat/completions")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIResponses(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "responses")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIEmbeddings(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "embeddings")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIImageGenerations(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "images/generations")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIImageEdits(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "images/edits")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIImageVariations(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "images/variations")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIAudioSpeech(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "audio/speech")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIAudioTranscriptions(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "audio/transcriptions")
}

func (h *aiGatewayRelayHandler) RelayAzureOpenAIAudioTranslations(c *gin.Context) {
	h.relayLLM(c, "azure-openai", "audio/translations")
}

func (h *aiGatewayRelayHandler) RelayGeminiModels(c *gin.Context) {
	h.relayLLM(c, "gemini", "models")
}

func (h *aiGatewayRelayHandler) RelayGeminiInteractions(c *gin.Context) {
	h.relayLLM(c, "gemini", "interactions")
}

func (h *aiGatewayRelayHandler) RelayGeminiModelAction(c *gin.Context) {
	pathModel, endpoint, ok := parseGeminiRelayModelAction(c.Param("modelAction"))
	if !ok {
		relayNativeError(c, http.StatusBadRequest, "invalid_argument", "unsupported Gemini relay model action")
		return
	}
	h.relayLLMWithPathModel(c, "gemini", endpoint, pathModel)
}

func (h *aiGatewayRelayHandler) RelayCohereRerank(c *gin.Context) {
	h.relayLLM(c, "cohere", "rerank")
}

func (h *aiGatewayRelayHandler) RelayAnthropicModels(c *gin.Context) {
	h.relayLLM(c, "anthropic", "models")
}

func (h *aiGatewayRelayHandler) RelayAnthropicMessages(c *gin.Context) {
	h.relayLLM(c, "anthropic", "messages")
}

func (h *aiGatewayRelayHandler) relayLLM(c *gin.Context, providerKind, endpoint string) {
	h.relayLLMWithPathModel(c, providerKind, endpoint, "")
}

func (h *aiGatewayRelayHandler) relayLLMWebSocket(c *gin.Context, providerKind, endpoint string) {
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

func (h *aiGatewayRelayHandler) relayLLMWithPathModel(c *gin.Context, providerKind, endpoint, pathModel string) {
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
	if err := clearResponseWriteDeadline(c); err != nil {
		_ = c.Error(err)
		relayNativeError(c, http.StatusInternalServerError, "stream_unavailable", "relay response streaming is unavailable")
		return
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

func (h *aiGatewayApprovalHandler) decideApprovalRequest(c *gin.Context, action string) {
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
