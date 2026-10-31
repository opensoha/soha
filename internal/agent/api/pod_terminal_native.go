package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type nativePodTerminalClient interface {
	StreamNativePodTerminal(context.Context, string, string, contractresource.PodTerminalInput, io.ReadCloser, io.Writer, contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error)
}

func registerNativePodTerminalRoute(platform *gin.RouterGroup, client nativePodTerminalClient, actions actionPolicy, registry *nativePodTerminals) {
	platform.GET("/pod-execution-v1/pods/:podName/terminal", actions.Require(actionPlatformPodsExec), func(c *gin.Context) {
		if len(c.Request.Header.Values("Origin")) != 0 {
			apiresponse.Error(c, 403, "forbidden", "native terminal requires no Origin")
			return
		}
		if !websocket.IsWebSocketUpgrade(c.Request) {
			apiresponse.Error(c, 400, "invalid_argument", "WebSocket upgrade required")
			return
		}
		input, deadline, ok := decodeNativePodTerminal(c)
		if !ok {
			return
		}
		ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
		defer cancel()
		ctx, release, admitted := registry.acquire(ctx)
		if !admitted {
			apiresponse.Error(c, 503, "service_unavailable", "native terminal capacity unavailable")
			return
		}
		defer release()
		upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(request *http.Request) bool { return len(request.Header.Values("Origin")) == 0 }}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, http.Header{"Cache-Control": []string{"no-store"}})
		if err != nil {
			return
		}
		stream := terminalstream.NewServer(ctx, conn, contractresource.NativeTerminalSize{Width: input.Width, Height: input.Height})
		defer func() { _ = stream.Close() }()
		if stream.SendReady() != nil {
			return
		}
		result, err := client.StreamNativePodTerminal(stream.Context(), c.Query("namespace"), c.Param("podName"), input, stream.Input(), stream, stream)
		if err != nil {
			_ = stream.SendError(nativeTerminalErrorCode(err))
			return
		}
		if result.Validate() != nil {
			_ = stream.SendError("transport_lost")
			return
		}
		_ = stream.SendExit(result)
	})
}

func decodeNativePodTerminal(c *gin.Context) (contractresource.PodTerminalInput, time.Time, bool) {
	query := c.Request.URL.Query()
	allowed := map[string]bool{"namespace": true, "expectedUid": true, "container": true, "shell": true, "width": true, "height": true, "timeoutSeconds": true, "deadlineAt": true}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			apiresponse.Error(c, 400, "invalid_argument", "invalid native terminal query")
			return contractresource.PodTerminalInput{}, time.Time{}, false
		}
	}
	width, wErr := strconv.ParseUint(query.Get("width"), 10, 16)
	height, hErr := strconv.ParseUint(query.Get("height"), 10, 16)
	timeout, tErr := strconv.ParseInt(query.Get("timeoutSeconds"), 10, 64)
	input, err := (contractresource.PodTerminalInput{ExpectedUID: query.Get("expectedUid"), Container: query.Get("container"), Shell: query.Get("shell"), Width: uint16(width), Height: uint16(height), TimeoutSeconds: timeout}).Normalized()
	deadline, dErr := time.Parse(time.RFC3339Nano, query.Get("deadlineAt"))
	if query.Get("namespace") == "" || wErr != nil || hErr != nil || tErr != nil || dErr != nil || err != nil || !deadline.After(time.Now()) || deadline.After(time.Now().Add(time.Duration(input.TimeoutSeconds+5)*time.Second)) {
		apiresponse.Error(c, 400, "invalid_argument", "invalid native terminal target, bounds or deadline")
		return contractresource.PodTerminalInput{}, time.Time{}, false
	}
	return input, deadline, true
}

func nativeTerminalErrorCode(err error) string {
	switch {
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return "authorization_revoked"
	case apierrors.IsConflict(err) || apierrors.IsNotFound(err):
		return "target_changed"
	case apierrors.IsBadRequest(err):
		return "invalid_frame"
	default:
		return "transport_lost"
	}
}
