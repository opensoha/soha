package kubernetes

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"

	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
)

type nodeResourceTotals struct {
	cpuMilli       int64
	memoryBytes    int64
	ephemeralBytes int64
	pods           int64
}

type nodeAggregate struct {
	podCount int
	requests nodeResourceTotals
	limits   nodeResourceTotals
	pods     []domainresource.NodePodView
}

func buildNodeViews(nodes []corev1.Node, pods []corev1.Pod) []domainresource.NodeView {
	aggregates := buildNodeAggregates(pods, false)
	items := make([]domainresource.NodeView, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, domainresource.NodeView{
			Name:       node.Name,
			Status:     nodeStatus(node),
			Roles:      nodeRoles(node),
			Version:    node.Status.NodeInfo.KubeletVersion,
			InternalIP: nodeInternalIP(node),
			PodCount:   aggregates[node.Name].podCount,
			AgeSeconds: secondsSince(node.CreationTimestamp.Time),
			Resources:  buildNodeResourceSummary(node, aggregates[node.Name]),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func buildNodeDetail(node corev1.Node, pods []corev1.Pod) domainresource.NodeDetailView {
	nodePods := make([]corev1.Pod, 0)
	for _, pod := range pods {
		if strings.TrimSpace(pod.Spec.NodeName) == node.Name {
			nodePods = append(nodePods, pod)
		}
	}
	aggregate := buildNodeAggregates(nodePods, true)[node.Name]
	return domainresource.NodeDetailView{
		Name:        node.Name,
		Status:      nodeStatus(node),
		Roles:       nodeRoles(node),
		Version:     node.Status.NodeInfo.KubeletVersion,
		InternalIP:  nodeInternalIP(node),
		PodCount:    aggregate.podCount,
		AgeSeconds:  secondsSince(node.CreationTimestamp.Time),
		Labels:      cloneStringMap(node.Labels),
		Annotations: cloneStringMap(node.Annotations),
		Conditions:  mapNodeConditions(node),
		Resources:   buildNodeResourceSummary(node, aggregate),
		Pods:        aggregate.pods,
	}
}

func buildNodeResourceSummary(node corev1.Node, aggregate nodeAggregate) domainresource.NodeResourceSummaryView {
	capacity := nodeResourceTotalsFromList(node.Status.Capacity)
	allocatable := nodeResourceTotalsFromList(node.Status.Allocatable)
	return domainresource.NodeResourceSummaryView{
		Capacity:           formatNodeResourceTotals(capacity),
		Allocatable:        formatNodeResourceTotals(allocatable),
		Requests:           formatNodeResourceTotals(aggregate.requests),
		Limits:             formatNodeResourceTotals(aggregate.limits),
		Usage:              domainresource.ResourceQuantityView{Pods: fmt.Sprintf("%d", aggregate.podCount)},
		RequestPercentages: calculateResourcePercentages(aggregate.requests, allocatable),
		LimitPercentages:   calculateResourcePercentages(aggregate.limits, allocatable),
		UsagePercentages:   domainresource.ResourcePercentageView{Pods: resourcePercentage(int64(aggregate.podCount), allocatable.pods)},
	}
}

func buildNodeAggregates(pods []corev1.Pod, includePods bool) map[string]nodeAggregate {
	out := make(map[string]nodeAggregate)
	for _, pod := range pods {
		nodeName := strings.TrimSpace(pod.Spec.NodeName)
		if nodeName == "" {
			continue
		}
		aggregate := out[nodeName]
		aggregate.podCount++
		requests, limits := podResourceTotals(pod)
		if !isTerminalPod(pod) {
			aggregate.requests.add(requests)
			aggregate.limits.add(limits)
		}
		if includePods {
			aggregate.pods = append(aggregate.pods, buildNodePodView(pod, requests, limits))
		}
		out[nodeName] = aggregate
	}
	if includePods {
		for nodeName, aggregate := range out {
			sort.Slice(aggregate.pods, func(i, j int) bool {
				if aggregate.pods[i].Namespace == aggregate.pods[j].Namespace {
					return aggregate.pods[i].Name < aggregate.pods[j].Name
				}
				return aggregate.pods[i].Namespace < aggregate.pods[j].Namespace
			})
			out[nodeName] = aggregate
		}
	}
	return out
}

func buildNodePodView(pod corev1.Pod, requests, limits nodeResourceTotals) domainresource.NodePodView {
	var ready int
	var restarts int32
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
		restarts += status.RestartCount
	}
	return domainresource.NodePodView{
		Name:            pod.Name,
		Namespace:       pod.Namespace,
		Phase:           string(pod.Status.Phase),
		PodIP:           pod.Status.PodIP,
		ReadyContainers: fmt.Sprintf("%d/%d", ready, len(pod.Status.ContainerStatuses)),
		Restarts:        restarts,
		Labels:          cloneStringMap(pod.Labels),
		Requests:        formatNodeResourceTotals(requests),
		Limits:          formatNodeResourceTotals(limits),
		AgeSeconds:      secondsSince(pod.CreationTimestamp.Time),
	}
}

func podResourceTotals(pod corev1.Pod) (nodeResourceTotals, nodeResourceTotals) {
	var appRequests nodeResourceTotals
	var appLimits nodeResourceTotals
	for _, container := range pod.Spec.Containers {
		appRequests.add(nodeResourceTotalsFromList(container.Resources.Requests))
		appLimits.add(nodeResourceTotalsFromList(container.Resources.Limits))
	}

	var initMaxRequests nodeResourceTotals
	var initMaxLimits nodeResourceTotals
	for _, container := range pod.Spec.InitContainers {
		initRequests := nodeResourceTotalsFromList(container.Resources.Requests)
		initLimits := nodeResourceTotalsFromList(container.Resources.Limits)
		initMaxRequests.max(initRequests)
		initMaxLimits.max(initLimits)
	}

	appRequests.max(initMaxRequests)
	appLimits.max(initMaxLimits)
	appRequests.add(nodeResourceTotalsFromList(pod.Spec.Overhead))
	appLimits.add(nodeResourceTotalsFromList(pod.Spec.Overhead))
	return appRequests, appLimits
}

func nodeResourceTotalsFromList(items corev1.ResourceList) nodeResourceTotals {
	var totals nodeResourceTotals
	if quantity, ok := items[corev1.ResourceCPU]; ok {
		totals.cpuMilli = quantity.MilliValue()
	}
	if quantity, ok := items[corev1.ResourceMemory]; ok {
		totals.memoryBytes = quantity.Value()
	}
	if quantity, ok := items[corev1.ResourceEphemeralStorage]; ok {
		totals.ephemeralBytes = quantity.Value()
	}
	if quantity, ok := items[corev1.ResourcePods]; ok {
		totals.pods = quantity.Value()
	}
	return totals
}

func formatNodeResourceTotals(totals nodeResourceTotals) domainresource.ResourceQuantityView {
	view := domainresource.ResourceQuantityView{}
	if totals.cpuMilli > 0 {
		view.CPU = apiresource.NewMilliQuantity(totals.cpuMilli, apiresource.DecimalSI).String()
	}
	if totals.memoryBytes > 0 {
		view.Memory = apiresource.NewQuantity(totals.memoryBytes, apiresource.BinarySI).String()
	}
	if totals.ephemeralBytes > 0 {
		view.EphemeralStorage = apiresource.NewQuantity(totals.ephemeralBytes, apiresource.BinarySI).String()
	}
	if totals.pods > 0 {
		view.Pods = fmt.Sprintf("%d", totals.pods)
	}
	return view
}

func calculateResourcePercentages(current, total nodeResourceTotals) domainresource.ResourcePercentageView {
	return domainresource.ResourcePercentageView{
		CPU:              resourcePercentage(current.cpuMilli, total.cpuMilli),
		Memory:           resourcePercentage(current.memoryBytes, total.memoryBytes),
		EphemeralStorage: resourcePercentage(current.ephemeralBytes, total.ephemeralBytes),
		Pods:             resourcePercentage(current.pods, total.pods),
	}
}

func resourcePercentage(current, total int64) float64 {
	if current <= 0 || total <= 0 {
		return 0
	}
	return math.Round((float64(current)/float64(total))*1000) / 10
}

func (t *nodeResourceTotals) add(other nodeResourceTotals) {
	t.cpuMilli += other.cpuMilli
	t.memoryBytes += other.memoryBytes
	t.ephemeralBytes += other.ephemeralBytes
	t.pods += other.pods
}

func (t *nodeResourceTotals) max(other nodeResourceTotals) {
	if other.cpuMilli > t.cpuMilli {
		t.cpuMilli = other.cpuMilli
	}
	if other.memoryBytes > t.memoryBytes {
		t.memoryBytes = other.memoryBytes
	}
	if other.ephemeralBytes > t.ephemeralBytes {
		t.ephemeralBytes = other.ephemeralBytes
	}
	if other.pods > t.pods {
		t.pods = other.pods
	}
}

func nodeRoles(item corev1.Node) []string {
	roles := make([]string, 0)
	for key := range item.Labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(key, "node-role.kubernetes.io/"))
		}
	}
	sort.Strings(roles)
	return roles
}

func nodeInternalIP(item corev1.Node) string {
	for _, address := range item.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address
		}
	}
	return ""
}

func nodeStatus(item corev1.Node) string {
	status := "unknown"
	for _, condition := range item.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			if condition.Status == corev1.ConditionTrue {
				status = "ready"
			} else {
				status = "not_ready"
			}
			break
		}
	}
	return status
}

func mapNodeConditions(item corev1.Node) []domainresource.WorkloadConditionView {
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		lastTransitionTime := ""
		if !condition.LastTransitionTime.Time.IsZero() {
			lastTransitionTime = condition.LastTransitionTime.Time.UTC().Format(time.RFC3339)
		}
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: lastTransitionTime,
		})
	}
	return conditions
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func isTerminalPod(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}
