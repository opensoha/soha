package proxypublication

import (
	"context"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"net/netip"
	"testing"
)

type protectionFake struct {
	value *access.ProtectedDestinations
	err   error
}

func (p protectionFake) CurrentProtection(context.Context) (*access.ProtectedDestinations, error) {
	return p.value, p.err
}

type resolverFake []netip.Addr

func (r resolverFake) Resolve(context.Context, string) ([]netip.Addr, error) { return r, nil }
func TestDestinationMandatoryAndProtectedSet(t *testing.T) {
	addr := netip.MustParseAddr("8.8.8.8")
	protected := netip.MustParseAddr("10.0.0.1")
	for _, test := range []struct {
		name, host, node string
		addresses        []netip.Addr
		rules            []api.NetworkProxyRule
		deny             bool
	}{
		{name: "public", host: "public.example", node: "node-1", addresses: []netip.Addr{addr}},
		{name: "dns_rebind", host: "public.example", node: "node-1", addresses: []netip.Addr{addr, protected}, deny: true},
		{name: "protected_domain", host: "child.protected.example", node: "node-1", addresses: []netip.Addr{addr}, deny: true},
		{name: "mandatory_reject", host: "public.example", node: "node-1", addresses: []netip.Addr{addr}, rules: []api.NetworkProxyRule{{RuleID: "deny", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "reject"}}}, deny: true},
		{name: "mandatory_route_wrong_node", host: "public.example", node: "node-1", addresses: []netip.Addr{addr}, rules: []api.NetworkProxyRule{{RuleID: "route", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "node", TargetID: "node-2"}}}, deny: true},
		{name: "mandatory_route_correct_node", host: "public.example", node: "node-2", addresses: []netip.Addr{addr}, rules: []api.NetworkProxyRule{{RuleID: "route", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "node", TargetID: "node-2"}}}},
		{name: "untrusted_process", host: "public.example", node: "node-1", addresses: []netip.Addr{addr}, rules: []api.NetworkProxyRule{{RuleID: "process", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "process_name", Value: "browser"}, Action: api.NetworkProxyRuleAction{Type: "reject"}}}, deny: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, _ := NewDestinationPolicy(protectionFake{value: &access.ProtectedDestinations{PolicyVersion: 1, Hosts: []string{"protected.example"}}}, resolverFake(test.addresses))
			err := p.AuthorizeDestination(context.Background(), domain.EndpointContext{}, domain.BundleRecord{Rules: test.rules}, test.node, test.host, 443)
			if (err != nil) != test.deny {
				t.Fatal("unexpected decision", err)
			}
		})
	}
}
