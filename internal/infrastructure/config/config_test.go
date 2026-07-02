package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestResolveMigrationPathUsesDriverDirectoryForMigrationRoot(t *testing.T) {
	root := t.TempDir()
	postgresDir := filepath.Join(root, "postgres")
	if err := os.MkdirAll(postgresDir, 0o755); err != nil {
		t.Fatalf("mkdir postgres dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(postgresDir, "0001_init.sql"), []byte("-- init"), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	if err := os.WriteFile(filepath.Join(postgresDir, "0011_application_services.sql"), []byte("-- app services"), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	cfg := DatabaseConfig{Driver: "postgres", MigrationPath: root}
	if got := cfg.ResolveMigrationPath(); got != postgresDir {
		t.Fatalf("ResolveMigrationPath() = %q, want %q", got, postgresDir)
	}
}

func TestResolveMigrationPathKeepsLegacySingleFileRedirect(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "migrations", "0001_init.sql")
	postgresDir := filepath.Join(root, "migrations", "postgres")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.MkdirAll(postgresDir, 0o755); err != nil {
		t.Fatalf("mkdir postgres dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("-- legacy"), 0o600); err != nil {
		t.Fatalf("write legacy migration: %v", err)
	}
	driverInit := filepath.Join(postgresDir, "0001_init.sql")
	if err := os.WriteFile(driverInit, []byte("-- driver"), 0o600); err != nil {
		t.Fatalf("write driver migration: %v", err)
	}

	cfg := DatabaseConfig{Driver: "postgres", MigrationFile: legacyPath}
	if got := cfg.ResolveMigrationPath(); got != driverInit {
		t.Fatalf("ResolveMigrationPath() = %q, want %q", got, driverInit)
	}
}

func TestDefaultsConfigurePostgresGatewayRateLimitBackend(t *testing.T) {
	v := viper.New()
	setDefaults(v)
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.AIGateway.RateLimit.Backend != "postgres" {
		t.Fatalf("expected postgres rate-limit backend default, got %q", cfg.AIGateway.RateLimit.Backend)
	}
	if cfg.AIGateway.RateLimit.Redis.KeyPrefix != "soha:ai-gateway:rate-limit" {
		t.Fatalf("unexpected redis key prefix default: %q", cfg.AIGateway.RateLimit.Redis.KeyPrefix)
	}
	if v.GetString("ai_gateway.rate_limit.redis.timeout") != "500ms" {
		t.Fatalf("unexpected redis timeout default: %s", v.GetString("ai_gateway.rate_limit.redis.timeout"))
	}
	if !cfg.Modules.AIGateway.Enabled {
		t.Fatalf("AI Gateway module should be enabled by default")
	}
	if got := cfg.AIGateway.ConnectorRuntimeConfigs(); len(got) != 0 {
		t.Fatalf("AI Gateway connector runtime should be disabled by default, got %#v", got)
	}
	if cfg.AIGateway.ConnectorEventSink.Token != "" {
		t.Fatalf("connector event sink token default = %q, want empty", cfg.AIGateway.ConnectorEventSink.Token)
	}
	if cfg.Auth.LoginVerification.SliderEnabled {
		t.Fatal("login slider verification should be disabled by default")
	}
	if !cfg.Runtime.VirtualizationStartupSync {
		t.Fatal("virtualization startup sync should be enabled by default")
	}
	if cfg.Runtime.VirtualizationWorkerInterval != 2*time.Second {
		t.Fatalf("virtualization worker interval default = %s, want 2s", cfg.Runtime.VirtualizationWorkerInterval)
	}
	if cfg.Runtime.VirtualizationSyncConcurrency != 1 {
		t.Fatalf("virtualization sync concurrency default = %d, want 1", cfg.Runtime.VirtualizationSyncConcurrency)
	}
	if cfg.Logger.Level != "info" {
		t.Fatalf("logger level default = %q, want info", cfg.Logger.Level)
	}
}

func TestLoadRejectsDefaultConfigWhenFileMissing(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	}()
	t.Setenv("SOHA_CONFIG_FILE", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want default config validation failure")
	}
}

func TestLoadUsesExplicitConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
app:
  name: custom-soha
runtime:
  execution_runner_token: runner-token-123456789012345678901234
database:
  password: postgres-password-123456
auth:
  enable_dev_auth: false
  dev_principal:
    password: admin-password-123456
  jwt:
    secret: jwt-secret-123456789012345678901234567890
monitoring:
  enabled: true
  webhook_token: webhook-token-123456789012345678901234
modules:
  monitoring:
    enabled: true
  virtualization:
    enabled: true
security:
  credential_encryption_key: credential-key-123456789012345678901234
`), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("SOHA_CONFIG_FILE", path)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.App.Name != "custom-soha" {
		t.Fatalf("App.Name = %q, want custom-soha", cfg.App.Name)
	}
}

func TestAIGatewayConnectorRuntimeConfigsNormalizeConfiguredEntries(t *testing.T) {
	cfg := AIGatewayConfig{
		ConnectorRuntime: AIGatewayConnectorRuntimeConfig{
			Endpoint:    " https://runtime.local ",
			Token:       " runtime-token ",
			PluginID:    " opensoha.feishu ",
			ConnectorID: " feishu ",
		},
		ConnectorRuntimes: []AIGatewayConnectorRuntimeConfig{
			{Endpoint: " "},
			{Endpoint: " https://runtime-2.local ", ConnectorID: " custom "},
		},
	}

	runtimes := cfg.ConnectorRuntimeConfigs()
	if len(runtimes) != 2 {
		t.Fatalf("ConnectorRuntimeConfigs() length = %d, want 2: %#v", len(runtimes), runtimes)
	}
	if runtimes[0].Endpoint != "https://runtime.local" || runtimes[0].Token != "runtime-token" || runtimes[0].PluginID != "opensoha.feishu" || runtimes[0].ConnectorID != "feishu" {
		t.Fatalf("unexpected normalized singular runtime: %#v", runtimes[0])
	}
	if runtimes[1].Endpoint != "https://runtime-2.local" || runtimes[1].ConnectorID != "custom" {
		t.Fatalf("unexpected normalized list runtime: %#v", runtimes[1])
	}
}

func TestAIGatewayConnectorRuntimeConfigExpandsEnv(t *testing.T) {
	t.Setenv("SOHA_TEST_CONNECTOR_ENDPOINT", "https://runtime.local")
	t.Setenv("SOHA_TEST_CONNECTOR_TOKEN", "runtime-token")
	t.Setenv("SOHA_TEST_CONNECTOR_PLUGIN_ID", "opensoha.feishu")
	t.Setenv("SOHA_TEST_CONNECTOR_ID", "feishu")
	t.Setenv("SOHA_TEST_CONNECTOR_EVENT_SINK_TOKEN", "sink-token")

	cfg := Config{
		AIGateway: AIGatewayConfig{
			ConnectorRuntime: AIGatewayConnectorRuntimeConfig{
				Endpoint: "${SOHA_TEST_CONNECTOR_ENDPOINT}",
				Token:    "${SOHA_TEST_CONNECTOR_TOKEN}",
			},
			ConnectorRuntimes: []AIGatewayConnectorRuntimeConfig{
				{
					Endpoint:    "${SOHA_TEST_CONNECTOR_ENDPOINT}",
					PluginID:    "${SOHA_TEST_CONNECTOR_PLUGIN_ID}",
					ConnectorID: "${SOHA_TEST_CONNECTOR_ID}",
				},
			},
			ConnectorEventSink: AIGatewayConnectorEventSinkConfig{
				Token: "${SOHA_TEST_CONNECTOR_EVENT_SINK_TOKEN}",
			},
		},
	}

	cfg.expandEnv()
	if cfg.AIGateway.ConnectorRuntime.Endpoint != "https://runtime.local" || cfg.AIGateway.ConnectorRuntime.Token != "runtime-token" {
		t.Fatalf("singular connector runtime env expansion failed: %#v", cfg.AIGateway.ConnectorRuntime)
	}
	runtime := cfg.AIGateway.ConnectorRuntimes[0]
	if runtime.Endpoint != "https://runtime.local" || runtime.PluginID != "opensoha.feishu" || runtime.ConnectorID != "feishu" {
		t.Fatalf("list connector runtime env expansion failed: %#v", runtime)
	}
	if cfg.AIGateway.ConnectorEventSink.Token != "sink-token" {
		t.Fatalf("connector event sink token = %q, want expanded env", cfg.AIGateway.ConnectorEventSink.Token)
	}
}

func TestConfigValidateRequiresOIDCFieldsWhenEnabled(t *testing.T) {
	cfg := Config{
		Auth: AuthConfig{
			OIDC: OIDCConfig{Enabled: true},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want OIDC validation failure")
	}
	message := err.Error()
	for _, want := range []string{
		"auth.oidc.issuer",
		"auth.oidc.client_id",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("Validate() error %q missing %q", message, want)
		}
	}
}

func TestConfigValidateRejectsDemoSecrets(t *testing.T) {
	cfg := validSecureConfig()
	cfg.Database.Password = "change-me"
	cfg.Auth.JWT.Secret = "dev-only-change-me"
	cfg.Runtime.ExecutionRunnerToken = "demo-execution-runner-token"
	cfg.Monitoring.WebhookToken = ""
	cfg.Security.CredentialEncryptionKey = "REPLACE_WITH_STABLE_CREDENTIAL_ENCRYPTION_KEY"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want config validation failure")
	}
	message := err.Error()
	for _, want := range []string{
		"database.password",
		"auth.jwt.secret",
		"runtime.execution_runner_token",
		"monitoring.webhook_token",
		"security.credential_encryption_key",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("Validate() error %q missing %q", message, want)
		}
	}
}

func TestConfigValidateRejectsDevAuth(t *testing.T) {
	cfg := validSecureConfig()
	cfg.Auth.EnableDevAuth = true

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want dev auth rejection")
	}
	if !strings.Contains(err.Error(), "auth.enable_dev_auth") {
		t.Fatalf("Validate() error %q missing auth.enable_dev_auth", err)
	}
}

func TestConfigValidateAllowsStrongSecrets(t *testing.T) {
	cfg := validSecureConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestExpandEnvExpandsSensitiveConfig(t *testing.T) {
	t.Setenv("SOHA_TEST_JWT_SECRET", "jwt-secret-from-env-12345678901234567890")
	t.Setenv("SOHA_TEST_KUBECONFIG", "/tmp/kubeconfig")
	cfg := Config{
		Auth: AuthConfig{JWT: JWTConfig{Secret: "${SOHA_TEST_JWT_SECRET}"}},
		Kubernetes: KubernetesConfig{Clusters: []ClusterConfig{{
			Kubeconfig: "${SOHA_TEST_KUBECONFIG}",
		}}},
	}

	cfg.expandEnv()

	if cfg.Auth.JWT.Secret != "jwt-secret-from-env-12345678901234567890" {
		t.Fatalf("jwt secret = %q, want expanded env", cfg.Auth.JWT.Secret)
	}
	if cfg.Kubernetes.Clusters[0].Kubeconfig != "/tmp/kubeconfig" {
		t.Fatalf("kubeconfig = %q, want expanded env", cfg.Kubernetes.Clusters[0].Kubeconfig)
	}
}

func validSecureConfig() Config {
	return Config{
		Runtime: RuntimeConfig{
			ExecutionRunnerToken: "runner-token-123456789012345678901234",
		},
		Database: DatabaseConfig{
			Password: "postgres-password-123456",
		},
		Auth: AuthConfig{
			EnableDevAuth: false,
			DevPrincipal: DevPrincipalConfig{
				UserID:   "admin",
				Password: "admin-password-123456",
			},
			JWT: JWTConfig{
				Secret: "jwt-secret-123456789012345678901234567890",
			},
		},
		Monitoring: MonitoringConfig{
			Enabled:      true,
			WebhookToken: "webhook-token-123456789012345678901234",
		},
		Modules: ModulesConfig{
			Monitoring:     ModuleToggleConfig{Enabled: true},
			Virtualization: ModuleToggleConfig{Enabled: true},
		},
		Security: SecurityConfig{
			CredentialEncryptionKey: "credential-key-123456789012345678901234",
		},
		Bootstrap: BootstrapConfig{SeedDefaults: true},
	}
}
