package appconfig

import (
	"fmt"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/platform/keyring"
)

// Auth contains the authentication settings consumed by the identity application service.
type Auth struct {
	EnableDevAuth     bool              `mapstructure:"enable_dev_auth"`
	LoginVerification LoginVerification `mapstructure:"login_verification"`
	DevPrincipal      DevPrincipal      `mapstructure:"dev_principal"`
	JWT               JWT               `mapstructure:"jwt"`
	OIDC              OIDC              `mapstructure:"oidc"`
}

type LoginVerification struct {
	SliderEnabled bool `mapstructure:"slider_enabled"`
}

type DevPrincipal struct {
	UserID   string   `mapstructure:"user_id"`
	Name     string   `mapstructure:"name"`
	Email    string   `mapstructure:"email"`
	Password string   `mapstructure:"password"`
	Roles    []string `mapstructure:"roles"`
}

type JWT struct {
	Secret     string        `mapstructure:"secret"`
	Keys       keyring.Ring  `mapstructure:"-"`
	Issuer     string        `mapstructure:"issuer"`
	AccessTTL  time.Duration `mapstructure:"access_ttl"`
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
}

type OIDC struct {
	Enabled             bool     `mapstructure:"enabled"`
	ProviderName        string   `mapstructure:"provider_name"`
	Issuer              string   `mapstructure:"issuer"`
	ClientID            string   `mapstructure:"client_id"`
	ClientSecret        string   `mapstructure:"client_secret"`
	RedirectURL         string   `mapstructure:"redirect_url"`
	FrontendRedirectURL string   `mapstructure:"frontend_redirect_url"`
	Scopes              []string `mapstructure:"scopes"`
	DefaultRoles        []string `mapstructure:"default_roles"`
}

type Monitoring struct {
	Enabled                       bool         `mapstructure:"enabled"`
	WebhookToken                  string       `mapstructure:"webhook_token"`
	WebhookKeys                   keyring.Ring `mapstructure:"-"`
	PrometheusURL                 string       `mapstructure:"prometheus_url"`
	PrometheusBearerToken         string       `mapstructure:"prometheus_bearer_token"`
	PrometheusDefaultRangeMinutes int          `mapstructure:"prometheus_default_range_minutes"`
	PrometheusStepSeconds         int          `mapstructure:"prometheus_step_seconds"`
	PrometheusClusterLabel        string       `mapstructure:"prometheus_cluster_label"`
	GrafanaBaseURL                string       `mapstructure:"grafana_base_url"`
}

type ModuleToggle struct {
	Enabled  bool           `mapstructure:"enabled"`
	Features map[string]any `mapstructure:"features"`
}

func (m ModuleToggle) FeatureFlags() map[string]bool {
	out := map[string]bool{}
	var flatten func(string, map[string]any)
	flatten = func(prefix string, values map[string]any) {
		for key, value := range values {
			name := strings.Trim(strings.TrimSpace(prefix+"."+key), ".")
			switch typed := value.(type) {
			case bool:
				out[name] = typed
			case map[string]any:
				flatten(name, typed)
			case map[any]any:
				nested := make(map[string]any, len(typed))
				for nestedKey, nestedValue := range typed {
					nested[fmt.Sprint(nestedKey)] = nestedValue
				}
				flatten(name, nested)
			}
		}
	}
	flatten("", m.Features)
	return out
}

type Modules struct {
	Home           ModuleToggle `mapstructure:"home"`
	Delivery       ModuleToggle `mapstructure:"delivery"`
	Monitoring     ModuleToggle `mapstructure:"monitoring"`
	AI             ModuleToggle `mapstructure:"ai"`
	AIGateway      ModuleToggle `mapstructure:"ai_gateway"`
	Virtualization ModuleToggle `mapstructure:"virtualization"`
	Docker         ModuleToggle `mapstructure:"docker"`
	Security       ModuleToggle `mapstructure:"security"`
	CMDB           ModuleToggle `mapstructure:"cmdb"`
}

type Cluster struct {
	ID                     string            `mapstructure:"id"`
	Name                   string            `mapstructure:"name"`
	Kubeconfig             string            `mapstructure:"kubeconfig"`
	KubeconfigData         string            `mapstructure:"kubeconfig_data"`
	Context                string            `mapstructure:"context"`
	Region                 string            `mapstructure:"region"`
	Environment            string            `mapstructure:"environment"`
	Labels                 map[string]string `mapstructure:"labels"`
	PrometheusURL          string            `mapstructure:"prometheus_url"`
	PrometheusBearerToken  string            `mapstructure:"prometheus_bearer_token"`
	PrometheusClusterLabel string            `mapstructure:"prometheus_cluster_label"`
	GrafanaBaseURL         string            `mapstructure:"grafana_base_url"`
}
