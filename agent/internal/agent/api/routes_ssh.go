package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	runnerpkg "github.com/opensoha/soha-agent/internal/agent/runner"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type sshStagedSession struct {
	input    sohaapi.AgentSSHSessionRequest
	deadline time.Time
}
type sshSessions struct {
	mu      sync.Mutex
	pending map[string]sshStagedSession
	active  map[string]context.CancelFunc
	closed  bool
	limit   int
}

func registerSSHRoutes(router *gin.Engine, cfg cfgpkg.Config, actions actionPolicy) *sshSessions {
	limit := cfg.ControlPlane.MaxConcurrency
	if limit < 1 {
		limit = 1
	}
	if limit > 32 {
		limit = 32
	}
	sessions := &sshSessions{pending: map[string]sshStagedSession{}, active: map[string]context.CancelFunc{}, limit: limit}
	routes := router.Group(cfg.HTTP.BasePath+"/ssh", authAnyMiddleware(cfg.Auth.BearerToken, cfg.ControlPlane.BearerToken))
	routes.GET("/capability", func(c *gin.Context) {
		available := actions.allows(actionVMSSHConnect) && cfg.ControlPlane.AgentID != "" && cfg.ControlPlane.BaseURL != "" && cfg.ControlPlane.BearerToken != ""
		apiresponse.Item(c, http.StatusOK, sohaapi.AgentSSHCapability{AgentID: cfg.ControlPlane.AgentID, ProtocolVersion: 1, Available: available, CommandsAvailable: available && actions.allows(actionVMSSHExecute) && cfg.ControlPlane.Enabled && cfg.Kubernetes.ID != ""})
	})
	routes.POST("/sessions", actions.Require(actionVMSSHConnect), func(c *gin.Context) {
		var input sohaapi.AgentSSHSessionRequest
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || runnerpkg.ValidateSSHRequest(cfg.ControlPlane, input) != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid SSH session")
			return
		}
		if !sessions.stage(input) {
			apiresponse.Error(c, http.StatusConflict, "conflict", "SSH session unavailable")
			return
		}
		apiresponse.Item(c, http.StatusOK, map[string]string{"sessionId": input.SessionID})
	})
	origins := newWebSocketOriginPolicy(cfg.HTTP.AllowedOrigins, cfg.Auth.BearerToken, cfg.ControlPlane.BearerToken)
	routes.GET("/sessions/:id/stream", actions.Require(actionVMSSHConnect), func(c *gin.Context) {
		if !origins.Check(c.Request) {
			apiresponse.Error(c, http.StatusForbidden, "access_denied", "WebSocket origin denied")
			return
		}
		input, ctx, release, ok := sessions.claim(c.Request.Context(), c.Param("id"))
		if !ok {
			apiresponse.Error(c, http.StatusConflict, "conflict", "SSH session unavailable")
			return
		}
		defer release()
		pty, err := runnerpkg.OpenSSHPTY(ctx, cfg.ControlPlane, input)
		if err != nil {
			apiresponse.Error(c, http.StatusBadGateway, "unavailable", "SSH session failed to open")
			return
		}
		defer func() { _ = pty.Close() }()
		upgrader := websocket.Upgrader{CheckOrigin: origins.Check}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		forwardSSHPTY(ctx, conn, pty)
	})
	return sessions
}

func (sessions *sshSessions) stage(input sohaapi.AgentSSHSessionRequest) bool {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	now := time.Now()
	for id, pending := range sessions.pending {
		if !pending.deadline.After(now) {
			delete(sessions.pending, id)
		}
	}
	if sessions.closed || len(sessions.pending)+len(sessions.active) >= sessions.limit {
		return false
	}
	if _, ok := sessions.pending[input.SessionID]; ok {
		return false
	}
	if _, ok := sessions.active[input.SessionID]; ok {
		return false
	}
	sessions.pending[input.SessionID] = sshStagedSession{input: input, deadline: now.Add(30 * time.Second)}
	return true
}

func (sessions *sshSessions) claim(parent context.Context, id string) (sohaapi.AgentSSHSessionRequest, context.Context, func(), bool) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	pending, ok := sessions.pending[id]
	delete(sessions.pending, id)
	if !ok || sessions.closed || !pending.deadline.After(time.Now()) {
		return sohaapi.AgentSSHSessionRequest{}, nil, nil, false
	}
	ctx, cancel := context.WithDeadline(parent, pending.input.ExpiresAt)
	sessions.active[id] = cancel
	release := func() { cancel(); sessions.mu.Lock(); delete(sessions.active, id); sessions.mu.Unlock() }
	return pending.input, ctx, release, true
}

func (sessions *sshSessions) Close() {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.closed = true
	clear(sessions.pending)
	for _, cancel := range sessions.active {
		cancel()
	}
}

func forwardSSHPTY(ctx context.Context, connection *websocket.Conn, pty *runnerpkg.SSHPTY) {
	connection.SetReadLimit(128 * 1024)
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Minute))
	inputDone := make(chan struct{})
	outputDone := make(chan struct{})
	waitDone := make(chan error, 1)
	var writeMu sync.Mutex
	write := func(frame sohaapi.VirtualMachineSSHFrame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return connection.WriteJSON(frame)
	}
	go func() { defer close(inputDone); readSSHInput(connection, pty) }()
	go func() {
		defer close(outputDone)
		buffer := make([]byte, 16384)
		for {
			count, err := pty.Output.Read(buffer)
			if count > 0 {
				if write(sohaapi.VirtualMachineSSHFrame{Type: "output", Data: base64.StdEncoding.EncodeToString(buffer[:count]), DataEncoding: "base64"}) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { waitDone <- pty.Wait() }()
	terminal := false
	var waitErr error
	select {
	case <-inputDone:
	case <-outputDone:
		select {
		case waitErr = <-waitDone:
			terminal = true
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	case waitErr = <-waitDone:
		terminal = true
	case <-ctx.Done():
	}
	_ = pty.Close()
	select {
	case <-outputDone:
	case <-time.After(10 * time.Second):
	}
	if terminal {
		exitCode := runnerpkg.SSHExitCode(waitErr)
		_ = write(sohaapi.VirtualMachineSSHFrame{Type: "close", ExitCode: exitCode, Message: "SSH shell exited"})
	}
	_ = connection.Close()
	select {
	case <-inputDone:
	case <-time.After(time.Second):
	}
}

func readSSHInput(connection *websocket.Conn, pty *runnerpkg.SSHPTY) {
	for {
		var frame sohaapi.VirtualMachineSSHFrame
		if connection.ReadJSON(&frame) != nil {
			return
		}
		switch frame.Type {
		case "input":
			if len(frame.Data) > 87384 || frame.Message != "" || frame.ExitCode != nil || frame.Cols != 0 || frame.Rows != 0 {
				return
			}
			data := []byte(frame.Data)
			if frame.DataEncoding == "base64" {
				var err error
				data, err = base64.StdEncoding.DecodeString(frame.Data)
				if err != nil {
					return
				}
			} else if frame.DataEncoding != "" && frame.DataEncoding != "utf8" {
				return
			}
			if len(data) > 65536 {
				return
			}
			if _, err := pty.Input.Write(data); err != nil {
				return
			}
		case "resize":
			if pty.Resize(frame.Cols, frame.Rows) != nil {
				return
			}
		case "close":
			return
		default:
			return
		}
		_ = connection.SetReadDeadline(time.Now().Add(5 * time.Minute))
	}
}
