package runtime

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestSecretCertificateMetadata(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "demo.test"}, NotBefore: start, NotAfter: start.Add(30 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	crt := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	for _, tc := range []struct {
		name, kind string
		data       []byte
		status     string
	}{
		{"valid", "kubernetes.io/tls", crt, "available"},
		{"missing", "kubernetes.io/tls", nil, "missing"},
		{"invalid", "kubernetes.io/tls", []byte("not a certificate"), "invalid"},
		{"oversized", "kubernetes.io/tls", make([]byte, (1<<20)+1), "invalid"},
		{"private key block", "kubernetes.io/tls", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("secret-sentinel")}), "invalid"},
		{"wrong type", "Opaque", crt, "unsupported_type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := SecretCertificateMetadata("demo-tls", "demo", tc.kind, tc.data)
			if v.Status != tc.status || v.Name != "demo-tls" || v.Namespace != "demo" {
				t.Fatalf("metadata = %+v", v)
			}
			if tc.status == "available" {
				if v.NotBefore != start.Format(time.RFC3339) || v.NotAfter != template.NotAfter.Format(time.RFC3339) || v.SelfSigned == nil || !*v.SelfSigned {
					t.Fatalf("certificate dates = %+v", v)
				}
			} else if v.NotBefore != "" || v.NotAfter != "" {
				t.Fatal("unreadable certificate has invented dates")
			}
			encoded, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"tls.key", "tls.crt", "BEGIN", "secret-sentinel", "\"data\""} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("response contains %q", forbidden)
				}
			}
		})
	}
}

func TestSecretCertificateMetadataExplicitlyReportsNotSelfSigned(t *testing.T) {
	issuerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	issuer := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Demo CA"}, NotBefore: start, NotAfter: start.Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "demo.test"}, NotBefore: start, NotAfter: start.Add(90 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, leaf, issuer, &leafKey.PublicKey, issuerKey)
	if err != nil {
		t.Fatal(err)
	}
	v := SecretCertificateMetadata("tls", "demo", "kubernetes.io/tls", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	encoded, err := json.Marshal(v)
	if err != nil || v.Status != "available" || v.SelfSigned == nil || *v.SelfSigned || !strings.Contains(string(encoded), `"selfSigned":false`) {
		t.Fatalf("non-self-signed projection: %s %v", encoded, err)
	}
}
