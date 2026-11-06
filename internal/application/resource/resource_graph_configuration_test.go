package resource

import (
	"context"
	"errors"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestConfigurationGraphNamespaceUsesItsNameForScope(t *testing.T) {
	for _, kind := range []string{"Namespace", "ResourceQuota"} {
		t.Run(kind, func(t *testing.T) {
			for _, allowed := range []bool{true, false} {
				allowedNamespace := "private"
				if allowed {
					allowedNamespace = "team-a"
				}
				namespace := domainresource.ResourceRef{APIVersion: "v1", Kind: "Namespace", Name: "team-a"}
				root := namespace
				graph := domainresource.ResourceGraph{RootID: "root", Nodes: []domainresource.ResourceGraphNode{{ID: "root", Resource: namespace}}}
				if kind == "ResourceQuota" {
					root = domainresource.ResourceRef{APIVersion: "v1", Kind: "ResourceQuota", Name: "quota", Namespace: "team-a"}
					graph.Nodes[0].Resource = root
					graph.Nodes = append(graph.Nodes, domainresource.ResourceGraphNode{ID: "namespace", Resource: namespace})
					graph.Edges = []domainresource.ResourceGraphEdge{{ID: "namespace-edge", SourceID: "root", TargetID: "namespace", Relation: "applies-in"}}
					graph.Evidence = []domainresource.ResourceEvidence{{ID: "namespace-event", ResourceID: "namespace"}}
				}
				authorizer := &graphNamespaceReferenceAuthorizer{scope: domainaccess.ResourceScope{Namespaces: []string{allowedNamespace}}}
				source := &graphViewDirectSource{graph: graph}
				service := &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "c"}}}, authorizer: authorizer, audit: discardAuditRecorder{}}, resourceGraph: source}
				got, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "c", root.Namespace, root.Kind, root.Name)
				if kind == "Namespace" && !allowed {
					if !errors.Is(err, apperrors.ErrAccessDenied) || source.called {
						t.Fatalf("denied Namespace root reached provider: graph=%+v err=%v", got, err)
					}
				} else if err != nil || len(got.Nodes) == 0 || (kind == "ResourceQuota" && (len(got.Edges) == 1) != allowed) || (!allowed && (len(got.Nodes) != 1 || len(got.Evidence) != 0)) {
					t.Fatalf("wrong Namespace visibility: allowed=%v graph=%+v err=%v", allowed, got, err)
				}
				for _, request := range authorizer.requests {
					if request.Resource.Kind == "Namespace" && request.Namespace.Namespace != namespace.Name {
						t.Fatalf("Namespace authorized without its real name: %+v", request)
					}
				}
			}
		})
	}
}

type configurationGraphAgentSource struct {
	GenericResourceAgent
	graph  domainresource.ResourceGraph
	called bool
}

func (s *configurationGraphAgentSource) GetResourceGraph(context.Context, string, string, string) (domainresource.ResourceGraph, error) {
	s.called = true
	return s.graph, nil
}

var configurationGraphIdentities = []struct{ api, kind, namespace, permission string }{
	{"v1", "ConfigMap", "team-a", "platform.configuration.config-maps.view"},
	{"v1", "Secret", "team-a", "platform.configuration.secrets.view"},
	{"v1", "ResourceQuota", "team-a", "platform.configuration.resource-quotas.view"},
	{"v1", "LimitRange", "team-a", "platform.configuration.limit-ranges.view"},
	{"autoscaling/v2", "HorizontalPodAutoscaler", "team-a", "platform.configuration.horizontal-pod-autoscalers.view"},
	{"policy/v1", "PodDisruptionBudget", "team-a", "platform.configuration.pod-disruption-budgets.view"},
	{"scheduling.k8s.io/v1", "PriorityClass", "", "platform.configuration.priority-classes.view"},
	{"node.k8s.io/v1", "RuntimeClass", "", "platform.configuration.runtime-classes.view"},
	{"coordination.k8s.io/v1", "Lease", "team-a", "platform.configuration.leases.view"},
	{"admissionregistration.k8s.io/v1", "MutatingWebhookConfiguration", "", "platform.configuration.mutating-webhook-configurations.view"},
	{"admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration", "", "platform.configuration.validating-webhook-configurations.view"},
	{"v1", "Namespace", "", "platform.namespaces.view"},
}

func TestConfigurationGraphExactBuiltinIdentity(t *testing.T) {
	for _, tc := range configurationGraphIdentities {
		t.Run(tc.kind, func(t *testing.T) {
			ref := domainresource.ResourceRef{APIVersion: tc.api, Kind: tc.kind, Namespace: tc.namespace, Name: "root"}
			if !graphBuiltinResource(ref) {
				t.Fatal("valid builtin was not recognized")
			}
			ref.APIVersion = "example.io/v1"
			if graphBuiltinResource(ref) {
				t.Fatal("custom group inherited builtin permission")
			}
			ref.APIVersion = tc.api
			if ref.Namespace == "" {
				ref.Namespace = "team-a"
			} else {
				ref.Namespace = ""
			}
			if graphBuiltinResource(ref) {
				t.Fatal("invalid namespace inherited builtin permission")
			}
			ref.Namespace = tc.namespace
			if tc.namespace == "" {
				ref.ScopeMode = domainresource.ResourceScopeModeNamespace
			} else {
				ref.ScopeMode = domainresource.ResourceScopeModeCluster
			}
			if graphBuiltinResource(ref) {
				t.Fatal("invalid scope inherited builtin permission")
			}
		})
	}
}

func TestConfigurationGraphPermissionsInBothModes(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		for _, tc := range configurationGraphIdentities {
			for _, allowed := range []bool{false, true} {
				label := "denied"
				if allowed {
					label = "allowed"
				}
				t.Run(string(mode)+"/"+tc.kind+"/"+label, func(t *testing.T) {
					ref := domainresource.ResourceRef{APIVersion: tc.api, Kind: tc.kind, Namespace: tc.namespace, Name: "root"}
					graph := domainresource.ResourceGraph{RootID: "root", Nodes: []domainresource.ResourceGraphNode{
						{ID: "root", Resource: ref},
						{ID: "denied-pod", Resource: domainresource.ResourceRef{APIVersion: "v1", Kind: "Pod", Name: "consumer", Namespace: "team-a"}},
						{ID: "spoof", Resource: domainresource.ResourceRef{APIVersion: "example.io/v1", Kind: tc.kind, Name: "spoof", Namespace: tc.namespace}},
					}, Edges: []domainresource.ResourceGraphEdge{
						{ID: "denied-edge", SourceID: "root", TargetID: "denied-pod", Relation: "applies-to-namespace"},
						{ID: "spoof-edge", SourceID: "root", TargetID: "spoof", Relation: "uses-configmap"},
					}, Evidence: []domainresource.ResourceEvidence{{ID: "root-event", ResourceID: "root"}, {ID: "denied-event", ResourceID: "denied-pod"}, {ID: "spoof-event", ResourceID: "spoof"}}}
					direct := &graphViewDirectSource{graph: graph}
					agent := &configurationGraphAgentSource{graph: graph}
					service := &GenericResources{resourceAccess: &resourceAccess{
						audit:      discardAuditRecorder{},
						resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: mode}}},
						authorizer: exactPermissionAuthorizer{tc.permission: allowed},
					}, resourceGraph: direct, agent: func(domaincluster.Connection) (GenericResourceAgent, error) { return agent, nil }}
					got, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{UserID: "reader"}, "cluster-a", tc.namespace, tc.kind, "root")
					if !allowed {
						if !errors.Is(err, apperrors.ErrAccessDenied) || direct.called || agent.called {
							t.Fatalf("denied root reached provider: %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if len(got.Nodes) != 1 || got.Nodes[0].ID != "root" || len(got.Edges) != 0 || len(got.Evidence) != 1 || got.Evidence[0].ResourceID != "root" || len(got.Warnings) == 0 {
						t.Fatalf("configuration permission filtering leaked: %+v", got)
					}
					if direct.called != (mode == domaincluster.ConnectionModeDirectKubeconfig) || agent.called != (mode == domaincluster.ConnectionModeAgent) {
						t.Fatal("wrong connection source")
					}
				})
			}
		}
	}
}

func TestConfigurationGraphInvalidScopeNeverReadsProvider(t *testing.T) {
	for _, tc := range configurationGraphIdentities {
		t.Run(tc.kind, func(t *testing.T) {
			namespace := ""
			if tc.namespace == "" {
				namespace = "team-a"
			}
			direct := &graphViewDirectSource{}
			access := &graphViewDecisionAuthorizer{}
			service := &GenericResources{resourceAccess: &resourceAccess{authorizer: access}, resourceGraph: direct}
			_, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", namespace, tc.kind, "root")
			if !errors.Is(err, apperrors.ErrInvalidArgument) || direct.called || len(access.requests) != 0 {
				t.Fatalf("invalid root was read or authorized: err=%v", err)
			}
		})
	}
}
