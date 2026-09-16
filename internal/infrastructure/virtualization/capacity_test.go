package virtualization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	kubeinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	typedfake "k8s.io/client-go/kubernetes/fake"
)

func TestPVECapacityUsesCommitmentsAndDeduplicatesSharedStorage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("inventory mutated provider")
			http.Error(w, "unexpected", 500)
			return
		}
		switch r.URL.Path {
		case "/api2/json/access/permissions":
			writePVEAny(w, map[string]any{"/": map[string]any{"Sys.Audit": 1, "VM.Audit": 1, "Datastore.Audit": 1}})
		case "/api2/json/cluster/resources":
			writePVEData(w, []map[string]any{
				{"type": "node", "node": "a", "status": "online", "maxcpu": 8, "maxmem": int64(16 << 30), "mem": int64(3 << 30)},
				{"type": "node", "node": "b", "status": "online", "maxcpu": 8, "maxmem": int64(16 << 30), "mem": int64(2 << 30)},
				{"type": "qemu", "node": "a", "vmid": 701, "status": "stopped", "maxcpu": 6, "maxmem": int64(12 << 30)},
				{"type": "lxc", "node": "b", "vmid": 702, "status": "running", "maxcpu": 2, "maxmem": int64(2 << 30)},
			})
		case "/api2/json/nodes/a/storage", "/api2/json/nodes/b/storage":
			writePVEData(w, []map[string]any{{"storage": "shared", "active": 1, "enabled": 1, "shared": 1, "content": "images", "total": int64(100 << 30), "avail": int64(90 << 30)}})
		case "/api2/json/nodes/a/storage/shared/content", "/api2/json/nodes/b/storage/shared/content":
			writePVEData(w, []map[string]any{{"size": int64(60 << 30)}})
		case "/api2/json/nodes/a/qemu/701/config":
			writePVEAny(w, map[string]any{"description": "soha-operation:original"})
		default:
			t.Errorf("unexpected read %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	snapshot, err := NewPVEAdapter(server.Client()).ObserveCapacity(context.Background(), Connection{Endpoint: server.URL}, CreateVMInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Complete || len(snapshot.Nodes) != 2 || snapshot.Nodes[0].CPU != 2 || snapshot.Nodes[0].MemoryMiB != 3584 {
		t.Fatalf("used utilization instead of stopped guest commitments: %+v", snapshot)
	}
	if len(snapshot.Storage) != 1 || snapshot.Storage[0].AvailableGiB != 40 || len(snapshot.Storage[0].Nodes) != 2 {
		t.Fatalf("shared or thin disk capacity incorrect: %+v", snapshot.Storage)
	}
	if len(snapshot.ObservedOperations) != 1 || snapshot.ObservedOperations[0] != "original" {
		t.Fatalf("owner observation lost: %+v", snapshot)
	}
}

func TestPVECapacityRejectsMissingAllocationFields(t *testing.T) {
	_, err := pveCapacityNodes([]map[string]any{{"type": "node", "node": "a", "status": "online", "maxcpu": 8, "maxmem": int64(16 << 30), "mem": int64(3 << 30)}, {"type": "qemu", "node": "a", "maxcpu": 2}}, 512)
	if err == nil {
		t.Fatal("partial guest inventory became available capacity")
	}
}

func TestKubeVirtCapacityRequestsQuotasAndStorage(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, kubeVirtTestListKinds())
	class := "standard"
	capBytes := resource.MustParse("100Gi")
	typed := typedfake.NewClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{"kubernetes.io/hostname": "node-a", "kubevirt.io/schedulable": "true", "kubernetes.io/arch": "amd64"}}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("16Gi")}, Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "existing", Namespace: "other"}, Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1500m"), corev1.ResourceMemory: resource.MustParse("2Gi")}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "pending", Namespace: "other"}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &class, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("20Gi")}}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}},
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: class, Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"}}, Provisioner: "driver"},
		&storagev1.CSIStorageCapacity{ObjectMeta: metav1.ObjectMeta{Name: "capacity", Namespace: "csi"}, StorageClassName: class, NodeTopology: &metav1.LabelSelector{}, Capacity: &capBytes},
		&corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "budget", Namespace: "apps"}, Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("4")}}, Status: corev1.ResourceQuotaStatus{Hard: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("4")}, Used: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("1")}}},
	)
	adapter := NewKubeVirtAdapter(stubBundleProvider{bundle: &kubeinfra.Bundle{Typed: typed, Dynamic: client}})
	if _, err := adapter.ObserveCapacity(ctx, Connection{ClusterID: "cluster"}, CreateVMInput{}); err == nil {
		t.Fatal("stopped VM admitted without an accounting model")
	}
	snapshot, err := adapter.ObserveCapacity(ctx, Connection{ClusterID: "cluster", Options: map[string]any{"namespace": "apps"}}, CreateVMInput{Architecture: "amd64", SourceMode: "datasource_clone", StartAfterCreate: true})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Complete || len(snapshot.Nodes) != 1 || snapshot.Nodes[0].CPU != 6 || snapshot.Nodes[0].MemoryMiB != 14*1024 {
		t.Fatalf("scheduler requests: %+v", snapshot)
	}
	if snapshot.QuotaCPU == nil || *snapshot.QuotaCPU != 3 || snapshot.Namespace != "apps" {
		t.Fatalf("namespace quota lost: %+v", snapshot)
	}
	if len(snapshot.Storage) != 1 || snapshot.Storage[0].AvailableGiB != 80 {
		t.Fatalf("pending PVC not reserved: %+v", snapshot.Storage)
	}
	if err := typed.StorageV1().CSIStorageCapacities("csi").Delete(ctx, "capacity", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	missing, err := adapter.ObserveCapacity(ctx, Connection{ClusterID: "cluster", Options: map[string]any{"namespace": "apps"}}, CreateVMInput{StartAfterCreate: true})
	if err != nil || len(missing.Storage) != 0 {
		t.Fatalf("missing CSI capacity was invented: %+v %v", missing, err)
	}
}

func TestCapacityRejectsUnmodeledQuotaDimensions(t *testing.T) {
	for _, key := range []corev1.ResourceName{corev1.ResourceLimitsCPU, corev1.ResourceLimitsMemory, corev1.ResourcePods, "count/virtualmachines.kubevirt.io"} {
		t.Run(string(key), func(t *testing.T) {
			hard := corev1.ResourceList{key: resource.MustParse("100")}
			quota := corev1.ResourceQuota{Spec: corev1.ResourceQuotaSpec{Hard: hard}, Status: corev1.ResourceQuotaStatus{Hard: hard, Used: corev1.ResourceList{key: resource.MustParse("0")}}}
			if err := applyCapacityQuotas(&domain.CapacitySnapshot{}, []corev1.ResourceQuota{quota}, "standard"); err == nil {
				t.Fatal("unmodeled quota was presented as reservable capacity")
			}
		})
	}
}

func TestCapacityPodRequestsIncludesRestartableInitAndOverhead(t *testing.T) {
	restart := corev1.ContainerRestartPolicyAlways
	requests := func(cpu, memory string) corev1.ResourceRequirements {
		return corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(memory)}}
	}
	pod := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Resources: requests("1", "1Gi")}}, InitContainers: []corev1.Container{{RestartPolicy: &restart, Resources: requests("500m", "512Mi")}, {Resources: requests("2", "2Gi")}}, Overhead: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")}}}
	cpu, memory := capacityPodRequests(pod)
	if cpu != 2600 || memory != (2*1024+512+128)*(1<<20) {
		t.Fatalf("init/overhead requests: %d %d", cpu, memory)
	}
}
