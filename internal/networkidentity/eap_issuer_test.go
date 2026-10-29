package networkidentity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func eapTestCA(t *testing.T, folder, name string, serial int64, now time.Time) (string, string, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(7 * 24 * time.Hour), SubjectKeyId: []byte("0123456789abcdef" + name)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(folder, name+".crt"), filepath.Join(folder, name+".key")
	for name, raw := range map[string][]byte{certFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})} {
		if err := os.WriteFile(name, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return certFile, keyFile, certificate
}

func TestDedicatedEAPIssuerBindsCSRWithoutControlIdentity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	folder := t.TempDir()
	ca, key, root := eapTestCA(t, folder, "eap", 1, now)
	control, _, _ := eapTestCA(t, folder, "control", 2, now)
	issuer, err := LoadEAPIssuer(ca, key, control)
	if err != nil {
		t.Fatal(err)
	}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := url.Parse("spiffe://opensoha.local/network-control/endpoint/forged")
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "forged"}, URIs: []*url.URL{forged}}, private)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))
	certPEM, _, err := issuer.Issue(context.Background(), "device-1", "subject-1", encoded, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(certPEM))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "subject-1" || len(cert.URIs) != 1 || cert.URIs[0].String() != "spiffe://opensoha.local/network-eap/endpoint/device-1" || !cert.NotAfter.Equal(now.Add(time.Hour)) || len(cert.DNSNames) != 0 {
		t.Fatalf("untrusted CSR identity or expiry escaped binding: %+v", cert)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{ScopeNetworkControl, ScopeIngest} {
		if _, err := ParseCertificate(cert, scope); err == nil {
			t.Fatalf("EAP certificate entered %s", scope)
		}
	}
	if _, err := LoadEAPIssuer(ca, key, ca); err == nil {
		t.Fatal("shared EAP/control CA accepted")
	}
	csr[len(csr)-1] ^= 1
	broken := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))
	if _, _, err := issuer.Issue(context.Background(), "device-1", "subject-1", broken, now, now.Add(time.Hour)); err == nil {
		t.Fatal("invalid CSR signature accepted")
	}
	if _, _, err := issuer.Issue(context.Background(), "device-1", "subject-1", encoded, now.Add(8*24*time.Hour), now.Add(9*24*time.Hour)); err == nil {
		t.Fatal("expired issuer accepted")
	}
}
