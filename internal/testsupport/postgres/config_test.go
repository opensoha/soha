package postgres

import (
	"testing"

	"github.com/opensoha/soha/internal/infrastructure/config"
)

func TestDisposablePostgresConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, host, password string
		invalid              bool
	}{
		{name: "legacy_loopback"},
		{name: "private_random_credential", host: "10.0.0.18", password: "owned-random-fixture"},
		{name: "private_requires_credential", host: "10.0.0.18", invalid: true},
		{name: "loopback_override", host: "127.0.0.1", password: "owned-random-fixture"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SOHA_TEST_POSTGRES_HOST", test.host)
			t.Setenv("SOHA_TEST_POSTGRES_PASSWORD", test.password)
			base := config.DatabaseConfig{Host: "127.0.0.1", Port: 32123, Name: "owned-fixture", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 12, MaxIdleConns: 4}
			got, err := configure(base)
			if (err != nil) != test.invalid {
				t.Fatalf("unexpected configuration error: %v", err)
			}
			if test.invalid {
				return
			}
			if test.host != "" {
				base.Host = test.host
			}
			if test.password != "" {
				base.Password = test.password
			}
			if got != base {
				t.Fatal("fixture configuration changed unrelated connection fields")
			}
		})
	}
}
