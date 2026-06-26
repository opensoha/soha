package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

type Config struct {
	App        AppConfig        `mapstructure:"app"`
	HTTP       HTTPConfig       `mapstructure:"http"`
	Logger     LoggerConfig     `mapstructure:"logger"`
	Runtime    RuntimeConfig    `mapstructure:"runtime"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Auth       AuthConfig       `mapstructure:"auth"`
	GitLab     GitLabConfig     `mapstructure:"gitlab"`
	Monitoring MonitoringConfig `mapstructure:"monitoring"`
	Swagger    SwaggerConfig    `mapstructure:"swagger"`
	MCP        MCPConfig        `mapstructure:"mcp"`
	AIGateway  AIGatewayConfig  `mapstructure:"ai_gateway"`
	Modules    ModulesConfig    `mapstructure:"modules"`
	Assets     AssetsConfig     `mapstructure:"assets"`
	Security   SecurityConfig   `mapstructure:"security"`
	Bootstrap  BootstrapConfig  `mapstructure:"bootstrap"`
	Kubernetes KubernetesConfig `mapstructure:"kubernetes"`
}

type AppConfig struct {
	Name string `mapstructure:"name"`
	Env  string `mapstructure:"env"`
}

type HTTPConfig struct {
	Addr               string        `mapstructure:"addr"`
	ReadTimeout        time.Duration `mapstructure:"read_timeout"`
	WriteTimeout       time.Duration `mapstructure:"write_timeout"`
	BasePath           string        `mapstructure:"base_path"`
	CORSAllowedOrigins []string      `mapstructure:"cors_allowed_origins"`
}

type LoggerConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type RuntimeConfig struct {
	WorkflowWorkers               int           `mapstructure:"workflow_workers"`
	WorkflowQueueSize             int           `mapstructure:"workflow_queue_size"`
	WorkflowNodeParallelism       int           `mapstructure:"workflow_node_parallelism"`
	ClusterSyncParallelism        int           `mapstructure:"cluster_sync_parallelism"`
	CopilotInspectionParallelism  int           `mapstructure:"copilot_inspection_parallelism"`
	AlertUpsertBatchSize          int           `mapstructure:"alert_upsert_batch_size"`
	VirtualizationStartupSync     bool          `mapstructure:"virtualization_startup_sync_enabled"`
	VirtualizationWorkerInterval  time.Duration `mapstructure:"virtualization_worker_interval"`
	VirtualizationSyncConcurrency int           `mapstructure:"virtualization_sync_concurrency"`
	ExecutionRunnerToken          string        `mapstructure:"execution_runner_token"`
	ExecutionJobClusterID         string        `mapstructure:"execution_job_cluster_id"`
	ExecutionJobNamespace         string        `mapstructure:"execution_job_namespace"`
	ExecutionJobImage             string        `mapstructure:"execution_job_image"`
	ExecutionJobGitImage          string        `mapstructure:"execution_job_git_image"`
	ExecutionJobTTLSeconds        int           `mapstructure:"execution_job_ttl_seconds"`
}

type DatabaseConfig struct {
	Driver          string        `mapstructure:"driver"`
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Name            string        `mapstructure:"name"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	SSLMode         string        `mapstructure:"sslmode"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	AutoMigrate     bool          `mapstructure:"auto_migrate"`
	MigrationPath   string        `mapstructure:"migration_path"`
	MigrationFile   string        `mapstructure:"migration_file"`
}

type AuthConfig struct {
	EnableDevAuth     bool                    `mapstructure:"enable_dev_auth"`
	LoginVerification LoginVerificationConfig `mapstructure:"login_verification"`
	DevPrincipal      DevPrincipalConfig      `mapstructure:"dev_principal"`
	JWT               JWTConfig               `mapstructure:"jwt"`
	OIDC              OIDCConfig              `mapstructure:"oidc"`
}

type LoginVerificationConfig struct {
	SliderEnabled bool `mapstructure:"slider_enabled"`
}

type GitLabConfig struct {
	Enabled bool          `mapstructure:"enabled"`
	BaseURL string        `mapstructure:"base_url"`
	Token   string        `mapstructure:"token"`
	GroupID string        `mapstructure:"group_id"`
	PerPage int           `mapstructure:"per_page"`
	Timeout time.Duration `mapstructure:"timeout"`
}

type DevPrincipalConfig struct {
	UserID   string   `mapstructure:"user_id"`
	Name     string   `mapstructure:"name"`
	Email    string   `mapstructure:"email"`
	Password string   `mapstructure:"password"`
	Roles    []string `mapstructure:"roles"`
}

type JWTConfig struct {
	Secret     string        `mapstructure:"secret"`
	Issuer     string        `mapstructure:"issuer"`
	AccessTTL  time.Duration `mapstructure:"access_ttl"`
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
}

type OIDCConfig struct {
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

type MonitoringConfig struct {
	Enabled                       bool   `mapstructure:"enabled"`
	WebhookToken                  string `mapstructure:"webhook_token"`
	PrometheusURL                 string `mapstructure:"prometheus_url"`
	PrometheusBearerToken         string `mapstructure:"prometheus_bearer_token"`
	PrometheusDefaultRangeMinutes int    `mapstructure:"prometheus_default_range_minutes"`
	PrometheusStepSeconds         int    `mapstructure:"prometheus_step_seconds"`
	PrometheusClusterLabel        string `mapstructure:"prometheus_cluster_label"`
	GrafanaBaseURL                string `mapstructure:"grafana_base_url"`
}

type SwaggerConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}

type MCPConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	DefaultTimeout time.Duration `mapstructure:"default_timeout"`
}

type AIGatewayConfig struct {
	RateLimit          AIGatewayRateLimitConfig          `mapstructure:"rate_limit"`
	Relay              AIGatewayRelayConfig              `mapstructure:"relay"`
	ConnectorRuntime   AIGatewayConnectorRuntimeConfig   `mapstructure:"connector_runtime"`
	ConnectorRuntimes  []AIGatewayConnectorRuntimeConfig `mapstructure:"connector_runtimes"`
	ConnectorEventSink AIGatewayConnectorEventSinkConfig `mapstructure:"connector_event_sink"`
}

type AIGatewayRelayConfig struct {
	Enabled                     bool          `mapstructure:"enabled"`
	DefaultTimeout              time.Duration `mapstructure:"default_timeout"`
	StreamTimeout               time.Duration `mapstructure:"stream_timeout"`
	HealthCheckEnabled          bool          `mapstructure:"health_check_enabled"`
	HealthCheckInterval         time.Duration `mapstructure:"health_check_interval"`
	MaxRequestBodyMB            int           `mapstructure:"max_request_body_mb"`
	AllowInsecureUpstreamHTTP   bool          `mapstructure:"allow_insecure_upstream_http"`
	AllowPrivateUpstreamHosts   bool          `mapstructure:"allow_private_upstream_hosts"`
	IncludeUsageForOpenAIStream bool          `mapstructure:"include_usage_for_openai_stream"`
}

type AIGatewayRateLimitConfig struct {
	Backend string                        `mapstructure:"backend"`
	Redis   AIGatewayRateLimitRedisConfig `mapstructure:"redis"`
}

type AIGatewayRateLimitRedisConfig struct {
	Addr      string        `mapstructure:"addr"`
	Username  string        `mapstructure:"username"`
	Password  string        `mapstructure:"password"`
	DB        int           `mapstructure:"db"`
	TLS       bool          `mapstructure:"tls"`
	KeyPrefix string        `mapstructure:"key_prefix"`
	Timeout   time.Duration `mapstructure:"timeout"`
}

type AIGatewayConnectorRuntimeConfig struct {
	Endpoint    string `mapstructure:"endpoint"`
	Token       string `mapstructure:"token"`
	PluginID    string `mapstructure:"plugin_id"`
	ConnectorID string `mapstructure:"connector_id"`
}

type AIGatewayConnectorEventSinkConfig struct {
	Token string `mapstructure:"token"`
}

func (c AIGatewayConfig) ConnectorRuntimeConfigs() []AIGatewayConnectorRuntimeConfig {
	out := make([]AIGatewayConnectorRuntimeConfig, 0, 1+len(c.ConnectorRuntimes))
	if strings.TrimSpace(c.ConnectorRuntime.Endpoint) != "" {
		out = append(out, c.ConnectorRuntime.normalized())
	}
	for _, runtime := range c.ConnectorRuntimes {
		if strings.TrimSpace(runtime.Endpoint) == "" {
			continue
		}
		out = append(out, runtime.normalized())
	}
	return out
}

func (c AIGatewayConnectorRuntimeConfig) normalized() AIGatewayConnectorRuntimeConfig {
	return AIGatewayConnectorRuntimeConfig{
		Endpoint:    strings.TrimSpace(c.Endpoint),
		Token:       strings.TrimSpace(c.Token),
		PluginID:    strings.TrimSpace(c.PluginID),
		ConnectorID: strings.TrimSpace(c.ConnectorID),
	}
}

type ModuleToggleConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type ModulesConfig struct {
	Delivery       ModuleToggleConfig `mapstructure:"delivery"`
	Monitoring     ModuleToggleConfig `mapstructure:"monitoring"`
	AI             ModuleToggleConfig `mapstructure:"ai"`
	AIGateway      ModuleToggleConfig `mapstructure:"ai_gateway"`
	Virtualization ModuleToggleConfig `mapstructure:"virtualization"`
	Docker         ModuleToggleConfig `mapstructure:"docker"`
	Security       ModuleToggleConfig `mapstructure:"security"`
	CMDB           ModuleToggleConfig `mapstructure:"cmdb"`
}

type AssetsConfig struct {
	Web  WebAssetsConfig  `mapstructure:"web"`
	Docs DocsAssetsConfig `mapstructure:"docs"`
}

type WebAssetsConfig struct {
	Mode     string `mapstructure:"mode"`
	Dir      string `mapstructure:"dir"`
	ProxyURL string `mapstructure:"proxy_url"`
}

type DocsAssetsConfig struct {
	Mode        string `mapstructure:"mode"`
	Dir         string `mapstructure:"dir"`
	ProxyURL    string `mapstructure:"proxy_url"`
	ExternalURL string `mapstructure:"external_url"`
}

type SecurityConfig struct {
	CredentialEncryptionKey string `mapstructure:"credential_encryption_key"`
	SecretProvider          string `mapstructure:"secret_provider"`
}

type BootstrapConfig struct {
	SeedDefaults bool `mapstructure:"seed_defaults"`
}

type KubernetesConfig struct {
	Clusters []ClusterConfig `mapstructure:"clusters"`
}

type ClusterConfig struct {
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

func Load() (Config, error) {
	v := newConfigViper("SOHA", "SOHA_CONFIG_FILE", "config", "configs", ".")
	setDefaults(v)
	if err := readConfig(v); err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
	))); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.expandEnv()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func newConfigViper(envPrefix, configFileEnv, configName string, configPaths ...string) *viper.Viper {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(stringsReplacer())

	configFile := strings.TrimSpace(os.Getenv(configFileEnv))
	if configFile != "" {
		v.SetConfigFile(configFile)
		return v
	}

	v.SetConfigName(configName)
	for _, path := range configPaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		v.AddConfigPath(path)
	}
	return v
}

func readConfig(v *viper.Viper) error {
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("read config file: %w", err)
		}
	}
	v.AutomaticEnv()
	return nil
}

func (c *Config) expandEnv() {
	c.Database.Password = os.ExpandEnv(c.Database.Password)
	c.Database.MigrationFile = os.ExpandEnv(c.Database.MigrationFile)
	c.Runtime.ExecutionRunnerToken = os.ExpandEnv(c.Runtime.ExecutionRunnerToken)
	c.Auth.DevPrincipal.Password = os.ExpandEnv(c.Auth.DevPrincipal.Password)
	c.Auth.JWT.Secret = os.ExpandEnv(c.Auth.JWT.Secret)
	c.Auth.OIDC.ClientSecret = os.ExpandEnv(c.Auth.OIDC.ClientSecret)
	c.GitLab.Token = os.ExpandEnv(c.GitLab.Token)
	c.Monitoring.WebhookToken = os.ExpandEnv(c.Monitoring.WebhookToken)
	c.Monitoring.PrometheusBearerToken = os.ExpandEnv(c.Monitoring.PrometheusBearerToken)
	c.AIGateway.RateLimit.Redis.Password = os.ExpandEnv(c.AIGateway.RateLimit.Redis.Password)
	c.AIGateway.ConnectorRuntime.expandEnv()
	for i := range c.AIGateway.ConnectorRuntimes {
		c.AIGateway.ConnectorRuntimes[i].expandEnv()
	}
	c.AIGateway.ConnectorEventSink.Token = os.ExpandEnv(c.AIGateway.ConnectorEventSink.Token)
	c.Security.CredentialEncryptionKey = os.ExpandEnv(c.Security.CredentialEncryptionKey)
	for i := range c.Kubernetes.Clusters {
		c.Kubernetes.Clusters[i].Kubeconfig = os.ExpandEnv(c.Kubernetes.Clusters[i].Kubeconfig)
		c.Kubernetes.Clusters[i].KubeconfigData = os.ExpandEnv(c.Kubernetes.Clusters[i].KubeconfigData)
		c.Kubernetes.Clusters[i].PrometheusBearerToken = os.ExpandEnv(c.Kubernetes.Clusters[i].PrometheusBearerToken)
	}
}

func (c *AIGatewayConnectorRuntimeConfig) expandEnv() {
	c.Endpoint = os.ExpandEnv(c.Endpoint)
	c.Token = os.ExpandEnv(c.Token)
	c.PluginID = os.ExpandEnv(c.PluginID)
	c.ConnectorID = os.ExpandEnv(c.ConnectorID)
}

func (c Config) Validate() error {
	if !isProductionEnv(c.App.Env) {
		return nil
	}

	var problems []string
	if c.Auth.EnableDevAuth {
		problems = append(problems, "auth.enable_dev_auth must be false in production")
	}
	problems = appendSecretProblem(problems, "database.password", c.Database.Password, true, 12)
	problems = appendSecretProblem(problems, "auth.jwt.secret", c.Auth.JWT.Secret, true, 32)
	if c.Bootstrap.SeedDefaults && strings.TrimSpace(c.Auth.DevPrincipal.UserID) != "" {
		problems = appendSecretProblem(problems, "auth.dev_principal.password", c.Auth.DevPrincipal.Password, true, 12)
	}
	problems = appendSecretProblem(problems, "runtime.execution_runner_token", c.Runtime.ExecutionRunnerToken, true, 32)
	if c.Monitoring.Enabled || c.Modules.Monitoring.Enabled {
		problems = appendSecretProblem(problems, "monitoring.webhook_token", c.Monitoring.WebhookToken, true, 32)
	}
	if c.Modules.Virtualization.Enabled {
		problems = appendSecretProblem(problems, "security.credential_encryption_key", c.Security.CredentialEncryptionKey, true, 32)
	}
	if c.GitLab.Enabled {
		problems = appendSecretProblem(problems, "gitlab.token", c.GitLab.Token, true, 20)
	}
	if c.Auth.OIDC.Enabled {
		if strings.TrimSpace(c.Auth.OIDC.Issuer) == "" {
			problems = append(problems, "auth.oidc.issuer is required when OIDC is enabled in production")
		}
		if strings.TrimSpace(c.Auth.OIDC.ClientID) == "" {
			problems = append(problems, "auth.oidc.client_id is required when OIDC is enabled in production")
		}
		if strings.TrimSpace(c.Auth.OIDC.ClientSecret) != "" {
			problems = appendSecretProblem(problems, "auth.oidc.client_secret", c.Auth.OIDC.ClientSecret, false, 20)
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("production config validation failed: %s", strings.Join(problems, "; "))
	}
	return nil
}

func appendSecretProblem(problems []string, name, value string, required bool, minLength int) []string {
	secret := strings.TrimSpace(value)
	switch {
	case secret == "" && required:
		return append(problems, fmt.Sprintf("%s is required", name))
	case secret == "":
		return problems
	case isDemoSecret(secret):
		return append(problems, fmt.Sprintf("%s must not use a demo or placeholder value", name))
	case minLength > 0 && len(secret) < minLength:
		return append(problems, fmt.Sprintf("%s must be at least %d characters", name, minLength))
	default:
		return problems
	}
}

func isProductionEnv(env string) bool {
	return strings.EqualFold(strings.TrimSpace(env), "production") || strings.EqualFold(strings.TrimSpace(env), "prod")
}

func isDemoSecret(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("_", "-", " ", "-", ".", "-").Replace(normalized)
	switch normalized {
	case "change-me", "changeme", "changeit", "password", "secret", "token", "admin",
		"soha", "pgsql", "postgres", "dev-only-change-me", "demo-execution-runner-token",
		"dev-alert-webhook-token", "test-secret", "example-secret":
		return true
	}
	return strings.HasPrefix(normalized, "demo-") ||
		strings.HasPrefix(normalized, "dev-") ||
		strings.HasPrefix(normalized, "test-") ||
		strings.HasPrefix(normalized, "example-") ||
		strings.HasPrefix(normalized, "replace-") ||
		strings.HasPrefix(normalized, "replace-with-") ||
		strings.HasPrefix(normalized, "placeholder-") ||
		strings.Contains(normalized, "replace-with") ||
		strings.Contains(normalized, "placeholder") ||
		strings.Contains(normalized, "change-me")
}

func (c DatabaseConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode)
}

func (c DatabaseConfig) ResolveMigrationPath() string {
	candidate := strings.TrimSpace(c.MigrationPath)
	if candidate == "" {
		candidate = strings.TrimSpace(c.MigrationFile)
	}
	if candidate == "" {
		candidate = "migrations"
	}
	candidate = os.ExpandEnv(candidate)
	driver := normalizedDatabaseDriver(c.Driver)

	if redirected := resolveLegacyMigrationFile(candidate, driver); redirected != "" {
		return redirected
	}

	info, err := os.Stat(candidate)
	if err == nil && info.IsDir() {
		driverSpecific := filepath.Join(candidate, driver)
		if info, err := os.Stat(driverSpecific); err == nil && info.IsDir() {
			return driverSpecific
		}
		fallback := filepath.Join(candidate, "0001_init.sql")
		if _, err := os.Stat(fallback); err == nil {
			return fallback
		}
	}

	return candidate
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "soha")
	v.SetDefault("app.env", "development")
	v.SetDefault("http.addr", ":8080")
	v.SetDefault("http.read_timeout", "15s")
	v.SetDefault("http.write_timeout", "15s")
	v.SetDefault("http.base_path", "/api/v1")
	v.SetDefault("http.cors_allowed_origins", []string{"http://localhost:*", "http://127.0.0.1:*"})
	v.SetDefault("logger.level", "debug")
	v.SetDefault("logger.format", "console")
	v.SetDefault("runtime.workflow_workers", 4)
	v.SetDefault("runtime.workflow_queue_size", 64)
	v.SetDefault("runtime.workflow_node_parallelism", 4)
	v.SetDefault("runtime.cluster_sync_parallelism", 4)
	v.SetDefault("runtime.copilot_inspection_parallelism", 2)
	v.SetDefault("runtime.alert_upsert_batch_size", 100)
	v.SetDefault("runtime.virtualization_startup_sync_enabled", true)
	v.SetDefault("runtime.virtualization_worker_interval", "2s")
	v.SetDefault("runtime.virtualization_sync_concurrency", 1)
	v.SetDefault("runtime.execution_runner_token", "")
	v.SetDefault("runtime.execution_job_cluster_id", "")
	v.SetDefault("runtime.execution_job_namespace", "soha-system")
	v.SetDefault("runtime.execution_job_image", "alpine:3.20")
	v.SetDefault("runtime.execution_job_git_image", "alpine/git:2.47.0")
	v.SetDefault("runtime.execution_job_ttl_seconds", 3600)
	v.SetDefault("database.driver", "postgres")
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.name", "soha")
	v.SetDefault("database.user", "pgsql")
	v.SetDefault("database.password", "pgsql")
	v.SetDefault("database.sslmode", "disable")
	v.SetDefault("database.max_open_conns", 20)
	v.SetDefault("database.max_idle_conns", 10)
	v.SetDefault("database.conn_max_lifetime", "1h")
	v.SetDefault("database.auto_migrate", true)
	v.SetDefault("database.migration_path", "migrations")
	v.SetDefault("database.migration_file", "migrations/postgres/0001_init.sql")
	v.SetDefault("auth.enable_dev_auth", true)
	v.SetDefault("auth.login_verification.slider_enabled", false)
	v.SetDefault("auth.dev_principal.user_id", "admin")
	v.SetDefault("auth.dev_principal.name", "Admin")
	v.SetDefault("auth.dev_principal.email", "admin@soha.local")
	v.SetDefault("auth.dev_principal.password", "soha")
	v.SetDefault("auth.dev_principal.roles", []string{"admin", "ops", "auditor"})
	v.SetDefault("auth.jwt.issuer", "soha")
	v.SetDefault("auth.jwt.access_ttl", "15m")
	v.SetDefault("auth.jwt.refresh_ttl", "168h")
	v.SetDefault("auth.oidc.enabled", false)
	v.SetDefault("auth.oidc.provider_name", "default")
	v.SetDefault("auth.oidc.scopes", []string{"openid", "profile", "email"})
	v.SetDefault("auth.oidc.default_roles", []string{"readonly"})
	v.SetDefault("gitlab.enabled", false)
	v.SetDefault("gitlab.base_url", "https://gitlab.com/api/v4")
	v.SetDefault("gitlab.group_id", "")
	v.SetDefault("gitlab.per_page", 50)
	v.SetDefault("gitlab.timeout", "10s")
	v.SetDefault("monitoring.enabled", true)
	v.SetDefault("monitoring.webhook_token", "dev-alert-webhook-token")
	v.SetDefault("monitoring.prometheus_url", "")
	v.SetDefault("monitoring.prometheus_bearer_token", "")
	v.SetDefault("monitoring.prometheus_default_range_minutes", 60)
	v.SetDefault("monitoring.prometheus_step_seconds", 60)
	v.SetDefault("monitoring.prometheus_cluster_label", "cluster")
	v.SetDefault("monitoring.grafana_base_url", "")
	v.SetDefault("swagger.enabled", true)
	v.SetDefault("swagger.path", "/swagger/*any")
	v.SetDefault("mcp.enabled", true)
	v.SetDefault("mcp.default_timeout", "10s")
	v.SetDefault("ai_gateway.rate_limit.backend", "postgres")
	v.SetDefault("ai_gateway.rate_limit.redis.addr", "")
	v.SetDefault("ai_gateway.rate_limit.redis.username", "")
	v.SetDefault("ai_gateway.rate_limit.redis.password", "")
	v.SetDefault("ai_gateway.rate_limit.redis.db", 0)
	v.SetDefault("ai_gateway.rate_limit.redis.tls", false)
	v.SetDefault("ai_gateway.rate_limit.redis.key_prefix", "soha:ai-gateway:rate-limit")
	v.SetDefault("ai_gateway.rate_limit.redis.timeout", "500ms")
	v.SetDefault("ai_gateway.relay.enabled", true)
	v.SetDefault("ai_gateway.relay.default_timeout", "120s")
	v.SetDefault("ai_gateway.relay.stream_timeout", "300s")
	v.SetDefault("ai_gateway.relay.health_check_enabled", false)
	v.SetDefault("ai_gateway.relay.health_check_interval", "1m")
	v.SetDefault("ai_gateway.relay.max_request_body_mb", 32)
	v.SetDefault("ai_gateway.relay.allow_insecure_upstream_http", false)
	v.SetDefault("ai_gateway.relay.allow_private_upstream_hosts", false)
	v.SetDefault("ai_gateway.relay.include_usage_for_openai_stream", true)
	v.SetDefault("ai_gateway.connector_runtime.endpoint", "")
	v.SetDefault("ai_gateway.connector_runtime.token", "")
	v.SetDefault("ai_gateway.connector_runtime.plugin_id", "")
	v.SetDefault("ai_gateway.connector_runtime.connector_id", "")
	v.SetDefault("ai_gateway.connector_runtimes", []map[string]any{})
	v.SetDefault("ai_gateway.connector_event_sink.token", "")
	v.SetDefault("modules.delivery.enabled", true)
	v.SetDefault("modules.monitoring.enabled", true)
	v.SetDefault("modules.ai.enabled", true)
	v.SetDefault("modules.ai_gateway.enabled", true)
	v.SetDefault("modules.virtualization.enabled", true)
	v.SetDefault("modules.docker.enabled", true)
	v.SetDefault("modules.security.enabled", false)
	v.SetDefault("modules.cmdb.enabled", false)
	v.SetDefault("assets.web.mode", "embed")
	v.SetDefault("assets.web.dir", "internal/staticassets/web/dist")
	v.SetDefault("assets.web.proxy_url", "http://localhost:5173")
	v.SetDefault("assets.docs.mode", "external")
	v.SetDefault("assets.docs.dir", "../soha-docs/build")
	v.SetDefault("assets.docs.proxy_url", "http://localhost:3000")
	v.SetDefault("assets.docs.external_url", "https://docs.opensoha.dev/")
	v.SetDefault("security.credential_encryption_key", "")
	v.SetDefault("security.secret_provider", "")
	v.SetDefault("bootstrap.seed_defaults", true)
	v.SetDefault("kubernetes.clusters", []map[string]any{})
}

func stringsReplacer() *strings.Replacer {
	return strings.NewReplacer(".", "_")
}

func normalizedDatabaseDriver(raw string) string {
	driver := strings.ToLower(strings.TrimSpace(raw))
	if driver == "" {
		return "postgres"
	}
	return driver
}

func resolveLegacyMigrationFile(candidate, driver string) string {
	clean := filepath.Clean(candidate)
	if filepath.Base(clean) != "0001_init.sql" {
		return ""
	}
	if filepath.Base(filepath.Dir(clean)) != "migrations" {
		return ""
	}
	driverSpecific := filepath.Join(filepath.Dir(clean), driver, "0001_init.sql")
	if _, err := os.Stat(driverSpecific); err == nil {
		return driverSpecific
	}
	return ""
}
