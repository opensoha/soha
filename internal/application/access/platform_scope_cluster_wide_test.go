package access

import (
	"context"
	"slices"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/policy"
)

func TestClusterWideNamespaceScopeDoesNotBorrowAnotherActionsGrant(t *testing.T) {
	request := domainaccess.Request{
		RequireClusterWideNamespace: true,
		Cluster:                     domainaccess.ClusterAttributes{ClusterID: "cluster"},
		Resource:                    domainaccess.ResourceAttributes{Kind: "HelmRelease"},
		Action:                      domainaccess.ActionUpdate,
	}
	roles := map[string][]domainaccess.Action{"viewer": {domainaccess.ActionView}, "writer": {domainaccess.ActionUpdate}}
	decision := domainaccess.Decision{Allowed: true, AllowedActions: []domainaccess.Action{domainaccess.ActionUpdate}}
	grants := []domainscopegrant.Record{
		{ScopeType: domainscopegrant.ScopeTypePlatform, Role: "viewer"},
		{ScopeType: domainscopegrant.ScopeTypePlatform, Role: "writer", Namespaces: []string{"team-a"}},
	}
	limited := evaluatePlatformScopeGrants(request, decision, roles, grants, nil, nil)
	if limited.Allowed {
		t.Fatalf("namespace-scoped write borrowed the cluster-wide read grant: %+v", limited)
	}
	grants[1].Namespaces = nil
	wide := evaluatePlatformScopeGrants(request, decision, roles, grants, nil, nil)
	if !wide.Allowed || wide.ResourceScope == nil || len(wide.ResourceScope.Namespaces) != 0 {
		t.Fatalf("explicitly unrestricted action was denied: %+v", wide)
	}
}

func TestClusterWideNamespacePolicyRejectsLabelRestrictedDeny(t *testing.T) {
	request := domainaccess.Request{
		RequireClusterWideNamespace: true,
		Cluster:                     domainaccess.ClusterAttributes{ClusterID: "cluster"},
		Resource:                    domainaccess.ResourceAttributes{Kind: "HelmRelease"},
		Action:                      domainaccess.ActionUpdate,
	}
	policies := []domainaccess.Policy{
		{Effect: domainaccess.EffectAllow, Priority: 10},
		{Effect: domainaccess.EffectDeny, Priority: 20, Actions: []domainaccess.Action{domainaccess.ActionUpdate}, Namespaces: domainaccess.NamespaceMatcher{Labels: map[string][]string{"restricted": {"true"}}}},
	}
	engine := policy.NewEngine()
	denied, err := engine.Evaluate(context.Background(), request, policies)
	if err != nil || denied.Allowed || denied.Reason == "" {
		t.Fatalf("cluster-wide write did not account for label-based deny: %+v, %v", denied, err)
	}
	request.RequireClusterWideNamespace = false
	request.Namespace = domainaccess.NamespaceAttributes{Namespace: "team-a", Labels: map[string]string{"restricted": "false"}}
	allowed, err := engine.Evaluate(context.Background(), request, policies)
	if err != nil || !allowed.Allowed {
		t.Fatalf("ordinary policy decision changed: %+v, %v", allowed, err)
	}
}

func TestClusterWideNamespaceScopeIgnoresOnlyRedundantPositiveSelectors(t *testing.T) {
	request := domainaccess.Request{
		RequireClusterWideNamespace: true,
		Cluster:                     domainaccess.ClusterAttributes{ClusterID: "cluster"},
		Resource:                    domainaccess.ResourceAttributes{Kind: "HelmRelease"},
		Action:                      domainaccess.ActionUpdate,
	}
	roles := map[string][]domainaccess.Action{"writer": {domainaccess.ActionUpdate}}
	grants := []domainscopegrant.Record{
		{ScopeType: domainscopegrant.ScopeTypePlatform, Role: "writer", NamespaceSelector: "team=dev"},
		{ScopeType: domainscopegrant.ScopeTypePlatform, Role: "writer"},
		{ScopeType: domainscopegrant.ScopeTypePlatform, Role: "writer", Effect: "deny", NamespaceSelector: "blocked=true"},
	}
	decision := domainaccess.Decision{Allowed: true, AllowedActions: []domainaccess.Action{domainaccess.ActionUpdate}}
	got := evaluatePlatformScopeGrants(request, decision, roles, grants, nil, nil)
	if !got.Allowed || got.ResourceScope == nil || len(got.ResourceScope.NamespaceSelectors) != 0 ||
		got.ResourceScope.LabelSelector != "" || !slices.Equal(got.ResourceScope.ExcludedNamespaceSelectors, []string{"blocked=true"}) {
		t.Fatalf("strict namespace scope = %+v", got.ResourceScope)
	}
	decision.ResourceScope = &domainaccess.ResourceScope{NamespaceSelectors: []string{"policy=applies"}}
	got = evaluatePlatformScopeGrants(request, decision, roles, grants, nil, nil)
	if got.ResourceScope == nil || !slices.Equal(got.ResourceScope.NamespaceSelectors, []string{"policy=applies"}) {
		t.Fatalf("strict scope discarded policy selector: %+v", got.ResourceScope)
	}
}
