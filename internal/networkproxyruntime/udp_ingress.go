package networkproxyruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxypolicy"
	"github.com/opensoha/soha-contracts/proxytransport"
)

type ingressLease interface {
	Refresh(time.Time) error
	Close() error
}
type packetIngressLease struct {
	*proxytransport.PacketHandler
	state *udpIngressAssociation
}

func (h *packetIngressLease) Refresh(until time.Time) error {
	if err := h.PacketHandler.Refresh(until); err != nil {
		return err
	}
	h.state.mu.Lock()
	h.state.until = until
	h.state.mu.Unlock()
	return nil
}

type udpIngressAssociation struct {
	mu                        sync.Mutex
	p                         *publicationIngress
	input                     api.NetworkProxyIngressCheckInput
	until                     time.Time
	requested, outbound, host string
	pinned                    netip.Addr
}

func udpTransportPresent(header http.Header) bool {
	for key := range header {
		if strings.EqualFold(key, proxytransport.PacketTransportHeader) {
			return true
		}
	}
	return false
}
func validUDPTransport(header http.Header) bool {
	values := []string{}
	for key, value := range header {
		if strings.EqualFold(key, proxytransport.PacketTransportHeader) {
			values = append(values, value...)
		}
	}
	return len(values) == 1 && values[0] == proxytransport.PacketTransportVersion
}

func (p *publicationIngress) serveUDP(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || r.ProtoMajor != 1 || r.Method != http.MethodConnect || !validUDPTransport(r.Header) {
		http.Error(w, "invalid datagram transport", 400)
		return
	}
	p.mu.Lock()
	until, allowed := p.until, p.accepting && !p.closed
	p.mu.Unlock()
	if !allowed || !until.After(time.Now()) {
		http.Error(w, "proxy unavailable", http.StatusServiceUnavailable)
		return
	}
	if !p.packetEnabled || !p.packetGuardReady() {
		w.Header().Set(proxytransport.PacketTransportHeader, proxytransport.PacketTransportVersion)
		http.Error(w, "UDP transport unsupported", http.StatusNotImplemented)
		return
	}
	user, secret, ok := ingressCredential(r)
	credential, node, err := proxytransport.DecodeMappedUsername(user)
	if !ok || err != nil {
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	host, port, err := net.SplitHostPort(r.Host)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 {
		http.Error(w, "invalid destination", 400)
		return
	}
	input := api.NetworkProxyIngressCheckInput{BundleID: p.publication.BundleID, Revision: p.publication.Revision, CredentialEpoch: p.publication.CredentialEpoch, CredentialID: credential, CredentialSecret: secret, NodeID: node, DestinationHost: host, DestinationPort: number, Network: "udp", ProcessEpoch: p.epoch}
	ctx, cancel := context.WithDeadline(r.Context(), minTime(until, time.Now().Add(3*time.Second)))
	decision, err := p.client.CheckIngress(ctx, input)
	cancel()
	if err != nil {
		http.Error(w, "proxy authorization denied", http.StatusForbidden)
		return
	}
	until = minTime(until, decision.ExpiresAt)
	state := &udpIngressAssociation{p: p, input: input, requested: node, until: until}
	handler, err := proxytransport.NewPacketHandler(map[string]string{user: secret}, state.dial, state.authorize, until)
	if err != nil {
		http.Error(w, "proxy unavailable", http.StatusServiceUnavailable)
		return
	}
	lease := &packetIngressLease{PacketHandler: handler, state: state}
	if !p.admitPacket(lease) {
		_ = handler.Close()
		http.Error(w, "proxy draining", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		_ = handler.Close()
		p.mu.Lock()
		delete(p.packetHandlers, lease)
		p.active.Add(-1)
		p.mu.Unlock()
	}()
	done := make(chan struct{})
	defer close(done)
	go p.recheck(done, lease, input, until)
	handler.ServeHTTP(w, r)
}

func (p *publicationIngress) admitPacket(handler *packetIngressLease) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || !p.accepting || p.active.Load() >= 4096 {
		return false
	}
	if p.packetHandlers == nil {
		p.packetHandlers = map[*packetIngressLease]struct{}{}
	}
	if p.udpOwned == nil {
		p.udpOwned = newOwnedPackets()
	}
	p.packetHandlers[handler] = struct{}{}
	p.active.Add(1)
	return true
}

func (p *publicationIngress) capPacketLeases() {
	p.mu.Lock()
	until := p.until
	handlers := make([]*packetIngressLease, 0, len(p.packetHandlers))
	for h := range p.packetHandlers {
		handlers = append(handlers, h)
	}
	p.mu.Unlock()
	for _, h := range handlers {
		h.state.mu.Lock()
		old := h.state.until
		h.state.mu.Unlock()
		if until.Before(old) {
			if err := h.Refresh(until); err != nil {
				_ = h.Close()
			}
		}
	}
}

func (a *udpIngressAssociation) authorize(ctx context.Context, _ string, target string) error {
	a.mu.Lock()
	until, host, ip, node := a.until, a.host, a.pinned, a.outbound
	a.mu.Unlock()
	a.p.mu.Lock()
	hard, closed := a.p.until, a.p.closed
	a.p.mu.Unlock()
	if closed || !a.p.packetGuardReady() || !until.After(time.Now()) || !hard.After(time.Now()) {
		return net.ErrClosed
	}
	policy := a.p.udpPolicy.Load()
	if policy == nil || policy.publication == nil {
		return net.ErrClosed
	}
	visible := false
	for _, n := range policy.publication.Nodes {
		if n.NodeID == a.requested {
			visible = true
		}
	}
	if !visible {
		return errors.New("UDP node scope revoked")
	}
	if !ip.IsValid() {
		return nil
	}
	return policy.check(ctx, host, ip, node)
}

func (a *udpIngressAssociation) dial(ctx context.Context, _ string, target string) (net.Conn, error) {
	policy := a.p.udpPolicy.Load()
	host, ip, pinned, err := resolveUDPDestination(ctx, policy, target)
	if err != nil {
		return nil, err
	}
	backend, err := a.p.packetBackend(ctx, policy, a.requested, host, ip)
	if err != nil {
		return nil, err
	}
	if !backend.udp {
		return nil, proxytransport.ErrPacketUnsupported
	}
	a.mu.Lock()
	a.host, a.pinned, a.outbound = host, ip, backend.name
	until := a.until
	a.mu.Unlock()
	check := func(ctx context.Context) error { return a.authorize(ctx, "", target) }
	return a.p.udpOwned.dial(ctx, func(ctx context.Context) (net.Conn, error) { return dialOwnedUDP(ctx, backend, pinned, until) }, check)
}

func (p *publicationIngress) packetBackend(ctx context.Context, policy *udpPolicySnapshot, node, host string, ip netip.Addr) (*egressBackend, error) {
	if policy == nil || policy.publication == nil {
		return nil, errUnsafeConfiguration
	}
	outbound := ""
	for _, n := range policy.publication.Nodes {
		if n.NodeID == node {
			outbound = n.OutboundID
		}
	}
	action, matched, err := proxypolicy.EvaluateMandatory(policy.publication.Rules, host, []netip.Addr{ip})
	if err != nil {
		return nil, err
	}
	if matched {
		switch string(action.Type) {
		case "reject":
			return nil, errUnsafeConfiguration
		case "direct":
			outbound = "soha-egress-direct"
		case "node":
			outbound = ""
			for _, n := range policy.publication.Nodes {
				if n.NodeID == action.TargetID {
					outbound = n.OutboundID
				}
			}
		case "group":
			outbound = action.TargetID
		default:
			return nil, errUnsafeConfiguration
		}
	}
	b := p.backends[outbound]
	if b == nil {
		return nil, errUnsafeConfiguration
	}
	if !b.group {
		return b, nil
	}
	return p.selectedPacketBackend(ctx, policy.publication, outbound)
}

func (p *publicationIngress) selectedPacketBackend(ctx context.Context, current *api.NetworkProxyServerPublication, id string) (*egressBackend, error) {
	installed, ok := installedGroup(p.publication.Groups, id)
	if !ok || p.controller == nil {
		return nil, errUnsafeConfiguration
	}
	group, ok := installedGroup(current.Groups, id)
	if !ok {
		return nil, errUnsafeConfiguration
	}
	raw, err := p.controller.request(ctx, http.MethodGet, "/proxies/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var native struct {
		Now string   `json:"now"`
		All []string `json:"all"`
	}
	if json.Unmarshal(raw, &native) != nil {
		return nil, errUnsafeConfiguration
	}
	narrowed, err := narrowedGroupReadback(group, installed, native.Now, native.All)
	if err != nil || narrowed.SelectedNodeID == "" {
		return nil, errors.New("UDP group selection unavailable")
	}
	b := p.backends[narrowed.SelectedNodeID]
	if b == nil || b.group {
		return nil, errUnsafeConfiguration
	}
	return b, nil
}

func (p *publicationIngress) packetGuardReady() bool {
	return p.udpGuard != nil && p.udpGuard.ready() && p.udpGuard.udpVerified.Load()
}
