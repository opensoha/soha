package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadNetworkControlConfigIgnoresManagementSecrets(t *testing.T) {
	path := writeRuntimeConfig(t, `
auth:
  enable_dev_auth: true
network_control:
  tls:
    cert_file: /run/secrets/control.crt
    key_file: /run/secrets/control.key
    client_ca_file: /run/secrets/runtime-ca.crt
`)
	t.Setenv("SOHA_CONFIG_FILE", path)
	t.Setenv("SOHA_NETWORK_CONTROL_DATABASE_PASSWORD", "control-password")
	t.Setenv("SOHA_SECURITY_CREDENTIAL_ENCRYPTION_KEY", "control-encryption-key-with-32-characters")

	cfg, err := LoadNetworkControlConfig()
	if err != nil {
		t.Fatalf("LoadNetworkControlConfig() error = %v", err)
	}
	if cfg.HTTP.Addr != ":8082" {
		t.Fatalf("HTTP.Addr = %q, want :8082", cfg.HTTP.Addr)
	}
	if cfg.Database.Password != "control-password" {
		t.Fatalf("Database.Password = %q, want environment override", cfg.Database.Password)
	}
	if cfg.SnapshotRefreshInterval.String() != "1s" {
		t.Fatalf("SnapshotRefreshInterval = %s, want 1s", cfg.SnapshotRefreshInterval)
	}
	if cfg.MaxClockSkew.String() != "5m0s" || cfg.ConfigurationTTL.String() != "5m0s" || cfg.LeaseTTL.String() != "5m0s" {
		t.Fatalf("control windows = skew:%s configuration:%s lease:%s", cfg.MaxClockSkew, cfg.ConfigurationTTL, cfg.LeaseTTL)
	}
	if cfg.CredentialEncryptionKeys.Active().ID() != encryptionKeyID || !cfg.CredentialEncryptionKeys.Match("control-encryption-key-with-32-characters", time.Now()) {
		t.Fatal("network-control did not load its credential encryption keyring")
	}
}

func TestLoadNetworkControlConfigRejectsUnboundedLeaseTTL(t *testing.T) {
	path := writeRuntimeConfig(t, `
network_control:
  tls:
    cert_file: /run/secrets/control.crt
    key_file: /run/secrets/control.key
    client_ca_file: /run/secrets/runtime-ca.crt
  lease_ttl: 6m
`)
	t.Setenv("SOHA_CONFIG_FILE", path)

	if _, err := LoadNetworkControlConfig(); err == nil || !strings.Contains(err.Error(), "network_control.lease_ttl") {
		t.Fatalf("LoadNetworkControlConfig() error = %v, want bounded lease TTL failure", err)
	}
}

func TestLoadIngestConfigUsesIndependentDatabase(t *testing.T) {
	path := writeRuntimeConfig(t, `
ingest:
  tls:
    cert_file: /run/secrets/ingest.crt
    key_file: /run/secrets/ingest.key
    client_ca_file: /run/secrets/ingest-ca.crt
`)
	t.Setenv("SOHA_CONFIG_FILE", path)

	cfg, err := LoadIngestConfig()
	if err != nil {
		t.Fatalf("LoadIngestConfig() error = %v", err)
	}
	if cfg.Database.Name != "soha_ingest" {
		t.Fatalf("Database.Name = %q, want soha_ingest", cfg.Database.Name)
	}
	if cfg.Database.MigrationPath != "migrations/network-ingest/postgres" {
		t.Fatalf("Database.MigrationPath = %q", cfg.Database.MigrationPath)
	}
	if cfg.HTTP.Addr != ":8083" {
		t.Fatalf("HTTP.Addr = %q, want :8083", cfg.HTTP.Addr)
	}
}

func TestLoadNetworkRuntimeConfigRequiresMutualTLS(t *testing.T) {
	path := writeRuntimeConfig(t, "network_control: {}\n")
	t.Setenv("SOHA_CONFIG_FILE", path)

	_, err := LoadNetworkControlConfig()
	if err == nil {
		t.Fatal("LoadNetworkControlConfig() error = nil, want mTLS validation failure")
	}
	for _, field := range []string{"network_control.tls.cert_file", "network_control.tls.key_file", "network_control.tls.client_ca_file"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("error %q does not mention %s", err, field)
		}
	}
}

func TestLoadNetworkRuntimeTLSFromEnvironment(t *testing.T) {
	for _, kind := range []string{"network_control", "ingest"} {
		for _, content := range []string{"{}\n", kind + ":\n  tls:\n    cert_file: /old/cert\n    key_file: /old/key\n    client_ca_file: /old/ca\n"} {
			t.Run(kind+"/"+strings.SplitN(content, "\n", 2)[0], func(t *testing.T) {
				t.Setenv("SOHA_CONFIG_FILE", writeRuntimeConfig(t, content))
				prefix := "SOHA_" + strings.ToUpper(kind) + "_TLS_"
				t.Setenv(prefix+"CERT_FILE", "/run/runtime/tls.crt")
				t.Setenv(prefix+"KEY_FILE", "/run/runtime/tls.key")
				t.Setenv(prefix+"CLIENT_CA_FILE", "/run/runtime/ca.crt")
				var actual MutualTLSConfig
				if kind == "network_control" {
					cfg, err := LoadNetworkControlConfig()
					if err != nil {
						t.Fatal(err)
					}
					actual = cfg.TLS
				} else {
					cfg, err := LoadIngestConfig()
					if err != nil {
						t.Fatal(err)
					}
					actual = cfg.TLS
				}
				if actual != (MutualTLSConfig{CertFile: "/run/runtime/tls.crt", KeyFile: "/run/runtime/tls.key", ClientCAFile: "/run/runtime/ca.crt"}) {
					t.Fatalf("TLS environment not applied: %+v", actual)
				}
			})
		}
	}
}

func TestLoadNetworkRuntimeTLSRejectsMissingEnvironmentField(t *testing.T) {
	for _, kind := range []string{"network_control", "ingest"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("SOHA_CONFIG_FILE", writeRuntimeConfig(t, "{}\n"))
			prefix := "SOHA_" + strings.ToUpper(kind) + "_TLS_"
			t.Setenv(prefix+"CERT_FILE", "/run/runtime/tls.crt")
			t.Setenv(prefix+"KEY_FILE", "/run/runtime/tls.key")
			t.Setenv(prefix+"CLIENT_CA_FILE", "")
			var err error
			if kind == "network_control" {
				_, err = LoadNetworkControlConfig()
			} else {
				_, err = LoadIngestConfig()
			}
			if err == nil || !strings.Contains(err.Error(), kind+".tls.client_ca_file is required") {
				t.Fatalf("missing client CA error = %v", err)
			}
		})
	}
}

func writeRuntimeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	return path
}
