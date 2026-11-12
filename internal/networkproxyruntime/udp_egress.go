package networkproxyruntime

import (
	"context"
	"errors"
	"golang.org/x/net/proxy"
	"net"
	"strconv"
	"time"

	"github.com/opensoha/soha-contracts/proxytransport"
)

const udpGuardProbeTarget = "198.18.0.1:9"

func (g *egressGuard) startSOCKS(p map[string]string, byUser map[string]*egressBackend, scope *proxytransport.Scope, until time.Time) error {
	g.udpOwned = newOwnedPackets()
	g.udpUntil = until
	dialTCP := func(ctx context.Context, user, target string) (net.Conn, error) {
		b := byUser[user]
		if b == nil {
			return nil, errUnsafeConfiguration
		}
		host, port, err := net.SplitHostPort(target)
		if err != nil {
			return nil, err
		}
		if host == g.probeHost && port == "80" {
			b.probes.Add(1)
			return proxytransport.ProbeConnection(), nil
		}
		if !g.accepting.Load() {
			return nil, net.ErrClosed
		}
		ip, err := scope.Resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		return dialOwnedTCP(ctx, b, net.JoinHostPort(ip.String(), port))
	}
	authorize := func(ctx context.Context, user, target string) error {
		b := byUser[user]
		if b == nil || !b.udp || !g.udpLease().After(time.Now()) {
			return errUnsafeConfiguration
		}
		if target == udpGuardProbeTarget {
			return nil
		}
		return g.udpPolicy.Load().checkNode(b.name)
	}
	dialPacket := func(ctx context.Context, user, target string) (net.Conn, error) {
		b := byUser[user]
		if b == nil || !b.udp {
			return nil, proxytransport.ErrPacketUnsupported
		}
		if target == udpGuardProbeTarget {
			return udpProbeConn(g.udpLease(), b), nil
		}
		if !g.accepting.Load() {
			return nil, net.ErrClosed
		}
		host, ip, pinned, err := resolveUDPDestination(ctx, g.udpPolicy.Load(), target)
		if err != nil {
			return nil, err
		}
		check := func(ctx context.Context) error {
			if !g.udpLease().After(time.Now()) {
				return net.ErrClosed
			}
			return g.udpPolicy.Load().check(ctx, host, ip, b.name)
		}
		return g.udpOwned.dial(ctx, func(ctx context.Context) (net.Conn, error) { return dialOwnedUDP(ctx, b, pinned, g.udpLease()) }, check)
	}
	gate, err := proxytransport.NewSOCKS5Gate(p, dialTCP, dialPacket, authorize, until)
	if err != nil {
		return err
	}
	g.socks = gate
	g.udpServing.Store(true)
	go func() {
		_ = gate.Serve(g.listener)
		g.udpServing.Store(false)
		g.udpVerified.Store(false)
		_ = gate.Close()
	}()
	return nil
}

func dialOwnedTCP(ctx context.Context, b *egressBackend, target string) (net.Conn, error) {
	dialer, err := newOwnedSOCKS(b)
	if err != nil {
		return nil, err
	}
	return dialer.DialContext(ctx, "tcp", target)
}

func dialOwnedUDP(ctx context.Context, b *egressBackend, pinned string, until time.Time) (net.Conn, error) {
	return proxytransport.DialSOCKS5UDP(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(b.port)), b.user, b.secret, pinned, until)
}

func (s *udpPolicySnapshot) checkNode(node string) error {
	if !s.permitsNode(node) {
		return errors.New("UDP node no longer authorized")
	}
	return nil
}

func udpProbeConn(until time.Time, b *egressBackend) net.Conn {
	left, right := net.Pipe()
	client, err := proxytransport.NewPacketConn(left, until)
	if err != nil {
		_ = left.Close()
		_ = right.Close()
		return left
	}
	server, err := proxytransport.NewPacketConn(right, until)
	if err != nil {
		_ = client.Close()
		_ = right.Close()
		return client
	}
	go func() {
		defer func() { _ = server.Close() }()
		buffer := make([]byte, proxytransport.MaxDatagramBytes)
		for {
			n, err := server.Read(buffer)
			if err != nil {
				return
			}
			b.udpProbes.Add(1)
			if _, err := server.Write(buffer[:n]); err != nil {
				return
			}
		}
	}()
	return client
}

func (g *egressGuard) verifyUDP(ctx context.Context, b *egressBackend) error {
	if !b.udp {
		return nil
	}
	until := minTime(g.udpLease(), time.Now().Add(2*time.Second))
	probeCtx, cancel := context.WithDeadline(ctx, until)
	defer cancel()
	c, err := proxytransport.DialSOCKS5UDP(probeCtx, net.JoinHostPort("127.0.0.1", strconv.Itoa(b.probePort)), b.user, b.secret, udpGuardProbeTarget, until)
	if err != nil {
		return errors.New("UDP guard probe failed")
	}
	defer func() { _ = c.Close() }()
	before := b.udpProbes.Load()
	payload := []byte(g.probeHost)
	if _, err := c.Write(payload); err != nil {
		return errors.New("UDP guard probe write failed")
	}
	buffer := make([]byte, len(payload))
	n, err := c.Read(buffer)
	if err != nil || n != len(payload) || string(buffer[:n]) != string(payload) || b.udpProbes.Load() <= before {
		return errors.New("UDP guard adapter bypassed")
	}
	return nil
}

func (g *egressGuard) ready() bool {
	if g == nil {
		return false
	}
	if g.socks != nil {
		return g.udpServing.Load() && g.udpLease().After(time.Now()) && g.upstreamsReady()
	}
	return g.handler != nil && g.handler.Ready()
}

func (g *egressGuard) refresh(until time.Time) error {
	if g.socks == nil {
		return g.handler.Refresh(until)
	}
	g.udpMu.Lock()
	g.tcpRefreshUntil = until
	g.udpMu.Unlock()
	err := g.refreshSOCKSGates(until)
	g.udpMu.Lock()
	if err == nil && !g.udpUntil.After(time.Now()) {
		err = net.ErrClosed
	}
	if err == nil {
		g.udpUntil = until
	}
	g.tcpRefreshUntil = time.Time{}
	g.udpMu.Unlock()
	if err != nil {
		g.close()
	}
	return err
}

func (g *egressGuard) refreshSOCKSGates(until time.Time) error {
	for _, bridge := range g.upstreamBridges {
		if err := bridge.gate.Refresh(until); err != nil {
			return err
		}
	}
	return g.socks.Refresh(until)
}

// Only deadline propagation sees the candidate lease. Authorize/read/write
// checks continue to use udpUntil until every owned gate has renewed.
func (g *egressGuard) tcpLeaseLimit() time.Time {
	g.udpMu.Lock()
	defer g.udpMu.Unlock()
	if !g.tcpRefreshUntil.IsZero() {
		return g.tcpRefreshUntil
	}
	return g.udpUntil
}

func newOwnedSOCKS(b *egressBackend) (proxy.ContextDialer, error) {
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(b.port)), &proxy.Auth{User: b.user, Password: b.secret}, &net.Dialer{Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	contextual, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("owned SOCKS lacks cancellation")
	}
	return contextual, nil
}
