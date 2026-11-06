package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type graphCRDObservation struct {
	views                 []domainresource.CRDView
	reads, lists, reviews int
	readError             error
	content               string
	namespace             string
	review                []string
	cancel                context.CancelFunc
}

func (s *graphCRDObservation) get(ctx context.Context, request cr.CustomResourceGraphObjectRequest) (json.RawMessage, error) {
	definition, namespace, name := request.Definition, request.Namespace, request.Name
	s.reads++
	s.namespace = namespace
	if definition.Group != "example.io" || definition.Version != "v1" || definition.Resource != "widgets" {
		panic("unobserved GVR")
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 8*time.Second {
		panic("missing graph deadline")
	}
	if s.cancel != nil {
		s.cancel()
		return nil, ctx.Err()
	}
	content := s.content
	if content == "" {
		content = fmt.Sprintf(`{"apiVersion":"example.io/v1","kind":%q,"metadata":{"name":%q,"namespace":%q,"uid":%q},"spec":{"token":"secret-payload"},"data":{"password":"secret-payload"}}`, definition.Kind, name, namespace, name+"-uid")
	}
	return json.RawMessage(content), s.readError
}

type graphCRDDirect struct {
	DirectCustomResource
	s *graphCRDObservation
}

func (d graphCRDDirect) ListCRDs(context.Context, string) ([]domainresource.CRDView, error) {
	d.s.lists++
	return d.s.views, nil
}
func (d graphCRDDirect) ReadCustomResourceGraphObject(ctx context.Context, _ string, request cr.CustomResourceGraphObjectRequest) (json.RawMessage, error) {
	return d.s.get(ctx, request)
}

type graphCRDAgent struct {
	CustomResourceAgent
	s *graphCRDObservation
}

func (d graphCRDAgent) ListCRDs(context.Context) ([]domainresource.CRDView, error) {
	d.s.lists++
	return d.s.views, nil
}
func (d graphCRDAgent) ReadCustomResourceGraphObject(ctx context.Context, request cr.CustomResourceGraphObjectRequest) (json.RawMessage, error) {
	return d.s.get(ctx, request)
}
func (d graphCRDAgent) CustomResourceActions(_ context.Context, def domainresource.CRDResourceDefinition, ns, name string) ([]string, error) {
	d.s.reviews++
	if name == "" || def.Group != "example.io" || def.Resource != "widgets" {
		panic("named exact GVR review required")
	}
	return d.s.review, nil
}

type graphCRDRoot struct{ graph domainresource.ResourceGraph }

func (d graphCRDRoot) GetResourceGraph(context.Context, string, string, string, string) (domainresource.ResourceGraph, error) {
	return d.graph, nil
}

type graphCRDRootAgent struct {
	GenericResourceAgent
	graph domainresource.ResourceGraph
}

func (d graphCRDRootAgent) GetResourceGraph(context.Context, string, string, string) (domainresource.ResourceGraph, error) {
	return d.graph, nil
}

func graphCRDFixture(kind string, count int) domainresource.ResourceGraph {
	graph := domainresource.ResourceGraph{ClusterID: "cluster-a", RootID: "root", Nodes: []domainresource.ResourceGraphNode{{ID: "root", Resource: domainresource.ResourceRef{ClusterID: "cluster-a", APIVersion: "v1", Kind: "Pod", Name: "pod", Namespace: "team", UID: "pod-uid"}}}, Edges: []domainresource.ResourceGraphEdge{}, Evidence: []domainresource.ResourceEvidence{}, Warnings: []string{}}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("owner-%02d", i)
		id := "custom-" + name
		graph.Nodes = append(graph.Nodes, domainresource.ResourceGraphNode{ID: id, Status: "unresolved", Resource: domainresource.ResourceRef{ClusterID: "cluster-a", APIVersion: "example.io/v1", Kind: kind, Namespace: "team", Name: name, UID: name + "-uid"}})
		graph.Edges = append(graph.Edges, domainresource.ResourceGraphEdge{ID: "controls-" + name, SourceID: id, TargetID: "root", Relation: "controls"})
	}
	return graph
}
func graphCRDView(kind string) domainresource.CRDView {
	return domainresource.CRDView{UID: "crd-uid", Name: "widgets.example.io", Group: "example.io", Kind: kind, Plural: "widgets", Scope: "Namespaced", Version: "v1", Versions: []string{"v1"}}
}
func graphCRDService(mode domaincluster.ConnectionMode, graph domainresource.ResourceGraph, state *graphCRDObservation, permissions domainaccess.Authorizer, saved bool) *GenericResources {
	connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: mode}, Metadata: map[string]any{}}
	if saved {
		connection.Metadata["agent_custom_resource_rules"] = []domaincluster.AgentCustomResourceRule{{APIGroup: "example.io", Resources: []string{"widgets"}, Verbs: []string{"get"}, Namespaces: []string{"team"}}}
	}
	access := &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: permissions, audit: discardAuditRecorder{}}
	custom := &CustomResources{resourceAccess: access, direct: graphCRDDirect{s: state}, agent: func(domaincluster.Connection) (CustomResourceAgent, error) { return graphCRDAgent{s: state}, nil }}
	return &GenericResources{resourceAccess: access, customResources: custom, resourceGraph: graphCRDRoot{graph: graph}, agent: func(domaincluster.Connection) (GenericResourceAgent, error) {
		return graphCRDRootAgent{graph: graph}, nil
	}}
}
func graphCRDPermissions() exactPermissionAuthorizer {
	return exactPermissionAuthorizer{appaccess.PermPlatformPodsView: true, appaccess.PermPlatformExtensionsView: true}
}
func graphCRDRun(ctx context.Context, service *GenericResources) (domainresource.ResourceGraph, error) {
	return service.GetResourceGraph(ctx, domainidentity.Principal{UserID: "reader"}, "cluster-a", "team", "Pod", "pod")
}

func TestResourceGraphCRDDirectAgentParityAndMetadataOnly(t *testing.T) {
	var prior domainresource.ResourceGraph
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView("Widget")}, review: []string{"view"}}
		graph, err := graphCRDRun(context.Background(), graphCRDService(mode, graphCRDFixture("Widget", 1), state, graphCRDPermissions(), true))
		if err != nil || state.reads != 1 || state.lists != 1 || len(graph.Nodes) != 3 || len(graph.Edges) != 2 {
			t.Fatalf("graph=%+v reads=%d lists=%d err=%v", graph, state.reads, state.lists, err)
		}
		if mode == domaincluster.ConnectionModeAgent && state.reviews != 1 {
			t.Fatal("Agent named SSAR not required")
		}
		encoded, _ := json.Marshal(graph)
		if strings.Contains(string(encoded), "secret-payload") || strings.Contains(string(encoded), "token") {
			t.Fatal("custom payload leaked")
		}
		if prior.RootID != "" && (!reflect.DeepEqual(prior.Nodes, graph.Nodes) || !reflect.DeepEqual(prior.Edges, graph.Edges)) {
			t.Fatal("direct/Agent observations diverged")
		}
		prior = graph
	}
}

func TestResourceGraphCRDAuthorizationBeforeRead(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       domaincluster.ConnectionMode
		permission string
		saved      bool
		review     []string
	}{
		{"definition denied", domaincluster.ConnectionModeDirectKubeconfig, "definition", true, nil},
		{"custom denied", domaincluster.ConnectionModeDirectKubeconfig, "custom", true, nil},
		{"Agent saved grant missing", domaincluster.ConnectionModeAgent, "", false, []string{"view"}},
		{"Agent named SSAR denied", domaincluster.ConnectionModeAgent, "", true, []string{}},
		{"custom Pod cannot inherit builtin permission", domaincluster.ConnectionModeDirectKubeconfig, "custom", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := "Widget"
			if strings.HasPrefix(tc.name, "custom Pod") {
				kind = "Pod"
			}
			state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView(kind)}, review: tc.review}
			permissions := graphCRDPermissions()
			if tc.permission == "definition" {
				delete(permissions, appaccess.PermPlatformExtensionsView)
			}
			authorizer := &graphCRDScopedAuthorizer{permissions: permissions, denyCustom: tc.permission == "custom"}
			graph, err := graphCRDRun(context.Background(), graphCRDService(tc.mode, graphCRDFixture(kind, 1), state, authorizer, tc.saved))
			if err != nil || state.reads != 0 || len(graph.Nodes) != 1 || len(graph.Edges) != 0 {
				t.Fatalf("unauthorized owner leaked/read: %+v reads=%d err=%v", graph, state.reads, err)
			}
		})
	}
}

func TestResourceGraphCRDRejectsUnobservedIdentityAndScope(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*domainresource.CRDView, *graphCRDObservation)
		wantReads int
	}{
		{"other group", func(v *domainresource.CRDView, _ *graphCRDObservation) { v.Group = "other.io" }, 0},
		{"unserved version", func(v *domainresource.CRDView, _ *graphCRDObservation) { v.Versions = []string{"v2"}; v.Version = "v1" }, 0},
		{"no plural", func(v *domainresource.CRDView, _ *graphCRDObservation) { v.Plural = "" }, 0},
		{"unknown scope", func(v *domainresource.CRDView, _ *graphCRDObservation) { v.Scope = "Unknown" }, 0},
		{"changed UID", func(_ *domainresource.CRDView, s *graphCRDObservation) {
			s.content = `{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"owner-00","namespace":"team","uid":"replacement"}}`
		}, 1},
		{"foreign namespace", func(_ *domainresource.CRDView, s *graphCRDObservation) {
			s.content = `{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"owner-00","namespace":"foreign","uid":"owner-00-uid"}}`
		}, 1},
		{"read failure", func(_ *domainresource.CRDView, s *graphCRDObservation) {
			s.readError = errors.New("private detail owner-00")
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := graphCRDView("Widget")
			state := &graphCRDObservation{}
			tc.mutate(&v, state)
			state.views = []domainresource.CRDView{v}
			graph, err := graphCRDRun(context.Background(), graphCRDService(domaincluster.ConnectionModeDirectKubeconfig, graphCRDFixture("Widget", 1), state, graphCRDPermissions(), true))
			if err != nil || state.reads != tc.wantReads || len(graph.Nodes) != 1 {
				t.Fatalf("invalid identity resolved: %+v %v", graph, err)
			}
			encoded, _ := json.Marshal(graph)
			if strings.Contains(string(encoded), "private detail") || strings.Contains(string(encoded), "owner-00") {
				t.Fatalf("unauthorized details escaped: %s", encoded)
			}
		})
	}
}

func TestResourceGraphCRDClusterScopeUsesEmptyNamespace(t *testing.T) {
	view := graphCRDView("Widget")
	view.Scope = "Cluster"
	state := &graphCRDObservation{views: []domainresource.CRDView{view}}
	graph, err := graphCRDRun(context.Background(), graphCRDService(domaincluster.ConnectionModeDirectKubeconfig, graphCRDFixture("Widget", 1), state, graphCRDPermissions(), true))
	if err != nil || state.namespace != "" || state.reads != 1 || len(graph.Nodes) != 3 {
		t.Fatalf("cluster owner=%+v readns=%q err=%v", graph, state.namespace, err)
	}
	for _, node := range graph.Nodes {
		if node.Resource.Kind == "Widget" && (node.Resource.Namespace != "" || node.Resource.ScopeMode != domainresource.ResourceScopeModeCluster) {
			t.Fatal("cluster owner retained guessed namespace")
		}
	}
}

func TestResourceGraphCRDBudgetsAndCancellation(t *testing.T) {
	state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView("Widget")}}
	graph, err := graphCRDRun(context.Background(), graphCRDService(domaincluster.ConnectionModeDirectKubeconfig, graphCRDFixture("Widget", 20), state, graphCRDPermissions(), true))
	if err != nil || state.reads != 16 || len(graph.Warnings) == 0 {
		t.Fatalf("read budget violated: reads=%d graph=%+v err=%v", state.reads, graph, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	state = &graphCRDObservation{views: []domainresource.CRDView{graphCRDView("Widget")}, cancel: cancel}
	_, err = graphCRDRun(ctx, graphCRDService(domaincluster.ConnectionModeDirectKubeconfig, graphCRDFixture("Widget", 1), state, graphCRDPermissions(), true))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
	state = &graphCRDObservation{}
	graph, err = graphCRDRun(context.Background(), graphCRDService(domaincluster.ConnectionModeDirectKubeconfig, graphCRDFixture("Widget", 0), state, graphCRDPermissions(), true))
	if err != nil || state.lists != 0 || state.reads != 0 || len(graph.Nodes) != 1 {
		t.Fatalf("no-owner graph caused discovery: %+v %v", graph, err)
	}
}

func TestResourceGraphCRDAuthorizationIsRequestLocal(t *testing.T) {
	state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView("Pod")}}
	permissions := &graphCRDScopedAuthorizer{permissions: graphCRDPermissions()}
	service := graphCRDService(domaincluster.ConnectionModeDirectKubeconfig, graphCRDFixture("Pod", 1), state, permissions, true)
	first, err := graphCRDRun(context.Background(), service)
	if err != nil || len(first.Nodes) != 3 || state.reads != 1 {
		t.Fatalf("custom Pod did not use extensions permission: %+v %v", first, err)
	}
	permissions.denyCustom = true
	second, err := graphCRDRun(context.Background(), service)
	if err != nil || len(second.Nodes) != 1 || state.reads != 1 {
		t.Fatalf("previous request authorization leaked: %+v %v", second, err)
	}
}

func TestResourceGraphCRDMetadataDropsUnknownPayloadAndRejectsOversize(t *testing.T) {
	ref := domainresource.ResourceRef{ClusterID: "cluster-a", APIVersion: "example.io/v1", Kind: "Widget", Namespace: "team", Name: "owner", UID: "owner-uid"}
	content := []byte(`{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"owner","namespace":"team","uid":"owner-uid","annotations":{"token":"private-payload"}},"spec":{"password":"private-payload"},"status":{"password":"private-payload"}}`)
	projected, _, err := graphCustomMetadata(content, ref, "team")
	if err != nil || strings.Contains(string(projected), "private-payload") || strings.Contains(string(projected), "annotations") || strings.Contains(string(projected), "spec") || strings.Contains(string(projected), "status") {
		t.Fatalf("unsafe metadata projection: %s %v", projected, err)
	}
	if _, _, err := graphCustomMetadata([]byte(strings.Repeat("x", (1<<20)+1)), ref, "team"); err == nil {
		t.Fatal("oversized CR input accepted")
	}
}

func TestResourceGraphCRDRejectsOversizedDiscoveryAndMissingDefinitionUID(t *testing.T) {
	for _, tc := range []struct {
		name  string
		views []domainresource.CRDView
	}{
		{"oversized discovery", make([]domainresource.CRDView, graphCRDDefinitionLimit+1)},
		{"missing definition UID", []domainresource.CRDView{{Name: "widgets.example.io", Group: "example.io", Kind: "Widget", Plural: "widgets", Scope: "Namespaced", Versions: []string{"v1"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &graphCRDObservation{views: tc.views}
			graph, err := graphCRDRun(context.Background(), graphCRDService(domaincluster.ConnectionModeDirectKubeconfig, graphCRDFixture("Widget", 1), state, graphCRDPermissions(), true))
			if err != nil || state.reads != 0 || len(graph.Nodes) != 1 || len(graph.Warnings) == 0 {
				t.Fatalf("unbounded/unverified discovery used: %+v %v reads=%d", graph, err, state.reads)
			}
		})
	}
}

func (d graphCRDDirect) ListResourceGraphCRDs(ctx context.Context, cluster string) ([]domainresource.CRDView, error) {
	return d.ListCRDs(ctx, cluster)
}

type graphLegacyCRDDirect struct{ DirectCustomResource }

func TestResourceGraphCRDMissingBoundedDiscoveryPortIsPartial(t *testing.T) {
	state := &graphCRDObservation{views: []domainresource.CRDView{graphCRDView("Widget")}}
	service := graphCRDService(domaincluster.ConnectionModeDirectKubeconfig, graphCRDFixture("Widget", 1), state, graphCRDPermissions(), true)
	service.customResources.direct = graphLegacyCRDDirect{DirectCustomResource: graphCRDDirect{s: state}}
	graph, err := graphCRDRun(context.Background(), service)
	if err != nil || state.reads != 0 || state.lists != 0 || len(graph.Nodes) != 1 || len(graph.Warnings) == 0 {
		t.Fatalf("unbounded discovery fallback used: %+v %v", graph, err)
	}
}

// Policies can deny a custom kind while allowing definition discovery with the
// same catalog permission. Preserve the real Kind and Group for that decision.
type graphCRDScopedAuthorizer struct {
	permissions exactPermissionAuthorizer
	denyCustom  bool
}

func (a *graphCRDScopedAuthorizer) Authorize(ctx context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	if request.Resource.Group == "extensions" && request.Resource.Kind != "CustomResourceDefinition" {
		if request.PermissionKey != appaccess.PermPlatformExtensionsView {
			return domainaccess.Decision{Allowed: false}, nil
		}
		if a.denyCustom {
			return domainaccess.Decision{Allowed: false}, nil
		}
	}
	return a.permissions.Authorize(ctx, request)
}
func TestResourceGraphCRDFixtureUsesAssignableCatalogPermissions(t *testing.T) {
	for permission := range graphCRDPermissions() {
		if !appaccess.IsActiveAssignablePermission(permission) {
			t.Fatalf("fixture uses nonexistent or inactive permission %s", permission)
		}
	}
}
