package networkidentity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"strings"
	"testing"
)

func TestParseCertificateIdentityScopesRuntimeCredential(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	identityURI, _ := url.Parse("spiffe://opensoha.local/network-control/endpoint/endpoint-1")
	certificate := &x509.Certificate{
		Raw:                     []byte("certificate-der"),
		RawSubjectPublicKeyInfo: []byte("public-key-der"),
		PublicKey:               publicKey,
		URIs:                    []*url.URL{identityURI},
	}

	identity, err := ParseCertificate(certificate, ScopeNetworkControl)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	if identity.Kind != "endpoint" || identity.ID != "endpoint-1" {
		t.Fatalf("identity = %#v", identity)
	}
	if !strings.HasPrefix(identity.CertificateFingerprint, "sha256:") {
		t.Fatalf("certificate fingerprint = %q", identity.CertificateFingerprint)
	}
	if _, err := ParseCertificate(certificate, ScopeIngest); err == nil {
		t.Fatal("control certificate accepted for ingest")
	}

	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}
	publicKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
	if err := MatchPublicKey(certificate, publicKeyPEM); err != nil {
		t.Fatalf("MatchPublicKey() error = %v", err)
	}
}

func TestParseCertificateRejectsAmbiguousSohaIdentity(t *testing.T) {
	controlURI, _ := url.Parse("spiffe://opensoha.local/network-control/gateway/gateway-1")
	ingestURI, _ := url.Parse("spiffe://opensoha.local/network-ingest/gateway/gateway-1")
	certificate := &x509.Certificate{URIs: []*url.URL{controlURI, ingestURI}}
	if _, err := ParseCertificate(certificate, ScopeNetworkControl); err == nil {
		t.Fatal("ambiguous certificate identity accepted")
	}
}

func TestParseCertificateAcceptsDedicatedIngestCoreReader(t *testing.T) {
	identityURI, _ := url.Parse("spiffe://opensoha.local/network-ingest/core/soha-server")
	certificate := &x509.Certificate{Raw: []byte("certificate-der"), RawSubjectPublicKeyInfo: []byte("public-key-der"), URIs: []*url.URL{identityURI}}
	identity, err := ParseCertificate(certificate, ScopeIngest)
	if err != nil || identity.Kind != "core" || identity.ID != "soha-server" {
		t.Fatalf("ParseCertificate() = %#v, %v", identity, err)
	}
}
