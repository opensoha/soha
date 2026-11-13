package networkproxyruntime

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/opensoha/soha/contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

// Only the original SOCKS transport needs a second bridge: its UDP ASSOCIATE
// relay is untrusted input and must never make the native engine dial that relay.
var errSOCKSUDPUnsupported = errors.New("SOCKS UDP transport unsupported")

type socksUpstreamBridge struct {
	gate    *proxytransport.SOCKS5Gate
	owned   *ownedPackets
	serving atomic.Bool
}

func (g *egressGuard) bridgeMihomoSOCKS(doc map[string]any, desired domain.Configuration, explicit map[string]bool) error {
	nodes, err := arraySection(doc["proxies"])
	if err != nil {
		return err
	}
	scope, err := proxytransport.NewScope(desired.Protection.CIDRs, desired.Protection.Hosts)
	if err != nil {
		return err
	}
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		if node["type"] != "socks5" || !mihomoNodeUDP(node) {
			continue
		}
		if err := g.bridgeSOCKSNode(node, scope, desired.ValidUntil); err != nil {
			name, _ := node["name"].(string)
			if explicit[name] || !errors.Is(err, errSOCKSUDPUnsupported) {
				return err
			}
			node["udp"] = false
		}
	}
	return nil
}

func (g *egressGuard) bridgeSOCKSNode(node map[string]any, scope *proxytransport.Scope, until time.Time) error {
	connect, ip, err := upstreamSOCKSFactory(node, scope)
	if err != nil {
		return err
	}
	name, _ := node["name"].(string)
	authorize := func(ctx context.Context, _ string, target string) error {
		return g.authorizeSOCKSUpstream(ctx, name, ip, target)
	}
	return g.installSOCKSBridge(node, until, connect, ip, authorize)
}

func (g *egressGuard) installSOCKSBridge(node map[string]any, until time.Time, connect proxytransport.UpstreamConnect, ip netip.Addr, authorize proxytransport.PacketAuthorize) error {
	user, _ := node["username"].(string)
	secret, _ := node["password"].(string)
	requireTLS, _ := node["tls"].(bool)
	listener, port, err := listenEgressPort()
	if err != nil {
		return err
	}
	localUser, localSecret := "s"+strings.ToLower(rand.Text()), rand.Text()+rand.Text()
	owner := newOwnedPackets()
	dial := func(ctx context.Context, target string, packet bool) (net.Conn, error) {
		if !g.accepting.Load() {
			return nil, net.ErrClosed
		}
		if err := authorize(ctx, "", target); err != nil {
			return nil, err
		}
		deadline := g.udpLease()
		return owner.dial(ctx, func(ctx context.Context) (net.Conn, error) {
			if packet {
				return proxytransport.DialSOCKS5UpstreamUDP(ctx, connect, ip, user, secret, target, deadline)
			}
			return g.dialSOCKSTCP(ctx, connect, ip, user, secret, target, deadline, requireTLS)
		}, func(ctx context.Context) error { return authorize(ctx, "", target) })
	}
	gate, err := proxytransport.NewSOCKS5Gate(map[string]string{localUser: localSecret}, func(ctx context.Context, _ string, target string) (net.Conn, error) { return dial(ctx, target, false) }, func(ctx context.Context, _ string, target string) (net.Conn, error) { return dial(ctx, target, true) }, authorize, until)
	if err != nil {
		_ = listener.Close()
		return err
	}
	bridge := &socksUpstreamBridge{gate: gate, owned: owner}
	g.upstreamBridges = append(g.upstreamBridges, bridge)
	bridge.serving.Store(true)
	go func() {
		defer func() { _ = listener.Close() }()
		defer bridge.serving.Store(false)
		_ = gate.Serve(listener)
		_ = gate.Close()
	}()
	node["server"], node["port"], node["username"], node["password"] = "127.0.0.1", port, localUser, localSecret
	for _, key := range []string{"tls", "skip-cert-verify", "servername", "sni", "alpn"} {
		delete(node, key)
	}
	return nil
}

type renewableSOCKSTCP interface {
	net.Conn
	Refresh(time.Time) error
}

type socksTCPLeaseConn struct {
	renewableSOCKSTCP
	limit func() time.Time
}

func (c *socksTCPLeaseConn) SetDeadline(until time.Time) error {
	if limit := c.limit(); until.IsZero() || limit.Before(until) {
		until = limit
	}
	return c.renewableSOCKSTCP.Refresh(until)
}

func (c *socksTCPLeaseConn) Refresh(until time.Time) error { return c.SetDeadline(until) }

func (g *egressGuard) dialSOCKSTCP(ctx context.Context, connect proxytransport.UpstreamConnect, ip netip.Addr, user, secret, target string, deadline time.Time, requireTLS bool) (net.Conn, error) {
	var certificateUntil time.Time
	verifiedConnect := func(ctx context.Context) (net.Conn, error) {
		conn, err := connect(ctx)
		if err != nil {
			return conn, err
		}
		certificateUntil, err = socksTCPCertificateLimit(conn, requireTLS)
		if err != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return nil, err
		}
		return conn, nil
	}
	conn, err := proxytransport.DialSOCKS5UpstreamTCP(ctx, verifiedConnect, ip, user, secret, target, deadline)
	if err != nil {
		return nil, err
	}
	stream, ok := conn.(renewableSOCKSTCP)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("SOCKS TCP transport cannot renew")
	}
	bounded := &socksTCPLeaseConn{renewableSOCKSTCP: stream, limit: func() time.Time {
		until := g.tcpLeaseLimit()
		if !certificateUntil.IsZero() {
			until = minTime(until, certificateUntil)
		}
		return until
	}}
	// Clamp before registration as well as on every subsequent gate renewal.
	if err := bounded.SetDeadline(deadline); err != nil {
		_ = bounded.Close()
		return nil, err
	}
	return bounded, nil
}

func socksTCPCertificateLimit(conn net.Conn, required bool) (time.Time, error) {
	secure, ok := conn.(*tls.Conn)
	if !ok {
		if required {
			return time.Time{}, errors.New("SOCKS TCP source requires verified TLS")
		}
		return time.Time{}, nil
	}
	state := secure.ConnectionState()
	if !state.HandshakeComplete || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return time.Time{}, errors.New("SOCKS TCP source requires verified TLS")
	}
	until := state.VerifiedChains[0][0].NotAfter
	for _, chain := range state.VerifiedChains {
		for _, certificate := range chain {
			until = minTime(until, certificate.NotAfter)
		}
	}
	return until, nil
}

func upstreamSOCKSFactory(node map[string]any, scope *proxytransport.Scope) (proxytransport.UpstreamConnect, netip.Addr, error) {
	// Reject source extensions we cannot reproduce with verified standard TLS.
	for key := range node {
		if !slices.Contains([]string{"name", "type", "server", "port", "username", "password", "udp", "tls", "skip-cert-verify", "servername", "sni", "alpn"}, key) {
			return nil, netip.Addr{}, errSOCKSUDPUnsupported
		}
	}
	for _, key := range []string{"username", "password"} {
		if value, exists := node[key]; exists {
			if _, ok := value.(string); !ok {
				return nil, netip.Addr{}, errUnsafeConfiguration
			}
		}
	}
	host, _ := node["server"].(string)
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return nil, netip.Addr{}, errUnsafeConfiguration
	}
	ip = ip.Unmap()
	if _, err = scope.Resolve(context.Background(), ip.String()); err != nil {
		return nil, netip.Addr{}, err
	}
	port, err := strconv.Atoi(fmt.Sprint(node["port"]))
	if err != nil || port < 1 || port > 65535 {
		return nil, netip.Addr{}, errUnsafeConfiguration
	}
	tlsConfig, err := upstreamSOCKSTLS(node, ip)
	if err != nil {
		return nil, netip.Addr{}, err
	}
	target := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	return func(ctx context.Context) (net.Conn, error) {
		// Recheck the complete current ProtectedSet before every upstream socket.
		if _, err := scope.Resolve(ctx, ip.String()); err != nil {
			return nil, err
		}
		c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", target)
		if err != nil || tlsConfig == nil {
			return c, err
		}
		secure := tls.Client(c, tlsConfig.Clone())
		if err := secure.HandshakeContext(ctx); err != nil {
			_ = c.Close()
			return nil, errors.New("upstream TLS verification failed")
		}
		return secure, nil
	}, ip, nil
}

func upstreamSOCKSTLS(node map[string]any, ip netip.Addr) (*tls.Config, error) {
	enabled, ok := node["tls"].(bool)
	if node["tls"] != nil && !ok {
		return nil, errSOCKSUDPUnsupported
	}
	if node["skip-cert-verify"] != nil && node["skip-cert-verify"] != false {
		return nil, errSOCKSUDPUnsupported
	}
	if !enabled {
		if node["alpn"] != nil {
			return nil, errSOCKSUDPUnsupported
		}
		return nil, nil
	}
	name := ip.String()
	for _, key := range []string{"servername", "sni"} {
		if value, exists := node[key]; exists {
			v, ok := value.(string)
			if !ok || v == "" || (name != ip.String() && name != v) {
				return nil, errSOCKSUDPUnsupported
			}
			name = v
		}
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: name}
	var err error
	config.NextProtos, err = upstreamSOCKSALPN(node["alpn"])
	if err != nil {
		return nil, err
	}

	return config, nil
}

func (g *egressGuard) closeUpstreams() {
	for _, bridge := range g.upstreamBridges {
		bridge.gate.NoNew()
		g.closeErr = errors.Join(g.closeErr, bridge.owned.close(), bridge.gate.Close())
	}
}

func (g *egressGuard) authorizeSOCKSUpstream(ctx context.Context, node string, source netip.Addr, target string) error {
	if !g.udpLease().After(time.Now()) {
		return net.ErrClosed
	}
	policy := g.udpPolicy.Load()
	host, ip, _, err := resolveUDPDestination(ctx, policy, target)
	if err != nil {
		return err
	}
	scope, err := proxytransport.NewScope(policy.protection.CIDRs, policy.protection.Hosts)
	if err != nil {
		return err
	}
	if _, err = scope.Resolve(ctx, source.String()); err != nil {
		return err
	}
	return policy.check(ctx, host, ip, node)
}

func (g *egressGuard) upstreamsReady() bool {
	for _, bridge := range g.upstreamBridges {
		if !bridge.serving.Load() {
			return false
		}
	}
	return true
}

func upstreamSOCKSALPN(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	values, err := arraySection(raw)
	if err != nil || len(values) > 8 {
		return nil, errSOCKSUDPUnsupported
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		v, ok := value.(string)
		if !ok || len(v) == 0 || len(v) > 255 {
			return nil, errSOCKSUDPUnsupported
		}
		out = append(out, v)
	}
	return out, nil
}
