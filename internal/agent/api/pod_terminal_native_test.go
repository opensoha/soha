package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
	"go.uber.org/zap"
)

type nativePodTerminalSpy struct {
	calls  atomic.Int64
	inputs chan contractresource.PodTerminalInput
}

func (spy *nativePodTerminalSpy) StreamNativePodTerminal(ctx context.Context, namespace, name string, input contractresource.PodTerminalInput, stdin io.ReadCloser, stdout io.Writer, sizes contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	spy.calls.Add(1)
	spy.inputs <- input
	data, err := io.ReadAll(stdin)
	if err != nil {
		return contractresource.PodTerminalResult{}, err
	}
	n, err := stdout.Write(data)
	code := int64(42)
	return contractresource.PodTerminalResult{Status: "failed", ExitCode: &code, InputBytes: int64(len(data)), OutputBytes: int64(n), UIDVerifiedBeforeStart: true, ReasonCode: "process_exited"}, err
}

func nativeTerminalQuery() url.Values {
	return url.Values{"namespace": {"test"}, "expectedUid": {"reviewed"}, "container": {"main"}, "shell": {"/bin/sh"}, "width": {"120"}, "height": {"40"}, "timeoutSeconds": {"300"}, "deadlineAt": {time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}}
}

func nativeTerminalRouter(client nativePodTerminalClient, registry *nativePodTerminals) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	platform := router.Group("/api/v1/platform")
	platform.Use(authMiddleware("test-only-agent-token"))
	registerNativePodTerminalRoute(platform, client, newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: []string{actionPlatformPodsExec}}, zap.NewNop(), nil), registry)
	return router
}

func TestNativeAgentTerminalRejectsOriginAndUnboundedQueryBeforeExec(t *testing.T) {
	for _, scenario := range []string{"origin", "extra", "duplicate", "uid", "shell", "width", "timeout", "expired", "far-deadline"} {
		t.Run(scenario, func(t *testing.T) {
			spy := &nativePodTerminalSpy{}
			router := nativeTerminalRouter(spy, newNativePodTerminals())
			query := nativeTerminalQuery()
			switch scenario {
			case "extra":
				query.Set("access_token", "forbidden")
			case "duplicate":
				query.Add("namespace", "other")
			case "uid":
				query.Del("expectedUid")
			case "shell":
				query.Set("shell", "sh -c id")
			case "width":
				query.Set("width", "1001")
			case "timeout":
				query.Set("timeoutSeconds", "901")
			case "expired":
				query.Set("deadlineAt", time.Now().Add(-time.Minute).Format(time.RFC3339Nano))
			case "far-deadline":
				query.Set("deadlineAt", time.Now().Add(time.Hour).Format(time.RFC3339Nano))
			}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/platform/pod-execution-v1/pods/app/terminal?"+query.Encode(), nil)
			request.Header.Set("Authorization", "Bearer test-only-agent-token")
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			if scenario == "origin" {
				request.Header.Set("Origin", "https://untrusted.example")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code < 400 || spy.calls.Load() != 0 {
				t.Fatalf("invalid query executed: %d attempts=%d", response.Code, spy.calls.Load())
			}
		})
	}
}

func TestNativeAgentTerminalRealWebSocketAndShutdownRegistry(t *testing.T) {
	registry := newNativePodTerminals()
	spy := &nativePodTerminalSpy{inputs: make(chan contractresource.PodTerminalInput, 1)}
	server := httptest.NewServer(nativeTerminalRouter(spy, registry))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	header := http.Header{"Authorization": []string{"Bearer test-only-agent-token"}}
	conn, handshake, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/platform/pod-execution-v1/pods/app/terminal?"+nativeTerminalQuery().Encode(), header)
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
	data := []byte{0, 255, 27, '\n'}
	if _, err = stream.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = stream.SendEOF(); err != nil {
		t.Fatal(err)
	}
	output, _ := io.ReadAll(stream)
	<-stream.Done()
	result, known := stream.Result()
	input := <-spy.inputs
	if !bytes.Equal(data, output) || !known || result.ExitCode == nil || *result.ExitCode != 42 || input.ExpectedUID != "reviewed" || input.Width != 120 || spy.calls.Load() != 1 {
		t.Fatalf("output=%v result=%+v input=%+v", output, result, input)
	}
	if err = registry.shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, admitted := registry.acquire(ctx); admitted {
		t.Fatal("shutdown registry admitted new terminal")
	}
}

func TestNativeAgentTerminalBoundedAdmissionCancelsAndWaits(t *testing.T) {
	registry := newNativePodTerminals()
	contexts := make([]context.Context, 0, 64)
	releases := make([]func(), 0, 64)
	for range 64 {
		ctx, release, ok := registry.acquire(t.Context())
		if !ok {
			t.Fatal("early capacity rejection")
		}
		contexts = append(contexts, ctx)
		releases = append(releases, release)
	}
	if _, _, ok := registry.acquire(t.Context()); ok {
		t.Fatal("unbounded admission")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- registry.shutdown(ctx) }()
	for _, active := range contexts {
		select {
		case <-active.Done():
		case <-ctx.Done():
			t.Fatal("shutdown did not cancel")
		}
	}
	select {
	case <-done:
		t.Fatal("shutdown did not wait")
	default:
	}
	for _, release := range releases {
		release()
		release()
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type disconnectNativePodClient struct{ started, stopped chan struct{} }

func (client disconnectNativePodClient) StreamNativePodTerminal(ctx context.Context, _, _ string, _ contractresource.PodTerminalInput, _ io.ReadCloser, _ io.Writer, _ contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	close(client.started)
	<-ctx.Done()
	close(client.stopped)
	return contractresource.PodTerminalResult{Status: "canceled", ReasonCode: "request_canceled"}, nil
}

func TestNativeAgentWebSocketDisconnectCancelsKubernetesProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client := disconnectNativePodClient{started: make(chan struct{}), stopped: make(chan struct{})}
	registry := newNativePodTerminals()
	server := httptest.NewServer(nativeTerminalRouter(client, registry))
	defer server.Close()
	header := http.Header{"Authorization": []string{"Bearer test-only-agent-token"}}
	conn, handshake, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/platform/pod-execution-v1/pods/app/terminal?"+nativeTerminalQuery().Encode(), header)
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
	<-client.started
	_ = conn.Close()
	select {
	case <-client.stopped:
	case <-time.After(time.Second):
		t.Fatal("disconnected Core left Kubernetes stream running")
	}
	if err = registry.shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
