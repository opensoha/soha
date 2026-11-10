package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	"strings"
	"testing"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type networkGraphSnapshotStub struct {
	requests     []cr.NetworkTopologySnapshotRequest
	oldAgent     bool
	rootState    string
	rootReason   string
	extraObjects map[string][]cr.NetworkTopologySourceObject
}

func (s *networkGraphSnapshotStub) read(input cr.NetworkTopologySnapshotRequest) cr.NetworkTopologySnapshot {
	s.requests = append(s.requests, input)
	output := cr.NetworkTopologySnapshot{Version: cr.NetworkTopologySnapshotFeature}
	for _, query := range input.Queries {
		definition, _ := cr.LookupNetworkTopologySource(query.SourceID)
		source := cr.NetworkTopologySourceResult{Coverage: topologyCoverage(definition, query.Namespace, "available", ""), Objects: s.extraObjects[query.SourceID]}
		if query.Name != "" {
			output.Version = cr.NetworkTopologyNamedRootFeature
			source.Coverage.Name = query.Name
			if s.rootState != "" {
				source.Coverage.State = s.rootState
				source.Coverage.ReasonCode = s.rootReason
			} else {
				source.Objects = []cr.NetworkTopologySourceObject{{Resource: cr.ResourceRef{ClusterID: "agent-local-id", APIVersion: "v1", Kind: "Service", Namespace: "team-a", Name: "api", UID: "uid", ScopeMode: cr.ResourceScopeModeNamespace}, Content: `{"metadata":{"name":"api","namespace":"team-a","uid":"uid"},"spec":{"selector":{"app":"api"},"ports":[{"port":80}]}}`}}
			}
		}
		output.Sources = append(output.Sources, source)
	}
	if s.oldAgent {
		output.Version = cr.NetworkTopologySnapshotFeature
	}
	return output
}

func (s *networkGraphSnapshotStub) NetworkTopologySnapshot(_ context.Context, _ string, input cr.NetworkTopologySnapshotRequest) (cr.NetworkTopologySnapshot, error) {
	return s.read(input), nil
}

type networkGraphAgentSnapshot struct{ stub *networkGraphSnapshotStub }

func (s networkGraphAgentSnapshot) NetworkTopologySnapshot(_ context.Context, input cr.NetworkTopologySnapshotRequest) (cr.NetworkTopologySnapshot, error) {
	return s.stub.read(input), nil
}

func TestResourceGraphNetworkUsesAuthorizedSnapshotWithoutTopologyMenu(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		t.Run(string(mode), func(t *testing.T) {
			stub := &networkGraphSnapshotStub{}
			access := &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: mode}}}, authorizer: exactPermissionAuthorizer{"platform.network.services.view": true}, audit: discardAuditRecorder{}}
			generic := &GenericResources{resourceAccess: access, topologyDirect: stub, topologyAgent: func(domaincluster.Connection) (TopologySourceAgent, error) {
				return networkGraphAgentSnapshot{stub}, nil
			}}
			graph, err := generic.GetResourceGraph(context.Background(), domainidentity.Principal{UserID: "reader"}, "cluster-a", "team-a", "Service", "api")
			if err != nil || len(graph.Nodes) == 0 || len(graph.Warnings) == 0 {
				t.Fatalf("authorized partial graph: %+v %v", graph, err)
			}
			for _, node := range graph.Nodes {
				if node.Resource.ClusterID != "cluster-a" {
					t.Fatal("Agent-local cluster ID leaked")
				}
			}
			if len(stub.requests) != 2 || stub.requests[0].Queries[0].Name != "api" {
				t.Fatalf("root named read absent: %+v", stub.requests)
			}
			for _, query := range stub.requests[1].Queries {
				if query.SourceID != "kubernetes.services" {
					t.Fatalf("denied source reached adapter: %+v", query)
				}
			}
		})
	}
}

func TestResourceGraphNetworkRootDeniedOldAgentAndRootErrors(t *testing.T) {
	for _, tc := range []struct {
		name, state, reason string
		old, allowed        bool
		want                error
	}{
		{name: "root denied", want: apperrors.ErrAccessDenied},
		{name: "old Agent", old: true, allowed: true, want: apperrors.ErrUnsupportedOperation},
		{name: "root missing", state: "not_installed", reason: "named_not_found", allowed: true, want: apperrors.ErrNotFound},
		{name: "root source absent", state: "not_installed", reason: "source_not_served", allowed: true, want: apperrors.ErrUnsupportedOperation},
		{name: "root Kubernetes denied", state: "denied", reason: "kubernetes_rbac", allowed: true, want: apperrors.ErrAccessDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &networkGraphSnapshotStub{oldAgent: tc.old, rootState: tc.state, rootReason: tc.reason}
			generic := &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, authorizer: exactPermissionAuthorizer{"platform.network.services.view": tc.allowed}, audit: discardAuditRecorder{}}, topologyDirect: stub}
			_, err := generic.GetResourceGraph(context.Background(), domainidentity.Principal{UserID: "reader"}, "cluster-a", "team-a", "Service", "api")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if !tc.allowed && len(stub.requests) != 0 || tc.allowed && len(stub.requests) != 1 {
				t.Fatalf("failed root reached source collection: %+v", stub.requests)
			}
		})
	}
}

func TestResourceGraphNetworkRootRequiresProvenNamedScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scope   domainaccess.ResourceScope
		allowed bool
	}{
		{"projected namespace labels", domainaccess.ResourceScope{Namespaces: []string{"team-a"}, NamespaceSelectors: []string{"tenant=retail"}, LabelSelector: "tenant=retail"}, true},
		{"selector without named proof", domainaccess.ResourceScope{NamespaceSelectors: []string{"tenant=retail"}, LabelSelector: "tenant=retail"}, false},
		{"unrelated namespace", domainaccess.ResourceScope{Namespaces: []string{"team-b"}}, false},
		{"object label selector", domainaccess.ResourceScope{Namespaces: []string{"team-a"}, LabelSelector: "app=api"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &networkGraphSnapshotStub{}
			authorizer := &scopeDecisionAuthorizer{decision: domainaccess.Decision{Allowed: true, ResourceScope: &tc.scope}}
			service := &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "c"}}}, authorizer: authorizer, audit: discardAuditRecorder{}}, topologyDirect: stub}
			graph, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "c", "team-a", "Service", "api")
			if tc.allowed {
				if err != nil || len(graph.Nodes) != 1 || len(stub.requests) != 1 || stub.requests[0].Queries[0].Name != "api" {
					t.Fatalf("named root was unavailable or scope was widened: graph=%+v requests=%+v err=%v", graph, stub.requests, err)
				}
			} else if !errors.Is(err, apperrors.ErrAccessDenied) || len(stub.requests) != 0 {
				t.Fatalf("unproven scope reached provider: graph=%+v requests=%+v err=%v", graph, stub.requests, err)
			}
		})
	}
}

func TestResourceGraphNetworkExactGVKScope(t *testing.T) {
	for _, tc := range []struct {
		api, kind, namespace string
		want                 bool
	}{
		{"gateway.networking.k8s.io/v1", "Gateway", "team", true},
		{"example.io/v1", "Gateway", "team", false},
		{"gateway.networking.k8s.io/v9", "Gateway", "team", false},
		{"gateway.networking.k8s.io/v1", "GatewayClass", "", true},
		{"gateway.networking.k8s.io/v1", "GatewayClass", "team", false},
		{"gateway.networking.k8s.io/v1beta1", "ReferenceGrant", "team", true},
		{"discovery.k8s.io/v1", "EndpointSlice", "team", true},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			if got := graphBuiltinResource(cr.ResourceRef{APIVersion: tc.api, Kind: tc.kind, Namespace: tc.namespace}); got != tc.want {
				t.Fatalf("builtin=%v", got)
			}
		})
	}
}

func TestResourceGraphNetworkFiltersSourceRefAndFreeText(t *testing.T) {
	service := &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "c"}}}, authorizer: &graphViewDecisionAuthorizer{}, audit: discardAuditRecorder{}}}
	root := cr.ResourceRef{ClusterID: "c", APIVersion: "v1", Kind: "Service", Name: "api", Namespace: "team"}
	other := root
	other.Name = "other"
	secret := cr.ResourceRef{ClusterID: "c", APIVersion: "v1", Kind: "Secret", Name: "private-secret-name", Namespace: "hidden"}
	graph := cr.ResourceGraph{RootID: "root", Nodes: []cr.ResourceGraphNode{{ID: "root", Resource: root}, {ID: "other", Resource: other}, {ID: "hidden", Resource: secret}}, Edges: []cr.ResourceGraphEdge{{ID: "safe", SourceID: "root", TargetID: "other", Relation: "routes-to", Note: secret.Name, FieldPath: secret.Name}, {ID: "hidden-evidence", SourceID: "root", TargetID: "other", Relation: "routes-to", SourceRef: &secret}}, Evidence: []cr.ResourceEvidence{{ID: "evidence", ResourceID: "root", Summary: secret.Name}}}
	filtered := service.filterAuthorizedGraph(context.Background(), domainidentity.Principal{}, "c", graph, nil)
	raw, _ := json.Marshal(filtered)
	if len(filtered.Nodes) != 2 || len(filtered.Edges) != 1 || len(filtered.Evidence) != 0 || strings.Contains(string(raw), secret.Name) {
		t.Fatalf("unauthorized evidence leaked: %s", raw)
	}
}

func TestResourceGraphNetworkReservesReadBudgetAfterSlowSourceAuthorization(t *testing.T) {
	stub := &networkGraphSnapshotStub{}
	service := &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, authorizer: &topologyReadAuthorizer{blockKind: "HTTPRoute"}, audit: discardAuditRecorder{}}, topologyDirect: stub}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	graph, err := service.GetResourceGraph(ctx, domainidentity.Principal{}, "cluster-a", "team-a", "Service", "api")
	if err != nil || len(graph.Nodes) == 0 || len(graph.Warnings) == 0 || len(stub.requests) != 2 {
		t.Fatalf("authorized reads starved: %+v %v requests=%+v", graph, err, stub.requests)
	}
}

type networkGraphScopedAuthorizer struct{}

func (networkGraphScopedAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	allowed := request.Resource.Kind == "Service" && (request.Namespace.Namespace == "" || request.Namespace.Namespace == "team-a")
	return domainaccess.Decision{Allowed: allowed, ResourceScope: &domainaccess.ResourceScope{Namespaces: []string{"team-a"}}}, nil
}
func TestResourceGraphNetworkRestrictsSourceNamespacesBeforeReads(t *testing.T) {
	stub := &networkGraphSnapshotStub{}
	service := &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, authorizer: networkGraphScopedAuthorizer{}, audit: discardAuditRecorder{}}, topologyDirect: stub}
	if _, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "team-a", "Service", "api"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 2 || len(stub.requests[1].Queries) != 1 || stub.requests[1].Queries[0].Namespace != "team-a" {
		t.Fatalf("broad source read escaped scope: %+v", stub.requests)
	}
}

type graphNetworkRootScopeAuthorizer struct{ scope domainaccess.ResourceScope }

func (a graphNetworkRootScopeAuthorizer) Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: true, ResourceScope: &a.scope}, nil
}
func TestResourceGraphNetworkNamedRootRejectsUnprovableScopesBeforeRead(t *testing.T) {
	for _, tc := range []struct {
		kind, namespace string
		scope           domainaccess.ResourceScope
	}{
		{kind: "GatewayClass", scope: domainaccess.ResourceScope{Namespaces: []string{"team-a"}}},
		{kind: "IngressClass", scope: domainaccess.ResourceScope{NamespaceSelectors: []string{"team=public"}}},
		{kind: "Service", namespace: "team-a", scope: domainaccess.ResourceScope{LabelSelector: "app=public"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			stub := &networkGraphSnapshotStub{}
			service := &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, authorizer: graphNetworkRootScopeAuthorizer{scope: tc.scope}, audit: discardAuditRecorder{}}, topologyDirect: stub}
			if _, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", tc.namespace, tc.kind, "api"); !errors.Is(err, apperrors.ErrAccessDenied) || len(stub.requests) != 0 {
				t.Fatalf("unproven scoped root reached named reader: err=%v requests=%+v", err, stub.requests)
			}
		})
	}
}

func TestResourceGraphNetworkPreservesAuthorizedObservedProviderAndL4(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		for _, sourceID := range []string{"gateway.tcproutes", "traefik.ingressroutes"} {
			t.Run(string(mode)+sourceID, func(t *testing.T) {
				definition, _ := cr.LookupNetworkTopologySource(sourceID)
				content := `{"metadata":{"name":"route","namespace":"team-a","uid":"route-uid"},"spec":{"rules":[{"backendRefs":[{"name":"api","port":80}]}]}}`
				if sourceID == "traefik.ingressroutes" {
					content = `{"metadata":{"name":"route","namespace":"team-a","uid":"route-uid"},"spec":{"routes":[{"match":"Host(test.example)","services":[{"name":"api","port":80}]}]}}`
				}
				ref := cr.ResourceRef{ClusterID: "agent-local", APIVersion: definition.Group + "/" + definition.Version, Kind: definition.Kind, Namespace: "team-a", Name: "route", UID: "route-uid", ScopeMode: cr.ResourceScopeModeNamespace}
				stub := &networkGraphSnapshotStub{extraObjects: map[string][]cr.NetworkTopologySourceObject{sourceID: {{Resource: ref, Content: content}}}}
				connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: mode}, Metadata: map[string]any{"agent_custom_resource_rules": []domaincluster.AgentCustomResourceRule{{APIGroup: definition.Group, Resources: []string{definition.Resource}, Verbs: []string{"list"}, Namespaces: []string{"team-a"}}}}}
				access := &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: &topologyReadAuthorizer{}, audit: discardAuditRecorder{}}
				service := &GenericResources{resourceAccess: access, customResources: &CustomResources{resourceAccess: access}, topologyDirect: stub, topologyAgent: func(domaincluster.Connection) (TopologySourceAgent, error) {
					return networkGraphAgentSnapshot{stub}, nil
				}}
				graph, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "team-a", "Service", "api")
				if err != nil {
					t.Fatal(err)
				}
				routeID := ""
				for _, node := range graph.Nodes {
					if node.Resource.Kind == definition.Kind && node.Resource.Name == "route" && node.Resource.UID == "route-uid" {
						routeID = node.ID
					}
				}
				connected := false
				for _, edge := range graph.Edges {
					if edge.SourceID == routeID || edge.TargetID == routeID {
						connected = true
					}
					if edge.SourceRef != nil && edge.SourceRef.Kind == definition.Kind && edge.SourceRef.UID != "route-uid" {
						t.Fatal("sourceRef identity changed")
					}
				}
				if routeID == "" || !connected {
					t.Fatalf("authorized observed source was filtered: %+v", graph)
				}
				if strings.Contains(strings.Join(graph.Warnings, " "), "hidden by access policy") {
					t.Fatalf("authorized source was reported hidden: %+v", graph.Warnings)
				}
				// Observation of route-uid never authorizes another same-Kind UID.
				ref.ClusterID = "cluster-a"
				credentials := map[string]bool{graphAuthorizedIdentity(ref): true}
				ref.UID = "unobserved-uid"
				if service.graphNodeReferenceAllowed(context.Background(), domainidentity.Principal{}, "cluster-a", ref, credentials, true) {
					t.Fatal("same-Kind unobserved declaration inherited credentials")
				}
			})
		}
	}
}

type graphNamespaceReferenceAuthorizer struct {
	scope    domainaccess.ResourceScope
	requests []domainaccess.Request
}

func (a *graphNamespaceReferenceAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	if request.Resource.Kind == "Namespace" || request.Resource.Kind == "Node" {
		return domainaccess.Decision{Allowed: true, ResourceScope: &a.scope}, nil
	}
	return domainaccess.Decision{Allowed: true}, nil
}

func TestResourceGraphNetworkNamespaceReferenceScopesAndSourceRef(t *testing.T) {
	for _, tc := range []struct {
		name, namespace string
		scope           domainaccess.ResourceScope
		allowed         bool
	}{
		{"allowed namespace", "team-a", domainaccess.ResourceScope{Namespaces: []string{"team-a"}}, true},
		{"other namespace", "private", domainaccess.ResourceScope{Namespaces: []string{"team-a"}}, false},
		{"excluded namespace", "private", domainaccess.ResourceScope{ExcludedNamespaces: []string{"private"}}, false},
		{"unproven selector", "private", domainaccess.ResourceScope{NamespaceSelectors: []string{"team=public"}}, false},
		{"projected namespace selector", "team-a", domainaccess.ResourceScope{Namespaces: []string{"team-a"}, NamespaceSelectors: []string{"team=public"}, LabelSelector: "team=public"}, true},
		{"projected exclusion selector", "team-a", domainaccess.ResourceScope{Namespaces: []string{"team-a"}, ExcludedNamespaceSelectors: []string{"private=true"}}, true},
		{"arbitrary label selector", "scope-only", domainaccess.ResourceScope{Namespaces: []string{"scope-only"}, LabelSelector: "app=public"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorizer := &graphNamespaceReferenceAuthorizer{scope: tc.scope}
			service := &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "c"}}}, authorizer: authorizer, audit: discardAuditRecorder{}}}
			root := cr.ResourceRef{ClusterID: "c", APIVersion: "networking.k8s.io/v1", Kind: "IngressClass", Name: "public", ScopeMode: cr.ResourceScopeModeCluster}
			namespace := cr.ResourceRef{ClusterID: "c", APIVersion: "v1", Kind: "Namespace", Name: tc.namespace, ScopeMode: cr.ResourceScopeModeCluster}
			backend := cr.ResourceRef{ClusterID: "c", APIVersion: "v1", Kind: "Service", Namespace: "team-a", Name: "backend", ScopeMode: cr.ResourceScopeModeNamespace}
			graph := cr.ResourceGraph{RootID: "root", Nodes: []cr.ResourceGraphNode{{ID: "root", Resource: root}, {ID: "namespace", Resource: namespace, Status: "unresolved"}, {ID: "backend", Resource: backend}}, Edges: []cr.ResourceGraphEdge{{ID: "parameters", SourceID: "root", TargetID: "namespace", Relation: "uses-parameters", SourceRef: &root}, {ID: "namespace-source", SourceID: "root", TargetID: "backend", Relation: "uses-parameters", SourceRef: &namespace, Note: tc.namespace}}, Evidence: []cr.ResourceEvidence{{ID: "evidence", ResourceID: "root", Summary: tc.namespace}}}
			filtered := service.filterAuthorizedGraph(context.Background(), domainidentity.Principal{}, "c", graph, nil)
			if tc.allowed {
				if len(filtered.Nodes) != 3 || len(filtered.Edges) != 2 || len(filtered.Evidence) != 1 {
					t.Fatalf("allowed Namespace declaration hidden: %+v", filtered)
				}
			} else {
				raw, _ := json.Marshal(filtered)
				if len(filtered.Nodes) != 2 || len(filtered.Edges) != 0 || len(filtered.Evidence) != 0 || strings.Contains(string(raw), tc.namespace) {
					t.Fatalf("scoped Namespace leaked: %s", raw)
				}
			}
			for _, request := range authorizer.requests {
				if request.Resource.Kind == "Namespace" && request.Namespace.Namespace != tc.namespace {
					t.Fatalf("Namespace permission evaluated without its name: %+v", request)
				}
			}
			otherCluster := cr.ResourceRef{ClusterID: "c", APIVersion: "v1", Kind: "Node", Name: "worker", ScopeMode: cr.ResourceScopeModeCluster}
			if service.graphNodeReferenceAllowed(context.Background(), domainidentity.Principal{}, "c", otherCluster, nil, true) {
				t.Fatal("other cluster reference bypassed cluster-wide scope")
			}
		})
	}
}
