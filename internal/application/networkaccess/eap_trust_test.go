package networkaccess

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

func TestEAPTrustRequiresExactServerAndCurrentPublicCA(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}))
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		names       []string
		certificate string
		wantValid   bool
	}{
		{"valid", []string{"radius.example.test"}, ca, true},
		{"wildcard", []string{"*.example.test"}, ca, false},
		{"duplicate", []string{"radius.example.test", "RADIUS.example.test"}, ca, false},
		{"empty DNS label", []string{"radius..example.test"}, ca, false},
		{"private key", []string{"radius.example.test"}, string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private})), false},
		{"extra material", []string{"radius.example.test"}, ca + ca, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			trust := &domain.EAPServerTrust{ServerNames: test.names, CaCertificates: []string{test.certificate}, WifiSecurity: sohaapi.NetworkEAPServerTrustWifiSecurityWpa2Enterprise}
			if err := ValidateEAPServerTrust(trust); (err == nil) != test.wantValid {
				t.Fatalf("valid=%v error=%v", test.wantValid, err)
			}
		})
	}
}
