package proxypublication

import (
	"context"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxypolicy"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type RuleStore interface {
	ReadRules(context.Context, string, string) (api.NetworkProxyRules, error)
	SaveRules(context.Context, string, string, api.NetworkProxyRules, int64, int64) error
}
type Rules struct {
	store RuleStore
	auth  Authorizer
	audit Audit
}

func NewRules(store RuleStore, auth Authorizer, audit Audit) (*Rules, error) {
	if absent(store) || absent(auth) || absent(audit) {
		return nil, apperrors.ErrServiceUnavailable
	}
	return &Rules{store: store, auth: auth, audit: audit}, nil
}
func (r *Rules) Get(ctx context.Context, p identity.Principal, instance string, personal bool, targets []proxypolicy.Target) (api.NetworkProxyRules, error) {
	action := "view"
	owner := ""
	if personal {
		action = "view"
		owner = p.UserID
		if owner == "" {
			return api.NetworkProxyRules{}, apperrors.ErrUnauthorized
		}
	}
	if err := r.auth.Authorize(ctx, p, instance, action); err != nil {
		return api.NetworkProxyRules{}, err
	}
	return r.effective(ctx, instance, owner, targets)
}
func (r *Rules) readDraft(ctx context.Context, instance, owner string) (api.NetworkProxyRules, []api.NetworkProxyRule, error) {
	org, err := r.store.ReadRules(ctx, instance, "")
	if err != nil {
		return org, nil, err
	}
	out := org
	rules := append([]api.NetworkProxyRule(nil), org.Rules...)
	if owner != "" {
		draft, err := r.store.ReadRules(ctx, instance, owner)
		if err != nil {
			return draft, nil, err
		}
		out.Rules = draft.Rules
		out.PersonalRevision = draft.PersonalRevision
		rules = append(rules, draft.Rules...)
	}

	return out, rules, nil
}
func (r *Rules) effective(ctx context.Context, instance, owner string, targets []proxypolicy.Target) (api.NetworkProxyRules, error) {
	out, rules, err := r.readDraft(ctx, instance, owner)
	if err != nil {
		return out, err
	}
	return repairableRuleView(out, rules, targets), nil
}
func repairableRuleView(out api.NetworkProxyRules, rules []api.NetworkProxyRule, targets []proxypolicy.Target) api.NetworkProxyRules {
	valid, conflicts, blocked := readableRules(rules, targets)
	out.Conflicts = conflicts
	out.EffectiveRules = []api.NetworkProxyRule{}
	if blocked {
		return out
	}
	merged, err := proxypolicy.Compose(out.OrgRevision, out.PersonalRevision, valid, targets, true)
	if err != nil {
		for _, rule := range rules {
			out.Conflicts = append(out.Conflicts, api.NetworkProxyRuleConflict{RuleID: rule.RuleID, BlockingRuleID: rule.RuleID, ReasonCode: api.NetworkProxyRuleConflictReasonCodeMandatoryConflict})
		}
		return out
	}
	out.EffectiveRules = merged.EffectiveRules
	out.Conflicts = append(out.Conflicts, merged.Conflicts...)
	return out
}
func (r *Rules) Put(ctx context.Context, p identity.Principal, instance string, personal bool, input api.NetworkProxyRulesInput, targets []proxypolicy.Target) (api.NetworkProxyRules, error) {
	owner := ""
	action := "update"
	if personal {
		owner = p.UserID
		action = "update"
		if owner == "" {
			return api.NetworkProxyRules{}, apperrors.ErrUnauthorized
		}
	}
	if err := r.auth.Authorize(ctx, p, instance, action); err != nil {
		return api.NetworkProxyRules{}, err
	}
	old, _, err := r.readDraft(ctx, instance, owner)
	if err != nil {
		return old, err
	}
	if proxypolicy.CheckRevision(input.ExpectedOrgRevision, input.ExpectedPersonalRevision, old.OrgRevision, old.PersonalRevision) != nil {
		return api.NetworkProxyRules{}, apperrors.ErrConflict
	}
	for _, rule := range input.Rules {
		if personal {
			if rule.Source != "personal" {
				return api.NetworkProxyRules{}, domain.Invalid("invalid_layer", "rules")
			}
		} else if rule.Source != "organization_required" && rule.Source != "organization_default" {
			return api.NetworkProxyRules{}, domain.Invalid("invalid_layer", "rules")
		}
	}
	rules := input.Rules
	retainedConflicts := []api.NetworkProxyRuleConflict{}
	if personal {
		org, e := r.store.ReadRules(ctx, instance, "")
		if e != nil {
			return old, e
		}
		managed, conflicts, blocked := readableRules(org.Rules, targets)
		if blocked {
			return api.NetworkProxyRules{}, domain.Invalid("invalid_rules", "rules")
		}
		retainedConflicts = conflicts
		rules = append(append([]api.NetworkProxyRule(nil), managed...), rules...)
	}
	merged, err := proxypolicy.Compose(old.OrgRevision, old.PersonalRevision, rules, targets, true)
	if err != nil {
		return api.NetworkProxyRules{}, domain.Invalid("invalid_rules", "rules")
	}
	out := api.NetworkProxyRules{OrgRevision: old.OrgRevision, PersonalRevision: old.PersonalRevision, Rules: input.Rules, EffectiveRules: merged.EffectiveRules, Conflicts: append(retainedConflicts, merged.Conflicts...)}
	if personal {
		out.PersonalRevision++
	} else {
		out.OrgRevision++
	}
	if err = r.store.SaveRules(ctx, instance, owner, out, old.OrgRevision, old.PersonalRevision); err != nil {
		return api.NetworkProxyRules{}, err
	}
	if err = r.audit.RecordPublication(ctx, p, instance, "rules_updated", owner, out.OrgRevision); err != nil {
		return api.NetworkProxyRules{}, err
	}
	return out, nil
}

func readableRules(rules []api.NetworkProxyRule, targets []proxypolicy.Target) ([]api.NetworkProxyRule, []api.NetworkProxyRuleConflict, bool) {
	valid := []api.NetworkProxyRule{}
	conflicts := []api.NetworkProxyRuleConflict{}
	blocked := false
	for _, rule := range rules {
		if err := proxypolicy.ValidateRule(rule, targets, true); err != nil {
			reason := api.NetworkProxyRuleConflictReasonCodeUnsupportedMatcher
			if err.Error() == "invalid_target" {
				reason = api.NetworkProxyRuleConflictReasonCodeInvalidTarget
			}
			conflicts = append(conflicts, api.NetworkProxyRuleConflict{RuleID: rule.RuleID, BlockingRuleID: rule.RuleID, ReasonCode: reason})
			blocked = blocked || proxypolicy.IsMandatory(rule)
			continue
		}
		valid = append(valid, rule)
	}

	return valid, conflicts, blocked
}
