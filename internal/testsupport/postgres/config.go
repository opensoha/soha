// Package postgres configures disposable integration databases independently of development data.
package postgres

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/infrastructure/config"
)

// Configure preserves fixture defaults while allowing an explicitly owned private PG instance.
func Configure(t testing.TB, cfg config.DatabaseConfig) config.DatabaseConfig {
	t.Helper()
	updated, err := configure(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func configure(cfg config.DatabaseConfig) (config.DatabaseConfig, error) {
	if host := strings.TrimSpace(os.Getenv("SOHA_TEST_POSTGRES_HOST")); host != "" {
		cfg.Host = host
	}
	password := os.Getenv("SOHA_TEST_POSTGRES_PASSWORD")
	if password != "" {
		cfg.Password = password
	} else if cfg.Host != "127.0.0.1" && cfg.Host != "localhost" && cfg.Host != "::1" {
		return config.DatabaseConfig{}, fmt.Errorf("non-loopback test PG requires SOHA_TEST_POSTGRES_PASSWORD")
	}
	return cfg, nil
}
