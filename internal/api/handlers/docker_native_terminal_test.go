package handlers

import (
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
	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	"github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type dockerTerminalSpy struct {
	calls   atomic.Int32
	session appdocker.NativeDockerTerminalSession
}

func (s *dockerTerminalSpy) Create(_ context.Context, p domainidentity.Principal, a domainidentity.AccessContext, id string, input protocol.TerminalInput) (protocol.TerminalCreated, error) {
	s.calls.Add(1)
	return protocol.TerminalCreated{}, nil
}
func (s *dockerTerminalSpy) Get(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string) (protocol.TerminalLease, error) {
	s.calls.Add(1)
	return protocol.TerminalLease{}, nil
}
func (s *dockerTerminalSpy) Close(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string) (protocol.TerminalLease, error) {
	s.calls.Add(1)
	return protocol.TerminalLease{}, nil
}
func (s *dockerTerminalSpy) Acquire(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string, string) (appdocker.NativeDockerTerminalSession, error) {
	s.calls.Add(1)
	return s.session, nil
}
func dockerTerminalRouter(service DockerNativeTerminalService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewDockerHandlerWithServices(DockerServices{NativeTerminals: service}, keyring.Ring{})
	r.POST("/docker/projects/:id/terminal-sessions", h.CreateNativeDockerTerminal)
	r.GET("/docker/projects/:id/terminal-sessions/:sessionID/stream", h.StreamNativeDockerTerminal)
	return r
}
func TestDockerNativeTerminalStrictBoundary(t *testing.T) {
	valid := `{"expectedContainerId":"` + strings.Repeat("a", 64) + `","serviceName":"web"}`
	for _, scenario := range []string{"valid", "empty-shell", "unknown", "case", "duplicate", "null", "trailing", "array", "oversize", "origin", "query", "empty-query", "missing-owner", "typed-nil"} {
		t.Run(scenario, func(t *testing.T) { dockerNativeTerminalStrictBoundaryPhase1(t, &valid, &scenario) })
	}
}

type dockerDisconnectedSession struct {
	ctx              context.Context
	cancel           context.CancelFunc
	started, stopped chan struct{}
}

func (s *dockerDisconnectedSession) Context() context.Context      { return s.ctx }
func (s *dockerDisconnectedSession) Lease() protocol.TerminalLease { return protocol.TerminalLease{} }
func (s *dockerDisconnectedSession) InitialSize() resource.NativeTerminalSize {
	return resource.NativeTerminalSize{Width: 80, Height: 24}
}
func (s *dockerDisconnectedSession) Cancel()      { s.cancel() }
func (s *dockerDisconnectedSession) Close() error { s.cancel(); return nil }
func (s *dockerDisconnectedSession) Stream(io.ReadCloser, io.Writer, resource.NativeTerminalSizeQueue) (protocol.TerminalResult, error) {
	close(s.started)
	<-s.ctx.Done()
	close(s.stopped)
	return protocol.TerminalResult{Status: "unknown", ReasonCode: "transport_lost"}, s.ctx.Err()
}
func TestDockerNativeTerminalDisconnectCancelsSilentSession(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	session := &dockerDisconnectedSession{ctx: ctx, cancel: cancel, started: make(chan struct{}), stopped: make(chan struct{})}
	spy := &dockerTerminalSpy{session: session}
	server := httptest.NewServer(dockerTerminalRouter(spy))
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/docker/projects/p/terminal-sessions/s/stream"
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, url, http.Header{resource.NativePodTerminalTicketHeader: []string{strings.Repeat("A", 43)}})
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatal(err)
	}
	peer := terminalstream.NewClient(ctx, conn)
	defer func() { _ = peer.Close() }()
	if err := peer.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.started:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	_ = peer.SendEOF()
	_ = peer.Close()
	select {
	case <-session.stopped:
	case <-time.After(time.Second):
		t.Fatal("disconnect left silent provider running")
	}
}

func dockerNativeTerminalStrictBoundaryPhase1(t *testing.T, sharedValid *string, sharedScenario *string) {

	spy := &dockerTerminalSpy{}
	r := dockerTerminalRouter(spy)
	body, path := (*sharedValid), "/docker/projects/p/terminal-sessions"
	switch *sharedScenario {
	case "empty-shell":
		body = strings.TrimSuffix((*sharedValid), "}") + `,"shell":""}`
	case "unknown":
		body = strings.TrimSuffix((*sharedValid), "}") + `,"actor":"forged"}`
	case "case":
		body = strings.ReplaceAll((*sharedValid), "serviceName", "ServiceName")
	case "duplicate":
		body = strings.TrimSuffix((*sharedValid), "}") + `,"serviceName":"other"}`
	case "null":
		body = strings.TrimSuffix((*sharedValid), "}") + `,"width":null}`
	case "trailing":
		body += "{}"
	case "array":
		body = "[" + (*sharedValid) + "]"
	case "oversize":
		body = strings.Repeat(" ", 4097) + (*sharedValid)
	case "query":
		path += "?ticket=forbidden"
	case "empty-query":
		path += "?"
	case "missing-owner":
		r = dockerTerminalRouter(nil)
	case "typed-nil":
		var absent *dockerTerminalSpy
		r = dockerTerminalRouter(absent)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if (*sharedScenario) == "origin" {
		req.Header["Origin"] = []string{""}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if (*sharedScenario) == "valid" {
		if w.Code != 201 || spy.calls.Load() != 1 {
			t.Fatal(w.Code, w.Body.String())
		}
	} else if w.Code < 400 || spy.calls.Load() != 0 {
		t.Fatal("invalid input reached service", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
}
