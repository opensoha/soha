package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

type PersonalGuestConfig struct {
	Enabled               bool            `mapstructure:"enabled"`
	Addr                  string          `mapstructure:"addr"`
	TLS                   MutualTLSConfig `mapstructure:"tls"`
	Endpoint              string          `mapstructure:"endpoint"`
	IssuerKeyFile         string          `mapstructure:"issuer_key_file"`
	ArtifactFile          string          `mapstructure:"artifact_file"`
	CertificateTTLSeconds int             `mapstructure:"certificate_ttl_seconds"`
}

func (c PersonalGuestConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	_, portText, err := net.SplitHostPort(c.Addr)
	port, portErr := strconv.Atoi(portText)
	if err != nil || portErr != nil || port < 1 || port > 65535 || strings.TrimSpace(c.Addr) != c.Addr {
		return fmt.Errorf("personal_guest.addr must be a valid listen address")
	}
	for _, value := range []string{c.TLS.CertFile, c.TLS.KeyFile, c.TLS.ClientCAFile} {
		if value == "" || strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("personal_guest requires server certificate, private key and dedicated client CA files")
		}
	}
	return c.validatePreparation()
}

func (c PersonalGuestConfig) validatePreparation() error {
	if c.CertificateTTLSeconds != 0 && (c.CertificateTTLSeconds < 300 || c.CertificateTTLSeconds > 3600) {
		return fmt.Errorf("personal guest certificate TTL must be 300–3600 seconds")
	}
	if c.ArtifactFile != "" && (c.IssuerKeyFile == "" || !filepath.IsAbs(c.ArtifactFile) || filepath.Clean(c.ArtifactFile) != c.ArtifactFile) {
		return fmt.Errorf("personal guest provisioning requires an absolute artifact path and issuer")
	}
	if c.Endpoint != "" || c.IssuerKeyFile != "" {
		return c.validatePreparationEndpoint()
	}
	return nil
}

func (c PersonalGuestConfig) validatePreparationEndpoint() error {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || len(c.Endpoint) > 512 || strings.TrimSpace(c.Endpoint) != c.Endpoint || c.IssuerKeyFile == "" || strings.TrimSpace(c.IssuerKeyFile) != c.IssuerKeyFile {
		return fmt.Errorf("personal guest preparation requires an explicit HTTPS endpoint and issuer private key")
	}
	return nil
}
