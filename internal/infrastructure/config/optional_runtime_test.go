package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOptionalRuntimeLoadEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("auth:\n  enable_dev_auth: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOHA_CONFIG_FILE", path)
	for key, value := range map[string]string{
		"SOHA_DSH_RUNTIME_ENABLED": "true", "SOHA_DSH_RUNTIME_BASE_URL": "http://dsh:9000", "SOHA_DSH_RUNTIME_SERVICE_TOKEN_FILE": "/token", "SOHA_DSH_RUNTIME_ALLOW_INSECURE_HTTP": "true", "SOHA_DSH_RUNTIME_TIMEOUT": "45s",
		"SOHA_HINDSIGHT_ENABLED": "true", "SOHA_HINDSIGHT_BASE_URL": "http://hindsight:8888", "SOHA_HINDSIGHT_TOKEN_FILE": "/memory-token", "SOHA_HINDSIGHT_INSTANCE_NAMESPACE": "instance", "SOHA_HINDSIGHT_TIMEOUT": "20s",
	} {
		t.Setenv(key, value)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DSHRuntime.Enabled || cfg.DSHRuntime.BaseURL != "http://dsh:9000" || cfg.DSHRuntime.ServiceTokenFile != "/token" || !cfg.DSHRuntime.AllowInsecureHTTP || cfg.DSHRuntime.Timeout != 45*time.Second {
		t.Fatalf("DSH settings not loaded: %+v", cfg.DSHRuntime)
	}
	if !cfg.Hindsight.Enabled || cfg.Hindsight.BaseURL != "http://hindsight:8888" || cfg.Hindsight.TokenFile != "/memory-token" || cfg.Hindsight.InstanceNamespace != "instance" || cfg.Hindsight.Timeout != 20*time.Second {
		t.Fatalf("Hindsight settings not loaded: %+v", cfg.Hindsight)
	}
}
func TestOptionalRuntimeValidation(t *testing.T) {
	if len((DSHRuntimeConfig{}).problems()) != 0 || len((HindsightConfig{}).problems()) != 0 {
		t.Fatal("disabled settings must be optional")
	}
	if len((DSHRuntimeConfig{Enabled: true}).problems()) == 0 || len((HindsightConfig{Enabled: true}).problems()) == 0 {
		t.Fatal("enabled settings must be complete")
	}
	if len((DSHRuntimeConfig{Enabled: true, BaseURL: "http://private:9000", ServiceTokenFile: "/token", Timeout: time.Second}).problems()) == 0 {
		t.Fatal("private HTTP requires explicit opt-in")
	}
}
