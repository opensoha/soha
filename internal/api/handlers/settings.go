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
)

type SettingsService interface {
	GetIdentitySettings(context.Context, domainidentity.Principal) (domainsettings.IdentitySettings, error)
	UpdateOIDCSettings(context.Context, domainidentity.Principal, domainsettings.OIDCSettings) (domainsettings.IdentitySettings, error)
	GetMonitoringSettings(context.Context, domainidentity.Principal) (domainsettings.MonitoringSettings, error)
	UpdatePrometheusSettings(context.Context, domainidentity.Principal, domainsettings.PrometheusSettings) (domainsettings.MonitoringSettings, error)
	GetAISettings(context.Context, domainidentity.Principal) (domainsettings.AISettings, error)
	UpdateAISettings(context.Context, domainidentity.Principal, domainsettings.AIProviderSettings) (domainsettings.AISettings, error)
}

type SettingsHandler struct {
	service SettingsService
}

func NewSettingsHandler(service SettingsService) *SettingsHandler {
	return &SettingsHandler{service: service}
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
