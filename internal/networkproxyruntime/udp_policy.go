package networkproxyruntime

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxyadapter"
	"github.com/opensoha/soha-contracts/proxypolicy"
	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

type udpPolicySnapshot struct {
	protection  domain.Protection
	publication *api.NetworkProxyServerPublication
}

func copyUDPPolicy(desired domain.Configuration) *udpPolicySnapshot {
	if desired.Protection == nil {
		return nil
	}
	s := &udpPolicySnapshot{protection: *desired.Protection}
	s.protection.Hosts = slices.Clone(desired.Protection.Hosts)
	s.protection.CIDRs = slices.Clone(desired.Protection.CIDRs)
	if p := desired.Publication; p != nil {
		copy := *p
		copy.Nodes = slices.Clone(p.Nodes)
		copy.Rules = slices.Clone(p.Rules)
		copy.Groups = slices.Clone(p.Groups)
		for i := range copy.Groups {
			copy.Groups[i].NodeIDs = slices.Clone(p.Groups[i].NodeIDs)
		}
		s.publication = &copy
	}
	return s
}

func (s *udpPolicySnapshot) permitsNode(name string) bool {
	if s == nil {
		return false
	}
	if s.publication == nil || name == "soha-egress-direct" {
		return true
	}
	for _, n := range s.publication.Nodes {
		if n.OutboundID == name {
			return true
		}
	}
	for _, g := range s.publication.Groups {
		if slices.Contains(g.NodeIDs, name) {
			return true
		}
	}
	return false
}

func (s *udpPolicySnapshot) check(ctx context.Context, host string, pinned netip.Addr, node string) error {
	if !s.permitsNode(node) {
		return errors.New("UDP node no longer authorized")
	}
	canonical := strings.ToLower(strings.TrimSuffix(host, "."))
	for _, protected := range s.protection.Hosts {
		if canonical == protected || strings.HasSuffix(canonical, "."+protected) {
			return errors.New("protected UDP destination")
		}
	}
	scope, err := proxytransport.NewScope(s.protection.CIDRs, nil)
	if err != nil {
		return err
	}
	if _, err := scope.Resolve(ctx, pinned.String()); err != nil {
		return err
	}
	return s.checkMandatory(host, pinned, node)
}

func (s *udpPolicySnapshot) checkMandatory(host string, pinned netip.Addr, node string) error {
	if s.publication == nil {
		return nil
	}
	action, matched, err := proxypolicy.EvaluateMandatory(s.publication.Rules, host, []netip.Addr{pinned})
	if err != nil {
		return err
	}
	if !matched {
		return nil
	}
	switch string(action.Type) {
	case "reject":
		return errors.New("mandatory UDP destination rejected")
	case "direct":
		if node != "soha-egress-direct" {
			return errors.New("mandatory UDP direct mismatch")
		}
	case "node":
		for _, n := range s.publication.Nodes {
			if n.NodeID == action.TargetID && n.OutboundID == node {
				return nil
			}
		}
		return errors.New("mandatory UDP node mismatch")
	case "group":
		g, ok := installedGroup(s.publication.Groups, action.TargetID)
		if !ok || !slices.Contains(g.NodeIDs, node) {
			return errors.New("mandatory UDP group mismatch")
		}
	default:
		return errors.New("mandatory UDP action unsupported")
	}
	return nil
}

func resolveUDPDestination(ctx context.Context, s *udpPolicySnapshot, target string) (string, netip.Addr, string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil || s == nil {
		return "", netip.Addr{}, "", errors.New("invalid UDP destination")
	}
	scope, err := proxytransport.NewScope(s.protection.CIDRs, s.protection.Hosts)
	if err != nil {
		return "", netip.Addr{}, "", err
	}
	ip, err := scope.Resolve(ctx, host)
	if err != nil {
		return "", netip.Addr{}, "", err
	}
	return host, ip, net.JoinHostPort(ip.String(), port), nil
}

// Protocol eligibility is separate from upstream availability. HTTP never
// becomes UDP by enabling a listener; a failed UDP upstream has no DIRECT fallback.
func mihomoUDPProtocol(kind string) bool { return proxyadapter.SupportsMihomoUDP(kind) }

func (g *egressGuard) udpLease() time.Time { g.udpMu.Lock(); defer g.udpMu.Unlock(); return g.udpUntil }

func mihomoNodeUDP(node map[string]any) bool {
	kind, _ := node["type"].(string)
	if !mihomoUDPProtocol(kind) {
		return false
	}
	value, present := node["udp"]
	if !present {
		return true
	}
	enabled, ok := value.(bool)
	return ok && enabled
}
