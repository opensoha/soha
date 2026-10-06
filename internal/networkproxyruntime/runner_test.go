package networkproxyruntime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/opensoha/soha/internal/platform/securefile"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestFailedConfigurationReasonSurvivesObservation(t *testing.T) {
	runner := &Runner{
		engine:      &engine{},
		lastAttempt: &domain.Applied{Status: "rejected", ReasonCode: "validation_failed"},
	}
	observation, _ := runner.observe(context.Background())
	if observation.ReasonCode != "validation_failed" {
		t.Fatalf("reason code = %q", observation.ReasonCode)
	}
}

func TestEnrollmentMarkerTracksTLSIdentity(t *testing.T) {
	for _, scenario := range []string{"restart", "renewed-certificate", "new-challenge", "new-runtime", "legacy", "legacy-renewed-certificate", "legacy-unavailable", "failed-enrollment"} {
		t.Run(scenario, func(t *testing.T) {
			_, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			var enrollments atomic.Int32
			var unavailable atomic.Bool
			var activeFingerprint atomic.Pointer[string]
			activeFingerprint.Store(new(string))
			var consumed sync.Map
			server := httptest.NewUnstartedServer(enrollmentTestHandler(t, &enrollments, &unavailable, &activeFingerprint, &consumed))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert}
			server.StartTLS()
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			schemas, err := networkprotocol.CompileSchemas()
			if err != nil {
				t.Fatal(err)
			}
			originalCertificate := enrollmentTestCertificate(t, key, "proxy-test", 1)
			newClient := func(id string, serial int64) *Client {
				certificate := originalCertificate
				if serial != 1 || id != "proxy-test" {
					certificate = enrollmentTestCertificate(t, key, id, serial)
				}
				client := NewClient(server.URL, id, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}})
				client.SetSchemas(schemas)
				return client
			}
			stateDir := t.TempDir()
			tokenFile := filepath.Join(stateDir, "token")
			token := strings.Repeat("test-token", 4)
			if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
				t.Fatal(err)
			}
			runner := &Runner{config: Config{StateDir: stateDir, RuntimeID: "proxy-test", EnrollmentID: uuid.NewString(), ChallengeID: uuid.NewString(), EnrollmentTokenFile: tokenFile}, client: newClient("proxy-test", 1)}
			if err := runner.enroll(context.Background()); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(stateDir, "enrolled")
			before, err := securefile.Read(marker, 4096, true)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(before, []byte(token)) {
				t.Fatal("enrollment marker contains the token")
			}
			if strings.HasPrefix(scenario, "legacy") {
				before = []byte("proxy-test")
				if err := os.WriteFile(marker, before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			wantEnrollments := int32(2)
			switch scenario {
			case "restart", "legacy", "legacy-unavailable":
				runner.client = newClient("proxy-test", 1)
				wantEnrollments = 1
				if err := os.Remove(tokenFile); err != nil {
					t.Fatal(err)
				}
			case "renewed-certificate", "legacy-renewed-certificate":
				// Reuse the public key: the certificate fingerprint must still change.
				runner.client = newClient("proxy-test", 2)
				runner.config.ChallengeID = uuid.NewString()
			case "new-challenge", "failed-enrollment":
				runner.config.ChallengeID = uuid.NewString()
			case "new-runtime":
				runner.config.RuntimeID = "proxy-new"
				runner.config.ChallengeID = uuid.NewString()
				runner.client = newClient("proxy-new", 2)
			}
			wantError := scenario == "legacy-unavailable" || scenario == "failed-enrollment"
			unavailable.Store(wantError)
			err = runner.enroll(context.Background())
			if (err != nil) != wantError || enrollments.Load() != wantEnrollments {
				t.Fatalf("enroll error=%v, requests=%d, want error=%v requests=%d", err, enrollments.Load(), wantError, wantEnrollments)
			}
			assertEnrollmentMarker(t, marker, before, wantError, enrollmentState{
				RuntimeID: runner.config.RuntimeID, ControlURL: server.URL, CertificateFingerprint: *activeFingerprint.Load(),
				EnrollmentID: runner.config.EnrollmentID, ChallengeID: runner.config.ChallengeID,
			})
		})
	}
}

func enrollmentTestHandler(t *testing.T, enrollments *atomic.Int32, unavailable *atomic.Bool, activeFingerprint *atomic.Pointer[string], consumed *sync.Map) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		identity, err := networkidentity.ParseCertificate(request.TLS.PeerCertificates[0], networkidentity.ScopeNetworkControl)
		if err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.HasSuffix(request.URL.Path, "/configuration") {
			if unavailable.Load() {
				writer.WriteHeader(http.StatusServiceUnavailable)
			} else if identity.CertificateFingerprint != *activeFingerprint.Load() {
				writer.WriteHeader(http.StatusUnauthorized)
			} else {
				writer.WriteHeader(http.StatusNoContent)
			}
			return
		}
		enrollments.Add(1)
		if unavailable.Load() {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var incoming networkprotocol.RuntimeMessage
		if err := json.NewDecoder(request.Body).Decode(&incoming); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		payload, err := networkprotocol.DecodePayload[networkprotocol.EnrollmentRequest](incoming.Payload)
		if err != nil || networkidentity.MatchPublicKey(identity.Certificate, payload.DevicePublicKey) != nil || incoming.RuntimeID != identity.ID {
			t.Error("enrollment does not match the active TLS identity")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, duplicate := consumed.LoadOrStore(payload.ChallengeID, true); duplicate {
			writer.WriteHeader(http.StatusConflict)
			return
		}
		activeFingerprint.Store(&identity.CertificateFingerprint)
		result, err := json.Marshal(networkprotocol.EnrollmentResult{Accepted: true, ReasonCode: "enrolled"})
		if err != nil {
			t.Error(err)
			return
		}
		writer.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(writer).Encode(networkprotocol.RuntimeMessage{
			SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: uuid.NewString(),
			MessageType: networkprotocol.MessageEnrollmentResult, ProducerID: "network-control",
			RuntimeID: identity.ID, RuntimeKind: "proxy", OccurredAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(time.Minute), Payload: result,
		}); err != nil {
			t.Error(err)
		}
	}
}

func assertEnrollmentMarker(t *testing.T, marker string, before []byte, wantError bool, expected enrollmentState) {
	t.Helper()
	after, err := securefile.Read(marker, 4096, true)
	if err != nil {
		t.Fatal(err)
	}
	if wantError {
		if !bytes.Equal(before, after) {
			t.Fatal("failed enrollment changed the marker")
		}
		return
	}
	var state enrollmentState
	if err := json.Unmarshal(after, &state); err != nil {
		t.Fatal(err)
	}
	if state != expected {
		t.Fatalf("marker does not match the registered identity: %+v", state)
	}
}

func enrollmentTestCertificate(t *testing.T, key ed25519.PrivateKey, id string, serial int64) tls.Certificate {
	t.Helper()
	identity, err := url.Parse("spiffe://opensoha.local/network-control/proxy/" + id)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), URIs: []*url.URL{identity}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestFailedConfigurationDegradesHealthyOldEngine(t *testing.T) {
	controller := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/version" {
			_, _ = fmt.Fprint(writer, `{"version":"test"}`)
			return
		}
		_, _ = fmt.Fprint(writer, `{"uploadTotal":1,"downloadTotal":2,"connections":[]}`)
	}))
	defer controller.Close()
	runner := &Runner{
		config: Config{Engine: domain.EngineMihomo},
		engine: &engine{controller: newClashController(controller.URL, "test-secret"),
			process: &exec.Cmd{}, startedAt: time.Now().Add(-time.Second)},
		enabled:     true,
		lastAttempt: &domain.Applied{Status: "rejected", ReasonCode: "validation_failed"},
	}
	observation, _ := runner.observe(context.Background())
	if observation.Health != "degraded" || observation.ReasonCode != "validation_failed" || observation.DownloadTotal != 2 {
		t.Fatalf("observation = %+v", observation)
	}
}
