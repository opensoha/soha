package networkcontrol

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
)

type controlServiceStub struct {
	ready          bool
	identity       networkidentity.Identity
	token          string
	raw            []byte
	enrollCalls    int
	authorizeCalls int
	vpnCalls       int
	radiusResult   networkprotocol.NASAuthorizationResult
	radiusCalls    int
	nextCommand    networkprotocol.RuntimeMessage
	nextCalls      int
	resultCalls    int
	subscription   networkprotocol.MihomoSubscription
	subscriptionID string
	source         networkprotocol.MihomoSource
	sourceID       string
}

func (service *controlServiceStub) Ready() bool { return service.ready }

func (service *controlServiceStub) Enroll(_ context.Context, identity networkidentity.Identity, token string, raw []byte) (networkprotocol.RuntimeMessage, error) {
	service.identity, service.token, service.raw = identity, token, append([]byte(nil), raw...)
	service.enrollCalls++
	return networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageType: networkprotocol.MessageEnrollmentResult}, nil
}

func (service *controlServiceStub) Configuration(context.Context, networkidentity.Identity) (networkprotocol.RuntimeMessage, error) {
	return networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageType: networkprotocol.MessageConfiguration}, nil
}

func (service *controlServiceStub) MihomoSubscription(_ context.Context, identity networkidentity.Identity, profileID string) (networkprotocol.MihomoSubscription, error) {
	service.identity, service.subscriptionID = identity, profileID
	return service.subscription, nil
}

func (service *controlServiceStub) MihomoSource(_ context.Context, identity networkidentity.Identity, profileID string) (networkprotocol.MihomoSource, error) {
	service.identity, service.sourceID = identity, profileID
	return service.source, nil
}

func (service *controlServiceStub) Apply(context.Context, networkidentity.Identity, []byte) error {
	return nil
}

func (service *controlServiceStub) Renew(context.Context, networkidentity.Identity, []byte) (networkprotocol.RuntimeMessage, error) {
	return networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageType: networkprotocol.MessageLeaseRenewResult}, nil
}

func (service *controlServiceStub) Revoke(context.Context, networkidentity.Identity, []byte) (int64, error) {
	return 1, nil
}

func (service *controlServiceStub) ConnectVPN(_ context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	service.identity, service.raw = identity, append([]byte(nil), raw...)
	service.vpnCalls++
	return networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageType: networkprotocol.MessageVPNConnectResult}, nil
}

func (service *controlServiceStub) AuthorizeNAS(_ context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	service.identity, service.raw = identity, append([]byte(nil), raw...)
	service.authorizeCalls++
	return networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageType: networkprotocol.MessageNASAuthorizationResult}, nil
}

func (service *controlServiceStub) AuthorizeRADIUS(_ context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.NASAuthorizationResult, error) {
	service.identity, service.raw = identity, append([]byte(nil), raw...)
	service.radiusCalls++
	return service.radiusResult, nil
}

func (service *controlServiceStub) Snapshot(context.Context, networkidentity.Identity) (domainnetworkruntime.PolicySnapshot, error) {
	return domainnetworkruntime.PolicySnapshot{PolicyVersion: 7}, nil
}

func (service *controlServiceStub) NextNASSessionCommand(_ context.Context, identity networkidentity.Identity) (networkprotocol.RuntimeMessage, error) {
	service.identity = identity
	service.nextCalls++
	return service.nextCommand, nil
}

func (service *controlServiceStub) CompleteNASSessionCommand(_ context.Context, identity networkidentity.Identity, raw []byte) error {
	service.identity, service.raw = identity, append([]byte(nil), raw...)
	service.resultCalls++
	return nil
}

type controlReadyStoreStub struct{ err error }

func (store controlReadyStoreStub) Ping(context.Context) error { return store.err }

func TestRouterMihomoSubscriptionRequiresExactMTLSIdentityAndDisablesCaching(t *testing.T) {
	service := &controlServiceStub{ready: true, subscription: networkprotocol.MihomoSubscription{
		ProfileID: "mihomo-1", ProfileRevision: 2, SubscriptionURL: "https://subscriptions.example.test/endpoint-1",
	}, source: networkprotocol.MihomoSource{ProfileID: "mihomo-1", ProfileRevision: 2, SourceType: "manual_node", ManualNode: &networkprotocol.MihomoManualNode{Protocol: "http", Server: "proxy.example.test", Port: 8080}}}
	router, err := NewRouter(service, controlReadyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 100})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/network-control/v1/runtimes/endpoint-1/mihomo-profiles/mihomo-1/subscription", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthenticated status/cache = %d/%q", response.Code, response.Header().Get("Cache-Control"))
	}

	request = authenticatedControlRequest(t, http.MethodGet, "/api/network-control/v1/runtimes/endpoint-2/mihomo-profiles/mihomo-1/subscription", "")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || service.subscriptionID != "" {
		t.Fatalf("mismatched runtime status/profile = %d/%q", response.Code, service.subscriptionID)
	}

	request = authenticatedControlRequest(t, http.MethodGet, "/api/network-control/v1/runtimes/endpoint-1/mihomo-profiles/mihomo-1/subscription", "")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || service.subscriptionID != "mihomo-1" || !strings.Contains(response.Body.String(), "subscriptions.example.test") {
		t.Fatalf("subscription status/cache/profile/body = %d/%q/%q/%s", response.Code, response.Header().Get("Cache-Control"), service.subscriptionID, response.Body.String())
	}

	request = authenticatedControlRequest(t, http.MethodGet, "/api/network-control/v1/runtimes/endpoint-1/mihomo-profiles/mihomo-1/source", "")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || service.sourceID != "mihomo-1" || !strings.Contains(response.Body.String(), "proxy.example.test") {
		t.Fatalf("source status/cache/profile/body = %d/%q/%q/%s", response.Code, response.Header().Get("Cache-Control"), service.sourceID, response.Body.String())
	}
}

func TestRouterRequiresMTLSExactRuntimeAndBearer(t *testing.T) {
	service := &controlServiceStub{ready: true}
	router, err := NewRouter(service, controlReadyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 100})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	body := `{"schemaVersion":"network-runtime/v1alpha1"}`

	request := httptest.NewRequest(http.MethodPost, "/api/network-control/v1/runtimes/endpoint-1/enroll", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("without certificate status = %d", response.Code)
	}

	request = authenticatedControlRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/endpoint-2/enroll", body)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || service.enrollCalls != 0 {
		t.Fatalf("mismatched path status/calls = %d/%d", response.Code, service.enrollCalls)
	}

	request = authenticatedControlRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/endpoint-1/enroll", body)
	request.Header["Authorization"] = []string{"Bearer " + strings.Repeat("a", 32), "Bearer " + strings.Repeat("b", 32)}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || service.enrollCalls != 0 {
		t.Fatalf("multiple bearer headers status/calls = %d/%d", response.Code, service.enrollCalls)
	}

	token := strings.Repeat("a", 32)
	request = authenticatedControlRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/endpoint-1/enroll", body)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("authenticated enrollment status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.identity.ID != "endpoint-1" || service.token != token || string(service.raw) != body {
		t.Fatalf("service identity/token/raw = %#v / %q / %q", service.identity, service.token, service.raw)
	}
}

func TestRouterBoundsBodiesAndKeepsProbesOpen(t *testing.T) {
	service := &controlServiceStub{ready: true}
	router, err := NewRouter(service, controlReadyStoreStub{}, Options{MaxBodyBytes: 16, RequestsPerMinute: 100})
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

	request := authenticatedControlRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/endpoint-1/leases:renew", strings.Repeat("x", 17))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, body = %s", response.Code, response.Body.String())
	}

	unready, err := NewRouter(&controlServiceStub{}, controlReadyStoreStub{err: errors.New("database down")}, Options{MaxBodyBytes: 16, RequestsPerMinute: 1})
	if err != nil {
		t.Fatalf("NewRouter(unready) error = %v", err)
	}
	response = httptest.NewRecorder()
	unready.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unready status = %d", response.Code)
	}
}

func TestRouterRoutesNASAuthorizationWithNASIdentity(t *testing.T) {
	service := &controlServiceStub{ready: true}
	router, err := NewRouter(service, controlReadyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 100})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	body := `{"schemaVersion":"network-runtime/v1alpha1"}`
	request := authenticatedRuntimeRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/freeradius-hq/nas:authorize", body, "nas", "freeradius-hq")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.authorizeCalls != 1 {
		t.Fatalf("NAS authorization status/calls = %d/%d, body = %s", response.Code, service.authorizeCalls, response.Body.String())
	}
	if service.identity.Kind != "nas" || service.identity.ID != "freeradius-hq" || string(service.raw) != body {
		t.Fatalf("NAS authorization identity/raw = %#v / %q", service.identity, service.raw)
	}
}

func TestRouterRoutesVPNConnectWithEndpointIdentity(t *testing.T) {
	service := &controlServiceStub{ready: true}
	router, err := NewRouter(service, controlReadyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 100})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"schemaVersion":"network-runtime/v1alpha1"}`
	request := authenticatedControlRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/endpoint-1/vpn:connect", body)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.vpnCalls != 1 || service.identity.Kind != "endpoint" || string(service.raw) != body {
		t.Fatalf("VPN connect status/calls/identity/raw = %d/%d/%#v/%q, body = %s", response.Code, service.vpnCalls, service.identity, service.raw, response.Body.String())
	}
}

func TestRouterTranslatesRADIUSPostAuthWithoutXLAT(t *testing.T) {
	service := &controlServiceStub{ready: true, radiusResult: networkprotocol.NASAuthorizationResult{
		SessionID: "soha-session-1", Decision: "allow", RadiusAttributes: &networkprotocol.RadiusAttributes{VLANID: 30, FilterID: "soha-restricted", SessionTimeoutSeconds: 900},
	}}
	router, err := NewRouter(service, controlReadyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 100})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	body := `{"requestId":"request-1","nasId":"nas-hq","subjectId":"user-1","deviceId":"device-1","authenticationMethod":"password-compatible"}`
	request := authenticatedRuntimeRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/freeradius-hq/radius:post-auth", body, "nas", "freeradius-hq")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.radiusCalls != 1 {
		t.Fatalf("RADIUS post-auth status/calls = %d/%d, body = %s", response.Code, service.radiusCalls, response.Body.String())
	}
	for _, fragment := range []string{`"reply:Class"`, `"soha-session-1"`, `"reply:Tunnel-Type"`, `"VLAN"`, `"reply:Tunnel-Medium-Type"`, `"IEEE-802"`, `"reply:Tunnel-Private-Group-Id"`, `"30"`, `"reply:Filter-Id"`, `"soha-restricted"`, `"reply:Session-Timeout"`, `"900"`, `"do_xlat":false`} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Fatalf("RADIUS post-auth response missing %s: %s", fragment, response.Body.String())
		}
	}

	service.radiusResult = networkprotocol.NASAuthorizationResult{Decision: "deny", ReasonCode: "subject_not_active"}
	request = authenticatedRuntimeRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/freeradius-hq/radius:post-auth", body, "nas", "freeradius-hq")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("denied RADIUS post-auth status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestRouterRoutesNASSessionCommandsWithNASIdentity(t *testing.T) {
	service := &controlServiceStub{ready: true, nextCommand: networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageType: networkprotocol.MessageNASSessionCommand}}
	router, err := NewRouter(service, controlReadyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 100})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	request := authenticatedRuntimeRequest(t, http.MethodGet, "/api/network-control/v1/runtimes/freeradius-hq/nas-session-commands/next", "", "nas", "freeradius-hq")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.nextCalls != 1 {
		t.Fatalf("next command status/calls = %d/%d, body = %s", response.Code, service.nextCalls, response.Body.String())
	}

	body := `{"schemaVersion":"network-runtime/v1alpha1"}`
	request = authenticatedRuntimeRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/freeradius-hq/nas-session-commands/result", body, "nas", "freeradius-hq")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || service.resultCalls != 1 || string(service.raw) != body {
		t.Fatalf("command result status/calls/raw = %d/%d/%q, body = %s", response.Code, service.resultCalls, service.raw, response.Body.String())
	}
}

func authenticatedControlRequest(t *testing.T, method, path, body string) *http.Request {
	return authenticatedRuntimeRequest(t, method, path, body, "endpoint", "endpoint-1")
}

func authenticatedRuntimeRequest(t *testing.T, method, path, body, kind, id string) *http.Request {
	t.Helper()
	identityURI, err := url.Parse("spiffe://opensoha.local/network-control/" + kind + "/" + id)
	if err != nil {
		t.Fatalf("parse identity URI: %v", err)
	}
	certificate := &x509.Certificate{Raw: []byte("cert"), RawSubjectPublicKeyInfo: []byte("key"), URIs: []*url.URL{identityURI}}
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{certificate}}}
	return request
}
