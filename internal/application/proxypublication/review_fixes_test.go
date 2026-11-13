package proxypublication

import (
	"context"
	"encoding/json"
	"errors"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxypolicy"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"strings"
	"testing"
	"time"
)

type disclosureEngine struct{}

func (disclosureEngine) Export(ir domain.IR) (string, error) {
	return disclosureEngine{}.ExportDirect(ir)
}
func (disclosureEngine) ExportDirect(ir domain.IR) (string, error) {
	b, err := json.Marshal(ir.Nodes) // #nosec G117 -- Disclosure regression fixture intentionally serializes synthetic node secrets to test redaction.
	return string(b), err
}
func directScopeFixture(t *testing.T) (*Bundles, *bundleFake) {
	t.Helper()
	b, f, _ := ingressFixture()
	b.keys = testKeys(t)
	f.record.Public.DeliveryMode = "direct_subscription"
	f.record.Scope.NodeIDs = []string{"node-a"}
	f.record.SourceNodeIDs = []string{"node-a"}
	f.record.Nodes = []string{"node-a", "node-b"}
	f.record.Groups = []api.NetworkProxyGroup{{GroupID: "group", Label: "Group", Type: "select", NodeIDs: []string{"node-a", "node-b"}, SelectedNodeID: "node-b"}}
	f.record.Rules = []api.NetworkProxyRule{{RuleID: "optional-b", Source: "organization_default", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "node", TargetID: "node-b"}}, {RuleID: "mandatory-deny", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "domain", Value: "blocked.example"}, Action: api.NetworkProxyRuleAction{Type: "reject"}}}
	ir := domain.IR{Nodes: []api.NetworkProxyNode{{NodeID: "node-a", Protocol: "http", Server: "allowed-a.example", Port: 443, Secret: "allowed-a-secret"}, {NodeID: "node-b", Protocol: "http", Server: "hidden-b.example", Port: 443, Secret: "hidden-b-secret"}}}
	raw, _ := json.Marshal(ir)
	f.record.EncryptedIR, _ = secretcrypto.EncryptStringWithKeyring(b.keys, string(raw))
	b.engine = disclosureEngine{}
	b.endpoint = endpointFake{value: domain.EndpointContext{UserID: "user-1", DeviceID: "device-1", RuntimeID: "endpoint-1", CredentialID: "endpoint-cert", ProfileID: "profile-1", BundleID: "bundle-1", DeliveryMode: "direct_subscription", OrgRevision: 1, SourceRevision: 1, ExpiresAt: b.now().Add(time.Minute), ConfigurationValidUntil: b.now().Add(time.Minute)}}
	return b, f
}
func TestDirectProjectionExcludesUnauthorizedSourceNode(t *testing.T) {
	b, f := directScopeFixture(t)
	out, err := b.DirectProjection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "profile-1", "boot-1")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	for _, hidden := range []string{"hidden-b.example", "hidden-b-secret", "node-b"} {
		if strings.Contains(string(raw), hidden) {
			t.Fatalf("unauthorized source disclosed: %s", hidden)
		}
	}
	if !strings.Contains(out.Content, "allowed-a-secret") || len(out.Groups) != 1 || out.Groups[0].SelectedNodeID != "" || len(out.Rules) != 2 || out.Rules[0].Action.Type != "reject" || out.Rules[1].Action.Type != "reject" {
		t.Fatal("scoped topology/mandatory rules changed", out)
	}
	if len(f.credential.NodeIDs) != 1 || f.credential.NodeIDs[0] != "node-a" {
		t.Fatal("receipt marker exceeded scope")
	}
}
func TestDirectProjectionRejectsRevokedMandatoryRoute(t *testing.T) {
	b, f := directScopeFixture(t)
	f.record.Rules = append(f.record.Rules, api.NetworkProxyRule{RuleID: "must-b", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "domain", Value: "required.example"}, Action: api.NetworkProxyRuleAction{Type: "node", TargetID: "node-b"}})
	out, err := b.DirectProjection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "profile-1", "boot-1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rule := range out.Rules {
		if rule.RuleID == "must-b" {
			found = rule.Action.Type == "reject" && rule.Action.TargetID == ""
		}
	}
	if !found || strings.Contains(out.Content, "hidden-b") {
		t.Fatal("mandatory revoked route was not narrowed to reject")
	}
}

type repairRuleStore struct {
	org, draft api.NetworkProxyRules
	saves      int
}

func (s *repairRuleStore) ReadRules(_ context.Context, _ string, owner string) (api.NetworkProxyRules, error) {
	if owner == "" {
		return s.org, nil
	}
	return s.draft, nil
}
func (s *repairRuleStore) SaveRules(_ context.Context, _ string, owner string, out api.NetworkProxyRules, org, personal int64) error {
	if org != s.org.OrgRevision || (owner != "" && personal != s.draft.PersonalRevision) {
		return apperrors.ErrConflict
	}
	if owner == "" {
		s.org = out
	} else {
		s.draft = out
	}
	s.saves++
	return nil
}
func repairFixture(t *testing.T) (*Rules, *repairRuleStore) {
	t.Helper()
	store := &repairRuleStore{org: api.NetworkProxyRules{OrgRevision: 3, Rules: []api.NetworkProxyRule{{RuleID: "required", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "reject"}}}}, draft: api.NetworkProxyRules{PersonalRevision: 5, Rules: []api.NetworkProxyRule{{RuleID: "deleted-a", Source: "personal", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "domain", Value: "public.example"}, Action: api.NetworkProxyRuleAction{Type: "node", TargetID: "node-a"}}}}}
	service, err := NewRules(store, authFake{}, auditFake{})
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}
func TestDeletedRuleTargetRemainsVisibleAndDraftCanBeRepaired(t *testing.T) {
	service, store := repairFixture(t)
	p := identity.Principal{UserID: "user-1"}
	out, err := service.Get(context.Background(), p, "instance", true, []proxypolicy.Target{})
	if err != nil || len(out.Rules) != 1 || out.Rules[0].RuleID != "deleted-a" || len(out.Conflicts) != 1 || out.Conflicts[0].ReasonCode != "invalid_target" || len(out.EffectiveRules) != 1 || out.EffectiveRules[0].RuleID != "required" {
		t.Fatal("invalid draft not repairable", out, err)
	}
	input := api.NetworkProxyRulesInput{ExpectedOrgRevision: 3, ExpectedPersonalRevision: 5, Rules: []api.NetworkProxyRule{}}
	fixed, err := service.Put(context.Background(), p, "instance", true, input, nil)
	if err != nil || len(fixed.Rules) != 0 || fixed.PersonalRevision != 6 || len(store.org.Rules) != 1 {
		t.Fatal("empty repair rejected or mandatory lost", err)
	}
	if _, err = service.Put(context.Background(), p, "instance", true, input, nil); !errors.Is(err, apperrors.ErrConflict) || store.saves != 1 {
		t.Fatal("stale repair bypassed CAS", err)
	}
}
func TestInvalidMandatoryRuleCannotBeHiddenByPersonalRepair(t *testing.T) {
	service, store := repairFixture(t)
	store.org.Rules[0].Action = api.NetworkProxyRuleAction{Type: "node", TargetID: "deleted-required"}
	out, err := service.Get(context.Background(), identity.Principal{UserID: "user-1"}, "instance", true, nil)
	if err != nil || len(out.EffectiveRules) != 0 || len(out.Conflicts) == 0 {
		t.Fatal("broken mandatory rule became permissive", err)
	}
	if _, err = service.Put(context.Background(), identity.Principal{UserID: "user-1"}, "instance", true, api.NetworkProxyRulesInput{ExpectedOrgRevision: 3, ExpectedPersonalRevision: 5, Rules: []api.NetworkProxyRule{}}, nil); err == nil || store.saves != 0 {
		t.Fatal("personal repair bypassed mandatory policy")
	}
}

func TestManagedRequiredRuleStillMasksRepairedPersonalDraft(t *testing.T) {
	service, store := repairFixture(t)
	out, err := service.Put(context.Background(), identity.Principal{UserID: "user-1"}, "instance", true, api.NetworkProxyRulesInput{ExpectedOrgRevision: 3, ExpectedPersonalRevision: 5, Rules: []api.NetworkProxyRule{{RuleID: "personal-direct", Source: "personal", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "domain", Value: "public.example"}, Action: api.NetworkProxyRuleAction{Type: "direct"}}}}, nil)
	if err != nil || len(out.Rules) != 1 || len(out.EffectiveRules) != 1 || out.EffectiveRules[0].RuleID != "required" || len(out.Conflicts) != 1 || out.Conflicts[0].ReasonCode != "masked_by_required" || len(store.org.Rules) != 1 {
		t.Fatal("personal repair weakened mandatory policy", out, err)
	}
}
func TestDirectScopeNarrowsMandatoryGroupWithSharedPolicy(t *testing.T) {
	b, f := directScopeFixture(t)
	f.record.Rules = []api.NetworkProxyRule{{RuleID: "required-group", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "group", TargetID: "group"}}}
	out, err := b.DirectProjection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "profile-1", "boot-1")
	if err != nil || len(out.Groups) != 1 || len(out.Groups[0].NodeIDs) != 1 || out.Groups[0].NodeIDs[0] != "node-a" || len(out.Rules) != 1 || out.Rules[0].Action.TargetID != "group" {
		t.Fatal("mandatory group did not preserve its allowed subset", err)
	}
}

func TestDeletedOrganizationDefaultDoesNotBlockPersonalRepair(t *testing.T) {
	service, store := repairFixture(t)
	store.org.Rules = append(store.org.Rules, api.NetworkProxyRule{RuleID: "old-default", Source: "organization_default", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "node", TargetID: "node-a"}})
	out, err := service.Put(context.Background(), identity.Principal{UserID: "user-1"}, "instance", true, api.NetworkProxyRulesInput{ExpectedOrgRevision: 3, ExpectedPersonalRevision: 5, Rules: []api.NetworkProxyRule{}}, nil)
	if err != nil || len(out.EffectiveRules) != 1 || out.EffectiveRules[0].RuleID != "required" || len(out.Conflicts) != 1 || out.Conflicts[0].RuleID != "old-default" || len(store.org.Rules) != 2 {
		t.Fatal("old default blocked personal repair or was erased", out, err)
	}
}
