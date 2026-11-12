package proxypolicy

import (
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"testing"
)

func rule(id, source, matcher, v, action string) Rule {
	r := Rule{RuleID: id, Source: api.NetworkProxyRuleSource(source), Enabled: true, Matcher: Matcher{Type: api.NetworkProxyRuleMatcherType(matcher)}, Action: Action{Type: api.NetworkProxyRuleActionType(action)}}
	if v != "" {
		r.Matcher.Value = v
	}
	return r
}
func TestComposePriorityAndDraft(t *testing.T) {
	rules := []Rule{rule("mine", "personal", "domain", "x.example.invalid", "direct"), rule("guard", "protection", "domain_suffix", "example.invalid", "reject"), rule("default", "organization_default", "all", "", "direct")}
	got, err := Compose(2, 3, rules, nil, false)
	if err != nil || len(got.EffectiveRules) != 2 || len(got.Conflicts) != 1 || got.EffectiveRules[0].RuleID != "guard" || rules[0].RuleID != "mine" {
		t.Fatalf("%+v %v", got, err)
	}
	if CheckRevision(2, 2, 2, 3) == nil {
		t.Fatal("stale CAS accepted")
	}
}
func TestMandatoryAndUnknownReject(t *testing.T) {
	for _, rules := range [][]Rule{{rule("a", "organization_required", "all", "", "reject"), rule("b", "organization_required", "all", "", "direct")}, {rule("a", "organization_required", "process_name", "app", "reject")}, {rule("a", "personal", "regex", ".*", "direct")}} {
		if _, err := Compose(0, 0, rules, nil, true); err == nil {
			t.Fatalf("accepted %+v", rules)
		}
	}
}
func TestDomainBoundary(t *testing.T) {
	if overlap(rule("a", "personal", "domain_suffix", "example.invalid", "reject").Matcher, rule("b", "personal", "domain", "notexample.invalid", "direct").Matcher) {
		t.Fatal("suffix crossed label boundary")
	}
}
