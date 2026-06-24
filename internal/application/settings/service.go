package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Service struct {
	store       domainsettings.Store
	auth        cfgpkg.AuthConfig
	monitoring  cfgpkg.MonitoringConfig
	permissions *appaccess.PermissionResolver
	http        *http.Client
}

func New(store domainsettings.Store, auth cfgpkg.AuthConfig, monitoring cfgpkg.MonitoringConfig, permissions *appaccess.PermissionResolver) *Service {
	return &Service{store: store, auth: auth, monitoring: monitoring, permissions: permissions, http: &http.Client{Timeout: 20 * time.Second}}
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
	current, err := s.identitySettings(ctx)
	if err != nil {
		return domainsettings.IdentitySettings{}, err
	}
	legacyProvider := loginProviderFromOIDC(input)
	providers := upsertLoginProvider(current.Providers, legacyProvider)
	defaultProviderID := strings.TrimSpace(current.DefaultProviderID)
	if defaultProviderID == "" || !slices.ContainsFunc(providers, func(item domainsettings.LoginProviderSettings) bool {
		return item.ID == defaultProviderID
	}) {
		defaultProviderID = legacyProvider.ID
	}
	if err := s.persistLoginProvidersSettings(ctx, principal.UserID, providers, defaultProviderID); err != nil {
		return domainsettings.IdentitySettings{}, err
	}
	return s.identitySettings(ctx)
}

func (s *Service) UpdateLoginProvidersSettings(ctx context.Context, principal domainidentity.Principal, providers []domainsettings.LoginProviderSettings, defaultProviderID string) (domainsettings.IdentitySettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsIdentityManage); err != nil {
		return domainsettings.IdentitySettings{}, err
	}
	normalized := make([]domainsettings.LoginProviderSettings, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for index, item := range providers {
		current := normalizeLoginProvider(item, index)
		if _, exists := seen[current.ID]; exists {
			return domainsettings.IdentitySettings{}, fmt.Errorf("%w: duplicated login provider id %s", apperrors.ErrInvalidArgument, current.ID)
		}
		seen[current.ID] = struct{}{}
		if err := validateLoginProvider(current); err != nil {
			return domainsettings.IdentitySettings{}, err
		}
		normalized = append(normalized, current)
	}
	if len(normalized) > 0 {
		if strings.TrimSpace(defaultProviderID) == "" {
			defaultProviderID = normalized[0].ID
		}
		if !slices.ContainsFunc(normalized, func(item domainsettings.LoginProviderSettings) bool {
			return item.ID == strings.TrimSpace(defaultProviderID)
		}) {
			return domainsettings.IdentitySettings{}, fmt.Errorf("%w: default login provider is invalid", apperrors.ErrInvalidArgument)
		}
	} else {
		defaultProviderID = ""
	}
	if err := s.persistLoginProvidersSettings(ctx, principal.UserID, normalized, defaultProviderID); err != nil {
		return domainsettings.IdentitySettings{}, err
	}
	if err := s.syncLegacyOIDCSettings(ctx, principal.UserID, normalized, defaultProviderID); err != nil {
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
	input.Provider = normalizeAIProvider(input.Provider)
	for index := range input.Providers {
		input.Providers[index] = normalizeAIProvider(input.Providers[index])
		if input.Providers[index].ID == "" {
			input.Providers[index].ID = fmt.Sprintf("provider-%d", index+1)
		}
		if input.Providers[index].Name == "" {
			input.Providers[index].Name = input.Providers[index].ProviderKind
		}
		if input.Providers[index].Enabled {
			if input.Providers[index].BaseURL == "" {
				return domainsettings.AISettings{}, fmt.Errorf("%w: ai provider base url is required", apperrors.ErrInvalidArgument)
			}
			if input.Providers[index].APIKey == "" {
				return domainsettings.AISettings{}, fmt.Errorf("%w: ai provider api key is required", apperrors.ErrInvalidArgument)
			}
		}
	}
	if input.DefaultProviderID == "" && len(input.Providers) > 0 {
		input.DefaultProviderID = input.Providers[0].ID
	}
	if len(input.Providers) == 0 && input.Provider.BaseURL != "" {
		input.Provider.ID = "default"
		input.Provider.Name = "default"
		input.Provider.ProviderKind = defaultProviderKind(input.Provider.ProviderKind)
		input.Providers = []domainsettings.AIProviderSettings{input.Provider}
		input.DefaultProviderID = input.Provider.ID
	}
	input.Provider = resolveDefaultProvider(input)
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
	return s.persistAISettings(ctx, principal.UserID, input.Provider, input.Providers, input.DefaultProviderID, skills)
}

func (s *Service) UpdateAIProviderConnections(ctx context.Context, principal domainidentity.Principal, providers []domainsettings.AIProviderSettings, defaultProviderID string) (domainsettings.AISettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAIView); err != nil {
		return domainsettings.AISettings{}, err
	}
	current, err := s.aiSettings(ctx)
	if err != nil {
		return domainsettings.AISettings{}, err
	}
	for index := range providers {
		providers[index] = normalizeAIProvider(providers[index])
		if providers[index].ID == "" {
			providers[index].ID = fmt.Sprintf("provider-%d", index+1)
		}
		if providers[index].Name == "" {
			providers[index].Name = providers[index].ProviderKind
		}
	}
	next := domainsettings.AISettings{
		Provider:          current.Provider,
		Providers:         providers,
		DefaultProviderID: strings.TrimSpace(defaultProviderID),
		SkillsRegistry:    current.SkillsRegistry,
	}
	if next.DefaultProviderID == "" && len(next.Providers) > 0 {
		next.DefaultProviderID = next.Providers[0].ID
	}
	next.Provider = resolveDefaultProvider(next)
	skills := make([]map[string]any, 0, len(current.SkillsRegistry))
	for _, item := range current.SkillsRegistry {
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
	return s.persistAISettings(ctx, principal.UserID, next.Provider, next.Providers, next.DefaultProviderID, skills)
}

func (s *Service) ListAIProviderModels(ctx context.Context, principal domainidentity.Principal, provider domainsettings.AIProviderSettings) ([]string, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsAIView); err != nil {
		return nil, err
	}
	provider = normalizeAIProvider(provider)
	if provider.BaseURL == "" || provider.APIKey == "" {
		return nil, fmt.Errorf("%w: provider base url and api key are required", apperrors.ErrInvalidArgument)
	}
	return s.fetchProviderModels(ctx, provider)
}

func (s *Service) TestAIProviderConnectivity(ctx context.Context, principal domainidentity.Principal, provider domainsettings.AIProviderSettings, prompt string) (domainsettings.AIProviderTestResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsAIView); err != nil {
		return domainsettings.AIProviderTestResult{}, err
	}
	provider = normalizeAIProvider(provider)
	if provider.BaseURL == "" || provider.APIKey == "" {
		return domainsettings.AIProviderTestResult{}, fmt.Errorf("%w: provider base url and api key are required", apperrors.ErrInvalidArgument)
	}
	reply, err := s.providerHello(ctx, provider, strings.TrimSpace(prompt))
	if err != nil {
		return domainsettings.AIProviderTestResult{}, err
	}
	return domainsettings.AIProviderTestResult{
		OK:      true,
		Model:   provider.Model,
		Message: "connectivity verified",
		Reply:   reply,
	}, nil
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
	if provider, ok := resolvePreferredOIDCProvider(settings.Providers, settings.DefaultProviderID); ok {
		return oidcConfigFromProvider(provider), nil
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

func (s *Service) ResolveLoginProviders(ctx context.Context) ([]domainsettings.LoginProviderSettings, string, error) {
	settings, err := s.identitySettings(ctx)
	if err != nil {
		return nil, "", err
	}
	return append([]domainsettings.LoginProviderSettings(nil), settings.Providers...), settings.DefaultProviderID, nil
}

func (s *Service) ResolveLoginProvider(ctx context.Context, providerID string) (domainsettings.LoginProviderSettings, error) {
	providers, defaultProviderID, err := s.ResolveLoginProviders(ctx)
	if err != nil {
		return domainsettings.LoginProviderSettings{}, err
	}
	targetID := strings.TrimSpace(providerID)
	if targetID == "" {
		targetID = strings.TrimSpace(defaultProviderID)
	}
	if targetID != "" {
		for _, item := range providers {
			if item.ID == targetID {
				return item, nil
			}
		}
	}
	if targetID == "" {
		for _, item := range providers {
			if item.Enabled {
				return item, nil
			}
		}
	}
	return domainsettings.LoginProviderSettings{}, fmt.Errorf("%w: login provider not found", apperrors.ErrNotFound)
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
	rawProviders, ok, err := s.store.Get(ctx, domainsettings.IdentityLoginProvidersSettingKey)
	if err != nil {
		return item, err
	}
	if ok {
		if value, ok := rawProviders["defaultProviderId"].(string); ok {
			item.DefaultProviderID = strings.TrimSpace(value)
		}
		if values, ok := rawProviders["providers"].([]any); ok {
			item.Providers = make([]domainsettings.LoginProviderSettings, 0, len(values))
			for index, current := range values {
				record, ok := current.(map[string]any)
				if !ok {
					continue
				}
				item.Providers = append(item.Providers, normalizeLoginProvider(domainsettings.LoginProviderSettings{
					ID:                  settingStringValue(record["id"]),
					Name:                settingStringValue(record["name"]),
					Type:                settingStringValue(record["type"]),
					Enabled:             boolValue(record["enabled"]),
					ClientID:            settingStringValue(record["clientId"]),
					ClientSecret:        settingStringValue(record["clientSecret"]),
					Issuer:              settingStringValue(record["issuer"]),
					AuthorizeURL:        settingStringValue(record["authorizeUrl"]),
					TokenURL:            settingStringValue(record["tokenUrl"]),
					UserInfoURL:         settingStringValue(record["userInfoUrl"]),
					ProfileURL:          settingStringValue(record["profileUrl"]),
					RedirectURL:         settingStringValue(record["redirectUrl"]),
					FrontendRedirectURL: settingStringValue(record["frontendRedirectUrl"]),
					Scopes:              sliceOfStringsAny(record["scopes"]),
					DefaultRoles:        sliceOfStringsAny(record["defaultRoles"]),
					UserIDField:         settingStringValue(record["userIdField"]),
					UserNameField:       settingStringValue(record["userNameField"]),
					EmailField:          settingStringValue(record["emailField"]),
					RoleField:           settingStringValue(record["roleField"]),
					OrganizationField:   settingStringValue(record["organizationField"]),
					SyncRolesOnLogin:    boolValue(record["syncRolesOnLogin"]),
					SyncOrgsOnLogin:     boolValue(record["syncOrgsOnLogin"]),
					RoleSyncMode:        settingStringValue(record["roleSyncMode"]),
					OrgSyncMode:         settingStringValue(record["orgSyncMode"]),
					MetadataURL:         settingStringValue(record["metadataUrl"]),
					EntityID:            settingStringValue(record["entityId"]),
					Certificate:         settingStringValue(record["certificate"]),
				}, index))
			}
		}
	}
	if len(item.Providers) == 0 && hasLegacyOIDCConfig(item.OIDC) {
		legacyProvider := loginProviderFromOIDC(item.OIDC)
		item.Providers = []domainsettings.LoginProviderSettings{legacyProvider}
		if item.DefaultProviderID == "" {
			item.DefaultProviderID = legacyProvider.ID
		}
		s.migrateLegacyLoginProviders(ctx, legacyProvider)
	}
	if item.DefaultProviderID == "" && len(item.Providers) > 0 {
		item.DefaultProviderID = item.Providers[0].ID
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
			Enabled:      false,
			Model:        "gpt-4.1-mini",
			ProviderKind: "openai-compatible",
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
	if value, ok := raw["defaultProviderId"].(string); ok && strings.TrimSpace(value) != "" {
		item.DefaultProviderID = strings.TrimSpace(value)
	}
	if values, ok := raw["providers"].([]any); ok {
		item.Providers = make([]domainsettings.AIProviderSettings, 0, len(values))
		for _, current := range values {
			record, ok := current.(map[string]any)
			if !ok {
				continue
			}
			item.Providers = append(item.Providers, domainsettings.AIProviderSettings{
				ID:           strings.TrimSpace(fmt.Sprint(record["id"])),
				Name:         strings.TrimSpace(fmt.Sprint(record["name"])),
				ProviderKind: defaultProviderKind(strings.TrimSpace(fmt.Sprint(record["providerKind"]))),
				Enabled:      boolValue(record["enabled"]),
				BaseURL:      strings.TrimSpace(fmt.Sprint(record["baseUrl"])),
				APIKey:       strings.TrimSpace(fmt.Sprint(record["apiKey"])),
				Model:        strings.TrimSpace(fmt.Sprint(record["model"])),
			})
		}
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
	if len(item.Providers) == 0 && (item.Provider.BaseURL != "" || item.Provider.APIKey != "" || item.Provider.Model != "") {
		item.Provider.ID = "default"
		item.Provider.Name = "default"
		item.Provider.ProviderKind = defaultProviderKind(item.Provider.ProviderKind)
		item.Providers = []domainsettings.AIProviderSettings{item.Provider}
		if item.DefaultProviderID == "" {
			item.DefaultProviderID = item.Provider.ID
		}
	}
	item.Provider = resolveDefaultProvider(item)
	return item, nil
}

func normalizeAIProvider(input domainsettings.AIProviderSettings) domainsettings.AIProviderSettings {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.ProviderKind = defaultProviderKind(strings.TrimSpace(input.ProviderKind))
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.Model = strings.TrimSpace(input.Model)
	if input.Model == "" {
		input.Model = "gpt-4.1-mini"
	}
	return input
}

func defaultProviderKind(value string) string {
	if strings.TrimSpace(value) == "" {
		return "openai-compatible"
	}
	return strings.TrimSpace(value)
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func uniqueNonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func sliceOfStringsAny(raw any) []string {
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(fmt.Sprint(value))
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func resolveDefaultProvider(input domainsettings.AISettings) domainsettings.AIProviderSettings {
	if input.DefaultProviderID != "" {
		for _, item := range input.Providers {
			if item.ID == input.DefaultProviderID {
				return item
			}
		}
	}
	if len(input.Providers) > 0 {
		return input.Providers[0]
	}
	return normalizeAIProvider(input.Provider)
}

func providersToMaps(items []domainsettings.AIProviderSettings) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":           item.ID,
			"name":         item.Name,
			"providerKind": item.ProviderKind,
			"enabled":      item.Enabled,
			"baseUrl":      item.BaseURL,
			"apiKey":       item.APIKey,
			"model":        item.Model,
		})
	}
	return out
}

func loginProvidersToMaps(items []domainsettings.LoginProviderSettings) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":                  item.ID,
			"name":                item.Name,
			"type":                item.Type,
			"enabled":             item.Enabled,
			"clientId":            item.ClientID,
			"clientSecret":        item.ClientSecret,
			"issuer":              item.Issuer,
			"authorizeUrl":        item.AuthorizeURL,
			"tokenUrl":            item.TokenURL,
			"userInfoUrl":         item.UserInfoURL,
			"profileUrl":          item.ProfileURL,
			"redirectUrl":         item.RedirectURL,
			"frontendRedirectUrl": item.FrontendRedirectURL,
			"scopes":              item.Scopes,
			"defaultRoles":        item.DefaultRoles,
			"userIdField":         item.UserIDField,
			"userNameField":       item.UserNameField,
			"emailField":          item.EmailField,
			"roleField":           item.RoleField,
			"organizationField":   item.OrganizationField,
			"syncRolesOnLogin":    item.SyncRolesOnLogin,
			"syncOrgsOnLogin":     item.SyncOrgsOnLogin,
			"roleSyncMode":        item.RoleSyncMode,
			"orgSyncMode":         item.OrgSyncMode,
			"metadataUrl":         item.MetadataURL,
			"entityId":            item.EntityID,
			"certificate":         item.Certificate,
		})
	}
	return out
}

func (s *Service) persistLoginProvidersSettings(ctx context.Context, updatedBy string, providers []domainsettings.LoginProviderSettings, defaultProviderID string) error {
	value := map[string]any{
		"defaultProviderId": strings.TrimSpace(defaultProviderID),
		"providers":         loginProvidersToMaps(providers),
	}
	return s.store.Upsert(ctx, domainsettings.IdentityLoginProvidersSettingKey, "identity", value, updatedBy)
}

func (s *Service) migrateLegacyLoginProviders(ctx context.Context, provider domainsettings.LoginProviderSettings) {
	if s.store == nil {
		return
	}
	_ = s.persistLoginProvidersSettings(ctx, "system", []domainsettings.LoginProviderSettings{provider}, provider.ID)
}

func (s *Service) syncLegacyOIDCSettings(ctx context.Context, updatedBy string, providers []domainsettings.LoginProviderSettings, defaultProviderID string) error {
	provider, ok := resolvePreferredOIDCProvider(providers, defaultProviderID)
	if !ok {
		value := map[string]any{
			"enabled":             false,
			"providerName":        "",
			"issuer":              "",
			"clientId":            "",
			"clientSecret":        "",
			"redirectUrl":         "",
			"frontendRedirectUrl": "",
			"scopes":              []string{"openid", "profile", "email"},
			"defaultRoles":        []string{},
		}
		return s.store.Upsert(ctx, domainsettings.IdentityOIDCSettingKey, "identity", value, updatedBy)
	}
	cfg := oidcConfigFromProvider(provider)
	value := map[string]any{
		"enabled":             cfg.Enabled,
		"providerName":        cfg.ProviderName,
		"issuer":              cfg.Issuer,
		"clientId":            cfg.ClientID,
		"clientSecret":        cfg.ClientSecret,
		"redirectUrl":         cfg.RedirectURL,
		"frontendRedirectUrl": cfg.FrontendRedirectURL,
		"scopes":              cfg.Scopes,
		"defaultRoles":        cfg.DefaultRoles,
	}
	return s.store.Upsert(ctx, domainsettings.IdentityOIDCSettingKey, "identity", value, updatedBy)
}

func (s *Service) persistAISettings(ctx context.Context, updatedBy string, provider domainsettings.AIProviderSettings, providers []domainsettings.AIProviderSettings, defaultProviderID string, skills []map[string]any) (domainsettings.AISettings, error) {
	value := map[string]any{
		"enabled":           provider.Enabled,
		"baseUrl":           provider.BaseURL,
		"apiKey":            provider.APIKey,
		"model":             provider.Model,
		"defaultProviderId": defaultProviderID,
		"providers":         providersToMaps(providers),
		"skillsRegistry":    skills,
	}
	if err := s.store.Upsert(ctx, domainsettings.AIProviderSettingKey, "ai", value, updatedBy); err != nil {
		return domainsettings.AISettings{}, err
	}
	return s.aiSettings(ctx)
}

func normalizeLoginProvider(input domainsettings.LoginProviderSettings, index int) domainsettings.LoginProviderSettings {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Type = normalizeLoginProviderType(input.Type)
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.ClientSecret = strings.TrimSpace(input.ClientSecret)
	input.Issuer = strings.TrimSpace(input.Issuer)
	input.AuthorizeURL = strings.TrimSpace(input.AuthorizeURL)
	input.TokenURL = strings.TrimSpace(input.TokenURL)
	input.UserInfoURL = strings.TrimSpace(input.UserInfoURL)
	input.ProfileURL = strings.TrimSpace(input.ProfileURL)
	input.RedirectURL = strings.TrimSpace(input.RedirectURL)
	input.FrontendRedirectURL = strings.TrimSpace(input.FrontendRedirectURL)
	input.MetadataURL = strings.TrimSpace(input.MetadataURL)
	input.EntityID = strings.TrimSpace(input.EntityID)
	input.Certificate = strings.TrimSpace(input.Certificate)
	input.UserIDField = strings.TrimSpace(input.UserIDField)
	input.UserNameField = strings.TrimSpace(input.UserNameField)
	input.EmailField = strings.TrimSpace(input.EmailField)
	input.RoleField = strings.TrimSpace(input.RoleField)
	input.OrganizationField = strings.TrimSpace(input.OrganizationField)
	input.RoleSyncMode = normalizeLoginSyncMode(input.RoleSyncMode)
	input.OrgSyncMode = normalizeLoginSyncMode(input.OrgSyncMode)
	input.Scopes = uniqueNonEmptyStrings(input.Scopes)
	input.DefaultRoles = uniqueNonEmptyStrings(input.DefaultRoles)
	if input.ID == "" {
		input.ID = fmt.Sprintf("%s-%d", input.Type, index+1)
	}
	if input.Name == "" {
		input.Name = input.ID
	}
	if len(input.Scopes) == 0 && (input.Type == "oidc" || input.Type == "oauth2") {
		input.Scopes = defaultScopesForProviderType(input.Type)
	}
	if input.UserIDField == "" {
		input.UserIDField = defaultUserIDFieldForProviderType(input.Type)
	}
	if input.UserNameField == "" {
		input.UserNameField = defaultUserNameFieldForProviderType(input.Type)
	}
	if input.EmailField == "" {
		input.EmailField = defaultEmailFieldForProviderType(input.Type)
	}
	if input.Type == "oidc" && input.Issuer != "" {
		if input.AuthorizeURL == "" {
			input.AuthorizeURL = strings.TrimRight(input.Issuer, "/") + "/auth"
		}
	}
	return input
}

func normalizeLoginSyncMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "replace_external":
		return "replace_external"
	default:
		return "append"
	}
}

func normalizeLoginProviderType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "oidc":
		return "oidc"
	case "oauth2", "feishu", "dingtalk", "wecom":
		return strings.ToLower(strings.TrimSpace(value))
	case "saml", "smal":
		return "saml"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func validateLoginProvider(input domainsettings.LoginProviderSettings) error {
	if input.ID == "" {
		return fmt.Errorf("%w: login provider id is required", apperrors.ErrInvalidArgument)
	}
	if input.Name == "" {
		return fmt.Errorf("%w: login provider name is required", apperrors.ErrInvalidArgument)
	}
	switch input.Type {
	case "oidc":
		if input.Enabled {
			switch {
			case input.Issuer == "":
				return fmt.Errorf("%w: oidc issuer is required", apperrors.ErrInvalidArgument)
			case input.ClientID == "":
				return fmt.Errorf("%w: oidc client id is required", apperrors.ErrInvalidArgument)
			case input.ClientSecret == "":
				return fmt.Errorf("%w: oidc client secret is required", apperrors.ErrInvalidArgument)
			case input.RedirectURL == "":
				return fmt.Errorf("%w: oidc redirect url is required", apperrors.ErrInvalidArgument)
			case input.FrontendRedirectURL == "":
				return fmt.Errorf("%w: oidc frontend redirect url is required", apperrors.ErrInvalidArgument)
			}
		}
	case "oauth2", "feishu", "dingtalk", "wecom":
		if input.Enabled {
			switch {
			case input.AuthorizeURL == "":
				return fmt.Errorf("%w: login authorize url is required", apperrors.ErrInvalidArgument)
			case input.TokenURL == "":
				return fmt.Errorf("%w: login token url is required", apperrors.ErrInvalidArgument)
			case input.ClientID == "":
				return fmt.Errorf("%w: login client id is required", apperrors.ErrInvalidArgument)
			case input.ClientSecret == "":
				return fmt.Errorf("%w: login client secret is required", apperrors.ErrInvalidArgument)
			case input.RedirectURL == "":
				return fmt.Errorf("%w: login redirect url is required", apperrors.ErrInvalidArgument)
			case input.FrontendRedirectURL == "":
				return fmt.Errorf("%w: login frontend redirect url is required", apperrors.ErrInvalidArgument)
			}
		}
	case "saml":
		if input.Enabled {
			switch {
			case input.MetadataURL == "" && input.Certificate == "":
				return fmt.Errorf("%w: saml metadata url or certificate is required", apperrors.ErrInvalidArgument)
			case input.RedirectURL == "":
				return fmt.Errorf("%w: saml acs url is required", apperrors.ErrInvalidArgument)
			case input.FrontendRedirectURL == "":
				return fmt.Errorf("%w: saml frontend redirect url is required", apperrors.ErrInvalidArgument)
			}
		}
	default:
		return fmt.Errorf("%w: unsupported login provider type %s", apperrors.ErrInvalidArgument, input.Type)
	}
	return nil
}

func upsertLoginProvider(items []domainsettings.LoginProviderSettings, provider domainsettings.LoginProviderSettings) []domainsettings.LoginProviderSettings {
	out := append([]domainsettings.LoginProviderSettings(nil), items...)
	for index := range out {
		if out[index].ID == provider.ID {
			out[index] = provider
			return out
		}
	}
	return append(out, provider)
}

func hasLegacyOIDCConfig(input domainsettings.OIDCSettings) bool {
	return input.Enabled || input.Issuer != "" || input.ClientID != "" || input.ClientSecret != "" || input.RedirectURL != "" || input.FrontendRedirectURL != ""
}

func loginProviderFromOIDC(input domainsettings.OIDCSettings) domainsettings.LoginProviderSettings {
	id := strings.TrimSpace(input.ProviderName)
	if id == "" {
		id = "oidc-default"
	}
	return normalizeLoginProvider(domainsettings.LoginProviderSettings{
		ID:                  id,
		Name:                firstNonEmptyTrimmed(input.ProviderName, "OIDC"),
		Type:                "oidc",
		Enabled:             input.Enabled,
		ClientID:            input.ClientID,
		ClientSecret:        input.ClientSecret,
		Issuer:              input.Issuer,
		RedirectURL:         input.RedirectURL,
		FrontendRedirectURL: input.FrontendRedirectURL,
		Scopes:              input.Scopes,
		DefaultRoles:        input.DefaultRoles,
	}, 0)
}

func resolvePreferredOIDCProvider(items []domainsettings.LoginProviderSettings, defaultProviderID string) (domainsettings.LoginProviderSettings, bool) {
	targetID := strings.TrimSpace(defaultProviderID)
	if targetID != "" {
		for _, item := range items {
			if item.ID == targetID && item.Type == "oidc" {
				return item, true
			}
		}
	}
	for _, item := range items {
		if item.Type == "oidc" && item.Enabled {
			return item, true
		}
	}
	for _, item := range items {
		if item.Type == "oidc" {
			return item, true
		}
	}
	return domainsettings.LoginProviderSettings{}, false
}

func oidcConfigFromProvider(item domainsettings.LoginProviderSettings) cfgpkg.OIDCConfig {
	return cfgpkg.OIDCConfig{
		Enabled:             item.Enabled,
		ProviderName:        item.Name,
		Issuer:              item.Issuer,
		ClientID:            item.ClientID,
		ClientSecret:        item.ClientSecret,
		RedirectURL:         item.RedirectURL,
		FrontendRedirectURL: item.FrontendRedirectURL,
		Scopes:              append([]string(nil), item.Scopes...),
		DefaultRoles:        append([]string(nil), item.DefaultRoles...),
	}
}

func defaultScopesForProviderType(providerType string) []string {
	switch providerType {
	case "oidc":
		return []string{"openid", "profile", "email"}
	case "feishu":
		return []string{"contact:user.base:readonly"}
	case "dingtalk":
		return []string{"openid"}
	case "wecom":
		return []string{"snsapi_base"}
	default:
		return []string{}
	}
}

func defaultUserIDFieldForProviderType(providerType string) string {
	switch providerType {
	case "feishu":
		return "open_id"
	case "dingtalk":
		return "unionId"
	case "wecom":
		return "UserId"
	default:
		return "sub"
	}
}

func defaultUserNameFieldForProviderType(providerType string) string {
	switch providerType {
	case "feishu":
		return "name"
	case "dingtalk":
		return "nick"
	case "wecom":
		return "UserId"
	default:
		return "name"
	}
}

func defaultEmailFieldForProviderType(providerType string) string {
	switch providerType {
	case "feishu":
		return "enterprise_email"
	case "dingtalk":
		return "email"
	case "wecom":
		return "email"
	default:
		return "email"
	}
}

func (s *Service) fetchProviderModels(ctx context.Context, provider domainsettings.AIProviderSettings) ([]string, error) {
	switch provider.ProviderKind {
	case "anthropic":
		return s.fetchAnthropicModels(ctx, provider)
	case "gemini":
		return s.fetchGeminiModels(ctx, provider)
	default:
		return s.fetchOpenAICompatibleModels(ctx, provider)
	}
}

func (s *Service) fetchOpenAICompatibleModels(ctx context.Context, provider domainsettings.AIProviderSettings) ([]string, error) {
	endpoint := strings.TrimRight(provider.BaseURL, "/")
	if strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint = strings.TrimSuffix(endpoint, "/chat/completions")
	}
	if !strings.HasSuffix(endpoint, "/models") {
		endpoint += "/models"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) != "" {
			models = append(models, strings.TrimSpace(item.ID))
		}
	}
	if len(models) == 0 && provider.Model != "" {
		models = append(models, provider.Model)
	}
	return models, nil
}

func (s *Service) fetchAnthropicModels(ctx context.Context, provider domainsettings.AIProviderSettings) ([]string, error) {
	endpoint := strings.TrimRight(provider.BaseURL, "/")
	if strings.HasSuffix(endpoint, "/messages") {
		endpoint = strings.TrimSuffix(endpoint, "/messages")
	}
	if !strings.HasSuffix(endpoint, "/models") {
		endpoint += "/models"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", provider.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) != "" {
			models = append(models, strings.TrimSpace(item.ID))
		}
	}
	if len(models) == 0 && provider.Model != "" {
		models = append(models, provider.Model)
	}
	return models, nil
}

func (s *Service) fetchGeminiModels(ctx context.Context, provider domainsettings.AIProviderSettings) ([]string, error) {
	endpoint := strings.TrimRight(provider.BaseURL, "/")
	if strings.HasSuffix(endpoint, "/v1beta") || strings.HasSuffix(endpoint, "/v1beta/") {
		endpoint += "/models"
	} else if !strings.Contains(endpoint, "/models") {
		endpoint += "/models"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	query := req.URL.Query()
	query.Set("key", provider.APIKey)
	req.URL.RawQuery = query.Encode()
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(payload.Models))
	for _, item := range payload.Models {
		name := strings.TrimPrefix(strings.TrimSpace(item.Name), "models/")
		if name != "" {
			models = append(models, name)
		}
	}
	if len(models) == 0 && provider.Model != "" {
		models = append(models, provider.Model)
	}
	return models, nil
}

func (s *Service) providerHello(ctx context.Context, provider domainsettings.AIProviderSettings, prompt string) (string, error) {
	if prompt == "" {
		prompt = "hello"
	}
	switch provider.ProviderKind {
	case "anthropic":
		return s.anthropicHello(ctx, provider, prompt)
	case "gemini":
		return s.geminiHello(ctx, provider, prompt)
	default:
		return s.openAICompatibleHello(ctx, provider, prompt)
	}
}

func (s *Service) openAICompatibleHello(ctx context.Context, provider domainsettings.AIProviderSettings, prompt string) (string, error) {
	endpoint := strings.TrimRight(provider.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}
	payload := map[string]any{
		"model": provider.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return openAICompatibleReplyFromBody(raw)
}

func openAICompatibleReplyFromBody(raw []byte) (string, error) {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return "", nil
	}
	if strings.HasPrefix(body, "data:") {
		var builder strings.Builder
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			reply, err := openAICompatibleReplyPartFromJSON([]byte(payload))
			if err != nil {
				return "", err
			}
			if reply != "" {
				builder.WriteString(reply)
			}
		}
		return strings.TrimSpace(builder.String()), nil
	}
	return openAICompatibleReplyFromJSON([]byte(body))
}

func openAICompatibleReplyFromJSON(raw []byte) (string, error) {
	reply, err := openAICompatibleReplyPartFromJSON(raw)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply), nil
}

func openAICompatibleReplyPartFromJSON(raw []byte) (string, error) {
	var body struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", err
	}
	for _, choice := range body.Choices {
		switch {
		case strings.TrimSpace(choice.Message.Content) != "":
			return choice.Message.Content, nil
		case strings.TrimSpace(choice.Delta.Content) != "":
			return choice.Delta.Content, nil
		case strings.TrimSpace(choice.Text) != "":
			return choice.Text, nil
		}
	}
	return "", nil
}

func (s *Service) anthropicHello(ctx context.Context, provider domainsettings.AIProviderSettings, prompt string) (string, error) {
	endpoint := strings.TrimRight(provider.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/messages") {
		endpoint += "/messages"
	}
	payload := map[string]any{
		"model":      provider.Model,
		"max_tokens": 64,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", provider.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var body struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if len(body.Content) == 0 {
		return "", nil
	}
	return strings.TrimSpace(body.Content[0].Text), nil
}

func (s *Service) geminiHello(ctx context.Context, provider domainsettings.AIProviderSettings, prompt string) (string, error) {
	endpoint := strings.TrimRight(provider.BaseURL, "/")
	if !strings.Contains(endpoint, ":generateContent") {
		base := strings.TrimRight(endpoint, "/")
		if !strings.Contains(base, "/models/") {
			base = strings.TrimRight(base, "/") + "/models/" + url.PathEscape(provider.Model)
		}
		endpoint = base + ":generateContent"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(fmt.Sprintf(`{"contents":[{"parts":[{"text":%q}]}]}`, prompt)))
	if err != nil {
		return "", err
	}
	query := req.URL.Query()
	query.Set("key", provider.APIKey)
	req.URL.RawQuery = query.Encode()
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var body struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if len(body.Candidates) == 0 || len(body.Candidates[0].Content.Parts) == 0 {
		return "", nil
	}
	return strings.TrimSpace(body.Candidates[0].Content.Parts[0].Text), nil
}

func (s *Service) brandingSettings(ctx context.Context) (domainsettings.BrandingSettings, error) {
	item := domainsettings.BrandingSettings{
		AppTitle:     "Soha",
		SidebarTitle: "Soha",
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

func settingStringValue(value any) string {
	current := strings.TrimSpace(fmt.Sprint(value))
	if current == "<nil>" {
		return ""
	}
	return current
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
