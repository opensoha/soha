package api

import (
	"context"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	"github.com/opensoha/soha/agent/internal/agent/environment"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
	contractenv "github.com/opensoha/soha/contracts/agent/environment"
)

const personalMCPStdioMaxDuration = 15 * time.Minute

func registerPersonalMCPStdio(group *gin.RouterGroup, actions actionPolicy, streams *nativePodTerminals, assets map[cfgpkg.MCPStdioAssetRef][]string, reserve func(context.Context, contractenv.Lease, []string) (environment.PersonalMCPStdioCommand, error)) {
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, CheckOrigin: func(r *http.Request) bool { return len(r.Header.Values("Origin")) == 0 }}
	group.GET("/mcp-stdio", actions.Require(actionPersonalEnvironmentExecute), func(c *gin.Context) {
		input, ok := decodePersonalMCPStdio(c.Request)
		if !ok {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "invalid personal MCP stdio request")
			return
		}
		argv, ok := assets[cfgpkg.MCPStdioAssetRef{ID: input.AssetID, Version: input.Version}]
		if !ok {
			apiresponse.Error(c, http.StatusForbidden, "asset_not_allowed", "personal MCP stdio asset is unavailable")
			return
		}
		deadline := time.Now().Add(personalMCPStdioMaxDuration)
		if input.Lease.ExpiresAt.Before(deadline) {
			deadline = input.Lease.ExpiresAt
		}
		ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
		defer cancel()
		ctx, release, ok := streams.acquire(ctx)
		if !ok {
			apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "personal MCP stdio capacity unavailable")
			return
		}
		defer release()
		stdin, inputPipe, err := os.Pipe()
		if err != nil {
			apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "personal MCP stdio unavailable")
			return
		}
		defer func() { _ = stdin.Close() }()
		defer func() { _ = inputPipe.Close() }()
		command, err := reserve(ctx, input.Lease, argv)
		if err != nil {
			if errors.Is(err, environment.ErrEnvironmentCommandBusy) {
				apiresponse.Error(c, http.StatusConflict, "environment_command_busy", "personal environment command is already active")
			} else if errors.Is(err, environment.ErrLeaseNotFound) {
				apiresponse.Error(c, http.StatusNotFound, "environment_not_found", "personal environment is unavailable")
			} else {
				apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "personal MCP stdio unavailable")
			}
			return
		}
		defer command.Abort()
		conn, err := upgrader.Upgrade(c.Writer, c.Request, http.Header{"Cache-Control": {"no-store"}})
		if err != nil {
			return
		}
		servePersonalMCPStdio(ctx, deadline, conn, inputPipe, func(ctx context.Context, stdout io.Writer) environment.PersonalMCPStdioStatus {
			return command.Run(ctx, stdin, stdout)
		})
	})
}

func decodePersonalMCPStdio(r *http.Request) (contractenv.PersonalMCPStdioRequest, bool) {
	var input contractenv.PersonalMCPStdioRequest
	if len(r.Header.Values("Origin")) != 0 || r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || !websocket.IsWebSocketUpgrade(r) {
		return input, false
	}
	values := r.Header.Values(contractenv.PersonalMCPStdioHeader)
	if len(values) != 1 || len(values[0]) > 4096 {
		return input, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(values[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != values[0] || json.Unmarshal(raw, &input, json.RejectUnknownMembers(true)) != nil {
		return input, false
	}
	return input, input.Validate() == nil && input.Lease.ExpiresAt.After(time.Now())
}

func servePersonalMCPStdio(ctx context.Context, deadline time.Time, conn *websocket.Conn, inputPipe *os.File, run func(context.Context, io.Writer) environment.PersonalMCPStdioStatus) {
	ctx, cancel := context.WithCancel(ctx)
	stopPipe := context.AfterFunc(ctx, func() { _ = inputPipe.Close() })
	defer stopPipe()
	configureWebSocketReadLimit(conn)
	_ = conn.SetReadDeadline(deadline)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer cancel()
		readPersonalMCPStdio(conn, inputPipe, cancel)
	}()
	defer func() {
		cancel()
		_ = inputPipe.Close()
		_ = conn.Close()
		<-readDone
	}()
	stdout := personalMCPStdioOutput{ctx: ctx, conn: conn, cancel: cancel}
	status := run(ctx, stdout)
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	if conn.WriteJSON(struct {
		Status environment.PersonalMCPStdioStatus `json:"status"`
	}{Status: status}) == nil {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	}
}

func readPersonalMCPStdio(conn *websocket.Conn, input *os.File, cancel context.CancelFunc) {
	for {
		kind, frame, err := conn.NextReader()
		if err != nil {
			return
		}
		switch kind {
		case websocket.BinaryMessage:
			if input.SetWriteDeadline(time.Now().Add(30*time.Second)) != nil {
				return
			}
			if _, err := io.Copy(input, frame); err != nil {
				return
			}
		case websocket.TextMessage:
			var request struct {
				Close bool `json:"close"`
			}
			raw, err := io.ReadAll(io.LimitReader(frame, 65))
			if err != nil || len(raw) > 64 || json.Unmarshal(raw, &request, json.RejectUnknownMembers(true)) != nil || !request.Close {
				return
			}
			if input.Close() != nil {
				return
			}
			closeTimer := time.AfterFunc(5*time.Second, cancel)
			defer closeTimer.Stop()
			// Keep observing disconnects until the command has reaped and the
			// server can return its cleanup receipt. More input is a fault.
			_, _, _ = conn.NextReader()
			return
		default:
			return
		}
	}
}

type personalMCPStdioOutput struct {
	ctx    context.Context
	conn   *websocket.Conn
	cancel context.CancelFunc
}

func (output personalMCPStdioOutput) Write(data []byte) (int, error) {
	for sent := 0; sent < len(data); {
		if err := output.ctx.Err(); err != nil {
			return sent, err
		}
		end := min(sent+websocketControlMessageReadLimit, len(data))
		_ = output.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := output.conn.WriteMessage(websocket.BinaryMessage, data[sent:end]); err != nil {
			output.cancel()
			return sent, err
		}
		sent = end
	}
	return len(data), nil
}
