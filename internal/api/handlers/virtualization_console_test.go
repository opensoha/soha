package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	"github.com/opensoha/soha/internal/application/virtualization/consoleport"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	virtualizationinfra "github.com/opensoha/soha/internal/infrastructure/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const vmConsolePath = "/api/v1/virtualization/vms/vm-1/console"

type vmConsoleHandshakeFixture struct {
	name         string
	kind         string
	target       string
	token        string
	subprotocol  string
	tls          bool
	headers      http.Header
	backendQuery string
}

func TestStreamVMConsoleProviderHandshake(t *testing.T) {
	const rfbTicket = "test-rfb-ticket"
	const providerCookie = "PVEAuthCookie=test-auth-ticket"
	const providerBearer = "Bearer test-kubevirt-token"
	for _, test := range []vmConsoleHandshakeFixture{
		{
			name: "PVE ticket and cookie", kind: "novnc", token: rfbTicket, subprotocol: "binary",
			target: "/api2/json/nodes/pve-a/qemu/101/vncwebsocket", backendQuery: "?port=5901",
			headers: http.Header{"Cookie": {providerCookie}},
		},
		{
			name: "PVE configured self-signed TLS", kind: "novnc", token: rfbTicket, subprotocol: "binary", tls: true,
			target: "/pve/api2/json/nodes/pve-a/qemu/101/vncwebsocket", backendQuery: "?port=5901&vncticket=" + rfbTicket,
			headers: http.Header{"Cookie": {providerCookie}},
		},
		{
			name: "KubeVirt target and authentication", kind: "vnc",
			target:  "/apis/subresources.kubevirt.io/v1/namespaces/apps/virtualmachineinstances/demo/vnc",
			headers: http.Header{"Authorization": {providerBearer}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			backendDone := make(chan struct{})
			backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(backendDone)
				serveVMConsoleProviderHandshake(t, test, w, r)
			}))
			if test.tls {
				backend.StartTLS()
			} else {
				backend.Start()
			}
			defer backend.Close()

			result := consoleport.ConsoleURLResult{
				Type: test.kind, URL: vmConsolePath + "/" + test.kind, Token: test.token, Ready: true,
				BackendURL: backend.URL + test.target + test.backendQuery, BackendHeaders: test.headers,
				BackendTLS: consoleport.BackendTLS{InsecureSkipVerify: test.tls},
			}
			server, _ := newVMConsoleTestServer(t, &consoleURLStubService{result: result}, virtualizationinfra.DialConsole)
			console, err := server.Client().Do(newVMConsoleBrowserRequest(t, http.MethodGet, server.URL+vmConsolePath, ""))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = console.Body.Close() }()
			var data struct {
				Data consoleport.ConsoleURLResult
			}
			if err := json.NewDecoder(console.Body).Decode(&data); err != nil || console.StatusCode != http.StatusOK ||
				data.Data.URL != result.URL || data.Data.Token != result.Token || !data.Data.Ready ||
				data.Data.BackendHeaders != nil || data.Data.BackendTLS.InsecureSkipVerify {
				t.Fatalf("unexpected browser console response status=%d err=%v", console.StatusCode, err)
			}
			ticket := issueVMConsoleStreamTicket(t, server, data.Data.URL)
			conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+data.Data.URL+"?stream_ticket="+ticket, http.Header{"Origin": {server.URL}})
			if response != nil {
				defer func() { _ = response.Body.Close() }()
			}
			if err != nil {
				t.Fatalf("dial browser console: %v", err)
			}
			defer func() { _ = conn.Close() }()
			if err := conn.WriteMessage(websocket.BinaryMessage, []byte("hello")); err != nil {
				t.Fatal(err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			kind, payload, err := conn.ReadMessage()
			if err != nil || kind != websocket.TextMessage || string(payload) != "ready" {
				t.Fatalf("browser read type=%d payload=%q err=%v", kind, payload, err)
			}
			_ = conn.Close()
			waitProxyDone(t, backendDone)
		})
	}
}

func serveVMConsoleProviderHandshake(t *testing.T, fixture vmConsoleHandshakeFixture, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	query := r.URL.Query()
	if r.URL.Path != fixture.target || query.Get("stream_ticket") != "" ||
		query.Get("vncticket") != fixture.token || (fixture.kind == "novnc" && query.Get("port") != "5901") ||
		r.Header.Get("Sec-WebSocket-Protocol") != fixture.subprotocol ||
		r.Header.Get("Cookie") != fixture.headers.Get("Cookie") ||
		r.Header.Get("Authorization") != fixture.headers.Get("Authorization") {
		t.Error("upstream received unexpected console path, query, subprotocol, or credentials")
		http.Error(w, "bad console handshake", http.StatusBadRequest)
		return
	}
	upgrader := websocket.Upgrader{Subprotocols: []string{"binary"}}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		t.Errorf("upgrade upstream: %v", err)
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	kind, data, err := conn.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage || string(data) != "hello" {
		t.Errorf("upstream read type=%d data=%q err=%v", kind, data, err)
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ready")); err != nil {
		t.Errorf("upstream write: %v", err)
		return
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Error("upstream stream stayed open after browser disconnect")
	} else if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
		t.Error("upstream stream timed out waiting for browser disconnect")
	}
}

func TestStreamVMConsoleRejectsBeforeDial(t *testing.T) {
	for _, test := range []struct {
		name     string
		want     int
		expire   bool
		noTicket bool
		wrongVM  bool
		origin   string
		authErr  error
	}{
		{name: "missing stream ticket", want: http.StatusUnauthorized, noTicket: true},
		{name: "expired stream ticket", want: http.StatusUnauthorized, expire: true},
		{name: "stream ticket bound to a different VM", want: http.StatusUnauthorized, wrongVM: true},
		{name: "lacks VM console permission", want: http.StatusForbidden, authErr: apperrors.ErrAccessDenied},
		{name: "untrusted browser origin", want: http.StatusForbidden, origin: "https://untrusted.example"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var dials atomic.Int32
			server, store := newVMConsoleTestServer(t, &consoleURLStubService{
				result: consoleport.ConsoleURLResult{Ready: true}, err: test.authErr,
			}, func(context.Context, consoleport.ConsoleURLResult) (consoleport.Stream, error) {
				dials.Add(1)
				return nil, consoleport.ErrBackendConnection
			})
			path := vmConsolePath + "/novnc"
			query := "?token=provider-ticket-cannot-authorize"
			if !test.noTicket {
				ticket := issueVMConsoleStreamTicket(t, server, path)
				if test.expire {
					store.expireTicket()
				}
				query = "?stream_ticket=" + ticket
			}
			if test.wrongVM {
				path = strings.Replace(path, "vm-1", "vm-2", 1)
			}
			conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+path+query, http.Header{"Origin": {test.origin}})
			if conn != nil {
				_ = conn.Close()
			}
			if response != nil {
				defer func() { _ = response.Body.Close() }()
			}
			if err == nil || response == nil || response.StatusCode != test.want {
				t.Fatalf("websocket response=%v err=%v, want HTTP %d", response, err, test.want)
			}
			if got := dials.Load(); got != 0 {
				t.Fatalf("upstream dials=%d, want zero", got)
			}
		})
	}
}

func TestStreamVMConsoleSanitizesDialErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want consoleport.DialError
	}{
		{name: "raw provider error", err: errors.New("provider-secret: connection refused"), want: consoleport.ErrBackendConnection},
		{name: "unknown typed error", err: consoleport.DialError("provider-secret: unknown failure"), want: consoleport.ErrBackendConnection},
		{name: "wrapped known error", err: fmt.Errorf("provider-secret: %w", consoleport.ErrInvalidBackendTLS), want: consoleport.ErrInvalidBackendTLS},
		{name: "unsupported console", err: consoleport.ErrUnsupportedConsole, want: consoleport.ErrUnsupportedConsole},
		{name: "invalid URL", err: consoleport.ErrInvalidBackendURL, want: consoleport.ErrInvalidBackendURL},
		{name: "KubeVirt TLS", err: consoleport.ErrInvalidKubeVirtTLS, want: consoleport.ErrInvalidKubeVirtTLS},
		{name: "KubeVirt connection", err: consoleport.ErrKubeVirtConnection, want: consoleport.ErrKubeVirtConnection},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, _ := newVMConsoleTestServer(t, &consoleURLStubService{result: consoleport.ConsoleURLResult{Ready: true}},
				func(context.Context, consoleport.ConsoleURLResult) (consoleport.Stream, error) { return nil, test.err })
			path := vmConsolePath + "/novnc"
			ticket := issueVMConsoleStreamTicket(t, server, path)
			conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+path+"?stream_ticket="+ticket, http.Header{"Origin": {server.URL}})
			if response != nil {
				defer func() { _ = response.Body.Close() }()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			kind, payload, err := conn.ReadMessage()
			var message struct {
				Error string `json:"error"`
			}
			if err != nil || kind != websocket.TextMessage || json.Unmarshal(payload, &message) != nil || message.Error != string(test.want) {
				t.Fatalf("unexpected browser error frame type=%d payload=%q err=%v", kind, payload, err)
			}
		})
	}
}

func TestVirtualizationHandlerRequiresConsoleDialerWithRuntime(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("handler accepted a nil console dialer")
		}
	}()
	var missing consoleport.Dialer
	NewVirtualizationHandlerWithServices(VirtualizationServices{Runtime: &consoleURLStubService{}, ConsoleDialer: missing})
}

func newVMConsoleTestServer(t *testing.T, service VirtualizationRuntimeService, dialer consoleport.Dialer) (*httptest.Server, *vmConsoleIdentityStore) {
	t.Helper()
	store := &vmConsoleIdentityStore{}
	identity, err := appidentity.New(appidentity.Dependencies{
		Accounts: store, Passwords: store, Authorization: store,
		RoleBindings: store, TeamBindings: store, Identities: store,
		Sessions: store, SessionAdmin: store, EphemeralTokens: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := NewAuthHandlerWithServices(nil, nil, nil, nil, identity, nil, nil, cfgpkg.AuthConfig{})
	handler := NewVirtualizationHandlerWithServices(VirtualizationServices{Runtime: service, ConsoleDialer: dialer})
	router := gin.New()
	protected := router.Group("/api/v1", apiMiddleware.BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, vmConsoleBrowserAuth{identity}), apiMiddleware.RequireAuth())
	protected.POST("/auth/stream-ticket", auth.IssueStreamTicket)
	protected.GET("/virtualization/vms/:id/console", handler.GetConsoleURL)
	protected.GET("/virtualization/vms/:id/console/novnc", handler.StreamVMConsole)
	protected.GET("/virtualization/vms/:id/console/vnc", handler.StreamVMConsole)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, store
}

func newVMConsoleBrowserRequest(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer browser-access")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func issueVMConsoleStreamTicket(t *testing.T, server *httptest.Server, path string) string {
	t.Helper()
	req := newVMConsoleBrowserRequest(t, http.MethodPost, server.URL+"/api/v1/auth/stream-ticket", `{"path":"`+path+`"}`)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	var data struct {
		Data domainidentity.StreamTicket
	}
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil || response.StatusCode != http.StatusCreated || data.Data.Ticket == "" {
		t.Fatalf("issue stream ticket status=%d err=%v", response.StatusCode, err)
	}
	return data.Data.Ticket
}

// Only the initial browser session is simulated. Stream ticket issue and consumption use the real identity service.
type vmConsoleBrowserAuth struct{ *appidentity.Service }

func (vmConsoleBrowserAuth) ParseAccessToken(_ context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	if token != "browser-access" {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, apperrors.ErrUnauthorized
	}
	return domainidentity.Principal{UserID: "u1"}, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "s1"}, nil
}

type vmConsoleIdentityStore struct {
	appidentity.UserAccountStore
	appidentity.PasswordStore
	appidentity.RoleBindingStore
	appidentity.TeamBindingStore
	appidentity.ExternalIdentityStore
	appidentity.SessionStore
	appidentity.SessionAdminStore
	appidentity.EphemeralTokenStore
	mu     sync.Mutex
	ticket domainidentity.EphemeralToken
}

func (*vmConsoleIdentityStore) GetAuthSessionByID(_ context.Context, id string) (domainidentity.Session, error) {
	return domainidentity.Session{ID: id, UserID: "u1", Status: "active", AuthzVersion: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (*vmConsoleIdentityStore) GetByID(_ context.Context, id string) (domainidentity.User, error) {
	return domainidentity.User{ID: id, Username: "browser"}, nil
}

func (*vmConsoleIdentityStore) GetAuthzState(_ context.Context, id string) (domainidentity.AuthzState, error) {
	return domainidentity.AuthzState{UserID: id, Status: "active", AuthzVersion: 1}, nil
}

func (*vmConsoleIdentityStore) ListRoles(context.Context, string) ([]string, error) { return nil, nil }
func (*vmConsoleIdentityStore) ListTeams(context.Context, string) ([]string, error) { return nil, nil }
func (*vmConsoleIdentityStore) ListProjects(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *vmConsoleIdentityStore) CreateEphemeralToken(_ context.Context, ticket domainidentity.EphemeralToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ticket = ticket
	return nil
}

func (s *vmConsoleIdentityStore) ConsumeEphemeralToken(_ context.Context, token, kind string) (domainidentity.EphemeralToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket := s.ticket
	if ticket.Token != token || ticket.Kind != kind || !ticket.ExpiresAt.After(time.Now()) {
		return domainidentity.EphemeralToken{}, apperrors.ErrUnauthorized
	}
	s.ticket = domainidentity.EphemeralToken{}
	return ticket, nil
}

func (s *vmConsoleIdentityStore) expireTicket() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ticket.ExpiresAt = time.Now().Add(-time.Second)
}
