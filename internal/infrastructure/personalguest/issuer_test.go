package personalguest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func issuerFixture(t *testing.T) (*Issuer, string, string, guest.Binding) {
	t.Helper()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "dedicated-guest-ca"}, IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, public, key)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	caPath, keyPath := filepath.Join(directory, "ca.pem"), filepath.Join(directory, "key.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	issuer, err := LoadIssuer(caPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	binding := guest.Binding{SchemaVersion: guest.BindingVersion, ID: uuid.NewString(), Version: 1, AssistantID: uuid.NewString(), OwnerID: uuid.NewString(),
		Provider: "pve", ComputeRef: uuid.NewString(), ComputeGeneration: 1, ProviderIdentityHash: "sha256:" + strings.Repeat("a", 64),
		DataRef: uuid.NewString(), DataDiskSerial: "soha49data01", DataDiskBytes: 8 << 30, PolicyRevision: "sha256:" + strings.Repeat("b", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	return issuer, caPath, keyPath, binding
}

func TestGuestIssuerPinsCoreIdentityAndOnlyPublicCSRKey(t *testing.T) {
	issuer, _, _, binding := issuerFixture(t)
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	foreign := &url.URL{Scheme: "spiffe", Host: "opensoha.local", Path: "/network-control/core/foreign"}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "foreign"}, DNSNames: []string{"foreign.example"}, URIs: []*url.URL{foreign}}, key)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))
	certificate, ca, fingerprint, err := issuer.Issue(t.Context(), binding, encoded)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(certificate))
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := networkidentity.ParseCertificate(leaf, networkidentity.ScopePersonalGuest)
	leafKey, ok := leaf.PublicKey.(ed25519.PublicKey)
	if !ok || err != nil || identity.ID != binding.ID || identity.CertificateFingerprint != fingerprint || fmt.Sprintf("sha256:%x", sha256.Sum256(block.Bytes)) != fingerprint || !leafKey.Equal(public) || leaf.IsCA || len(leaf.DNSNames) != 0 || len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatal("untrusted CSR identity or privilege entered the guest certificate")
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(ca))
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatal(err)
	}
	csr[len(csr)-1] ^= 1
	if _, _, _, err := issuer.Issue(t.Context(), binding, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("invalid CSR proof accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, err := issuer.Issue(ctx, binding, encoded); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled certificate issuance continued")
	}
}

func TestGuestIssuerRejectsExposedOrLinkedPrivateKey(t *testing.T) {
	_, ca, key, _ := issuerFixture(t)
	if err := os.Chmod(key, 0644); err != nil { //nolint:gosec // Deliberately exposed test key must be rejected.
		t.Fatal(err)
	}
	if _, err := LoadIssuer(ca, key); err == nil {
		t.Fatal("public-readable signing key accepted")
	}
	if err := os.Chmod(key, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(key), "linked-key.pem")
	if err := os.Symlink(key, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIssuer(ca, link); err == nil {
		t.Fatal("symlink signing key accepted")
	}
}
