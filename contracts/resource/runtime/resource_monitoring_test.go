package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func monitoringResourceList(cpu, memory string) corev1.ResourceList {
	return corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(memory)}
}
func TestMonitoringFootprintHandlesSidecarsInitAndOverhead(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	container := func(name, cpu string) corev1.Container {
		return corev1.Container{Name: name, Resources: corev1.ResourceRequirements{Requests: monitoringResourceList(cpu, "1Mi"), Limits: monitoringResourceList(cpu, "1Mi")}}
	}
	sidecar := container("sidecar", "1")
	sidecar.RestartPolicy = &always
	pod := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{container("app", "2")}, InitContainers: []corev1.Container{sidecar, container("init", "4")}, Overhead: monitoringResourceList("100m", "1Mi")}}
	requests, limits, cpuComplete, memoryComplete := effectiveMonitoringPodResources(pod)
	if requests.cpu != 5.1 || limits.cpu != 5.1 || !cpuComplete || !memoryComplete {
		t.Fatalf("effective footprint requests=%+v limits=%+v complete=%v/%v", requests, limits, cpuComplete, memoryComplete)
	}
	pod.Spec.Containers[0].Resources.Limits = monitoringResourceList("0", "0")
	_, _, cpuComplete, memoryComplete = effectiveMonitoringPodResources(pod)
	if cpuComplete || memoryComplete {
		t.Fatal("zero limits treated as an explicit finite limit")
	}
	pod.Spec.Resources = &corev1.ResourceRequirements{Limits: monitoringResourceList("0", "0")}
	_, _, cpuComplete, memoryComplete = effectiveMonitoringPodResources(pod)
	if cpuComplete || memoryComplete {
		t.Fatal("Pod-level zero limits treated as complete finite limits")
	}
}
func TestMonitoringInventoryCountsOnlyBoundNonterminalPods(t *testing.T) {
	nodes := corev1.NodeList{Items: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node", UID: types.UID("node-uid")}, Status: corev1.NodeStatus{Capacity: monitoringResourceList("4", "8Gi"), Allocatable: corev1.ResourceList{corev1.ResourcePods: resource.MustParse("1")}}}}}
	deleting := metav1.Now()
	pods := corev1.PodList{Items: []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &deleting}, Spec: corev1.PodSpec{NodeName: "node"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{Spec: corev1.PodSpec{NodeName: "node"}, Status: corev1.PodStatus{Phase: corev1.PodPending}},
		{Spec: corev1.PodSpec{NodeName: "node"}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
		{Spec: corev1.PodSpec{NodeName: "node"}, Status: corev1.PodStatus{Phase: corev1.PodFailed}},
		{Status: corev1.PodStatus{Phase: corev1.PodPending}},
	}}
	forbidden := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/nodes":
			_ = json.NewEncoder(w).Encode(nodes)
		case "/api/v1/pods":
			if forbidden {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode(pods)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, _ := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	inventory, err := ReadResourceMonitoringInventory(context.Background(), client.CoreV1().RESTClient(), "c")
	if err != nil {
		t.Fatal(err)
	}
	fact := inventory.Nodes[0]
	if fact.BoundPods == nil || *fact.BoundPods != 2 || *fact.PendingPods != 1 || *fact.RemainingPodSlots != 0 || *fact.OverflowPodSlots != 1 || *inventory.UnscheduledPendingPods != 1 {
		t.Fatalf("wrong capacity: %+v", fact)
	}
	forbidden = true
	inventory, err = ReadResourceMonitoringInventory(context.Background(), client.CoreV1().RESTClient(), "c")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Quality != "partial" || inventory.Nodes[0].BoundPods != nil || inventory.Nodes[0].Requests.CPUCores != nil {
		t.Fatalf("partial Pod read invented zero capacity facts: %+v", inventory)
	}
}
