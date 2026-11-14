package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	cr "github.com/opensoha/soha/contracts/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func graphTestObject(api, kind, name, uid string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": api, "kind": kind, "metadata": map[string]interface{}{"name": name, "namespace": "ns", "uid": uid}}}
	return o
}
func graphTestRoot(kind, name string) cr.ResourceRef {
	return cr.ResourceRef{ClusterID: "cluster-a", Kind: kind, Name: name, Namespace: "ns", ScopeMode: cr.ResourceScopeModeNamespace}
}
func graphTestEdge(graph cr.ResourceGraph, relation, source, target string) bool {
	nodes := map[string]string{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = node.Resource.Name
	}
	for _, edge := range graph.Edges {
		if edge.Relation == relation && nodes[edge.SourceID] == source && nodes[edge.TargetID] == target {
			return true
		}
	}
	return false
}
func graphTestHas(graph cr.ResourceGraph, name string) bool {
	for _, node := range graph.Nodes {
		if node.Resource.Name == name {
			return true
		}
	}
	return false
}
func graphTestOwner(o *unstructured.Unstructured, api, kind, name, uid string, controller bool) {
	o.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: api, Kind: kind, Name: name, UID: types.UID(uid), Controller: &controller}})
}

func TestResourceGraphOwnerIdentityAndUnknownMetadata(t *testing.T) {
	for _, controller := range []bool{false, true} {
		t.Run(fmt.Sprint(controller), func(t *testing.T) {
			owner := graphTestObject("custom.example/v1", "CustomOwner", "owner", "owner-new")
			pod := graphTestObject("v1", "Pod", "pod", "pod-uid")
			graphTestOwner(pod, "custom.example/v1", "CustomOwner", "owner", "owner-old", controller)
			graph, err := BuildResourceGraphFromObjects(graphTestRoot("Pod", "pod"), []*unstructured.Unstructured{owner, pod})
			if err != nil {
				t.Fatal(err)
			}
			relation := "owns"
			if controller {
				relation = "controls"
			}
			if !graphTestEdge(graph, relation, "owner", "pod") {
				t.Fatalf("missing owner edge: %+v", graph)
			}
			for _, node := range graph.Nodes {
				if node.Resource.Name == "owner" && (node.Status != "unresolved" || node.Resource.UID != "owner-old") {
					t.Fatalf("stale owner incorrectly resolved: %+v", node)
				}
			}
			graphTestOwner(pod, "custom.example/v1", "CustomOwner", "owner", "owner-new", controller)
			graph, err = BuildResourceGraphFromObjects(graphTestRoot("Pod", "pod"), []*unstructured.Unstructured{owner, pod})
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range graph.Nodes {
				if node.Resource.Name == "owner" && node.Status == "unresolved" {
					t.Fatal("matching owner not resolved")
				}
			}
		})
	}
}
func TestResourceGraphEightWorkloadsAndPodSources(t *testing.T) {
	for _, kind := range []string{"Deployment", "Pod", "StatefulSet", "DaemonSet", "Job", "CronJob", "ReplicaSet", "ReplicationController"} {
		t.Run(kind, func(t *testing.T) {
			api := "apps/v1"
			if kind == "Pod" || kind == "ReplicationController" {
				api = "v1"
			}
			if kind == "Job" || kind == "CronJob" {
				api = "batch/v1"
			}
			o := graphTestObject(api, kind, "root", "root-uid")
			spec := map[string]interface{}{
				"serviceAccountName": "account", "nodeName": "node",
				"imagePullSecrets": []interface{}{map[string]interface{}{"name": "pull"}},
				"containers":       []interface{}{map[string]interface{}{"name": "main", "env": []interface{}{map[string]interface{}{"valueFrom": map[string]interface{}{"configMapKeyRef": map[string]interface{}{"name": "config"}}}}}},
				"initContainers":   []interface{}{map[string]interface{}{"name": "init", "envFrom": []interface{}{map[string]interface{}{"secretRef": map[string]interface{}{"name": "init-secret"}}}}},
				"volumes":          []interface{}{map[string]interface{}{"persistentVolumeClaim": map[string]interface{}{"claimName": "claim"}}, map[string]interface{}{"projected": map[string]interface{}{"sources": []interface{}{map[string]interface{}{"secret": map[string]interface{}{"name": "projected"}}, map[string]interface{}{"configMap": map[string]interface{}{"name": "projected-config"}}}}}},
			}
			path := []string{"spec", "template", "spec"}
			if kind == "Pod" {
				path = []string{"spec"}
			}
			if kind == "CronJob" {
				path = []string{"spec", "jobTemplate", "spec", "template", "spec"}
			}
			if err := unstructured.SetNestedMap(o.Object, spec, path...); err != nil {
				t.Fatal(err)
			}
			graph, err := BuildResourceGraphFromObjects(graphTestRoot(kind, "root"), []*unstructured.Unstructured{o})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"config", "init-secret", "claim", "account", "pull", "node", "projected", "projected-config"} {
				if !graphTestHas(graph, name) {
					t.Errorf("missing %s", name)
				}
			}
			if !graphTestEdge(graph, "mounts", "root", "claim") {
				t.Fatal("PVC relation token changed")
			}
		})
	}
}
func TestResourceGraphDirectionalBoundsAndSelectors(t *testing.T) {
	root := graphTestObject("apps/v1", "Deployment", "root", "root-uid")
	replica := graphTestObject("apps/v1", "ReplicaSet", "rs", "rs-uid")
	graphTestOwner(replica, "apps/v1", "Deployment", "root", "root-uid", true)
	pod := graphTestObject("v1", "Pod", "pod", "pod-uid")
	graphTestOwner(pod, "apps/v1", "ReplicaSet", "rs", "rs-uid", true)
	pod.SetLabels(map[string]string{"app": "a"})
	other := graphTestObject("v1", "Pod", "unrelated", "other-uid")
	other.SetLabels(map[string]string{"app": "a"})
	spec := map[string]interface{}{"volumes": []interface{}{map[string]interface{}{"configMap": map[string]interface{}{"name": "shared"}}}}
	_ = unstructured.SetNestedMap(pod.Object, spec, "spec")
	_ = unstructured.SetNestedMap(other.Object, spec, "spec")
	service := graphTestObject("v1", "Service", "svc", "svc-uid")
	_ = unstructured.SetNestedStringMap(service.Object, map[string]string{"app": "a"}, "spec", "selector")
	empty := graphTestObject("v1", "Service", "empty", "empty-uid")
	_ = unstructured.SetNestedMap(empty.Object, map[string]interface{}{}, "spec", "selector")
	graph, err := BuildResourceGraphFromObjects(graphTestRoot("Deployment", "root"), []*unstructured.Unstructured{root, replica, pod, other, service, empty})
	if err != nil {
		t.Fatal(err)
	}
	if graphTestHas(graph, "unrelated") || graphTestHas(graph, "empty") {
		t.Fatal("graph leaked through shared config or selector")
	}
	if !graphTestHas(graph, "shared") || !graphTestHas(graph, "svc") {
		t.Fatalf("missing third-level relationships: %+v", graph)
	}
	// Starting at Pod may ascend owners, but must not turn around into siblings.
	sibling := graphTestObject("v1", "Pod", "sibling", "sibling-uid")
	graphTestOwner(sibling, "apps/v1", "ReplicaSet", "rs", "rs-uid", true)
	graph, err = BuildResourceGraphFromObjects(graphTestRoot("Pod", "pod"), []*unstructured.Unstructured{root, replica, pod, sibling})
	if err != nil {
		t.Fatal(err)
	}
	if graphTestHas(graph, "sibling") {
		t.Fatal("ancestor traversal reversed into sibling")
	}
}
func TestResourceGraphLimitsRootIdentityAndProjection(t *testing.T) {
	root := graphTestObject("v1", "ConfigMap", "root", "root-uid")
	objects := []*unstructured.Unstructured{root}
	for i := 0; i < 600; i++ {
		child := graphTestObject("v1", "Pod", fmt.Sprintf("p-%03d", i), fmt.Sprintf("uid-%d", i))
		graphTestOwner(child, "v1", "ConfigMap", "root", "root-uid", true)
		objects = append(objects, child)
	}
	graph, err := BuildResourceGraphFromObjects(graphTestRoot("ConfigMap", "root"), objects)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 500 || len(graph.Edges) > 1000 || len(graph.Warnings) == 0 {
		t.Fatalf("limits not applied: %d/%d %+v", len(graph.Nodes), len(graph.Edges), graph.Warnings)
	}
	ref := graphTestRoot("ConfigMap", "root")
	ref.UID = "replaced"
	if _, err = BuildResourceGraphFromObjects(ref, objects); err == nil {
		t.Fatal("root UID mismatch accepted")
	}
	ref = graphTestRoot("Pod", "root")
	ref.Namespace = ""
	ref.ScopeMode = cr.ResourceScopeModeCluster
	if _, err = BuildResourceGraphFromObjects(ref, objects); err == nil {
		t.Fatal("namespace bypass accepted")
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret", Namespace: "ns", Annotations: map[string]string{"password": "hidden"}}, Data: map[string][]byte{"password": []byte("sensitive")}}
	projected, err := graphObjectProjection(secret, "v1", "Secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"data", "stringData", "spec"} {
		if _, ok := projected.Object[key]; ok {
			t.Fatalf("secret payload retained: %s", key)
		}
	}
	if len(projected.GetAnnotations()) > 0 {
		t.Fatal("annotations retained")
	}
}
func TestResourceGraphCollectorErrorsWarningsAndLimit(t *testing.T) {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "ns", UID: "root-uid"}}
	client := fake.NewSimpleClientset(deployment)
	client.PrependReactor("*", "secrets", func(ktesting.Action) (bool, kruntime.Object, error) {
		t.Error("Secret API must not be called")
		return true, nil, errors.New("Secret read prohibited")
	})
	client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, kruntime.Object, error) {
		list, ok := action.(interface{ GetListOptions() metav1.ListOptions })
		if !ok || list.GetListOptions().Limit != 500 {
			t.Error("list was not bounded")
		}
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("denied"))
	})
	_, secretErr := BuildKubernetesResourceGraph(context.Background(), client, graphTestRoot("Secret", "secret"))
	if secretErr == nil {
		t.Fatal("Secret root must require metadata client")
	}
	// A single-page response may be truncated; the independent root Get must survive.
	client.PrependReactor("list", "deployments", func(action ktesting.Action) (bool, kruntime.Object, error) {
		return true, &appsv1.DeploymentList{ListMeta: metav1.ListMeta{Continue: "next-page"}}, nil
	})
	graph, err := BuildKubernetesResourceGraph(context.Background(), client, graphTestRoot("Deployment", "root"))
	if err != nil {
		t.Fatal(err)
	}
	if !graphTestHas(graph, "root") {
		t.Fatal("root lost to source truncation")
	}
	warnings := strings.Join(graph.Warnings, "\n")
	if !strings.Contains(warnings, "Pod source Forbidden") || !strings.Contains(warnings, "Deployment source truncated") {
		t.Fatalf("missing warnings: %s", warnings)
	}
	_, err = BuildKubernetesResourceGraph(context.Background(), client, graphTestRoot("Deployment", "missing"))
	if !apierrors.IsNotFound(err) {
		t.Fatalf("root error not preserved: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = BuildKubernetesResourceGraph(ctx, client, graphTestRoot("Deployment", "root"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation swallowed: %v", err)
	}
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, kruntime.Object, error) { return true, nil, context.DeadlineExceeded })
	_, err = BuildKubernetesResourceGraph(context.Background(), client, graphTestRoot("Deployment", "root"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("source timeout swallowed: %v", err)
	}
}

func TestResourceGraphSupportingRulesAndUnknownSpec(t *testing.T) {
	pod := graphTestObject("v1", "Pod", "pod", "pod-uid")
	pod.SetLabels(map[string]string{"app": "a"})
	service := graphTestObject("v1", "Service", "svc", "svc-uid")
	_ = unstructured.SetNestedStringMap(service.Object, map[string]string{"app": "a"}, "spec", "selector")
	ingress := graphTestObject("networking.k8s.io/v1", "Ingress", "ingress", "ingress-uid")
	_ = unstructured.SetNestedMap(ingress.Object, map[string]interface{}{"service": map[string]interface{}{"name": "svc"}}, "spec", "defaultBackend")
	pdb := graphTestObject("policy/v1", "PodDisruptionBudget", "pdb", "pdb-uid")
	_ = unstructured.SetNestedMap(pdb.Object, map[string]interface{}{"matchLabels": map[string]interface{}{"app": "a"}}, "spec", "selector")
	hpa := graphTestObject("autoscaling/v2", "HorizontalPodAutoscaler", "hpa", "hpa-uid")
	_ = unstructured.SetNestedMap(hpa.Object, map[string]interface{}{"apiVersion": "v1", "kind": "Pod", "name": "pod"}, "spec", "scaleTargetRef")
	event := graphTestObject("v1", "Event", "event", "event-uid")
	event.Object["involvedObject"] = map[string]interface{}{"apiVersion": "v1", "kind": "Pod", "namespace": "ns", "name": "pod", "uid": "pod-uid"}
	event.Object["type"] = "Warning"
	event.Object["reason"] = "BackOff"
	event.Object["message"] = "retrying"
	event.Object["lastTimestamp"] = "2026-11-05T00:00:00Z"
	stale := event.DeepCopy()
	stale.SetName("stale")
	_ = unstructured.SetNestedField(stale.Object, "old-uid", "involvedObject", "uid")
	graph, err := BuildResourceGraphFromObjects(graphTestRoot("Pod", "pod"), []*unstructured.Unstructured{pod, service, ingress, pdb, hpa, event, stale})
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range []struct{ relation, source, target string }{{"selects", "svc", "pod"}, {"routes-to", "ingress", "svc"}, {"protects", "pdb", "pod"}, {"scales", "hpa", "pod"}} {
		if !graphTestEdge(graph, edge.relation, edge.source, edge.target) {
			t.Errorf("missing %s", edge.relation)
		}
	}
	if len(graph.Evidence) != 1 || graph.Evidence[0].Severity != "warning" || graph.Evidence[0].Summary != "BackOff retrying" {
		t.Fatalf("events lost identity or semantics: %+v", graph.Evidence)
	}
	custom := graphTestObject("custom.example/v1", "Pod", "custom", "custom-uid")
	_ = unstructured.SetNestedMap(custom.Object, map[string]interface{}{"serviceAccountName": "must-not-infer"}, "spec")
	graph, err = BuildResourceGraphFromObjects(graphTestRoot("Pod", "custom"), []*unstructured.Unstructured{custom})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 || len(graph.Edges) != 0 {
		t.Fatal("unknown GVK spec inferred")
	}
}

func TestResourceGraphEventEvidenceUsesObservedTimeAndSortsNewestFirst(t *testing.T) {
	base := time.Date(2026, time.November, 5, 10, 0, 0, 0, time.UTC)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "ns", UID: "pod-uid"}}
	involved := corev1.ObjectReference{APIVersion: "v1", Kind: "Pod", Name: "root", Namespace: "ns", UID: "pod-uid"}
	client := fake.NewSimpleClientset(pod,
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "a-creation", Namespace: "ns", CreationTimestamp: metav1.NewTime(base)}, InvolvedObject: involved, Reason: "Created"},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "b-event", Namespace: "ns", CreationTimestamp: metav1.NewTime(base)}, InvolvedObject: involved, EventTime: metav1.NewMicroTime(base.Add(2 * time.Second)), Reason: "Scheduled"},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "c-last", Namespace: "ns", CreationTimestamp: metav1.NewTime(base)}, InvolvedObject: involved, EventTime: metav1.NewMicroTime(base.Add(time.Second)), LastTimestamp: metav1.NewTime(base.Add(3 * time.Second)), Reason: "Started"},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "d-unknown", Namespace: "ns"}, InvolvedObject: involved, Reason: "Unknown"},
	)
	graph, err := BuildKubernetesResourceGraph(context.Background(), client, graphTestRoot("Pod", "root"))
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Evidence) != 3 {
		t.Fatalf("unexpected evidence: %+v", graph.Evidence)
	}
	for i, want := range []struct{ id, observed string }{
		{"event/ns/c-last", "2026-11-05T10:00:03Z"},
		{"event/ns/b-event", "2026-11-05T10:00:02Z"},
		{"event/ns/a-creation", "2026-11-05T10:00:00Z"},
	} {
		if got := graph.Evidence[i]; got.ID != want.id || got.ObservedAt != want.observed {
			t.Errorf("evidence[%d]=%+v, want %+v", i, got, want)
		}
	}
}

func TestResourceGraphNormalizeAndSafeWarnings(t *testing.T) {
	root, err := NormalizeResourceGraphRoot(cr.ResourceRef{ClusterID: " cluster ", Kind: " deployment ", Name: " root ", Namespace: " ns "})
	if err != nil {
		t.Fatal(err)
	}
	if root.Kind != "Deployment" || root.Namespace != "ns" || root.Name != "root" {
		t.Fatalf("bad normalization: %+v", root)
	}
	root.Namespace = " "
	if _, err = NormalizeResourceGraphRoot(root); err == nil {
		t.Fatal("blank namespace accepted")
	}
	client := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "ns", UID: "root-uid"}})
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, kruntime.Object, error) {
		return true, nil, fmt.Errorf("sensitive transport response token=must-not-appear")
	})
	remaining := int64(12)
	client.PrependReactor("list", "deployments", func(ktesting.Action) (bool, kruntime.Object, error) {
		return true, &appsv1.DeploymentList{ListMeta: metav1.ListMeta{RemainingItemCount: &remaining}}, nil
	})
	input := graphTestRoot("deployment", "root")
	graph, err := BuildKubernetesResourceGraph(context.Background(), client, input)
	if err != nil {
		t.Fatal(err)
	}
	warnings := strings.Join(graph.Warnings, "\n")
	if strings.Contains(warnings, "token=") || !strings.Contains(warnings, "Pod source unavailable") || !strings.Contains(warnings, "Deployment source truncated") {
		t.Fatalf("unsafe or missing warnings: %s", warnings)
	}
	for _, node := range graph.Nodes {
		if node.Resource.Name == "root" && node.Resource.Kind != "Deployment" {
			t.Fatal("wire kind not canonical")
		}
	}
}
func TestResourceGraphDepthAndReplacementUID(t *testing.T) {
	objects := []*unstructured.Unstructured{}
	for i := 0; i < 5; i++ {
		object := graphTestObject("custom.example/v1", "Owner", fmt.Sprintf("owner-%d", i), fmt.Sprintf("uid-%d", i))
		if i > 0 {
			graphTestOwner(object, "custom.example/v1", "Owner", fmt.Sprintf("owner-%d", i-1), fmt.Sprintf("uid-%d", i-1), true)
		}
		objects = append(objects, object)
	}
	graph, err := BuildResourceGraphFromObjects(graphTestRoot("Owner", "owner-0"), objects)
	if err != nil {
		t.Fatal(err)
	}
	if graphTestHas(graph, "owner-4") || !strings.Contains(strings.Join(graph.Warnings, " "), "truncated by depth") {
		t.Fatalf("depth omission not explicit: %+v", graph)
	}
	replacement := graphTestObject("apps/v1", "Deployment", "same-name", "new-uid")
	child := graphTestObject("v1", "Pod", "child", "child-uid")
	graphTestOwner(child, "apps/v1", "Deployment", "same-name", "old-uid", true)
	graph, err = BuildResourceGraphFromObjects(graphTestRoot("Deployment", "same-name"), []*unstructured.Unstructured{replacement, child})
	if err != nil {
		t.Fatal(err)
	}
	if graphTestHas(graph, "child") || len(graph.Edges) != 0 {
		t.Fatal("stale owner UID connected to replacement root")
	}
	graph, err = BuildResourceGraphFromObjects(graphTestRoot("Pod", "child"), []*unstructured.Unstructured{replacement, child})
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range graph.Nodes {
		if node.Resource.Name == "same-name" && (node.Resource.UID != "old-uid" || node.Status != "unresolved") {
			t.Fatal("stale owner resolved as replacement")
		}
	}
}

func TestResourceGraphErrorClassification(t *testing.T) {
	_, err := BuildKubernetesResourceGraph(context.Background(), fake.NewSimpleClientset(), graphTestRoot("secret", "secret"))
	if !errors.Is(err, ErrUnsupportedResourceGraphRoot) {
		t.Fatalf("unsupported root unclassified: %v", err)
	}
	invalid := graphTestRoot("pod", "pod")
	invalid.Namespace = ""
	_, err = NormalizeResourceGraphRoot(invalid)
	if !errors.Is(err, ErrInvalidResourceGraphRoot) {
		t.Fatalf("invalid root unclassified: %v", err)
	}
}

func TestResourceGraphGenericOwnerScope(t *testing.T) {
	for _, test := range []struct {
		name, ownerNS string
		confirmed     bool
	}{{"cluster owner", "", true}, {"same namespace", "ns", true}, {"other namespace", "other", false}} {
		t.Run(test.name, func(t *testing.T) {
			owner := graphTestObject("custom.example/v1", "CustomOwner", "owner", "owner-uid")
			owner.SetNamespace(test.ownerNS)
			pod := graphTestObject("v1", "Pod", "pod", "pod-uid")
			graphTestOwner(pod, "custom.example/v1", "CustomOwner", "owner", "owner-uid", true)
			graph, err := BuildResourceGraphFromObjects(graphTestRoot("Pod", "pod"), []*unstructured.Unstructured{owner, pod})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, node := range graph.Nodes {
				if node.Resource.Kind != "CustomOwner" {
					continue
				}
				found = true
				if test.confirmed {
					if node.Status == "unresolved" || node.Resource.Namespace != test.ownerNS {
						t.Fatalf("actual owner scope lost: %+v", node)
					}
				} else if node.Status != "unresolved" || node.Resource.Namespace == "other" {
					t.Fatalf("unobserved scope falsely confirmed: %+v", node)
				}
			}
			if !found {
				t.Fatal("owner missing")
			}
		})
	}
	// A cluster-scoped root can traverse its explicitly supplied namespaced children.
	owner := graphTestObject("custom.example/v1", "CustomOwner", "owner", "owner-uid")
	owner.SetNamespace("")
	pod := graphTestObject("v1", "Pod", "child", "child-uid")
	graphTestOwner(pod, "custom.example/v1", "CustomOwner", "owner", "owner-uid", true)
	root := graphTestRoot("CustomOwner", "owner")
	root.Namespace = ""
	root.ScopeMode = cr.ResourceScopeModeCluster
	graph, err := BuildResourceGraphFromObjects(root, []*unstructured.Unstructured{owner, pod})
	if err != nil {
		t.Fatal(err)
	}
	if !graphTestEdge(graph, "controls", "owner", "child") {
		t.Fatal("cluster owner could not reach namespaced dependent")
	}
}

// Cancel after actual work has started; cancellation still closes Done through
// a normal cancel function, without sleep-based timing or goroutine races.
type graphCancelAfterChecks struct {
	context.Context
	cancel        context.CancelFunc
	checks, after int
}

func (c *graphCancelAfterChecks) Err() error {
	c.checks++
	if c.checks >= c.after {
		c.cancel()
	}
	return c.Context.Err()
}
func graphFanoutObjects(count int) []*unstructured.Unstructured {
	objects := make([]*unstructured.Unstructured, 0, count*3)
	for i := 0; i < count; i++ {
		pod := graphTestObject("v1", "Pod", fmt.Sprintf("pod-%03d", i), fmt.Sprintf("pod-uid-%d", i))
		pod.SetLabels(map[string]string{"app": "shared"})
		objects = append(objects, pod)
		service := graphTestObject("v1", "Service", fmt.Sprintf("svc-%03d", i), fmt.Sprintf("svc-uid-%d", i))
		_ = unstructured.SetNestedStringMap(service.Object, map[string]string{"app": "shared"}, "spec", "selector")
		objects = append(objects, service)
		pdb := graphTestObject("policy/v1", "PodDisruptionBudget", fmt.Sprintf("pdb-%03d", i), fmt.Sprintf("pdb-uid-%d", i))
		_ = unstructured.SetNestedMap(pdb.Object, map[string]interface{}{"matchLabels": map[string]interface{}{"app": "shared"}}, "spec", "selector")
		objects = append(objects, pdb)
	}
	return objects
}
func TestResourceGraphWorkingSetAndHighFanout(t *testing.T) {
	objects := graphFanoutObjects(500)
	owner := graphTestObject("custom.example/v1", "Owner", "owner", "owner-uid")
	grand := graphTestObject("custom.example/v1", "Owner", "grand", "grand-uid")
	graphTestOwner(owner, "custom.example/v1", "Owner", "grand", "grand-uid", true)
	for _, o := range objects {
		if o.GetKind() == "Service" && o.GetName() == "svc-000" {
			graphTestOwner(o, "custom.example/v1", "Owner", "owner", "owner-uid", true)
		}
	}
	objects = append(objects, owner, grand)
	root := graphTestRoot("Service", "svc-000")
	graph, err := BuildResourceGraphFromObjects(root, objects)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) > 500 || len(graph.Edges) > 1000 || !graphTestHas(graph, "svc-000") || !graphTestHas(graph, "owner") || !graphTestHas(graph, "grand") {
		t.Fatal("output bounds or root lost")
	}
	if !strings.Contains(strings.Join(graph.Warnings, " "), "working set truncated") {
		t.Fatal("high fanout was silently treated as complete")
	}
	// Internal writes have a hard bound independent of the final output filter.
	b := &graphRules{ctx: context.Background(), nodes: map[string]cr.ResourceGraphNode{}, edges: map[string]cr.ResourceGraphEdge{}}
	for i := 0; i < ResourceGraphWorkingNodeLimit+1; i++ {
		b.node(cr.ResourceRef{Kind: "Pod", Name: fmt.Sprint(i)}, "")
	}
	if len(b.nodes) != ResourceGraphWorkingNodeLimit || !b.partial {
		t.Fatal("working node set is unbounded")
	}
	b = &graphRules{ctx: context.Background(), nodes: map[string]cr.ResourceGraphNode{}, edges: map[string]cr.ResourceGraphEdge{}}
	for i := 0; i < ResourceGraphWorkingEdgeLimit+1; i++ {
		b.edge("source", fmt.Sprint(i), "owns")
	}
	if len(b.edges) != ResourceGraphWorkingEdgeLimit || !b.partial {
		t.Fatal("working edge set is unbounded")
	}
	_, err = BuildResourceGraphFromObjects(root, make([]*unstructured.Unstructured, ResourceGraphWorkingNodeLimit+1))
	if !errors.Is(err, ErrResourceGraphBudget) {
		t.Fatalf("input budget not classified: %v", err)
	}
}
func TestResourceGraphCancellationDuringBuildAndTraversal(t *testing.T) {
	objects := graphFanoutObjects(200)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &graphCancelAfterChecks{Context: base, cancel: cancel, after: 10000}
	_, err := buildResourceGraphWithContext(ctx, graphTestRoot("Service", "svc-000"), objects)
	if !errors.Is(err, context.Canceled) || ctx.checks < 10000 {
		t.Fatalf("processing cancellation ignored: %v (%d checks)", err, ctx.checks)
	}
	edges := make([]cr.ResourceGraphEdge, 3000)
	for i := range edges {
		edges[i] = cr.ResourceGraphEdge{ID: fmt.Sprint(i), SourceID: "root", TargetID: fmt.Sprint(i), Relation: "owns"}
	}
	base, cancel = context.WithCancel(context.Background())
	defer cancel()
	ctx = &graphCancelAfterChecks{Context: base, cancel: cancel, after: 3100}
	_, err = graphTraverse(ctx, "root", edges)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("traversal cancellation ignored: %v", err)
	}
}
func TestResourceGraphStatusAndUnknownEvent(t *testing.T) {
	for _, test := range []struct{ kind, api, ready string }{{"Deployment", "apps/v1", "availableReplicas"}, {"ReplicaSet", "apps/v1", "readyReplicas"}} {
		t.Run(test.kind, func(t *testing.T) {
			object := graphTestObject(test.api, test.kind, "root", "root-uid")
			for _, ready := range []int64{0, 2} {
				_ = unstructured.SetNestedField(object.Object, int64(2), "status", "replicas")
				_ = unstructured.SetNestedField(object.Object, ready, "status", test.ready)
				graph, err := BuildResourceGraphFromObjects(graphTestRoot(test.kind, "root"), []*unstructured.Unstructured{object})
				if err != nil {
					t.Fatal(err)
				}
				want := "progressing"
				if ready == 2 {
					want = "healthy"
				}
				if graph.Nodes[0].Status != want {
					t.Errorf("got %s want %s", graph.Nodes[0].Status, want)
				}
			}
			delete(object.Object, "status")
			if graphStatus(object) == "healthy" {
				t.Fatal("missing status claimed healthy")
			}
		})
	}
	ingress := graphTestObject("networking.k8s.io/v1", "Ingress", "ingress", "ingress-uid")
	if graphStatus(ingress) != "pending" {
		t.Fatal("pending Ingress lost")
	}
	_ = unstructured.SetNestedSlice(ingress.Object, []interface{}{map[string]interface{}{"ip": "192.0.2.1"}}, "status", "loadBalancer", "ingress")
	if graphStatus(ingress) != "ready" {
		t.Fatal("ready Ingress lost")
	}
	service := graphTestObject("v1", "Service", "svc", "svc-uid")
	_ = unstructured.SetNestedField(service.Object, "ClusterIP", "spec", "type")
	if graphStatus(service) != "ClusterIP" {
		t.Fatal("service type lost")
	}
	custom := graphTestObject("custom.example/v1", "Event", "custom-event", "custom-event-uid")
	graphTestOwner(custom, "v1", "Service", "svc", "svc-uid", false)
	custom.Object["involvedObject"] = map[string]interface{}{"apiVersion": "v1", "kind": "Service", "namespace": "ns", "name": "svc", "uid": "svc-uid"}
	custom.Object["message"] = "must-not-be-interpreted"
	graph, err := BuildResourceGraphFromObjects(graphTestRoot("Event", "custom-event"), []*unstructured.Unstructured{custom, service})
	if err != nil {
		t.Fatal(err)
	}
	if !graphTestEdge(graph, "owns", "svc", "custom-event") || len(graph.Evidence) != 0 {
		t.Fatal("custom Event metadata lost or spec interpreted")
	}
}

func TestResourceGraphCustomKindClusterScope(t *testing.T) {
	for _, kind := range []string{"Event", "Pod"} {
		object := graphTestObject("custom.example/v1", kind, "custom", "custom-uid")
		object.SetNamespace("")
		root := graphTestRoot(kind, "custom")
		root.APIVersion = "custom.example/v1"
		root.Namespace = ""
		root.ScopeMode = cr.ResourceScopeModeCluster
		graph, err := BuildResourceGraphFromObjects(root, []*unstructured.Unstructured{object})
		if err != nil || len(graph.Nodes) != 1 {
			t.Fatalf("cluster custom %s rejected: %v", kind, err)
		}
		root.APIVersion = "v1"
		if _, err := NormalizeResourceGraphRoot(root); !errors.Is(err, ErrInvalidResourceGraphRoot) {
			t.Fatalf("actual v1/%s namespace bypass accepted", kind)
		}
	}
}
