package proxytransport

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeScopeResolver(t *testing.T) {
	// A poison default resolver proves success and rejection never use OS DNS.
	previous := net.DefaultResolver
	var defaultCalls atomic.Int32
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		defaultCalls.Add(1)
		return nil, errors.New("default DNS forbidden")
	}}
	t.Cleanup(func() {
		net.DefaultResolver = previous
		if defaultCalls.Load() != 0 {
			t.Errorf("default resolver called %d times", defaultCalls.Load())
		}
	})

	if _, err := NewScopeWithResolver(nil, nil, nil); err == nil {
		t.Fatal("nil resolver accepted")
	}
	for _, tc := range []struct {
		name          string
		answers       []netip.Addr
		fail, wantErr bool
	}{
		{name: "public", answers: []netip.Addr{netip.MustParseAddr("198.51.100.9")}},
		{name: "private outside protection", answers: []netip.Addr{netip.MustParseAddr("192.168.1.1")}},
		{name: "resolver error", fail: true, wantErr: true},
		{name: "empty", wantErr: true},
		{name: "invalid", answers: []netip.Addr{{}}, wantErr: true},
		{name: "protected private", answers: []netip.Addr{netip.MustParseAddr("10.1.2.3")}, wantErr: true},
		{name: "mixed protected", answers: []netip.Addr{netip.MustParseAddr("198.51.100.9"), netip.MustParseAddr("10.1.2.3")}, wantErr: true},
		{name: "loopback", answers: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, wantErr: true},
		{name: "mapped", answers: []netip.Addr{netip.MustParseAddr("::ffff:198.51.100.9")}, wantErr: true},
		{name: "excess answers", answers: make([]netip.Addr, 33), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, true)
			scope, err := NewScopeWithResolver([]string{"10.0.0.0/8"}, []string{"blocked.invalid"}, func(got context.Context, network, host string) ([]netip.Addr, error) {
				calls++
				if got != ctx || network != "ip" || host != "resolver-only.invalid" {
					t.Fatal("resolver arguments changed")
				}
				if tc.fail {
					return tc.answers, errors.New("resolver denied")
				}
				return tc.answers, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := scope.Resolve(ctx, "RESOLVER-ONLY.invalid.")
			if (err != nil) != tc.wantErr || calls != 1 {
				t.Fatalf("got %v, %v calls=%d", got, err, calls)
			}
			if !tc.wantErr && got != tc.answers[0] {
				t.Fatal("injected answer not used")
			}
			for _, host := range []string{"blocked.invalid", "sub.blocked.invalid", "bad host", "10.2.3.4"} {
				if _, err := scope.Resolve(ctx, host); err == nil {
					t.Fatalf("accepted protected/invalid %s", host)
				}
			}
			if calls != 1 {
				t.Fatal("protected name/literal reached resolver")
			}
		})
	}
}

func TestNativeScopeResolverCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scope, err := NewScopeWithResolver(nil, nil, func(context.Context, string, string) ([]netip.Addr, error) {
		cancel()
		return []netip.Addr{netip.MustParseAddr("198.51.100.9")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Resolve(ctx, "resolver-only.invalid"); err == nil {
		t.Fatal("canceled resolver accepted")
	}
}

func nativeRelayBound(t *testing.T) func(int) []byte {
	return func(port int) []byte { return append([]byte{1, 0, 0, 0, 0}, packetFixturePort(t, port)...) }
}

func TestNativeUDPInjectedDialRoundTrip(t *testing.T) {
	target := realPacketEcho(t)
	peer := startUpstreamPeer(t, 3, true, nativeRelayBound(t), target)
	calls := 0
	var injected *net.UDPConn
	conn, err := DialSOCKS5UpstreamUDPWithDial(context.Background(), upstreamTestConnect(peer.address), func(ctx context.Context, relay netip.AddrPort) (*net.UDPConn, error) {
		calls++
		if ctx.Err() != nil || relay.Addr() != netip.MustParseAddr("127.0.0.1") || relay.Port() == 0 {
			t.Fatal("dial did not receive approved literal relay")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing setup deadline")
		}
		c, err := (&net.Dialer{}).DialContext(ctx, "udp4", relay.String())
		if err != nil {
			return nil, err
		}
		injected = packetFixtureAs[*net.UDPConn](t, c)
		return injected, nil
	}, netip.MustParseAddr("127.0.0.1"), "", "", target.String(), packetTestLease())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("unexpected dial count")
	}
	// A packet from a different relay socket must never reach the connected read.
	spoof, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, spoof)
	packet, err := encodeSOCKSDatagram(target.String(), []byte("off-path"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spoof.WriteToUDP(packet, packetFixtureAs[*net.UDPAddr](t, injected.LocalAddr())); err != nil {
		t.Fatal(err)
	}
	packetRoundTrip(t, conn, []byte("injected UDP"))
	packetRoundTrip(t, conn, nil)
	_ = conn.Close()
	awaitUpstreamClose(t, peer.closed)
}

func TestNativeUDPInjectedDialRejectsInvalidSockets(t *testing.T) {
	capture, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, capture)
	for _, mode := range []string{"error", "nil", "error with socket", "wrong remote", "wrong family", "unconnected", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			peer := startUpstreamPeer(t, 3, true, nativeRelayBound(t), nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			var returned *net.UDPConn
			conn, err := DialSOCKS5UpstreamUDPWithDial(ctx, upstreamTestConnect(peer.address), func(ctx context.Context, relay netip.AddrPort) (*net.UDPConn, error) {
				calls++
				if mode == "error" {
					return nil, errors.New("denied")
				}
				if mode == "nil" {
					return nil, nil
				}
				var err error
				switch mode {
				case "wrong remote":
					returned, err = net.DialUDP("udp4", nil, packetFixtureAs[*net.UDPAddr](t, capture.LocalAddr()))
				case "wrong family":
					returned, err = net.DialUDP("udp6", nil, &net.UDPAddr{IP: net.IPv6loopback, Port: int(relay.Port())})
				case "unconnected":
					returned, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
				default:
					returned, err = net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(relay))
				}
				if err != nil {
					t.Fatal(err)
				}
				if mode == "canceled" {
					cancel()
				}
				if mode == "error with socket" {
					return returned, errors.New("denied")
				}
				return returned, nil
			}, netip.MustParseAddr("127.0.0.1"), "", "", "198.51.100.2:443", packetTestLease())
			if conn != nil {
				closePacketFixture(t, conn)
				t.Fatal("invalid callback returned usable connection")
			}
			if err == nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if returned != nil {
				if _, err := returned.Write([]byte("must remain unsent")); !errors.Is(err, net.ErrClosed) {
					t.Fatalf("returned socket was not closed: %v", err)
				}
			}
			awaitUpstreamClose(t, peer.closed)
		})
	}
	_ = capture.SetReadDeadline(time.Now().Add(40 * time.Millisecond))
	if n, _, err := capture.ReadFromUDP(make([]byte, 64)); err == nil || n != 0 {
		t.Fatalf("rejected callback sent %d bytes: %v", n, err)
	}
}

func TestNativeUDPRejectsBeforeInjectedDial(t *testing.T) {
	calls := 0
	dial := func(context.Context, netip.AddrPort) (*net.UDPConn, error) {
		calls++
		return nil, errors.New("must not dial")
	}
	for _, reply := range [][]byte{{1, 127, 0, 0, 2}, {4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, append([]byte{3, 9}, []byte("localhost")...)} {
		peer := startUpstreamPeer(t, 3, true, func(port int) []byte { return append(append([]byte(nil), reply...), packetFixturePort(t, port)...) }, nil)
		conn, err := DialSOCKS5UpstreamUDPWithDial(context.Background(), upstreamTestConnect(peer.address), dial, netip.MustParseAddr("127.0.0.1"), "", "", "198.51.100.2:443", packetTestLease())
		if conn != nil || err == nil {
			t.Fatal("unapproved relay accepted")
		}
		awaitUpstreamClose(t, peer.closed)
	}
	connect := func(context.Context) (net.Conn, error) { calls++; return nil, errors.New("must not connect") }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c, e := DialSOCKS5UpstreamUDPWithDial(ctx, upstreamTestConnect("127.0.0.1:1"), dial, netip.MustParseAddr("127.0.0.1"), "", "", "198.51.100.2:443", packetTestLease()); c != nil || e == nil {
		t.Fatal("canceled context accepted")
	}
	if c, e := DialSOCKS5UpstreamUDPWithDial(context.Background(), connect, nil, netip.MustParseAddr("127.0.0.1"), "", "", "198.51.100.2:443", packetTestLease()); c != nil || e == nil {
		t.Fatal("nil callback accepted")
	}
	if calls != 0 {
		t.Fatal("invalid dependencies/relay reached callback")
	}
}
