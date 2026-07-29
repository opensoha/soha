package resourcebackend

import (
	"context"
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestSetNodeUnschedulableCordonAndUncordon(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})

	if err := setNodeUnschedulable(ctx, client, "node-a", true); err != nil {
		t.Fatalf("cordon node: %v", err)
	}
	item, err := client.CoreV1().Nodes().Get(ctx, "node-a", metav1.GetOptions{})
	if err != nil || !item.Spec.Unschedulable {
		t.Fatalf("cordoned node = %#v, err = %v", item, err)
	}

	if err := setNodeUnschedulable(ctx, client, "node-a", false); err != nil {
		t.Fatalf("uncordon node: %v", err)
	}
	item, err = client.CoreV1().Nodes().Get(ctx, "node-a", metav1.GetOptions{})
	if err != nil || item.Spec.Unschedulable {
		t.Fatalf("uncordoned node = %#v, err = %v", item, err)
	}
}

func TestDrainNodeCordonsNodeWithoutEvictablePods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})

	err := drainNode(ctx, client, "node-a", domainresource.NodeDrainInput{TimeoutSeconds: 5})
	if err != nil {
		t.Fatalf("drain node: %v", err)
	}
	item, err := client.CoreV1().Nodes().Get(ctx, "node-a", metav1.GetOptions{})
	if err != nil || !item.Spec.Unschedulable {
		t.Fatalf("drained node = %#v, err = %v", item, err)
	}
}

func TestDrainNodeSubmitsEvictionForManagedPod(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	controller := true
	client := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "api-0", Namespace: "team-a",
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api", Controller: &controller}},
			},
			Spec: corev1.PodSpec{NodeName: "node-a"},
		},
	)
	client.Discovery().(*fakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{{Name: "pods/eviction", Kind: "Eviction", Group: policyv1.GroupName, Version: policyv1.SchemeGroupVersion.Version}},
	}}
	client.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAction, ok := action.(k8stesting.CreateAction)
		if !ok || createAction.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		eviction := createAction.GetObject().(*policyv1.Eviction)
		if err := client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), eviction.Namespace, eviction.Name); err != nil {
			return true, nil, err
		}
		return true, eviction, nil
	})

	err := drainNode(ctx, client, "node-a", domainresource.NodeDrainInput{TimeoutSeconds: 5})
	if err != nil {
		t.Fatalf("drain node: %v", err)
	}
	foundEviction := false
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" && action.GetResource().Resource == "pods" && action.GetSubresource() == "eviction" {
			foundEviction = true
		}
	}
	if !foundEviction {
		t.Fatalf("actions = %#v, want pod eviction", client.Actions())
	}
	if _, err := client.CoreV1().Pods("team-a").Get(ctx, "api-0", metav1.GetOptions{}); err == nil {
		t.Fatal("drain returned before the evicted pod was deleted")
	}
}

func TestBuildNodeViewIncludesSchedulability(t *testing.T) {
	t.Parallel()
	view := buildNodeView(corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	}, nodeAggregate{})
	if !view.Unschedulable {
		t.Fatal("Unschedulable = false, want true")
	}
}
