package settings

import (
	"context"
	"fmt"
	"slices"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/appconfig"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Service struct {
	store       domainsettings.Store
	monitoring  appconfig.Monitoring
	permissions *appaccess.PermissionResolver
}

func New(store domainsettings.Store, monitoring appconfig.Monitoring, permissions *appaccess.PermissionResolver) *Service {
	return &Service{store: store, monitoring: monitoring, permissions: permissions}
}

func (s *Service) GetIdentitySettings(ctx context.Context, principal domainidentity.Principal) (domainsettings.IdentitySettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsIdentityView); err != nil {
		return domainsettings.IdentitySettings{}, err
	}
	return s.identitySettings(ctx)
}

func (s *Service) UpdateLoginProvidersSettings(ctx context.Context, principal domainidentity.Principal, providers []domainsettings.LoginProviderSettings, defaultProviderID string, localPasswordEnabled bool) (domainsettings.IdentitySettings, error) {
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
	if !localPasswordEnabled && !slices.ContainsFunc(normalized, func(item domainsettings.LoginProviderSettings) bool { return item.Enabled }) {
		return domainsettings.IdentitySettings{}, fmt.Errorf("%w: local password login cannot be disabled without an enabled external provider", apperrors.ErrInvalidArgument)
	}
	if err := s.persistLoginProvidersSettings(ctx, principal.UserID, normalized, defaultProviderID, localPasswordEnabled); err != nil {
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

func (s *Service) UpdateAIWorkbenchModelSettings(ctx context.Context, principal domainidentity.Principal, input domainsettings.AIWorkbenchModelSettings) (domainsettings.AISettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domainsettings.AISettings{}, err
	}
	current, err := s.aiSettings(ctx)
	if err != nil {
		return domainsettings.AISettings{}, err
	}
	current.WorkbenchModel = normalizeAIWorkbenchModel(input)
	return s.persistAISettings(ctx, principal.UserID, current.WorkbenchModel, skillsToMaps(current.SkillsRegistry))
}

func (s *Service) UpdateAISkillsRegistry(ctx context.Context, principal domainidentity.Principal, skills []domainsettings.AISkillSettings) (domainsettings.AISettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domainsettings.AISettings{}, err
	}
	current, err := s.aiSettings(ctx)
	if err != nil {
		return domainsettings.AISettings{}, err
	}
	return s.persistAISettings(ctx, principal.UserID, current.WorkbenchModel, skillsToMaps(skills))
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

func (s *Service) ResolveMonitoringSettings(ctx context.Context) (domainsettings.MonitoringSettings, error) {
	return s.monitoringSettings(ctx)
}

func (s *Service) ResolveAISettings(ctx context.Context) (domainsettings.AISettings, error) {
	return s.aiSettings(ctx)
}

func (s *Service) ResolveAIWorkbenchSettings(ctx context.Context) (domainsettings.AIWorkbenchModelSettings, error) {
	settings, err := s.aiSettings(ctx)
	if err != nil {
		return domainsettings.AIWorkbenchModelSettings{}, err
	}
	return settings.WorkbenchModel, nil
}

func (s *Service) ResolveAISkillsRegistry(ctx context.Context) ([]domainsettings.AISkillSettings, error) {
	settings, err := s.aiSettings(ctx)
	if err != nil {
		return nil, err
	}
	return append([]domainsettings.AISkillSettings(nil), settings.SkillsRegistry...), nil
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

func (s *Service) LocalPasswordLoginEnabled(ctx context.Context) (bool, error) {
	settings, err := s.identitySettings(ctx)
	return settings.LocalPasswordLoginEnabled, err
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
	item := domainsettings.IdentitySettings{LocalPasswordLoginEnabled: true}
	if s.store == nil {
		return item, nil
	}
	rawProviders, ok, err := s.store.Get(ctx, domainsettings.IdentityLoginProvidersSettingKey)
	if err != nil {
		return item, err
	}
	if ok {
		if value, exists := rawProviders["localPasswordLoginEnabled"].(bool); exists {
			item.LocalPasswordLoginEnabled = value
		}
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
					IconURL:             settingStringValue(record["iconUrl"]),
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
					PhoneField:          settingStringValue(record["phoneField"]),
					AvatarField:         settingStringValue(record["avatarField"]),
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
		WorkbenchModel: domainsettings.AIWorkbenchModelSettings{
			DefaultEndpoint: "chat/completions",
			Enabled:         true,
		},
	}
	if s.store == nil {
		return item, nil
	}
	raw, ok, err := s.store.Get(ctx, domainsettings.AISettingsKey)
	if err != nil || !ok {
		return item, err
	}
	if values, ok := raw["workbenchModel"].(map[string]any); ok {
		if value, ok := values["defaultPublicModel"].(string); ok {
			item.WorkbenchModel.DefaultPublicModel = strings.TrimSpace(value)
		}
		if value, ok := values["defaultRouteId"].(string); ok {
			item.WorkbenchModel.DefaultRouteID = strings.TrimSpace(value)
		}
		if value, ok := values["defaultEndpoint"].(string); ok {
			item.WorkbenchModel.DefaultEndpoint = strings.TrimSpace(value)
		}
		if value, ok := values["enabled"].(bool); ok {
			item.WorkbenchModel.Enabled = value
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
	item.WorkbenchModel = normalizeAIWorkbenchModel(item.WorkbenchModel)
	return item, nil
}

func normalizeAIWorkbenchModel(input domainsettings.AIWorkbenchModelSettings) domainsettings.AIWorkbenchModelSettings {
	input.DefaultPublicModel = strings.TrimSpace(input.DefaultPublicModel)
	input.DefaultRouteID = strings.TrimSpace(input.DefaultRouteID)
	input.DefaultEndpoint = strings.TrimSpace(input.DefaultEndpoint)
	if input.DefaultEndpoint == "" {
		input.DefaultEndpoint = "chat/completions"
	}
	return input
}

func skillsToMaps(items []domainsettings.AISkillSettings) []map[string]any {
	skills := make([]map[string]any, 0, len(items))
	for _, item := range items {
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
	return skills
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
	switch values := raw.(type) {
	case []string:
		return uniqueNonEmptyStrings(values)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			item := strings.TrimSpace(fmt.Sprint(value))
			if item == "" {
				continue
			}
			out = append(out, item)
		}
		return out
	default:
		return []string{}
	}
}

func loginProvidersToMaps(items []domainsettings.LoginProviderSettings) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":                  item.ID,
			"name":                item.Name,
			"type":                item.Type,
			"iconUrl":             item.IconURL,
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
			"phoneField":          item.PhoneField,
			"avatarField":         item.AvatarField,
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

func (s *Service) persistLoginProvidersSettings(ctx context.Context, updatedBy string, providers []domainsettings.LoginProviderSettings, defaultProviderID string, localPasswordEnabled bool) error {
	value := map[string]any{
		"defaultProviderId":         strings.TrimSpace(defaultProviderID),
		"providers":                 loginProvidersToMaps(providers),
		"localPasswordLoginEnabled": localPasswordEnabled,
	}
	return s.store.Upsert(ctx, domainsettings.IdentityLoginProvidersSettingKey, "identity", value, updatedBy)
}

func (s *Service) persistAISettings(ctx context.Context, updatedBy string, workbenchModel domainsettings.AIWorkbenchModelSettings, skills []map[string]any) (domainsettings.AISettings, error) {
	workbenchModel = normalizeAIWorkbenchModel(workbenchModel)
	value := map[string]any{
		"workbenchModel": map[string]any{
			"defaultPublicModel": workbenchModel.DefaultPublicModel,
			"defaultRouteId":     workbenchModel.DefaultRouteID,
			"defaultEndpoint":    workbenchModel.DefaultEndpoint,
			"enabled":            workbenchModel.Enabled,
		},
		"skillsRegistry": skills,
	}
	if err := s.store.Upsert(ctx, domainsettings.AISettingsKey, "ai", value, updatedBy); err != nil {
		return domainsettings.AISettings{}, err
	}
	return s.aiSettings(ctx)
}

func normalizeLoginProvider(input domainsettings.LoginProviderSettings, index int) domainsettings.LoginProviderSettings {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Type = normalizeLoginProviderType(input.Type)
	input.IconURL = strings.TrimSpace(input.IconURL)
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
	input.PhoneField = strings.TrimSpace(input.PhoneField)
	input.AvatarField = strings.TrimSpace(input.AvatarField)
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
	if !input.Enabled {
		return validateLoginProviderType(input.Type)
	}
	switch input.Type {
	case "oidc":
		return validateOIDCLoginProvider(input)
	case "oauth2", "feishu", "dingtalk", "wecom":
		return validateOAuth2LoginProvider(input)
	case "saml":
		return validateSAMLLoginProvider(input)
	default:
		return unsupportedLoginProviderType(input.Type)
	}
}

func validateLoginProviderType(providerType string) error {
	switch providerType {
	case "oidc", "oauth2", "feishu", "dingtalk", "wecom", "saml":
		return nil
	default:
		return unsupportedLoginProviderType(providerType)
	}
}

func validateOIDCLoginProvider(input domainsettings.LoginProviderSettings) error {
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
	default:
		return nil
	}
}

func validateOAuth2LoginProvider(input domainsettings.LoginProviderSettings) error {
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
	default:
		return nil
	}
}

func validateSAMLLoginProvider(input domainsettings.LoginProviderSettings) error {
	switch {
	case input.MetadataURL == "" && input.Certificate == "":
		return fmt.Errorf("%w: saml metadata url or certificate is required", apperrors.ErrInvalidArgument)
	case input.RedirectURL == "":
		return fmt.Errorf("%w: saml acs url is required", apperrors.ErrInvalidArgument)
	case input.FrontendRedirectURL == "":
		return fmt.Errorf("%w: saml frontend redirect url is required", apperrors.ErrInvalidArgument)
	default:
		return nil
	}
}

func unsupportedLoginProviderType(providerType string) error {
	return fmt.Errorf("%w: unsupported login provider type %s", apperrors.ErrInvalidArgument, providerType)
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

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}
