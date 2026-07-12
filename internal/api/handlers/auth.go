package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

type IdentityAuthService interface {
	ListProviders(context.Context) []domainidentity.Provider
	LoginWithPassword(context.Context, string, string) (domainidentity.AuthResult, error)
	RefreshSession(context.Context, string) (domainidentity.AuthResult, error)
	Logout(context.Context, string, string) error
	CurrentPrincipal(context.Context, string) (domainidentity.Principal, error)
}

type IdentityProfileService interface {
	CurrentProfile(context.Context, domainidentity.Principal) (domainidentity.UserProfile, error)
	UpdateCurrentProfile(context.Context, domainidentity.Principal, domainidentity.ProfileUpdate) (domainidentity.UserProfile, error)
	ChangeCurrentPassword(context.Context, domainidentity.Principal, domainidentity.PasswordChange) error
}

type IdentityFederationService interface {
	BeginOIDCLogin(context.Context, string) (string, error)
	BeginProviderLogin(context.Context, string, string) (string, error)
	BeginProviderLink(context.Context, domainidentity.Principal, string, string) (string, error)
	HandleOIDCCallback(context.Context, string, string) (string, error)
	HandleProviderCallback(context.Context, string, string, string) (string, error)
	ConsumeOIDCExchange(context.Context, string) (domainidentity.AuthResult, error)
}

type IdentitySessionService interface {
	ListActiveSessions(context.Context, domainidentity.Principal, int) ([]domainidentity.SessionRecord, error)
	RevokeSessionByID(context.Context, domainidentity.Principal, string) error
}

type IdentityStreamTicketService interface {
	IssueStreamTicket(context.Context, domainidentity.Principal, domainidentity.AccessContext, domainidentity.StreamTicketRequest) (domainidentity.StreamTicket, error)
}

type IdentityService interface {
	IdentityAuthService
	IdentityProfileService
	IdentityFederationService
	IdentitySessionService
	IdentityStreamTicketService
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

const refreshCookieName = "soha_refresh_token"

type AuthHandler struct {
	auth                IdentityAuthService
	profile             IdentityProfileService
	federation          IdentityFederationService
	sessions            IdentitySessionService
	streamTickets       IdentityStreamTicketService
	access              AuthBootstrapAccessService
	settings            AuthBootstrapSettingsService
	loginOptions        dto.LoginOptionsResponse
	refreshCookieMaxAge int
}

func NewAuthHandler(identity IdentityService, access AuthBootstrapAccessService, settings AuthBootstrapSettingsService, authCfg cfgpkg.AuthConfig) *AuthHandler {
	return NewAuthHandlerWithServices(identity, identity, identity, identity, identity, access, settings, authCfg)
}

func NewAuthHandlerWithServices(auth IdentityAuthService, profile IdentityProfileService, federation IdentityFederationService, sessions IdentitySessionService, streamTickets IdentityStreamTicketService, access AuthBootstrapAccessService, settings AuthBootstrapSettingsService, authCfg cfgpkg.AuthConfig) *AuthHandler {
	refreshCookieMaxAge := int(authCfg.JWT.RefreshTTL / time.Second)
	if refreshCookieMaxAge <= 0 {
		refreshCookieMaxAge = int((7 * 24 * time.Hour) / time.Second)
	}
	return &AuthHandler{
		auth:                auth,
		profile:             profile,
		federation:          federation,
		sessions:            sessions,
		streamTickets:       streamTickets,
		access:              access,
		settings:            settings,
		refreshCookieMaxAge: refreshCookieMaxAge,
		loginOptions: dto.LoginOptionsResponse{
			Verification: dto.LoginVerificationOptions{
				SliderEnabled: authCfg.LoginVerification.SliderEnabled,
			},
		},
	}
}

func (h *AuthHandler) setRefreshCookie(c *gin.Context, result domainidentity.AuthResult) {
	refreshToken := strings.TrimSpace(result.Tokens.RefreshToken)
	if refreshToken == "" {
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, refreshToken, h.refreshCookieMaxAge, "/api/v1/auth", "", c.Request.TLS != nil, true)
}

func (h *AuthHandler) setProtocolAccessCookie(c *gin.Context, result domainidentity.AuthResult) {
	accessToken := strings.TrimSpace(result.Tokens.AccessToken)
	if accessToken == "" {
		return
	}
	maxAge := int(result.Tokens.ExpiresIn)
	if maxAge <= 0 && !result.Tokens.ExpiresAt.IsZero() {
		maxAge = int(time.Until(result.Tokens.ExpiresAt) / time.Second)
	}
	if maxAge <= 0 {
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(apiMiddleware.ProtocolAccessCookieName, accessToken, maxAge, "/", "", authRequestIsHTTPS(c), true)
}

func (h *AuthHandler) setAuthCookies(c *gin.Context, result domainidentity.AuthResult) {
	h.setRefreshCookie(c, result)
	h.setProtocolAccessCookie(c, result)
}

func (h *AuthHandler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, "", -1, "/api/v1/auth", "", c.Request.TLS != nil, true)
}

func (h *AuthHandler) clearProtocolAccessCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(apiMiddleware.ProtocolAccessCookieName, "", -1, "/", "", authRequestIsHTTPS(c), true)
}

func (h *AuthHandler) clearAuthCookies(c *gin.Context) {
	h.clearRefreshCookie(c)
	h.clearProtocolAccessCookie(c)
}

func authRequestIsHTTPS(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}

func refreshTokenFromRequest(c *gin.Context, bodyValue string) string {
	refreshToken := strings.TrimSpace(bodyValue)
	if refreshToken != "" {
		return refreshToken
	}
	if cookieValue, err := c.Cookie(refreshCookieName); err == nil {
		return strings.TrimSpace(cookieValue)
	}
	return ""
}

func (h *AuthHandler) ListProviders(c *gin.Context) {
	apiresponse.Items(c, http.StatusOK, h.auth.ListProviders(c.Request.Context()))
}

func (h *AuthHandler) LoginOptions(c *gin.Context) {
	options := h.loginOptions
	options.LocalPasswordLoginEnabled = slices.ContainsFunc(h.auth.ListProviders(c.Request.Context()), func(provider domainidentity.Provider) bool {
		return provider.Type == "password" && provider.Enabled
	})
	apiresponse.Item(c, http.StatusOK, options)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.PasswordLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid login payload")
		return
	}
	result, err := h.auth.LoginWithPassword(c.Request.Context(), req.Login, req.Password)
	if err != nil {
		writeError(c, err)
		return
	}
	h.setAuthCookies(c, result)
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
	result, err := h.auth.LoginWithPassword(c.Request.Context(), login, req.Password)
	if err != nil {
		writeError(c, err)
		return
	}
	h.setAuthCookies(c, result)
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
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid refresh payload")
		return
	}
	refreshToken := refreshTokenFromRequest(c, req.RefreshToken)
	if refreshToken == "" {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "refresh token is required")
		return
	}
	result, err := h.auth.RefreshSession(c.Request.Context(), refreshToken)
	if err != nil {
		writeError(c, err)
		return
	}
	h.setAuthCookies(c, result)
	apiresponse.Item(c, http.StatusOK, result)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	var req dto.LogoutRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.auth.Logout(c.Request.Context(), apiMiddleware.BearerTokenFromContext(c), refreshTokenFromRequest(c, req.RefreshToken)); err != nil {
		writeError(c, err)
		return
	}
	h.clearAuthCookies(c)
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AuthHandler) ProLogout(c *gin.Context) {
	var req dto.LogoutRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.auth.Logout(c.Request.Context(), apiMiddleware.BearerTokenFromContext(c), refreshTokenFromRequest(c, req.RefreshToken)); err != nil {
		writeError(c, err)
		return
	}
	h.clearAuthCookies(c)
	apiresponse.JSON(c, http.StatusOK, gin.H{"success": true})
}

func (h *AuthHandler) Me(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	current, err := h.auth.CurrentPrincipal(c.Request.Context(), principal.UserID)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, current)
}

func (h *AuthHandler) Profile(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	profile, err := h.profile.CurrentProfile(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, profile)
}

func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	var req dto.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid profile payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	profile, err := h.profile.UpdateCurrentProfile(c.Request.Context(), principal, domainidentity.ProfileUpdate{
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Phone:       req.Phone,
		AvatarURL:   req.AvatarURL,
		AvatarFit:   req.AvatarFit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, profile)
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req dto.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid password payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.profile.ChangeCurrentPassword(c.Request.Context(), principal, domainidentity.PasswordChange{
		CurrentPassword: req.CurrentPassword,
		NewPassword:     req.NewPassword,
	}); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AuthHandler) ProCurrentUser(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	current, err := h.auth.CurrentPrincipal(c.Request.Context(), principal.UserID)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, toProCurrentUser(current))
}

func (h *AuthHandler) Bootstrap(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	current, err := h.auth.CurrentPrincipal(c.Request.Context(), principal.UserID)
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

func (h *AuthHandler) IssueStreamTicket(c *gin.Context) {
	var req domainidentity.StreamTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid stream ticket payload")
		return
	}
	item, err := h.streamTickets.IssueStreamTicket(
		c.Request.Context(),
		apiMiddleware.PrincipalFromContext(c),
		apiMiddleware.AccessContextFromContext(c),
		req,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AuthHandler) OIDCLogin(c *gin.Context) {
	loginURL, err := h.federation.BeginOIDCLogin(c.Request.Context(), c.Query("return_to"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, loginURL)
}

func (h *AuthHandler) OIDCCallback(c *gin.Context) {
	redirectURL, err := h.federation.HandleOIDCCallback(c.Request.Context(), c.Query("state"), c.Query("code"))
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
	result, err := h.federation.ConsumeOIDCExchange(c.Request.Context(), req.Code)
	if err != nil {
		writeError(c, err)
		return
	}
	h.setAuthCookies(c, result)
	apiresponse.Item(c, http.StatusOK, result)
}

func (h *AuthHandler) ProviderLogin(c *gin.Context) {
	loginURL, err := h.federation.BeginProviderLogin(c.Request.Context(), c.Param("providerID"), c.Query("return_to"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, loginURL)
}

func (h *AuthHandler) ProviderLink(c *gin.Context) {
	loginURL, err := h.federation.BeginProviderLink(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("providerID"), c.Query("return_to"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, gin.H{"url": loginURL})
}

func (h *AuthHandler) ProviderCallback(c *gin.Context) {
	redirectURL, err := h.federation.HandleProviderCallback(c.Request.Context(), c.Param("providerID"), c.Query("state"), c.Query("code"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

func (h *AuthHandler) ListSessions(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	limit := 100
	items, err := h.sessions.ListActiveSessions(c.Request.Context(), principal, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AuthHandler) RevokeSession(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.sessions.RevokeSessionByID(c.Request.Context(), principal, c.Param("sessionID")); err != nil {
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
		Avatar:    principal.AvatarURL,
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
