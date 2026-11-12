package proxypolicy

import (
	"net/netip"
	"testing"
)

func TestMatchesDestination(t *testing.T) {
	for _, tc := range []struct {
		name, kind, value, host string
		ips                     []netip.Addr
		match, wantErr          bool
	}{
		{name: "domain case and terminal dot", kind: "domain", value: "example.invalid", host: "EXAMPLE.INVALID.", match: true},
		{name: "suffix child", kind: "domain_suffix", value: "example.invalid", host: "child.example.invalid", match: true},
		{name: "suffix label boundary", kind: "domain_suffix", value: "example.invalid", host: "notexample.invalid"},
		{name: "CIDR any resolved address", kind: "cidr", value: "10.0.0.0/8", host: "example.invalid", ips: []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("10.1.2.3")}, match: true},
		{name: "mapped IPv4 literal", kind: "cidr", value: "10.0.0.0/8", host: "::ffff:10.1.2.3", match: true},
		{name: "CIDR nonmatch", kind: "cidr", value: "10.0.0.0/8", host: "192.0.2.1"},
		{name: "missing resolution fails closed", kind: "cidr", value: "10.0.0.0/8", host: "example.invalid", wantErr: true},
		{name: "process is untrusted", kind: "process_name", value: "browser", host: "example.invalid", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := rule("r", "organization_required", tc.kind, tc.value, "reject").Matcher
			got, err := MatchesDestination(m, tc.host, tc.ips)
			if got != tc.match || (err != nil) != tc.wantErr {
				t.Fatalf("got %v,%v", got, err)
			}
		})
	}
}
func TestEvaluateMandatoryRejectPriority(t *testing.T) {
	allow := rule("allow", "organization_required", "all", "", "direct")
	deny := rule("deny", "protection", "domain_suffix", "example.invalid", "reject")
	for _, rules := range [][]Rule{{allow, deny}, {deny, allow}} {
		got, matched, err := EvaluateMandatory(rules, "x.example.invalid", nil)
		if err != nil || !matched || got.Type != "reject" {
			t.Fatalf("%+v %v %v", got, matched, err)
		}
	}
	personal := rule("mine", "personal", "all", "", "reject")
	if _, matched, err := EvaluateMandatory([]Rule{personal}, "example.invalid", nil); err != nil || matched {
		t.Fatalf("personal became mandatory: %v %v", matched, err)
	}
	if _, _, err := EvaluateMandatory([]Rule{rule("process", "organization_required", "process_name", "browser", "direct")}, "example.invalid", nil); err == nil {
		t.Fatal("untrusted mandatory process accepted")
	}
}
