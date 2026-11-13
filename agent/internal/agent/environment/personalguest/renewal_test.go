package personalguest

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func TestPersonalGuestRenewalLostRepliesAndRestartPreserveSequence(t *testing.T) {
	f := newGuestTLSFixture(t)
	var mu sync.Mutex
	var pending *guest.RenewalResult
	var activeDER, nextDER []byte
	issued, committed := 0, false
	lostPrepare, lostCommit := true, true
	certPEM, err := os.ReadFile(f.cfg.CertificateFile)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	activeDER = block.Bytes
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		der := r.TLS.VerifiedChains[0][0].Raw
		if strings.HasSuffix(r.URL.Path, "/renewals") {
			if !bytes.Equal(der, activeDER) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var input guest.Renewal
			if json.NewDecoder(r.Body).Decode(&input) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if pending == nil {
				csrBlock, _ := pem.Decode([]byte(input.CSRPEM))
				csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
				if err != nil || csr.CheckSignature() != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				binding := f.binding
				binding.Version++
				binding.IssuedAt, binding.ExpiresAt = time.Now().UTC(), time.Now().UTC().Add(time.Hour)
				uri, _ := url.Parse("spiffe://opensoha.local/personal-guest/guest/" + binding.ID)
				nextDER, err = x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(9), NotBefore: binding.IssuedAt, NotAfter: binding.ExpiresAt, URIs: []*url.URL{uri}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}, f.ca, csr.PublicKey, f.caKey)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				ca, err := os.ReadFile(f.cfg.CAFile)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				pending = &guest.RenewalResult{RequestID: input.RequestID, Binding: binding, CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: nextDER})), ClientCAPEM: string(ca), CommitBefore: binding.IssuedAt.Add(5 * time.Minute)}
				issued++
			}
			if pending.RequestID != input.RequestID {
				w.WriteHeader(http.StatusConflict)
				return
			}
			if lostPrepare {
				lostPrepare = false
				dropGuestTestResponse(w)
				return
			}
			_ = json.NewEncoder(w).Encode(pending)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/commit") {
			if pending == nil || !bytes.Equal(der, nextDER) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			activeDER, committed = nextDER, true
			f.mu.Lock()
			f.binding = pending.Binding
			f.mu.Unlock()
			if lostCommit {
				lostCommit = false
				dropGuestTestResponse(w)
				return
			}
			_ = json.NewEncoder(w).Encode(pending.Binding)
			return
		}
		if !bytes.Equal(der, activeDER) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.handle(w, r)
	})
	c, err := New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.enroll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.heartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
	sequence := c.state.Sequence
	if err := c.renew(context.Background()); err == nil || c.state.Renewal == nil {
		t.Fatal("lost issuance did not retain request")
	}
	if err := c.renew(context.Background()); err == nil || c.state.RenewalResult == nil || !committed {
		t.Fatal("lost commit did not retain new certificate")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.enroll(context.Background()); err != nil {
		t.Fatal("pending commit failed across restart", err)
	}
	if c.state.Sequence != sequence || c.state.Binding.Version != 2 || issued != 1 {
		t.Fatal("renewal reset sequence or issued another identity")
	}
	if err := c.heartbeat(context.Background()); err != nil || c.state.Sequence <= sequence {
		t.Fatal("renewed heartbeat failed", err)
	}
}

func TestExpiredPendingRenewalRequiresCurrentAuthority(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(fmt.Sprint("revoked=", revoked), func(t *testing.T) {
			f := newGuestTLSFixture(t)
			c, err := New(f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.enroll(t.Context()); err != nil {
				t.Fatal(err)
			}
			c.state.Sequence = 154
			if err := c.prepareRenewal(); err != nil {
				t.Fatal(err)
			}
			c.state.RenewalResult = &guest.RenewalResult{RequestID: c.state.Renewal.RequestID, Binding: *c.state.Binding, CommitBefore: time.Now().Add(-time.Second)}
			if err := c.store.save(c.state); err != nil {
				t.Fatal(err)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/renewals") {
					w.WriteHeader(http.StatusGone)
					return
				}
				if revoked {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				f.handle(w, r)
			})
			c, err = New(f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			err = c.enroll(t.Context())
			if revoked {
				if err == nil || c.state.Renewal == nil {
					t.Fatal("revoked authority recovered expired renewal")
				}
			} else if err != nil || c.state.Renewal != nil || c.state.RenewalResult != nil {
				t.Fatal("current authority cannot recover", err)
			}
			if c.state.Sequence != 154 {
				t.Fatal("expiry recovery reset sequence")
			}
		})
	}
}
