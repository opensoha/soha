package proxytransport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const packetTestSecret = "0123456789abcdef0123456789abcdef"

func closePacketFixture(t *testing.T, closer io.Closer) {
	t.Helper()
	if err := closer.Close(); !packetFixtureAlreadyClosed(err) {
		t.Errorf("fixture close failed: %v", err)
	}
}

func packetFixtureAlreadyClosed(err error) bool {
	if err == nil {
		return true
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		children := wrapped.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !packetFixtureAlreadyClosed(child) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return packetFixtureAlreadyClosed(wrapped.Unwrap())
	default:
		return errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe)
	}
}

func TestPacketFixtureCleanupKeepsUnexpectedErrors(t *testing.T) {
	unexpected := errors.New("owned body cleanup failed")
	for _, tc := range []struct {
		err    error
		closed bool
	}{
		{nil, true}, {net.ErrClosed, true}, {fmt.Errorf("closed transport: %w", net.ErrClosed), true},
		{errors.Join(net.ErrClosed, io.ErrClosedPipe), true}, {unexpected, false},
		{errors.Join(net.ErrClosed, unexpected), false},
		{fmt.Errorf("wrapped cleanup: %w", errors.Join(net.ErrClosed, unexpected)), false},
	} {
		if got := packetFixtureAlreadyClosed(tc.err); got != tc.closed {
			t.Fatalf("cleanup error=%v, closed=%v, want=%v", tc.err, got, tc.closed)
		}
	}
}

func packetFixtureAs[T any](t *testing.T, value any) T {
	t.Helper()
	typed, ok := value.(T)
	if !ok {
		t.Fatalf("unexpected fixture type %T", value)
	}
	return typed
}

func packetFixturePort(t *testing.T, port int) []byte {
	t.Helper()
	if port < 0 || port > 65535 {
		t.Fatalf("invalid fixture port %d", port)
		return nil
	}
	return binary.BigEndian.AppendUint16(nil, uint16(port))
}

func packetTestLease() time.Time { return time.Now().Add(time.Minute) }
func packetEchoPair(t *testing.T) net.Conn {
	t.Helper()
	a, b := net.Pipe()
	client, err := NewPacketConn(a, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewPacketConn(b, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer closePacketFixture(t, server)
		buffer := make([]byte, MaxDatagramBytes)
		for {
			n, err := server.Read(buffer)
			if err != nil {
				return
			}
			if _, err = server.Write(buffer[:n]); err != nil {
				return
			}
		}
	}()
	return client
}
func packetRoundTrip(t *testing.T, c net.Conn, b []byte) {
	t.Helper()
	if n, err := c.Write(b); err != nil || n != len(b) {
		t.Fatalf("write %d %v", n, err)
	}
	buffer := make([]byte, MaxDatagramBytes)
	n, err := c.Read(buffer)
	if err != nil || !bytes.Equal(buffer[:n], b) {
		t.Fatalf("read %d %v", n, err)
	}
}
func waitPacketCount(t *testing.T, count func() int, want int) {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		if count() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("active=%d want=%d", count(), want)
}

func TestPacketFramingAndExpiry(t *testing.T) {
	c := packetEchoPair(t)
	defer closePacketFixture(t, c)
	packetRoundTrip(t, c, nil)
	packetRoundTrip(t, c, []byte("one"))
	packetRoundTrip(t, c, bytes.Repeat([]byte{'m'}, MaxDatagramBytes))
	if _, err := c.Write(make([]byte, MaxDatagramBytes+1)); err == nil {
		t.Fatal("oversized write accepted")
	}
	if err := packetFixtureAs[*PacketConn](t, c).Refresh(packetTestLease()); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed revived: %v", err)
	}
	for _, tc := range []struct {
		name  string
		frame []byte
		size  int
	}{{"oversize", []byte{255, 255}, MaxDatagramBytes}, {"truncated", []byte{0, 3, 'x'}, 5}, {"short buffer", []byte{0, 2, 'x', 'y'}, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			p, err := NewPacketConn(a, packetTestLease())
			if err != nil {
				t.Fatal(err)
			}
			defer closePacketFixture(t, p)
			go func() { _, _ = b.Write(tc.frame); _ = b.Close() }()
			if _, err := p.Read(make([]byte, tc.size)); err == nil {
				t.Fatal("invalid frame accepted")
			}
		})
	}
	a, b := net.Pipe()
	defer closePacketFixture(t, b)
	p, err := NewPacketConn(a, time.Now().Add(25*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, p)
	time.Sleep(35 * time.Millisecond)
	if p.Refresh(packetTestLease()) == nil {
		t.Fatal("expired grant revived")
	}
	a2, b2 := net.Pipe()
	defer closePacketFixture(t, b2)
	idle, err := NewPacketConn(a2, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, idle)
	idle.mu.Lock()
	idle.lastActivity = time.Now().Add(-PacketIdleTimeout - time.Second)
	idle.mu.Unlock()
	if idle.Refresh(packetTestLease()) == nil {
		t.Fatal("idle association revived")
	}
}

func TestPacketTLSHTTPOptInAuthenticationAndAuthorization(t *testing.T) {
	var permitted atomic.Bool
	permitted.Store(true)
	var checks atomic.Int64
	authorize := func(context.Context, string, string) error {
		checks.Add(1)
		if !permitted.Load() {
			return errors.New("revoked")
		}
		return nil
	}
	h, err := NewPacketHandler(map[string]string{"mapped": packetTestSecret}, func(context.Context, string, string) (net.Conn, error) { return packetEchoPair(t), nil }, authorize, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, h)
	tcp, err := New(map[string]string{"mapped": packetTestSecret}, func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() { _, _ = io.Copy(b, b); _ = b.Close() }()
		return a, nil
	}, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, tcp)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values(PacketTransportHeader)) != 0 {
			h.ServeHTTP(w, r)
		} else {
			tcp.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	dial := func(secret string) (*PacketConn, error) {
		cfg := packetFixtureAs[*http.Transport](t, server.Client().Transport).TLSClientConfig.Clone()
		raw, err := tls.Dial("tcp", strings.TrimPrefix(server.URL, "https://"), cfg)
		if err != nil {
			t.Fatal(err)
		}
		return DialPacketTunnel(context.Background(), raw, "example.invalid:443", "mapped", secret, packetTestLease())
	}
	c, err := dial(packetTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, c)
	packetRoundTrip(t, c, []byte("tls datagram"))
	packetRoundTrip(t, c, nil)
	if checks.Load() < 5 {
		t.Fatalf("per-direction checks missing: %d", checks.Load())
	}
	waitPacketCount(t, h.ActiveCount, 1)
	if _, err := dial(strings.Repeat("q", 32)); err == nil {
		t.Fatal("wrong HTTP secret accepted")
	}
	h.NoNew()
	if _, err := dial(packetTestSecret); err == nil {
		t.Fatal("NoNew accepted new association")
	}
	packetRoundTrip(t, c, []byte("retained"))
	// No UDP header still uses the unchanged stream CONNECT behavior.
	cfg := packetFixtureAs[*http.Transport](t, server.Client().Transport).TLSClientConfig.Clone()
	raw, err := tls.Dial("tcp", strings.TrimPrefix(server.URL, "https://"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, raw)
	_ = raw.SetDeadline(time.Now().Add(time.Second))
	req := httptest.NewRequest(http.MethodConnect, "http://example.invalid:443", nil)
	req.URL = &url.URL{Opaque: "example.invalid:443"}
	req.Host = "example.invalid:443"
	req.Header = make(http.Header)
	req.SetBasicAuth("mapped", packetTestSecret)
	req.Header.Set("Proxy-Authorization", req.Header.Get("Authorization"))
	req.Header.Del("Authorization")
	if req.Write(raw) != nil {
		t.Fatal("TCP request failed")
	}
	reader := bufio.NewReader(raw)
	response, err := http.ReadResponse(reader, req)
	if response != nil {
		defer func() {
			closePacketFixture(t, raw)
			if err := response.Body.Close(); err != nil {
				t.Errorf("TCP response body close: %v", err)
			}
		}()
	}
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("TCP compatibility %v", err)
	}
	if _, err := raw.Write([]byte("tcp")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 3)
	if _, err := io.ReadFull(reader, b); err != nil || string(b) != "tcp" {
		t.Fatalf("TCP data %s %v", b, err)
	}
	permitted.Store(false)
	_, _ = c.Write([]byte("revoked"))
	if _, err := c.Read(make([]byte, 100)); err == nil {
		t.Fatal("revoked packet delivered")
	}
	waitPacketCount(t, h.ActiveCount, 0)
}
func TestPacketHandlerRejectsMalformedOptIn(t *testing.T) {
	h, err := NewPacketHandler(map[string]string{"u": packetTestSecret}, func(context.Context, string, string) (net.Conn, error) { t.Error("malformed dialled"); return nil, nil }, func(context.Context, string, string) error { return nil }, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, h)
	for _, values := range [][]string{nil, {"udp/2"}, {"udp/1", "udp/1"}, {"udp/1, udp/1"}} {
		r := httptest.NewRequest("CONNECT", "http://example.invalid:443", nil)
		r.URL = &url.URL{Host: "example.invalid:443"}
		r.Host = "example.invalid:443"
		r.TLS = &tls.ConnectionState{}
		r.Header[PacketTransportHeader] = values
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("header %v status=%d", values, w.Code)
		}
	}
}

func realPacketEcho(t *testing.T) *net.UDPAddr {
	t.Helper()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = socket.Close() })
	go func() {
		b := make([]byte, 65536)
		for {
			n, from, err := socket.ReadFromUDP(b)
			if err != nil {
				return
			}
			_, _ = socket.WriteToUDP(b[:n], from)
		}
	}()
	return packetFixtureAs[*net.UDPAddr](t, socket.LocalAddr())
}
func openGateUDP(t *testing.T, address, user string) (net.Conn, *net.UDPConn) {
	t.Helper()
	control, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	_ = control.SetDeadline(time.Now().Add(time.Second))
	if err := socksClientAuth(control, user, packetTestSecret); err != nil {
		t.Fatal(err)
	}
	if writePacketAll(control, []byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}) != nil {
		t.Fatal("associate request")
	}
	var h [3]byte
	if _, err := io.ReadFull(control, h[:]); err != nil || h != [3]byte{5, 0, 0} {
		t.Fatalf("associate reply %v %v", h, err)
	}
	relay, err := readSOCKSAddress(control)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := net.ResolveUDPAddr("udp", relay)
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	return control, udp
}
func gateRoundTrip(t *testing.T, c *net.UDPConn, target string, payload []byte) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	b, err := encodeSOCKSDatagram(target, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(b); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 65536)
	n, err := c.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	got, data, err := decodeSOCKSDatagram(buffer[:n])
	if err != nil || got != target || !bytes.Equal(data, payload) {
		t.Fatalf("SOCKS response %s %v", got, err)
	}
}
func TestSOCKS5GateMultiTargetSourceFragmentDrainAndRevoke(t *testing.T) {
	a, b, c := realPacketEcho(t), realPacketEcho(t), realPacketEcho(t)
	var mu sync.Mutex
	connections := map[string]net.Conn{}
	var revoked atomic.Bool
	authorize := func(_ context.Context, _ string, target string) error {
		if revoked.Load() && target == a.String() {
			return errors.New("revoked")
		}
		return nil
	}
	gate, err := NewSOCKS5Gate(map[string]string{"local-node": packetTestSecret}, func(context.Context, string, string) (net.Conn, error) {
		left, right := net.Pipe()
		go func() { _, _ = io.Copy(right, right); _ = right.Close() }()
		return left, nil
	}, func(_ context.Context, _ string, target string) (net.Conn, error) {
		addr, err := net.ResolveUDPAddr("udp", target)
		if err != nil {
			return nil, err
		}
		socket, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			return nil, err
		}
		packet, err := newMessageConn(socket, packetTestLease(), socket.Read, socket.Write)
		if err == nil {
			mu.Lock()
			connections[target] = packet
			mu.Unlock()
		}
		return packet, err
	}, authorize, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, gate)
	go func() { _ = gate.Serve(listener) }()
	control, udp := openGateUDP(t, listener.Addr().String(), "local-node")
	defer closePacketFixture(t, control)
	defer closePacketFixture(t, udp)
	gateRoundTrip(t, udp, a.String(), []byte("A"))
	gateRoundTrip(t, udp, b.String(), nil)
	waitPacketCount(t, gate.ActiveCount, 2)
	// Wrong source port and RFC fragmentation never reach a new destination.
	other, err := net.DialUDP("udp", nil, packetFixtureAs[*net.UDPAddr](t, udp.RemoteAddr()))
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, other)
	frame, _ := encodeSOCKSDatagram(c.String(), []byte("bad"))
	_, _ = other.Write(frame)
	frame[2] = 1
	_, _ = udp.Write(frame)
	time.Sleep(20 * time.Millisecond)
	if gate.ActiveCount() != 2 {
		t.Fatal("fragment/source created association")
	}
	gate.NoNew()
	gateRoundTrip(t, udp, b.String(), []byte("B retained"))
	frame, _ = encodeSOCKSDatagram(c.String(), []byte("new denied"))
	_, _ = udp.Write(frame)
	time.Sleep(20 * time.Millisecond)
	if gate.ActiveCount() != 2 {
		t.Fatal("NoNew added destination")
	}
	revoked.Store(true)
	mu.Lock()
	oldA := connections[a.String()]
	mu.Unlock()
	_ = oldA.Close()
	waitPacketCount(t, gate.ActiveCount, 1)
	gateRoundTrip(t, udp, b.String(), []byte("B after A close"))
	if err := gate.Close(); err != nil {
		t.Fatal(err)
	}
	waitPacketCount(t, gate.ActiveCount, 0)
	if gate.Refresh(packetTestLease()) == nil {
		t.Fatal("closed gate revived")
	}
}

// A small independent SOCKS UDP backend forwards to a real UDP echo socket.
func fakeOwnedSOCKSUDP(t *testing.T, target *net.UDPAddr, mutate bool) (string, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		defer close(done)
		control, err := listener.Accept()
		if err != nil {
			return
		}
		defer closePacketFixture(t, control)
		_ = control.SetDeadline(time.Now().Add(3 * time.Second))
		var greeting [3]byte
		if _, err := io.ReadFull(control, greeting[:]); err != nil || greeting != [3]byte{5, 1, 2} {
			return
		}
		_, _ = control.Write([]byte{5, 2})
		var auth [2]byte
		if _, err := io.ReadFull(control, auth[:]); err != nil {
			return
		}
		user := make([]byte, int(auth[1]))
		if _, err := io.ReadFull(control, user); err != nil {
			return
		}
		var plen [1]byte
		if _, err := io.ReadFull(control, plen[:]); err != nil {
			return
		}
		password := make([]byte, int(plen[0]))
		if _, err := io.ReadFull(control, password); err != nil {
			return
		}
		if string(user) != "backend" || string(password) != packetTestSecret {
			_, _ = control.Write([]byte{1, 1})
			return
		}
		_, _ = control.Write([]byte{1, 0})
		var request [10]byte
		if _, err := io.ReadFull(control, request[:]); err != nil || request != [10]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0} {
			return
		}
		relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			return
		}
		defer closePacketFixture(t, relay)
		port := packetFixtureAs[*net.UDPAddr](t, relay.LocalAddr()).Port
		_, _ = control.Write(append([]byte{5, 0, 0, 1, 127, 0, 0, 1}, packetFixturePort(t, port)...))
		upstream, err := net.DialUDP("udp4", nil, target)
		if err != nil {
			return
		}
		defer closePacketFixture(t, upstream)
		go func() { _, _ = io.Copy(io.Discard, control); _ = relay.Close() }()
		b := make([]byte, 65536)
		for {
			n, from, err := relay.ReadFromUDP(b)
			if err != nil {
				return
			}
			if n < 10 || b[0] != 0 || b[1] != 0 || b[2] != 0 || b[3] != 1 || !net.IP(b[4:8]).Equal(target.IP) || int(binary.BigEndian.Uint16(b[8:10])) != target.Port {
				return
			}
			header := append([]byte(nil), b[:10]...)
			_, _ = upstream.Write(b[10:n])
			_ = upstream.SetReadDeadline(time.Now().Add(time.Second))
			n, err = upstream.Read(b[10:])
			if err != nil {
				return
			}
			if mutate {
				header[9] ^= 1
			}
			copy(b[:10], header)
			_, _ = relay.WriteToUDP(b[:10+n], from)
		}
	}()
	return listener.Addr().String(), done
}
func TestDialSOCKS5UDPRealEchoAndPinnedReply(t *testing.T) {
	target := realPacketEcho(t)
	for _, mutate := range []bool{false, true} {
		address, done := fakeOwnedSOCKSUDP(t, target, mutate)
		conn, err := DialSOCKS5UDP(context.Background(), address, "backend", packetTestSecret, target.String(), packetTestLease())
		if err != nil {
			t.Fatal(err)
		}
		if mutate {
			_, _ = conn.Write([]byte("wrong target"))
			if _, err := conn.Read(make([]byte, 100)); err == nil {
				t.Fatal("cross-destination reply accepted")
			}
		} else {
			packetRoundTrip(t, conn, []byte("real UDP"))
			packetRoundTrip(t, conn, nil)
			if _, err := conn.Write(make([]byte, MaxDatagramBytes)); err == nil {
				t.Fatal("SOCKS header budget ignored")
			}
		}
		_ = conn.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("SOCKS control/UDP leaked")
		}
	}
	if _, err := DialSOCKS5UDP(context.Background(), "192.0.2.1:1080", "backend", packetTestSecret, target.String(), packetTestLease()); err == nil {
		t.Fatal("nonloopback backend accepted")
	}
}
func TestSOCKSDatagramBounds(t *testing.T) {
	for _, target := range []string{"127.0.0.1:53", "[::1]:53", strings.Repeat("a", 253) + ":53"} {
		address, err := encodeSOCKSAddress(target, false)
		if err != nil {
			t.Fatal(err)
		}
		limit := MaxDatagramBytes - 3 - len(address)
		if _, err := encodeSOCKSDatagram(target, make([]byte, limit)); err != nil {
			t.Fatal(err)
		}
		if _, err := encodeSOCKSDatagram(target, make([]byte, limit+1)); err == nil {
			t.Fatal("header overflow accepted")
		}
	}
}

func TestSOCKS5GateTCPAuthenticationAndExpiredRenewal(t *testing.T) {
	gate, err := NewSOCKS5Gate(map[string]string{"local": packetTestSecret}, func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() { _, _ = io.Copy(b, b); _ = b.Close() }()
		return a, nil
	}, func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("unused") }, func(context.Context, string, string) error { return nil }, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, gate)
	go func() { _ = gate.Serve(listener) }()
	bad, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = bad.SetDeadline(time.Now().Add(time.Second))
	if socksClientAuth(bad, "local", strings.Repeat("x", 32)) == nil {
		t.Fatal("bad SOCKS password accepted")
	}
	_ = bad.Close()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, conn)
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if socksClientAuth(conn, "local", packetTestSecret) != nil {
		t.Fatal("SOCKS auth failed")
	}
	address, _ := encodeSOCKSAddress("example.invalid:443", false)
	if writePacketAll(conn, append([]byte{5, 1, 0}, address...)) != nil {
		t.Fatal("TCP connect failed")
	}
	var reply [3]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil || reply != [3]byte{5, 0, 0} {
		t.Fatalf("TCP reply %v %v", reply, err)
	}
	if _, err := readSOCKSAddress(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("TCP")); err != nil {
		t.Fatal(err)
	}
	result := make([]byte, 3)
	if _, err := io.ReadFull(conn, result); err != nil || string(result) != "TCP" {
		t.Fatalf("TCP echo %s %v", result, err)
	}
	gate.mu.Lock()
	gate.until = time.Now().Add(-time.Second)
	gate.mu.Unlock()
	if gate.Refresh(packetTestLease()) == nil {
		t.Fatal("expired SOCKS lease revived")
	}
	handler, err := NewPacketHandler(map[string]string{"u": packetTestSecret}, func(context.Context, string, string) (net.Conn, error) { return nil, nil }, func(context.Context, string, string) error { return nil }, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, handler)
	handler.mu.Lock()
	handler.until = time.Now().Add(-time.Second)
	handler.mu.Unlock()
	if handler.Refresh(packetTestLease()) == nil {
		t.Fatal("expired HTTP UDP lease revived")
	}
}

func TestPacketUnsupportedStaticMappingAndUnsafeHeader(t *testing.T) {
	handler, err := NewPacketHandler(map[string]string{"u": packetTestSecret}, func(context.Context, string, string) (net.Conn, error) {
		return nil, fmt.Errorf("backend: %w", ErrPacketUnsupported)
	}, func(context.Context, string, string) error { return nil }, packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, handler)
	cases := []struct {
		name        string
		serve       http.Handler
		unsupported bool
	}{{"known unsupported", handler, true}}
	for _, protocol := range []string{PacketTransportVersion, "udp/2", ""} {
		version := protocol
		release := make(chan struct{})
		serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if version != "" {
				w.Header().Set(PacketTransportHeader, version)
			}
			w.Header().Set("Content-Length", "1000000")
			w.WriteHeader(http.StatusNotImplemented)
			packetFixtureAs[http.Flusher](t, w).Flush()
			<-release
		})
		t.Run("bodyless "+version, func(t *testing.T) {
			server := httptest.NewTLSServer(serve)
			defer server.Close()
			defer close(release)
			cfg := packetFixtureAs[*http.Transport](t, server.Client().Transport).TLSClientConfig.Clone()
			raw, e := tls.Dial("tcp", strings.TrimPrefix(server.URL, "https://"), cfg)
			if e != nil {
				t.Fatal(e)
			}
			started := time.Now()
			_, e = DialPacketTunnel(context.Background(), raw, "example.invalid:443", "u", packetTestSecret, packetTestLease())
			if errors.Is(e, ErrPacketUnsupported) != (version == PacketTransportVersion) {
				t.Fatalf("protocol %q: %v", version, e)
			}
			if time.Since(started) > time.Second {
				t.Fatal("client attempted to consume unsupported response body")
			}
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(tc.serve)
			defer server.Close()
			cfg := packetFixtureAs[*http.Transport](t, server.Client().Transport).TLSClientConfig.Clone()
			raw, e := tls.Dial("tcp", strings.TrimPrefix(server.URL, "https://"), cfg)
			if e != nil {
				t.Fatal(e)
			}
			_, e = DialPacketTunnel(context.Background(), raw, "example.invalid:443", "u", packetTestSecret, packetTestLease())
			if errors.Is(e, ErrPacketUnsupported) != tc.unsupported {
				t.Fatalf("unsupported mapping: %v", e)
			}
		})
	}
}

type packetBodyFixture struct {
	io.Reader
	close func() error
}

func (body *packetBodyFixture) Close() error { return body.close() }

type packetConnectionFixture struct {
	net.Conn
	close func() error
}

func (connection *packetConnectionFixture) Close() error { return connection.close() }

func TestPacketTunnelResponseBodyOwnership(t *testing.T) {
	bodyFailure := errors.New("response body close failed")
	for _, want := range []error{nil, bodyFailure} {
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			client, peer := net.Pipe()
			defer closePacketFixture(t, peer)
			order := []string{}
			connection := &packetConnectionFixture{Conn: client, close: func() error {
				order = append(order, "transport")
				return client.Close()
			}}
			body := &packetBodyFixture{Reader: strings.NewReader(""), close: func() error {
				order = append(order, "body")
				return want
			}}
			stream := &bufferedPacketStream{Conn: connection, reader: bufio.NewReader(connection), responseBody: body}
			packet, err := NewPacketConn(stream, packetTestLease())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = packet.Close() }()
			written := make(chan error, 1)
			go func() { _, err := peer.Write([]byte{0, 3, 'u', 'd', 'p'}); written <- err }()
			payload := make([]byte, 3)
			if n, err := packet.Read(payload); err != nil || n != 3 || string(payload) != "udp" || len(order) != 0 {
				t.Fatalf("tunnel closed before ownership ended: n=%d err=%v order=%v", n, err, order)
			}
			if err := <-written; err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := packet.Close(); !errors.Is(err, want) {
					t.Fatalf("close error=%v, want=%v", err, want)
				}
			}
			if len(order) != 2 || order[0] != "transport" || order[1] != "body" {
				t.Fatalf("owned close order/count=%v", order)
			}
		})
	}
}
