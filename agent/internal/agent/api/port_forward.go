package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

type activePortForward interface {
	LocalPort() int
	Stop()
}

type startPortForwardFunc func(context.Context, string, string, string, int) (activePortForward, error)

type portForwardRegistry struct {
	clusterID string
	starter   startPortForwardFunc
	mu        sync.Mutex
	sessions  map[string]*portForwardSession
}

type portForwardSession struct {
	view       domainresource.PortForwardSessionView
	localPort  int
	forwarding activePortForward
}

func newPortForwardRegistry(cfg cfgpkg.KubernetesConfig, starter startPortForwardFunc) *portForwardRegistry {
	clusterID := strings.TrimSpace(cfg.ID)
	if clusterID == "" {
		clusterID = "local-agent"
	}
	return &portForwardRegistry{
		clusterID: clusterID,
		starter:   starter,
		sessions:  map[string]*portForwardSession{},
	}
}

func (r *portForwardRegistry) list() []domainresource.PortForwardSessionView {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domainresource.PortForwardSessionView, 0, len(r.sessions))
	for _, item := range r.sessions {
		items = append(items, item.view)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items
}

func (r *portForwardRegistry) register(ctx context.Context, input domainresource.PortForwardRegisterInput, createdBy string) (domainresource.PortForwardSessionView, error) {
	namespace := strings.TrimSpace(input.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	kind := strings.TrimSpace(input.TargetKind)
	if kind == "" {
		kind = "Pod"
	}
	targetName := strings.TrimSpace(input.TargetName)
	if targetName == "" {
		return domainresource.PortForwardSessionView{}, errInvalidPortForward("targetName is required")
	}
	if input.LocalPort <= 0 || input.RemotePort <= 0 {
		return domainresource.PortForwardSessionView{}, errInvalidPortForward("localPort and remotePort must be positive")
	}
	if r.starter == nil {
		return domainresource.PortForwardSessionView{}, errInvalidPortForward("port-forward runtime is unavailable")
	}
	forwarding, err := r.starter(ctx, namespace, kind, targetName, input.RemotePort)
	if err != nil {
		return domainresource.PortForwardSessionView{}, err
	}
	localPort := forwarding.LocalPort()
	if localPort <= 0 {
		forwarding.Stop()
		return domainresource.PortForwardSessionView{}, fmt.Errorf("port-forward runtime did not expose a tunnel port")
	}
	item := domainresource.PortForwardSessionView{
		SessionID:  uuid.NewString(),
		ClusterID:  r.clusterID,
		Namespace:  namespace,
		TargetKind: kind,
		TargetName: targetName,
		LocalPort:  input.LocalPort,
		RemotePort: input.RemotePort,
		Status:     "active",
		CreatedBy:  createdBy,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[item.SessionID] = &portForwardSession{
		view:       item,
		localPort:  localPort,
		forwarding: forwarding,
	}
	return item, nil
}

func (r *portForwardRegistry) delete(sessionID string) bool {
	r.mu.Lock()
	session, ok := r.sessions[sessionID]
	if !ok {
		r.mu.Unlock()
		return false
	}
	delete(r.sessions, sessionID)
	r.mu.Unlock()
	session.forwarding.Stop()
	return true
}

func (r *portForwardRegistry) get(sessionID string) (*portForwardSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[sessionID]
	return session, ok
}

type errInvalidPortForward string

func (e errInvalidPortForward) Error() string {
	return string(e)
}

func registerPortForwardRoutes(
	platform *gin.RouterGroup,
	registry *portForwardRegistry,
	actions actionPolicy,
	origins websocketOriginPolicy,
) {
	upgrader := websocket.Upgrader{CheckOrigin: origins.Check}
	platform.GET("/network/port-forwards", func(c *gin.Context) {
		apiresponse.Items(c, http.StatusOK, registry.list())
	})
	platform.POST("/network/port-forwards", actions.Require(actionPlatformPortForwardsCreate), func(c *gin.Context) {
		var req domainresource.PortForwardRegisterInput
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid port-forward payload")
			return
		}
		item, err := registry.register(c.Request.Context(), req, c.ClientIP())
		if err != nil {
			if _, ok := err.(errInvalidPortForward); ok {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
				return
			}
			apiresponse.Error(c, http.StatusServiceUnavailable, "cluster_unready", "cluster is not ready for port-forward")
			return
		}
		apiresponse.Item(c, http.StatusCreated, item)
	})
	platform.GET("/network/port-forwards/:sessionID/tunnel", actions.Require(actionPlatformPortForwardsTunnel), func(c *gin.Context) {
		session, ok := registry.get(c.Param("sessionID"))
		if !ok {
			apiresponse.Error(c, http.StatusNotFound, "not_found", "port-forward session not found")
			return
		}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		configureWebSocketReadLimit(conn)
		targetConn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(session.localPort)), 5*time.Second)
		if err != nil {
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "port-forward tunnel is unavailable"))
			return
		}
		defer targetConn.Close()
		_ = bridgePortForwardTunnel(c.Request.Context(), conn, targetConn)
	})
	platform.DELETE("/network/port-forwards/:sessionID", actions.Require(actionPlatformPortForwardsDelete), func(c *gin.Context) {
		if !registry.delete(c.Param("sessionID")) {
			apiresponse.Error(c, http.StatusNotFound, "not_found", "port-forward session not found")
			return
		}
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})
}

func kubernetesPortForwardStarter(client *k8sagent.Client) startPortForwardFunc {
	return func(ctx context.Context, namespace, kind, name string, remotePort int) (activePortForward, error) {
		return client.StartPortForward(ctx, namespace, kind, name, remotePort)
	}
}

func bridgePortForwardTunnel(ctx context.Context, ws *websocket.Conn, tcp net.Conn) error {
	errCh := make(chan error, 2)
	go func() {
		errCh <- copyPortForwardWebSocketToTCP(ctx, ws, tcp)
	}()
	go func() {
		errCh <- copyPortForwardTCPToWebSocket(ctx, ws, tcp)
	}()

	select {
	case err := <-errCh:
		_ = tcp.Close()
		_ = ws.Close()
		if portForwardTunnelClosed(err) {
			return nil
		}
		return err
	case <-ctx.Done():
		_ = tcp.Close()
		_ = ws.Close()
		return ctx.Err()
	}
}

func copyPortForwardWebSocketToTCP(ctx context.Context, ws *websocket.Conn, tcp net.Conn) error {
	for {
		messageType, payload, err := ws.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if messageType != websocket.BinaryMessage && messageType != websocket.TextMessage {
			continue
		}
		if len(payload) == 0 {
			continue
		}
		if _, err := tcp.Write(payload); err != nil {
			return err
		}
	}
}

func copyPortForwardTCPToWebSocket(ctx context.Context, ws *websocket.Conn, tcp net.Conn) error {
	buffer := make([]byte, 32*1024)
	for {
		n, err := tcp.Read(buffer)
		if n > 0 {
			if writeErr := ws.WriteMessage(websocket.BinaryMessage, buffer[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
}

func portForwardTunnelClosed(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	return websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
		errors.Is(err, websocket.ErrCloseSent)
}
