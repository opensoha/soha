package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
)

type IdentityService interface {
	ListProviders(context.Context) []domainidentity.Provider
	LoginWithPassword(context.Context, string, string) (domainidentity.AuthResult, error)
	RefreshSession(context.Context, string) (domainidentity.AuthResult, error)
	Logout(context.Context, string, string) error
	CurrentPrincipal(context.Context, string) (domainidentity.Principal, error)
	BeginOIDCLogin(context.Context) (string, error)
	HandleOIDCCallback(context.Context, string, string) (string, error)
	ConsumeOIDCExchange(context.Context, string) (domainidentity.AuthResult, error)
	ListActiveSessions(context.Context, domainidentity.Principal, int) ([]domainidentity.SessionRecord, error)
	RevokeSessionByID(context.Context, domainidentity.Principal, string) error
}

type AuthHandler struct {
	identity IdentityService
}

func NewAuthHandler(identity IdentityService) *AuthHandler {
	return &AuthHandler{identity: identity}
}

func (h *AuthHandler) ListProviders(c *gin.Context) {
	apiresponse.Items(c, http.StatusOK, h.identity.ListProviders(c.Request.Context()))
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.PasswordLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid login payload")
		return
	}
	result, err := h.identity.LoginWithPassword(c.Request.Context(), req.Login, req.Password)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req dto.RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid refresh payload")
		return
	}
	result, err := h.identity.RefreshSession(c.Request.Context(), req.RefreshToken)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	var req dto.LogoutRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.identity.Logout(c.Request.Context(), apiMiddleware.BearerTokenFromContext(c), req.RefreshToken); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AuthHandler) Me(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	current, err := h.identity.CurrentPrincipal(c.Request.Context(), principal.UserID)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, current)
}

func (h *AuthHandler) OIDCLogin(c *gin.Context) {
	loginURL, err := h.identity.BeginOIDCLogin(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, loginURL)
}

func (h *AuthHandler) OIDCCallback(c *gin.Context) {
	redirectURL, err := h.identity.HandleOIDCCallback(c.Request.Context(), c.Query("state"), c.Query("code"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

func (h *AuthHandler) OIDCExchange(c *gin.Context) {
	var req dto.OIDCExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oidc exchange payload")
		return
	}
	result, err := h.identity.ConsumeOIDCExchange(c.Request.Context(), req.Code)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}

func (h *AuthHandler) ListSessions(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	limit := 100
	items, err := h.identity.ListActiveSessions(c.Request.Context(), principal, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AuthHandler) RevokeSession(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.identity.RevokeSessionByID(c.Request.Context(), principal, c.Param("sessionID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
