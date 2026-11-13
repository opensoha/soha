package proxypolicy

import (
	"encoding/json"
	"reflect"
	"testing"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func narrowFixture() ([]api.NetworkProxyGroup, []api.NetworkProxyRule) {
	groups := []api.NetworkProxyGroup{{GroupID: "choice", Label: "Choice", Type: "select", NodeIDs: []string{"a", "b"}, SelectedNodeID: "a"}, {GroupID: "only-a", Label: "A", Type: "select", NodeIDs: []string{"a"}, SelectedNodeID: "a"}}
	rules := []api.NetworkProxyRule{rule("mandatory", "organization_required", "domain", "example.invalid", "node"), rule("personal", "personal", "all", "", "group"), rule("empty-group", "organization_default", "all", "", "group"), rule("protected", "protection", "cidr", "10.0.0.0/8", "reject")}
	rules[0].Action.TargetID = "a"
	rules[0].Order = 9
	rules[1].Action.TargetID = "choice"
	rules[2].Action.TargetID = "only-a"
	rules[2].Enabled = false
	return groups, rules
}
func TestNarrowTargetsPreservesPolicyAndInput(t *testing.T) {
	groups, rules := narrowFixture()
	before, _ := json.Marshal([]any{groups, rules})
	got, err := NarrowTargets(groups, rules, []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || !reflect.DeepEqual(got.Groups[0].NodeIDs, []string{"b"}) || got.Groups[0].SelectedNodeID != "" {
		t.Fatalf("selection was replaced or group retained: %+v", got.Groups)
	}
	if got.Rules[0].Action.Type != "reject" || got.Rules[0].Action.TargetID != "" || got.Rules[0].Source != rules[0].Source || got.Rules[0].Matcher != rules[0].Matcher || got.Rules[0].Order != 9 || !got.Rules[0].Enabled {
		t.Fatalf("mandatory weakened: %+v", got.Rules[0])
	}
	if got.Rules[1].Action != rules[1].Action || got.Rules[2].Action.Type != "reject" || got.Rules[2].Enabled || len(got.Conflicts) != 2 {
		t.Fatalf("rules/conflicts: %+v", got)
	}
	for _, c := range got.Conflicts {
		if c.ReasonCode != "invalid_target" || c.BlockingRuleID != c.RuleID {
			t.Fatalf("invalid conflict: %+v", c)
		}
	}
	after, _ := json.Marshal([]any{groups, rules})
	if string(before) != string(after) {
		t.Fatal("mutated input")
	}
	got.Groups[0].NodeIDs[0] = "changed"
	got.Rules[0].Matcher.Value = "changed"
	after, _ = json.Marshal([]any{groups, rules})
	if string(before) != string(after) {
		t.Fatal("output aliases input")
	}
}
func TestNarrowTargetsComposesEffectivePolicy(t *testing.T) {
	groups, rules := narrowFixture()
	for _, s1 := range [][]string{{"a", "b"}, {"a"}, {"b"}, {}} {
		for _, s2 := range [][]string{{"a", "b"}, {"a"}, {"b"}, {}} {
			subset := true
			for _, id := range s2 {
				found := false
				for _, old := range s1 {
					found = found || id == old
				}
				subset = subset && found
			}
			if !subset {
				continue
			}
			first, err := NarrowTargets(groups, rules, s1)
			if err != nil {
				t.Fatal(err)
			}
			twice, err := NarrowTargets(first.Groups, first.Rules, s2)
			if err != nil {
				t.Fatal(err)
			}
			direct, err := NarrowTargets(groups, rules, s2)
			if err != nil {
				t.Fatal(err)
			}
			// Diagnostics are per invocation; original references are deliberately lost
			// after reject. Only effective Groups/Rules obey the composition identity.
			if !reflect.DeepEqual(twice.Groups, direct.Groups) || !reflect.DeepEqual(twice.Rules, direct.Rules) {
				t.Fatalf("nonmonotonic %v -> %v", s1, s2)
			}
		}
	}
	empty, err := NarrowTargets(groups, rules, nil)
	if err != nil || len(empty.Groups) != 0 {
		t.Fatalf("empty scope: %+v %v", empty, err)
	}
	for _, r := range empty.Rules {
		if r.Action.Type != "reject" {
			t.Fatalf("empty scope widened: %+v", r)
		}
	}
}
func TestNarrowTargetsRejectsInvalidFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*[]api.NetworkProxyGroup, *[]api.NetworkProxyRule, *[]string)
	}{
		{"empty allowed ID", func(_ *[]api.NetworkProxyGroup, _ *[]api.NetworkProxyRule, a *[]string) { *a = []string{""} }},
		{"duplicate allowed ID", func(_ *[]api.NetworkProxyGroup, _ *[]api.NetworkProxyRule, a *[]string) { *a = []string{"a", "a"} }},
		{"unknown group type", func(g *[]api.NetworkProxyGroup, _ *[]api.NetworkProxyRule, _ *[]string) { (*g)[0].Type = "unknown" }},
		{"invalid group selection", func(g *[]api.NetworkProxyGroup, _ *[]api.NetworkProxyRule, _ *[]string) {
			(*g)[0].SelectedNodeID = "missing"
		}},
		{"unknown matcher", func(_ *[]api.NetworkProxyGroup, r *[]api.NetworkProxyRule, _ *[]string) {
			(*r)[0].Matcher.Type = "regex"
		}},
		{"unknown source", func(_ *[]api.NetworkProxyGroup, r *[]api.NetworkProxyRule, _ *[]string) { (*r)[0].Source = "unknown" }},
		{"unknown action", func(_ *[]api.NetworkProxyGroup, r *[]api.NetworkProxyRule, _ *[]string) {
			(*r)[0].Action.Type = "unknown"
		}},
		{"invalid target syntax", func(_ *[]api.NetworkProxyGroup, r *[]api.NetworkProxyRule, _ *[]string) {
			(*r)[0].Action.TargetID = "bad/id"
		}},
		{"extra reject target", func(_ *[]api.NetworkProxyGroup, r *[]api.NetworkProxyRule, _ *[]string) {
			(*r)[3].Action.TargetID = "a"
		}},
		{"extra all matcher value", func(_ *[]api.NetworkProxyGroup, r *[]api.NetworkProxyRule, _ *[]string) {
			(*r)[1].Matcher.Value = "unexpected"
		}},
		{"mandatory process", func(_ *[]api.NetworkProxyGroup, r *[]api.NetworkProxyRule, _ *[]string) {
			(*r)[0].Matcher.Type = "process_name"
			(*r)[0].Matcher.Value = "browser"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, r := narrowFixture()
			a := []string{"b"}
			tc.mutate(&g, &r, &a)
			if _, err := NarrowTargets(g, r, a); err == nil {
				t.Fatal("invalid policy silently narrowed")
			}
		})
	}
}
