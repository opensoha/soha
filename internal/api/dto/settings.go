package dto

type UpdateOIDCSettingsRequest struct {
	Enabled             bool     `json:"enabled"`
	ProviderName        string   `json:"providerName"`
	Issuer              string   `json:"issuer"`
	ClientID            string   `json:"clientId"`
	ClientSecret        string   `json:"clientSecret"`
	RedirectURL         string   `json:"redirectUrl"`
	FrontendRedirectURL string   `json:"frontendRedirectUrl"`
	Scopes              []string `json:"scopes"`
	DefaultRoles        []string `json:"defaultRoles"`
}

type UpdateLoginProvidersSettingsRequest struct {
	DefaultProviderID string                  `json:"defaultProviderId"`
	Providers         []LoginProviderSettings `json:"providers"`
}

type LoginProviderSettings struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Type                string   `json:"type"`
	Enabled             bool     `json:"enabled"`
	ClientID            string   `json:"clientId"`
	ClientSecret        string   `json:"clientSecret"`
	Issuer              string   `json:"issuer"`
	AuthorizeURL        string   `json:"authorizeUrl"`
	TokenURL            string   `json:"tokenUrl"`
	UserInfoURL         string   `json:"userInfoUrl"`
	ProfileURL          string   `json:"profileUrl"`
	RedirectURL         string   `json:"redirectUrl"`
	FrontendRedirectURL string   `json:"frontendRedirectUrl"`
	Scopes              []string `json:"scopes"`
	DefaultRoles        []string `json:"defaultRoles"`
	UserIDField         string   `json:"userIdField"`
	UserNameField       string   `json:"userNameField"`
	EmailField          string   `json:"emailField"`
	RoleField           string   `json:"roleField"`
	OrganizationField   string   `json:"organizationField"`
	SyncRolesOnLogin    bool     `json:"syncRolesOnLogin"`
	SyncOrgsOnLogin     bool     `json:"syncOrgsOnLogin"`
	RoleSyncMode        string   `json:"roleSyncMode"`
	OrgSyncMode         string   `json:"orgSyncMode"`
	MetadataURL         string   `json:"metadataUrl"`
	EntityID            string   `json:"entityId"`
	Certificate         string   `json:"certificate"`
}

type UpdatePrometheusSettingsRequest struct {
	Enabled             bool   `json:"enabled"`
	BaseURL             string `json:"baseUrl"`
	BearerToken         string `json:"bearerToken"`
	DefaultRangeMinutes int    `json:"defaultRangeMinutes"`
	StepSeconds         int    `json:"stepSeconds"`
	ClusterLabel        string `json:"clusterLabel"`
	GrafanaBaseURL      string `json:"grafanaBaseUrl"`
}

type UpdateAIWorkbenchModelRequest struct {
	WorkbenchModel AIWorkbenchModelSettings `json:"workbenchModel"`
}

type UpdateAISkillsRequest struct {
	SkillsRegistry []AISkillSettings `json:"skillsRegistry"`
}

type AIWorkbenchModelSettings struct {
	DefaultPublicModel string `json:"defaultPublicModel"`
	DefaultRouteID     string `json:"defaultRouteId"`
	DefaultEndpoint    string `json:"defaultEndpoint"`
	Enabled            bool   `json:"enabled"`
}

type AISkillSettings struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Category       string         `json:"category"`
	OwnerModule    string         `json:"ownerModule"`
	Description    string         `json:"description"`
	CapabilityRefs []string       `json:"capabilityRefs"`
	BlueprintRefs  []string       `json:"blueprintRefs"`
	InputSchema    map[string]any `json:"inputSchema"`
	OutputSchema   map[string]any `json:"outputSchema"`
	ScopeRules     []string       `json:"scopeRules"`
	Enabled        bool           `json:"enabled"`
	Scopes         []string       `json:"scopes"`
}

type UpdateBrandingSettingsRequest struct {
	AppTitle         string `json:"appTitle"`
	SidebarTitle     string `json:"sidebarTitle"`
	LoginLogoURL     string `json:"loginLogoUrl"`
	ExpandedLogoURL  string `json:"expandedLogoUrl"`
	CollapsedLogoURL string `json:"collapsedLogoUrl"`
	FaviconURL       string `json:"faviconUrl"`
}
