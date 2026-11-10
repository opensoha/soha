package resource

import (
	"context"
	"errors"
	"fmt"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/policy"
)

type graphDirectStub struct{ called bool }

func (s *graphDirectStub) GetResourceGraph(context.Context, string, string, string, string) (domainresource.ResourceGraph, error) {
	s.called = true
	return graphPermissionFixture(), nil
}

type graphAgentStub struct {
	GenericResourceAgent
	called bool
}

func (s *graphAgentStub) GetResourceGraph(context.Context, string, string, string) (domainresource.ResourceGraph, error) {
	s.called = true
	return graphPermissionFixture(), nil
}
func graphPermissionFixture() domainresource.ResourceGraph {
	return domainresource.ResourceGraph{ClusterID: "agent-local", RootID: "pod", Nodes: []domainresource.ResourceGraphNode{
		{ID: "pod", Resource: domainresource.ResourceRef{ClusterID: "agent-local", APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: "api"}},
		{ID: "secret", Resource: domainresource.ResourceRef{ClusterID: "foreign-cluster", APIVersion: "v1", Kind: "Secret", Namespace: "team-a", Name: "secret"}},
	}, Edges: []domainresource.ResourceGraphEdge{{ID: "ref", SourceID: "pod", TargetID: "secret", Relation: "uses-secret"}}, Evidence: []domainresource.ResourceEvidence{{ID: "pod-event", ResourceID: "pod"}, {ID: "secret-event", ResourceID: "secret"}}}
}

func TestResourceGraphRejectsUnexpectedAgentRoot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*domainresource.ResourceGraph)
	}{
		{"missing", func(g *domainresource.ResourceGraph) { g.RootID = "" }},
		{"wrong name", func(g *domainresource.ResourceGraph) { g.Nodes[0].Resource.Name = "other" }},
		{"wrong namespace", func(g *domainresource.ResourceGraph) { g.Nodes[0].Resource.Namespace = "other" }},
		{"wrong api", func(g *domainresource.ResourceGraph) { g.Nodes[0].Resource.APIVersion = "other.example/v1" }},
		{"wrong scope", func(g *domainresource.ResourceGraph) {
			g.Nodes[0].Resource.ScopeMode = domainresource.ResourceScopeModeCluster
		}},
		{"unresolved", func(g *domainresource.ResourceGraph) { g.Nodes[0].Status = "unresolved" }},
		{"duplicate", func(g *domainresource.ResourceGraph) { g.Nodes = append(g.Nodes, g.Nodes[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &configurationGraphAgentSource{graph: graphPermissionFixture()}
			tc.mutate(&source.graph)
			service := &GenericResources{resourceAccess: &resourceAccess{
				resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: domaincluster.ConnectionModeAgent}}},
				authorizer: exactPermissionAuthorizer{"platform.pods.view": true}, audit: discardAuditRecorder{},
			}, agent: func(domaincluster.Connection) (GenericResourceAgent, error) { return source, nil }}
			graph, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "c", "team-a", "Pod", "api")
			if !errors.Is(err, apperrors.ErrClusterUnready) || !source.called || len(graph.Nodes) != 0 {
				t.Fatalf("unexpected Agent root was accepted: %+v %v", graph, err)
			}
		})
	}
}

type graphPolicyRepository []domainaccess.Policy

func (r graphPolicyRepository) ListPolicies(context.Context) ([]domainaccess.Policy, error) {
	return r, nil
}

func (r graphPolicyRepository) ListRoleCapabilities(context.Context) (map[string][]domainaccess.Action, error) {
	return nil, nil
}

func (r graphPolicyRepository) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"reader": {appaccess.PermPlatformPodsView}}, nil
}

func TestResourceGraphResolvesLabelsBeforeAcceptingGenericPolicy(t *testing.T) {
	t.Cleanup(func() { appaccess.SetRolePermissionMatrix(nil) })
	for _, tc := range []struct {
		name, denyNamespace string
		resolver            *namespaceLabelResolverStub
		allowed             bool
	}{
		{"label-specific deny precedes allow", "team-a", &namespaceLabelResolverStub{labels: map[string]string{"restricted": "true"}}, false},
		{"observed empty labels", "team-a", &namespaceLabelResolverStub{}, true},
		{"required labels unavailable", "team-a", nil, false},
		{"unrelated label policy", "other", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := graphPolicyRepository{
				{Effect: domainaccess.EffectAllow, Priority: 10, Actions: []domainaccess.Action{domainaccess.ActionView}},
				{Effect: domainaccess.EffectDeny, Priority: 20, Actions: []domainaccess.Action{domainaccess.ActionView}, Namespaces: domainaccess.NamespaceMatcher{Names: []string{tc.denyNamespace}, Labels: map[string][]string{"restricted": {"true"}}}},
			}
			authorizer := appaccess.New(policy.NewEngine(), repo, nil, nil)
			authorizer.SetPermissionResolver(appaccess.NewPermissionResolver(repo))
			direct := &graphDirectStub{}
			service := &GenericResources{resourceAccess: &resourceAccess{
				resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "c"}}},
				authorizer: authorizer, audit: discardAuditRecorder{},
			}, resourceGraph: direct}
			if tc.resolver != nil {
				service.namespaceLabels = tc.resolver
			}
			principal := domainidentity.Principal{Roles: []string{"reader"}}
			graph, err := service.GetResourceGraph(context.Background(), principal, "c", "team-a", "Pod", "api")
			if tc.allowed {
				if err != nil || !direct.called || len(graph.Nodes) != 1 {
					t.Fatalf("authorized Graph unavailable: %+v %v", graph, err)
				}
			} else if !errors.Is(err, apperrors.ErrAccessDenied) || direct.called {
				t.Fatalf("Graph reached provider without label authorization: %+v %v", graph, err)
			}
			if tc.resolver != nil && tc.resolver.calls == 0 {
				t.Fatal("relevant policy did not resolve namespace labels")
			}
		})
	}
}

func TestResourceGraphAuthorizesEachResourceName(t *testing.T) {
	t.Cleanup(func() { appaccess.SetRolePermissionMatrix(nil) })
	privateLabel := map[string][]string{"restricted": {"true"}}
	for _, tc := range []struct {
		name     string
		matcher  domainaccess.ResourceMatcher
		wantRoot bool
		wantPeer bool
	}{
		{"root name deny", domainaccess.ResourceMatcher{Names: []string{"api"}}, false, false},
		{"neighbor name deny", domainaccess.ResourceMatcher{Names: []string{"private"}}, true, false},
		{"root label deny", domainaccess.ResourceMatcher{Labels: privateLabel}, false, false},
		{"neighbor name and label deny", domainaccess.ResourceMatcher{Names: []string{"private"}, Labels: privateLabel}, true, false},
		{"unrelated name and label deny", domainaccess.ResourceMatcher{Names: []string{"other"}, Labels: privateLabel}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			matcher := tc.matcher
			matcher.Kinds = []string{"Pod"}
			repo := graphPolicyRepository{
				{Effect: domainaccess.EffectAllow, Priority: 10, Actions: []domainaccess.Action{domainaccess.ActionView}},
				{Effect: domainaccess.EffectDeny, Priority: 20, Actions: []domainaccess.Action{domainaccess.ActionView}, Resources: matcher},
			}
			authorizer := appaccess.New(policy.NewEngine(), repo, nil, nil)
			authorizer.SetPermissionResolver(appaccess.NewPermissionResolver(repo))
			graph := graphPermissionFixture()
			other := graph.Nodes[0]
			other.ID, other.Resource.Name = "neighbor", "private"
			graph.Nodes = append(graph.Nodes, other)
			graph.Edges = append(graph.Edges, domainresource.ResourceGraphEdge{ID: "named-neighbor", SourceID: graph.RootID, TargetID: other.ID, Relation: "owns"})
			direct := &graphViewDirectSource{graph: graph}
			service := &GenericResources{resourceAccess: &resourceAccess{
				resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "c"}}},
				authorizer: authorizer, audit: discardAuditRecorder{},
			}, resourceGraph: direct}
			principal := domainidentity.Principal{Roles: []string{"reader"}}
			got, err := service.GetResourceGraph(context.Background(), principal, "c", "team-a", "Pod", "api")
			if !tc.wantRoot {
				if !errors.Is(err, apperrors.ErrAccessDenied) || direct.called {
					t.Fatalf("named root denial reached provider: %+v %v", got, err)
				}
				return
			}
			wantNodes, wantEdges := 1, 0
			if tc.wantPeer {
				wantNodes, wantEdges = 2, 1
			}
			root, hasRoot := resourceGraphRootNode(got)
			if err != nil || !direct.called || !hasRoot || root.Resource.Name != "api" || len(got.Nodes) != wantNodes || len(got.Edges) != wantEdges {
				t.Fatalf("unexpected Graph policy result: %+v %v", got, err)
			}
		})
	}
}
func TestResourceGraphAuthorizationAndSelectedClusterIdentity(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		for _, allowRoot := range []bool{false, true} {
			for _, allowSecret := range []bool{false, true} {
				t.Run(string(mode)+map[bool]string{true: " allow", false: " deny"}[allowRoot]+map[bool]string{true: " secret", false: " hidden"}[allowSecret], func(t *testing.T) {
					direct := &graphDirectStub{}
					agent := &graphAgentStub{}
					access := &resourceAccess{audit: discardAuditRecorder{}, resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: mode}}}, authorizer: exactPermissionAuthorizer{"platform.pods.view": allowRoot, "platform.configuration.secrets.view": allowSecret}}
					service := &GenericResources{resourceAccess: access, resourceGraph: direct, agent: func(domaincluster.Connection) (GenericResourceAgent, error) { return agent, nil }}
					graph, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{UserID: "reader"}, "cluster-a", "team-a", "Pod", "api")
					if !allowRoot {
						if !errors.Is(err, apperrors.ErrAccessDenied) || direct.called || agent.called {
							t.Fatalf("denial reached backend: %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if graph.ClusterID != "cluster-a" {
						t.Fatal("Agent local identity escaped")
					}
					for _, node := range graph.Nodes {
						if node.Resource.ClusterID != "cluster-a" {
							t.Fatal("foreign node identity escaped")
						}
					}
					if allowSecret {
						if len(graph.Nodes) != 2 || len(graph.Edges) != 1 || len(graph.Evidence) != 2 {
							t.Fatalf("allowed graph changed: %+v", graph)
						}
					} else if len(graph.Nodes) != 1 || len(graph.Edges) != 0 || len(graph.Evidence) != 1 || len(graph.Warnings) != 1 {
						t.Fatalf("unauthorized relation/evidence leaked: %+v", graph)
					}
				})
			}
		}
	}
}

type graphViewDecisionAuthorizer struct {
	requests []domainaccess.Request
	denyRoot bool
}

func (a *graphViewDecisionAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	return domainaccess.Decision{Allowed: !a.denyRoot && request.Namespace.Namespace != "hidden", Reason: "scope policy denied"}, nil
}

type graphViewAuditRecorder struct{ entries []domainaudit.Entry }

func (a *graphViewAuditRecorder) Record(_ context.Context, entry domainaudit.Entry) error {
	a.entries = append(a.entries, entry)
	return nil
}

type graphViewDirectSource struct {
	graph  domainresource.ResourceGraph
	called bool
}

func (s *graphViewDirectSource) GetResourceGraph(context.Context, string, string, string, string) (domainresource.ResourceGraph, error) {
	s.called = true
	return s.graph, nil
}

func TestResourceGraphOnlyEvaluatesViewDecisionsAndAuditsDenials(t *testing.T) {
	for _, denyRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("rootDenied=%t", denyRoot), func(t *testing.T) {
			graph := domainresource.ResourceGraph{RootID: "root", Nodes: []domainresource.ResourceGraphNode{{ID: "root", Resource: domainresource.ResourceRef{APIVersion: "v1", Kind: "Pod", Name: "api", Namespace: "team-a"}}}}
			for i := 0; i < 24; i++ {
				id := fmt.Sprintf("related-%02d", i)
				graph.Nodes = append(graph.Nodes, domainresource.ResourceGraphNode{ID: id, Resource: domainresource.ResourceRef{APIVersion: "v1", Kind: "Pod", Name: id, Namespace: "team-a"}})
				graph.Edges = append(graph.Edges, domainresource.ResourceGraphEdge{ID: id, SourceID: "root", TargetID: id, Relation: "owns"})
			}
			graph.Nodes = append(graph.Nodes, domainresource.ResourceGraphNode{ID: "denied", Resource: domainresource.ResourceRef{APIVersion: "v1", Kind: "Secret", Name: "private", Namespace: "hidden"}})
			graph.Edges = append(graph.Edges, domainresource.ResourceGraphEdge{ID: "denied-edge", SourceID: "root", TargetID: "denied", Relation: "uses-secret"})
			graph.Evidence = []domainresource.ResourceEvidence{{ID: "allowed-event", ResourceID: "root"}, {ID: "denied-event", ResourceID: "denied"}}
			source := &graphViewDirectSource{graph: graph}
			authorizer := &graphViewDecisionAuthorizer{denyRoot: denyRoot}
			audit := &graphViewAuditRecorder{}
			service := &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, authorizer: authorizer, audit: audit}, resourceGraph: source}
			result, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{UserID: "reader"}, "cluster-a", "team-a", "Pod", "api")
			wantCalls := len(graph.Nodes) + 1
			if denyRoot {
				wantCalls = 1
				if !errors.Is(err, apperrors.ErrAccessDenied) || source.called {
					t.Fatalf("denied root reached source: %v", err)
				}
			} else {
				if err != nil || len(result.Nodes) != 25 || len(result.Edges) != 24 || len(result.Evidence) != 1 || result.Evidence[0].ID != "allowed-event" || len(result.Warnings) != 1 {
					t.Fatalf("scope filtering changed: %+v err=%v", result, err)
				}
			}
			if len(authorizer.requests) != wantCalls {
				t.Fatalf("unnecessary permission work: got=%d want=%d", len(authorizer.requests), wantCalls)
			}
			for _, request := range authorizer.requests {
				if request.Action != domainaccess.ActionView {
					t.Fatalf("Graph queried unrelated action %s", request.Action)
				}
			}
			assertGraphViewDenialAudit(t, audit.entries, denyRoot)
		})
	}
}

func assertGraphViewDenialAudit(t *testing.T, entries []domainaudit.Entry, denyRoot bool) {
	t.Helper()
	denials := 0
	for _, entry := range entries {
		if entry.Result != "deny" {
			continue
		}
		denials++
		if entry.Action != string(domainaccess.ActionView) || entry.ClusterID != "cluster-a" {
			t.Fatalf("deny audit changed: %+v", entry)
		}
		if denyRoot && entry.ResourceKind != "Pod" {
			t.Fatalf("wrong root deny audit: %+v", entry)
		}
		if !denyRoot && (entry.ResourceKind != "Secret" || entry.Namespace != "hidden") {
			t.Fatalf("wrong node deny audit: %+v", entry)
		}
	}
	if denials != 1 {
		t.Fatalf("deny audit missing/duplicated: %+v", entries)
	}
}
