package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestResolveMigrationPathUsesDriverDirectoryForMigrationRoot(t *testing.T) {
	root := t.TempDir()
	postgresDir := filepath.Join(root, "postgres")
	if err := os.MkdirAll(postgresDir, 0o755); err != nil {
		t.Fatalf("mkdir postgres dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(postgresDir, "0001_init.sql"), []byte("-- init"), 0o644); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	if err := os.WriteFile(filepath.Join(postgresDir, "0011_application_services.sql"), []byte("-- app services"), 0o644); err != nil {
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
	if err := os.WriteFile(legacyPath, []byte("-- legacy"), 0o644); err != nil {
		t.Fatalf("write legacy migration: %v", err)
	}
	driverInit := filepath.Join(postgresDir, "0001_init.sql")
	if err := os.WriteFile(driverInit, []byte("-- driver"), 0o644); err != nil {
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
	if cfg.Auth.LoginVerification.SliderEnabled {
		t.Fatal("login slider verification should be disabled by default")
	}
}
