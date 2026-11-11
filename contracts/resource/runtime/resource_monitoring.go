package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

// ReadResourceMonitoringInventory returns bounded current aggregate facts, not Pod manifests.
func ReadResourceMonitoringInventory(ctx context.Context, client rest.Interface, clusterID string) (cr.ResourceMonitoringInventory, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	now := time.Now().UTC().Format(time.RFC3339)
	output := cr.ResourceMonitoringInventory{Nodes: []cr.ResourceMonitoringNode{}, Quality: "valid", ObservedAt: now}
	var nodes corev1.NodeList
	if err := readMonitoringList(ctx, client, "nodes", cr.ResourceMonitoringMaxNodes, 2<<20, &nodes); err != nil {
		return output, err
	}
	var pods corev1.PodList
	podErr := readMonitoringList(ctx, client, "pods", 5000, 16<<20, &pods)
	complete := podErr == nil && pods.Continue == "" && len(pods.Items) <= 5000
	if !complete {
		output.Quality = "partial"
		output.ReasonCode = "pod_snapshot_incomplete"
		output.Truncated = pods.Continue != "" || len(pods.Items) > 5000
	}
	if nodes.Continue != "" || len(nodes.Items) > cr.ResourceMonitoringMaxNodes {
		output.Quality = "partial"
		output.ReasonCode = "node_budget"
		output.Truncated = true
	}
	aggregates := monitoringPodAggregates(pods.Items)
	if complete {
		output.UnscheduledPendingPods = number(aggregates[""].pending)
	}
	for index, node := range nodes.Items {
		if index >= cr.ResourceMonitoringMaxNodes {
			break
		}
		fact := monitoringNodeFact(clusterID, node, aggregates[node.Name], complete, now)
		output.Nodes = append(output.Nodes, fact)
	}

	sort.Slice(output.Nodes, func(i, j int) bool { return output.Nodes[i].NodeRef.Name < output.Nodes[j].NodeRef.Name })
	return output, nil
}

func readMonitoringList(ctx context.Context, client rest.Interface, resource string, limit, maxBytes int, target any) error {
	if client == nil {
		return fmt.Errorf("kubernetes REST client unavailable")
	}
	stream, err := client.Get().AbsPath("/api/v1/"+resource).Param("limit", fmt.Sprint(limit)).SetHeader("Accept", "application/json").Stream(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	raw, err := io.ReadAll(io.LimitReader(stream, int64(maxBytes)+1))
	if err != nil {
		return err
	}
	if len(raw) > maxBytes {
		return fmt.Errorf("monitoring inventory byte budget")
	}
	return json.Unmarshal(raw, target)
}

type monitoringPodAggregate struct {
	cpuRequest, memoryRequest, cpuLimit, memoryLimit, count, pending float64
	cpuComplete, memoryComplete                                      bool
	initialized                                                      bool
}

func monitoringPodAggregates(pods []corev1.Pod) map[string]monitoringPodAggregate {
	result := map[string]monitoringPodAggregate{}
	for _, pod := range pods {
		if monitoringTerminalPod(pod) {
			continue
		}
		aggregate := result[pod.Spec.NodeName]
		if !aggregate.initialized {
			aggregate.cpuComplete = true
			aggregate.memoryComplete = true
			aggregate.initialized = true
		}
		aggregate.count++
		if pod.Status.Phase == corev1.PodPending {
			aggregate.pending++
		}
		requests, limits, cpuComplete, memoryComplete := effectiveMonitoringPodResources(pod)
		aggregate.cpuRequest += requests.cpu
		aggregate.memoryRequest += requests.memory
		aggregate.cpuLimit += limits.cpu
		aggregate.memoryLimit += limits.memory
		aggregate.cpuComplete = aggregate.cpuComplete && cpuComplete
		aggregate.memoryComplete = aggregate.memoryComplete && memoryComplete
		result[pod.Spec.NodeName] = aggregate
	}
	return result
}
func monitoringTerminalPod(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

type monitoringResources struct{ cpu, memory float64 }

func monitoringResourcesFromList(values corev1.ResourceList) monitoringResources {
	cpu := values[corev1.ResourceCPU]
	memory := values[corev1.ResourceMemory]
	return monitoringResources{cpu: float64(cpu.MilliValue()) / 1000, memory: float64(memory.Value())}
}
func (r monitoringResources) add(other monitoringResources) monitoringResources {
	return monitoringResources{cpu: r.cpu + other.cpu, memory: r.memory + other.memory}
}
func (r monitoringResources) max(other monitoringResources) monitoringResources {
	return monitoringResources{cpu: math.Max(r.cpu, other.cpu), memory: math.Max(r.memory, other.memory)}
}

func effectiveMonitoringPodResources(pod corev1.Pod) (monitoringResources, monitoringResources, bool, bool) {
	var regularRequest, regularLimit, sidecarRequest, sidecarLimit, initRequest, initLimit monitoringResources
	cpuComplete, memoryComplete := true, true
	addCoverage := func(container corev1.Container) {
		cpuValue, cpu := container.Resources.Limits[corev1.ResourceCPU]
		memoryValue, memory := container.Resources.Limits[corev1.ResourceMemory]
		cpu = cpu && !cpuValue.IsZero()
		memory = memory && !memoryValue.IsZero()
		cpuComplete = cpuComplete && cpu
		memoryComplete = memoryComplete && memory
	}
	for _, container := range pod.Spec.Containers {
		regularRequest = regularRequest.add(monitoringResourcesFromList(container.Resources.Requests))
		regularLimit = regularLimit.add(monitoringResourcesFromList(container.Resources.Limits))
		addCoverage(container)
	}
	for _, container := range pod.Spec.InitContainers {
		requests := monitoringResourcesFromList(container.Resources.Requests)
		limits := monitoringResourcesFromList(container.Resources.Limits)
		addCoverage(container)
		if container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			sidecarRequest = sidecarRequest.add(requests)
			sidecarLimit = sidecarLimit.add(limits)
			requests = sidecarRequest
			limits = sidecarLimit
		} else {
			requests = requests.add(sidecarRequest)
			limits = limits.add(sidecarLimit)
		}
		initRequest = initRequest.max(requests)
		initLimit = initLimit.max(limits)
	}
	requests := regularRequest.add(sidecarRequest).max(initRequest)
	limits := regularLimit.add(sidecarLimit).max(initLimit)
	if pod.Spec.Resources != nil {
		requests = monitoringPodLevelOverride(requests, pod.Spec.Resources.Requests)
		limits = monitoringPodLevelOverride(limits, pod.Spec.Resources.Limits)
		if value, ok := pod.Spec.Resources.Limits[corev1.ResourceCPU]; ok {
			cpuComplete = !value.IsZero()
		}
		if value, ok := pod.Spec.Resources.Limits[corev1.ResourceMemory]; ok {
			memoryComplete = !value.IsZero()
		}
	}
	overhead := monitoringResourcesFromList(pod.Spec.Overhead)
	requests = requests.add(overhead)
	if limits.cpu > 0 {
		limits.cpu += overhead.cpu
	}
	if limits.memory > 0 {
		limits.memory += overhead.memory
	}
	return requests, limits, cpuComplete, memoryComplete
}
func monitoringPodLevelOverride(current monitoringResources, values corev1.ResourceList) monitoringResources {
	if value, ok := values[corev1.ResourceCPU]; ok {
		current.cpu = float64(value.MilliValue()) / 1000
	}
	if value, ok := values[corev1.ResourceMemory]; ok {
		current.memory = float64(value.Value())
	}
	return current
}
func monitoringQuantity(values corev1.ResourceList) cr.ResourceMonitoringQuantity {
	result := cr.ResourceMonitoringQuantity{}
	if cpu, ok := values[corev1.ResourceCPU]; ok {
		result.CPUCores = number(float64(cpu.MilliValue()) / 1000)
	}
	if memory, ok := values[corev1.ResourceMemory]; ok {
		result.MemoryBytes = number(float64(memory.Value()))
	}
	if pods, ok := values[corev1.ResourcePods]; ok {
		result.PodSlots = number(float64(pods.Value()))
	}
	return result
}
func monitoringNodeFact(clusterID string, node corev1.Node, aggregate monitoringPodAggregate, complete bool, now string) cr.ResourceMonitoringNode {
	fact := cr.ResourceMonitoringNode{NodeRef: cr.ResourceRef{ClusterID: clusterID, APIVersion: "v1", Kind: "Node", Name: node.Name, ScopeMode: cr.ResourceScopeModeCluster, UID: string(node.UID), ResourceVersion: node.ResourceVersion}, OS: node.Status.NodeInfo.OperatingSystem, CreatedAt: node.CreationTimestamp.UTC().Format(time.RFC3339), Ready: "Unknown", MemoryPressure: "Unknown", DiskPressure: "Unknown", PIDPressure: "Unknown", Cordoned: node.Spec.Unschedulable, Capacity: monitoringQuantity(node.Status.Capacity), Allocatable: monitoringQuantity(node.Status.Allocatable), Quality: "valid", ObservedAt: now}
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			fact.InternalIP = address.Address
			break
		}
	}
	for _, condition := range node.Status.Conditions {
		switch condition.Type {
		case corev1.NodeReady:
			fact.Ready = string(condition.Status)
		case corev1.NodeMemoryPressure:
			fact.MemoryPressure = string(condition.Status)
		case corev1.NodeDiskPressure:
			fact.DiskPressure = string(condition.Status)
		case corev1.NodePIDPressure:
			fact.PIDPressure = string(condition.Status)
		}
	}
	if !complete {
		fact.Quality = "partial"
		return fact
	}
	fact.Requests = cr.ResourceMonitoringQuantity{CPUCores: number(aggregate.cpuRequest), MemoryBytes: number(aggregate.memoryRequest)}
	fact.CPULimitsComplete = !aggregate.initialized || aggregate.cpuComplete
	fact.MemoryLimitsComplete = !aggregate.initialized || aggregate.memoryComplete
	if fact.CPULimitsComplete {
		fact.Limits.CPUCores = number(aggregate.cpuLimit)
	}
	if fact.MemoryLimitsComplete {
		fact.Limits.MemoryBytes = number(aggregate.memoryLimit)
	}
	fact.BoundPods = number(aggregate.count)
	fact.PendingPods = number(aggregate.pending)
	if fact.Allocatable.PodSlots != nil {
		fact.RemainingPodSlots = number(math.Max(0, *fact.Allocatable.PodSlots-aggregate.count))
		fact.OverflowPodSlots = number(math.Max(0, aggregate.count-*fact.Allocatable.PodSlots))
	}
	return fact
}
func number(value float64) *float64 { return &value }
