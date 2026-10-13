package personalguest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const testBindingID = "00000000-0000-4000-8000-000000000049"

type guestStub struct{ calls atomic.Int32 }

func (s *guestStub) Enroll(_ context.Context, _ networkidentity.Identity, authorization string, _ guest.Enrollment) (guest.Binding, error) {
	s.calls.Add(1)
	if _, err := guest.EnrollmentTokenHash(authorization); err != nil {
		return guest.Binding{}, apperrors.ErrUnauthorized
	}
	return guest.Binding{}, nil
}
func (s *guestStub) Binding(context.Context, networkidentity.Identity) (guest.Binding, error) {
	s.calls.Add(1)
	return guest.Binding{}, nil
}
func (s *guestStub) Heartbeat(context.Context, networkidentity.Identity, guest.Heartbeat) error {
	s.calls.Add(1)
	return nil
}

func TestPersonalGuestRouterRejectsUnverifiedIdentityHeadersAndWrongScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &guestStub{}
	router, err := NewRouter(service)
	if err != nil {
		t.Fatal(err)
	}
	for _, verified := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodGet, guest.BindingsPath+"/"+testBindingID, nil)
		request.Header.Set("X-Client-Cert", "pretend-certificate")
		request.Header.Set("X-Guest-ID", testBindingID)
		if verified {
			uri, err := url.Parse("spiffe://opensoha.local/network-control/endpoint/" + testBindingID)
			if err != nil {
				t.Fatal(err)
			}
			request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)}}}}
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || service.calls.Load() != 0 {
			t.Fatalf("unverified or network identity reached service: %d", response.Code)
		}
	}
}

func TestPersonalGuestRouterRejectsCertificateExpiredAfterTLSHandshake(t *testing.T) {
	service := &guestStub{}
	router, err := NewRouter(service)
	if err != nil {
		t.Fatal(err)
	}
	uri, _ := url.Parse("spiffe://opensoha.local/personal-guest/guest/" + testBindingID)
	request := httptest.NewRequest(http.MethodGet, guest.BindingsPath+"/"+testBindingID, nil)
	request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(-time.Second)}}}}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || service.calls.Load() != 0 {
		t.Fatal("expired keepalive identity reached service")
	}
}

func TestPersonalGuestRouterBoundsAndStrictlyDecodesEnrollment(t *testing.T) {
	uri, err := url.Parse("spiffe://opensoha.local/personal-guest/guest/" + testBindingID)
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"schemaVersion":"opensoha.dev/personal-guest-enrollment/v1","enrollmentId":"00000000-0000-4000-8000-000000000050"}`
	for _, test := range []struct {
		name, body, contentType, encoding string
		authorizations                    int
		status                            int
	}{
		{"valid", valid, "application/json", "", 1, 201},
		{"unknown owner", strings.TrimSuffix(valid, "}") + `,"ownerId":"other"}`, "application/json", "", 1, 400},
		{"duplicate", strings.TrimSuffix(valid, "}") + `,"enrollmentId":"00000000-0000-4000-8000-000000000050"}`, "application/json", "", 1, 400},
		{"case mismatch", strings.Replace(valid, "enrollmentId", "EnrollmentID", 1), "application/json", "", 1, 400},
		{"oversized", valid + strings.Repeat(" ", 4096), "application/json", "", 1, 413},
		{"compressed", valid, "application/json", "gzip", 1, 415},
		{"content type", valid, "text/plain", "", 1, 415},
		{"missing authorization", valid, "application/json", "", 0, 401},
		{"ambiguous authorization", valid, "application/json", "", 2, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &guestStub{}
			router, err := NewRouter(service)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, guest.BindingsPath+"/"+testBindingID+"/enroll", strings.NewReader(test.body))
			request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)}}}}
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set("Content-Encoding", test.encoding)
			for range test.authorizations {
				request.Header.Add("Authorization", "Bearer "+strings.Repeat("A", 43))
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || (test.status != 201 && service.calls.Load() != 0) {
				t.Fatalf("request boundary: %d, calls %d", response.Code, service.calls.Load())
			}
		})
	}
}

func TestPersonalGuestActualTLSRejectsOtherCertificateNamespacesAndPath(t *testing.T) {
	service := &guestStub{}
	router, err := NewRouter(service)
	if err != nil {
		t.Fatal(err)
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-only"}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	server := httptest.NewUnstartedServer(router)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pool}
	server.StartTLS()
	defer server.Close()
	for index, test := range []struct {
		uri, path string
		status    int
	}{
		{"personal-guest/guest/" + testBindingID, testBindingID, 200},
		{"network-control/endpoint/" + testBindingID, testBindingID, 401},
		{"network-ingest/core/" + testBindingID, testBindingID, 401},
		{"personal-guest/guest/" + testBindingID, "other", 401},
	} {
		certificate := guestTestCertificate(t, root, caKey, test.uri, int64(index+2))
		baseTransport, ok := server.Client().Transport.(*http.Transport)
		if !ok {
			t.Fatal("TLS test transport is unavailable")
		}
		transport := baseTransport.Clone()
		transport.TLSClientConfig.Certificates = []tls.Certificate{certificate}
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		response, err := client.Get(server.URL + guest.BindingsPath + "/" + test.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		transport.CloseIdleConnections()
		if response.StatusCode != test.status || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("TLS namespace/path isolation: %d", response.StatusCode)
		}
	}
	if service.calls.Load() != 1 {
		t.Fatalf("other namespace reached guest service: %d", service.calls.Load())
	}
}

func guestTestCertificate(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, path string, serial int64) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse("spiffe://opensoha.local/" + path)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(serial), URIs: []*url.URL{uri}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage: x509.KeyUsageDigitalSignature, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter}
	der, err := x509.CreateCertificate(rand.Reader, certificate, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key}
}

func TestPersonalGuestHeartbeatRejectsClientReadinessClaim(t *testing.T) {
	uri, err := url.Parse("spiffe://opensoha.local/personal-guest/guest/" + testBindingID)
	if err != nil {
		t.Fatal(err)
	}
	service := &guestStub{}
	router, err := NewRouter(service)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"schemaVersion": guest.HeartbeatVersion, "bindingId": testBindingID, "bindingVersion": 1,
		"computeGeneration": 1, "sequence": 1, "observedAt": time.Now().UTC(), "ready": true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, guest.BindingsPath+"/"+testBindingID+"/heartbeats", strings.NewReader(string(data)))
	request.Header.Set("Content-Type", "application/json")
	request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)}}}}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || service.calls.Load() != 0 {
		t.Fatal("client readiness claim reached guest service")
	}
}
