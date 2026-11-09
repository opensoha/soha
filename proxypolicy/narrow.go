package proxypolicy

import (
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// NarrowResult contains the effective structural subset and diagnostics for
// references invalidated in this invocation. It is not an authorization grant.
type NarrowResult struct {
	Groups    []api.NetworkProxyGroup
	Rules     []api.NetworkProxyRule
	Conflicts []api.NetworkProxyRuleConflict
}

var narrowIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// NarrowTargets preserves ordering and clones its inputs. It removes group
// members outside allowedNodeIDs, clears a revoked selection without selecting
// a replacement, and turns revoked node/empty-group references into reject.
// Mandatory rules retain their source, matcher, order and enabled state.
//
// The caller verifies scope is a subset of its previous authorization, persists
// a monotonic ceiling and validates inventory/credentials. ProjectionHash is the
// published bundle template hash, not a per-device scope/lease hash; sharing that
// hash is never permission to widen a previously narrowed scope.
//
// Effective Groups/Rules compose monotonically for S2 subset S1. Conflicts only
// describe newly invalidated references: after node/group -> reject the original
// target cannot be recovered. Recompute diagnostics from retained original
// template/personal drafts, rather than guessing a target from an existing reject.
func NarrowTargets(groups []api.NetworkProxyGroup, rules []api.NetworkProxyRule, allowedNodeIDs []string) (NarrowResult, error) {
	invalid := func(err error) (NarrowResult, error) { return NarrowResult{}, err }
	allowed := make(map[string]bool, len(allowedNodeIDs))
	for _, id := range allowedNodeIDs {
		if !narrowIdentifier.MatchString(id) || allowed[id] {
			return invalid(errors.New("invalid or duplicate allowed node ID"))
		}
		allowed[id] = true
	}
	if len(groups) > 2048 || len(rules) > 10240 {
		return invalid(errors.New("proxy policy quota exceeded"))
	}
	seenGroups := map[string]bool{}
	for _, g := range groups {
		if err := validateNarrowGroup(g); err != nil {
			return invalid(err)
		}
		if seenGroups[g.GroupID] {
			return invalid(errors.New("duplicate group ID"))
		}
		seenGroups[g.GroupID] = true
	}
	seenRules := map[string]bool{}
	layers := map[string]int{}
	for _, r := range rules {
		if !narrowIdentifier.MatchString(r.RuleID) || seenRules[r.RuleID] {
			return invalid(errors.New("invalid or duplicate rule ID"))
		}
		seenRules[r.RuleID] = true
		layers[string(r.Source)]++
		if layers[string(r.Source)] > 2048 {
			return invalid(errors.New("rule layer quota exceeded"))
		}
		// Check shape before rewriting, including disabled rules. Inventory existence
		// is intentionally not asserted: a missing group is a revoked reference.
		targets := []Target(nil)
		if r.Action.TargetID != "" {
			if !narrowIdentifier.MatchString(r.Action.TargetID) {
				return invalid(errors.New("invalid logical target ID"))
			}
			targets = append(targets, Target{ID: r.Action.TargetID, Kind: string(r.Action.Type)})
		}
		if err := ValidateRule(r, targets, true); err != nil {
			return invalid(fmt.Errorf("rule %s: %w", r.RuleID, err))
		}
	}
	out := NarrowResult{Groups: make([]api.NetworkProxyGroup, 0, len(groups)), Rules: make([]api.NetworkProxyRule, 0, len(rules)), Conflicts: []api.NetworkProxyRuleConflict{}}
	keptGroups := map[string]bool{}
	targets := make([]Target, 0, len(allowed)+len(groups))
	for _, id := range allowedNodeIDs {
		targets = append(targets, Target{ID: id, Kind: "node"})
	}
	for _, original := range groups {
		g := original
		g.NodeIDs = make([]string, 0, len(original.NodeIDs))
		selectedKept := false
		for _, id := range original.NodeIDs {
			if allowed[id] {
				g.NodeIDs = append(g.NodeIDs, id)
				if id == g.SelectedNodeID {
					selectedKept = true
				}
			}
		}
		if len(g.NodeIDs) == 0 {
			continue
		}
		if !selectedKept {
			g.SelectedNodeID = ""
		}
		if err := validateNarrowGroup(g); err != nil {
			return invalid(err)
		}
		out.Groups = append(out.Groups, g)
		keptGroups[g.GroupID] = true
		targets = append(targets, Target{ID: g.GroupID, Kind: "group"})
	}
	for _, original := range rules {
		r := original
		revoked := string(r.Action.Type) == "node" && !allowed[r.Action.TargetID] || string(r.Action.Type) == "group" && !keptGroups[r.Action.TargetID]
		if revoked {
			r.Action = api.NetworkProxyRuleAction{Type: api.NetworkProxyRuleActionType("reject")}
			// invalid_target has no masking rule. Self-reference identifies the rule
			// requiring repair while satisfying the existing required conflict fields.
			out.Conflicts = append(out.Conflicts, api.NetworkProxyRuleConflict{RuleID: r.RuleID, BlockingRuleID: r.RuleID, ReasonCode: api.NetworkProxyRuleConflictReasonCode("invalid_target")})
		}
		if err := ValidateRule(r, targets, true); err != nil {
			return invalid(fmt.Errorf("narrowed rule %s: %w", r.RuleID, err))
		}
		out.Rules = append(out.Rules, r)
	}
	return out, nil
}

func validateNarrowGroup(g api.NetworkProxyGroup) error {
	if !narrowIdentifier.MatchString(g.GroupID) || !utf8.ValidString(g.Label) || utf8.RuneCountInString(g.Label) < 1 || utf8.RuneCountInString(g.Label) > 128 || len(g.NodeIDs) > 128 {
		return errors.New("invalid proxy group fields")
	}
	switch string(g.Type) {
	case "select", "url_test", "fallback", "load_balance":
	default:
		return errors.New("unsupported proxy group type")
	}
	selected := g.SelectedNodeID == ""
	for _, id := range g.NodeIDs {
		if !narrowIdentifier.MatchString(id) {
			return errors.New("invalid group node ID")
		}
		if id == g.SelectedNodeID {
			selected = true
		}
	}
	if !selected {
		return errors.New("group selection is not a member")
	}
	return nil
}
