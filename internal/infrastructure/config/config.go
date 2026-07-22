package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/opensoha/soha/internal/platform/appconfig"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/spf13/viper"
)

const (
	defaultDevPrincipalUserID  = "67d90df8-9de4-4a7b-b3f8-86cd36f899e2"
	defaultSystemSecret        = "soha-123456789012345678901234567890"
	DefaultMarketplaceURL      = "https://marketplace.opensoha.com/marketplace/index.json"
	DefaultMarketplaceSourceID = "opensoha-official"
	jwtKeyID                   = "config-jwt-v1"
	runnerKeyID                = "config-runner-v1"
	webhookKeyID               = "config-webhook-v1"
	encryptionKeyID            = "config-credential-v1"
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
	Plugins    PluginsConfig    `mapstructure:"plugins"`
	Modules    ModulesConfig    `mapstructure:"modules"`
	Assets     AssetsConfig     `mapstructure:"assets"`
	Security   SecurityConfig   `mapstructure:"security"`
	Bootstrap  BootstrapConfig  `mapstructure:"bootstrap"`
	Kubernetes KubernetesConfig `mapstructure:"kubernetes"`
}

type AppConfig struct {
	Name string `mapstructure:"name"`
}

type HTTPConfig struct {
	Addr               string        `mapstructure:"addr"`
	ReadTimeout        time.Duration `mapstructure:"read_timeout"`
	WriteTimeout       time.Duration `mapstructure:"write_timeout"`
	IdleTimeout        time.Duration `mapstructure:"idle_timeout"`
	MaxHeaderBytes     int           `mapstructure:"max_header_bytes"`
	BasePath           string        `mapstructure:"base_path"`
	CORSAllowedOrigins []string      `mapstructure:"cors_allowed_origins"`
	TrustedProxies     []string      `mapstructure:"trusted_proxies"`
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
	ExecutionRunnerKeys           keyring.Ring  `mapstructure:"-"`
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

type AuthConfig = appconfig.Auth

type LoginVerificationConfig = appconfig.LoginVerification

type GitLabConfig struct {
	Enabled bool          `mapstructure:"enabled"`
	BaseURL string        `mapstructure:"base_url"`
	Token   string        `mapstructure:"token"`
	GroupID string        `mapstructure:"group_id"`
	PerPage int           `mapstructure:"per_page"`
	Timeout time.Duration `mapstructure:"timeout"`
}

type DevPrincipalConfig = appconfig.DevPrincipal

type JWTConfig = appconfig.JWT

type OIDCConfig = appconfig.OIDC

type MonitoringConfig = appconfig.Monitoring

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
	FirstByteTimeout            time.Duration `mapstructure:"first_byte_timeout"`
	StreamIdleTimeout           time.Duration `mapstructure:"stream_idle_timeout"`
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

type PluginsConfig struct {
	Marketplace PluginMarketplaceConfig `mapstructure:"marketplace"`
}

type PluginMarketplaceConfig struct {
	URL      string                    `mapstructure:"url"`
	SourceID string                    `mapstructure:"source_id"`
	Sources  []PluginMarketplaceSource `mapstructure:"sources"`
}

type PluginMarketplaceSource struct {
	ID  string `mapstructure:"id"`
	URL string `mapstructure:"url"`
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

type ModuleToggleConfig = appconfig.ModuleToggle

type ModulesConfig = appconfig.Modules

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
	CredentialEncryptionKey  string       `mapstructure:"credential_encryption_key"`
	CredentialEncryptionKeys keyring.Ring `mapstructure:"-"`
	SecretProvider           string       `mapstructure:"secret_provider"`
}

type BootstrapConfig struct {
	SeedDefaults bool `mapstructure:"seed_defaults"`
}

type KubernetesConfig struct {
	Clusters []ClusterConfig `mapstructure:"clusters"`
}

type ClusterConfig = appconfig.Cluster

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
	if err := cfg.initializeRuntimeKeyrings(); err != nil {
		return Config{}, fmt.Errorf("initialize configured keyrings: %w", err)
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
	c.Plugins.Marketplace.URL = os.ExpandEnv(c.Plugins.Marketplace.URL)
	for i := range c.Plugins.Marketplace.Sources {
		c.Plugins.Marketplace.Sources[i].URL = os.ExpandEnv(c.Plugins.Marketplace.Sources[i].URL)
	}
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
	problems := c.staticProblems()
	if len(problems) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) staticProblems() []string {
	problems := make([]string, 0)
	if c.Auth.EnableDevAuth {
		problems = append(problems, "auth.enable_dev_auth must be false")
	}
	for _, proxy := range c.HTTP.TrustedProxies {
		if !validTrustedProxy(proxy) {
			problems = append(problems, fmt.Sprintf("http.trusted_proxies contains invalid IP or CIDR %q", proxy))
		}
	}
	coreSecrets := []struct {
		name  string
		value string
	}{
		{name: "auth.jwt.secret", value: c.Auth.JWT.Secret},
		{name: "runtime.execution_runner_token", value: c.Runtime.ExecutionRunnerToken},
		{name: "monitoring.webhook_token", value: c.Monitoring.WebhookToken},
		{name: "security.credential_encryption_key", value: c.Security.CredentialEncryptionKey},
	}
	for _, item := range coreSecrets {
		problems = appendSecretProblem(problems, item.name, item.value, true, 32)
		if strings.TrimSpace(item.value) != item.value {
			problems = append(problems, fmt.Sprintf("%s must not have leading or trailing whitespace", item.name))
		}
	}
	if c.GitLab.Enabled {
		problems = appendSecretProblem(problems, "gitlab.token", c.GitLab.Token, true, 20)
	}
	problems = append(problems, validateSharedConfigProblems(c)...)
	return problems
}

func validTrustedProxy(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if net.ParseIP(value) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(value)
	return err == nil
}

func (c *Config) initializeRuntimeKeyrings() error {
	type configuredKey struct {
		id     string
		secret string
		assign func(keyring.Ring)
	}
	items := []configuredKey{
		{id: jwtKeyID, secret: c.Auth.JWT.Secret, assign: func(ring keyring.Ring) { c.Auth.JWT.Keys = ring }},
		{id: runnerKeyID, secret: c.Runtime.ExecutionRunnerToken, assign: func(ring keyring.Ring) { c.Runtime.ExecutionRunnerKeys = ring }},
		{id: webhookKeyID, secret: c.Monitoring.WebhookToken, assign: func(ring keyring.Ring) { c.Monitoring.WebhookKeys = ring }},
		{id: encryptionKeyID, secret: c.Security.CredentialEncryptionKey, assign: func(ring keyring.Ring) { c.Security.CredentialEncryptionKeys = ring }},
	}
	for _, item := range items {
		key, err := keyring.NewKey(item.id, item.secret, time.Unix(0, 0).UTC(), nil)
		if err != nil {
			return fmt.Errorf("build %s: %w", item.id, err)
		}
		ring, err := keyring.New(key, nil)
		if err != nil {
			return fmt.Errorf("build %s keyring: %w", item.id, err)
		}
		item.assign(ring)
	}
	return nil
}

func validateSharedConfigProblems(c Config) []string {
	var problems []string
	if c.Auth.OIDC.Enabled {
		problems = appendRequiredFieldProblem(problems, "auth.oidc.issuer", c.Auth.OIDC.Issuer)
		problems = appendRequiredFieldProblem(problems, "auth.oidc.client_id", c.Auth.OIDC.ClientID)
		if strings.TrimSpace(c.Auth.OIDC.ClientSecret) != "" {
			problems = appendSecretProblem(problems, "auth.oidc.client_secret", c.Auth.OIDC.ClientSecret, false, 20)
		}
	}
	return problems
}

func appendRequiredFieldProblem(problems []string, name, value string) []string {
	if strings.TrimSpace(value) == "" {
		return append(problems, fmt.Sprintf("%s is required", name))
	}
	return problems
}

func appendSecretProblem(problems []string, name, value string, required bool, minLength int) []string {
	secret := strings.TrimSpace(value)
	switch {
	case secret == "" && required:
		return append(problems, fmt.Sprintf("%s is required", name))
	case secret == "":
		return problems
	case minLength > 0 && len(secret) < minLength:
		return append(problems, fmt.Sprintf("%s must be at least %d characters", name, minLength))
	default:
		return problems
	}
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

var configDefaults = []struct {
	key   string
	value any
}{
	{"app.name", "soha"},
	{"http.addr", ":8080"},
	{"http.read_timeout", "15s"},
	{"http.write_timeout", "15s"},
	{"http.idle_timeout", "120s"},
	{"http.max_header_bytes", 1 << 20},
	{"http.base_path", "/api/v1"},
	{"http.cors_allowed_origins", []string{"http://localhost:*", "http://127.0.0.1:*"}},
	{"http.trusted_proxies", []string{}},
	{"logger.level", "info"},
	{"logger.format", "console"},
	{"runtime.workflow_workers", 4},
	{"runtime.workflow_queue_size", 64},
	{"runtime.workflow_node_parallelism", 4},
	{"runtime.cluster_sync_parallelism", 4},
	{"runtime.copilot_inspection_parallelism", 2},
	{"runtime.alert_upsert_batch_size", 100},
	{"runtime.virtualization_startup_sync_enabled", true},
	{"runtime.virtualization_worker_interval", "2s"},
	{"runtime.virtualization_sync_concurrency", 1},
	{"runtime.execution_runner_token", defaultSystemSecret},
	{"runtime.execution_job_cluster_id", ""},
	{"runtime.execution_job_namespace", "soha-system"},
	{"runtime.execution_job_image", "alpine:3.20"},
	{"runtime.execution_job_git_image", "alpine/git:2.47.0"},
	{"runtime.execution_job_ttl_seconds", 3600},
	{"database.driver", "postgres"},
	{"database.host", "localhost"},
	{"database.port", 5432},
	{"database.name", "soha"},
	{"database.user", "pgsql"},
	{"database.password", "pgsql"},
	{"database.sslmode", "disable"},
	{"database.max_open_conns", 20},
	{"database.max_idle_conns", 10},
	{"database.conn_max_lifetime", "1h"},
	{"database.auto_migrate", true},
	{"database.migration_path", "migrations"},
	{"database.migration_file", "migrations/postgres/0001_init.sql"},
	{"auth.enable_dev_auth", true},
	{"auth.login_verification.slider_enabled", false},
	{"auth.dev_principal.user_id", defaultDevPrincipalUserID},
	{"auth.dev_principal.name", "OpenSoha"},
	{"auth.dev_principal.email", "opensoha@soha.local"},
	{"auth.dev_principal.password", "opensoha"},
	{"auth.dev_principal.roles", []string{"admin", "ops", "auditor"}},
	{"auth.jwt.issuer", "soha"},
	{"auth.jwt.secret", defaultSystemSecret},
	{"auth.jwt.access_ttl", "15m"},
	{"auth.jwt.refresh_ttl", "168h"},
	{"auth.oidc.enabled", false},
	{"auth.oidc.scopes", []string{"openid", "profile", "email"}},
	{"auth.oidc.default_roles", []string{"readonly"}},
	{"gitlab.enabled", false},
	{"gitlab.base_url", "https://gitlab.com/api/v4"},
	{"gitlab.group_id", ""},
	{"gitlab.per_page", 50},
	{"gitlab.timeout", "10s"},
	{"monitoring.enabled", true},
	{"monitoring.webhook_token", defaultSystemSecret},
	{"monitoring.prometheus_url", ""},
	{"monitoring.prometheus_bearer_token", ""},
	{"monitoring.prometheus_default_range_minutes", 60},
	{"monitoring.prometheus_step_seconds", 60},
	{"monitoring.prometheus_cluster_label", "cluster"},
	{"monitoring.grafana_base_url", ""},
	{"swagger.enabled", true},
	{"swagger.path", "/swagger/*any"},
	{"mcp.enabled", true},
	{"mcp.default_timeout", "10s"},
	{"ai_gateway.rate_limit.backend", "postgres"},
	{"ai_gateway.rate_limit.redis.addr", ""},
	{"ai_gateway.rate_limit.redis.username", ""},
	{"ai_gateway.rate_limit.redis.password", ""},
	{"ai_gateway.rate_limit.redis.db", 0},
	{"ai_gateway.rate_limit.redis.tls", false},
	{"ai_gateway.rate_limit.redis.key_prefix", "soha:ai-gateway:rate-limit"},
	{"ai_gateway.rate_limit.redis.timeout", "500ms"},
	{"ai_gateway.relay.enabled", true},
	{"ai_gateway.relay.default_timeout", "120s"},
	{"ai_gateway.relay.stream_timeout", "300s"},
	{"ai_gateway.relay.first_byte_timeout", "30s"},
	{"ai_gateway.relay.stream_idle_timeout", "60s"},
	{"ai_gateway.relay.health_check_enabled", false},
	{"ai_gateway.relay.health_check_interval", "1m"},
	{"ai_gateway.relay.max_request_body_mb", 32},
	{"ai_gateway.relay.allow_insecure_upstream_http", false},
	{"ai_gateway.relay.allow_private_upstream_hosts", false},
	{"ai_gateway.relay.include_usage_for_openai_stream", true},
	{"ai_gateway.connector_runtime.endpoint", ""},
	{"ai_gateway.connector_runtime.token", ""},
	{"ai_gateway.connector_runtime.plugin_id", ""},
	{"ai_gateway.connector_runtime.connector_id", ""},
	{"ai_gateway.connector_runtimes", []map[string]any{}},
	{"ai_gateway.connector_event_sink.token", ""},
	{"plugins.marketplace.url", DefaultMarketplaceURL},
	{"plugins.marketplace.source_id", DefaultMarketplaceSourceID},
	{"plugins.marketplace.sources", []map[string]any{}},
	{"modules.home.enabled", true},
	{"modules.delivery.enabled", true},
	{"modules.monitoring.enabled", true},
	{"modules.ai.enabled", true},
	{"modules.ai_gateway.enabled", true},
	{"modules.virtualization.enabled", true},
	{"modules.docker.enabled", true},
	{"modules.security.enabled", false},
	{"modules.cmdb.enabled", false},
	{"assets.web.mode", "embed"},
	{"assets.web.dir", "internal/staticassets/web/dist"},
	{"assets.web.proxy_url", "http://localhost:5173"},
	{"assets.docs.mode", "external"},
	{"assets.docs.dir", "../soha-docs/build"},
	{"assets.docs.proxy_url", "http://localhost:3000"},
	{"assets.docs.external_url", "https://docs.opensoha.dev/"},
	{"security.credential_encryption_key", defaultSystemSecret},
	{"security.secret_provider", ""},
	{"bootstrap.seed_defaults", true},
	{"kubernetes.clusters", []map[string]any{}},
}

func setDefaults(v *viper.Viper) {
	for _, item := range configDefaults {
		v.SetDefault(item.key, item.value)
	}
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
