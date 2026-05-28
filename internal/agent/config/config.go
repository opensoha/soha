package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

type Config struct {
	App          AppConfig          `mapstructure:"app"`
	HTTP         HTTPConfig         `mapstructure:"http"`
	Logger       LoggerConfig       `mapstructure:"logger"`
	Auth         AuthConfig         `mapstructure:"auth"`
	ControlPlane ControlPlaneConfig `mapstructure:"control_plane"`
	Kubernetes   KubernetesConfig   `mapstructure:"kubernetes"`
}

type AppConfig struct {
	Name string `mapstructure:"name"`
	Env  string `mapstructure:"env"`
}

type HTTPConfig struct {
	Addr         string        `mapstructure:"addr"`
	BasePath     string        `mapstructure:"base_path"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type LoggerConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type AuthConfig struct {
	BearerToken string `mapstructure:"bearer_token"`
}

type ControlPlaneConfig struct {
	Enabled         bool               `mapstructure:"enabled"`
	BaseURL         string             `mapstructure:"base_url"`
	BearerToken     string             `mapstructure:"bearer_token"`
	AgentID         string             `mapstructure:"agent_id"`
	RuntimeEndpoint string             `mapstructure:"runtime_endpoint"`
	PollInterval    time.Duration      `mapstructure:"poll_interval"`
	ProviderKinds   []string           `mapstructure:"provider_kinds"`
	WorkspaceRoot   string             `mapstructure:"workspace_root"`
	Docker          DockerRunnerConfig `mapstructure:"docker"`
	AgentRuntime    AgentRuntimeConfig `mapstructure:"agent_runtime"`
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
	Enabled       bool                           `mapstructure:"enabled"`
	WorkerID      string                         `mapstructure:"worker_id"`
	ProviderIDs   []string                       `mapstructure:"provider_ids"`
	ProviderKinds []string                       `mapstructure:"provider_kinds"`
	HermesCommand string                         `mapstructure:"hermes_command"`
	WorkspaceRoot string                         `mapstructure:"workspace_root"`
	PollInterval  time.Duration                  `mapstructure:"poll_interval"`
	Providers     map[string]AgentProviderConfig `mapstructure:"providers"`
}

type AgentProviderConfig struct {
	Command          string   `mapstructure:"command"`
	Args             []string `mapstructure:"args"`
	PromptArg        string   `mapstructure:"prompt_arg"`
	SkillArg         string   `mapstructure:"skill_arg"`
	ProviderSkillArg string   `mapstructure:"provider_skill_arg"`
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
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("SOHA_AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	setDefaults(v)

	configFile := os.Getenv("SOHA_AGENT_CONFIG_FILE")
	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		v.SetConfigName("agent.config")
		v.AddConfigPath("configs")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
	}
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
	))); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.Kubernetes.Kubeconfig = os.ExpandEnv(cfg.Kubernetes.Kubeconfig)
	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "soha-agent")
	v.SetDefault("app.env", "development")
	v.SetDefault("http.addr", ":18080")
	v.SetDefault("http.base_path", "/api/v1")
	v.SetDefault("http.read_timeout", "15s")
	v.SetDefault("http.write_timeout", "15s")
	v.SetDefault("logger.level", "debug")
	v.SetDefault("logger.format", "console")
	v.SetDefault("auth.bearer_token", "")
	v.SetDefault("control_plane.enabled", false)
	v.SetDefault("control_plane.base_url", "http://127.0.0.1:8080")
	v.SetDefault("control_plane.bearer_token", "")
	v.SetDefault("control_plane.agent_id", "local-agent")
	v.SetDefault("control_plane.runtime_endpoint", "http://127.0.0.1:18080")
	v.SetDefault("control_plane.poll_interval", "5s")
	v.SetDefault("control_plane.provider_kinds", []string{"ci_agent_runner"})
	v.SetDefault("control_plane.workspace_root", ".")
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
