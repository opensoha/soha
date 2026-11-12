package networkproxyruntime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxypolicy"
	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"golang.org/x/net/proxy"
)

type publicationIngress struct {
	certificateUntil time.Time
	udpGuard         *egressGuard
	packetEnabled    bool
	packetHandlers   map[*packetIngressLease]struct{}
	udpOwned         *ownedPackets
	udpPolicy        atomic.Pointer[udpPolicySnapshot]
	controller       *clashController
	mu               sync.Mutex
	server           *http.Server
	listener         net.Listener
	accepting        bool
	closed           bool
	closeErr         error
	until            time.Time
	active           atomic.Int64
	handlers         map[*proxytransport.Handler]struct{}
	publication      api.NetworkProxyServerPublication
	epoch            string
	scope            *proxytransport.Scope
	backends         map[string]*egressBackend
	client           *Client
}

func (g *egressGuard) compilePublication(doc map[string]any, desired domain.Configuration, dialect string) error {
	p := desired.Publication
	if p == nil {
		return nil
	}
	if dialect != domain.EngineMihomo || p.ServerHash != desired.ContentHash || !p.ValidUntil.Equal(desired.ValidUntil) || len(p.Nodes) == 0 || len(p.Nodes) > 128 {
		return errors.New("publication unsupported or mismatched")
	}
	// Publication traffic enters only the separately installed TLS gate.
	doc["allow-lan"], doc["bind-address"] = false, "127.0.0.1"
	for _, key := range []string{"port", "socks-port", "mixed-port", "redir-port", "tproxy-port"} {
		doc[key] = 0
	}
	if err := g.compilePublicationGroups(doc, p); err != nil {
		return err
	}
	if err := g.validatePublicationNodes(p); err != nil {
		return err
	}
	targets := []proxypolicy.Target{}
	for _, node := range p.Nodes {
		targets = append(targets, proxypolicy.Target{ID: node.NodeID, Kind: "node"})
	}
	for _, group := range p.Groups {
		targets = append(targets, proxypolicy.Target{ID: group.GroupID, Kind: "group"})
	}
	_, err := proxypolicy.Compose(p.OrgRevision, 0, p.Rules, targets, true)
	return err
}

func (r *Runner) closeIngress() {
	r.pruneRetiredIngress()
	if r.ingress != nil {
		r.ingress.close()
		r.ingress.mu.Lock()
		failed := r.ingress.closeErr != nil
		r.ingress.mu.Unlock()
		if failed || r.ingress.active.Load() != 0 {
			r.retiredIngress = append(r.retiredIngress, r.ingress)
		}
		r.ingress = nil
	}
}

func (p *publicationIngress) close() {
	p.mu.Lock()
	p.closed, p.accepting = true, false
	for handler := range p.handlers {
		p.closeErr = errors.Join(p.closeErr, handler.Close())
	}
	for handler := range p.packetHandlers {
		handler.NoNew()
		p.closeErr = errors.Join(p.closeErr, handler.Close())
	}
	p.mu.Unlock()
	if p.udpOwned != nil {
		err := p.udpOwned.close()
		p.mu.Lock()
		p.closeErr = errors.Join(p.closeErr, err)
		p.mu.Unlock()
	}
	if p.server != nil {
		if err := p.server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			p.mu.Lock()
			p.closeErr = errors.Join(p.closeErr, err)
			p.mu.Unlock()
		}
	}
}

func (p *publicationIngress) setAccepting(accept bool) {
	p.mu.Lock()
	p.accepting = accept && !p.closed
	p.mu.Unlock()
}

// Telemetry failures must not strand a still-authorized owned ingress on its
// previous local deadline. Core's per-flow checks remain authoritative.
func (r *Runner) refreshExistingIngressDeadline(now time.Time) {
	p := r.ingress
	if p == nil || !r.enabled || r.suspended || !r.currentIngressIdentity(p) {
		return
	}
	p.mu.Lock()
	until := r.publicationDeadline(p)
	refresh := !p.closed && p.until.After(now) && until.After(now)
	if refresh {
		p.until = until
	}
	p.mu.Unlock()
	if refresh {
		p.capPacketLeases()
	}
}

func (r *Runner) currentIngressIdentity(p *publicationIngress) bool {
	desired := r.engine.desired
	pub := desired.Publication
	return pub != nil && pub.BundleID != "" && pub.Revision > 0 && pub.CredentialEpoch > 0 &&
		desired.ContentHash != "" && r.appliedRevision == desired.Revision &&
		r.engine.processEpoch != "" && p.epoch == r.engine.processEpoch &&
		pub.ServerHash == desired.ContentHash && p.publication.ServerHash == pub.ServerHash &&
		p.publication.BundleID == pub.BundleID && p.publication.Revision == pub.Revision &&
		p.publication.CredentialEpoch == pub.CredentialEpoch
}

func (r *Runner) publicationDeadline(p *publicationIngress) time.Time {
	until := minTime(r.validUntil, r.engine.desired.Publication.ValidUntil)
	if r.config.ManagedDeployment {
		until = minTime(until, r.deployment.flowUntil())
	}
	return p.capCertificate(until)
}

func (r *Runner) updatePublication(ctx context.Context) error {
	desired := r.engine.desired
	if r.ingress != nil {
		r.ingress.udpPolicy.Store(copyUDPPolicy(desired))
	}
	if !r.enabled || r.suspended || desired.Publication == nil {
		r.closeIngress()
		return nil
	}
	if r.config.ManagedDeployment && !r.deployment.mayApply(time.Now()) {
		if r.ingress != nil {
			r.ingress.setAccepting(false)
			r.ingress.mu.Lock()
			r.ingress.until = r.publicationDeadline(r.ingress)
			r.ingress.mu.Unlock()
			r.ingress.capPacketLeases()
		}
		return nil
	}
	if r.config.IngressListen == "" {
		return errors.New("publication requires installed TLS ingress")
	}
	if r.ingress == nil {
		if err := r.engine.health(ctx); err != nil {
			return err
		}
		p, err := r.newPublicationIngress(desired)
		if err != nil {
			r.suspend("ingress_unavailable")
			return err
		}
		r.ingress = p
	}
	r.ingress.mu.Lock()
	r.ingress.until = r.publicationDeadline(r.ingress)
	ready := r.ingress.until.After(time.Now())
	r.ingress.accepting = ready && (!r.config.ManagedDeployment || r.deployment.mayApply(time.Now()))
	r.ingress.mu.Unlock()
	r.ingress.capPacketLeases()
	if !ready {
		return errors.New("TLS ingress lease expired")
	}
	p := desired.Publication
	if err := r.client.PublicationReady(ctx, api.NetworkProxyBundleReadback{BundleID: p.BundleID, Revision: p.Revision, ConfigurationRevision: desired.Revision, ServerHash: desired.ContentHash, ProcessEpoch: r.engine.processEpoch, ObservedAt: time.Now().UTC(), IngressReady: true, ProtectionReady: true}); err != nil {
		return fmt.Errorf("publication readiness: %w", err)
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (r *Runner) newPublicationIngress(desired domain.Configuration) (*publicationIngress, error) {
	identity, err := tls.LoadX509KeyPair(r.config.IngressCertFile, r.config.IngressKeyFile)
	if err != nil {
		return nil, errors.New("TLS ingress identity unavailable")
	}
	leaf, err := x509.ParseCertificate(identity.Certificate[0])
	if err != nil || leaf.NotBefore.After(time.Now()) || !leaf.NotAfter.After(desired.ValidUntil) {
		return nil, errors.New("TLS ingress certificate lease invalid")
	}
	scope, err := proxytransport.NewScope(desired.Protection.CIDRs, desired.Protection.Hosts)
	if err != nil {
		return nil, err
	}
	if err := verifyPublicationListeners(desired.Publication, r.engine.egress.backends); err != nil {
		return nil, err
	}
	listener, err := tls.Listen("tcp", r.config.IngressListen, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{identity}})
	if err != nil {
		return nil, errors.New("TLS ingress listener unavailable")
	}
	p := &publicationIngress{listener: listener, scope: scope, publication: *desired.Publication, epoch: r.engine.processEpoch, client: r.client, backends: r.engine.egress.backends, until: desired.ValidUntil, handlers: map[*proxytransport.Handler]struct{}{}}
	p.certificateUntil = leaf.NotAfter
	p.packetEnabled = r.engine.egress.udpVerified.Load()
	p.udpGuard = r.engine.egress
	p.controller = r.engine.controller
	p.udpPolicy.Store(copyUDPPolicy(desired))
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { _ = p.server.Serve(listener); p.close() }()
	return p, nil
}

func (p *publicationIngress) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if udpTransportPresent(request.Header) {
		p.serveUDP(w, request)
		return
	}
	p.serveTCP(w, request)
}

func (p *publicationIngress) serveTCP(w http.ResponseWriter, request *http.Request) {
	if request.TLS == nil {
		http.Error(w, "TLS required", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	until, allowed := p.until, p.accepting && !p.closed
	p.mu.Unlock()
	if !allowed || !until.After(time.Now()) || p.active.Load() >= 4096 {
		http.Error(w, "proxy unavailable", http.StatusServiceUnavailable)
		return
	}
	if request.Method != http.MethodConnect {
		http.Error(w, "TCP CONNECT required", http.StatusMethodNotAllowed)
		return
	}
	user, secret, ok := ingressCredential(request)
	credentialID, nodeID, err := proxytransport.DecodeMappedUsername(user)
	if !ok || err != nil {
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	var outbound string
	for _, node := range p.publication.Nodes {
		if node.NodeID == nodeID {
			outbound = node.OutboundID
		}
	}
	b := p.backends[outbound]
	if b == nil {
		http.Error(w, "proxy authorization denied", http.StatusForbidden)
		return
	}
	host, port, err := net.SplitHostPort(request.Host)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 {
		http.Error(w, "invalid destination", http.StatusBadRequest)
		return
	}
	input := api.NetworkProxyIngressCheckInput{BundleID: p.publication.BundleID, Revision: p.publication.Revision, CredentialEpoch: p.publication.CredentialEpoch, CredentialID: credentialID, CredentialSecret: secret, NodeID: nodeID, DestinationHost: host, DestinationPort: number, Network: "tcp", ProcessEpoch: p.epoch}
	checkCtx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	decision, err := p.client.CheckIngress(checkCtx, input)
	cancel()
	if err != nil {
		http.Error(w, "proxy authorization denied", http.StatusForbidden)
		return
	}
	until = minTime(until, decision.ExpiresAt)
	dial := func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return p.dial(ctx, address, nodeID, outbound, b)
	}
	handler, err := proxytransport.New(map[string]string{user: secret}, dial, until)
	if err != nil {
		http.Error(w, "proxy unavailable", http.StatusServiceUnavailable)
		return
	}
	p.mu.Lock()
	if p.closed || !p.accepting {
		p.mu.Unlock()
		_ = handler.Close()
		http.Error(w, "proxy draining", http.StatusServiceUnavailable)
		return
	}
	p.handlers[handler] = struct{}{}
	p.active.Add(1)
	p.mu.Unlock()
	defer func() { _ = handler.Close(); p.mu.Lock(); delete(p.handlers, handler); p.active.Add(-1); p.mu.Unlock() }()
	done := make(chan struct{})
	defer close(done)
	go p.recheck(done, handler, input, until)
	handler.ServeHTTP(w, request)
}

func ingressCredential(r *http.Request) (string, string, bool) {
	values := r.Header.Values("Proxy-Authorization")
	if len(values) != 1 {
		return "", "", false
	}
	copy := r.Clone(r.Context())
	copy.Header = make(http.Header)
	copy.Header.Set("Authorization", values[0])
	return copy.BasicAuth()
}

// Renew before the current authorization expires. A five-second grant leaves
// room for the bounded control call; shorter grants proportionally renew sooner.
func ingressRenewDelay(until, now time.Time) time.Duration {
	return min(2*time.Second, max(50*time.Millisecond, until.Sub(now)/3))
}

func (p *publicationIngress) recheck(done <-chan struct{}, handler ingressLease, input api.NetworkProxyIngressCheckInput, until time.Time) {
	started := time.Now()
	renewal := time.NewTimer(ingressRenewDelay(until, time.Now()))
	defer renewal.Stop()
	expiry := time.NewTimer(time.Until(until))
	defer expiry.Stop()
	for {
		select {
		case <-done:
			return
		case <-expiry.C:
			_ = handler.Close()
			logIngressClosure("ingress.lease", input.Network, time.Since(started), &ingressCheckError{classification: "local_lease_expired"})
			return
		case <-renewal.C:
			attempted := time.Now()
			next, err := p.renewIngress(input, until)
			if err != nil {
				_ = handler.Close()
				logIngressClosure("ingress.check", input.Network, time.Since(attempted), err)
				return
			}
			attempted = time.Now()
			if err := handler.Refresh(next); err != nil {
				_ = handler.Close()
				logIngressClosure("ingress.refresh", input.Network, time.Since(attempted), err)
				return
			}
			until = next
			resetIngressTimer(expiry, time.Until(until))
			renewal.Reset(ingressRenewDelay(until, time.Now()))
		}
	}
}

func (p *publicationIngress) renewIngress(input api.NetworkProxyIngressCheckInput, until time.Time) (time.Time, error) {
	// Never let an in-flight recheck extend the authorization it is renewing.
	ctx, cancel := context.WithDeadline(context.Background(), minTime(until, time.Now().Add(3*time.Second)))
	defer cancel()
	decision, err := p.client.CheckIngress(ctx, input)
	if err != nil {
		return time.Time{}, err
	}
	p.mu.Lock()
	lease, closed := p.until, p.closed
	p.mu.Unlock()
	if closed || !until.After(time.Now()) || !lease.After(time.Now()) {
		return time.Time{}, ingressDecisionError("local_lease_expired", "proxy ingress lease expired", decision)
	}
	return minTime(lease, decision.ExpiresAt), nil
}

func resetIngressTimer(timer *time.Timer, delay time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func (p *publicationIngress) dial(ctx context.Context, address, nodeID, outbound string, b *egressBackend) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip, err := p.scope.Resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	addresses := []netip.Addr{ip}
	if _, err := netip.ParseAddr(host); err != nil {
		addresses, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 || len(addresses) > 32 {
			return nil, errors.New("destination DNS unavailable")
		}
		for _, addr := range addresses {
			if _, err := p.scope.Resolve(ctx, addr.String()); err != nil {
				return nil, err
			}
		}
		ip = addresses[0]
	}
	action, matched, err := proxypolicy.EvaluateMandatory(p.publication.Rules, host, addresses)
	if err != nil {
		return nil, err
	}
	if matched {
		switch string(action.Type) {
		case "reject":
			return nil, errors.New("mandatory proxy destination rejected")
		case "direct":
			b = p.backends["soha-egress-direct"]
		case "node":
			b = nil
			for _, node := range p.publication.Nodes {
				if node.NodeID == action.TargetID {
					b = p.backends[node.OutboundID]
				}
			}
		case "group":
			b = p.backends[action.TargetID]
		default:
			return nil, errors.New("mandatory proxy action unsupported")
		}
		if b == nil {
			return nil, errors.New("mandatory proxy route unavailable")
		}
	}
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(b.port)), &proxy.Auth{User: b.user, Password: b.secret}, &net.Dialer{Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	contextual, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("unbounded backend")
	}
	return contextual.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
}

// Authenticate each generated listener without opening an upstream connection.
func verifyPublicationListeners(p *api.NetworkProxyServerPublication, backends map[string]*egressBackend) error {
	for _, node := range p.Nodes {
		b := backends[node.OutboundID]
		if b == nil {
			return errors.New("publication backend missing")
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(b.port)), time.Second)
		if err != nil {
			return errors.New("publication listener unavailable")
		}
		err = authenticateSOCKS(conn, b)
		_ = conn.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func authenticateSOCKS(conn net.Conn, b *egressBackend) error {
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte{5, 1, 2}); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil || reply[0] != 5 || reply[1] != 2 {
		return errors.New("publication listener authentication unavailable")
	}
	auth := append([]byte{1, byte(len(b.user))}, []byte(b.user)...)
	auth = append(auth, byte(len(b.secret)))
	auth = append(auth, []byte(b.secret)...)
	if _, err := conn.Write(auth); err != nil {
		return err
	}
	if _, err := io.ReadFull(conn, reply); err != nil || reply[0] != 1 || reply[1] != 0 {
		return errors.New("publication listener authentication rejected")
	}
	return nil
}

func mihomoGroupType(kind string) string {
	switch kind {
	case "url_test":
		return "url-test"
	case "load_balance":
		return "load-balance"
	default:
		return kind
	}
}

func (g *egressGuard) compilePublicationGroups(doc map[string]any, p *api.NetworkProxyServerPublication) error {
	groups, err := arraySection(doc["proxy-groups"])
	if err != nil {
		return err
	}
	actual := map[string]map[string]any{}
	for _, raw := range groups {
		group, ok := raw.(map[string]any)
		if !ok {
			return errUnsafeConfiguration
		}
		name, _ := group["name"].(string)
		actual[name] = group
	}
	for _, group := range p.Groups {
		if err := g.validatePublicationGroup(actual[group.GroupID], group); err != nil {
			return err
		}
		b, err := g.add(group.GroupID)
		if err != nil {
			return err
		}
		b.group = true
		listeners, err := arraySection(doc["listeners"])
		if err != nil {
			return err
		}
		doc["listeners"] = append(listeners, mihomoBackend(b, group.GroupID))
	}
	return nil
}
func (g *egressGuard) validatePublicationGroup(raw map[string]any, group api.NetworkProxyGroup) error {
	if raw == nil || raw["type"] != mihomoGroupType(string(group.Type)) || g.backends[group.GroupID] != nil {
		return errUnsafeConfiguration
	}
	members, err := arraySection(raw["proxies"])
	if err != nil || len(members) != len(group.NodeIDs) {
		return errUnsafeConfiguration
	}
	for i, id := range group.NodeIDs {
		if members[i] != id || g.backends[id] == nil || g.backends[id].group {
			return errUnsafeConfiguration
		}
	}
	return nil
}
func (g *egressGuard) validatePublicationNodes(p *api.NetworkProxyServerPublication) error {
	if p.DeliveryMode != "mapped_nodes" && p.DeliveryMode != "gateway" {
		return errUnsafeConfiguration
	}
	seen := map[string]bool{}
	for _, node := range p.Nodes {
		b := g.backends[node.OutboundID]
		if !bindingID.MatchString(node.NodeID) || seen[node.NodeID] || b == nil {
			return errUnsafeConfiguration
		}
		seen[node.NodeID] = true
		if p.DeliveryMode == "mapped_nodes" && (node.NodeID != node.OutboundID || b.group) {
			return errUnsafeConfiguration
		}
		if p.DeliveryMode == "gateway" && !b.group {
			return errUnsafeConfiguration
		}
	}
	return nil
}

func (p *publicationIngress) capCertificate(until time.Time) time.Time {
	if !p.certificateUntil.IsZero() {
		return minTime(until, p.certificateUntil)
	}
	return until
}
