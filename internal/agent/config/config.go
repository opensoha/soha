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
	App        AppConfig        `mapstructure:"app"`
	HTTP       HTTPConfig       `mapstructure:"http"`
	Logger     LoggerConfig     `mapstructure:"logger"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Kubernetes KubernetesConfig `mapstructure:"kubernetes"`
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

type KubernetesConfig struct {
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
	v.SetEnvPrefix("KC_AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	setDefaults(v)

	configFile := os.Getenv("KC_AGENT_CONFIG_FILE")
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
	v.SetDefault("app.name", "kubecrux-agent")
	v.SetDefault("app.env", "development")
	v.SetDefault("http.addr", ":18080")
	v.SetDefault("http.base_path", "/api/v1")
	v.SetDefault("http.read_timeout", "15s")
	v.SetDefault("http.write_timeout", "15s")
	v.SetDefault("logger.level", "debug")
	v.SetDefault("logger.format", "console")
	v.SetDefault("auth.bearer_token", "")
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
