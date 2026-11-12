// Package proxypolicy owns deterministic managed/personal proxy rule composition.
// It is shared by server and endpoint consumers and has no runtime dependencies.
package proxypolicy

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type Rule = api.NetworkProxyRule
type Matcher = api.NetworkProxyRuleMatcher
type Action = api.NetworkProxyRuleAction
type Conflict = api.NetworkProxyRuleConflict
type Composition struct {
	OrgRevision      int64      `json:"orgRevision"`
	PersonalRevision int64      `json:"personalRevision"`
	EffectiveRules   []Rule     `json:"effectiveRules"`
	Conflicts        []Conflict `json:"conflicts"`
}
type Target struct {
	ID   string
	Kind string
}

var ErrRevisionConflict = errors.New("proxy rule revision conflict")

func CheckRevision(expectedOrg, expectedPersonal, actualOrg, actualPersonal int64) error {
	if expectedOrg != actualOrg || expectedPersonal != actualPersonal {
		return ErrRevisionConflict
	}
	return nil
}
func layer(s string) int {
	switch s {
	case "protection":
		return 0
	case "organization_required":
		return 1
	case "personal":
		return 2
	case "organization_default":
		return 3
	case "fallback":
		return 4
	}
	return -1
}
func value(m Matcher) string {
	return m.Value
}

// IsMandatory identifies only the two trusted mandatory layers.
func IsMandatory(r Rule) bool {
	return string(r.Source) == "protection" || string(r.Source) == "organization_required"
}
func mandatory(r Rule) bool { return IsMandatory(r) }
func ValidateRule(r Rule, targets []Target, server bool) error {
	if r.RuleID == "" || len(r.RuleID) > 128 || r.Order < 0 || layer(string(r.Source)) < 0 {
		return errors.New("invalid rule identity/source/order")
	}
	if err := validateMatcher(r.Matcher, server && mandatory(r)); err != nil {
		return err
	}

	switch string(r.Action.Type) {
	case "reject", "direct":
		if r.Action.TargetID != "" {
			return errors.New("unexpected target")
		}
	case "node", "group":
		if r.Action.TargetID == "" {
			return errors.New("missing logical target")
		}
		found := false
		for _, t := range targets {
			if t.ID == r.Action.TargetID && t.Kind == string(r.Action.Type) {
				found = true
				break
			}
		}
		if !found {
			return errors.New("invalid_target")
		}
	default:
		return errors.New("unsupported action")
	}
	if string(r.Source) == "protection" && string(r.Action.Type) != "reject" {
		return errors.New("protection must reject")
	}
	return nil
}
func sameAction(a, b Action) bool {
	return a.Type == b.Type && a.TargetID == b.TargetID
}

// overlap is deliberately conservative for cross-family matchers: a process and
// destination matcher can match the same flow. Unprovable mandatory conflicts fail.
func overlap(a, b Matcher) bool {
	at, bt := string(a.Type), string(b.Type)
	av, bv := strings.ToLower(value(a)), strings.ToLower(value(b))
	if at == "all" || bt == "all" {
		return true
	}
	if at == "cidr" && bt == "cidr" {
		ap, _ := netip.ParsePrefix(av)
		bp, _ := netip.ParsePrefix(bv)
		return ap.Overlaps(bp)
	}
	if (at == "domain" || at == "domain_suffix") && (bt == "domain" || bt == "domain_suffix") {
		if at == "domain" && bt == "domain" {
			return av == bv
		}
		return av == bv || (at == "domain_suffix" && strings.HasSuffix(bv, "."+av)) || (bt == "domain_suffix" && strings.HasSuffix(av, "."+bv))
	}
	if at == bt && (at == "process_name" || at == "process_path") {
		return value(a) == value(b)
	}
	return true
}
func covers(a, b Matcher) bool {
	if string(a.Type) == "all" {
		return true
	}
	if a.Type == b.Type && value(a) == value(b) {
		return true
	}
	if string(a.Type) == "domain_suffix" && (string(b.Type) == "domain" || string(b.Type) == "domain_suffix") {
		av, bv := strings.ToLower(value(a)), strings.ToLower(value(b))
		return av == bv || strings.HasSuffix(bv, "."+av)
	}
	if string(a.Type) == "cidr" && string(b.Type) == "cidr" {
		ap, _ := netip.ParsePrefix(value(a))
		bp, _ := netip.ParsePrefix(value(b))
		return ap.Bits() <= bp.Bits() && ap.Contains(bp.Addr())
	}
	return false
}

// Compose never mutates the input/draft. ProtectedSet deny precedes all rules;
// required organization, personal, organization defaults and fallback follow.
// Mandatory contradictions reject the publication. Partial masks remain ordered
// and explained; a wholly masked personal rule is retained only in its draft.
func Compose(orgRevision, personalRevision int64, rules []Rule, targets []Target, server bool) (Composition, error) {
	out := Composition{OrgRevision: orgRevision, PersonalRevision: personalRevision, EffectiveRules: []Rule{}, Conflicts: []Conflict{}}
	if orgRevision < 0 || personalRevision < 0 {
		return out, errors.New("negative revision")
	}
	ordered := append([]Rule(nil), rules...)
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, r := range ordered {
		counts[string(r.Source)]++
		if counts[string(r.Source)] > 2048 {
			return out, errors.New("rule layer quota exceeded")
		}
		if seen[r.RuleID] {
			return out, errors.New("duplicate ruleId")
		}
		seen[r.RuleID] = true
		if err := ValidateRule(r, targets, server); err != nil {
			return out, fmt.Errorf("rule %s: %w", r.RuleID, err)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if layer(string(a.Source)) != layer(string(b.Source)) {
			return layer(string(a.Source)) < layer(string(b.Source))
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.RuleID < b.RuleID
	})
	for i, r := range ordered {
		if !r.Enabled {
			continue
		}
		masked := false
		for _, higher := range ordered[:i] {
			if !higher.Enabled || !overlap(higher.Matcher, r.Matcher) || sameAction(higher.Action, r.Action) {
				continue
			}
			if mandatory(higher) && mandatory(r) && string(higher.Source) != "protection" {
				return out, fmt.Errorf("mandatory_conflict: %s and %s", higher.RuleID, r.RuleID)
			}
			if mandatory(higher) && string(r.Source) == "personal" {
				out.Conflicts = append(out.Conflicts, Conflict{RuleID: r.RuleID, BlockingRuleID: higher.RuleID, ReasonCode: api.NetworkProxyRuleConflictReasonCode("masked_by_required")})
				if covers(higher.Matcher, r.Matcher) {
					masked = true
				}
			}
		}
		if !masked {
			out.EffectiveRules = append(out.EffectiveRules, r)
		}
	}
	return out, nil
}

func validateMatcher(m Matcher, serverMandatory bool) error {
	v := value(m)
	switch string(m.Type) {
	case "domain", "domain_suffix":
		if v == "" || len(v) > 253 || strings.ContainsAny(v, " /:@*\\\t\r\n") || strings.HasPrefix(v, ".") || strings.HasSuffix(v, ".") {
			return errors.New("invalid domain matcher")
		}
	case "cidr":
		p, e := netip.ParsePrefix(v)
		if e != nil || p.Masked().String() != v {
			return errors.New("noncanonical CIDR")
		}
	case "process_name", "process_path":
		if v == "" || len(v) > 1024 {
			return errors.New("invalid process matcher")
		}
		if serverMandatory {
			return errors.New("unsupported_matcher: mandatory process identity is not trusted at server")
		}
	case "all":
		if m.Value != "" {
			return errors.New("all matcher forbids value")
		}
	default:
		return errors.New("unsupported_matcher")
	}
	return nil
}

// MatchesDestination evaluates only destination-visible matchers. The supplied
// addresses must be the complete resolved, pinned destination set. A CIDR match
// against any address is a match; a missing address set cannot bypass CIDR rules.
// Process conditions cannot be established by a remote proxy and return an error.
func MatchesDestination(m Matcher, host string, addresses []netip.Addr) (bool, error) {
	if err := validateMatcher(m, true); err != nil {
		return false, err
	}
	if strings.TrimSpace(host) != host || host == "" || strings.ContainsAny(host, "/@?#\\\t\r\n%") {
		return false, errors.New("invalid destination host")
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	switch string(m.Type) {
	case "all":
		return true, nil
	case "domain":
		return strings.EqualFold(host, m.Value), nil
	case "domain_suffix":
		suffix := strings.ToLower(m.Value)
		return host == suffix || strings.HasSuffix(host, "."+suffix), nil
	case "cidr":
		prefix, _ := netip.ParsePrefix(m.Value)
		candidates := append([]netip.Addr(nil), addresses...)
		if literal, err := netip.ParseAddr(host); err == nil {
			candidates = append(candidates, literal)
		}
		if len(candidates) == 0 {
			return false, errors.New("resolved destination addresses required for CIDR matcher")
		}
		matched := false
		for _, addr := range candidates {
			if !addr.IsValid() {
				return false, errors.New("invalid resolved destination address")
			}
			if prefix.Contains(addr.Unmap()) {
				matched = true
			}
		}
		return matched, nil
	}
	return false, errors.New("unsupported destination matcher")
}

// EvaluateMandatory shares destination enforcement between Core and App.
// Rules must come from the authenticated policy and their logical targets must
// already be checked against the current inventory/scope. This function checks
// rule shape and matching; it never grants access to an unverified target.
// A matching reject wins over every matching route action. Contradictory route
// actions or unsupported mandatory process conditions fail closed. matched=false
// is not an authorization grant and must not be interpreted as automatic DIRECT.
func EvaluateMandatory(rules []Rule, host string, addresses []netip.Addr) (action Action, matched bool, err error) {
	reject := false
	conflict := false
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if layer(string(r.Source)) < 0 {
			return Action{}, false, errors.New("invalid rule source")
		}
		if !IsMandatory(r) {
			continue
		}
		// Validate structural action shape without claiming inventory authorization.
		targets := []Target(nil)
		if r.Action.TargetID != "" {
			targets = append(targets, Target{ID: r.Action.TargetID, Kind: string(r.Action.Type)})
		}
		if err := ValidateRule(r, targets, true); err != nil {
			return Action{}, false, err
		}
		matches, e := MatchesDestination(r.Matcher, host, addresses)
		if e != nil {
			return Action{}, false, e
		}
		if !matches {
			continue
		}
		if string(r.Action.Type) == "reject" {
			reject = true
			continue
		}
		if matched && !sameAction(action, r.Action) {
			conflict = true
		}
		action = r.Action
		matched = true
	}
	if reject {
		return Action{Type: api.NetworkProxyRuleActionType("reject")}, true, nil
	}
	if conflict {
		return Action{}, false, errors.New("mandatory_conflict")
	}
	return action, matched, nil
}
