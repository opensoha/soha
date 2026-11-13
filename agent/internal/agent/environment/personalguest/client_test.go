package personalguest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-agent/internal/agent/config"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

type guestTLSFixture struct {
	cfg        config.PersonalGuestConfig
	binding    guest.Binding
	server     *httptest.Server
	mu         sync.Mutex
	registered bool
	sequence   int64
	lostEnroll bool
	lostBeat   bool
	revoked    bool
	redirect   string
	ca         *x509.Certificate
	caKey      ed25519.PrivateKey
}

func newGuestTLSFixture(t *testing.T) *guestTLSFixture {
	t.Helper()
	now := time.Now().UTC().Add(-time.Second)
	f := &guestTLSFixture{binding: guest.Binding{SchemaVersion: guest.BindingVersion, ID: uuid.NewString(), Version: 1,
		AssistantID: uuid.NewString(), OwnerID: uuid.NewString(), Provider: "pve", ComputeRef: uuid.NewString(), ComputeGeneration: 1,
		ProviderIdentityHash: "sha256:" + strings.Repeat("a", 64), DataRef: uuid.NewString(), DataDiskSerial: "testdata01", DataDiskBytes: 8 << 30,
		PolicyRevision: "sha256:" + strings.Repeat("b", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "dedicated guest test CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, public, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	f.ca, f.caKey = ca, key
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	issue := func(leaf *x509.Certificate) ([]byte, []byte) {
		t.Helper()
		p, k, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, p, key)
		if err != nil {
			t.Fatal(err)
		}
		pkcs, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs})
	}
	uri, _ := url.Parse("spiffe://opensoha.local/personal-guest/guest/" + f.binding.ID)
	certPEM, keyPEM := issue(&x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: now, NotAfter: now.Add(time.Hour), URIs: []*url.URL{uri}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	serverPEM, serverKey := issue(&x509.Certificate{SerialNumber: big.NewInt(3), NotBefore: now, NotAfter: now.Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	pair, err := tls.X509KeyPair(serverPEM, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(f.handle))
	f.server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	f.server.StartTLS()
	t.Cleanup(f.server.Close)
	dir := filepath.Join(t.TempDir(), "guest-private")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f.cfg = config.PersonalGuestConfig{Endpoint: f.server.URL, BindingID: f.binding.ID, EnrollmentID: uuid.NewString(), Challenge: strings.Repeat("A", 43), CertificateFile: filepath.Join(dir, "guest.pem"), KeyFile: filepath.Join(dir, "guest.key"), CAFile: filepath.Join(dir, "ca.pem"), StateFile: filepath.Join(dir, "state.json")}
	for name, raw := range map[string][]byte{f.cfg.CertificateFile: certPEM, f.cfg.KeyFile: keyPEM, f.cfg.CAFile: caPEM} {
		if err := os.WriteFile(name, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *guestTLSFixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0][0].URIs) != 1 || r.TLS.VerifiedChains[0][0].URIs[0].String() != "spiffe://opensoha.local/personal-guest/guest/"+f.binding.ID {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.revoked {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if f.redirect != "" {
		w.Header().Set("Location", f.redirect)
		w.WriteHeader(http.StatusTemporaryRedirect)
		return
	}

	if strings.Contains(r.URL.Path, "/data-operations/") {
		_ = json.NewEncoder(w).Encode(guest.DataPlan{SchemaVersion: guest.DataPlanVersion, OperationID: "11111111-1111-4111-8111-111111111111", Binding: f.binding, DiskQuotaBytes: 6 << 30})
		return
	}
	if r.Method == http.MethodGet {
		if !f.registered {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(f.binding)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/enroll") {
		var input guest.Enrollment
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.EnrollmentID != f.cfg.EnrollmentID || r.Header.Get("Authorization") != "Bearer "+f.cfg.Challenge || f.registered {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.registered = true
		if f.lostEnroll {
			dropGuestTestResponse(w)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(f.binding)
		return
	}
	var heartbeat guest.Heartbeat
	if !f.registered || json.NewDecoder(r.Body).Decode(&heartbeat) != nil || heartbeat.ValidateFor(f.binding, time.Now(), f.sequence) != nil || r.Header.Get("Authorization") != "" {
		w.WriteHeader(http.StatusConflict)
		return
	}
	f.sequence = heartbeat.Sequence
	if f.lostBeat {
		dropGuestTestResponse(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func dropGuestTestResponse(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	connection, _, err := hijacker.Hijack()
	if err == nil {
		_ = connection.Close()
	}
}

func TestPersonalGuestMutualTLSRecoveryAndDurableSequence(t *testing.T) {
	f := newGuestTLSFixture(t)
	f.lostEnroll = true
	client, err := New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := New(f.cfg); err == nil {
		_ = other.Close()
		t.Fatal("second process acquired guest state")
	}
	ctx := context.Background()
	if err := client.enroll(ctx); err != nil {
		t.Fatalf("committed enrollment response could not recover: %v", err)
	}
	f.mu.Lock()
	f.lostBeat = true
	f.mu.Unlock()
	if err := client.heartbeat(ctx); !errors.Is(err, errTransient) {
		t.Fatal("lost heartbeat was reported as confirmed")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.lostBeat = false
	f.mu.Unlock()
	client, err = New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.enroll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.heartbeat(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.sequence != 2 {
		t.Error("restart reset heartbeat sequence")
	}
	f.revoked = true
	f.mu.Unlock()
	if err := client.heartbeat(ctx); err == nil || errors.Is(err, errTransient) {
		t.Fatal("revocation did not fail closed")
	}
}

func TestPersonalGuestRejectsLostStateAndRedirect(t *testing.T) {
	f := newGuestTLSFixture(t)
	f.registered = true
	client, err := New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.enroll(context.Background()); err == nil {
		t.Fatal("lost sequence state was recreated for a registered guest")
	}
	_ = client.Close()
	client, err = New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.enroll(context.Background()); err == nil {
		t.Fatal("restarting after lost-state denial restored a registered identity")
	}
	_ = client.Close()
	f.registered = false
	f.redirect = "https://untrusted.example/enrollment"
	client, err = New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.enroll(context.Background()); err == nil || errors.Is(err, errTransient) {
		t.Fatal("redirect followed or treated as recoverable transport")
	}
}

func TestPersonalGuestChecksServerNameAndCertificateScope(t *testing.T) {
	f := newGuestTLSFixture(t)
	client, err := New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok {
		t.Fatal("personal guest TLS transport is missing")
	}
	transport.TLSClientConfig.ServerName = "wrong.example"
	if err := client.enroll(context.Background()); !errors.Is(err, errTransient) {
		t.Fatal("server name mismatch did not reject TLS")
	}
	_ = client.Close()
	f.cfg.BindingID = uuid.NewString()
	if other, err := New(f.cfg); err == nil {
		_ = other.Close()
		t.Fatal("certificate from a different guest accepted")
	}
}
