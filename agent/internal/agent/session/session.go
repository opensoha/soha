package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"go.uber.org/zap"
)

const tunnelSubprotocol = "soha-agent-tunnel.v1"

type Manager struct {
	cfg        cfgpkg.ControlPlaneConfig
	logger     *zap.Logger
	runtimeURL *url.URL
	semaphore  chan struct{}
}

func New(cfg cfgpkg.ControlPlaneConfig, logger *zap.Logger) (*Manager, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("session")
	runtimeURL, err := url.Parse(strings.TrimSpace(cfg.RuntimeEndpoint))
	if err != nil || runtimeURL.Host == "" {
		return nil, fmt.Errorf("parse Agent runtime endpoint")
	}
	maxStreams := cfg.Session.MaxStreams
	if maxStreams <= 0 {
		maxStreams = 64
	}
	return &Manager{cfg: cfg, logger: logger, runtimeURL: runtimeURL, semaphore: make(chan struct{}, maxStreams)}, nil
}

func (m *Manager) Start(ctx context.Context) {
	if m == nil || !m.cfg.Session.Enabled {
		return
	}
	go m.run(ctx)
}

func (m *Manager) run(ctx context.Context) {
	backoff := m.cfg.Session.ReconnectMin
	if backoff <= 0 {
		backoff = time.Second
	}
	maxBackoff := m.cfg.Session.ReconnectMax
	if maxBackoff < backoff {
		maxBackoff = 30 * time.Second
	}
	for ctx.Err() == nil {
		if !m.runtimeReady(ctx) {
			return
		}
		err := m.runSession(ctx)
		if ctx.Err() != nil {
			return
		}
		m.logger.Warn("Agent session disconnected; reconnecting",
			zap.String("event", "agent.session.disconnected"),
			zap.String("cluster_id", m.cfg.AgentID),
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.Duration("retry_in", backoff),
		)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (m *Manager) runtimeReady(ctx context.Context) bool {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", m.runtimeURL.Host)
		if err == nil {
			_ = connection.Close()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func (m *Manager) runSession(ctx context.Context) error {
	endpoint, err := sessionURL(m.cfg.BaseURL, m.cfg.AgentID)
	if err != nil {
		return err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(m.cfg.BearerToken))
	dialer := websocket.Dialer{Subprotocols: []string{tunnelSubprotocol}, HandshakeTimeout: m.cfg.Session.HandshakeTimeout}
	conn, response, err := dialer.DialContext(ctx, endpoint, headers)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		if response != nil {
			return fmt.Errorf("connect Agent session: status %d", response.StatusCode)
		}
		return fmt.Errorf("connect Agent session: %w", err)
	}
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(1 << 20)
	tunnel := newWebSocketNetConn(conn)
	config := yamux.DefaultConfig()
	config.KeepAliveInterval = 15 * time.Second
	config.StreamOpenTimeout = 10 * time.Second
	config.LogOutput = io.Discard
	mux, err := yamux.Client(tunnel, config)
	if err != nil {
		return fmt.Errorf("create Agent session multiplexer: %w", err)
	}
	defer func() { _ = mux.Close() }()
	m.logger.Info("Agent session connected",
		zap.String("event", "agent.session.connected"),
		zap.String("cluster_id", m.cfg.AgentID),
	)

	for {
		stream, err := mux.AcceptStream()
		if err != nil {
			return fmt.Errorf("accept Agent session stream: %w", err)
		}
		select {
		case m.semaphore <- struct{}{}:
			go func() {
				defer func() { <-m.semaphore }()
				m.proxyStream(ctx, stream)
			}()
		case <-ctx.Done():
			_ = stream.Close()
			return ctx.Err()
		default:
			_ = stream.Close()
		}
	}
}

func (m *Manager) proxyStream(ctx context.Context, stream net.Conn) {
	defer func() { _ = stream.Close() }()
	local, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", m.runtimeURL.Host)
	if err != nil {
		return
	}
	defer func() { _ = local.Close() }()
	errCh := make(chan error, 2)
	go func() { _, err := io.Copy(local, stream); errCh <- err }()
	go func() { _, err := io.Copy(stream, local); errCh <- err }()
	select {
	case <-ctx.Done():
	case <-errCh:
	}
}

func sessionURL(baseURL, clusterID string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid Soha access URL")
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("invalid Soha access URL scheme")
	}
	basePath := strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), "/api/v1")
	parsed.Path = path.Join(basePath, "api/v1/agent-sessions/connect")
	query := parsed.Query()
	query.Set("clusterId", strings.TrimSpace(clusterID))
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String(), nil
}

type webSocketNetConn struct {
	conn    *websocket.Conn
	readMu  sync.Mutex
	writeMu sync.Mutex
	reader  io.Reader
}

func newWebSocketNetConn(conn *websocket.Conn) net.Conn { return &webSocketNetConn{conn: conn} }

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
		if messageType == websocket.BinaryMessage {
			c.reader = reader
		}
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
	return n, errors.Join(writeErr, writer.Close())
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
