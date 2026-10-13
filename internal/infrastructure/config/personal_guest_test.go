package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestPersonalGuestEnvironmentOverridesAreBoundWithoutYAMLSection(t *testing.T) {
	t.Setenv("SOHA_PERSONAL_GUEST_ENABLED", "true")
	t.Setenv("SOHA_PERSONAL_GUEST_ADDR", "127.0.0.1:8445")
	t.Setenv("SOHA_PERSONAL_GUEST_TLS_CERT_FILE", "/run/guest/server.crt")
	t.Setenv("SOHA_PERSONAL_GUEST_TLS_KEY_FILE", "/run/guest/server.key")
	t.Setenv("SOHA_PERSONAL_GUEST_TLS_CLIENT_CA_FILE", "/run/guest/client-ca.crt")
	t.Setenv("SOHA_PERSONAL_GUEST_ENDPOINT", "https://guest.example:8445")
	t.Setenv("SOHA_PERSONAL_GUEST_ISSUER_KEY_FILE", "/run/guest/issuer.key")
	t.Setenv("SOHA_PERSONAL_GUEST_ARTIFACT_FILE", "/run/guest/personal-guest")
	t.Setenv("SOHA_PERSONAL_GUEST_CERTIFICATE_TTL_SECONDS", "300")
	v := viper.New()
	v.SetEnvPrefix("SOHA")
	v.SetEnvKeyReplacer(stringsReplacer())
	v.AutomaticEnv()
	setDefaults(v)
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.PersonalGuest.Enabled || cfg.PersonalGuest.Addr != "127.0.0.1:8445" || cfg.PersonalGuest.TLS.ClientCAFile != "/run/guest/client-ca.crt" || cfg.PersonalGuest.Endpoint != "https://guest.example:8445" || cfg.PersonalGuest.IssuerKeyFile != "/run/guest/issuer.key" || cfg.PersonalGuest.ArtifactFile != "/run/guest/personal-guest" || cfg.PersonalGuest.CertificateTTLSeconds != 300 || cfg.PersonalGuest.Validate() != nil {
		t.Fatal("guest environment overrides were not bound")
	}
}

func TestPersonalGuestPreparationConfigRequiresPairedHTTPSAndIssuer(t *testing.T) {
	valid := PersonalGuestConfig{Enabled: true, Addr: ":8444", TLS: MutualTLSConfig{CertFile: "/run/guest/server.crt", KeyFile: "/run/guest/server.key", ClientCAFile: "/run/guest/client-ca.crt"}, Endpoint: "https://guest.example:8444", IssuerKeyFile: "/run/guest/issuer.key"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*PersonalGuestConfig)
	}{
		{"endpoint_only", func(c *PersonalGuestConfig) { c.IssuerKeyFile = "" }},
		{"issuer_only", func(c *PersonalGuestConfig) { c.Endpoint = "" }},
		{"http", func(c *PersonalGuestConfig) { c.Endpoint = "http://guest.example" }},
		{"url_credentials", func(c *PersonalGuestConfig) { c.Endpoint = "https://user:secret@guest.example" }},
		{"path", func(c *PersonalGuestConfig) { c.Endpoint += "/path" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.edit(&cfg)
			if cfg.Validate() == nil {
				t.Fatal("incomplete or unsafe issuer configuration accepted")
			}
		})
	}
}

func TestPersonalGuestConfigRequiresExplicitAddressAndTLS(t *testing.T) {
	if err := (PersonalGuestConfig{}).Validate(); err != nil {
		t.Fatal("disabled guest changed existing startup", err)
	}
	valid := PersonalGuestConfig{Enabled: true, Addr: ":8444", TLS: MutualTLSConfig{CertFile: "/run/guest/server.crt", KeyFile: "/run/guest/server.key", ClientCAFile: "/run/guest/client-ca.crt"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*PersonalGuestConfig){
		func(c *PersonalGuestConfig) { c.Addr = "" },
		func(c *PersonalGuestConfig) { c.Addr = ":0" },
		func(c *PersonalGuestConfig) { c.Addr = ":65536" },
		func(c *PersonalGuestConfig) { c.TLS.CertFile = "" },
		func(c *PersonalGuestConfig) { c.TLS.KeyFile = "" },
		func(c *PersonalGuestConfig) { c.TLS.ClientCAFile = "" },
	} {
		cfg := valid
		edit(&cfg)
		if cfg.Validate() == nil {
			t.Fatal("incomplete guest TLS config accepted")
		}
	}
}
