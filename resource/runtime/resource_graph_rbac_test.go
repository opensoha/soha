package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestResourceGraphRBACCollectorRoots(t *testing.T) {
	ns := "team"
	account := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: ns, UID: "account-uid"}}
	foreign := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "other", UID: "foreign-uid"}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: ns, UID: "pod-uid"},
		Spec: corev1.PodSpec{
			ServiceAccountName: account.Name,
			ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "registry-token"}},
		},
	}
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: ns, UID: "role-uid"}}
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "reader", UID: "cluster-role-uid"},
		AggregationRule: &rbacv1.AggregationRule{
			ClusterRoleSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"tier": "reader"}}},
		},
	}
	selectedRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "selected", UID: "selected-role-uid", Labels: map[string]string{"tier": "reader"}},
	}
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "read", Namespace: ns, UID: "binding-uid"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: account.Name},
			{Kind: "ServiceAccount", Name: account.Name, Namespace: ns},
			{Kind: "ServiceAccount", Name: foreign.Name, Namespace: foreign.Namespace},
			{Kind: "User", Name: "alice", APIGroup: rbacv1.GroupName},
			{Kind: "Group", Name: "operators", APIGroup: rbacv1.GroupName},
		},
	}
	clusterBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-read", UID: "cluster-binding-uid"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: clusterRole.Name},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: account.Name, Namespace: ns}},
	}
	foreignBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "foreign-read", Namespace: "other", UID: "foreign-binding-uid"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: clusterRole.Name},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: account.Name, Namespace: ns}},
	}
	for _, tc := range []struct {
		kind, name, namespace string
		calls                 []string
		edges                 []string
	}{
		{"ServiceAccount", "app", ns, []string{"get serviceaccounts/team/app", "list clusterrolebindings/", "list pods/team", "list rolebindings/team"}, []string{
			"binds-subject:cluster-read:team/app:subjects[0]:resolved",
			"binds-subject:read:team/app:subjects[0]:resolved",
			"uses-service-account:worker:team/app:spec.serviceAccountName:resolved",
		}},
		{"Role", "reader", ns, []string{"get roles/team/reader", "list rolebindings/team"}, []string{"binds-role:read:team/reader:roleRef:resolved"}},
		{"RoleBinding", "read", ns, []string{"get rolebindings/team/read", "list clusterroles/", "list roles/team", "list serviceaccounts/team", "get serviceaccounts/other/app"}, []string{
			"binds-role:read:team/reader:roleRef:resolved",
			"binds-subject:read:team/app:subjects[0]:resolved",
			"binds-subject:read:other/app:subjects[2]:resolved",
		}},
		{"ClusterRole", "reader", "", []string{"get clusterroles//reader", "list clusterroles/", "list clusterrolebindings/", "list rolebindings/"}, []string{
			"aggregates-role:reader:/selected:aggregationRule.clusterRoleSelectors[0]:resolved",
			"binds-role:cluster-read:/reader:roleRef:resolved",
			"binds-role:foreign-read:/reader:roleRef:resolved",
		}},
		{"ClusterRoleBinding", "cluster-read", "", []string{"get clusterrolebindings//cluster-read", "list clusterroles/", "list serviceaccounts/"}, []string{
			"aggregates-role:reader:/selected:aggregationRule.clusterRoleSelectors[0]:resolved",
			"binds-role:cluster-read:/reader:roleRef:resolved",
			"binds-subject:cluster-read:team/app:subjects[0]:resolved",
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			client := fake.NewClientset(account, foreign, pod, role, clusterRole, selectedRole, binding, clusterBinding, foreignBinding)
			api := graphRBACAPI
			if tc.kind == "ServiceAccount" {
				api = "v1"
			}
			graph, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), client, nil, cr.ResourceRef{
				ClusterID: "cluster", APIVersion: api, Kind: tc.kind, Namespace: tc.namespace, Name: tc.name, ScopeMode: topologyScope(tc.namespace != ""),
			})
			if err != nil {
				t.Fatal(err)
			}
			assertStorageGraphActions(t, client, tc.calls)
			nodes := storageGraphNodes(graph)
			root := nodes[graph.RootID].Resource
			if root.Kind != tc.kind || root.APIVersion != api || root.Name != tc.name || root.Namespace != tc.namespace || root.UID == "" {
				t.Fatalf("unexpected root: %+v", root)
			}
			if len(graph.Edges) != len(tc.edges) || graphTestHas(graph, "alice") || graphTestHas(graph, "operators") || graphTestHas(graph, "registry-token") {
				t.Fatalf("unexpected graph: %+v", graph)
			}
			edges := map[string]bool{}
			for _, edge := range graph.Edges {
				source, target := nodes[edge.SourceID].Resource, nodes[edge.TargetID].Resource
				edges[edge.Relation+":"+source.Name+":"+target.Namespace+"/"+target.Name+":"+edge.FieldPath+":"+edge.ResolutionState] = true
				if (target.UID != "") != (edge.ResolutionState == "resolved") {
					t.Fatalf("target observation mismatch: %+v / %+v", target, edge)
				}
			}
			for _, expected := range tc.edges {
				if !edges[expected] {
					t.Errorf("missing %q in %v", expected, edges)
				}
			}
			scopeLimited := strings.Contains(strings.Join(graph.Warnings, "; "), "RoleBinding source limited")
			if scopeLimited != (tc.kind == "ServiceAccount") {
				t.Fatalf("unexpected scope coverage: %v", graph.Warnings)
			}
		})
	}
}

func TestResourceGraphRBACRootValidationAndPartialSources(t *testing.T) {
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "read", Namespace: "team", UID: "binding-uid"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "reader"},
	}
	root := cr.ResourceRef{ClusterID: "cluster", APIVersion: graphRBACAPI, Kind: "RoleBinding", Name: binding.Name, Namespace: binding.Namespace}
	for _, mutate := range []func(*cr.ResourceRef){
		func(r *cr.ResourceRef) { r.APIVersion = "rbac.authorization.k8s.io/v1beta1" },
		func(r *cr.ResourceRef) { r.ScopeMode = cr.ResourceScopeModeCluster },
		func(r *cr.ResourceRef) { r.Namespace = "" },
		func(r *cr.ResourceRef) { r.Kind = "ClusterRoleBinding" },
		func(r *cr.ResourceRef) { r.Name = "" },
	} {
		input := root
		mutate(&input)
		client := fake.NewClientset(binding)
		if _, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), client, nil, input); !errors.Is(err, ErrInvalidResourceGraphRoot) || len(client.Actions()) != 0 {
			t.Fatalf("invalid root did not fail before reads: input=%+v err=%v actions=%v", input, err, client.Actions())
		}
	}
	client := fake.NewClientset(binding)
	stale := root
	stale.UID = "old"
	if _, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), client, nil, stale); !errors.Is(err, ErrInvalidResourceGraphRoot) {
		t.Fatalf("stale root UID: %v", err)
	}
	assertStorageGraphActions(t, client, []string{"get rolebindings/team/read"})
	client = fake.NewClientset(binding)
	client.PrependReactor("list", "roles", func(ktesting.Action) (bool, kruntime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: rbacv1.GroupName, Resource: "roles"}, "", errors.New("private failure detail"))
	})
	client.PrependReactor("list", "serviceaccounts", func(ktesting.Action) (bool, kruntime.Object, error) {
		return true, &corev1.ServiceAccountList{ListMeta: metav1.ListMeta{Continue: "private-token"}}, nil
	})
	graph, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), client, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	assertStorageGraphActions(t, client, []string{"get rolebindings/team/read", "list clusterroles/", "list roles/team", "list serviceaccounts/team"})
	warnings := strings.Join(graph.Warnings, "; ")
	if !strings.Contains(warnings, "Role source Forbidden") || !strings.Contains(warnings, "ServiceAccount source truncated at 500 objects") || strings.Contains(warnings, "private") {
		t.Fatalf("unexpected partial coverage: %s", warnings)
	}
	if len(graph.Edges) != 1 || graph.Edges[0].ResolutionState != "unresolved" {
		t.Fatalf("inaccessible Role should remain unresolved: %+v", graph)
	}
}

func TestResourceGraphRBACRejectsMalformedDeclarations(t *testing.T) {
	roleRef := map[string]interface{}{"apiGroup": rbacv1.GroupName, "kind": "Role", "name": "reader"}
	account := graphTestObject("v1", "ServiceAccount", "reader", "account-uid")
	for _, tc := range []struct {
		kind, namespace string
		role            map[string]interface{}
	}{
		{"ClusterRoleBinding", "", roleRef},
		{"RoleBinding", "ns", map[string]interface{}{"apiGroup": "custom.example", "kind": "Role", "name": "reader"}},
		{"RoleBinding", "ns", map[string]interface{}{"apiGroup": rbacv1.GroupName, "kind": "role", "name": "reader"}},
		{"RoleBinding", "ns", map[string]interface{}{"apiGroup": rbacv1.GroupName, "kind": "Role", "name": "ns/reader"}},
		{"RoleBinding", "ns", map[string]interface{}{"apiGroup": rbacv1.GroupName, "kind": "Role", "name": ""}},
	} {
		binding := graphTestObject(graphRBACAPI, tc.kind, "read", "binding-uid")
		binding.SetNamespace(tc.namespace)
		binding.Object["roleRef"] = tc.role
		subjects := []interface{}{
			map[string]interface{}{"kind": "ServiceAccount", "name": "reader", "namespace": "ns", "apiGroup": rbacv1.GroupName},
			map[string]interface{}{"kind": "ServiceAccount", "name": "reader", "namespace": "ns/other"},
			map[string]interface{}{"kind": "ServiceAccount", "name": "reader", "namespace": int64(5)},
			map[string]interface{}{"kind": "ServiceAccount", "name": "ns/reader", "namespace": "ns"},
		}
		if tc.namespace == "" {
			subjects = append(subjects, map[string]interface{}{"kind": "ServiceAccount", "name": "reader"})
		}
		binding.Object["subjects"] = subjects
		graph, err := BuildResourceGraphFromObjects(cr.ResourceRef{
			ClusterID: "cluster", APIVersion: graphRBACAPI, Kind: tc.kind, Name: binding.GetName(), Namespace: tc.namespace,
		}, []*unstructured.Unstructured{binding, account})
		if err != nil || len(graph.Edges) != 0 || len(graph.Nodes) != 1 {
			t.Fatalf("malformed declaration matched for %v: graph=%+v err=%v", tc, graph, err)
		}
	}
}

func TestResourceGraphRBACCrossNamespaceSubjectBudget(t *testing.T) {
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "read", Namespace: "team"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "reader"},
	}
	objects := []kruntime.Object{
		binding,
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: "foreign", UID: "foreign-role"}},
	}
	calls := []string{"get rolebindings/team/read", "list clusterroles/", "list roles/team", "list serviceaccounts/team"}
	for i := range 17 {
		name := fmt.Sprintf("account-%02d", i)
		subject := rbacv1.Subject{Kind: "ServiceAccount", Namespace: "foreign", Name: name}
		binding.Subjects = append(binding.Subjects, subject)
		if i == 0 {
			binding.Subjects = append(binding.Subjects, subject)
		}
		if i < 16 {
			calls = append(calls, "get serviceaccounts/foreign/"+name)
		}
		if i != 2 {
			objects = append(objects, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "foreign", UID: "uid"}})
		}
	}
	client := fake.NewClientset(objects...)
	client.PrependReactor("get", "serviceaccounts", func(action ktesting.Action) (bool, kruntime.Object, error) {
		get, ok := action.(ktesting.GetAction)
		if !ok {
			t.Fatalf("unexpected service account action: %+v", action)
		}
		switch get.GetName() {
		case "account-01":
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "serviceaccounts"}, "private-name", errors.New("private-cause"))
		case "account-03":
			return true, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "account-03", Namespace: "wrong"}}, nil
		default:
			return false, nil, nil
		}
	})
	graph, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), client, nil, cr.ResourceRef{
		ClusterID: "cluster", APIVersion: graphRBACAPI, Kind: "RoleBinding", Name: binding.Name, Namespace: binding.Namespace,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStorageGraphActions(t, client, calls)
	warnings := strings.Join(graph.Warnings, "; ")
	for _, marker := range []string{"reference Forbidden", "reference NotFound", "identity unverified", "truncated at 16 targets"} {
		if !strings.Contains(warnings, marker) {
			t.Errorf("missing coverage warning %q: %s", marker, warnings)
		}
	}
	if strings.Contains(warnings, "private") || len(graph.Edges) != 18 {
		t.Fatalf("unexpected graph coverage or edges: %+v", graph)
	}
	nodes := storageGraphNodes(graph)
	unresolved := map[string]bool{"account-01": true, "account-02": true, "account-03": true, "account-16": true}
	for _, edge := range graph.Edges {
		target := nodes[edge.TargetID]
		switch edge.Relation {
		case ResourceGraphRelationBindsRole:
			if target.Resource.Namespace != "team" || target.Status != "unresolved" {
				t.Fatalf("Role resolved outside binding namespace: %+v", target)
			}
		case ResourceGraphRelationBindsSubject:
			if target.Resource.Namespace != "foreign" || (target.Status == "unresolved") != unresolved[target.Resource.Name] ||
				(edge.ResolutionState == "unresolved") != unresolved[target.Resource.Name] {
				t.Fatalf("unexpected ServiceAccount observation: target=%+v edge=%+v", target, edge)
			}
		default:
			t.Fatalf("unexpected edge: %+v", edge)
		}
	}
}

func TestResourceGraphRBACColonInUnresolvedRoleName(t *testing.T) {
	binding := graphTestObject(graphRBACAPI, "RoleBinding", "binding", "binding-uid")
	binding.Object["roleRef"] = map[string]interface{}{"apiGroup": rbacv1.GroupName, "kind": "ClusterRole", "name": "reader:abc"}
	observed := graphTestObject(graphRBACAPI, "ClusterRole", "reader", "abc")
	observed.SetNamespace("")
	graph, err := BuildResourceGraphFromObjects(graphTestRoot("RoleBinding", "binding"), []*unstructured.Unstructured{binding, observed})
	if err != nil || len(graph.Edges) != 1 || len(graph.Nodes) != 2 {
		t.Fatalf("unexpected graph: %+v / %v", graph, err)
	}
	edge := graph.Edges[0]
	target := storageGraphNodes(graph)[edge.TargetID]
	if edge.ResolutionState != "unresolved" || target.Resource.Name != "reader:abc" || target.Resource.UID != "" || target.Status != "unresolved" ||
		edge.TargetID != graphRBACAPI+":ClusterRole:/reader%3Aabc" {
		t.Fatalf("unresolved role aliased the observed role: edge=%+v target=%+v", edge, target)
	}
	ref := cr.ResourceRef{APIVersion: graphRBACAPI, Kind: "ClusterRole", Name: "system:reader", UID: "abc%3A:def"}
	if got := graphNodeID(ref); got != graphRBACAPI+":ClusterRole:/system%3Areader:abc%253A%3Adef" {
		t.Fatalf("node ID did not encode separators unambiguously: %s", got)
	}
}

func TestResourceGraphRBACPreservesOrdinaryPodAccountEdge(t *testing.T) {
	pod := graphTestObject("v1", "Pod", "pod", "pod-uid")
	pod.Object["spec"] = map[string]interface{}{"serviceAccountName": "app"}
	account := graphTestObject("v1", "ServiceAccount", "app", "account-uid")
	graph, err := BuildResourceGraphFromObjects(graphTestRoot("Pod", "pod"), []*unstructured.Unstructured{pod, account})
	if err != nil || len(graph.Edges) != 1 {
		t.Fatalf("unexpected Pod graph: %+v / %v", graph, err)
	}
	edge := graph.Edges[0]
	if edge.Relation != ResourceGraphRelationUsesServiceAccount || edge.FieldPath != "" || edge.ResolutionState != "" {
		t.Fatalf("ordinary Pod edge changed: %+v", edge)
	}
}

func TestResourceGraphRBACAggregation(t *testing.T) {
	aggregate := graphTestObject(graphRBACAPI, "ClusterRole", "aggregate", "aggregate-uid")
	aggregate.SetNamespace("")
	aggregate.SetLabels(map[string]string{"tier": "yes"})
	selected := graphTestObject(graphRBACAPI, "ClusterRole", "selected", "selected-uid")
	selected.SetNamespace("")
	selected.SetLabels(map[string]string{"tier": "yes"})
	unmatched := graphTestObject(graphRBACAPI, "ClusterRole", "unmatched", "unmatched-uid")
	unmatched.SetNamespace("")
	custom := selected.DeepCopy()
	custom.SetAPIVersion("custom.example/v1")
	custom.SetName("custom")
	misscoped := selected.DeepCopy()
	misscoped.SetNamespace("ns")
	misscoped.SetName("misscoped")
	expression := map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "tier", "operator": "In", "values": []interface{}{"yes"}}}}
	for _, tc := range []struct {
		name      string
		selectors []interface{}
		want      int
	}{
		{"expressions-and-labels-deduplicate", []interface{}{expression, map[string]interface{}{"matchLabels": map[string]interface{}{"tier": "yes"}}}, 1},
		{"explicit-empty-selects-other-observed-roles", []interface{}{map[string]interface{}{}}, 2},
		{"no-selectors", nil, 0},
		{"null-is-not-an-empty-selector", []interface{}{expression, nil}, 0},
		{"unknown-field-is-not-an-empty-selector", []interface{}{map[string]interface{}{"matchLabel": map[string]interface{}{"tier": "yes"}}}, 0},
		{"invalid-expression", []interface{}{expression, map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "tier", "operator": "In"}}}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aggregate.Object["aggregationRule"] = map[string]interface{}{"clusterRoleSelectors": tc.selectors}
			graph, err := BuildResourceGraphFromObjects(cr.ResourceRef{
				ClusterID: "cluster", APIVersion: graphRBACAPI, Kind: "ClusterRole", Name: aggregate.GetName(),
			}, []*unstructured.Unstructured{aggregate, selected, unmatched, custom, misscoped})
			if err != nil || len(graph.Edges) != tc.want || len(graph.Nodes) != tc.want+1 {
				t.Fatalf("unexpected aggregation: graph=%+v err=%v", graph, err)
			}
			for _, edge := range graph.Edges {
				if edge.SourceID != graph.RootID || edge.Relation != ResourceGraphRelationAggregatesRole || edge.ResolutionState != "resolved" || edge.FieldPath != "aggregationRule.clusterRoleSelectors[0]" {
					t.Fatalf("unexpected aggregation edge: %+v", edge)
				}
			}
			if tc.want == 1 && !graphTestHas(graph, selected.GetName()) || graphTestHas(graph, custom.GetName()) || graphTestHas(graph, misscoped.GetName()) {
				t.Fatalf("incorrect GVK, scope or selector match: %+v", graph.Nodes)
			}
		})
	}
}
