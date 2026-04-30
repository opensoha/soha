package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainsettings "github.com/kubecrux/kubecrux/internal/domain/settings"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
)

type SettingsService interface {
	GetIdentitySettings(context.Context, domainidentity.Principal) (domainsettings.IdentitySettings, error)
	UpdateOIDCSettings(context.Context, domainidentity.Principal, domainsettings.OIDCSettings) (domainsettings.IdentitySettings, error)
	GetMonitoringSettings(context.Context, domainidentity.Principal) (domainsettings.MonitoringSettings, error)
	UpdatePrometheusSettings(context.Context, domainidentity.Principal, domainsettings.PrometheusSettings) (domainsettings.MonitoringSettings, error)
	GetAISettings(context.Context, domainidentity.Principal) (domainsettings.AISettings, error)
	UpdateAISettings(context.Context, domainidentity.Principal, domainsettings.AIProviderSettings) (domainsettings.AISettings, error)
	GetBrandingSettings(context.Context, domainidentity.Principal) (domainsettings.BrandingSettings, error)
	UpdateBrandingSettings(context.Context, domainidentity.Principal, domainsettings.BrandingSettings) (domainsettings.BrandingSettings, error)
}

type SettingsHandler struct {
	service     SettingsService
	permissions *appaccess.PermissionResolver
}

func NewSettingsHandler(service SettingsService, permissions *appaccess.PermissionResolver) *SettingsHandler {
	return &SettingsHandler{service: service, permissions: permissions}
}

func (h *SettingsHandler) GetIdentitySettings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetIdentitySettings(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) UpdateOIDCSettings(c *gin.Context) {
	var req dto.UpdateOIDCSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid oidc settings payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateOIDCSettings(c.Request.Context(), principal, domainsettings.OIDCSettings{
		Enabled:             req.Enabled,
		ProviderName:        req.ProviderName,
		Issuer:              req.Issuer,
		ClientID:            req.ClientID,
		ClientSecret:        req.ClientSecret,
		RedirectURL:         req.RedirectURL,
		FrontendRedirectURL: req.FrontendRedirectURL,
		Scopes:              req.Scopes,
		DefaultRoles:        req.DefaultRoles,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) GetMonitoringSettings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetMonitoringSettings(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) UpdatePrometheusSettings(c *gin.Context) {
	var req dto.UpdatePrometheusSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid prometheus settings payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdatePrometheusSettings(c.Request.Context(), principal, domainsettings.PrometheusSettings{
		Enabled:             req.Enabled,
		BaseURL:             req.BaseURL,
		BearerToken:         req.BearerToken,
		DefaultRangeMinutes: req.DefaultRangeMinutes,
		StepSeconds:         req.StepSeconds,
		ClusterLabel:        req.ClusterLabel,
		GrafanaBaseURL:      req.GrafanaBaseURL,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) GetAISettings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetAISettings(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) UpdateAISettings(c *gin.Context) {
	var req dto.UpdateAISettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid ai settings payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateAISettings(c.Request.Context(), principal, domainsettings.AIProviderSettings{
		Enabled: req.Enabled,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Model:   req.Model,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) GetBrandingSettings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetBrandingSettings(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) UpdateBrandingSettings(c *gin.Context) {
	var req dto.UpdateBrandingSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid branding settings payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateBrandingSettings(c.Request.Context(), principal, domainsettings.BrandingSettings{
		AppTitle:         req.AppTitle,
		SidebarTitle:     req.SidebarTitle,
		LoginLogoURL:     req.LoginLogoURL,
		ExpandedLogoURL:  req.ExpandedLogoURL,
		CollapsedLogoURL: req.CollapsedLogoURL,
		FaviconURL:       req.FaviconURL,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
