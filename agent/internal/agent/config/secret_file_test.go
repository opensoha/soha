package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadResolvesControlPlaneBearerTokenFile(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "runner-token")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create token directory: %v", err)
	}
	tokenPath := filepath.Join(directory, "token")
	if err := os.WriteFile(tokenPath, []byte(productionControlPlaneToken+"\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "agent.config.yaml")
	configData := "app:\n  env: production\n" +
		"auth:\n  bearer_token: " + productionAgentToken + "\n" +
		"control_plane:\n  enabled: true\n  bearer_token_file: " + tokenPath + "\n" +
		"kubernetes:\n  enabled: false\n"
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("SOHA_AGENT_CONFIG_FILE", configPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ControlPlane.BearerToken != productionControlPlaneToken {
		t.Fatal("control plane bearer token was not loaded from the projection file")
	}
}

func TestReadBearerTokenFileRejectsUnsafeFiles(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T) string
		wantErr string
	}{
		{
			name: "relative path",
			prepare: func(*testing.T) string {
				return "relative-token"
			},
			wantErr: "absolute",
		},
		{
			name: "broad file permissions",
			prepare: func(t *testing.T) string {
				directory := secureTokenDirectory(t)
				path := filepath.Join(directory, "token")
				//nolint:gosec // Intentionally broad permissions exercise fail-closed validation.
				if err := os.WriteFile(path, []byte(productionControlPlaneToken), 0o644); err != nil {
					t.Fatalf("write broad token file: %v", err)
				}
				return path
			},
			wantErr: "too broad",
		},
		{
			name: "symlink",
			prepare: func(t *testing.T) string {
				directory := secureTokenDirectory(t)
				target := filepath.Join(directory, "target")
				if err := os.WriteFile(target, []byte(productionControlPlaneToken), 0o600); err != nil {
					t.Fatalf("write token target: %v", err)
				}
				path := filepath.Join(directory, "token")
				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("create token symlink: %v", err)
				}
				return path
			},
			wantErr: "not a symlink",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := readBearerTokenFile(test.prepare(t))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("readBearerTokenFile() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestResolveControlPlaneBearerTokenRejectsAmbiguousSources(t *testing.T) {
	cfg := Config{ControlPlane: ControlPlaneConfig{
		BearerToken:     productionControlPlaneToken,
		BearerTokenFile: "/tmp/runner-token",
	}}
	if err := resolveBearerTokenFiles(&cfg); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("resolveBearerTokenFiles() error = %v, want ambiguous-source rejection", err)
	}
}

func secureTokenDirectory(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "runner-token")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create token directory: %v", err)
	}
	return directory
}
