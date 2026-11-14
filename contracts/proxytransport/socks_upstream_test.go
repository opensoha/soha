package proxytransport

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type upstreamTestPeer struct {
	address string
	closed  <-chan struct{}
	targets <-chan string
}

// The fake proxy is separate from the production SOCKS gate. It authenticates,
// reports the chosen BND address and forwards UDP via a real echo socket.
func startUpstreamPeer(t *testing.T, command byte, noAuth bool, bound func(int) []byte, target *net.UDPAddr) upstreamTestPeer {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	closed := make(chan struct{})
	targets := make(chan string, 1)
	go func() {
		defer close(closed)
		control, err := listener.Accept()
		if err != nil {
			return
		}
		defer closePacketFixture(t, control)
		_ = control.SetDeadline(time.Now().Add(3 * time.Second))
		var greeting [3]byte
		if _, err := io.ReadFull(control, greeting[:]); err != nil {
			return
		}
		method := byte(2)
		if noAuth {
			method = 0
		}
		if greeting != [3]byte{5, 1, method} {
			return
		}
		if writePacketAll(control, []byte{5, method}) != nil {
			return
		}
		if !noAuth {
			var h [2]byte
			if _, err := io.ReadFull(control, h[:]); err != nil || h[0] != 1 {
				return
			}
			user := make([]byte, int(h[1]))
			if _, err := io.ReadFull(control, user); err != nil {
				return
			}
			var size [1]byte
			if _, err := io.ReadFull(control, size[:]); err != nil {
				return
			}
			secret := make([]byte, int(size[0]))
			if _, err := io.ReadFull(control, secret); err != nil {
				return
			}
			if string(user) != "upstream-user" || string(secret) != "short-secret" {
				_, _ = control.Write([]byte{1, 1})
				return
			}
			if writePacketAll(control, []byte{1, 0}) != nil {
				return
			}
		}
		var h [3]byte
		if _, err := io.ReadFull(control, h[:]); err != nil || h != [3]byte{5, command, 0} {
			return
		}
		destination, err := readSOCKSAddress(control)
		if err != nil {
			return
		}
		targets <- destination
		if command == 1 {
			if writePacketAll(control, []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}) != nil {
				return
			}
			_, _ = io.Copy(control, control)
			return
		}
		relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			return
		}
		defer closePacketFixture(t, relay)
		port := packetFixtureAs[*net.UDPAddr](t, relay.LocalAddr()).Port
		if writePacketAll(control, append([]byte{5, 0, 0}, bound(port)...)) != nil {
			return
		}
		go func() { _, _ = io.Copy(io.Discard, control); _ = relay.Close() }()
		if target == nil {
			buffer := make([]byte, 65536)
			_, _, _ = relay.ReadFromUDP(buffer)
			return
		}
		upstream, err := net.DialUDP("udp", nil, target)
		if err != nil {
			return
		}
		defer closePacketFixture(t, upstream)
		buffer := make([]byte, 65536)
		for {
			n, from, err := relay.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if n < 10 || buffer[0] != 0 || buffer[1] != 0 || buffer[2] != 0 || buffer[3] != 1 || !net.IP(buffer[4:8]).Equal(target.IP) || int(binary.BigEndian.Uint16(buffer[8:10])) != target.Port {
				return
			}
			header := append([]byte(nil), buffer[:10]...)
			_, _ = upstream.Write(buffer[10:n])
			_ = upstream.SetReadDeadline(time.Now().Add(time.Second))
			n, err = upstream.Read(buffer[10:])
			if err != nil {
				return
			}
			copy(buffer[:10], header)
			_, _ = relay.WriteToUDP(buffer[:10+n], from)
		}
	}()
	return upstreamTestPeer{address: listener.Addr().String(), closed: closed, targets: targets}
}
func upstreamTestConnect(address string) UpstreamConnect {
	return func(ctx context.Context) (net.Conn, error) { return (&net.Dialer{}).DialContext(ctx, "tcp", address) }
}
func awaitUpstreamClose(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("upstream control/relay not closed")
	}
}
func TestSOCKS5UpstreamUDPSamePinnedSourceAndUnspecified(t *testing.T) {
	target := realPacketEcho(t)
	for _, tc := range []struct {
		name   string
		prefix []byte
		noAuth bool
	}{{"same IP", []byte{1, 127, 0, 0, 1}, false}, {"unspecified IPv4", []byte{1, 0, 0, 0, 0}, true}, {"unspecified IPv6", append([]byte{4}, make([]byte, 16)...), false}} {
		t.Run(tc.name, func(t *testing.T) {
			peer := startUpstreamPeer(t, 3, tc.noAuth, func(port int) []byte { return append(append([]byte(nil), tc.prefix...), packetFixturePort(t, port)...) }, target)
			user, secret := "upstream-user", "short-secret"
			if tc.noAuth {
				user, secret = "", ""
			}
			conn, err := DialSOCKS5UpstreamUDP(context.Background(), upstreamTestConnect(peer.address), netip.MustParseAddr("127.0.0.1"), user, secret, target.String(), packetTestLease())
			if err != nil {
				t.Fatal(err)
			}
			packetRoundTrip(t, conn, []byte("source-pinned UDP"))
			packetRoundTrip(t, conn, nil)
			if packetFixtureAs[*net.UDPAddr](t, conn.RemoteAddr()).IP.String() != "127.0.0.1" {
				t.Fatal("socket not pinned to approved source")
			}
			if request := <-peer.targets; request != "0.0.0.0:0" {
				t.Fatalf("associate request %s", request)
			}
			_ = conn.Close()
			awaitUpstreamClose(t, peer.closed)
			if packetFixtureAs[interface{ Refresh(time.Time) error }](t, conn).Refresh(packetTestLease()) == nil {
				t.Fatal("closed upstream UDP revived")
			}
		})
	}
}
func TestSOCKS5UpstreamUDPRejectsRelayRedirectionWithoutPackets(t *testing.T) {
	capture, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer closePacketFixture(t, capture)
	port := packetFixtureAs[*net.UDPAddr](t, capture.LocalAddr()).Port
	for _, tc := range []struct {
		name  string
		reply []byte
	}{{"other source IP", []byte{1, 192, 0, 2, 99}}, {"other loopback IP", []byte{1, 127, 0, 0, 2}}, {"IPv6 protected loopback", append([]byte{4}, net.IPv6loopback...)}, {"hostname", append([]byte{3, 9}, []byte("localhost")...)}, {"numeric hostname", append([]byte{3, 9}, []byte("127.0.0.1")...)}, {"zero port", []byte{1, 127, 0, 0, 1}}} {
		t.Run(tc.name, func(t *testing.T) {
			peer := startUpstreamPeer(t, 3, true, func(_ int) []byte {
				p := port
				if tc.name == "zero port" {
					p = 0
				}
				return append(append([]byte(nil), tc.reply...), byte(p>>8), byte(p))
			}, nil)
			conn, err := DialSOCKS5UpstreamUDP(context.Background(), upstreamTestConnect(peer.address), netip.MustParseAddr("127.0.0.1"), "", "", "198.51.100.2:443", packetTestLease())
			if conn != nil {
				_ = conn.Close()
				t.Fatal("unapproved relay returned a connection")
			}
			if err == nil {
				t.Fatal("unapproved relay accepted")
			}
			awaitUpstreamClose(t, peer.closed)
		})
	}
	_ = capture.SetReadDeadline(time.Now().Add(40 * time.Millisecond))
	if n, _, err := capture.ReadFromUDP(make([]byte, 65536)); err == nil || n != 0 {
		t.Fatalf("refused relay captured %d packets: %v", n, err)
	}
}
func TestSOCKS5UpstreamRejectsWrongFactoryPeerBeforeAuthentication(t *testing.T) {
	for _, udp := range []bool{false, true} {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		bytesSeen := make(chan int, 1)
		go func() {
			c, err := listener.Accept()
			if err != nil {
				bytesSeen <- 0
				return
			}
			defer closePacketFixture(t, c)
			_ = c.SetReadDeadline(time.Now().Add(time.Second))
			n, _ := c.Read(make([]byte, 512))
			bytesSeen <- n
		}()
		connect := upstreamTestConnect(listener.Addr().String())
		var conn net.Conn
		if udp {
			conn, err = DialSOCKS5UpstreamUDP(context.Background(), connect, netip.MustParseAddr("127.0.0.2"), "upstream-user", "short-secret", "198.51.100.2:443", packetTestLease())
		} else {
			conn, err = DialSOCKS5UpstreamTCP(context.Background(), connect, netip.MustParseAddr("127.0.0.2"), "upstream-user", "short-secret", "198.51.100.2:443", packetTestLease())
		}
		if err == nil || conn != nil {
			t.Fatal("wrong actual peer accepted")
		}
		if n := <-bytesSeen; n != 0 {
			t.Fatalf("credentials/request sent before peer verification: %d bytes", n)
		}
		_ = listener.Close()
	}
}
func TestSOCKS5UpstreamTCPKeepsPinnedTargetAndLease(t *testing.T) {
	for _, noAuth := range []bool{false, true} {
		peer := startUpstreamPeer(t, 1, noAuth, nil, nil)
		user, secret := "upstream-user", "short-secret"
		if noAuth {
			user, secret = "", ""
		}
		conn, err := DialSOCKS5UpstreamTCP(context.Background(), upstreamTestConnect(peer.address), netip.MustParseAddr("127.0.0.1"), user, secret, "198.51.100.2:443", packetTestLease())
		if err != nil {
			t.Fatal(err)
		}
		if target := <-peer.targets; target != "198.51.100.2:443" {
			t.Fatalf("CONNECT retargeted: %s", target)
		}
		payload := bytes.Repeat([]byte("t"), MaxDatagramBytes+100)
		done := make(chan error, 1)
		go func() { _, err := conn.Write(payload); done <- err }()
		result := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, result); err != nil || !bytes.Equal(result, payload) {
			t.Fatalf("TCP stream changed: %v", err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		awaitUpstreamClose(t, peer.closed)
		if packetFixtureAs[interface{ Refresh(time.Time) error }](t, conn).Refresh(packetTestLease()) == nil {
			t.Fatal("closed TCP revived")
		}
	}
	peer := startUpstreamPeer(t, 1, true, nil, nil)
	conn, err := DialSOCKS5UpstreamTCP(context.Background(), upstreamTestConnect(peer.address), netip.MustParseAddr("127.0.0.1"), "", "", "198.51.100.2:443", time.Now().Add(35*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(45 * time.Millisecond)
	if packetFixtureAs[interface{ Refresh(time.Time) error }](t, conn).Refresh(packetTestLease()) == nil {
		t.Fatal("expired TCP revived")
	}
	_ = conn.Close()
	awaitUpstreamClose(t, peer.closed)
}
func TestSOCKS5UpstreamInputBounds(t *testing.T) {
	calls := 0
	connect := func(context.Context) (net.Conn, error) { calls++; return nil, errors.New("should not dial") }
	for _, target := range []string{"example.invalid:443", "0.0.0.0:443", "127.0.0.1:0", "[::1]:65536"} {
		if _, err := DialSOCKS5UpstreamUDP(context.Background(), connect, netip.MustParseAddr("127.0.0.1"), "", "", target, packetTestLease()); err == nil {
			t.Fatal("invalid pinned target accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid target invoked factory")
	}
	if _, err := DialSOCKS5UpstreamTCP(context.Background(), connect, netip.Addr{}, "", "", "198.51.100.2:443", packetTestLease()); err == nil {
		t.Fatal("missing source accepted")
	}
	if _, err := DialSOCKS5UpstreamUDP(context.Background(), connect, netip.MustParseAddr("127.0.0.1"), "", "", "198.51.100.2:443", time.Now().Add(-time.Second)); err == nil {
		t.Fatal("expired lease accepted")
	}
	if err := upstreamSOCKSAuth(nil, "", "secret"); err == nil {
		t.Fatal("password silently dropped for NoAuth")
	}
	if err := upstreamSOCKSAuth(nil, strings.Repeat("u", 256), "p"); err == nil {
		t.Fatal("oversized upstream user accepted")
	}
}

func TestSOCKS5UpstreamErrorsDoNotDiscloseSource(t *testing.T) {
	sensitive := "upstream-secret@192.0.2.99"
	if got := upstreamIOError(errors.New(sensitive)); got == nil || strings.Contains(got.Error(), sensitive) {
		t.Fatal("transport error disclosed upstream")
	}
	connect := func(context.Context) (net.Conn, error) { return nil, errors.New(sensitive) }
	_, err := DialSOCKS5UpstreamTCP(context.Background(), connect, netip.MustParseAddr("127.0.0.1"), "", "", "198.51.100.2:443", packetTestLease())
	if err == nil || strings.Contains(err.Error(), sensitive) {
		t.Fatal("factory error disclosed upstream")
	}
	if !errors.Is(upstreamIOError(net.ErrClosed), net.ErrClosed) {
		t.Fatal("closed sentinel lost")
	}
}

func TestSOCKS5UpstreamResponseSourceIsLiteralAndPinned(t *testing.T) {
	for _, tc := range []struct {
		name   string
		packet []byte
		valid  bool
	}{
		{"literal", []byte{0, 0, 0, 1, 127, 0, 0, 1, 0, 53, 'x'}, true},
		{"numeric hostname", append(append([]byte{0, 0, 0, 3, 9}, []byte("127.0.0.1")...), 0, 53, 'x'), false},
		{"different source", []byte{0, 0, 0, 1, 127, 0, 0, 2, 0, 53, 'x'}, false},
		{"fragment", []byte{0, 0, 1, 1, 127, 0, 0, 1, 0, 53, 'x'}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer closePacketFixture(t, relay)
			udp, err := net.DialUDP("udp4", nil, packetFixtureAs[*net.UDPAddr](t, relay.LocalAddr()))
			if err != nil {
				t.Fatal(err)
			}
			defer closePacketFixture(t, udp)
			_ = udp.SetReadDeadline(time.Now().Add(time.Second))
			_, err = relay.WriteToUDP(tc.packet, packetFixtureAs[*net.UDPAddr](t, udp.LocalAddr()))
			if err != nil {
				t.Fatal(err)
			}
			transport := &upstreamPacketTransport{socksUDPTransport: &socksUDPTransport{udp: udp, target: "127.0.0.1:53"}}
			result := make([]byte, 100)
			n, err := transport.Read(result)
			if (err == nil) != tc.valid || tc.valid && (n != 1 || result[0] != 'x') {
				t.Fatalf("pinned reply n=%d err=%v", n, err)
			}
		})
	}
}
