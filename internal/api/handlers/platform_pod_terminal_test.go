package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
	appresource "github.com/opensoha/soha/internal/application/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type nativeTerminalHandlerSpy struct {
	calls                    atomic.Int64
	actor, sessionID, ticket string
	input                    contractresource.PodTerminalInput
	session                  appresource.NativePodTerminalSession
}

func (spy *nativeTerminalHandlerSpy) Create(_ context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, _, _, _ string, input contractresource.PodTerminalInput) (contractresource.PodTerminalCreated, error) {
	spy.calls.Add(1)
	spy.actor, spy.sessionID, spy.input = principal.UserID, access.SessionID, input
	return contractresource.PodTerminalCreated{}, nil
}
func (spy *nativeTerminalHandlerSpy) Get(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.PodTerminalLease, error) {
	spy.calls.Add(1)
	return contractresource.PodTerminalLease{}, nil
}
func (spy *nativeTerminalHandlerSpy) Close(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.PodTerminalLease, error) {
	spy.calls.Add(1)
	return contractresource.PodTerminalLease{}, nil
}
func (spy *nativeTerminalHandlerSpy) Acquire(_ context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, _, _, _, _, ticket string) (appresource.NativePodTerminalSession, error) {
	spy.calls.Add(1)
	spy.actor, spy.sessionID, spy.ticket = principal.UserID, access.SessionID, ticket
	return spy.session, nil
}

func terminalHandlerRouter(spy PodTerminalService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &podResourceHandler{terminals: spy}
	router.Use(func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{UserID: "trusted-actor"})
		c.Set("access_context", domainidentity.AccessContext{SessionID: "trusted-session", AuthzVersion: 3})
	})
	router.POST("/pods/:podName/terminal-sessions", handler.CreatePodTerminal)
	router.GET("/pods/:podName/terminal-sessions/:sessionID/stream", handler.StreamNativePodTerminal)
	return router
}

func TestNativeTerminalHandlerStrictBoundary(t *testing.T) {
	for _, scenario := range []string{"valid", "unknown-field", "trailing", "missing-uid", "oversize", "origin", "extra-query", "duplicate-namespace", "missing-owner"} {
		t.Run(scenario, func(t *testing.T) {
			spy := &nativeTerminalHandlerSpy{}
			router := terminalHandlerRouter(spy)
			body := `{"expectedUid":"reviewed","width":120,"height":40}`
			path := "/pods/app/terminal-sessions?namespace=test"
			switch scenario {
			case "unknown-field":
				body = `{"expectedUid":"reviewed","actor":"attacker"}`
			case "trailing":
				body += "{}"
			case "missing-uid":
				body = `{}`
			case "oversize":
				body = `{"expectedUid":"` + strings.Repeat("a", 5000) + `"}`
			case "extra-query":
				path += "&access_token=forbidden"
			case "duplicate-namespace":
				path += "&namespace=other"
			case "missing-owner":
				router = terminalHandlerRouter(nil)
			}
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			if scenario == "origin" {
				request.Header.Set("Origin", "https://untrusted.example")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if scenario == "valid" {
				if response.Code != 201 || spy.calls.Load() != 1 || spy.actor != "trusted-actor" || spy.sessionID != "trusted-session" || spy.input.Width != 120 || spy.input.Shell != "/bin/sh" || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("invalid create boundary: %d %+v", response.Code, spy)
				}
			} else if response.Code < 400 || spy.calls.Load() != 0 {
				t.Fatalf("invalid request reached service: %d %d", response.Code, spy.calls.Load())
			}
		})
	}
}

func TestNativeTerminalWebSocketPreflightDoesNotConsumeTicket(t *testing.T) {
	for _, scenario := range []string{"missing-ticket", "duplicate-ticket", "origin", "query-token", "not-upgrade"} {
		t.Run(scenario, func(t *testing.T) {
			spy := &nativeTerminalHandlerSpy{}
			router := terminalHandlerRouter(spy)
			path := "/pods/app/terminal-sessions/id/stream?namespace=test"
			if scenario == "query-token" {
				path += "&ticket=forbidden"
			}
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			request.Header.Set(contractresource.NativePodTerminalTicketHeader, strings.Repeat("A", 43))
			switch scenario {
			case "missing-ticket":
				request.Header.Del(contractresource.NativePodTerminalTicketHeader)
			case "duplicate-ticket":
				request.Header.Add(contractresource.NativePodTerminalTicketHeader, strings.Repeat("B", 43))
			case "origin":
				request.Header.Set("Origin", "https://untrusted.example")
			case "not-upgrade":
				request.Header.Del("Upgrade")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code < 400 || spy.calls.Load() != 0 {
				t.Fatalf("preflight consumed ticket: %d %d", response.Code, spy.calls.Load())
			}
		})
	}
}

type nativeTerminalSessionStub struct {
	ctx    context.Context
	cancel context.CancelFunc
	closed atomic.Bool
	size   atomic.Int64
}

func (session *nativeTerminalSessionStub) Context() context.Context { return session.ctx }
func (session *nativeTerminalSessionStub) Lease() contractresource.PodTerminalLease {
	return contractresource.PodTerminalLease{Status: "active"}
}
func (session *nativeTerminalSessionStub) InitialSize() contractresource.NativeTerminalSize {
	return contractresource.NativeTerminalSize{Width: 120, Height: 40}
}
func (session *nativeTerminalSessionStub) Close() error {
	session.closed.Store(true)
	session.cancel()
	return nil
}
func (session *nativeTerminalSessionStub) Stream(stdin io.ReadCloser, stdout io.Writer, sizes contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	size := sizes.Next(session.ctx)
	if size != nil {
		session.size.Store(int64(size.Width))
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return contractresource.PodTerminalResult{}, err
	}
	n, err := stdout.Write(data)
	code := int64(42)
	return contractresource.PodTerminalResult{Status: "failed", ExitCode: &code, UIDVerifiedBeforeStart: true, InputBytes: int64(len(data)), OutputBytes: int64(n), ReasonCode: "process_exited"}, err
}

func TestNativeTerminalHandlerRealWebSocketBinaryEOFAndExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	serverCtx, serverCancel := context.WithCancel(t.Context())
	defer serverCancel()
	session := &nativeTerminalSessionStub{ctx: serverCtx, cancel: serverCancel}
	spy := &nativeTerminalHandlerSpy{session: session}
	server := httptest.NewServer(terminalHandlerRouter(spy))
	defer server.Close()
	header := http.Header{contractresource.NativePodTerminalTicketHeader: []string{strings.Repeat("A", 43)}}
	conn, handshake, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/pods/app/terminal-sessions/id/stream?namespace=test", header)
	if handshake != nil && handshake.Body != nil {
		defer func() { _ = handshake.Body.Close() }()
	}
	if err != nil {
		t.Fatal(err)
	}
	stream := terminalstream.NewClient(ctx, conn)
	defer func() { _ = stream.Close() }()
	if err = stream.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	input := []byte{0, 255, 27, '\n'}
	if _, err = stream.Write(input); err != nil {
		t.Fatal(err)
	}
	if err = stream.SendEOF(); err != nil {
		t.Fatal(err)
	}
	output, _ := io.ReadAll(stream)
	<-stream.Done()
	result, known := stream.Result()
	if !bytes.Equal(input, output) || !known || result.ExitCode == nil || *result.ExitCode != 42 || session.size.Load() != 120 {
		t.Fatalf("output=%v result=%+v size=%d", output, result, session.size.Load())
	}
	for !session.closed.Load() {
		select {
		case <-ctx.Done():
			t.Fatal("session was not closed")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if spy.actor != "trusted-actor" || spy.sessionID != "trusted-session" || spy.ticket != strings.Repeat("A", 43) {
		t.Fatal("untrusted authentication/ticket binding")
	}
}

type disconnectNativeSessionStub struct {
	*nativeTerminalSessionStub
	started, stopped chan struct{}
}

func (session *disconnectNativeSessionStub) Stream(io.ReadCloser, io.Writer, contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	close(session.started)
	<-session.ctx.Done()
	close(session.stopped)
	return contractresource.PodTerminalResult{Status: "canceled", ReasonCode: "request_canceled"}, nil
}

func TestNativeTerminalWebSocketDisconnectCancelsProviderWithoutRESTClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session := &disconnectNativeSessionStub{nativeTerminalSessionStub: &nativeTerminalSessionStub{ctx: ctx, cancel: cancel}, started: make(chan struct{}), stopped: make(chan struct{})}
	server := httptest.NewServer(terminalHandlerRouter(&nativeTerminalHandlerSpy{session: session}))
	defer server.Close()
	header := http.Header{contractresource.NativePodTerminalTicketHeader: []string{strings.Repeat("A", 43)}}
	conn, handshake, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/pods/app/terminal-sessions/id/stream?namespace=test", header)
	if handshake != nil && handshake.Body != nil {
		defer func() { _ = handshake.Body.Close() }()
	}
	if err != nil {
		t.Fatal(err)
	}
	stream := terminalstream.NewClient(ctx, conn)
	defer func() { _ = stream.Close() }()
	if err = stream.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	<-session.started
	_ = conn.Close()
	select {
	case <-session.stopped:
	case <-time.After(time.Second):
		t.Fatal("WebSocket disconnect left provider session running")
	}
}
