package kubernetes

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func readyWorkerFixture(t *testing.T) (*Manager, *fake.Clientset, domain.WorkerPool, string) {
	t.Helper()
	manager, client, pool := workerFixture(t)
	id := uuid.NewString()
	nodeName := domain.WorkerNodeName(id)
	controller := true
	objects := []runtime.Object{
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, UID: "node-uid", Labels: map[string]string{"soha.io/worker-pool-id": pool.ID.String()}, Annotations: map[string]string{workerOperationAnnotation: id}}, Status: corev1.NodeStatus{
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			NodeInfo:    corev1.NodeSystemInfo{SystemUUID: id, Architecture: "amd64", OperatingSystem: "linux", OSImage: "Ubuntu 24.04.2 LTS", KubeletVersion: pool.Spec.KubernetesVersion, ContainerRuntimeVersion: "containerd://2.0.0"},
			Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("8Gi"), corev1.ResourcePods: resource.MustParse("110")},
		}},
		&coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Namespace: "kube-node-lease", OwnerReferences: []metav1.OwnerReference{{UID: "node-uid"}}}, Spec: coordinationv1.LeaseSpec{HolderIdentity: &nodeName, RenewTime: &metav1.MicroTime{Time: time.Now()}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "network-current", Namespace: "kube-system", UID: "pod-uid", Labels: map[string]string{"app": "network", appsv1.DefaultDaemonSetUniqueLabelKey: "current"}, OwnerReferences: []metav1.OwnerReference{{UID: "daemon-uid", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: nodeName}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Minute))}}}},
	}
	daemon, _ := client.AppsV1().DaemonSets("kube-system").Get(context.Background(), "network", metav1.GetOptions{})
	data, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": daemon.Spec.Template}})
	objects = append(objects, &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: "network-current", Namespace: "kube-system", Labels: map[string]string{"app": "network", appsv1.DefaultDaemonSetUniqueLabelKey: "current"}, OwnerReferences: []metav1.OwnerReference{{UID: daemon.UID, Controller: &controller}}}, Revision: 2, Data: runtime.RawExtension{Raw: data}})
	for _, object := range objects {
		if err := client.Tracker().Add(object); err != nil {
			t.Fatal(err)
		}
	}
	return manager, client, pool, id
}

func TestWorkerAssessmentRequiresOriginalReadyNodeAndCurrentDaemon(t *testing.T) {
	for _, scenario := range []string{"ready", "unclaimed", "foreign-vm", "foreign-owner", "cordon", "taint", "no-runtime-capacity", "stale-heartbeat", "foreign-lease", "old-daemon", "pending-new-daemon", "daemon-window", "read-only"} {
		t.Run(scenario, func(t *testing.T) {
			manager, client, pool, id := readyWorkerFixture(t)
			ctx := context.Background()
			node, _ := client.CoreV1().Nodes().Get(ctx, domain.WorkerNodeName(id), metav1.GetOptions{})
			lease, _ := client.CoordinationV1().Leases("kube-node-lease").Get(ctx, node.Name, metav1.GetOptions{})
			pod, _ := client.CoreV1().Pods("kube-system").Get(ctx, "network-current", metav1.GetOptions{})
			alterObservedWorkerScenario(t, scenario, client, node, lease, pod)
			_, _ = client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
			_, _ = client.CoordinationV1().Leases("kube-node-lease").Update(ctx, lease, metav1.UpdateOptions{})
			_, _ = client.CoreV1().Pods("kube-system").Update(ctx, pod, metav1.UpdateOptions{})
			client.ClearActions()
			result, err := manager.ObserveWorker(ctx, pool, id)
			if scenario == "ready" {
				if err != nil || !result.Ready || result.NodeUID != "node-uid" || result.ValidUntil.Before(result.ObservedAt) {
					t.Fatalf("ready worker not verified: %+v %v", result, err)
				}
			} else if result.Ready {
				t.Fatalf("unverified worker was marked ready: %+v", result)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "create" || action.GetVerb() == "update" || action.GetVerb() == "patch" || action.GetVerb() == "delete" {
					t.Fatalf("assessment changed resources: %s", action.GetVerb())
				}
			}
		})
	}
}

func alterObservedWorkerScenario(t *testing.T, scenario string, client *fake.Clientset, node *corev1.Node, lease *coordinationv1.Lease, pod *corev1.Pod) {
	t.Helper()
	ctx := context.Background()
	switch scenario {
	case "foreign-vm":
		node.Status.NodeInfo.SystemUUID = uuid.NewString()
	case "unclaimed":
		node.Annotations = nil
		node.Labels = nil
	case "foreign-owner":
		node.Annotations[workerOperationAnnotation] = uuid.NewString()
	case "cordon":
		node.Spec.Unschedulable = true
	case "taint":
		node.Spec.Taints = []corev1.Taint{{Key: "dedicated", Effect: corev1.TaintEffectNoSchedule}}
	case "no-runtime-capacity":
		node.Status.Allocatable = nil
	case "stale-heartbeat":
		lease.Spec.RenewTime = &metav1.MicroTime{Time: time.Now().Add(-2 * time.Minute)}
	case "foreign-lease":
		lease.OwnerReferences[0].UID = types.UID(uuid.NewString())
	case "old-daemon":
		pod.Labels[appsv1.DefaultDaemonSetUniqueLabelKey] = "old"
	case "pending-new-daemon":
		old := pod.DeepCopy()
		old.Name, old.UID = "network-old", "old-pod"
		old.Labels[appsv1.DefaultDaemonSetUniqueLabelKey] = "old"
		if err := client.Tracker().Add(old); err != nil {
			t.Fatal(err)
		}
		pod.Status.Conditions[0].Status = corev1.ConditionFalse
	case "daemon-window":
		daemon, _ := client.AppsV1().DaemonSets("kube-system").Get(ctx, "network", metav1.GetOptions{})
		daemon.Spec.MinReadySeconds = 120
		_, _ = client.AppsV1().DaemonSets("kube-system").Update(ctx, daemon, metav1.UpdateOptions{})
	case "read-only":
		node.Annotations = nil
	}
}

func TestWorkerClaimRefusesDifferentVMAndBootstrapRevocationAllowsDaemonChange(t *testing.T) {
	manager, client, pool, id := readyWorkerFixture(t)
	ctx := context.Background()
	node, _ := client.CoreV1().Nodes().Get(ctx, domain.WorkerNodeName(id), metav1.GetOptions{})
	node.Status.NodeInfo.SystemUUID = uuid.NewString()
	_, _ = client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	client.ClearActions()
	if err := manager.ClaimWorkerNode(ctx, pool, id); err == nil {
		t.Fatal("claimed another VM")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" {
			t.Fatal("modified another VM node")
		}
	}
	bootstrap, err := manager.PrepareWorkerBootstrap(ctx, pool, id, domain.WorkerNodeName(id), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	pool.Identity.DaemonSetUIDs["kube-system/network"] = "old-daemon-uid"
	pool.Spec.KubernetesVersion = "v1.34.1"
	if err := manager.RevokeWorkerBootstrap(ctx, pool, id, bootstrap.SecretUID); err != nil {
		t.Fatalf("unrelated cluster changes blocked token revocation: %v", err)
	}
}
