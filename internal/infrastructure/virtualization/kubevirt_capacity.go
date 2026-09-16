package virtualization

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

func (a *KubeVirtAdapter) ObserveCapacity(ctx context.Context, connection Connection, input CreateVMInput) (domain.CapacitySnapshot, error) {
	snapshot := domain.CapacitySnapshot{SourceID: domain.CapacitySourceID(connection), ObservedAt: time.Now().UTC(), Namespace: firstNonEmpty(input.Namespace, namespaceOrDefault(connection, "default")), Nodes: []domain.CapacityNode{}, Storage: []domain.CapacityStorage{}}
	if input.SourceMode != "datasource_clone" && input.SourceMode != "" {
		return snapshot, fmt.Errorf("KubeVirt capacity admission currently requires a DataSource root disk")
	}
	if !input.StartAfterCreate {
		return snapshot, fmt.Errorf("KubeVirt capacity admission requires startAfterCreate to verify the reserved node allocation")
	}
	bundle, err := a.bundle(ctx, connection)
	if err != nil {
		return snapshot, err
	}
	if bundle.Typed == nil {
		return snapshot, fmt.Errorf("capacity needs the typed Kubernetes client")
	}
	nodes, err := bundle.Typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return snapshot, err
	}
	pods, err := bundle.Typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return snapshot, err
	}
	claims, err := bundle.Typed.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return snapshot, err
	}
	quotas, err := bundle.Typed.CoreV1().ResourceQuotas(snapshot.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return snapshot, err
	}
	classes, err := bundle.Typed.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return snapshot, err
	}
	capacities, err := bundle.Typed.StorageV1().CSIStorageCapacities("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return snapshot, err
	}
	class, err := capacityStorageClass(classes.Items, stringOption(input.ProviderParams, "storageClass"))
	if err != nil {
		return snapshot, err
	}
	if err := applyCapacityQuotas(&snapshot, quotas.Items, class.Name); err != nil {
		return snapshot, err
	}
	var eligible []corev1.Node
	snapshot.Nodes, eligible = kubeCapacityNodes(nodes.Items, pods.Items, input.Architecture)
	snapshot.Storage = kubeCapacityStorage(capacities.Items, claims.Items, eligible, class)
	// VM ownership is accounted only once its launcher is scheduled and every
	// requested PVC is Bound, so a pending VM never frees an admission reservation.
	vms, err := bundle.Dynamic.Resource(kubeVirtVMGVR).Namespace(snapshot.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return snapshot, err
	}
	instances, err := bundle.Dynamic.Resource(kubeVirtVMIGVR).Namespace(snapshot.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return snapshot, err
	}
	snapshot.ObservedOperations = kubeCapacityObservedOperations(vms.Items, instances.Items, pods.Items, claims.Items)
	snapshot.Complete = true
	snapshot.Reason = "Kubernetes requests, quotas and CSI-reported capacity; admission and VM readiness remain authoritative"
	return snapshot, nil
}

func kubeCapacityObservedOperations(vms, instances []unstructured.Unstructured, pods []corev1.Pod, claims []corev1.PersistentVolumeClaim) []string {
	result := []string{}
	for _, vm := range vms {
		owner := vm.GetAnnotations()["soha.io/creation-operation"]
		if owner == "" || vm.GetDeletionTimestamp() != nil {
			continue
		}
		for _, instance := range instances {
			for _, parent := range instance.GetOwnerReferences() {
				if parent.Kind == "VirtualMachine" && parent.UID == vm.GetUID() && instance.GetDeletionTimestamp() == nil && kubeCapacityVMAccounted(string(instance.GetUID()), vm.GetNamespace(), pods, claims) {
					result = append(result, owner)
				}
			}
		}
	}
	return result
}

func kubeCapacityNodes(nodes []corev1.Node, pods []corev1.Pod, architecture string) ([]domain.CapacityNode, []corev1.Node) {
	result := []domain.CapacityNode{}
	eligible := []corev1.Node{}
	for _, node := range nodes {
		if !capacityNodeEligible(node, architecture) {
			continue
		}
		cpu, memory := node.Status.Allocatable.Cpu().MilliValue(), node.Status.Allocatable.Memory().Value()
		for _, pod := range pods {
			if pod.Spec.NodeName != node.Name || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			usedCPU, usedMemory := capacityPodRequests(pod)
			cpu -= usedCPU
			memory -= usedMemory
		}
		result = append(result, domain.CapacityNode{Name: node.Labels["kubernetes.io/hostname"], CPU: max(0, cpu) / 1000, MemoryMiB: max(0, memory) / (1 << 20)})
		eligible = append(eligible, node)
	}
	slices.SortFunc(result, func(a, b domain.CapacityNode) int { return strings.Compare(a.Name, b.Name) })
	return result, eligible
}

func capacityNodeEligible(node corev1.Node, architecture string) bool {
	if node.Spec.Unschedulable || node.DeletionTimestamp != nil || node.Labels["kubernetes.io/hostname"] == "" || node.Labels["kubevirt.io/schedulable"] != "true" {
		return false
	}
	if architecture != "" && node.Labels["kubernetes.io/arch"] != kubeVirtArchitecture(architecture) {
		return false
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			return false
		}
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func capacityPodRequests(pod corev1.Pod) (int64, int64) {
	var cpu, memory, sideCPU, sideMemory, initCPU, initMemory int64
	for _, container := range pod.Spec.Containers {
		cpu += container.Resources.Requests.Cpu().MilliValue()
		memory += container.Resources.Requests.Memory().Value()
	}
	for _, container := range pod.Spec.InitContainers {
		c, m := container.Resources.Requests.Cpu().MilliValue(), container.Resources.Requests.Memory().Value()
		if container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			sideCPU += c
			sideMemory += m
			initCPU = max(initCPU, sideCPU)
			initMemory = max(initMemory, sideMemory)
		} else {
			initCPU = max(initCPU, sideCPU+c)
			initMemory = max(initMemory, sideMemory+m)
		}
	}
	cpu = max(cpu+sideCPU, initCPU)
	memory = max(memory+sideMemory, initMemory)
	if pod.Spec.Resources != nil {
		cpu = max(cpu, pod.Spec.Resources.Requests.Cpu().MilliValue())
		memory = max(memory, pod.Spec.Resources.Requests.Memory().Value())
	}
	return cpu + pod.Spec.Overhead.Cpu().MilliValue(), memory + pod.Spec.Overhead.Memory().Value()
}

func capacityStorageClass(classes []storagev1.StorageClass, name string) (storagev1.StorageClass, error) {
	var matches []storagev1.StorageClass
	for _, class := range classes {
		if name == class.Name || name == "" && class.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			matches = append(matches, class)
		}
	}
	if len(matches) != 1 {
		return storagev1.StorageClass{}, fmt.Errorf("select exactly one capacity-aware storage class")
	}
	return matches[0], nil
}

func applyCapacityQuotas(snapshot *domain.CapacitySnapshot, quotas []corev1.ResourceQuota, storage string) error {
	for _, quota := range quotas {
		if len(quota.Spec.Scopes) > 0 || quota.Spec.ScopeSelector != nil {
			return fmt.Errorf("scoped resource quota needs provider admission before capacity can be established")
		}
		for key, hard := range quota.Spec.Hard {
			used, ok := quota.Status.Used[key]
			observedHard, observed := quota.Status.Hard[key]
			if !ok || !observed || observedHard.Cmp(hard) != 0 {
				return fmt.Errorf("resource quota usage is not synchronized")
			}
			remaining := hard.DeepCopy()
			remaining.Sub(used)
			switch string(key) {
			case "cpu", "requests.cpu":
				mergeQuota(&snapshot.QuotaCPU, max(0, remaining.MilliValue())/1000)
			case "memory", "requests.memory":
				mergeQuota(&snapshot.QuotaMemoryMiB, max(0, remaining.Value())/(1<<20))
			case "requests.storage", storage + ".storageclass.storage.k8s.io/requests.storage":
				mergeQuota(&snapshot.QuotaDiskGiB, max(0, remaining.Value())/(1<<30))
			default:
				return fmt.Errorf("resource quota %s is not supported by capacity reservation accounting", key)
			}
		}
	}
	return nil
}

func mergeQuota(target **int64, value int64) {
	if *target == nil || value < **target {
		*target = &value
	}
}

func kubeCapacityStorage(capacities []storagev1.CSIStorageCapacity, claims []corev1.PersistentVolumeClaim, nodes []corev1.Node, class storagev1.StorageClass) []domain.CapacityStorage {
	// CSI objects may describe overlapping topology segments or classes on the
	// same driver. Reserve one conservative driver-wide pool, never sum segments.
	item := domain.CapacityStorage{Key: "csi/" + class.Provisioner, Name: class.Name, Nodes: []string{}}
	var minimum *int64
	for _, capacity := range capacities {
		if capacity.StorageClassName != class.Name || capacity.Capacity == nil || capacity.NodeTopology == nil {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(capacity.NodeTopology)
		if err != nil {
			continue
		}
		bytes := capacity.Capacity.Value()
		if capacity.MaximumVolumeSize != nil {
			bytes = min(bytes, capacity.MaximumVolumeSize.Value())
		}
		matched := false
		for _, node := range nodes {
			if selector.Matches(labels.Set(node.Labels)) {
				item.Nodes = append(item.Nodes, node.Labels["kubernetes.io/hostname"])
				matched = true
			}
		}
		if matched {
			mergeQuota(&minimum, max(0, bytes))
		}
	}
	if minimum == nil {
		return nil
	}
	for _, claim := range claims {
		if claim.Spec.StorageClassName != nil && *claim.Spec.StorageClassName == class.Name && claim.Status.Phase != corev1.ClaimBound {
			*minimum -= claim.Spec.Resources.Requests.Storage().Value()
		}
	}
	slices.Sort(item.Nodes)
	item.Nodes = slices.Compact(item.Nodes)
	item.AvailableGiB = max(0, *minimum) / (1 << 30)
	return []domain.CapacityStorage{item}
}

func kubeCapacityVMAccounted(uid, namespace string, pods []corev1.Pod, claims []corev1.PersistentVolumeClaim) bool {
	if uid == "" {
		return false
	}
	for _, pod := range pods {
		if pod.Namespace != namespace || pod.Spec.NodeName == "" || pod.Status.Phase != corev1.PodRunning {
			continue
		}
		// Caller established the VM -> VMI UID chain; names cannot prove ownership.
		owned := false
		for _, owner := range pod.OwnerReferences {
			owned = owned || owner.Kind == "VirtualMachineInstance" && string(owner.UID) == uid
		}
		if !owned {
			continue
		}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			bound := false
			for _, claim := range claims {
				if claim.Namespace == namespace && claim.Name == volume.PersistentVolumeClaim.ClaimName && claim.Status.Phase == corev1.ClaimBound {
					bound = true
					break
				}
			}
			if !bound {
				return false
			}
		}
		return true
	}
	return false
}
