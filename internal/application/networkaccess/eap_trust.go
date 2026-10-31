package networkaccess

import (
	"crypto/x509"
	"encoding/pem"
	"regexp"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

var eapServerLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

func ValidateEAPServerTrust(trust *domain.EAPServerTrust) error {
	if trust == nil {
		return nil
	}
	if !trust.WifiSecurity.Valid() || len(trust.ServerNames) < 1 || len(trust.ServerNames) > 16 || len(trust.CaCertificates) < 1 || len(trust.CaCertificates) > 8 {
		return invalid("eapServerTrust is invalid")
	}
	if err := validateEAPServerNames(trust.ServerNames); err != nil {
		return err
	}
	for _, raw := range trust.CaCertificates {
		if err := validateEAPCACertificate(raw); err != nil {
			return err
		}
	}
	return nil
}

func validateEAPServerNames(names []string) error {
	seen := make(map[string]bool)
	for _, name := range names {
		if len(name) > 253 || seen[strings.ToLower(name)] {
			return invalid("eapServerTrust server names must be exact and unique")
		}
		for _, label := range strings.Split(name, ".") {
			if len(label) > 63 || !eapServerLabel.MatchString(label) {
				return invalid("eapServerTrust server names must be exact DNS names")
			}
		}
		seen[strings.ToLower(name)] = true
	}
	return nil
}

func validateEAPCACertificate(raw string) error {
	if len(raw) > 16384 {
		return invalid("eapServerTrust certificate is too large")
	}
	block, rest := pem.Decode([]byte(raw))
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 || len(block.Headers) != 0 {
		return invalid("eapServerTrust requires public CA certificates only")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	now := time.Now()
	if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid || certificate.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
		return invalid("eapServerTrust CA certificate is invalid or expired")
	}
	return nil
}
