// Package proxytransport supplies the HTTP CONNECT transport shared by network
// consumers. Callers own listener lifecycle, destination policy and SOCKS routing.
package proxytransport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Dial receives the authenticated transport name and original destination. A
// consumer must resolve, authorize and pin an IP before dialing its backend.
type Dial func(context.Context, string, string) (net.Conn, error)

type Handler struct {
	mu          sync.Mutex
	credentials map[string][32]byte
	dial        Dial
	deadline    time.Time
	closed      bool
	connections map[net.Conn]struct{}
}

func New(credentials map[string]string, dial Dial, deadline time.Time) (*Handler, error) {
	if len(credentials) == 0 || len(credentials) > 2048 || dial == nil || !deadline.After(time.Now()) || deadline.After(time.Now().Add(5*time.Minute)) {
		return nil, errors.New("invalid proxy transport dependencies or lease")
	}
	h := &Handler{credentials: map[string][32]byte{}, dial: dial, deadline: deadline.UTC(), connections: map[net.Conn]struct{}{}}
	for user, password := range credentials {
		if user == "" || len(user) > 128 || len(password) < 32 {
			return nil, errors.New("invalid proxy transport credential")
		}
		h.credentials[user] = sha256.Sum256([]byte(password))
	}
	return h, nil
}

func (h *Handler) Refresh(deadline time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || !deadline.After(time.Now()) || deadline.After(time.Now().Add(5*time.Minute)) {
		return errors.New("proxy transport lease is inactive")
	}
	deadline = deadline.UTC()
	h.deadline = deadline
	for conn := range h.connections {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	var result error
	for conn := range h.connections {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authenticate(r)
	if !ok {
		w.Header().Set("Proxy-Authenticate", `Basic realm="Soha managed transport"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		h.connect(w, r, user)
		return
	}
	if r.URL.Scheme != "http" || r.URL.Host == "" || r.Host != r.URL.Host || r.URL.User != nil || r.Header.Get("Upgrade") != "" {
		http.Error(w, "unsupported proxy request", http.StatusBadRequest)
		return
	}
	h.forward(w, r, user)
}

func (h *Handler) authenticate(r *http.Request) (string, bool) {
	values := r.Header.Values("Proxy-Authorization")
	if len(values) != 1 {
		return "", false
	}
	copy := r.Clone(r.Context())
	copy.Header = make(http.Header)
	copy.Header.Set("Authorization", values[0])
	user, password, ok := copy.BasicAuth()
	if !ok {
		return "", false
	}
	expected, exists := h.credentials[user]
	actual := sha256.Sum256([]byte(password))
	return user, exists && subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}

func (h *Handler) open(ctx context.Context, user, address string) (net.Conn, error) {
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil || host == "" || len(host) > 253 {
		return nil, errors.New("invalid proxy target")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("invalid proxy target port")
	}
	h.mu.Lock()
	until, closed := h.deadline, h.closed
	h.mu.Unlock()
	if closed || !until.After(time.Now()) {
		return nil, errors.New("proxy transport lease expired")
	}
	ctx, cancel := context.WithDeadline(ctx, until)
	defer cancel()
	conn, err := h.dial(ctx, user, net.JoinHostPort(host, rawPort))
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(until); err != nil {
		_ = conn.Close()
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || !h.deadline.After(time.Now()) || len(h.connections) >= 4096 {
		_ = conn.Close()
		return nil, errors.New("proxy transport was closed or reached its limit")
	}
	if err := conn.SetDeadline(h.deadline); err != nil {
		_ = conn.Close()
		return nil, err
	}
	h.connections[conn] = struct{}{}
	return &trackedConn{Conn: conn, handler: h}, nil
}

func (h *Handler) Ready() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.closed && h.deadline.After(time.Now())
}

type trackedConn struct {
	net.Conn
	handler *Handler
}

func (c *trackedConn) Close() error {
	c.handler.mu.Lock()
	delete(c.handler.connections, c.Conn)
	c.handler.mu.Unlock()
	return c.Conn.Close()
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request, user string) {
	if r.URL.Host != "" && r.URL.Host != r.Host || r.URL.User != nil {
		http.Error(w, "ambiguous proxy target", http.StatusBadRequest)
		return
	}
	upstream, err := h.open(r.Context(), user, r.Host)
	if err != nil {
		http.Error(w, "proxy target unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstream.Close() }()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "CONNECT unavailable", http.StatusInternalServerError)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer func() { _ = client.Close() }()
	h.mu.Lock()
	closed := h.closed || !h.deadline.After(time.Now()) || len(h.connections) >= 4096
	if !closed {
		if err := client.SetDeadline(h.deadline); err != nil {
			closed = true
		} else {
			h.connections[client] = struct{}{}
		}
	}
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.connections, client); h.mu.Unlock() }()
	if closed {
		return
	}

	if _, err := buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if buffer.Flush() != nil {
		return
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = io.Copy(upstream, buffer); _ = upstream.Close() }()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	_ = upstream.Close()
	<-done
}

func (h *Handler) forward(w http.ResponseWriter, r *http.Request, user string) {
	h.mu.Lock()
	until, closed := h.deadline, h.closed
	h.mu.Unlock()
	if closed || !until.After(time.Now()) {
		http.Error(w, "proxy lease expired", http.StatusServiceUnavailable)
		return
	}
	control := http.NewResponseController(w)
	if control.SetReadDeadline(until) != nil || control.SetWriteDeadline(until) != nil {
		http.Error(w, "bounded proxy transport unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), until)
	defer cancel()
	request := r.Clone(ctx)
	request.RequestURI = ""
	request.Header = request.Header.Clone()
	removeHopHeaders(request.Header)
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			return nil, errors.New("unsupported proxy transport")
		}
		return h.open(ctx, user, address)
	}, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	response, err := transport.RoundTrip(request)
	if err != nil {
		http.Error(w, "proxy target unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = response.Body.Close() }()
	removeHopHeaders(response.Header)
	for name, values := range response.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func removeHopHeaders(header http.Header) {
	for _, line := range header.Values("Connection") {
		for _, name := range strings.Split(line, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}
