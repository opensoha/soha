package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/soha/soha/internal/api/dto"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
	domainaccess "github.com/soha/soha/internal/domain/access"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainsettings "github.com/soha/soha/internal/domain/settings"
	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
	"github.com/soha/soha/internal/platform/apperrors"
)

const loginVerificationTokenTTL = 2 * time.Minute

type IdentityService interface {
	ListProviders(context.Context) []domainidentity.Provider
	LoginWithPassword(context.Context, string, string) (domainidentity.AuthResult, error)
	RefreshSession(context.Context, string) (domainidentity.AuthResult, error)
	Logout(context.Context, string, string) error
	CurrentPrincipal(context.Context, string) (domainidentity.Principal, error)
	BeginOIDCLogin(context.Context) (string, error)
	BeginProviderLogin(context.Context, string) (string, error)
	HandleOIDCCallback(context.Context, string, string) (string, error)
	HandleProviderCallback(context.Context, string, string, string) (string, error)
	ConsumeOIDCExchange(context.Context, string) (domainidentity.AuthResult, error)
	ListActiveSessions(context.Context, domainidentity.Principal, int) ([]domainidentity.SessionRecord, error)
	RevokeSessionByID(context.Context, domainidentity.Principal, string) error
}

type AuthBootstrapAccessService interface {
	PermissionSnapshot(context.Context, domainidentity.Principal) (domainaccess.PermissionSnapshot, error)
}

type AuthBootstrapSettingsService interface {
	GetBrandingSettings(context.Context, domainidentity.Principal) (domainsettings.BrandingSettings, error)
}

type authBootstrapResponse struct {
	User               domainidentity.Principal        `json:"user"`
	CurrentUser        domainidentity.Principal        `json:"currentUser"`
	PermissionSnapshot domainaccess.PermissionSnapshot `json:"permissionSnapshot"`
	Branding           domainsettings.BrandingSettings `json:"branding"`
}

type proCurrentUser struct {
	Name        string             `json:"name"`
	Avatar      string             `json:"avatar"`
	UserID      string             `json:"userid"`
	Email       string             `json:"email"`
	Signature   string             `json:"signature"`
	Title       string             `json:"title"`
	Group       string             `json:"group"`
	Tags        []proCurrentTag    `json:"tags"`
	NotifyCount int                `json:"notifyCount"`
	UnreadCount int                `json:"unreadCount"`
	Country     string             `json:"country"`
	Geographic  proCurrentGeo      `json:"geographic"`
	Address     string             `json:"address"`
	Phone       string             `json:"phone"`
	Access      string             `json:"access,omitempty"`
	Notice      []proCurrentNotice `json:"notice"`
}

type proCurrentTag struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type proCurrentGeo struct {
	Province proCurrentGeoItem `json:"province"`
	City     proCurrentGeoItem `json:"city"`
}

type proCurrentGeoItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type proCurrentNotice struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Logo        string `json:"logo"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updatedAt"`
	Member      string `json:"member"`
	Href        string `json:"href"`
	MemberLink  string `json:"memberLink"`
}

type proLoginResponse struct {
	Status           string `json:"status"`
	Type             string `json:"type"`
	CurrentAuthority string `json:"currentAuthority"`
}

type loginVerificationChallenge struct {
	ExpiresAt time.Time
	ClientIP  string
	UserAgent string
}

type AuthHandler struct {
	identity                      IdentityService
	access                        AuthBootstrapAccessService
	settings                      AuthBootstrapSettingsService
	loginOptions                  dto.LoginOptionsResponse
	loginVerificationMu           sync.Mutex
	loginVerificationChallenges   map[string]loginVerificationChallenge
	loginVerificationTokenExpires time.Duration
}

func NewAuthHandler(identity IdentityService, access AuthBootstrapAccessService, settings AuthBootstrapSettingsService, authCfg cfgpkg.AuthConfig) *AuthHandler {
	return &AuthHandler{
		identity: identity,
		access:   access,
		settings: settings,
		loginOptions: dto.LoginOptionsResponse{
			Verification: dto.LoginVerificationOptions{
				SliderEnabled: authCfg.LoginVerification.SliderEnabled,
			},
		},
		loginVerificationChallenges:   map[string]loginVerificationChallenge{},
		loginVerificationTokenExpires: loginVerificationTokenTTL,
	}
}

func (h *AuthHandler) ListProviders(c *gin.Context) {
	apiresponse.Items(c, http.StatusOK, h.identity.ListProviders(c.Request.Context()))
}

func (h *AuthHandler) LoginOptions(c *gin.Context) {
	apiresponse.Item(c, http.StatusOK, h.loginOptions)
}

func (h *AuthHandler) IssueLoginVerification(c *gin.Context) {
	if !h.loginOptions.Verification.SliderEnabled {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "login verification is not enabled")
		return
	}

	var req dto.LoginVerificationChallengeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid login verification payload")
		return
	}
	if strings.TrimSpace(req.Type) != "slider" || req.SliderValue < 98 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "slider verification is incomplete")
		return
	}

	token, err := randomURLToken(32)
	if err != nil {
		writeError(c, err)
		return
	}

	expiresAt := time.Now().Add(h.loginVerificationTokenExpires)
	h.loginVerificationMu.Lock()
	h.pruneLoginVerificationChallengesLocked(time.Now())
	h.loginVerificationChallenges[token] = loginVerificationChallenge{
		ExpiresAt: expiresAt,
		ClientIP:  c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
	}
	h.loginVerificationMu.Unlock()

	apiresponse.Item(c, http.StatusOK, dto.LoginVerificationChallengeResponse{
		Token:     token,
		ExpiresIn: int64(h.loginVerificationTokenExpires.Seconds()),
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.PasswordLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid login payload")
		return
	}
	if err := h.consumeLoginVerification(c, req.VerificationToken); err != nil {
		writeError(c, err)
		return
	}
	result, err := h.identity.LoginWithPassword(c.Request.Context(), req.Login, req.Password)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}

func (h *AuthHandler) ProLogin(c *gin.Context) {
	var req dto.ProPasswordLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid login payload")
		return
	}
	login := strings.TrimSpace(req.Username)
	if login == "" {
		login = strings.TrimSpace(req.Login)
	}
	if err := h.consumeLoginVerification(c, req.VerificationToken); err != nil {
		writeError(c, err)
		return
	}
	result, err := h.identity.LoginWithPassword(c.Request.Context(), login, req.Password)
	if err != nil {
		writeError(c, err)
		return
	}
	authority := "user"
	for _, role := range result.User.Roles {
		if role == "admin" {
			authority = "admin"
			break
		}
	}
	apiresponse.JSON(c, http.StatusOK, proLoginResponse{
		Status:           "ok",
		Type:             strings.TrimSpace(req.Type),
		CurrentAuthority: authority,
	})
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

func (h *AuthHandler) ProLogout(c *gin.Context) {
	var req dto.LogoutRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.identity.Logout(c.Request.Context(), apiMiddleware.BearerTokenFromContext(c), req.RefreshToken); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"success": true})
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

func (h *AuthHandler) ProCurrentUser(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	current, err := h.identity.CurrentPrincipal(c.Request.Context(), principal.UserID)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, toProCurrentUser(current))
}

func (h *AuthHandler) Bootstrap(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	current, err := h.identity.CurrentPrincipal(c.Request.Context(), principal.UserID)
	if err != nil {
		writeError(c, err)
		return
	}

	snapshot := domainaccess.PermissionSnapshot{
		PermissionKeys: []string{},
		VisibleMenuIDs: []string{},
		VisibleMenus:   []domainaccess.VisibleMenu{},
	}
	if h.access != nil {
		snapshot, err = h.access.PermissionSnapshot(c.Request.Context(), current)
		if err != nil {
			writeError(c, err)
			return
		}
	}

	branding := domainsettings.BrandingSettings{}
	if h.settings != nil {
		branding, err = h.settings.GetBrandingSettings(c.Request.Context(), current)
		if err != nil {
			writeError(c, err)
			return
		}
	}

	apiresponse.Item(c, http.StatusOK, authBootstrapResponse{
		User:               current,
		CurrentUser:        current,
		PermissionSnapshot: snapshot,
		Branding:           branding,
	})
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

func (h *AuthHandler) ProviderLogin(c *gin.Context) {
	loginURL, err := h.identity.BeginProviderLogin(c.Request.Context(), c.Param("providerID"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, loginURL)
}

func (h *AuthHandler) ProviderCallback(c *gin.Context) {
	redirectURL, err := h.identity.HandleProviderCallback(c.Request.Context(), c.Param("providerID"), c.Query("state"), c.Query("code"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
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

func toProCurrentUser(principal domainidentity.Principal) proCurrentUser {
	name := principal.UserName
	if name == "" {
		name = principal.Email
	}
	tags := make([]proCurrentTag, 0, len(principal.Tags))
	for _, tag := range principal.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		tags = append(tags, proCurrentTag{
			Key:   tag,
			Label: tag,
		})
	}
	group := ""
	if len(principal.Teams) > 0 {
		group = principal.Teams[0]
	}
	access := "user"
	for _, role := range principal.Roles {
		if role == "admin" {
			access = "admin"
			break
		}
	}
	return proCurrentUser{
		Name:      name,
		UserID:    principal.UserID,
		Email:     principal.Email,
		Signature: "Soha operator",
		Title:     firstNonEmpty(principal.Roles...),
		Group:     group,
		Tags:      tags,
		Country:   "CN",
		Geographic: proCurrentGeo{
			Province: proCurrentGeoItem{Key: "shanghai", Label: "Shanghai"},
			City:     proCurrentGeoItem{Key: "shanghai", Label: "Shanghai"},
		},
		Address: "Soha Console",
		Phone:   "000-00000000",
		Access:  access,
		Notice:  []proCurrentNotice{},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func randomURLToken(byteCount int) (string, error) {
	buffer := make([]byte, byteCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate login verification token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (h *AuthHandler) consumeLoginVerification(c *gin.Context, token string) error {
	if !h.loginOptions.Verification.SliderEnabled {
		return nil
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("%w: login verification token is required", apperrors.ErrUnauthorized)
	}

	now := time.Now()
	h.loginVerificationMu.Lock()
	defer h.loginVerificationMu.Unlock()

	h.pruneLoginVerificationChallengesLocked(now)
	challenge, ok := h.loginVerificationChallenges[token]
	if !ok {
		return fmt.Errorf("%w: login verification token is invalid or expired", apperrors.ErrUnauthorized)
	}
	delete(h.loginVerificationChallenges, token)

	if challenge.ExpiresAt.Before(now) {
		return fmt.Errorf("%w: login verification token is invalid or expired", apperrors.ErrUnauthorized)
	}
	if challenge.ClientIP != c.ClientIP() || challenge.UserAgent != c.GetHeader("User-Agent") {
		return fmt.Errorf("%w: login verification token does not match this client", apperrors.ErrUnauthorized)
	}
	return nil
}

func (h *AuthHandler) pruneLoginVerificationChallengesLocked(now time.Time) {
	for token, challenge := range h.loginVerificationChallenges {
		if challenge.ExpiresAt.Before(now) {
			delete(h.loginVerificationChallenges, token)
		}
	}
}
