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
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
}
