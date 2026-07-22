package settings

import "context"

const (
	IdentityLoginProvidersSettingKey = "identity.login_providers"
	AISettingsKey                    = "ai.workbench"
	BrandingSettingKey               = "branding.console"
)

type LoginProviderSettings struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Type                string   `json:"type"`
	IconURL             string   `json:"iconUrl,omitempty"`
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
	PhoneField          string   `json:"phoneField,omitempty"`
	AvatarField         string   `json:"avatarField,omitempty"`
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
	Providers                 []LoginProviderSettings `json:"providers,omitempty"`
	DefaultProviderID         string                  `json:"defaultProviderId,omitempty"`
	LocalPasswordLoginEnabled bool                    `json:"localPasswordLoginEnabled"`
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

type AIWorkbenchModelSettings struct {
	DefaultPublicModel string `json:"defaultPublicModel,omitempty"`
	DefaultRouteID     string `json:"defaultRouteId,omitempty"`
	DefaultEndpoint    string `json:"defaultEndpoint,omitempty"`
	Enabled            bool   `json:"enabled"`
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
	WorkbenchModel AIWorkbenchModelSettings `json:"workbenchModel,omitempty"`
	SkillsRegistry []SkillDefinition        `json:"skillsRegistry,omitempty"`
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
