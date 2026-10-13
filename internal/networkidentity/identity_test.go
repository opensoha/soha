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

func TestPersonalGuestCertificateCannotEnterNetworkControlOrIngest(t *testing.T) {
	guestURI, err := url.Parse("spiffe://opensoha.local/personal-guest/guest/00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{Raw: []byte("guest-certificate"), RawSubjectPublicKeyInfo: []byte("guest-public-key"), URIs: []*url.URL{guestURI}}
	identity, err := ParseCertificate(certificate, ScopePersonalGuest)
	if err != nil || identity.Scope != ScopePersonalGuest || identity.Kind != "guest" {
		t.Fatalf("dedicated guest identity: %+v %v", identity, err)
	}
	for _, scope := range []string{ScopeNetworkControl, ScopeIngest} {
		if _, err := ParseCertificate(certificate, scope); err == nil {
			t.Fatalf("guest certificate accepted for %s", scope)
		}
	}
	for _, raw := range []string{
		"spiffe://opensoha.local/network-control/endpoint/endpoint-1",
		"spiffe://opensoha.local/network-ingest/core/soha-server",
		"spiffe://opensoha.local/personal-guest/endpoint/endpoint-1",
	} {
		uri, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		certificate.URIs = []*url.URL{uri}
		if _, err := ParseCertificate(certificate, ScopePersonalGuest); err == nil {
			t.Fatalf("non-guest certificate accepted: %s", raw)
		}
	}
	certificate.URIs = []*url.URL{guestURI, guestURI}
	if _, err := ParseCertificate(certificate, ScopePersonalGuest); err == nil {
		t.Fatal("ambiguous guest identity accepted")
	}
}
