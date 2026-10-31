package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	"github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
	"go.uber.org/zap"
)

type dockerTerminalSpy struct {
	calls, caps atomic.Int32
	requests    chan protocol.TerminalRequest
}

func (s *dockerTerminalSpy) Capabilities(context.Context) (protocol.TerminalCapabilities, error) {
	s.caps.Add(1)
	return protocol.TerminalCapabilities{Protocol: protocol.TerminalProtocol, NativePTY: true, MaxTimeoutSeconds: 900, MaxInputBytes: 1 << 20, MaxOutputBytes: 32 << 20}, nil
}
func (s *dockerTerminalSpy) Stream(ctx context.Context, input protocol.TerminalRequest, stdin io.ReadCloser, stdout io.Writer, sizes resource.NativeTerminalSizeQueue) (protocol.TerminalResult, error) {
	s.calls.Add(1)
	s.requests <- input
	data, err := io.ReadAll(stdin)
	if err != nil {
		return protocol.TerminalResult{}, err
	}
	n, err := stdout.Write(data)
	code := int64(42)
	return protocol.TerminalResult{Status: "failed", ExitCode: &code, InputBytes: int64(len(data)), OutputBytes: int64(n), UIDVerifiedBeforeStart: true, ReasonCode: "process_exited"}, err
}

func dockerTerminalHeader(t *testing.T) http.Header {
	t.Helper()
	binding, err := protocol.NewProjectBinding("6f773cfe-a522-468c-8c85-4b326d41b781", "65b279e6-ea7c-412f-a822-228349f35585")
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.TerminalRequest{Protocol: protocol.TerminalProtocol, ProjectID: "project:owned", Container: protocol.ContainerBinding{ProjectBinding: binding, ServiceName: "owned", ContainerID: strings.Repeat("a", 64)}, Input: protocol.TerminalInput{ServiceName: "owned", ExpectedContainerID: strings.Repeat("a", 64), Width: 120, Height: 40}, DeadlineAt: time.Now().Add(time.Minute).UTC()}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return http.Header{"Authorization": []string{"Bearer test-only-agent-token"}, protocol.TerminalInputHeader: []string{base64.RawURLEncoding.EncodeToString(raw)}}
}

func dockerTerminalRouter(spy dockerNativeTerminalClient, registry *nativePodTerminals, action string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1/docker/runtime")
	group.Use(authMiddleware("test-only-agent-token"))
	registerDockerNativeTerminal(group, newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: []string{action}}, zap.NewNop(), nil), registry, spy)
	return router
}

func TestNativeDockerAgentTerminalRejectsUnboundHeaderAndLegacyAllowlist(t *testing.T) {
	for _, fault := range []string{"origin", "query", "duplicate-header", "invalid-base64", "extra-field", "missing-cid", "legacy-action", "no-auth"} {
		t.Run(fault, func(t *testing.T) {
			spy := &dockerTerminalSpy{}
			header := dockerTerminalHeader(t)
			action := actionDockerNativeTerminal
			path := "/api/v1/docker/runtime/v1/terminal/stream"
			switch fault {
			case "origin":
				header.Set("Origin", "https://untrusted.example")
			case "query":
				path += "?access_token=forbidden"
			case "duplicate-header":
				header.Add(protocol.TerminalInputHeader, header.Get(protocol.TerminalInputHeader))
			case "invalid-base64":
				header.Set(protocol.TerminalInputHeader, "not_base64==")
			case "extra-field", "missing-cid":
				raw, _ := base64.RawURLEncoding.DecodeString(header.Get(protocol.TerminalInputHeader))
				var object map[string]any
				_ = json.Unmarshal(raw, &object)
				if fault == "extra-field" {
					object["unsafe"] = true
				} else {
					input, ok := object["input"].(map[string]any)
					if !ok {
						t.Fatal("terminal fixture input is not an object")
					}
					input["expectedContainerId"] = "prefix"
				}
				raw, _ = json.Marshal(object)
				header.Set(protocol.TerminalInputHeader, base64.RawURLEncoding.EncodeToString(raw))
			case "legacy-action":
				action = actionDockerRuntimeTerminal
			case "no-auth":
				header.Del("Authorization")
			}
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header = header
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			response := httptest.NewRecorder()
			dockerTerminalRouter(spy, newNativePodTerminals(), action).ServeHTTP(response, request)
			if response.Code < 400 || spy.calls.Load() != 0 || spy.caps.Load() != 0 {
				t.Fatalf("unbound input reached runtime status=%d", response.Code)
			}
		})
	}
}

func TestNativeDockerAgentTerminalHasBoundedSharedFramesAndLifecycle(t *testing.T) {
	registry := newNativePodTerminals()
	spy := &dockerTerminalSpy{requests: make(chan protocol.TerminalRequest, 1)}
	server := httptest.NewServer(dockerTerminalRouter(spy, registry, actionDockerNativeTerminal))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, handshake, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/docker/runtime/v1/terminal/stream", dockerTerminalHeader(t))
	if handshake != nil && handshake.Body != nil {
		defer func() { _ = handshake.Body.Close() }()
	}
	if err != nil {
		t.Fatal(err)
	}
	stream := terminalstream.NewClient(ctx, conn)
	defer func() { _ = stream.Close() }()
	if err := stream.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	data := []byte{0, 255, 27, '\n'}
	if _, err := stream.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := stream.SendEOF(); err != nil {
		t.Fatal(err)
	}
	output, _ := io.ReadAll(stream)
	<-stream.Done()
	result, known := stream.Result()
	input := <-spy.requests
	if !known || result.ExitCode == nil || *result.ExitCode != 42 || !bytes.Equal(output, data) || input.Container.ContainerID != strings.Repeat("a", 64) || input.Input.Width != 120 || spy.calls.Load() != 1 {
		t.Fatalf("original wire outcome=%+v", result)
	}
	if err := registry.shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, allowed := registry.acquire(ctx); allowed {
		t.Fatal("shutdown registry admitted another PTY")
	}
}

func TestNativeDockerAgentCapabilitiesRequireNewAction(t *testing.T) {
	for _, action := range []string{actionDockerRuntimeTerminal, actionDockerNativeTerminal} {
		spy := &dockerTerminalSpy{}
		router := dockerTerminalRouter(spy, newNativePodTerminals(), action)
		request := httptest.NewRequest(http.MethodGet, "/api/v1/docker/runtime/v1/terminal/capabilities", nil)
		request.Header.Set("Authorization", "Bearer test-only-agent-token")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if action == actionDockerNativeTerminal {
			if response.Code != 200 || spy.caps.Load() != 1 || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("native capability unavailable")
			}
		} else if response.Code != 403 || spy.caps.Load() != 0 {
			t.Fatal("legacy allowlist enabled the native protocol")
		}
	}
}
