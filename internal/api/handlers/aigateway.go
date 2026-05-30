package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	domainidentity "github.com/soha/soha/internal/domain/identity"
)

type AIGatewayService interface {
	Capabilities(context.Context, domainidentity.Principal, domainaigateway.ManifestRequest) (domainaigateway.Manifest, error)
	InvokeTool(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error)
	ReadResource(context.Context, domainidentity.Principal, domainaigateway.ResourceReadRequest) (domainaigateway.ResourceReadResult, error)
	GetPrompt(context.Context, domainidentity.Principal, domainaigateway.PromptGetRequest) (domainaigateway.PromptGetResult, error)
	ListPersonalAccessTokens(context.Context, domainidentity.Principal) ([]domainaigateway.PersonalAccessToken, error)
	CreatePersonalAccessToken(context.Context, domainidentity.Principal, domainaigateway.PersonalAccessTokenInput) (domainaigateway.CreatedPersonalAccessToken, error)
	RevokePersonalAccessToken(context.Context, domainidentity.Principal, string) error
	ListServiceAccounts(context.Context, domainidentity.Principal) ([]domainaigateway.ServiceAccount, error)
	CreateServiceAccount(context.Context, domainidentity.Principal, domainaigateway.ServiceAccountInput) (domainaigateway.ServiceAccount, error)
	ListServiceAccountTokens(context.Context, domainidentity.Principal) ([]domainaigateway.ServiceAccountToken, error)
	CreateServiceAccountToken(context.Context, domainidentity.Principal, string, domainaigateway.ServiceAccountTokenInput) (domainaigateway.CreatedServiceAccountToken, error)
	RevokeServiceAccountToken(context.Context, domainidentity.Principal, string) error
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
	items, err := h.service.ListPersonalAccessTokens(c.Request.Context(), principal)
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
	windowHours := 0
	if raw := c.Query("windowHours"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "windowHours must be an integer")
			return
		}
		if parsed != 0 && (parsed < 1 || parsed > maxAIGatewayGovernanceWindowHours) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "windowHours must be 0 or between 1 and 168")
			return
		}
		windowHours = parsed
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

func firstHeaderValue(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := c.GetHeader(name); value != "" {
			return value
		}
	}
	return ""
}
