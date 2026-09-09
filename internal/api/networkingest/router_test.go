package networkingest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/networkidentity"
)

type ingestServiceStub struct {
	identity    networkidentity.Identity
	raw         []byte
	radiusCalls int
}

func (service *ingestServiceStub) Summary(_ context.Context, _ networkidentity.Identity, filter domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error) {
	return domainnetworkingest.Summary{From: filter.From, To: filter.To, Producers: []domainnetworkingest.ProducerSummary{}, ProxyFlows: []domainnetworkingest.ProxyFlowSummary{}}, nil
}

func (service *ingestServiceStub) IngestRADIUS(_ context.Context, identity networkidentity.Identity, raw []byte) (domainnetworkingest.Acknowledgement, error) {
	service.identity, service.raw = identity, append([]byte(nil), raw...)
	service.radiusCalls++
	return domainnetworkingest.Acknowledgement{BatchID: "radius-event-1", Accepted: 1}, nil
}

func (service *ingestServiceStub) Ingest(_ context.Context, identity networkidentity.Identity, raw []byte) (domainnetworkingest.Acknowledgement, error) {
	service.identity, service.raw = identity, append([]byte(nil), raw...)
	return domainnetworkingest.Acknowledgement{BatchID: "batch-1", Accepted: 1}, nil
}

type readyStoreStub struct{ err error }

func (store readyStoreStub) Ping(context.Context) error { return store.err }

func TestRouterRequiresMTLSAndBoundsIngest(t *testing.T) {
	service := &ingestServiceStub{}
	router, err := NewRouter(service, readyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 1})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	body := `{"schemaVersion":"network-ingest/v1alpha1"}`

	request := httptest.NewRequest(http.MethodPost, "/api/ingest/v1/events:batch", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("without certificate status = %d", response.Code)
	}

	request = authenticatedRequest(t, body)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("authenticated status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.identity.ID != "gateway-1" || string(service.raw) != body {
		t.Fatalf("service identity/raw = %#v / %q", service.identity, service.raw)
	}

	request = authenticatedRequest(t, body)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limited response = %d, headers = %#v", response.Code, response.Header())
	}
}

func TestRouterHealthDoesNotRequireClientCertificate(t *testing.T) {
	router, err := NewRouter(&ingestServiceStub{}, readyStoreStub{}, Options{MaxBodyBytes: 32, RequestsPerMinute: 1})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, response.Code)
		}
	}
}

func TestRouterRoutesFreeRADIUSAccounting(t *testing.T) {
	service := &ingestServiceStub{}
	router, err := NewRouter(service, readyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 10})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	body := `{"schemaVersion":"network-radius-accounting/v1alpha1","eventTimestamp":1788336000,"statusType":"start","accountingSessionId":"session-1","nasId":"nas-hq","sessionTimeSeconds":0}`
	request := authenticatedIngestRequest(t, "/api/ingest/v1/radius/accounting", body, "freeradius", "freeradius-hq")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || service.radiusCalls != 1 {
		t.Fatalf("RADIUS accounting status/calls = %d/%d, body = %s", response.Code, service.radiusCalls, response.Body.String())
	}
	if service.identity.Kind != "freeradius" || service.identity.ID != "freeradius-hq" || string(service.raw) != body {
		t.Fatalf("RADIUS accounting identity/raw = %#v / %q", service.identity, service.raw)
	}
}

func TestRouterSummaryRequiresDedicatedCoreCertificate(t *testing.T) {
	router, err := NewRouter(&ingestServiceStub{}, readyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		kind string
		want int
	}{
		{want: http.StatusUnauthorized},
		{kind: "endpoint", want: http.StatusForbidden},
		{kind: "core", want: http.StatusOK},
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/ingest/v1/query/summary?from=2026-09-03T00%3A00%3A00Z&to=2026-09-03T01%3A00%3A00Z&limit=20", nil)
		if item.kind != "" {
			identityURI, _ := url.Parse("spiffe://opensoha.local/network-ingest/" + item.kind + "/reader-1")
			certificate := &x509.Certificate{Raw: []byte("cert"), RawSubjectPublicKeyInfo: []byte("key"), URIs: []*url.URL{identityURI}}
			request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{certificate}}}
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != item.want {
			t.Fatalf("kind %q status = %d, want %d: %s", item.kind, response.Code, item.want, response.Body.String())
		}
	}
}

func authenticatedRequest(t *testing.T, body string) *http.Request {
	return authenticatedIngestRequest(t, "/api/ingest/v1/events:batch", body, "gateway", "gateway-1")
}

func authenticatedIngestRequest(t *testing.T, path, body, kind, id string) *http.Request {
	t.Helper()
	identityURI, err := url.Parse("spiffe://opensoha.local/network-ingest/" + kind + "/" + id)
	if err != nil {
		t.Fatalf("parse identity URI: %v", err)
	}
	certificate := &x509.Certificate{Raw: []byte("cert"), RawSubjectPublicKeyInfo: []byte("key"), URIs: []*url.URL{identityURI}}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{certificate}}}
	return request
}
