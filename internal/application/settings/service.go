package settings

import (
	"context"
	"fmt"
	"strings"

	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainsettings "github.com/kubecrux/kubecrux/internal/domain/settings"
	cfgpkg "github.com/kubecrux/kubecrux/internal/infrastructure/config"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
)

type Service struct {
	store       domainsettings.Store
	auth        cfgpkg.AuthConfig
	monitoring  cfgpkg.MonitoringConfig
	permissions *appaccess.PermissionResolver
}

func New(store domainsettings.Store, auth cfgpkg.AuthConfig, monitoring cfgpkg.MonitoringConfig, permissions *appaccess.PermissionResolver) *Service {
	return &Service{store: store, auth: auth, monitoring: monitoring, permissions: permissions}
}

func (s *Service) GetIdentitySettings(ctx context.Context, principal domainidentity.Principal) (domainsettings.IdentitySettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsIdentityView); err != nil {
		return domainsettings.IdentitySettings{}, err
	}
	return s.identitySettings(ctx)
}

func (s *Service) UpdateOIDCSettings(ctx context.Context, principal domainidentity.Principal, input domainsettings.OIDCSettings) (domainsettings.IdentitySettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsIdentityManage); err != nil {
		return domainsettings.IdentitySettings{}, err
	}
	input.ProviderName = strings.TrimSpace(input.ProviderName)
	input.Issuer = strings.TrimSpace(input.Issuer)
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.ClientSecret = strings.TrimSpace(input.ClientSecret)
	input.RedirectURL = strings.TrimSpace(input.RedirectURL)
	input.FrontendRedirectURL = strings.TrimSpace(input.FrontendRedirectURL)
	if len(input.Scopes) == 0 {
		input.Scopes = []string{"openid", "profile", "email"}
	}
	if input.Enabled {
		switch {
		case input.ProviderName == "":
			return domainsettings.IdentitySettings{}, fmt.Errorf("%w: oidc provider name is required", apperrors.ErrInvalidArgument)
		case input.Issuer == "":
			return domainsettings.IdentitySettings{}, fmt.Errorf("%w: oidc issuer is required", apperrors.ErrInvalidArgument)
		case input.ClientID == "":
			return domainsettings.IdentitySettings{}, fmt.Errorf("%w: oidc client id is required", apperrors.ErrInvalidArgument)
		case input.ClientSecret == "":
			return domainsettings.IdentitySettings{}, fmt.Errorf("%w: oidc client secret is required", apperrors.ErrInvalidArgument)
		case input.RedirectURL == "":
			return domainsettings.IdentitySettings{}, fmt.Errorf("%w: oidc redirect url is required", apperrors.ErrInvalidArgument)
		case input.FrontendRedirectURL == "":
			return domainsettings.IdentitySettings{}, fmt.Errorf("%w: oidc frontend redirect url is required", apperrors.ErrInvalidArgument)
		}
	}
	value := map[string]any{
		"enabled":             input.Enabled,
		"providerName":        input.ProviderName,
		"issuer":              input.Issuer,
		"clientId":            input.ClientID,
		"clientSecret":        input.ClientSecret,
		"redirectUrl":         input.RedirectURL,
		"frontendRedirectUrl": input.FrontendRedirectURL,
		"scopes":              input.Scopes,
		"defaultRoles":        input.DefaultRoles,
	}
	if err := s.store.Upsert(ctx, domainsettings.IdentityOIDCSettingKey, "identity", value, principal.UserID); err != nil {
		return domainsettings.IdentitySettings{}, err
	}
	return s.identitySettings(ctx)
}

func (s *Service) GetMonitoringSettings(ctx context.Context, principal domainidentity.Principal) (domainsettings.MonitoringSettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsMonitoringView); err != nil {
		return domainsettings.MonitoringSettings{}, err
	}
	return s.monitoringSettings(ctx)
}

func (s *Service) GetAISettings(ctx context.Context, principal domainidentity.Principal) (domainsettings.AISettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsAIView); err != nil {
		return domainsettings.AISettings{}, err
	}
	return s.aiSettings(ctx)
}

func (s *Service) GetBrandingSettings(ctx context.Context, principal domainidentity.Principal) (domainsettings.BrandingSettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsBrandingView); err != nil {
		return domainsettings.BrandingSettings{}, err
	}
	return s.brandingSettings(ctx)
}

func (s *Service) UpdateBrandingSettings(ctx context.Context, principal domainidentity.Principal, input domainsettings.BrandingSettings) (domainsettings.BrandingSettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsBrandingManage); err != nil {
		return domainsettings.BrandingSettings{}, err
	}
	input.AppTitle = strings.TrimSpace(input.AppTitle)
	input.SidebarTitle = strings.TrimSpace(input.SidebarTitle)
	input.LoginLogoURL = strings.TrimSpace(input.LoginLogoURL)
	input.ExpandedLogoURL = strings.TrimSpace(input.ExpandedLogoURL)
	input.CollapsedLogoURL = strings.TrimSpace(input.CollapsedLogoURL)
	input.FaviconURL = strings.TrimSpace(input.FaviconURL)
	value := map[string]any{
		"appTitle":         input.AppTitle,
		"sidebarTitle":     input.SidebarTitle,
		"loginLogoUrl":     input.LoginLogoURL,
		"expandedLogoUrl":  input.ExpandedLogoURL,
		"collapsedLogoUrl": input.CollapsedLogoURL,
		"faviconUrl":       input.FaviconURL,
	}
	if err := s.store.Upsert(ctx, domainsettings.BrandingSettingKey, "branding", value, principal.UserID); err != nil {
		return domainsettings.BrandingSettings{}, err
	}
	return s.brandingSettings(ctx)
}

func (s *Service) UpdateAISettings(ctx context.Context, principal domainidentity.Principal, input domainsettings.AISettings) (domainsettings.AISettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domainsettings.AISettings{}, err
	}
	input.Provider.BaseURL = strings.TrimSpace(input.Provider.BaseURL)
	input.Provider.APIKey = strings.TrimSpace(input.Provider.APIKey)
	input.Provider.Model = strings.TrimSpace(input.Provider.Model)
	if input.Provider.Enabled {
		if input.Provider.BaseURL == "" {
			return domainsettings.AISettings{}, fmt.Errorf("%w: ai base url is required", apperrors.ErrInvalidArgument)
		}
		if input.Provider.APIKey == "" {
			return domainsettings.AISettings{}, fmt.Errorf("%w: ai api key is required", apperrors.ErrInvalidArgument)
		}
		if input.Provider.Model == "" {
			input.Provider.Model = "gpt-4.1-mini"
		}
	}
	skills := make([]map[string]any, 0, len(input.SkillsRegistry))
	for _, item := range input.SkillsRegistry {
		skills = append(skills, map[string]any{
			"id":             strings.TrimSpace(item.ID),
			"name":           strings.TrimSpace(item.Name),
			"category":       strings.TrimSpace(item.Category),
			"ownerModule":    strings.TrimSpace(item.OwnerModule),
			"description":    strings.TrimSpace(item.Description),
			"capabilityRefs": item.CapabilityRefs,
			"blueprintRefs":  item.BlueprintRefs,
			"inputSchema":    item.InputSchema,
			"outputSchema":   item.OutputSchema,
			"scopeRules":     item.ScopeRules,
			"enabled":        item.Enabled,
			"scopes":         item.Scopes,
		})
	}
	value := map[string]any{
		"enabled":        input.Provider.Enabled,
		"baseUrl":        input.Provider.BaseURL,
		"apiKey":         input.Provider.APIKey,
		"model":          input.Provider.Model,
		"skillsRegistry": skills,
	}
	if err := s.store.Upsert(ctx, domainsettings.AIProviderSettingKey, "ai", value, principal.UserID); err != nil {
		return domainsettings.AISettings{}, err
	}
	return s.aiSettings(ctx)
}

func (s *Service) UpdatePrometheusSettings(ctx context.Context, principal domainidentity.Principal, input domainsettings.PrometheusSettings) (domainsettings.MonitoringSettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsMonitoringManage); err != nil {
		return domainsettings.MonitoringSettings{}, err
	}
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.BearerToken = strings.TrimSpace(input.BearerToken)
	input.ClusterLabel = strings.TrimSpace(input.ClusterLabel)
	input.GrafanaBaseURL = strings.TrimSpace(input.GrafanaBaseURL)
	if input.DefaultRangeMinutes <= 0 {
		input.DefaultRangeMinutes = 60
	}
	if input.StepSeconds <= 0 {
		input.StepSeconds = 60
	}
	if input.Enabled && input.BaseURL == "" {
		return domainsettings.MonitoringSettings{}, fmt.Errorf("%w: prometheus base url is required", apperrors.ErrInvalidArgument)
	}
	value := map[string]any{
		"enabled":             input.Enabled,
		"baseUrl":             input.BaseURL,
		"bearerToken":         input.BearerToken,
		"defaultRangeMinutes": input.DefaultRangeMinutes,
		"stepSeconds":         input.StepSeconds,
		"clusterLabel":        input.ClusterLabel,
		"grafanaBaseUrl":      input.GrafanaBaseURL,
	}
	if err := s.store.Upsert(ctx, domainsettings.MonitoringPrometheusSettingKey, "monitoring", value, principal.UserID); err != nil {
		return domainsettings.MonitoringSettings{}, err
	}
	return s.monitoringSettings(ctx)
}

func (s *Service) ResolveOIDCSettings(ctx context.Context) (cfgpkg.OIDCConfig, error) {
	settings, err := s.identitySettings(ctx)
	if err != nil {
		return cfgpkg.OIDCConfig{}, err
	}
	return cfgpkg.OIDCConfig{
		Enabled:             settings.OIDC.Enabled,
		ProviderName:        settings.OIDC.ProviderName,
		Issuer:              settings.OIDC.Issuer,
		ClientID:            settings.OIDC.ClientID,
		ClientSecret:        settings.OIDC.ClientSecret,
		RedirectURL:         settings.OIDC.RedirectURL,
		FrontendRedirectURL: settings.OIDC.FrontendRedirectURL,
		Scopes:              settings.OIDC.Scopes,
		DefaultRoles:        settings.OIDC.DefaultRoles,
	}, nil
}

func (s *Service) ResolveMonitoringSettings(ctx context.Context) (domainsettings.MonitoringSettings, error) {
	return s.monitoringSettings(ctx)
}

func (s *Service) ResolveAISettings(ctx context.Context) (domainsettings.AISettings, error) {
	return s.aiSettings(ctx)
}

func (s *Service) ResolveBrandingSettings(ctx context.Context) (domainsettings.BrandingSettings, error) {
	return s.brandingSettings(ctx)
}

func (s *Service) identitySettings(ctx context.Context) (domainsettings.IdentitySettings, error) {
	item := domainsettings.IdentitySettings{
		OIDC: domainsettings.OIDCSettings{
			Enabled:             s.auth.OIDC.Enabled,
			ProviderName:        s.auth.OIDC.ProviderName,
			Issuer:              s.auth.OIDC.Issuer,
			ClientID:            s.auth.OIDC.ClientID,
			ClientSecret:        s.auth.OIDC.ClientSecret,
			RedirectURL:         s.auth.OIDC.RedirectURL,
			FrontendRedirectURL: s.auth.OIDC.FrontendRedirectURL,
			Scopes:              append([]string(nil), s.auth.OIDC.Scopes...),
			DefaultRoles:        append([]string(nil), s.auth.OIDC.DefaultRoles...),
		},
	}
	if s.store == nil {
		return item, nil
	}
	raw, ok, err := s.store.Get(ctx, domainsettings.IdentityOIDCSettingKey)
	if err != nil || !ok {
		return item, err
	}
	if value, ok := raw["providerName"].(string); ok && strings.TrimSpace(value) != "" {
		item.OIDC.ProviderName = value
	}
	if value, ok := raw["issuer"].(string); ok && strings.TrimSpace(value) != "" {
		item.OIDC.Issuer = value
	}
	if value, ok := raw["clientId"].(string); ok && strings.TrimSpace(value) != "" {
		item.OIDC.ClientID = value
	}
	if value, ok := raw["clientSecret"].(string); ok && strings.TrimSpace(value) != "" {
		item.OIDC.ClientSecret = value
	}
	if value, ok := raw["redirectUrl"].(string); ok && strings.TrimSpace(value) != "" {
		item.OIDC.RedirectURL = value
	}
	if value, ok := raw["frontendRedirectUrl"].(string); ok && strings.TrimSpace(value) != "" {
		item.OIDC.FrontendRedirectURL = value
	}
	if value, ok := raw["scopes"].([]any); ok && len(value) > 0 {
		item.OIDC.Scopes = sliceOfStrings(value)
	}
	if value, ok := raw["defaultRoles"].([]any); ok && len(value) > 0 {
		item.OIDC.DefaultRoles = sliceOfStrings(value)
	}
	if value, ok := raw["enabled"].(bool); ok {
		item.OIDC.Enabled = value
	}
	return item, nil
}

func (s *Service) monitoringSettings(ctx context.Context) (domainsettings.MonitoringSettings, error) {
	item := domainsettings.MonitoringSettings{
		Prometheus: domainsettings.PrometheusSettings{
			Enabled:             strings.TrimSpace(s.monitoring.PrometheusURL) != "",
			BaseURL:             s.monitoring.PrometheusURL,
			BearerToken:         s.monitoring.PrometheusBearerToken,
			DefaultRangeMinutes: s.monitoring.PrometheusDefaultRangeMinutes,
			StepSeconds:         s.monitoring.PrometheusStepSeconds,
			ClusterLabel:        s.monitoring.PrometheusClusterLabel,
			GrafanaBaseURL:      s.monitoring.GrafanaBaseURL,
		},
	}
	if item.Prometheus.DefaultRangeMinutes <= 0 {
		item.Prometheus.DefaultRangeMinutes = 60
	}
	if item.Prometheus.StepSeconds <= 0 {
		item.Prometheus.StepSeconds = 60
	}
	if s.store == nil {
		return item, nil
	}
	raw, ok, err := s.store.Get(ctx, domainsettings.MonitoringPrometheusSettingKey)
	if err != nil || !ok {
		return item, err
	}
	if value, ok := raw["enabled"].(bool); ok {
		item.Prometheus.Enabled = value
	}
	if value, ok := raw["baseUrl"].(string); ok {
		item.Prometheus.BaseURL = strings.TrimSpace(value)
	}
	if value, ok := raw["bearerToken"].(string); ok {
		item.Prometheus.BearerToken = strings.TrimSpace(value)
	}
	if value, ok := intValue(raw["defaultRangeMinutes"]); ok && value > 0 {
		item.Prometheus.DefaultRangeMinutes = value
	}
	if value, ok := intValue(raw["stepSeconds"]); ok && value > 0 {
		item.Prometheus.StepSeconds = value
	}
	if value, ok := raw["clusterLabel"].(string); ok {
		item.Prometheus.ClusterLabel = strings.TrimSpace(value)
	}
	if value, ok := raw["grafanaBaseUrl"].(string); ok {
		item.Prometheus.GrafanaBaseURL = strings.TrimSpace(value)
	}
	return item, nil
}

func (s *Service) aiSettings(ctx context.Context) (domainsettings.AISettings, error) {
	item := domainsettings.AISettings{
		Provider: domainsettings.AIProviderSettings{
			Enabled: false,
			Model:   "gpt-4.1-mini",
		},
	}
	if s.store == nil {
		return item, nil
	}
	raw, ok, err := s.store.Get(ctx, domainsettings.AIProviderSettingKey)
	if err != nil || !ok {
		return item, err
	}
	if value, ok := raw["enabled"].(bool); ok {
		item.Provider.Enabled = value
	}
	if value, ok := raw["baseUrl"].(string); ok {
		item.Provider.BaseURL = strings.TrimSpace(value)
	}
	if value, ok := raw["apiKey"].(string); ok {
		item.Provider.APIKey = strings.TrimSpace(value)
	}
	if value, ok := raw["model"].(string); ok && strings.TrimSpace(value) != "" {
		item.Provider.Model = strings.TrimSpace(value)
	}
	if values, ok := raw["skillsRegistry"].([]any); ok {
		item.SkillsRegistry = make([]domainsettings.AISkillSettings, 0, len(values))
		for _, current := range values {
			record, ok := current.(map[string]any)
			if !ok {
				continue
			}
			scopes := []string{}
			scopeRules := []string{}
			capabilityRefs := []string{}
			blueprintRefs := []string{}
			if rawScopes, ok := record["scopes"].([]any); ok {
				for _, scope := range rawScopes {
					scopes = append(scopes, fmt.Sprint(scope))
				}
			}
			if rawScopeRules, ok := record["scopeRules"].([]any); ok {
				for _, value := range rawScopeRules {
					scopeRules = append(scopeRules, fmt.Sprint(value))
				}
			}
			if rawCapabilityRefs, ok := record["capabilityRefs"].([]any); ok {
				for _, value := range rawCapabilityRefs {
					capabilityRefs = append(capabilityRefs, fmt.Sprint(value))
				}
			}
			if rawBlueprintRefs, ok := record["blueprintRefs"].([]any); ok {
				for _, value := range rawBlueprintRefs {
					blueprintRefs = append(blueprintRefs, fmt.Sprint(value))
				}
			}
			item.SkillsRegistry = append(item.SkillsRegistry, domainsettings.AISkillSettings{
				ID:             strings.TrimSpace(fmt.Sprint(record["id"])),
				Name:           strings.TrimSpace(fmt.Sprint(record["name"])),
				Category:       strings.TrimSpace(fmt.Sprint(record["category"])),
				OwnerModule:    strings.TrimSpace(fmt.Sprint(record["ownerModule"])),
				Description:    strings.TrimSpace(fmt.Sprint(record["description"])),
				CapabilityRefs: capabilityRefs,
				BlueprintRefs:  blueprintRefs,
				InputSchema:    mapValue(record["inputSchema"]),
				OutputSchema:   mapValue(record["outputSchema"]),
				ScopeRules:     scopeRules,
				Enabled:        boolValue(record["enabled"]),
				Scopes:         scopes,
			})
		}
	}
	return item, nil
}

func (s *Service) brandingSettings(ctx context.Context) (domainsettings.BrandingSettings, error) {
	item := domainsettings.BrandingSettings{
		AppTitle:     "KubeCrux",
		SidebarTitle: "KubeCrux",
	}
	if s.store == nil {
		return item, nil
	}
	raw, ok, err := s.store.Get(ctx, domainsettings.BrandingSettingKey)
	if err != nil || !ok {
		return item, err
	}
	if value, ok := raw["appTitle"].(string); ok && strings.TrimSpace(value) != "" {
		item.AppTitle = strings.TrimSpace(value)
	}
	if value, ok := raw["sidebarTitle"].(string); ok && strings.TrimSpace(value) != "" {
		item.SidebarTitle = strings.TrimSpace(value)
	}
	if value, ok := raw["loginLogoUrl"].(string); ok {
		item.LoginLogoURL = strings.TrimSpace(value)
	}
	if value, ok := raw["expandedLogoUrl"].(string); ok {
		item.ExpandedLogoURL = strings.TrimSpace(value)
	}
	if value, ok := raw["collapsedLogoUrl"].(string); ok {
		item.CollapsedLogoURL = strings.TrimSpace(value)
	}
	if value, ok := raw["faviconUrl"].(string); ok {
		item.FaviconURL = strings.TrimSpace(value)
	}
	return item, nil
}

func boolValue(value any) bool {
	current, ok := value.(bool)
	return ok && current
}

func mapValue(value any) map[string]any {
	current, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return current
}

func sliceOfStrings(items []any) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func ensureAdmin(principal domainidentity.Principal) error {
	for _, role := range principal.Roles {
		if role == "admin" {
			return nil
		}
	}
	return fmt.Errorf("%w: admin role required", apperrors.ErrAccessDenied)
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}
