package handlers

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appcluster "github.com/opensoha/soha/internal/application/cluster"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

const agentTunnelSubprotocol = "soha-agent-tunnel.v1"

type AgentConnectionService interface {
	CreateAgentInstallation(context.Context, domainidentity.Principal, string) (domaincluster.AgentInstallation, error)
	RenderAgentInstallation(context.Context, string) ([]byte, error)
	AuthenticateAgentSession(context.Context, string, string) error
	RefreshAgentSession(context.Context, string) error
}

type AgentSessionAccepter interface {
	Attach(context.Context, string, net.Conn) error
	Connected(string) bool
}

type AgentConnectionHandler struct {
	service  AgentConnectionService
	sessions AgentSessionAccepter
}

func NewAgentConnectionHandler(service AgentConnectionService, sessions AgentSessionAccepter) *AgentConnectionHandler {
	return &AgentConnectionHandler{service: service, sessions: sessions}
}

func (h *AgentConnectionHandler) CreateInstallation(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	installation, err := h.service.CreateAgentInstallation(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, installation)
}

func (h *AgentConnectionHandler) DownloadManifest(c *gin.Context) {
	manifest, err := h.service.RenderAgentInstallation(c.Request.Context(), c.Param("installTicket"))
	if err != nil {
		if errors.Is(err, appcluster.ErrAgentInstallationExpired) {
			apiresponse.Error(c, http.StatusGone, "installation_expired", "Agent installation URL has expired")
			return
		}
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `inline; filename="soha-agent.yaml"`)
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", manifest)
}

func (h *AgentConnectionHandler) Connect(c *gin.Context) {
	clusterID := strings.TrimSpace(c.Query("clusterId"))
	token, ok := bearerToken(c.GetHeader("Authorization"))
	if clusterID == "" || !ok {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "Agent session credentials are required")
		return
	}
	if !containsString(websocket.Subprotocols(c.Request), agentTunnelSubprotocol) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Agent tunnel subprotocol is required")
		return
	}
	if err := h.service.AuthenticateAgentSession(c.Request.Context(), clusterID, token); err != nil {
		writeError(c, err)
		return
	}
	upgrader := websocket.Upgrader{
		Subprotocols: []string{agentTunnelSubprotocol},
		CheckOrigin:  allowWebSocketOrigin,
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	configureWebSocketReadLimit(conn)
	tunnel := newWebSocketNetConn(conn)

	go h.refreshWhenConnected(clusterID)
	if err := h.sessions.Attach(c.Request.Context(), clusterID, tunnel); err != nil && !errors.Is(err, context.Canceled) {
		_ = c.Error(err)
	}
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 10*time.Second)
	defer cancel()
	_ = h.service.RefreshAgentSession(refreshCtx, clusterID)
}

func (h *AgentConnectionHandler) refreshWhenConnected(clusterID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if h.sessions.Connected(clusterID) {
			_ = h.service.RefreshAgentSession(ctx, clusterID)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	returnValue := ""
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		returnValue = strings.TrimSpace(parts[1])
	}
	return returnValue, returnValue != ""
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == expected {
			return true
		}
	}
	return false
}

type webSocketNetConn struct {
	conn    *websocket.Conn
	readMu  sync.Mutex
	writeMu sync.Mutex
	reader  io.Reader
}

func newWebSocketNetConn(conn *websocket.Conn) net.Conn {
	return &webSocketNetConn{conn: conn}
}

func (c *webSocketNetConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		if c.reader != nil {
			n, err := c.reader.Read(p)
			if !errors.Is(err, io.EOF) {
				return n, err
			}
			c.reader = nil
			if n > 0 {
				return n, nil
			}
		}
		messageType, reader, err := c.conn.NextReader()
		if err != nil {
			return 0, err
		}
		if messageType != websocket.BinaryMessage {
			continue
		}
		c.reader = reader
	}
}

func (c *webSocketNetConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	writer, err := c.conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	n, writeErr := writer.Write(p)
	closeErr := writer.Close()
	return n, errors.Join(writeErr, closeErr)
}

func (c *webSocketNetConn) Close() error         { return c.conn.Close() }
func (c *webSocketNetConn) LocalAddr() net.Addr  { return c.conn.NetConn().LocalAddr() }
func (c *webSocketNetConn) RemoteAddr() net.Addr { return c.conn.NetConn().RemoteAddr() }
func (c *webSocketNetConn) SetDeadline(deadline time.Time) error {
	return errors.Join(c.SetReadDeadline(deadline), c.SetWriteDeadline(deadline))
}
func (c *webSocketNetConn) SetReadDeadline(deadline time.Time) error {
	return c.conn.SetReadDeadline(deadline)
}
func (c *webSocketNetConn) SetWriteDeadline(deadline time.Time) error {
	return c.conn.SetWriteDeadline(deadline)
}
