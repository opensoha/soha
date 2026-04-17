package settings

import "context"

const (
	IdentityOIDCSettingKey         = "identity.oidc"
	MonitoringPrometheusSettingKey = "monitoring.prometheus"
	AIProviderSettingKey           = "ai.provider"
	BrandingSettingKey             = "branding.console"
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

type IdentitySettings struct {
	OIDC OIDCSettings `json:"oidc"`
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
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
}

type AISettings struct {
	Provider AIProviderSettings `json:"provider"`
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
