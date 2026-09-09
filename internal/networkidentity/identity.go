package networkidentity

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"regexp"
	"strings"
)

const (
	ScopeNetworkControl = "network-control"
	ScopeIngest         = "network-ingest"
	trustDomain         = "opensoha.local"
)

var identityIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Identity struct {
	Scope                  string
	Kind                   string
	ID                     string
	CertificateFingerprint string
	PublicKeyFingerprint   string
	Certificate            *x509.Certificate
}

func ParseCertificate(certificate *x509.Certificate, scope string) (Identity, error) {
	if certificate == nil {
		return Identity{}, fmt.Errorf("client certificate is required")
	}
	var segments []string
	identities := 0
	for _, candidate := range certificate.URIs {
		if candidate == nil || candidate.Scheme != "spiffe" || candidate.Host != trustDomain {
			continue
		}
		identities++
		segments = strings.Split(strings.Trim(candidate.EscapedPath(), "/"), "/")
	}
	if identities != 1 {
		return Identity{}, fmt.Errorf("client certificate must contain exactly one Soha URI identity")
	}
	if len(segments) != 3 || segments[0] != scope {
		return Identity{}, fmt.Errorf("client certificate is not scoped for %s", scope)
	}
	kind, id := segments[1], segments[2]
	if !allowedKind(scope, kind) || !identityIDPattern.MatchString(id) {
		return Identity{}, fmt.Errorf("client certificate contains an invalid %s identity", scope)
	}
	certificateDigest := sha256.Sum256(certificate.Raw)
	publicKeyDigest := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	return Identity{
		Scope:                  scope,
		Kind:                   kind,
		ID:                     id,
		CertificateFingerprint: fmt.Sprintf("sha256:%x", certificateDigest),
		PublicKeyFingerprint:   fmt.Sprintf("sha256:%x", publicKeyDigest),
		Certificate:            certificate,
	}, nil
}

func MatchPublicKey(certificate *x509.Certificate, encoded string) error {
	if certificate == nil {
		return fmt.Errorf("client certificate is required")
	}
	block, rest := pem.Decode([]byte(encoded))
	if block == nil || block.Type != "PUBLIC KEY" || strings.TrimSpace(string(rest)) != "" {
		return fmt.Errorf("devicePublicKey must contain one PKIX PUBLIC KEY PEM block")
	}
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse devicePublicKey: %w", err)
	}
	want, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return fmt.Errorf("marshal certificate public key: %w", err)
	}
	got, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return fmt.Errorf("marshal device public key: %w", err)
	}
	if len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
		return fmt.Errorf("devicePublicKey does not match the client certificate")
	}
	return nil
}

func allowedKind(scope, kind string) bool {
	switch scope {
	case ScopeNetworkControl:
		return kind == "endpoint" || kind == "gateway" || kind == "nas"
	case ScopeIngest:
		return kind == "endpoint" || kind == "gateway" || kind == "freeradius" || kind == "network-control" || kind == "core"
	default:
		return false
	}
}
