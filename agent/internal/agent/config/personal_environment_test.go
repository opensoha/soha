package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestPersonalEnvironmentConfigRequiresAuthPinnedImagesAndReceiptTimeout(t *testing.T) {
	image := "registry.example/fixture@sha256:" + strings.Repeat("a", 64)
	base := Config{App: AppConfig{Env: "production"}, Auth: AuthConfig{BearerToken: strings.Repeat("b", 48)},
		HTTP:                HTTPConfig{Addr: "127.0.0.1:18080", WriteTimeout: 35 * time.Second},
		PersonalEnvironment: PersonalEnvironmentConfig{Enabled: true, Image: image, QuotaHelperImage: image},
		Security:            SecurityConfig{AllowedActions: []string{"runtime.personal_environments.acquire", "runtime.personal_environments.inspect", "runtime.personal_environments.execute", "runtime.personal_environments.release", "runtime.personal_environments.delete_data", "runtime.personal_environments.export_data"}}}
	if err := Validate(base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.Auth.BearerToken = "" },
		func(c *Config) { c.PersonalEnvironment.Image = "fixture:latest" },
		func(c *Config) { c.PersonalEnvironment.QuotaHelperImage = "fixture:latest" },
		func(c *Config) { c.HTTP.WriteTimeout = 15 * time.Second },
	} {
		c := base
		change(&c)
		if Validate(c) == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
}

func TestPersonalEnvironmentEnvOverrides(t *testing.T) {
	image := "registry.example/fixture@sha256:" + strings.Repeat("a", 64)
	configPath := t.TempDir() + "/agent.yaml"
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOHA_AGENT_CONFIG_FILE", configPath)
	t.Setenv("SOHA_AGENT_PERSONAL_ENVIRONMENT_ENABLED", "true")
	t.Setenv("SOHA_AGENT_PERSONAL_ENVIRONMENT_IMAGE", image)
	t.Setenv("SOHA_AGENT_PERSONAL_ENVIRONMENT_QUOTA_HELPER_IMAGE", image)
	t.Setenv("SOHA_AGENT_AUTH_BEARER_TOKEN", strings.Repeat("b", 48))
	t.Setenv("SOHA_AGENT_HTTP_WRITE_TIMEOUT", "35s")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PersonalEnvironment.Enabled || cfg.PersonalEnvironment.Image != image || cfg.PersonalEnvironment.QuotaHelperImage != image {
		t.Fatal("personal environment env overrides not loaded")
	}
}
