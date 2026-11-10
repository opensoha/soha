package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/google/uuid"
	"github.com/spf13/viper"
)

const knownPublicProjectToken = "soha-1234567890123456789012345678901234567890"

type Config struct {
	Prometheus          PrometheusConfig          `mapstructure:"prometheus"`
	App                 AppConfig                 `mapstructure:"app"`
	HTTP                HTTPConfig                `mapstructure:"http"`
	Logger              LoggerConfig              `mapstructure:"logger"`
	Auth                AuthConfig                `mapstructure:"auth"`
	Security            SecurityConfig            `mapstructure:"security"`
	Audit               AuditConfig               `mapstructure:"audit"`
	ControlPlane        ControlPlaneConfig        `mapstructure:"control_plane"`
	Kubernetes          KubernetesConfig          `mapstructure:"kubernetes"`
	PersonalEnvironment PersonalEnvironmentConfig `mapstructure:"personal_environment"`
	DockerRuntime       DockerRuntimeConfig       `mapstructure:"docker_runtime"`
}

type DockerRuntimeConfig struct {
	VolumeReaderImage         string   `mapstructure:"volume_reader_image"`
	VolumeReaderOwner         string   `mapstructure:"volume_reader_owner"`
	VolumeReaderCleanupImages []string `mapstructure:"volume_reader_cleanup_images"`
}

var readerImagePattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func validateDockerRuntime(cfg DockerRuntimeConfig) error {
	if cfg.VolumeReaderImage == "" && cfg.VolumeReaderOwner == "" && len(cfg.VolumeReaderCleanupImages) == 0 {
		return nil
	}
	id, err := uuid.Parse(cfg.VolumeReaderOwner)
	if !readerImagePattern.MatchString(cfg.VolumeReaderImage) || err != nil || id == uuid.Nil || id.String() != cfg.VolumeReaderOwner {
		return fmt.Errorf("docker_runtime safe volume reader requires a fixed image ID and canonical nonzero owner UUID")
	}
	if len(cfg.VolumeReaderCleanupImages) > 4 {
		return fmt.Errorf("docker_runtime safe reader allows at most four cleanup-only image IDs")
	}
	seen := map[string]bool{cfg.VolumeReaderImage: true}
	for _, old := range cfg.VolumeReaderCleanupImages {
		if !readerImagePattern.MatchString(old) || seen[old] {
			return fmt.Errorf("docker_runtime safe reader cleanup image IDs must be pinned and distinct")
		}
		seen[old] = true
	}
	return nil
}

type PersonalEnvironmentConfig struct {
	Enabled          bool                          `mapstructure:"enabled"`
	Image            string                        `mapstructure:"image"`
	QuotaHelperImage string                        `mapstructure:"quota_helper_image"`
	MCPStdioAssets   map[MCPStdioAssetRef][]string `mapstructure:"-"`
}

type AppConfig struct {
	Name string `mapstructure:"name"`
	Env  string `mapstructure:"env"`
}

type HTTPConfig struct {
	Addr           string        `mapstructure:"addr"`
	BasePath       string        `mapstructure:"base_path"`
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`
	AllowedOrigins []string      `mapstructure:"allowed_origins"`
}

type LoggerConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type AuthConfig struct {
	BearerToken     string `mapstructure:"bearer_token"`
	BearerTokenFile string `mapstructure:"bearer_token_file"`
}

type SecurityConfig struct {
	AllowedActions []string `mapstructure:"allowed_actions"`
}

type AuditConfig struct {
	FilePath string `mapstructure:"file_path"`
}

type ControlPlaneConfig struct {
	Enabled         bool                `mapstructure:"enabled"`
	BaseURL         string              `mapstructure:"base_url"`
	BearerToken     string              `mapstructure:"bearer_token"`
	BearerTokenFile string              `mapstructure:"bearer_token_file"`
	AgentID         string              `mapstructure:"agent_id"`
	RuntimeEndpoint string              `mapstructure:"runtime_endpoint"`
	PollInterval    time.Duration       `mapstructure:"poll_interval"`
	MaxConcurrency  int                 `mapstructure:"max_concurrency"`
	DefaultTimeout  time.Duration       `mapstructure:"default_timeout"`
	CallbackRetry   CallbackRetryConfig `mapstructure:"callback_retry"`
	ProviderKinds   []string            `mapstructure:"provider_kinds"`
	WorkspaceRoot   string              `mapstructure:"workspace_root"`
	Docker          DockerRunnerConfig  `mapstructure:"docker"`
	AgentRuntime    AgentRuntimeConfig  `mapstructure:"agent_runtime"`
	Outpost         OutpostConfig       `mapstructure:"outpost"`
	Session         SessionConfig       `mapstructure:"session"`
	Buildpacks      BuildpacksConfig    `mapstructure:"buildpacks"`
}

type SessionConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	ReconnectMin     time.Duration `mapstructure:"reconnect_min"`
	ReconnectMax     time.Duration `mapstructure:"reconnect_max"`
	HandshakeTimeout time.Duration `mapstructure:"handshake_timeout"`
	MaxStreams       int           `mapstructure:"max_streams"`
}

type OutpostConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	AgentID           string        `mapstructure:"agent_id"`
	ProtocolVersion   string        `mapstructure:"protocol_version"`
	TrustKeyID        string        `mapstructure:"trust_key_id"`
	TrustPublicKey    string        `mapstructure:"trust_public_key"`
	PollInterval      time.Duration `mapstructure:"poll_interval"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
}

type CallbackRetryConfig struct {
	MaxAttempts int           `mapstructure:"max_attempts"`
	Backoff     time.Duration `mapstructure:"backoff"`
}

type DockerRunnerConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	WorkerID       string        `mapstructure:"worker_id"`
	HostIDs        []string      `mapstructure:"host_ids"`
	OperationKinds []string      `mapstructure:"operation_kinds"`
	ComposeRoot    string        `mapstructure:"compose_root"`
	PollInterval   time.Duration `mapstructure:"poll_interval"`
}

type AgentRuntimeConfig struct {
	Enabled            bool                           `mapstructure:"enabled"`
	WorkerID           string                         `mapstructure:"worker_id"`
	ProviderIDs        []string                       `mapstructure:"provider_ids"`
	ProviderKinds      []string                       `mapstructure:"provider_kinds"`
	HermesCommand      string                         `mapstructure:"hermes_command"`
	WorkspaceRoot      string                         `mapstructure:"workspace_root"`
	PollInterval       time.Duration                  `mapstructure:"poll_interval"`
	Environment        string                         `mapstructure:"environment"`
	Labels             map[string]string              `mapstructure:"labels"`
	ConformanceTimeout time.Duration                  `mapstructure:"conformance_timeout"`
	Providers          map[string]AgentProviderConfig `mapstructure:"providers"`
}

type AgentProviderConfig struct {
	CAFile           string   `mapstructure:"ca_file" json:"-"`
	Endpoint         string   `mapstructure:"endpoint"`
	BearerToken      string   `mapstructure:"bearer_token" json:"-"`
	BearerTokenFile  string   `mapstructure:"bearer_token_file" json:"-"`
	Command          string   `mapstructure:"command"`
	Args             []string `mapstructure:"args"`
	PromptArg        string   `mapstructure:"prompt_arg"`
	SkillArg         string   `mapstructure:"skill_arg"`
	ProviderSkillArg string   `mapstructure:"provider_skill_arg"`
}

type PrometheusConfig struct {
	BaseURL     string `mapstructure:"base_url"`
	BearerToken string `mapstructure:"bearer_token"`
}

type KubernetesConfig struct {
	Enabled        bool              `mapstructure:"enabled"`
	ID             string            `mapstructure:"id"`
	Name           string            `mapstructure:"name"`
	Kubeconfig     string            `mapstructure:"kubeconfig"`
	KubeconfigData string            `mapstructure:"kubeconfig_data"`
	Context        string            `mapstructure:"context"`
	Region         string            `mapstructure:"region"`
	Environment    string            `mapstructure:"environment"`
	Labels         map[string]string `mapstructure:"labels"`
}

func Load() (Config, error) {
	v := newConfigViper("SOHA_AGENT", "SOHA_AGENT_CONFIG_FILE", "agent.config", "configs", ".")
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

	cfg.Kubernetes.Kubeconfig = os.ExpandEnv(cfg.Kubernetes.Kubeconfig)
	if err := resolveBearerTokenFiles(&cfg); err != nil {
		return Config{}, err
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	if cfg.PersonalEnvironment.Enabled {
		assets, err := loadMCPStdioAssets(os.Getenv("SOHA_AGENT_MCP_STDIO_FILE"))
		if err != nil {
			return Config{}, fmt.Errorf("load SOHA_AGENT_MCP_STDIO_FILE: %w", err)
		}
		cfg.PersonalEnvironment.MCPStdioAssets = assets
	}
	return cfg, nil
}

func newConfigViper(envPrefix, configFileEnv, configName string, configPaths ...string) *viper.Viper {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

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

func Validate(cfg Config) error {
	if err := validateDockerRuntime(cfg.DockerRuntime); err != nil {
		return err
	}
	if err := validateListenSecurity(cfg.HTTP.Addr, cfg.Auth.BearerToken); err != nil {
		return err
	}
	if isProductionEnv(cfg.App.Env) {
		if err := validateProductionToken("auth.bearer_token", cfg.Auth.BearerToken); err != nil {
			return err
		}
		if err := validateProductionAllowedActions(cfg.Security.AllowedActions, cfg.HTTP.AllowedOrigins); err != nil {
			return err
		}
		if cfg.ControlPlane.Enabled || cfg.ControlPlane.Session.Enabled {
			if err := validateProductionToken("control_plane.bearer_token", cfg.ControlPlane.BearerToken); err != nil {
				return fmt.Errorf("%s when the control plane or Agent session is enabled", err.Error())
			}
		}
		if cfg.ControlPlane.Docker.Enabled && len(normalizeStringList(cfg.ControlPlane.Docker.OperationKinds)) == 0 {
			return fmt.Errorf("control_plane.docker.operation_kinds must be explicitly allowlisted in production when docker runner is enabled")
		}
		if cfg.ControlPlane.Docker.Enabled && containsNormalized(cfg.ControlPlane.Docker.OperationKinds, "*") {
			return fmt.Errorf("control_plane.docker.operation_kinds must not use wildcard in production")
		}
		if cfg.ControlPlane.Docker.Enabled {
			for _, kind := range normalizeStringList(cfg.ControlPlane.Docker.OperationKinds) {
				if !knownDockerOperationKind(kind) {
					return fmt.Errorf("control_plane.docker.operation_kinds contains unknown kind %q", kind)
				}
			}
		}
	}
	if err := validateAgentSessionConfig(cfg.ControlPlane); err != nil {
		return err
	}
	if err := validateBuildpacksConfig(cfg); err != nil {
		return err
	}
	if err := validatePersonalEnvironmentConfig(cfg); err != nil {
		return err
	}
	return validateOutpostConfig(cfg.ControlPlane)
}

func validatePersonalEnvironmentConfig(cfg Config) error {
	if !cfg.PersonalEnvironment.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.Auth.BearerToken) == "" {
		return fmt.Errorf("personal_environment requires an Agent bearer token")
	}
	if !pinnedBuildpacksImage.MatchString(cfg.PersonalEnvironment.Image) || !pinnedBuildpacksImage.MatchString(cfg.PersonalEnvironment.QuotaHelperImage) {
		return fmt.Errorf("personal_environment images must use immutable SHA256 digests")
	}
	if cfg.HTTP.WriteTimeout < 35*time.Second {
		return fmt.Errorf("http.write_timeout must be at least 35s when personal_environment is enabled")
	}
	return nil
}

func validateAgentSessionConfig(controlPlane ControlPlaneConfig) error {
	if !controlPlane.Session.Enabled {
		return nil
	}
	baseURL, err := url.Parse(strings.TrimSpace(controlPlane.BaseURL))
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return fmt.Errorf("control_plane.base_url must be an absolute http or https URL when Agent session is enabled")
	}
	if strings.TrimSpace(controlPlane.AgentID) == "" || strings.TrimSpace(controlPlane.BearerToken) == "" {
		return fmt.Errorf("control_plane.agent_id and bearer_token are required when Agent session is enabled")
	}
	runtimeURL, err := url.Parse(strings.TrimSpace(controlPlane.RuntimeEndpoint))
	if err != nil || runtimeURL.Host == "" || runtimeURL.Scheme != "http" {
		return fmt.Errorf("control_plane.runtime_endpoint must be an absolute http URL when Agent session is enabled")
	}
	if controlPlane.Session.ReconnectMin <= 0 || controlPlane.Session.ReconnectMax < controlPlane.Session.ReconnectMin {
		return fmt.Errorf("control_plane.session reconnect bounds are invalid")
	}
	if controlPlane.Session.HandshakeTimeout <= 0 || controlPlane.Session.MaxStreams <= 0 {
		return fmt.Errorf("control_plane.session handshake_timeout and max_streams must be positive")
	}
	return nil
}

func validateOutpostConfig(controlPlane ControlPlaneConfig) error {
	if controlPlane.Outpost.Enabled {
		if !controlPlane.Enabled {
			return fmt.Errorf("control_plane.enabled must be true when control_plane.outpost.enabled is true")
		}
		if strings.TrimSpace(controlPlane.BaseURL) == "" || strings.TrimSpace(controlPlane.BearerToken) == "" {
			return fmt.Errorf("control_plane base_url and bearer_token are required when outpost runtime is enabled")
		}
		if strings.TrimSpace(controlPlane.Outpost.AgentID) == "" {
			return fmt.Errorf("control_plane.outpost.agent_id is required when outpost runtime is enabled")
		}
		if strings.TrimSpace(controlPlane.Outpost.TrustKeyID) == "" || strings.TrimSpace(controlPlane.Outpost.TrustPublicKey) == "" {
			return fmt.Errorf("control_plane.outpost trust_key_id and trust_public_key are required when outpost runtime is enabled")
		}
		if controlPlane.Outpost.ProtocolVersion != "v1" {
			return fmt.Errorf("control_plane.outpost.protocol_version must be v1")
		}
		if key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(controlPlane.Outpost.TrustPublicKey)); err != nil || len(key) != 32 {
			return fmt.Errorf("control_plane.outpost.trust_public_key must be a base64-encoded Ed25519 public key")
		}
	}
	return nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "soha-agent")
	v.SetDefault("app.env", "development")
	v.SetDefault("http.addr", "127.0.0.1:18080")
	v.SetDefault("http.base_path", "/api/v1")
	v.SetDefault("http.read_timeout", "15s")
	v.SetDefault("http.write_timeout", "15s")
	v.SetDefault("http.allowed_origins", []string{})
	v.SetDefault("logger.level", "info")
	v.SetDefault("logger.format", "json")
	v.SetDefault("auth.bearer_token", "")
	v.SetDefault("auth.bearer_token_file", "")
	v.SetDefault("security.allowed_actions", []string{})
	v.SetDefault("docker_runtime.volume_reader_image", "")
	v.SetDefault("docker_runtime.volume_reader_owner", "")
	v.SetDefault("docker_runtime.volume_reader_cleanup_images", []string{})
	v.SetDefault("personal_environment.enabled", false)
	v.SetDefault("personal_environment.image", "")
	v.SetDefault("personal_environment.quota_helper_image", "")
	v.SetDefault("audit.file_path", "")
	v.SetDefault("prometheus.base_url", "")
	v.SetDefault("prometheus.bearer_token", "")
	v.SetDefault("control_plane.enabled", false)
	v.SetDefault("control_plane.base_url", "http://127.0.0.1:8080")
	v.SetDefault("control_plane.bearer_token", "")
	v.SetDefault("control_plane.bearer_token_file", "")
	v.SetDefault("control_plane.agent_id", "local-agent")
	v.SetDefault("control_plane.runtime_endpoint", "http://127.0.0.1:18080")
	v.SetDefault("control_plane.poll_interval", "5s")
	v.SetDefault("control_plane.max_concurrency", 1)
	v.SetDefault("control_plane.default_timeout", "30m")
	v.SetDefault("control_plane.callback_retry.max_attempts", 3)
	v.SetDefault("control_plane.callback_retry.backoff", "500ms")
	v.SetDefault("control_plane.provider_kinds", []string{"ci_agent_runner"})
	v.SetDefault("control_plane.workspace_root", ".")
	setBuildpacksDefaults(v)
	v.SetDefault("control_plane.docker.enabled", false)
	v.SetDefault("control_plane.docker.worker_id", "")
	v.SetDefault("control_plane.docker.host_ids", []string{})
	v.SetDefault("control_plane.docker.operation_kinds", []string{})
	v.SetDefault("control_plane.docker.compose_root", ".soha/docker")
	v.SetDefault("control_plane.docker.poll_interval", "5s")
	v.SetDefault("control_plane.agent_runtime.enabled", false)
	v.SetDefault("control_plane.agent_runtime.worker_id", "")
	v.SetDefault("control_plane.agent_runtime.provider_ids", []string{"hermes"})
	v.SetDefault("control_plane.agent_runtime.provider_kinds", []string{"hermes"})
	v.SetDefault("control_plane.agent_runtime.hermes_command", "hermes")
	v.SetDefault("control_plane.agent_runtime.workspace_root", ".soha/agent-runtime")
	v.SetDefault("control_plane.agent_runtime.poll_interval", "5s")
	v.SetDefault("control_plane.agent_runtime.providers.hermes.command", "hermes")
	v.SetDefault("control_plane.agent_runtime.providers.hermes.args", []string{"chat", "-Q"})
	v.SetDefault("control_plane.agent_runtime.providers.hermes.prompt_arg", "-q")
	v.SetDefault("control_plane.agent_runtime.providers.hermes.skill_arg", "")
	v.SetDefault("control_plane.agent_runtime.providers.hermes.provider_skill_arg", "-s")
	v.SetDefault("control_plane.outpost.enabled", false)
	v.SetDefault("control_plane.outpost.agent_id", "local-outpost-agent")
	v.SetDefault("control_plane.outpost.protocol_version", "v1")
	v.SetDefault("control_plane.outpost.trust_key_id", "")
	v.SetDefault("control_plane.outpost.trust_public_key", "")
	v.SetDefault("control_plane.outpost.poll_interval", "5s")
	v.SetDefault("control_plane.outpost.heartbeat_interval", "15s")
	v.SetDefault("control_plane.session.enabled", false)
	v.SetDefault("control_plane.session.reconnect_min", "1s")
	v.SetDefault("control_plane.session.reconnect_max", "30s")
	v.SetDefault("control_plane.session.handshake_timeout", "15s")
	v.SetDefault("control_plane.session.max_streams", 64)
	v.SetDefault("kubernetes.enabled", true)
	v.SetDefault("kubernetes.id", "local-agent")
	v.SetDefault("kubernetes.name", "Local Agent")
	v.SetDefault("kubernetes.kubeconfig", "$HOME/.kube/config")
	v.SetDefault("kubernetes.context", "")
	v.SetDefault("kubernetes.region", "local")
	v.SetDefault("kubernetes.environment", "development")
	v.SetDefault("kubernetes.labels", map[string]string{
		"provider": "agent",
		"owner":    "platform",
	})
}

func isProductionEnv(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "production" || normalized == "prod"
}

func validateProductionToken(name, value string) error {
	if isUnsafeToken(value) {
		return fmt.Errorf("%s is required in production and must not use an unsafe placeholder token", name)
	}
	if len(strings.TrimSpace(value)) < 32 {
		return fmt.Errorf("%s must be at least 32 characters in production", name)
	}
	return nil
}

func validateListenSecurity(addr, token string) error {
	loopback, err := isLoopbackListenAddress(addr)
	if err != nil {
		return err
	}
	if loopback {
		return nil
	}
	if isUnsafeToken(token) {
		return fmt.Errorf(
			"auth.bearer_token is required when http.addr listens beyond loopback and must not use an unsafe placeholder token",
		)
	}
	if len(strings.TrimSpace(token)) < 32 {
		return fmt.Errorf("auth.bearer_token must be at least 32 characters when http.addr listens beyond loopback")
	}
	return nil
}

func isLoopbackListenAddress(addr string) (bool, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false, nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("http.addr must be a valid host:port address: %w", err)
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback(), nil
}

func isUnsafeToken(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return true
	}
	if strings.HasPrefix(normalized, "demo-") {
		return true
	}
	switch normalized {
	case knownPublicProjectToken, "demo", "changeme", "change-me", "replace-me", "replace-with-runtime-token":
		return true
	default:
		return false
	}
}

func validateProductionAllowedActions(actions []string, allowedOrigins []string) error {
	normalizedActions := normalizeStringList(actions)
	if containsNormalized(normalizedActions, "*") {
		return fmt.Errorf("security.allowed_actions must not use wildcard in production")
	}
	for _, action := range normalizedActions {
		if !knownAction(action) {
			return fmt.Errorf("security.allowed_actions contains unknown action %q", action)
		}
	}
	if containsNormalized(normalizedActions, "docker.runtime.terminal") && len(normalizeStringList(allowedOrigins)) == 0 {
		return fmt.Errorf("http.allowed_origins must be configured in production when docker.runtime.terminal is allowlisted")
	}
	return nil
}

func knownAction(action string) bool {
	switch normalizeActionName(action) {
	case "platform.nodes.drain", "platform.pods.exec",
		"platform.deployments.restart",
		"platform.deployments.scale",
		"platform.deployments.image",
		"platform.deployments.rollback",
		"platform.statefulsets.restart",
		"platform.statefulsets.scale",
		"platform.daemonsets.restart",
		"platform.resources.apply",
		"platform.resources.create",
		"platform.resources.delete",
		"platform.crds.delete",
		"platform.custom_resources.list",
		"platform.custom_resources.create",
		"platform.custom_resources.apply",
		"platform.custom_resources.delete",
		"platform.port_forwards.create",
		"platform.port_forwards.tunnel",
		"platform.port_forwards.delete",
		"platform.helm_releases.install",
		"platform.helm_releases.values_update",
		"platform.helm_releases.delete",
		"platform.helm_releases.rollback",
		"runtime.execution_tasks.cancel", "runtime.vm_ssh.connect", "runtime.vm_ssh.execute",
		"runtime.personal_environments.acquire",
		"runtime.personal_environments.preflight",
		"runtime.personal_environments.inspect",
		"runtime.personal_environments.execute",
		"runtime.personal_environments.release",
		"runtime.personal_environments.delete_data", "runtime.personal_environments.export_data",
		"docker.runtime.terminal", "docker.runtime.terminal.v1", "docker.runtime.logs", "docker.runtime.files",
		"docker.runtime.logs.v1", "docker.runtime.files.v1":
		return true
	default:
		return false
	}
}

func knownDockerOperationKind(kind string) bool {
	switch normalizeActionName(kind) {
	case "host_provision", "container_start", "project_deploy", "service_action", "port_reserve", "host_sync":
		return true
	default:
		return false
	}
}

func containsNormalized(values []string, needle string) bool {
	needle = normalizeActionName(needle)
	for _, value := range values {
		if normalizeActionName(value) == needle {
			return true
		}
	}
	return false
}

func normalizeActionName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := normalizeActionName(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}
