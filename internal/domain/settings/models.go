package settings

import "context"

const (
	IdentityOIDCSettingKey           = "identity.oidc"
	IdentityLoginProvidersSettingKey = "identity.login_providers"
	MonitoringPrometheusSettingKey   = "monitoring.prometheus"
	AIProviderSettingKey             = "ai.provider"
	BrandingSettingKey               = "branding.console"
)

type OIDCSettings struct {
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

type LoginProviderSettings struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Type                string   `json:"type"`
	Enabled             bool     `json:"enabled"`
	ClientID            string   `json:"clientId,omitempty"`
	ClientSecret        string   `json:"clientSecret,omitempty"`
	Issuer              string   `json:"issuer,omitempty"`
	AuthorizeURL        string   `json:"authorizeUrl,omitempty"`
	TokenURL            string   `json:"tokenUrl,omitempty"`
	UserInfoURL         string   `json:"userInfoUrl,omitempty"`
	ProfileURL          string   `json:"profileUrl,omitempty"`
	RedirectURL         string   `json:"redirectUrl,omitempty"`
	FrontendRedirectURL string   `json:"frontendRedirectUrl,omitempty"`
	Scopes              []string `json:"scopes,omitempty"`
	DefaultRoles        []string `json:"defaultRoles,omitempty"`
	UserIDField         string   `json:"userIdField,omitempty"`
	UserNameField       string   `json:"userNameField,omitempty"`
	EmailField          string   `json:"emailField,omitempty"`
	RoleField           string   `json:"roleField,omitempty"`
	OrganizationField   string   `json:"organizationField,omitempty"`
	SyncRolesOnLogin    bool     `json:"syncRolesOnLogin,omitempty"`
	SyncOrgsOnLogin     bool     `json:"syncOrgsOnLogin,omitempty"`
	RoleSyncMode        string   `json:"roleSyncMode,omitempty"`
	OrgSyncMode         string   `json:"orgSyncMode,omitempty"`
	MetadataURL         string   `json:"metadataUrl,omitempty"`
	EntityID            string   `json:"entityId,omitempty"`
	Certificate         string   `json:"certificate,omitempty"`
}

type IdentitySettings struct {
	OIDC              OIDCSettings            `json:"oidc"`
	Providers         []LoginProviderSettings `json:"providers,omitempty"`
	DefaultProviderID string                  `json:"defaultProviderId,omitempty"`
}

type PrometheusSettings struct {
	Enabled             bool   `json:"enabled"`
	BaseURL             string `json:"baseUrl"`
	BearerToken         string `json:"bearerToken"`
	DefaultRangeMinutes int    `json:"defaultRangeMinutes"`
	StepSeconds         int    `json:"stepSeconds"`
	ClusterLabel        string `json:"clusterLabel"`
	GrafanaBaseURL      string `json:"grafanaBaseUrl"`
}

type MonitoringSettings struct {
	Prometheus PrometheusSettings `json:"prometheus"`
}

type AIProviderSettings struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	ProviderKind string `json:"providerKind,omitempty"`
	Enabled      bool   `json:"enabled"`
	BaseURL      string `json:"baseUrl"`
	APIKey       string `json:"apiKey"`
	Model        string `json:"model"`
}

type AIProviderTestResult struct {
	OK      bool   `json:"ok"`
	Model   string `json:"model,omitempty"`
	Message string `json:"message,omitempty"`
	Reply   string `json:"reply,omitempty"`
}

type SkillDefinition struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Category       string         `json:"category,omitempty"`
	OwnerModule    string         `json:"ownerModule,omitempty"`
	Description    string         `json:"description,omitempty"`
	CapabilityRefs []string       `json:"capabilityRefs,omitempty"`
	BlueprintRefs  []string       `json:"blueprintRefs,omitempty"`
	InputSchema    map[string]any `json:"inputSchema,omitempty"`
	OutputSchema   map[string]any `json:"outputSchema,omitempty"`
	ScopeRules     []string       `json:"scopeRules,omitempty"`
	Enabled        bool           `json:"enabled"`
	Scopes         []string       `json:"scopes,omitempty"`
}

type AISkillSettings = SkillDefinition

type AISettings struct {
	Provider          AIProviderSettings   `json:"provider"`
	Providers         []AIProviderSettings `json:"providers,omitempty"`
	DefaultProviderID string               `json:"defaultProviderId,omitempty"`
	SkillsRegistry    []SkillDefinition    `json:"skillsRegistry,omitempty"`
}

type BrandingSettings struct {
	AppTitle         string `json:"appTitle"`
	SidebarTitle     string `json:"sidebarTitle"`
	LoginLogoURL     string `json:"loginLogoUrl"`
	ExpandedLogoURL  string `json:"expandedLogoUrl"`
	CollapsedLogoURL string `json:"collapsedLogoUrl"`
	FaviconURL       string `json:"faviconUrl"`
}

type Store interface {
	Get(context.Context, string) (map[string]any, bool, error)
	Upsert(context.Context, string, string, map[string]any, string) error
}
