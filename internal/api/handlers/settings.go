package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainsettings "github.com/kubecrux/kubecrux/internal/domain/settings"
)

type SettingsService interface {
	GetIdentitySettings(context.Context, domainidentity.Principal) (domainsettings.IdentitySettings, error)
	UpdateOIDCSettings(context.Context, domainidentity.Principal, domainsettings.OIDCSettings) (domainsettings.IdentitySettings, error)
	UpdateLoginProvidersSettings(context.Context, domainidentity.Principal, []domainsettings.LoginProviderSettings, string) (domainsettings.IdentitySettings, error)
	GetMonitoringSettings(context.Context, domainidentity.Principal) (domainsettings.MonitoringSettings, error)
	UpdatePrometheusSettings(context.Context, domainidentity.Principal, domainsettings.PrometheusSettings) (domainsettings.MonitoringSettings, error)
	GetAISettings(context.Context, domainidentity.Principal) (domainsettings.AISettings, error)
	UpdateAISettings(context.Context, domainidentity.Principal, domainsettings.AISettings) (domainsettings.AISettings, error)
	UpdateAIProviderConnections(context.Context, domainidentity.Principal, []domainsettings.AIProviderSettings, string) (domainsettings.AISettings, error)
	ListAIProviderModels(context.Context, domainidentity.Principal, domainsettings.AIProviderSettings) ([]string, error)
	TestAIProviderConnectivity(context.Context, domainidentity.Principal, domainsettings.AIProviderSettings, string) (domainsettings.AIProviderTestResult, error)
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

func (h *SettingsHandler) UpdateLoginProvidersSettings(c *gin.Context) {
	var req dto.UpdateLoginProvidersSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid login providers payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateLoginProvidersSettings(c.Request.Context(), principal, mapLoginProviders(req.Providers), req.DefaultProviderID)
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
	item, err := h.service.UpdateAISettings(c.Request.Context(), principal, domainsettings.AISettings{
		Provider: domainsettings.AIProviderSettings{
			Enabled: req.Enabled,
			BaseURL: req.BaseURL,
			APIKey:  req.APIKey,
			Model:   req.Model,
		},
		DefaultProviderID: req.DefaultProviderID,
		Providers:         mapAIProviders(req.Providers),
		SkillsRegistry:    mapAISkills(req.SkillsRegistry),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) UpdateAIProviderConnections(c *gin.Context) {
	var req dto.UpdateAIProviderConnectionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid ai provider connections payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateAIProviderConnections(c.Request.Context(), principal, mapAIProviders(req.Providers), req.DefaultProviderID)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) ListAIProviderModels(c *gin.Context) {
	var req dto.AIProviderModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid ai provider payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAIProviderModels(c.Request.Context(), principal, domainsettings.AIProviderSettings{
		ID:           req.Provider.ID,
		Name:         req.Provider.Name,
		ProviderKind: req.Provider.ProviderKind,
		Enabled:      req.Provider.Enabled,
		BaseURL:      req.Provider.BaseURL,
		APIKey:       req.Provider.APIKey,
		Model:        req.Provider.Model,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, map[string]any{"models": items})
}

func (h *SettingsHandler) TestAIProviderConnectivity(c *gin.Context) {
	var req dto.AIProviderTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid ai provider test payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.TestAIProviderConnectivity(c.Request.Context(), principal, domainsettings.AIProviderSettings{
		ID:           req.Provider.ID,
		Name:         req.Provider.Name,
		ProviderKind: req.Provider.ProviderKind,
		Enabled:      req.Provider.Enabled,
		BaseURL:      req.Provider.BaseURL,
		APIKey:       req.Provider.APIKey,
		Model:        req.Provider.Model,
	}, req.Prompt)
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

func mapAISkills(items []dto.AISkillSettings) []domainsettings.AISkillSettings {
	out := make([]domainsettings.AISkillSettings, 0, len(items))
	for _, item := range items {
		out = append(out, domainsettings.AISkillSettings{
			ID:             item.ID,
			Name:           item.Name,
			Category:       item.Category,
			OwnerModule:    item.OwnerModule,
			Description:    item.Description,
			CapabilityRefs: item.CapabilityRefs,
			BlueprintRefs:  item.BlueprintRefs,
			InputSchema:    item.InputSchema,
			OutputSchema:   item.OutputSchema,
			ScopeRules:     item.ScopeRules,
			Enabled:        item.Enabled,
			Scopes:         item.Scopes,
		})
	}
	return out
}

func mapAIProviders(items []dto.AIProviderSettings) []domainsettings.AIProviderSettings {
	out := make([]domainsettings.AIProviderSettings, 0, len(items))
	for _, item := range items {
		out = append(out, domainsettings.AIProviderSettings{
			ID:           item.ID,
			Name:         item.Name,
			ProviderKind: item.ProviderKind,
			Enabled:      item.Enabled,
			BaseURL:      item.BaseURL,
			APIKey:       item.APIKey,
			Model:        item.Model,
		})
	}
	return out
}

func mapLoginProviders(items []dto.LoginProviderSettings) []domainsettings.LoginProviderSettings {
	out := make([]domainsettings.LoginProviderSettings, 0, len(items))
	for _, item := range items {
		out = append(out, domainsettings.LoginProviderSettings{
			ID:                  item.ID,
			Name:                item.Name,
			Type:                item.Type,
			Enabled:             item.Enabled,
			ClientID:            item.ClientID,
			ClientSecret:        item.ClientSecret,
			Issuer:              item.Issuer,
			AuthorizeURL:        item.AuthorizeURL,
			TokenURL:            item.TokenURL,
			UserInfoURL:         item.UserInfoURL,
			ProfileURL:          item.ProfileURL,
			RedirectURL:         item.RedirectURL,
			FrontendRedirectURL: item.FrontendRedirectURL,
			Scopes:              item.Scopes,
			DefaultRoles:        item.DefaultRoles,
			UserIDField:         item.UserIDField,
			UserNameField:       item.UserNameField,
			EmailField:          item.EmailField,
			MetadataURL:         item.MetadataURL,
			EntityID:            item.EntityID,
			Certificate:         item.Certificate,
		})
	}
	return out
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
