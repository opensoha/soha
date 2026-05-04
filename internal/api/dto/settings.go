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

type UpdatePrometheusSettingsRequest struct {
	Enabled             bool   `json:"enabled"`
	BaseURL             string `json:"baseUrl"`
	BearerToken         string `json:"bearerToken"`
	DefaultRangeMinutes int    `json:"defaultRangeMinutes"`
	StepSeconds         int    `json:"stepSeconds"`
	ClusterLabel        string `json:"clusterLabel"`
	GrafanaBaseURL      string `json:"grafanaBaseUrl"`
}

type UpdateAISettingsRequest struct {
	Enabled        bool              `json:"enabled"`
	BaseURL        string            `json:"baseUrl"`
	APIKey         string            `json:"apiKey"`
	Model          string            `json:"model"`
	SkillsRegistry []AISkillSettings `json:"skillsRegistry"`
}

type AISkillSettings struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Scopes      []string `json:"scopes"`
}

type UpdateBrandingSettingsRequest struct {
	AppTitle         string `json:"appTitle"`
	SidebarTitle     string `json:"sidebarTitle"`
	LoginLogoURL     string `json:"loginLogoUrl"`
	ExpandedLogoURL  string `json:"expandedLogoUrl"`
	CollapsedLogoURL string `json:"collapsedLogoUrl"`
	FaviconURL       string `json:"faviconUrl"`
}
