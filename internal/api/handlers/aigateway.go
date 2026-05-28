package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	domainidentity "github.com/soha/soha/internal/domain/identity"
)

type AIGatewayService interface {
	Capabilities(context.Context, domainidentity.Principal, domainaigateway.ManifestRequest) (domainaigateway.Manifest, error)
	InvokeTool(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error)
	ListPersonalAccessTokens(context.Context, domainidentity.Principal) ([]domainaigateway.PersonalAccessToken, error)
	CreatePersonalAccessToken(context.Context, domainidentity.Principal, domainaigateway.PersonalAccessTokenInput) (domainaigateway.CreatedPersonalAccessToken, error)
	RevokePersonalAccessToken(context.Context, domainidentity.Principal, string) error
	ListServiceAccounts(context.Context, domainidentity.Principal) ([]domainaigateway.ServiceAccount, error)
	CreateServiceAccount(context.Context, domainidentity.Principal, domainaigateway.ServiceAccountInput) (domainaigateway.ServiceAccount, error)
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
	ListSkillBindings(context.Context, domainidentity.Principal, domainaigateway.SkillBindingFilter) ([]domainaigateway.SkillBinding, error)
	CreateSkillBinding(context.Context, domainidentity.Principal, domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error)
	UpdateSkillBinding(context.Context, domainidentity.Principal, string, domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error)
	DeleteSkillBinding(context.Context, domainidentity.Principal, string) error
}

type AIGatewayHandler struct {
	service AIGatewayService
}

func NewAIGatewayHandler(service AIGatewayService) *AIGatewayHandler {
	return &AIGatewayHandler{service: service}
}

func (h *AIGatewayHandler) Capabilities(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	accessCtx := apiMiddleware.AccessContextFromContext(c)
	item, err := h.service.Capabilities(c.Request.Context(), principal, domainaigateway.ManifestRequest{
		AIClientID:   firstHeaderValue(c, "X-Soha-AI-Client-ID", "X-AI-Client-ID"),
		AIClientName: firstHeaderValue(c, "X-Soha-AI-Client", "X-AI-Client"),
		SkillID:      firstHeaderValue(c, "X-Soha-Skill-ID", "X-Skill-ID"),
		TokenID:      accessCtx.TokenID,
		TokenKind:    accessCtx.TokenKind,
		SessionID:    accessCtx.SessionID,
		SubjectType:  accessCtx.SubjectType,
		SubjectID:    accessCtx.SubjectID,
		Source:       firstHeaderValue(c, "X-Soha-Source", "X-Source"),
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

func firstHeaderValue(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := c.GetHeader(name); value != "" {
			return value
		}
	}
	return ""
}
