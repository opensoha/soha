package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	"github.com/opensoha/soha/agent/internal/agent/dockerruntime"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	"github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/contracts/resource/terminalstream"
)

type dockerNativeTerminalClient interface {
	Capabilities(context.Context) (protocol.TerminalCapabilities, error)
	Stream(context.Context, protocol.TerminalRequest, io.ReadCloser, io.Writer, resource.NativeTerminalSizeQueue) (protocol.TerminalResult, error)
}

type dockerEngineTerminalClient struct{}

func (dockerEngineTerminalClient) Capabilities(ctx context.Context) (protocol.TerminalCapabilities, error) {
	return dockerruntime.NativeTerminalCapabilities(ctx)
}
func (dockerEngineTerminalClient) Stream(ctx context.Context, input protocol.TerminalRequest, stdin io.ReadCloser, stdout io.Writer, sizes resource.NativeTerminalSizeQueue) (protocol.TerminalResult, error) {
	return dockerruntime.StreamNativeTerminal(ctx, input, stdin, stdout, sizes)
}

func registerNativeDockerTerminalRoutes(router *gin.Engine, cfg cfgpkg.Config, actions actionPolicy) *nativePodTerminals {
	group := router.Group(fmt.Sprintf("%s/docker/runtime", cfg.HTTP.BasePath))
	group.Use(authRequiredAnyMiddleware(cfg.Auth.BearerToken, cfg.ControlPlane.BearerToken))
	// The registry is only bounded cancellation/lifecycle ownership; it has no
	// Pod policy or target state and is shared as an implementation primitive.
	registry := newNativePodTerminals()
	registerDockerNativeTerminal(group, actions, registry, dockerEngineTerminalClient{})
	return registry
}

func registerDockerNativeTerminal(group *gin.RouterGroup, actions actionPolicy, registry *nativePodTerminals, client dockerNativeTerminalClient) {
	group.GET("/v1/terminal/capabilities", actions.Require(actionDockerNativeTerminal), func(c *gin.Context) {
		if len(c.Request.Header.Values("Origin")) != 0 || c.Request.URL.RawQuery != "" {
			apiresponse.Error(c, 400, "invalid_request", "native terminal capabilities require no Origin or query")
			return
		}
		caps, err := client.Capabilities(c.Request.Context())
		if err != nil || caps.Protocol != protocol.TerminalProtocol || !caps.NativePTY || caps.MaxInputBytes != resource.NativePodTerminalMaxInputBytes || caps.MaxOutputBytes != resource.NativePodTerminalMaxOutputBytes || caps.MaxTimeoutSeconds != resource.NativePodTerminalMaxTimeout {
			apiresponse.Error(c, 501, "unsupported", "native Docker Engine terminal is unavailable")
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, 200, caps)
	})
	group.GET("/v1/terminal/stream", actions.Require(actionDockerNativeTerminal), func(c *gin.Context) {
		request, ok := decodeDockerNativeTerminal(c)
		if !ok {
			return
		}
		ctx, stop := context.WithDeadline(c.Request.Context(), request.DeadlineAt)
		defer stop()
		ctx, release, admitted := registry.acquire(ctx)
		if !admitted {
			apiresponse.Error(c, 503, "service_unavailable", "native terminal capacity unavailable")
			return
		}
		defer release()
		upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(r *http.Request) bool { return len(r.Header.Values("Origin")) == 0 }}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, http.Header{"Cache-Control": []string{"no-store"}})
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		stream := terminalstream.NewServer(ctx, conn, resource.NativeTerminalSize{Width: request.Input.Width, Height: request.Input.Height})
		defer func() { _ = stream.Close() }()
		if stream.SendReady() != nil {
			return
		}
		result, err := client.Stream(stream.Context(), request, stream.Input(), stream, stream)
		if err != nil || result.Validate() != nil {
			_ = stream.SendError("transport_lost")
			return
		}
		_ = stream.SendExit(result)
	})
}

func decodeDockerNativeTerminal(c *gin.Context) (protocol.TerminalRequest, bool) {
	invalid := func() (protocol.TerminalRequest, bool) {
		apiresponse.Error(c, 400, "invalid_request", "invalid native Docker terminal protocol")
		return protocol.TerminalRequest{}, false
	}
	if len(c.Request.Header.Values("Origin")) != 0 || c.Request.URL.RawQuery != "" || !websocket.IsWebSocketUpgrade(c.Request) {
		return invalid()
	}
	values := c.Request.Header.Values(protocol.TerminalInputHeader)
	if len(values) != 1 || len(values[0]) > 8192 {
		return invalid()
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(values[0])
	if err != nil || len(raw) > 4096 || !utf8.Valid(raw) || base64.RawURLEncoding.EncodeToString(raw) != values[0] {
		return invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input protocol.TerminalRequest
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return invalid()
	}
	input, err = input.Normalized(time.Now().UTC())
	if err != nil {
		return invalid()
	}
	return input, true
}
