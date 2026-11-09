package networkproxyruntime

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

type socksUpstreamFixture struct {
	address      string
	packets      atomic.Int32
	targets      atomic.Int32
	associations atomic.Int32
	closed       atomic.Int32
}

// This peer echoes SOCKS UDP frames without ever dialing their destination.
func newSOCKSUpstreamFixture(t *testing.T, bound []byte, tcp bool) *socksUpstreamFixture {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	f := &socksUpstreamFixture{address: listener.Addr().String()}
	t.Cleanup(func() { listener.Close(); relay.Close() })
	go func() {
		buffer := make([]byte, 65536)
		for {
			n, from, err := relay.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			f.packets.Add(1)
			if n < 10 || string(buffer[4:8]) != string([]byte{1, 1, 1, 1}) || binary.BigEndian.Uint16(buffer[8:10]) != 443 {
				return
			}
			_, _ = relay.WriteToUDP(buffer[:n], from)
		}
	}()
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				defer f.closed.Add(1)
				_ = c.SetDeadline(time.Now().Add(4 * time.Second))
				var greeting [3]byte
				if _, err := io.ReadFull(c, greeting[:]); err != nil || greeting != [3]byte{5, 1, 2} {
					return
				}
				if _, err := c.Write([]byte{5, 2}); err != nil {
					return
				}
				var h [2]byte
				if _, err := io.ReadFull(c, h[:]); err != nil || h[0] != 1 {
					return
				}
				user := make([]byte, int(h[1]))
				if _, err := io.ReadFull(c, user); err != nil {
					return
				}
				var size [1]byte
				if _, err := io.ReadFull(c, size[:]); err != nil {
					return
				}
				secret := make([]byte, int(size[0]))
				if _, err := io.ReadFull(c, secret); err != nil {
					return
				}
				if string(user) != "upstream-user" || string(secret) != "short-secret" {
					return
				}
				_, _ = c.Write([]byte{1, 0})
				var request [10]byte
				if _, err := io.ReadFull(c, request[:]); err != nil || request[0] != 5 || request[3] != 1 {
					return
				}
				if tcp {
					if request[1] != 1 || string(request[4:8]) != string([]byte{1, 1, 1, 1}) || binary.BigEndian.Uint16(request[8:]) != 443 {
						return
					}
					f.targets.Add(1)
					_, _ = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
					_, _ = io.Copy(c, c)
					return
				}
				if request[1] != 3 {
					return
				}
				f.associations.Add(1)
				address := bound
				if address == nil {
					address = []byte{1, 0, 0, 0, 0}
				}
				response := append([]byte{5, 0, 0}, address...)
				response = binary.BigEndian.AppendUint16(response, uint16(relay.LocalAddr().(*net.UDPAddr).Port))
				_, _ = c.Write(response)
				_, _ = io.Copy(io.Discard, c)
			}()
		}
	}()
	return f
}

func installFixtureSOCKSBridge(t *testing.T, f *socksUpstreamFixture) (*egressGuard, *egressBackend) {
	t.Helper()
	until := time.Now().Add(time.Minute)
	g, err := newEgressGuard(nil, until)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.close)
	g.udpUntil = until
	g.udpPolicy.Store(copyUDPPolicy(domain.Configuration{Protection: &domain.Protection{CIDRs: []string{"10.0.0.0/8"}}}))
	node := map[string]any{"name": "node", "type": "socks5", "username": "upstream-user", "password": "short-secret", "udp": true}
	authorize := func(ctx context.Context, _ string, target string) error {
		if !g.udpLease().After(time.Now()) {
			return net.ErrClosed
		}
		host, ip, _, err := resolveUDPDestination(ctx, g.udpPolicy.Load(), target)
		if err != nil {
			return err
		}
		return g.udpPolicy.Load().check(ctx, host, ip, "node")
	}
	connect := func(ctx context.Context) (net.Conn, error) { return (&net.Dialer{}).DialContext(ctx, "tcp", f.address) }
	if err := g.installSOCKSBridge(node, until, connect, netip.MustParseAddr("127.0.0.1"), authorize); err != nil {
		t.Fatal(err)
	}
	if node["password"] == "short-secret" || node["server"] != "127.0.0.1" {
		t.Fatal("native source retained upstream credentials")
	}
	return g, &egressBackend{port: node["port"].(int), user: node["username"].(string), secret: node["password"].(string)}
}

func TestSOCKSUpstreamBridgePinsRelayAndSupportsDrainResume(t *testing.T) {
	f := newSOCKSUpstreamFixture(t, nil, false)
	g, b := installFixtureSOCKSBridge(t, f)
	c, err := dialOwnedUDP(context.Background(), b, "1.1.1.1:443", g.udpLease())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	packetEcho(t, c, []byte("approved"))
	g.accepting.Store(false)
	packetEcho(t, c, []byte("draining"))
	late, err := dialOwnedUDP(context.Background(), b, "1.1.1.1:443", g.udpLease())
	if err != nil {
		t.Fatal(err)
	}
	_ = late.SetDeadline(time.Now().Add(75 * time.Millisecond))
	_, _ = late.Write([]byte("blocked"))
	if _, err := late.Read(make([]byte, 100)); err == nil {
		late.Close()
		t.Fatal("drain forwarded a new destination")
	}
	late.Close()
	if f.associations.Load() != 1 {
		t.Fatal("drain opened a new upstream association")
	}

	g.accepting.Store(true)
	resumed, err := dialOwnedUDP(context.Background(), b, "1.1.1.1:443", g.udpLease())
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	packetEcho(t, resumed, []byte("resumed"))
	if f.packets.Load() != 3 {
		t.Fatal("source relay did not carry exact datagrams")
	}
	bound := time.Now().Add(150 * time.Millisecond)
	g.udpMu.Lock()
	g.udpUntil = bound
	g.udpMu.Unlock()
	if err := g.upstreamBridges[0].gate.Refresh(bound); err != nil {
		t.Fatal(err)
	}
	assertPacketClosed(t, c, bound.Add(time.Second))
	g.close()
	if g.closeErr != nil || g.upstreamBridges[0].owned.activeCount() != 0 {
		t.Fatal("source bridge resources not reaped")
	}
}

func TestSOCKSUpstreamBridgeRejectsUnapprovedRelayWithZeroWitness(t *testing.T) {
	for name, bound := range map[string][]byte{"protected": {1, 10, 0, 0, 1}, "other-ip": {1, 127, 0, 0, 2}, "domain": {3, 9, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't'}} {
		t.Run(name, func(t *testing.T) {
			f := newSOCKSUpstreamFixture(t, bound, false)
			g, b := installFixtureSOCKSBridge(t, f)
			c, err := dialOwnedUDP(context.Background(), b, "1.1.1.1:443", g.udpLease())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(100 * time.Millisecond))
			_, _ = c.Write([]byte("must not reach relay"))
			if _, err := c.Read(make([]byte, 100)); err == nil {
				t.Fatal("unapproved relay returned data")
			}
			until := time.Now().Add(time.Second)
			for f.closed.Load() == 0 && time.Now().Before(until) {
				time.Sleep(time.Millisecond)
			}
			if f.closed.Load() == 0 || g.upstreamBridges[0].owned.activeCount() != 0 {
				t.Fatal("rejected relay control was retained")
			}

			if f.packets.Load() != 0 {
				t.Fatal("unapproved relay received a packet")
			}
		})
	}
}

func TestSOCKSUpstreamBridgeRetainsTCPAndRejectsProtectedTarget(t *testing.T) {
	f := newSOCKSUpstreamFixture(t, nil, true)
	g, b := installFixtureSOCKSBridge(t, f)
	c, err := dialOwnedTCP(context.Background(), b, "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := c.Write([]byte("tcp")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "tcp" {
		t.Fatal("TCP source bridge changed stream")
	}
	if rejected, err := dialOwnedTCP(context.Background(), b, "10.0.0.1:443"); err == nil {
		rejected.Close()
		t.Fatal("protected source target accepted")
	}
	if f.targets.Load() != 1 {
		t.Fatal("protected destination reached source")
	}
	g.close()
}

func TestSOCKSUpstreamEligibilityPreservesExplicitTCPAndTLSVerification(t *testing.T) {
	for _, mode := range []string{"absent", "true", "false"} {
		t.Run(mode, func(t *testing.T) {
			node := map[string]any{"type": "socks5", "name": "node", "server": "1.1.1.1", "port": 1080, "client-fingerprint": "chrome"}
			if mode != "absent" {
				node["udp"] = mode == "true"
			}
			doc := map[string]any{"proxies": []any{node}}
			explicit := map[string]bool{}
			desired := domain.Configuration{Protection: &domain.Protection{}, ValidUntil: time.Now().Add(time.Minute)}
			if err := bootstrapConfigurationWithUDP(context.Background(), domain.EngineMihomo, doc, desired.Protection, explicit); err != nil {
				t.Fatal(err)
			}
			g, err := newEgressGuard(nil, desired.ValidUntil)
			if err != nil {
				t.Fatal(err)
			}
			defer g.close()
			err = g.bridgeMihomoSOCKS(doc, desired, explicit)
			if mode == "true" {
				if !errors.Is(err, errSOCKSUDPUnsupported) {
					t.Fatal("explicit unsafe UDP silently downgraded")
				}
				return
			}
			if err != nil || node["udp"] != false || node["server"] != "1.1.1.1" || len(g.upstreamBridges) != 0 {
				t.Fatal("legacy TCP transport changed")
			}
		})
	}
	node := map[string]any{"tls": true, "servername": "proxy.example", "sni": "proxy.example", "alpn": []any{"h2"}}
	config, err := upstreamSOCKSTLS(node, netip.MustParseAddr("1.1.1.1"))
	if err != nil || config.InsecureSkipVerify || config.ServerName != "proxy.example" || config.RootCAs != nil {
		t.Fatal("verified TLS policy changed")
	}
	node["skip-cert-verify"] = true
	if _, err := upstreamSOCKSTLS(node, netip.MustParseAddr("1.1.1.1")); err == nil {
		t.Fatal("unsafe TLS enabled")
	}
	scope, _ := proxytransport.NewScope(nil, nil)
	if _, _, err := upstreamSOCKSFactory(map[string]any{"server": "127.0.0.1", "port": strconv.Itoa(1080)}, scope); err == nil {
		t.Fatal("production factory allowed loopback source")
	}
}

func assertPacketClosed(t *testing.T, c net.Conn, deadline time.Time) {
	t.Helper()
	_ = c.SetReadDeadline(deadline)
	_, err := c.Read(make([]byte, 100))
	if err == nil {
		t.Fatal("association remained open")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("association only closed at test timeout")
	}
}
