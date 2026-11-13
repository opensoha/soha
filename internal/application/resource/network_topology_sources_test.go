package resource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cr "github.com/opensoha/soha/contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	"github.com/opensoha/soha/internal/policy"
)

type topologyDirectFixture struct {
	calls   int
	err     error
	queries []cr.NetworkTopologySourceQuery
}

func (f *topologyDirectFixture) NetworkTopologySnapshot(_ context.Context, clusterID string, input cr.NetworkTopologySnapshotRequest) (cr.NetworkTopologySnapshot, error) {
	f.calls++
	f.queries = append(f.queries, input.Queries...)
	return topologySourceFixture(clusterID, input), f.err
}

type topologyScopedAuthorizer struct {
	permissions exactPermissionAuthorizer
	scope       *domainaccess.ResourceScope
}

type topologyAgentFixture struct{ source *topologyDirectFixture }

func (f topologyAgentFixture) NetworkTopologySnapshot(ctx context.Context, input cr.NetworkTopologySnapshotRequest) (cr.NetworkTopologySnapshot, error) {
	return f.source.NetworkTopologySnapshot(ctx, "c", input)
}

func TestTopologyDoesNotReadCollectionsWithNamedResourceDeny(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		for _, tc := range []struct {
			name, namespace, denyNamespace, kind, sourceID string
			blocked                                        bool
		}{
			{"named Service in selected namespace", "team", "team", "Service", "kubernetes.services", true},
			{"named Service in another namespace", "team", "other", "Service", "kubernetes.services", false},
			{"named GatewayClass", "", "", "GatewayClass", "gateway.gatewayclasses", true},
		} {
			t.Run(string(mode)+"/"+tc.name, func(t *testing.T) {
				deny := domainaccess.Policy{Effect: domainaccess.EffectDeny, Priority: 20, Resources: domainaccess.ResourceMatcher{Kinds: []string{tc.kind}, Names: []string{"web"}}}
				if tc.denyNamespace != "" {
					deny.Namespaces.Names = []string{tc.denyNamespace}
				}
				allow := domainaccess.Policy{Effect: domainaccess.EffectAllow, Priority: 10, Resources: domainaccess.ResourceMatcher{Kinds: []string{"NetworkTopology", "Ingress", tc.kind}}}
				engine := policy.NewEngine()
				authorizer := rbacGraphTestAuthorizer(func(request domainaccess.Request) domainaccess.Decision {
					decision, err := engine.Evaluate(context.Background(), request, []domainaccess.Policy{allow, deny})
					if err != nil {
						t.Fatal(err)
					}
					return decision
				})
				provider := &topologyDirectFixture{}
				connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: mode}}
				network := &Network{
					resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: authorizer, audit: discardAuditRecorder{}},
					topologyDirect: provider,
					topologyAgent: func(domaincluster.Connection) (TopologySourceAgent, error) {
						return topologyAgentFixture{source: provider}, nil
					},
				}
				view, err := network.GetNetworkTopology(context.Background(), domainidentity.Principal{}, "c", tc.namespace)
				if err != nil {
					t.Fatal(err)
				}
				denied, targetRead, ingressRead := false, false, false
				for _, item := range view.Coverage {
					denied = denied || item.SourceID == tc.sourceID && item.State == "denied"
				}
				for _, query := range provider.queries {
					targetRead = targetRead || query.SourceID == tc.sourceID
					ingressRead = ingressRead || query.SourceID == "kubernetes.ingresses"
				}
				if denied != tc.blocked || targetRead == tc.blocked || !ingressRead {
					t.Fatalf("wrong source authorization: queries=%+v coverage=%+v", provider.queries, view.Coverage)
				}
			})
		}
	}
}

type topologyWidePolicy struct {
	denyAction domainaccess.Action
	requests   []domainaccess.Request
}

func (a *topologyWidePolicy) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	a.requests = append(a.requests, request)
	switch request.Resource.Kind {
	case "Service":
		return domainaccess.Decision{Allowed: !request.RequireClusterWideNamespace || request.Action != a.denyAction}, nil
	case "Ingress", "NetworkTopology":
		return domainaccess.Decision{Allowed: true}, nil
	default:
		return domainaccess.Decision{}, nil
	}
}

func TestTopologyRequiresStrictWideScopeBeforeNamespacedSourceRead(t *testing.T) {
	for _, namespace := range []string{"", "team"} {
		t.Run("namespace="+namespace, func(t *testing.T) {
			auth := &topologyWidePolicy{denyAction: domainaccess.ActionView}
			direct := &topologyDirectFixture{}
			connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}
			network := &Network{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: auth, audit: discardAuditRecorder{}}, topologyDirect: direct}
			view, err := network.GetNetworkTopology(context.Background(), domainidentity.Principal{}, "c", namespace)
			if err != nil {
				t.Fatal(err)
			}
			var wide, service, ingress bool
			for _, request := range auth.requests {
				if request.Resource.Kind == "Service" && request.RequireClusterWideNamespace {
					wide = true
				}
			}
			for _, query := range direct.queries {
				service = service || query.SourceID == "kubernetes.services"
				ingress = ingress || query.SourceID == "kubernetes.ingresses"
				if query.Namespace != namespace {
					t.Fatalf("unexpected source namespace: %+v", query)
				}
			}
			if wide != (namespace == "") || service != (namespace != "") || !ingress {
				t.Fatalf("incorrect source scope proof: wide=%t service=%t ingress=%t coverage=%+v", wide, service, ingress, view.Coverage)
			}
		})
	}
}

type topologyLegacyNetworkReader struct {
	DirectNetworkReader
	serviceReads, ingressReads int
}

func (r *topologyLegacyNetworkReader) ListServices(context.Context, string, string) ([]domainresource.ServiceView, string, error) {
	r.serviceReads++
	return nil, "test", nil
}

func (r *topologyLegacyNetworkReader) ListIngresses(context.Context, string, string) ([]domainresource.IngressView, string, error) {
	r.ingressReads++
	return []domainresource.IngressView{{Name: "visible", Namespace: "team", BackendServices: []string{"hidden-service"}}}, "test", nil
}

func TestTopologyLegacyFallbackCannotReadARestrictedSource(t *testing.T) {
	for _, namespace := range []string{"", "team"} {
		t.Run("namespace="+namespace, func(t *testing.T) {
			auth := &topologyWidePolicy{denyAction: domainaccess.ActionList}
			legacy := &topologyLegacyNetworkReader{}
			connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}
			network := &Network{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: auth, audit: discardAuditRecorder{}}, topologyDirect: &topologyDirectFixture{err: cr.ErrNetworkTopologySnapshotUnsupported}, directReader: legacy}
			view, err := network.GetNetworkTopology(context.Background(), domainidentity.Principal{}, "c", namespace)
			raw, _ := json.Marshal(view)
			if err != nil || legacy.serviceReads != 0 || legacy.ingressReads != 1 || len(view.Coverage) != 1 || view.Coverage[0].SourceID != "legacy.inventory" || len(view.Traces) != 1 || strings.Contains(string(raw), "hidden-service") {
				t.Fatalf("legacy fallback bypassed policy or lost an authorized source: services=%d ingresses=%d view=%s err=%v", legacy.serviceReads, legacy.ingressReads, raw, err)
			}
		})
	}
}

func (a topologyScopedAuthorizer) Authorize(ctx context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	decision, err := a.permissions.Authorize(ctx, request)
	decision.ResourceScope = a.scope
	return decision, err
}

func TestTopologyClusterSourcesRequireClusterWideScope(t *testing.T) {
	for _, test := range []struct {
		name        string
		scope       *domainaccess.ResourceScope
		wantQueries int
	}{
		{"unrestricted", nil, 2},
		{"namespace", &domainaccess.ResourceScope{Namespaces: []string{"team"}}, 0},
		{"excluded-namespace", &domainaccess.ResourceScope{ExcludedNamespaces: []string{"private"}}, 0},
		{"namespace-selector", &domainaccess.ResourceScope{NamespaceSelectors: []string{"team=a"}}, 0},
		{"excluded-selector", &domainaccess.ResourceScope{ExcludedNamespaceSelectors: []string{"private=true"}}, 0},
		{"label-selector", &domainaccess.ResourceScope{LabelSelector: "team=a"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}
			direct := &topologyDirectFixture{}
			authorizer := topologyScopedAuthorizer{scope: test.scope, permissions: exactPermissionAuthorizer{
				"platform.network.topology.view":                                          true,
				resourcePermissionKey("network", "IngressClass", domainaccess.ActionView): true,
				resourcePermissionKey("network", "GatewayClass", domainaccess.ActionView): true,
			}}
			network := &Network{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: authorizer, audit: discardAuditRecorder{}}, topologyDirect: direct}
			view, err := network.GetNetworkTopology(context.Background(), domainidentity.Principal{}, "c", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(direct.queries) != test.wantQueries {
				t.Fatalf("queries = %#v", direct.queries)
			}
			if test.wantQueries == 0 {
				for _, coverage := range view.Coverage {
					if coverage.SourceID == "kubernetes.ingressclasses" || coverage.SourceID == "gateway.gatewayclasses" {
						if coverage.State != "denied" || coverage.ReasonCode != "soha_permission" {
							t.Fatalf("coverage = %#v", coverage)
						}
					}
				}
			}
		})
	}
}

func TestTopologyVendorReadsKeepSavedGrantsWithoutUnrelatedActionReviews(t *testing.T) {
	for _, sourceID := range []string{"traefik.ingressroutes", "gateway.tcproutes", "gateway.tlsroutes", "gateway.udproutes"} {
		source, _ := cr.LookupNetworkTopologySource(sourceID)
		for _, test := range []struct {
			name      string
			namespace string
			verb      string
			permitted bool
			want      bool
		}{
			{"list-granted", "team", "list", true, true},
			{"wrong-namespace", "other", "list", true, false},
			{"mutation-only", "team", "update", true, false},
			{"soha-denied", "team", "list", false, false},
		} {
			t.Run(sourceID+"/"+test.name, func(t *testing.T) {
				connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: domaincluster.ConnectionModeAgent}, Metadata: map[string]any{"agent_custom_resource_rules": []domaincluster.AgentCustomResourceRule{{APIGroup: source.Group, Resources: []string{source.Resource}, Verbs: []string{test.verb}, Namespaces: []string{"team"}}}}}
				access := &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: exactPermissionAuthorizer{
					resourcePermissionKey("extensions", "CustomResourceDefinition", domainaccess.ActionView): true,
					resourcePermissionKey("extensions", source.Kind, domainaccess.ActionList):                test.permitted,
				}, audit: discardAuditRecorder{}}
				// No Agent client is supplied: actual LIST, rather than five SSARs, checks Kubernetes RBAC.
				network := &Network{resourceAccess: access, customResources: &CustomResources{resourceAccess: access}}
				got, err := network.canReadTopologyVendor(context.Background(), domainidentity.Principal{}, connection, source, test.namespace)
				if got != test.want || (got && err != nil) {
					t.Fatalf("allowed = %v, err = %v, want %v", got, err, test.want)
				}
			})
		}
	}
}

func TestTopologyTransportFailureHasOneCoverageEntryPerSource(t *testing.T) {
	connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}
	network := &Network{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: exactPermissionAuthorizer{"platform.network.topology.view": true, "platform.network.services.view": true}, audit: discardAuditRecorder{}}, topologyDirect: &topologyDirectFixture{err: errors.New("transport unavailable")}}
	view, err := network.GetNetworkTopology(context.Background(), domainidentity.Principal{}, "c", "team")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, coverage := range view.Coverage {
		if coverage.SourceID == "kubernetes.services" {
			count++
			if coverage.ReasonCode != "source_unavailable" {
				t.Fatalf("coverage = %#v", coverage)
			}
		}
	}
	if count != 1 || len(view.Nodes) != 0 {
		t.Fatalf("coverage count = %d, nodes = %d", count, len(view.Nodes))
	}
}
func topologySourceFixture(clusterID string, input cr.NetworkTopologySnapshotRequest) cr.NetworkTopologySnapshot {
	result := cr.NetworkTopologySnapshot{Version: cr.NetworkTopologySnapshotFeature}
	for _, query := range input.Queries {
		definition, _ := cr.LookupNetworkTopologySource(query.SourceID)
		source := cr.NetworkTopologySourceResult{Coverage: cr.NetworkTopologyCoverage{SourceID: query.SourceID, Namespace: query.Namespace, State: "available"}}
		name, spec := "web", `"spec":{"type":"ExternalName","externalName":"upstream.example","ports":[{"port":80}]}`
		if query.SourceID == "kubernetes.ingresses" {
			name = "entry"
			spec = `"spec":{"rules":[{"host":"shop.example","http":{"paths":[{"path":"/shop","backend":{"service":{"name":"web","port":{"number":80}}}}]}}]}`
		}
		content := `{"metadata":{"name":"` + name + `","namespace":"team","uid":"` + name + `-uid","annotations":{"private":"do-not-transport"}},` + spec + `}`
		source.Objects = []cr.NetworkTopologySourceObject{{Resource: cr.ResourceRef{ClusterID: clusterID, APIVersion: strings.TrimPrefix(definition.Group+"/"+definition.Version, "/"), Kind: definition.Kind, Namespace: "team", Name: name, UID: name + "-uid"}, Content: content}}
		result.Sources = append(result.Sources, source)
	}
	return result
}

func TestNetworkTopologyDirectAndAgentUseTheSameAuthorizedProjection(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		t.Run(string(mode), func(t *testing.T) {
			var agentCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("Agent authentication missing")
				}
				if r.URL.Path == "/api/v1/platform/summary" {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": domaincluster.Summary{Capabilities: []string{cr.NetworkTopologySnapshotFeature}}})
					return
				}
				if r.URL.Path != "/api/v1/platform/network/topology/snapshot" {
					t.Errorf("unexpected Agent route %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				agentCalls.Add(1)
				var input cr.NetworkTopologySnapshotRequest
				if json.NewDecoder(r.Body).Decode(&input) != nil {
					t.Error("invalid RPC")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": topologySourceFixture("c", input)})
			}))
			defer server.Close()
			connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: mode}, Metadata: map[string]any{"endpoint": server.URL, "token": "fixture-token"}}
			direct := &topologyDirectFixture{}
			registry := agentinfra.NewRegistry(time.Second)
			network := &Network{resourceAccess: &resourceAccess{audit: discardAuditRecorder{}, resolver: stubConnectionResolver{connection: connection}, authorizer: exactPermissionAuthorizer{"platform.network.topology.view": true, "platform.network.ingresses.view": true, "platform.network.services.view": true}}, topologyDirect: direct, topologyAgent: func(c domaincluster.Connection) (TopologySourceAgent, error) { return registry.ClientFor(c) }}
			view, err := network.GetNetworkTopology(context.Background(), domainidentity.Principal{}, "c", "team")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, edge := range view.Edges {
				found = found || (edge.Relation == "forwards" && edge.Port == "80")
			}
			raw, _ := json.Marshal(view)
			if !found || !strings.Contains(string(raw), "shop.example") || strings.Contains(string(raw), "do-not-transport") {
				t.Fatalf("bad graph projection: %s", raw)
			}
			if mode == domaincluster.ConnectionModeAgent {
				if direct.calls != 0 || agentCalls.Load() != 1 {
					t.Fatal("Agent snapshot fell back to direct")
				}
			} else if direct.calls != 1 || agentCalls.Load() != 0 {
				t.Fatal("direct snapshot used Agent")
			}
		})
	}
}

func TestTopologyRejectsDeniedObjectsAndMismatchedUID(t *testing.T) {
	input := cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{{SourceID: "kubernetes.services", Namespace: "team"}}}
	snapshot := topologySourceFixture("c", input)
	snapshot.Sources[0].Coverage.State = "denied"
	invalid := snapshot.Sources[0].Objects[0]
	invalid.Resource.UID = "different"
	snapshot.Sources[0].Objects = append([]cr.NetworkTopologySourceObject{invalid}, snapshot.Sources[0].Objects...)
	result := validateTopologySnapshot(snapshot, input, "c")
	if result.Sources[0].Coverage.State != "denied" || len(result.Sources[0].Objects) != 0 {
		t.Fatal("invalid object changed denied coverage into partial and exposed objects")
	}
	snapshot.Sources[0].Coverage.State = "available"
	snapshot.Sources[0].Objects = []cr.NetworkTopologySourceObject{invalid}
	if len(validateTopologySnapshot(snapshot, input, "c").Sources[0].Objects) != 0 {
		t.Fatal("metadata and reference UID mismatch accepted")
	}
}
