package proxypublication

import (
	"context"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxypolicy"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/netip"
	"slices"
)

type ProtectionReader interface {
	CurrentProtection(context.Context) (*access.ProtectedDestinations, error)
}
type DestinationResolver interface {
	Resolve(context.Context, string) ([]netip.Addr, error)
}
type DestinationPolicy struct {
	protection ProtectionReader
	resolver   DestinationResolver
}

func NewDestinationPolicy(protection ProtectionReader, resolver DestinationResolver) (*DestinationPolicy, error) {
	if absent(protection) || absent(resolver) {
		return nil, apperrors.ErrServiceUnavailable
	}
	return &DestinationPolicy{protection: protection, resolver: resolver}, nil
}
func (p *DestinationPolicy) AuthorizeDestination(ctx context.Context, _ domain.EndpointContext, bundle domain.BundleRecord, node, host string, port int) error {
	if port < 1 || port > 65535 {
		return apperrors.ErrAccessDenied
	}
	protection, err := p.protection.CurrentProtection(ctx)
	if err != nil || protection == nil || protection.PolicyVersion < 1 {
		return apperrors.ErrAccessDenied
	}
	addresses, err := p.resolvePublicDestination(ctx, host)
	if err != nil {
		return err
	}
	rules := append([]api.NetworkProxyRule(nil), bundle.Rules...)
	for _, cidr := range protection.CIDRs {
		rules = append(rules, api.NetworkProxyRule{RuleID: "protected-cidr-" + cidr, Source: "protection", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "cidr", Value: cidr}, Action: api.NetworkProxyRuleAction{Type: "reject"}})
	}
	for _, protectedHost := range protection.Hosts {
		rules = append(rules, api.NetworkProxyRule{RuleID: "protected-host-" + secretHash(protectedHost)[:16], Source: "protection", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "domain_suffix", Value: protectedHost}, Action: api.NetworkProxyRuleAction{Type: "reject"}})
	}
	action, matched, err := proxypolicy.EvaluateMandatory(rules, host, addresses)
	if err != nil {
		return apperrors.ErrAccessDenied
	}
	if !matched {
		return nil
	}
	switch action.Type {
	case "reject", "direct":
		return apperrors.ErrAccessDenied
	case "node":
		if action.TargetID == node {
			return nil
		}
	case "group":
		for _, group := range bundle.Groups {
			if group.GroupID == action.TargetID && slices.Contains(group.NodeIDs, node) {
				return nil
			}
		}
	}
	return apperrors.ErrAccessDenied
}

func (p *DestinationPolicy) CurrentProtection(ctx context.Context) (*access.ProtectedDestinations, error) {
	return p.protection.CurrentProtection(ctx)
}

func (p *DestinationPolicy) resolvePublicDestination(ctx context.Context, host string) ([]netip.Addr, error) {
	addresses := []netip.Addr{}
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = append(addresses, literal)
	} else {
		addresses, err = p.resolver.Resolve(ctx, host)
		if err != nil {
			return nil, apperrors.ErrAccessDenied
		}
	}
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, apperrors.ErrAccessDenied
	}
	for _, address := range addresses {
		ip := address.Unmap()
		if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
			return nil, apperrors.ErrAccessDenied
		}
	}

	return addresses, nil
}
