package networkproxyruntime

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"encoding/json"
	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"golang.org/x/net/proxy"
	"sigs.k8s.io/yaml"
)

// Each original outbound is reachable only through an authenticated loopback
// bridge. Resolve/authorize/pin happens before the engine sees the destination.
// No plaintext inspection or process-wide interception is implied.
type egressBackend struct {
	udp                   bool
	udpProbes             atomic.Int64
	group                 bool
	name, user, secret    string
	port, probePort       int
	reserve, probeReserve net.Listener
	probes                atomic.Int64
}
type egressGuard struct {
	upstreamBridges []*socksUpstreamBridge
	udpServing      atomic.Bool
	udpEnabled      bool
	udpVerified     atomic.Bool
	udpPolicy       atomic.Pointer[udpPolicySnapshot]
	udpMu           sync.Mutex
	udpUntil        time.Time
	tcpRefreshUntil time.Time
	socks           *proxytransport.SOCKS5Gate
	udpOwned        *ownedPackets
	closeErr        error
	accepting       atomic.Bool
	handler         *proxytransport.Handler
	server          *http.Server
	listener        net.Listener
	port            int
	probeHost       string
	backends        map[string]*egressBackend
}

func newEgressGuard(protection *domain.Protection, until time.Time) (*egressGuard, error) {
	listener, port, err := listenEgressPort()
	if err != nil {
		return nil, err
	}
	gate := &egressGuard{listener: listener, probeHost: "soha-probe-" + strings.ToLower(rand.Text()) + ".invalid", port: port, backends: map[string]*egressBackend{}}
	// Credentials are populated before serving. Start only after compiling every
	// backend; the public engine has not been launched at this point.
	gate.accepting.Store(true)
	gate.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	gate.server.RegisterOnShutdown(func() {
		if gate.handler != nil {
			_ = gate.handler.Close()
		}
	})
	return gate, nil
}

func listenEgressPort() (net.Listener, int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return nil, 0, errUnsafeConfiguration
	}
	return listener, address.Port, nil
}

func (g *egressGuard) add(name string) (*egressBackend, error) {
	if _, exists := g.backends[name]; exists || len(g.backends) >= 128 {
		return nil, errUnsafeConfiguration
	}
	listener, port, err := listenEgressPort()
	if err != nil {
		return nil, err
	}
	b := &egressBackend{name: name, user: "s" + strings.ToLower(rand.Text()), secret: rand.Text() + rand.Text(), port: port, reserve: listener}
	probeListener, probePort, err := listenEgressPort()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	b.probePort = probePort
	b.probeReserve = probeListener
	g.backends[name] = b
	return b, nil
}
func (g *egressGuard) start(p *domain.Protection, until time.Time) error {
	scope, err := proxytransport.NewScope(p.CIDRs, p.Hosts)
	if err != nil {
		return err
	}
	credentials := map[string]string{}
	byUser := map[string]*egressBackend{}
	for _, b := range g.backends {
		credentials[b.user] = b.secret
		byUser[b.user] = b
	}
	if g.udpEnabled {
		return g.startSOCKS(credentials, byUser, scope, until)
	}
	dial := func(ctx context.Context, user, target string) (net.Conn, error) {
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
		ip, err := scope.Resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(b.port)), &proxy.Auth{User: b.user, Password: b.secret}, &net.Dialer{Timeout: 5 * time.Second})
		if err != nil {
			return nil, err
		}
		contextual, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("SOCKS backend lacks cancellation")
		}
		return contextual.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
	}
	g.handler, err = proxytransport.New(credentials, dial, until)
	if err != nil {
		return err
	}
	g.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.accepting.Load() && r.Host != net.JoinHostPort(g.probeHost, "80") {
			http.Error(w, "proxy draining", http.StatusServiceUnavailable)
			return
		}
		g.handler.ServeHTTP(w, r)
	})
	listener := g.listener
	go func() { _ = g.server.Serve(listener); _ = g.handler.Close() }()
	return nil
}
func (g *egressGuard) release() {
	for _, b := range g.backends {
		if b.reserve != nil {
			_ = b.reserve.Close()
			b.reserve = nil
		}
		if b.probeReserve != nil {
			_ = b.probeReserve.Close()
			b.probeReserve = nil
		}
	}
}
func (g *egressGuard) close() {
	if g == nil {
		return
	}
	g.accepting.Store(false)
	g.udpServing.Store(false)
	g.udpVerified.Store(false)
	if g.socks != nil {
		g.socks.NoNew()
	}
	if g.udpOwned != nil {
		g.closeErr = errors.Join(g.closeErr, g.udpOwned.close())
	}
	if g.socks != nil {
		g.closeErr = errors.Join(g.closeErr, g.socks.Close())
	}
	g.closeUpstreams()
	g.release()
	if g.listener != nil {
		_ = g.listener.Close()
	}
	if g.handler != nil {
		_ = g.handler.Close()
	}
	_ = g.server.Close()
}
func (g *egressGuard) front(b *egressBackend) map[string]any {
	kind := "http"
	if g.udpEnabled {
		kind = "socks5"
	}
	return map[string]any{"type": kind, "name": b.name, "server": "127.0.0.1", "port": g.port, "username": b.user, "password": b.secret, "udp": b.udp}
}

func (e *engine) guardPreparedConfiguration(ctx context.Context, doc map[string]any, explicitUDP map[string]bool, desired domain.Configuration) ([]byte, error) {
	gate, err := newEgressGuard(desired.Protection, desired.ValidUntil)
	if err != nil {
		return nil, err
	}
	gate.udpEnabled = e.dialect() == domain.EngineMihomo
	gate.udpPolicy.Store(copyUDPPolicy(desired))
	success := false
	defer func() {
		if !success {
			gate.close()
		}
	}()
	switch e.dialect() {
	case domain.EngineMihomo:
		gate.udpUntil = desired.ValidUntil
		err = gate.bridgeMihomoSOCKS(doc, desired, explicitUDP)
		if err == nil {
			err = gate.mihomo(doc)
		}
	case domain.EngineSingBox:
		err = gate.singBox(doc)
	case domain.EngineV2Ray:
		err = gate.v2ray(doc)
	default:
		err = errUnsafeConfiguration
	}
	if err != nil {
		return nil, err
	}
	if err := gate.compilePublication(doc, desired, e.dialect()); err != nil {
		return nil, err
	}
	raw, err := encodeEngineDocument(e.dialect(), doc)
	if err != nil {
		return nil, err
	}
	if err = gate.start(desired.Protection, desired.ValidUntil); err != nil {
		return nil, err
	}
	if e.config.ManagedDeployment {
		gate.accepting.Store(false)
	}
	e.egress = gate
	success = true
	return raw, nil
}
func decodeEngineDocument(engine string, raw []byte) (map[string]any, error) {
	var doc map[string]any
	var err error
	if engine == domain.EngineMihomo {
		err = yaml.UnmarshalStrict(raw, &doc)
	} else {
		err = json.Unmarshal(raw, &doc)
	}
	if err != nil || doc == nil {
		return nil, errUnsafeConfiguration
	}
	return doc, nil
}
func encodeEngineDocument(engine string, doc map[string]any) ([]byte, error) {
	if engine == domain.EngineMihomo {
		return yaml.Marshal(doc)
	}
	return json.Marshal(doc)
}
func originalName(value any) (string, error) {
	name, ok := value.(string)
	if !ok || name == "" || len(name) > 128 || strings.HasPrefix(strings.ToLower(name), "soha-") {
		return "", errUnsafeConfiguration
	}
	return name, nil
}
func (b *egressBackend) realName() string { return "soha-backend-" + b.user }

func (g *egressGuard) mihomo(doc map[string]any) error {
	nodes, err := arraySection(doc["proxies"])
	if err != nil {
		return err
	}
	fronts := []any{}
	listeners := []any{}
	allowed := map[string]bool{"REJECT": true, "REJECT-DROP": true}
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		name, err := originalName(node["name"])
		if err != nil {
			return err
		}
		b, err := g.add(name)
		if err != nil {
			return err
		}
		allowed[name] = true
		b.udp = g.udpEnabled && mihomoNodeUDP(node)
		node["udp"] = b.udp
		node["name"] = b.realName()
		fronts = append(fronts, g.front(b), node)
		listeners = append(listeners, mihomoBackend(b, b.realName()), mihomoProbe(b))
	}
	direct, err := g.add("soha-egress-direct")
	if err != nil {
		return err
	}
	direct.udp = g.udpEnabled
	allowed[direct.name] = true
	fronts = append(fronts, g.front(direct))
	listeners = append(listeners, mihomoBackend(direct, "DIRECT"), mihomoProbe(direct))
	groups, err := arraySection(doc["proxy-groups"])
	if err != nil {
		return err
	}
	for _, raw := range groups {
		group, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		name, err := originalName(group["name"])
		if err != nil || allowed[name] {
			return errUnsafeConfiguration
		}
		allowed[name] = true
	}
	for _, raw := range groups {
		group, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		if err := guardMihomoGroup(group, allowed, direct.name); err != nil {
			return err
		}
	}
	rules, err := arraySection(doc["rules"])
	if err != nil || len(rules) > 16384 {
		return errUnsafeConfiguration
	}
	if err := guardMihomoRules(rules, allowed, direct.name); err != nil {
		return err
	}

	// A missing MATCH used to fall through to built-in DIRECT.
	rules = append(rules, "MATCH,"+direct.name)
	doc["proxies"], doc["listeners"], doc["rules"] = fronts, listeners, rules
	return nil
}
func mihomoBackend(b *egressBackend, policy string) map[string]any {
	return map[string]any{"name": b.realName(), "type": "socks", "listen": "127.0.0.1", "port": b.port, "udp": b.udp, "proxy": policy, "users": []any{map[string]any{"username": b.user, "password": b.secret}}}
}
func mihomoProbe(b *egressBackend) map[string]any {
	node := mihomoBackend(b, b.name)
	node["name"] = b.realName() + "-probe"
	node["port"] = b.probePort
	return node
}
func guardMihomoGroup(group map[string]any, allowed map[string]bool, direct string) error {
	kind, _ := group["type"].(string)
	if kind != "select" && kind != "url-test" && kind != "fallback" && kind != "load-balance" {
		return errUnsafeConfiguration
	}
	if nonemptySection(group["use"]) {
		return errUnsafeConfiguration
	}
	members, err := arraySection(group["proxies"])
	if err != nil || len(members) == 0 {
		return errUnsafeConfiguration
	}
	for i, raw := range members {
		name, ok := raw.(string)
		if !ok {
			return errUnsafeConfiguration
		}
		if name == "DIRECT" {
			name = direct
		}
		if !allowed[name] {
			return errUnsafeConfiguration
		}
		members[i] = name
	}
	group["proxies"] = members
	return nil
}

func (g *egressGuard) singBox(doc map[string]any) error {
	nodes, err := arraySection(doc["outbounds"])
	if err != nil || len(nodes) == 0 {
		return errUnsafeConfiguration
	}
	inbounds, err := arraySection(doc["inbounds"])
	if err != nil {
		return err
	}
	for _, raw := range inbounds {
		node, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		kind, _ := node["type"].(string)
		if kind != "http" && kind != "socks" && kind != "mixed" {
			return errUnsafeConfiguration
		}
	}
	route, err := object(doc["route"])
	if err != nil {
		return err
	}
	rules, err := arraySection(route["rules"])
	if err != nil {
		return err
	}
	output := []any{}
	backRules := []any{}
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		tag, err := originalName(node["tag"])
		if err != nil {
			return err
		}
		kind, _ := node["type"].(string)
		if kind == "block" {
			output = append(output, node)
			continue
		}
		if kind == "selector" || kind == "urltest" {
			output = append(output, node)
			continue
		}
		b, err := g.add(tag)
		if err != nil {
			return err
		}
		node["tag"] = b.realName()
		output = append(output, map[string]any{"type": "http", "tag": tag, "server": "127.0.0.1", "server_port": g.port, "username": b.user, "password": b.secret}, node)
		inbounds = append(inbounds, map[string]any{"type": "socks", "tag": b.realName(), "listen": "127.0.0.1", "listen_port": b.port, "users": []any{map[string]any{"username": b.user, "password": b.secret}}})
		inbounds = append(inbounds, map[string]any{"type": "socks", "tag": b.realName() + "-probe", "listen": "127.0.0.1", "listen_port": b.probePort, "users": []any{map[string]any{"username": b.user, "password": b.secret}}})
		backRules = append(backRules, map[string]any{"inbound": []string{b.realName()}, "action": "route", "outbound": b.realName()}, map[string]any{"inbound": []string{b.realName() + "-probe"}, "action": "route", "outbound": b.name})
	}
	if len(g.backends) == 0 {
		return errUnsafeConfiguration
	}
	// Backend routes precede front protection rules: their destination is already
	// an authorized literal IP, and credentials are unique to this apply epoch.
	route["rules"] = append(backRules, rules...)
	doc["route"], doc["inbounds"], doc["outbounds"] = route, inbounds, output
	return nil
}
func (g *egressGuard) v2ray(doc map[string]any) error {
	nodes, err := arraySection(doc["outbounds"])
	if err != nil {
		return err
	}
	inbounds, err := arraySection(doc["inbounds"])
	if err != nil {
		return err
	}
	for _, raw := range inbounds {
		node, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		if node["tag"] == "soha-api" {
			continue
		}
		kind, _ := node["protocol"].(string)
		if kind != "http" && kind != "socks" {
			return errUnsafeConfiguration
		}
	}
	route, err := object(doc["routing"])
	if err != nil {
		return err
	}
	rules, err := arraySection(route["rules"])
	if err != nil || len(rules) == 0 {
		return errUnsafeConfiguration
	}
	output := []any{}
	backRules := []any{rules[0]}
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		if node["protocol"] == "blackhole" {
			output = append(output, node)
			continue
		}
		tag, err := originalName(node["tag"])
		if err != nil {
			return err
		}
		b, err := g.add(tag)
		if err != nil {
			return err
		}
		node["tag"] = b.realName()
		front := map[string]any{"tag": tag, "protocol": "http", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": g.port, "users": []any{map[string]any{"user": b.user, "pass": b.secret}}}}}}
		output = append(output, front, node)
		inbounds = append(inbounds, map[string]any{"tag": b.realName(), "listen": "127.0.0.1", "port": b.port, "protocol": "socks", "settings": map[string]any{"auth": "password", "udp": false, "accounts": []any{map[string]any{"user": b.user, "pass": b.secret}}}})
		inbounds = append(inbounds, map[string]any{"tag": b.realName() + "-probe", "listen": "127.0.0.1", "port": b.probePort, "protocol": "socks", "settings": map[string]any{"auth": "password", "udp": false, "accounts": []any{map[string]any{"user": b.user, "pass": b.secret}}}})
		backRules = append(backRules, map[string]any{"type": "field", "inboundTag": []string{b.realName()}, "outboundTag": b.realName()}, map[string]any{"type": "field", "inboundTag": []string{b.realName() + "-probe"}, "outboundTag": b.name})
	}
	if len(g.backends) == 0 {
		return errUnsafeConfiguration
	}
	route["rules"] = append(backRules, rules[1:]...)
	doc["routing"], doc["inbounds"], doc["outbounds"] = route, inbounds, output
	return nil
}

func (e *engine) refreshEgress(until time.Time) error {
	if e.egress == nil || !e.egress.ready() {
		return fmt.Errorf("proxy egress guard missing")
	}
	return e.egress.refresh(until)
}

func (g *egressGuard) verify(ctx context.Context) error {
	if !g.ready() {
		return errors.New("proxy egress guard inactive")
	}
	for _, b := range g.backends {
		if b.group {
			continue
		}
		before := b.probes.Load()
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(b.probePort)), &proxy.Auth{User: b.user, Password: b.secret}, &net.Dialer{Timeout: time.Second})
		if err != nil {
			return err
		}
		contextual, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return errUnsafeConfiguration
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		conn, err := contextual.DialContext(probeCtx, "tcp", net.JoinHostPort(g.probeHost, "80"))
		cancel()
		if err != nil {
			return errors.New("proxy guard adapter probe failed")
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: "+g.probeHost+"\r\n\r\n"); err != nil {
			_ = conn.Close()
			return err
		}
		raw, err := io.ReadAll(io.LimitReader(conn, 128))
		_ = conn.Close()
		if err != nil || !strings.HasPrefix(string(raw), "HTTP/1.1 204") || b.probes.Load() <= before {
			return errors.New("proxy guard adapter was bypassed")
		}
		if g.udpEnabled {
			if err := g.verifyUDP(ctx, b); err != nil {
				return err
			}
		}
	}
	if g.udpEnabled {
		g.udpVerified.Store(true)
	}
	return nil
}

func guardMihomoRules(rules []any, allowed map[string]bool, direct string) error {
	for i, raw := range rules {
		rule, ok := raw.(string)
		if !ok {
			return errUnsafeConfiguration
		}
		parts := strings.Split(rule, ",")
		at := len(parts) - 1
		if at > 0 && parts[at] == "no-resolve" {
			at--
		}
		if at < 1 {
			return errUnsafeConfiguration
		}
		if parts[at] == "DIRECT" {
			parts[at] = direct
		}
		if !allowed[parts[at]] {
			return errUnsafeConfiguration
		}
		rules[i] = strings.Join(parts, ",")
	}
	return nil
}
