package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func graphHPACustomFixture(kind string) domainresource.ResourceGraph {
	graph := graphCRDFixture(kind, 1)
	graph.Nodes[0].Resource = domainresource.ResourceRef{ClusterID: "cluster-a", APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler", Name: "hpa", Namespace: "team", UID: "hpa-uid"}
	graph.Nodes[1].Resource.UID = ""
	graph.Nodes[1].Resource.ScopeMode = domainresource.ResourceScopeModeNamespace
	graph.Edges = []domainresource.ResourceGraphEdge{{ID: "scale-declaration", SourceID: "root", TargetID: graph.Nodes[1].ID, Relation: "scales"}}
	graph.Evidence = []domainresource.ResourceEvidence{{ID: "hpa-event", ResourceID: "root"}, {ID: "custom-event", ResourceID: graph.Nodes[1].ID}}
	return graph
}

func graphHPACustomPermissions() exactPermissionAuthorizer {
	return exactPermissionAuthorizer{"platform.configuration.horizontal-pod-autoscalers.view": true, appaccess.PermPlatformExtensionsView: true}
}

func graphHPACustomRun(service *GenericResources) (domainresource.ResourceGraph, error) {
	return service.GetResourceGraph(context.Background(), domainidentity.Principal{UserID: "reader"}, "cluster-a", "team", "HorizontalPodAutoscaler", "hpa")
}

func TestResourceGraphHPACustomTargetObservedInBothModes(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		for _, kind := range []string{"Widget", "Pod"} {
			t.Run(string(mode)+"/"+kind, func(t *testing.T) {
				state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView(kind)}, review: []string{"view"}}
				graph, err := graphHPACustomRun(graphCRDService(mode, graphHPACustomFixture(kind), state, graphHPACustomPermissions(), true))
				if err != nil || state.reads != 1 || state.lists != 1 || len(graph.Nodes) != 3 || len(graph.Edges) != 2 || len(graph.Evidence) != 2 {
					t.Fatalf("custom target not observed: graph=%+v reads=%d lists=%d err=%v", graph, state.reads, state.lists, err)
				}
				assertGraphHPACustomScaleTarget(t, graph, kind)
				if mode == domaincluster.ConnectionModeAgent && state.reviews != 1 {
					t.Fatal("Agent exact named-get RBAC review skipped")
				}
				encoded, _ := json.Marshal(graph)
				if strings.Contains(string(encoded), "secret-payload") {
					t.Fatal("custom spec/data escaped metadata projection")
				}
			})
		}
	}
}

func assertGraphHPACustomScaleTarget(t *testing.T, graph domainresource.ResourceGraph, kind string) {
	t.Helper()
	foundTarget, foundScale := false, false
	for _, node := range graph.Nodes {
		if node.ID == "custom-owner-00" {
			foundTarget = node.Resource.UID == "owner-00-uid" &&
				node.Resource.APIVersion == "example.io/v1" &&
				node.Resource.Kind == kind &&
				node.Resource.Namespace == "team" &&
				node.Resource.ScopeMode == domainresource.ResourceScopeModeNamespace &&
				node.Status != "unresolved"
		}
	}
	for _, edge := range graph.Edges {
		if edge.ID == "scale-declaration" && edge.Relation == "scales" {
			foundScale = true
		}
	}
	if !foundTarget || !foundScale {
		t.Fatalf("declared scale identity or edge lost: %+v", graph)
	}
}

func TestResourceGraphHPACustomTargetDenialsAndIdentityBoundaries(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		for _, tc := range []struct {
			name                       string
			mutate                     func(*domainresource.ResourceGraph, *graphCRDObservation)
			denyCustom, denyDefinition bool
			wantReads                  int
			rejectAgentRoot            bool
		}{
			{name: "custom permission denied", denyCustom: true},
			{name: "definition permission denied", denyDefinition: true},
			{name: "other group", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) { s.views[0].Group = "other.io" }},
			{name: "unserved version", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) { s.views[0].Versions = []string{"v2"} }},
			{name: "other kind", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) { s.views[0].Kind = "Other" }},
			{name: "missing plural", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) { s.views[0].Plural = "" }},
			{name: "ambiguous plural", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) {
				other := s.views[0]
				other.Name, other.Plural = "alternatives.example.io", "alternatives"
				s.views = append(s.views, other)
			}},
			{name: "duplicate definition", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) { s.views = append(s.views, s.views[0]) }},
			{name: "cluster scoped target", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) { s.views[0].Scope = "Cluster" }},
			{name: "other namespace", mutate: func(g *domainresource.ResourceGraph, _ *graphCRDObservation) { g.Nodes[1].Resource.Namespace = "other" }},
			{name: "invalid target scope", mutate: func(g *domainresource.ResourceGraph, _ *graphCRDObservation) {
				g.Nodes[1].Resource.ScopeMode = domainresource.ResourceScopeModeCluster
			}},
			{name: "non-HPA source", mutate: func(g *domainresource.ResourceGraph, _ *graphCRDObservation) {
				g.Nodes[0].Resource.Kind, g.Nodes[0].Resource.APIVersion = "Pod", "v1"
			}, rejectAgentRoot: true},
			{name: "custom HPA source", mutate: func(g *domainresource.ResourceGraph, _ *graphCRDObservation) {
				g.Nodes[0].Resource.APIVersion = "example.io/v1"
			}, rejectAgentRoot: true},
			{name: "missing owner UID remains rejected", mutate: func(g *domainresource.ResourceGraph, _ *graphCRDObservation) {
				g.Edges[0].Relation, g.Edges[0].SourceID, g.Edges[0].TargetID = "controls", g.Nodes[1].ID, "root"
			}},
			{name: "declared UID replaced", mutate: func(g *domainresource.ResourceGraph, _ *graphCRDObservation) {
				g.Nodes[1].Resource.UID = "previous-uid"
			}, wantReads: 1},
			{name: "returned group mismatch", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) {
				s.content = `{"apiVersion":"other.io/v1","kind":"Widget","metadata":{"name":"owner-00","namespace":"team","uid":"actual"}}`
			}, wantReads: 1},
			{name: "returned version mismatch", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) {
				s.content = `{"apiVersion":"example.io/v2","kind":"Widget","metadata":{"name":"owner-00","namespace":"team","uid":"actual"}}`
			}, wantReads: 1},
			{name: "returned kind mismatch", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) {
				s.content = `{"apiVersion":"example.io/v1","kind":"Other","metadata":{"name":"owner-00","namespace":"team","uid":"actual"}}`
			}, wantReads: 1},
			{name: "returned name mismatch", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) {
				s.content = `{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"other","namespace":"team","uid":"actual"}}`
			}, wantReads: 1},
			{name: "returned namespace mismatch", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) {
				s.content = `{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"owner-00","namespace":"other","uid":"actual"}}`
			}, wantReads: 1},
			{name: "returned UID absent", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) {
				s.content = `{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"owner-00","namespace":"team"}}`
			}, wantReads: 1},
			{name: "named get denied", mutate: func(_ *domainresource.ResourceGraph, s *graphCRDObservation) {
				s.readError = errors.New("private provider failure")
			}, wantReads: 1},
		} {
			t.Run(string(mode)+"/"+tc.name, func(t *testing.T) {
				fixture := graphHPACustomFixture("Widget")
				state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView("Widget")}, review: []string{"view"}}
				if tc.mutate != nil {
					tc.mutate(&fixture, state)
				}
				permissions := graphHPACustomPermissions()
				if tc.denyDefinition {
					delete(permissions, appaccess.PermPlatformExtensionsView)
				}
				authorizer := &graphCRDScopedAuthorizer{permissions: permissions, denyCustom: tc.denyCustom}
				graph, err := graphHPACustomRun(graphCRDService(mode, fixture, state, authorizer, true))
				if tc.rejectAgentRoot && mode == domaincluster.ConnectionModeAgent {
					if !errors.Is(err, apperrors.ErrClusterUnready) || state.reads != 0 || state.lists != 0 || len(graph.Nodes) != 0 {
						t.Fatalf("unexpected Agent root reached enrichment: graph=%+v reads=%d lists=%d err=%v", graph, state.reads, state.lists, err)
					}
					return
				}
				if err != nil || state.reads != tc.wantReads {
					t.Fatalf("invalid target was observed: graph=%+v reads=%d want=%d err=%v", graph, state.reads, tc.wantReads, err)
				}
				for _, node := range graph.Nodes {
					if node.ID == "custom-owner-00" {
						t.Fatalf("unauthorized or ambiguous target retained: %+v", graph)
					}
				}
				for _, edge := range graph.Edges {
					if edge.TargetID == "custom-owner-00" || edge.SourceID == "custom-owner-00" {
						t.Fatal("denied target edge retained")
					}
				}
				for _, evidence := range graph.Evidence {
					if evidence.ResourceID == "custom-owner-00" {
						t.Fatal("denied target evidence retained")
					}
				}
			})
		}
	}
}

func TestResourceGraphHPACustomTargetsShareObservationBudget(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		t.Run(string(mode), func(t *testing.T) {
			fixture := graphCRDFixture("Widget", 20)
			fixture.Nodes[0] = graphHPACustomFixture("Widget").Nodes[0]
			for i := 1; i < len(fixture.Nodes); i++ {
				fixture.Nodes[i].Resource.UID = ""
				fixture.Edges[i-1].Relation = "scales"
				fixture.Edges[i-1].SourceID, fixture.Edges[i-1].TargetID = "root", fixture.Nodes[i].ID
			}
			state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView("Widget")}, review: []string{"view"}}
			graph, err := graphHPACustomRun(graphCRDService(mode, fixture, state, graphHPACustomPermissions(), true))
			if err != nil || state.reads != graphCustomOwnerLimit || len(graph.Warnings) == 0 {
				t.Fatalf("scale targets exceeded shared budget: reads=%d graph=%+v err=%v", state.reads, graph, err)
			}
		})
	}
}

func TestResourceGraphHPACustomTargetRequiresAgentGrantAndNamedRBAC(t *testing.T) {
	for _, tc := range []struct {
		name   string
		saved  bool
		review []string
	}{{"missing saved grant", false, []string{"view"}}, {"named RBAC denied", true, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView("Widget")}, review: tc.review}
			graph, err := graphHPACustomRun(graphCRDService(domaincluster.ConnectionModeAgent, graphHPACustomFixture("Widget"), state, graphHPACustomPermissions(), tc.saved))
			if err != nil || state.reads != 0 || len(graph.Nodes) != 1 || len(graph.Edges) != 0 || len(graph.Evidence) != 1 {
				t.Fatalf("ungranted target leaked: graph=%+v reads=%d err=%v", graph, state.reads, err)
			}
		})
	}
}

func TestResourceGraphHPAScaleObservationDoesNotConfirmUIDLessOwnership(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		for _, relation := range []string{"controls", "owns"} {
			t.Run(string(mode)+"/"+relation, func(t *testing.T) {
				fixture := graphHPACustomFixture("Widget")
				fixture.Edges = append(fixture.Edges, domainresource.ResourceGraphEdge{ID: "uidless-owner", SourceID: "custom-owner-00", TargetID: "root", Relation: relation})
				state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView("Widget")}, review: []string{"view"}}
				graph, err := graphHPACustomRun(graphCRDService(mode, fixture, state, graphHPACustomPermissions(), true))
				if err != nil || state.reads != 1 || len(graph.Nodes) != 3 || len(graph.Edges) != 2 || len(graph.Warnings) == 0 {
					t.Fatalf("scale observation failed or uidless ownership retained: graph=%+v reads=%d err=%v", graph, state.reads, err)
				}
				for _, edge := range graph.Edges {
					if edge.ID == "uidless-owner" || edge.Relation == relation {
						t.Fatalf("scale UID upgraded a separate uidless owner claim: %+v", edge)
					}
				}
				if fixture.Nodes[1].Resource.UID != "" {
					t.Fatal("observation mutated the provider graph across requests")
				}
			})
		}
	}
}
