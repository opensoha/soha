package resource

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domaincluster "github.com/kubecrux/kubecrux/internal/domain/cluster"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
	domainsettings "github.com/kubecrux/kubecrux/internal/domain/settings"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
)

type resourceTotals struct {
	cpuMilli       int64
	memoryBytes    int64
	ephemeralBytes int64
	pods           int64
}

type nodeAggregate struct {
	podCount int
	requests resourceTotals
	limits   resourceTotals
	usage    resourceTotals
	pods     []domainresource.NodePodView
}

const nodePodUsageTimeout = 1200 * time.Millisecond

func (s *Service) GetNodeDetail(ctx context.Context, principal domainidentity.Principal, clusterID, nodeName string) (domainresource.NodeDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "Node", domainaccess.ActionView)
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}

	var (
		item   domainresource.NodeDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.NodeDetailView{}, err
		}
		item, err = client.GetNodeDetail(ctx, nodeName)
		if err != nil {
			return domainresource.NodeDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		s.applyNodeUsageMetrics(ctx, clusterID, &item)
		source = "agent"
	default:
		rawNode, err := s.getDirectNode(ctx, clusterID, nodeName)
		if err != nil {
			return domainresource.NodeDetailView{}, err
		}
		rawPods, _, err := s.listDirectPods(ctx, clusterID, metav1.NamespaceAll)
		if err != nil {
			return domainresource.NodeDetailView{}, err
		}
		item = s.buildNodeDetail(ctx, clusterID, *rawNode, rawPods, decision)
		source = "direct"
	}

	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "Node", nodeName, string(domainaccess.ActionView), "success", fmt.Sprintf("read node detail via %s", source))
	return item, nil
}

func (s *Service) applyNodeUsageMetrics(ctx context.Context, clusterID string, item *domainresource.NodeDetailView) {
	if item == nil {
		return
	}

	settings, err := s.resolveClusterPrometheusSettings(ctx, clusterID)
	switch {
	case err != nil:
		item.MetricsMessage = err.Error()
		return
	case !settings.Enabled || strings.TrimSpace(settings.BaseURL) == "":
		item.MetricsMessage = "prometheus is not configured"
		return
	}

	rawPods := make([]corev1.Pod, 0, len(item.Pods))
	for _, pod := range item.Pods {
		rawPods = append(rawPods, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace}})
	}

	values, err := s.listNodePodUsageValues(ctx, settings, clusterID, rawPods)
	item.MetricsConfigured = true
	if err != nil {
		item.MetricsMessage = err.Error()
		return
	}

	var usage resourceTotals
	usage.pods = int64(item.PodCount)
	for index := range item.Pods {
		key := podMetricsKey(item.Pods[index].Namespace, item.Pods[index].Name)
		value, ok := values[key]
		if !ok {
			continue
		}
		if value.CPUCores > 0 {
			item.Pods[index].CPU = formatCPUUsage(value.CPUCores)
			usage.cpuMilli += int64(math.Round(value.CPUCores * 1000))
		}
		if value.MemoryBytes > 0 {
			item.Pods[index].Memory = formatMemoryBytes(value.MemoryBytes)
			usage.memoryBytes += int64(math.Round(value.MemoryBytes))
		}
	}

	item.Resources.Usage = formatResourceTotals(usage)
	item.Resources.UsagePercentages = calculateResourcePercentages(usage, resourceTotalsFromQuantityView(item.Resources.Allocatable))
	if len(values) == 0 && len(item.Pods) > 0 {
		item.MetricsMessage = "prometheus returned no pod metrics for this node"
	}
}

func buildNodeViews(nodes []corev1.Node, pods []corev1.Pod, decision domainaccess.Decision) []domainresource.NodeView {
	aggregates := buildNodeAggregates(pods, nil, false)
	items := make([]domainresource.NodeView, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, buildNodeView(node, aggregates[node.Name], decision))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (s *Service) buildNodeDetail(ctx context.Context, clusterID string, node corev1.Node, pods []corev1.Pod, decision domainaccess.Decision) domainresource.NodeDetailView {
	nodePods := make([]corev1.Pod, 0)
	for _, pod := range pods {
		if strings.TrimSpace(pod.Spec.NodeName) == node.Name {
			nodePods = append(nodePods, pod)
		}
	}

	metricsConfigured := false
	metricsMessage := ""
	usageValues := map[string]podUsageValue(nil)
	settings, settingsErr := s.resolveClusterPrometheusSettings(ctx, clusterID)
	switch {
	case settingsErr != nil:
		metricsMessage = settingsErr.Error()
	case !settings.Enabled || strings.TrimSpace(settings.BaseURL) == "":
		metricsMessage = "prometheus is not configured"
	default:
		metricsConfigured = true
		values, err := s.listNodePodUsageValues(ctx, settings, clusterID, nodePods)
		if err != nil {
			metricsMessage = err.Error()
		} else {
			usageValues = values
			if len(values) == 0 && len(nodePods) > 0 {
				metricsMessage = "prometheus returned no pod metrics for this node"
			}
		}
	}

	aggregate := buildNodeAggregates(nodePods, usageValues, true)[node.Name]
	item := domainresource.NodeDetailView{
		Name:              node.Name,
		Status:            nodeStatus(node),
		Roles:             nodeRoles(node),
		Version:           node.Status.NodeInfo.KubeletVersion,
		InternalIP:        nodeInternalIP(node),
		PodCount:          aggregate.podCount,
		AgeSeconds:        secondsSince(node.CreationTimestamp.Time),
		Labels:            cloneStringMap(node.Labels),
		Annotations:       cloneStringMap(node.Annotations),
		Taints:            mapNodeTaints(node.Spec.Taints),
		Conditions:        mapNodeConditions(node),
		Resources:         buildNodeResourceSummary(node, aggregate),
		MetricsConfigured: metricsConfigured,
		MetricsMessage:    metricsMessage,
		Pods:              aggregate.pods,
		AllowedActions:    stringifyActions(decision.AllowedActions),
	}
	return item
}

func (s *Service) listNodePodUsageValues(ctx context.Context, settings domainsettings.PrometheusSettings, clusterID string, pods []corev1.Pod) (map[string]podUsageValue, error) {
	metricsCtx, cancel := context.WithTimeout(ctx, nodePodUsageTimeout)
	defer cancel()

	values, err := s.listPodUsageValues(metricsCtx, settings, clusterID, pods)
	if err == nil {
		return values, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(metricsCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("timed out while querying prometheus pod metrics")
	}
	return nil, err
}

func mapNodeTaints(items []corev1.Taint) []domainresource.NodeTaintView {
	out := make([]domainresource.NodeTaintView, 0, len(items))
	for _, item := range items {
		out = append(out, domainresource.NodeTaintView{
			Key:    item.Key,
			Value:  item.Value,
			Effect: string(item.Effect),
		})
	}
	return out
}

func buildNodeView(node corev1.Node, aggregate nodeAggregate, decision domainaccess.Decision) domainresource.NodeView {
	return domainresource.NodeView{
		Name:           node.Name,
		Status:         nodeStatus(node),
		Roles:          nodeRoles(node),
		Version:        node.Status.NodeInfo.KubeletVersion,
		InternalIP:     nodeInternalIP(node),
		PodCount:       aggregate.podCount,
		AgeSeconds:     secondsSince(node.CreationTimestamp.Time),
		Resources:      buildNodeResourceSummary(node, aggregate),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}

func buildNodeResourceSummary(node corev1.Node, aggregate nodeAggregate) domainresource.NodeResourceSummaryView {
	capacity := resourceTotalsFromList(node.Status.Capacity)
	allocatable := resourceTotalsFromList(node.Status.Allocatable)
	aggregate.usage.pods = int64(aggregate.podCount)

	return domainresource.NodeResourceSummaryView{
		Capacity:           formatResourceTotals(capacity),
		Allocatable:        formatResourceTotals(allocatable),
		Requests:           formatResourceTotals(aggregate.requests),
		Limits:             formatResourceTotals(aggregate.limits),
		Usage:              formatResourceTotals(aggregate.usage),
		RequestPercentages: calculateResourcePercentages(aggregate.requests, allocatable),
		LimitPercentages:   calculateResourcePercentages(aggregate.limits, allocatable),
		UsagePercentages:   calculateResourcePercentages(aggregate.usage, allocatable),
	}
}

func buildNodeAggregates(pods []corev1.Pod, usageValues map[string]podUsageValue, includePods bool) map[string]nodeAggregate {
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
		if usage, ok := usageValues[podMetricsKey(pod.Namespace, pod.Name)]; ok && !isTerminalPod(pod) {
			aggregate.usage.cpuMilli += int64(math.Round(usage.CPUCores * 1000))
			aggregate.usage.memoryBytes += int64(math.Round(usage.MemoryBytes))
		}
		if includePods {
			aggregate.pods = append(aggregate.pods, buildNodePodView(pod, requests, limits, usageValues))
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

func buildNodePodView(pod corev1.Pod, requests, limits resourceTotals, usageValues map[string]podUsageValue) domainresource.NodePodView {
	view := domainresource.NodePodView{
		Name:            pod.Name,
		Namespace:       pod.Namespace,
		Phase:           string(pod.Status.Phase),
		PodIP:           pod.Status.PodIP,
		ReadyContainers: readyContainers(pod),
		Restarts:        podRestartCount(pod),
		Labels:          cloneStringMap(pod.Labels),
		Requests:        formatResourceTotals(requests),
		Limits:          formatResourceTotals(limits),
		AgeSeconds:      secondsSince(pod.CreationTimestamp.Time),
	}
	if usage, ok := usageValues[podMetricsKey(pod.Namespace, pod.Name)]; ok {
		if usage.CPUCores > 0 {
			view.CPU = formatCPUUsage(usage.CPUCores)
		}
		if usage.MemoryBytes > 0 {
			view.Memory = formatMemoryBytes(usage.MemoryBytes)
		}
	}
	return view
}

func podResourceTotals(pod corev1.Pod) (resourceTotals, resourceTotals) {
	var appRequests resourceTotals
	var appLimits resourceTotals
	for _, container := range pod.Spec.Containers {
		appRequests.add(resourceTotalsFromList(container.Resources.Requests))
		appLimits.add(resourceTotalsFromList(container.Resources.Limits))
	}

	var initMaxRequests resourceTotals
	var initMaxLimits resourceTotals
	for _, container := range pod.Spec.InitContainers {
		initRequests := resourceTotalsFromList(container.Resources.Requests)
		initLimits := resourceTotalsFromList(container.Resources.Limits)
		initMaxRequests.max(initRequests)
		initMaxLimits.max(initLimits)
	}

	appRequests.max(initMaxRequests)
	appLimits.max(initMaxLimits)
	appRequests.add(resourceTotalsFromList(pod.Spec.Overhead))
	appLimits.add(resourceTotalsFromList(pod.Spec.Overhead))
	return appRequests, appLimits
}

func resourceTotalsFromList(items corev1.ResourceList) resourceTotals {
	if len(items) == 0 {
		return resourceTotals{}
	}
	var totals resourceTotals
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

func resourceTotalsFromQuantityView(view domainresource.ResourceQuantityView) resourceTotals {
	var totals resourceTotals
	if quantity, err := apiresource.ParseQuantity(strings.TrimSpace(view.CPU)); err == nil {
		totals.cpuMilli = quantity.MilliValue()
	}
	if quantity, err := apiresource.ParseQuantity(strings.TrimSpace(view.Memory)); err == nil {
		totals.memoryBytes = quantity.Value()
	}
	if quantity, err := apiresource.ParseQuantity(strings.TrimSpace(view.EphemeralStorage)); err == nil {
		totals.ephemeralBytes = quantity.Value()
	}
	if strings.TrimSpace(view.Pods) != "" {
		if quantity, err := apiresource.ParseQuantity(strings.TrimSpace(view.Pods)); err == nil {
			totals.pods = quantity.Value()
		}
	}
	return totals
}

func formatResourceTotals(totals resourceTotals) domainresource.ResourceQuantityView {
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

func calculateResourcePercentages(current, total resourceTotals) domainresource.ResourcePercentageView {
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

func (t *resourceTotals) add(other resourceTotals) {
	t.cpuMilli += other.cpuMilli
	t.memoryBytes += other.memoryBytes
	t.ephemeralBytes += other.ephemeralBytes
	t.pods += other.pods
}

func (t *resourceTotals) max(other resourceTotals) {
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

func readyContainers(pod corev1.Pod) string {
	var ready int
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
	}
	return fmt.Sprintf("%d/%d", ready, len(pod.Status.ContainerStatuses))
}

func podRestartCount(pod corev1.Pod) int32 {
	var total int32
	for _, status := range pod.Status.ContainerStatuses {
		total += status.RestartCount
	}
	return total
}

func isTerminalPod(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}
