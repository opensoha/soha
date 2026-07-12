package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

type IdentitySettingsService interface {
	GetIdentitySettings(context.Context, domainidentity.Principal) (domainsettings.IdentitySettings, error)
	UpdateLoginProvidersSettings(context.Context, domainidentity.Principal, []domainsettings.LoginProviderSettings, string, bool) (domainsettings.IdentitySettings, error)
}

type MonitoringSettingsService interface {
	GetMonitoringSettings(context.Context, domainidentity.Principal) (domainsettings.MonitoringSettings, error)
	UpdatePrometheusSettings(context.Context, domainidentity.Principal, domainsettings.PrometheusSettings) (domainsettings.MonitoringSettings, error)
}

type AISettingsService interface {
	GetAISettings(context.Context, domainidentity.Principal) (domainsettings.AISettings, error)
	UpdateAIWorkbenchModelSettings(context.Context, domainidentity.Principal, domainsettings.AIWorkbenchModelSettings) (domainsettings.AISettings, error)
	UpdateAISkillsRegistry(context.Context, domainidentity.Principal, []domainsettings.AISkillSettings) (domainsettings.AISettings, error)
}

type BrandingSettingsService interface {
	GetBrandingSettings(context.Context, domainidentity.Principal) (domainsettings.BrandingSettings, error)
	UpdateBrandingSettings(context.Context, domainidentity.Principal, domainsettings.BrandingSettings) (domainsettings.BrandingSettings, error)
}

type SettingsService interface {
	IdentitySettingsService
	MonitoringSettingsService
	AISettingsService
	BrandingSettingsService
}

type SettingsHandler struct {
	identity    IdentitySettingsService
	monitoring  MonitoringSettingsService
	ai          AISettingsService
	branding    BrandingSettingsService
	permissions *appaccess.PermissionResolver
}

func NewSettingsHandler(service SettingsService, permissions *appaccess.PermissionResolver) *SettingsHandler {
	return NewSettingsHandlerWithServices(service, service, service, service, permissions)
}

func NewSettingsHandlerWithServices(identity IdentitySettingsService, monitoring MonitoringSettingsService, ai AISettingsService, branding BrandingSettingsService, permissions *appaccess.PermissionResolver) *SettingsHandler {
	return &SettingsHandler{identity: identity, monitoring: monitoring, ai: ai, branding: branding, permissions: permissions}
}

func (h *SettingsHandler) GetIdentitySettings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.identity.GetIdentitySettings(c.Request.Context(), principal)
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
	current, err := h.identity.GetIdentitySettings(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	localPasswordEnabled := current.LocalPasswordLoginEnabled
	if req.LocalPasswordLoginEnabled != nil {
		localPasswordEnabled = *req.LocalPasswordLoginEnabled
	}
	item, err := h.identity.UpdateLoginProvidersSettings(c.Request.Context(), principal, mapLoginProviders(req.Providers), req.DefaultProviderID, localPasswordEnabled)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) GetMonitoringSettings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.monitoring.GetMonitoringSettings(c.Request.Context(), principal)
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
	item, err := h.monitoring.UpdatePrometheusSettings(c.Request.Context(), principal, domainsettings.PrometheusSettings{
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
	item, err := h.ai.GetAISettings(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapAISettingsResponse(item))
}

func (h *SettingsHandler) UpdateAIWorkbenchModelSettings(c *gin.Context) {
	var req dto.UpdateAIWorkbenchModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid ai workbench model payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.ai.UpdateAIWorkbenchModelSettings(c.Request.Context(), principal, mapAIWorkbenchModel(req.WorkbenchModel))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapAISettingsResponse(item))
}

func (h *SettingsHandler) UpdateAISkills(c *gin.Context) {
	var req dto.UpdateAISkillsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid ai skills payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.ai.UpdateAISkillsRegistry(c.Request.Context(), principal, mapAISkills(req.SkillsRegistry))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapAISettingsResponse(item))
}

func (h *SettingsHandler) GetBrandingSettings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.branding.GetBrandingSettings(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func mapAISettingsResponse(item domainsettings.AISettings) map[string]any {
	return map[string]any{
		"workbenchModel": map[string]any{
			"defaultPublicModel": item.WorkbenchModel.DefaultPublicModel,
			"defaultRouteId":     item.WorkbenchModel.DefaultRouteID,
			"defaultEndpoint":    item.WorkbenchModel.DefaultEndpoint,
			"enabled":            item.WorkbenchModel.Enabled,
		},
		"skillsRegistry": item.SkillsRegistry,
	}
}

func mapAIWorkbenchModel(item dto.AIWorkbenchModelSettings) domainsettings.AIWorkbenchModelSettings {
	return domainsettings.AIWorkbenchModelSettings{
		DefaultPublicModel: item.DefaultPublicModel,
		DefaultRouteID:     item.DefaultRouteID,
		DefaultEndpoint:    item.DefaultEndpoint,
		Enabled:            item.Enabled,
	}
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

func mapLoginProviders(items []dto.LoginProviderSettings) []domainsettings.LoginProviderSettings {
	out := make([]domainsettings.LoginProviderSettings, 0, len(items))
	for _, item := range items {
		out = append(out, domainsettings.LoginProviderSettings{
			ID:                  item.ID,
			Name:                item.Name,
			Type:                item.Type,
			IconURL:             item.IconURL,
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
			PhoneField:          item.PhoneField,
			AvatarField:         item.AvatarField,
			RoleField:           item.RoleField,
			OrganizationField:   item.OrganizationField,
			SyncRolesOnLogin:    item.SyncRolesOnLogin,
			SyncOrgsOnLogin:     item.SyncOrgsOnLogin,
			RoleSyncMode:        item.RoleSyncMode,
			OrgSyncMode:         item.OrgSyncMode,
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
	item, err := h.branding.UpdateBrandingSettings(c.Request.Context(), principal, domainsettings.BrandingSettings{
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
