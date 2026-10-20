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

	cr "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
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
				if got := network.canReadTopologyVendor(context.Background(), domainidentity.Principal{}, connection, source, test.namespace); got != test.want {
					t.Fatalf("allowed = %v, want %v", got, test.want)
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
