package proxytransport

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type packetAssociation struct{ front, back net.Conn }

func (a *packetAssociation) close() { _ = a.front.Close(); _ = a.back.Close() }
func refreshPacketConn(c net.Conn, until time.Time) error {
	if r, ok := c.(interface{ Refresh(time.Time) error }); ok {
		return r.Refresh(until)
	}
	return c.SetDeadline(until)
}
func packetCredentials(credentials map[string]string, maxUser int) (map[string][32]byte, error) {
	if len(credentials) == 0 || len(credentials) > 2048 {
		return nil, errors.New("invalid packet credentials")
	}
	out := make(map[string][32]byte, len(credentials))
	for user, password := range credentials {
		if user == "" || len(user) > maxUser || len(password) < 32 {
			return nil, errors.New("invalid packet credential")
		}
		out[user] = sha256.Sum256([]byte(password))
	}
	return out, nil
}
func authenticatePacket(credentials map[string][32]byte, user, password string) bool {
	expected, exists := credentials[user]
	actual := sha256.Sum256([]byte(password))
	return exists && subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}
func singleHeader(h http.Header, name string) (string, bool) {
	var values []string
	for k, v := range h {
		if strings.EqualFold(k, name) {
			values = append(values, v...)
		}
	}
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}
func packetTarget(target string) error {
	host, port, err := net.SplitHostPort(target)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || n < 1 || n > 65535 || host == "" || len(host) > 253 || strings.ContainsAny(host, "/@?#\\\r\n\t %") {
		return errors.New("invalid packet target")
	}
	return nil
}
func authorizePacket(authorize PacketAuthorize, user, target string, until time.Time) error {
	if !until.After(time.Now()) {
		return net.ErrClosed
	}
	ctx, cancel := context.WithDeadline(context.Background(), earlier(until, time.Now().Add(3*time.Second)))
	defer cancel()
	return authorize(ctx, user, target)
}

// PacketHandler is an explicitly selected HTTP/1 TLS datagram handler. Dispatch
// only requests carrying Soha-Transport to it; header-free requests retain the
// existing TCP Handler behavior. NoNew rejects new associations, not active ones.
type PacketHandler struct {
	mu            sync.Mutex
	credentials   map[string][32]byte
	dial          DialPacket
	authorize     PacketAuthorize
	until         time.Time
	closed, noNew bool
	pending       int
	associations  map[*packetAssociation]struct{}
}

func NewPacketHandler(credentials map[string]string, dial DialPacket, authorize PacketAuthorize, deadline time.Time) (*PacketHandler, error) {
	c, err := packetCredentials(credentials, MaxUsernameBytes)
	if err != nil {
		return nil, err
	}
	if dial == nil || authorize == nil || !validPacketLease(deadline) {
		return nil, errors.New("invalid packet handler dependencies or lease")
	}
	return &PacketHandler{credentials: c, dial: dial, authorize: authorize, until: deadline, associations: map[*packetAssociation]struct{}{}}, nil
}
func (h *PacketHandler) NoNew() { h.mu.Lock(); h.noNew = true; h.mu.Unlock() }

// ActiveCount counts admitted datagram associations, not HTTP or SOCKS controls.
func (h *PacketHandler) ActiveCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.associations)
}
func (h *PacketHandler) Refresh(until time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || !h.until.After(time.Now()) {
		return net.ErrClosed
	}
	if !validPacketLease(until) {
		return errors.New("invalid packet renewal")
	}
	h.until = until
	for a := range h.associations {
		for _, c := range []net.Conn{a.front, a.back} {
			if err := refreshPacketConn(c, until); err != nil {
				if errors.Is(err, net.ErrClosed) {
					a.close()
					delete(h.associations, a)
					break
				}
				return err
			}
		}
	}
	return nil
}
func (h *PacketHandler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.noNew = true
	for a := range h.associations {
		a.close()
		delete(h.associations, a)
	}
	return nil
}
func (h *PacketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	transport, ok := singleHeader(r.Header, PacketTransportHeader)
	if r.TLS == nil || r.ProtoMajor != 1 || r.Method != http.MethodConnect || !ok || transport != PacketTransportVersion || r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.URL.User != nil || r.URL.RawQuery != "" || r.URL.Path != "" || r.URL.Host != "" && r.URL.Host != r.Host || packetTarget(r.Host) != nil {
		http.Error(w, "invalid datagram tunnel", http.StatusBadRequest)
		return
	}
	auth, ok := singleHeader(r.Header, "Proxy-Authorization")
	request := r.Clone(r.Context())
	request.Header = make(http.Header)
	request.Header.Set("Authorization", auth)
	user, secret, basicOK := request.BasicAuth()
	if !ok || !basicOK || !authenticatePacket(h.credentials, user, secret) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="Soha managed transport"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	h.mu.Lock()
	until := h.until
	available := !h.closed && !h.noNew && until.After(time.Now()) && len(h.associations)+h.pending < MaxPacketAssociations
	if available {
		h.pending++
	}
	h.mu.Unlock()
	if !available {
		http.Error(w, "datagram transport unavailable", http.StatusServiceUnavailable)
		return
	}
	reserved := true
	defer func() {
		if reserved {
			h.mu.Lock()
			h.pending--
			h.mu.Unlock()
		}
	}()
	if err := authorizePacket(h.authorize, user, r.Host, until); err != nil {
		http.Error(w, "datagram authorization denied", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), earlier(until, time.Now().Add(5*time.Second)))
	back, err := h.dial(ctx, user, r.Host)
	cancel()
	if err != nil || back == nil {
		if errors.Is(err, ErrPacketUnsupported) {
			w.Header().Set(PacketTransportHeader, PacketTransportVersion)
			http.Error(w, "guarded UDP transport unsupported", http.StatusNotImplemented)
		} else {
			http.Error(w, "datagram backend unavailable", http.StatusBadGateway)
		}
		return
	}
	defer back.Close()
	if err := refreshPacketConn(back, until); err != nil {
		http.Error(w, "datagram backend unavailable", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "datagram hijack unavailable", http.StatusInternalServerError)
		return
	}
	raw, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer raw.Close()
	front, err := NewPacketConn(&bufferedPacketStream{Conn: raw, reader: buffer.Reader}, until)
	if err != nil {
		return
	}
	defer front.Close()
	association := &packetAssociation{front: front, back: back}
	h.mu.Lock()
	if h.closed || h.noNew || !h.until.After(time.Now()) {
		h.mu.Unlock()
		return
	}
	h.pending--
	reserved = false
	h.associations[association] = struct{}{}
	h.mu.Unlock()
	defer func() { association.close(); h.mu.Lock(); delete(h.associations, association); h.mu.Unlock() }()
	if _, err := buffer.WriteString("HTTP/1.1 200 Connection Established\r\nSoha-Transport: udp/1\r\n\r\n"); err != nil {
		return
	}
	if buffer.Flush() != nil {
		return
	}
	done := make(chan struct{}, 2)
	copyPackets := func(dst, src net.Conn) {
		defer func() { done <- struct{}{} }()
		b := make([]byte, MaxDatagramBytes)
		for {
			n, err := src.Read(b)
			if err != nil {
				return
			}
			h.mu.Lock()
			lease := h.until
			closed := h.closed
			h.mu.Unlock()
			if closed || authorizePacket(h.authorize, user, r.Host, lease) != nil {
				return
			}
			if _, err := dst.Write(b[:n]); err != nil {
				return
			}
		}
	}
	go copyPackets(back, front)
	go copyPackets(front, back)
	<-done
	association.close()
	<-done
}

type bufferedPacketStream struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedPacketStream) Read(p []byte) (int, error) { return c.reader.Read(p) }

type packetHeaderReader struct {
	reader    io.Reader
	remaining int
	tail      uint32
	done      bool
}

func (r *packetHeaderReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.done {
		return r.reader.Read(p)
	}
	if r.remaining <= 0 {
		return 0, errors.New("packet tunnel header exceeds limit")
	}
	n, err := r.reader.Read(p[:1])
	if n > 0 {
		r.remaining--
		r.tail = r.tail<<8 | uint32(p[0])
		r.done = r.tail == 0x0d0a0d0a
	}
	return n, err
}

// DialPacketTunnel consumes an already verified TLS connection to the approved
// internal endpoint, using the existing mapped HTTP Basic username. It does not
// follow redirects, select endpoints or fall back to TCP or OS UDP.
func DialPacketTunnel(ctx context.Context, rawTLSConn net.Conn, target, user, secret string, deadline time.Time) (*PacketConn, error) {
	fail := func(err error) (*PacketConn, error) {
		if rawTLSConn != nil {
			_ = rawTLSConn.Close()
		}
		return nil, err
	}
	if rawTLSConn == nil || packetTarget(target) != nil || user == "" || len(user) > MaxUsernameBytes || strings.Contains(user, ":") || len(secret) < 32 || !validPacketLease(deadline) {
		return fail(errors.New("invalid packet tunnel dependencies"))
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = rawTLSConn.Close() })
	defer stopCancel()
	secured, ok := rawTLSConn.(*tls.Conn)
	if !ok {
		return fail(errors.New("packet tunnel requires TLS"))
	}
	handshake := earlier(deadline, time.Now().Add(5*time.Second))
	if d, ok := ctx.Deadline(); ok {
		handshake = earlier(handshake, d)
	}
	if err := rawTLSConn.SetDeadline(handshake); err != nil {
		return fail(err)
	}
	handshakeCtx, cancel := context.WithDeadline(ctx, handshake)
	err := secured.HandshakeContext(handshakeCtx)
	cancel()
	if err != nil {
		return fail(errors.New("packet TLS handshake failed"))
	}
	state := secured.ConnectionState()
	if len(state.VerifiedChains) == 0 {
		return fail(errors.New("packet TLS identity unverified"))
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	req.Header.Set(PacketTransportHeader, PacketTransportVersion)
	req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+secret)))
	if err := req.Write(rawTLSConn); err != nil {
		return fail(errors.New("packet tunnel request failed"))
	}
	reader := bufio.NewReader(&packetHeaderReader{reader: rawTLSConn, remaining: 16 << 10})
	response, err := http.ReadResponse(reader, req)
	if err != nil {
		return fail(errors.New("packet tunnel response failed"))
	}
	version, ok := singleHeader(response.Header, PacketTransportHeader)
	if response.ProtoMajor != 1 || !ok || version != PacketTransportVersion || len(response.TransferEncoding) != 0 {
		return fail(errors.New("unsafe packet tunnel response"))
	}
	if response.StatusCode == http.StatusNotImplemented {
		return fail(ErrPacketUnsupported)
	}
	if response.StatusCode != http.StatusOK {
		return fail(errors.New("packet tunnel refused"))
	}
	packet, err := NewPacketConn(&bufferedPacketStream{Conn: rawTLSConn, reader: reader}, deadline)
	if err != nil {
		return fail(err)
	}
	return packet, nil
}
