package resourcebackend

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func (d *Direct) ListNamespaces(ctx context.Context, clusterID string) ([]domainresource.NamespaceView, string, error) {
	var (
		items  []corev1.Namespace
		source string
	)
	if d.cache != nil {
		cached, err := d.cache.ListNamespaces(clusterID)
		if err == nil {
			items, source = cached, "cache"
		} else if !d.cache.CacheUnavailable(err) {
			return nil, "cache", err
		}
	}
	if source == "" {
		bundle, err := d.directClients(ctx, clusterID)
		if err != nil {
			return nil, "live", err
		}
		queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		result, err := bundle.Typed.CoreV1().Namespaces().List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, "live", err
		}
		items, source = result.Items, "live"
	}
	views := make([]domainresource.NamespaceView, 0, len(items))
	for _, item := range items {
		views = append(views, mapNamespace(item))
	}
	return views, source, nil
}

func (d *Direct) CreateNamespace(ctx context.Context, clusterID string, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainresource.NamespaceView{}, fmt.Errorf("namespace name is required")
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	created, err := bundle.Typed.CoreV1().Namespaces().Create(queryCtx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: name, Labels: sanitizeStringMap(input.Labels), Annotations: sanitizeStringMap(input.Annotations),
	}}, metav1.CreateOptions{})
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	return mapNamespace(*created), nil
}

func (d *Direct) UpdateNamespace(ctx context.Context, clusterID, namespace string, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().Namespaces().Get(queryCtx, namespace, metav1.GetOptions{})
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	item.Labels, item.Annotations = sanitizeStringMap(input.Labels), sanitizeStringMap(input.Annotations)
	updated, err := bundle.Typed.CoreV1().Namespaces().Update(queryCtx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	return mapNamespace(*updated), nil
}

func (d *Direct) DeleteNamespace(ctx context.Context, clusterID, namespace string) error {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return bundle.Typed.CoreV1().Namespaces().Delete(queryCtx, namespace, metav1.DeleteOptions{})
}

func (d *Direct) ListNodes(ctx context.Context, clusterID string) ([]domainresource.NodeView, string, error) {
	nodes, source, err := d.listNodes(ctx, clusterID)
	if err != nil {
		return nil, source, err
	}
	pods, err := d.listAllPods(ctx, clusterID)
	if err != nil {
		return nil, source, err
	}
	aggregates := buildNodeAggregates(pods, false)
	views := make([]domainresource.NodeView, 0, len(nodes))
	for _, node := range nodes {
		views = append(views, buildNodeView(node, aggregates[node.Name]))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })
	return views, source, nil
}

func (d *Direct) GetNodeDetail(ctx context.Context, clusterID, name string) (domainresource.NodeDetailView, error) {
	node, pods, err := d.nodeAndPods(ctx, clusterID, name)
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	return buildNodeDetail(node, pods), nil
}

func (d *Direct) UpdateNode(ctx context.Context, clusterID, name string, input domainresource.NodeUpdateInput) (domainresource.NodeDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().Nodes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	item.Labels = sanitizeStringMap(input.Labels)
	item.Spec.Taints = make([]corev1.Taint, 0, len(input.Taints))
	for _, taint := range input.Taints {
		if strings.TrimSpace(taint.Key) == "" || strings.TrimSpace(taint.Effect) == "" {
			continue
		}
		item.Spec.Taints = append(item.Spec.Taints, corev1.Taint{Key: strings.TrimSpace(taint.Key), Value: strings.TrimSpace(taint.Value), Effect: corev1.TaintEffect(strings.TrimSpace(taint.Effect))})
	}
	updated, err := bundle.Typed.CoreV1().Nodes().Update(queryCtx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	pods, err := d.listAllPods(ctx, clusterID)
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	return buildNodeDetail(*updated, pods), nil
}

func (d *Direct) GetNodeYAML(ctx context.Context, clusterID, name string) (domainresource.ResourceYAMLView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().Nodes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "Node", Name: name, Content: string(content)}, nil
}

func (d *Direct) DeleteNode(ctx context.Context, clusterID, name string) error {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return bundle.Typed.CoreV1().Nodes().Delete(queryCtx, name, metav1.DeleteOptions{})
}

func (d *Direct) listNodes(ctx context.Context, clusterID string) ([]corev1.Node, string, error) {
	if d.cache != nil {
		items, err := d.cache.ListNodes(clusterID)
		if err == nil {
			return items, "cache", nil
		}
		if !d.cache.CacheUnavailable(err) {
			return nil, "cache", err
		}
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, "live", err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().Nodes().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, "live", err
	}
	return items.Items, "live", nil
}

func (d *Direct) listAllPods(ctx context.Context, clusterID string) ([]corev1.Pod, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().Pods(metav1.NamespaceAll).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}

func (d *Direct) nodeAndPods(ctx context.Context, clusterID, name string) (corev1.Node, []corev1.Pod, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return corev1.Node{}, nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	node, err := bundle.Typed.CoreV1().Nodes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return corev1.Node{}, nil, err
	}
	pods, err := d.listAllPods(ctx, clusterID)
	return *node, pods, err
}

type resourceTotals struct{ cpuMilli, memoryBytes, ephemeralBytes, pods int64 }
type nodeAggregate struct {
	podCount         int
	requests, limits resourceTotals
	pods             []domainresource.NodePodView
}

func buildNodeAggregates(pods []corev1.Pod, includePods bool) map[string]nodeAggregate {
	out := make(map[string]nodeAggregate)
	for _, pod := range pods {
		if strings.TrimSpace(pod.Spec.NodeName) == "" {
			continue
		}
		aggregate := out[pod.Spec.NodeName]
		aggregate.podCount++
		requests, limits := podResourceTotals(pod)
		if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			aggregate.requests.add(requests)
			aggregate.limits.add(limits)
		}
		if includePods {
			aggregate.pods = append(aggregate.pods, buildNodePodView(pod, requests, limits))
		}
		out[pod.Spec.NodeName] = aggregate
	}
	for name, aggregate := range out {
		sort.Slice(aggregate.pods, func(i, j int) bool {
			if aggregate.pods[i].Namespace == aggregate.pods[j].Namespace {
				return aggregate.pods[i].Name < aggregate.pods[j].Name
			}
			return aggregate.pods[i].Namespace < aggregate.pods[j].Namespace
		})
		out[name] = aggregate
	}
	return out
}

func buildNodeDetail(node corev1.Node, pods []corev1.Pod) domainresource.NodeDetailView {
	nodePods := make([]corev1.Pod, 0)
	for _, pod := range pods {
		if pod.Spec.NodeName == node.Name {
			nodePods = append(nodePods, pod)
		}
	}
	aggregate := buildNodeAggregates(nodePods, true)[node.Name]
	view := buildNodeView(node, aggregate)
	return domainresource.NodeDetailView{
		Name: view.Name, Status: view.Status, Roles: view.Roles, Version: view.Version, InternalIP: view.InternalIP,
		PodCount: view.PodCount, AgeSeconds: view.AgeSeconds, Labels: cloneMap(node.Labels), Annotations: cloneMap(node.Annotations),
		Taints: mapNodeTaints(node.Spec.Taints), Conditions: mapNodeConditions(node), Resources: view.Resources, Pods: aggregate.pods,
	}
}

func buildNodeView(node corev1.Node, aggregate nodeAggregate) domainresource.NodeView {
	capacity, allocatable := resourceTotalsFromList(node.Status.Capacity), resourceTotalsFromList(node.Status.Allocatable)
	return domainresource.NodeView{
		Name: node.Name, Status: nodeStatus(node), Roles: nodeRoles(node), Version: node.Status.NodeInfo.KubeletVersion,
		InternalIP: nodeInternalIP(node), PodCount: aggregate.podCount, AgeSeconds: secondsSince(node.CreationTimestamp.Time),
		Resources: domainresource.NodeResourceSummaryView{
			Capacity: formatTotals(capacity), Allocatable: formatTotals(allocatable), Requests: formatTotals(aggregate.requests), Limits: formatTotals(aggregate.limits),
			RequestPercentages: percentages(aggregate.requests, allocatable), LimitPercentages: percentages(aggregate.limits, allocatable),
		},
	}
}

func buildNodePodView(pod corev1.Pod, requests, limits resourceTotals) domainresource.NodePodView {
	return domainresource.NodePodView{
		Name: pod.Name, Namespace: pod.Namespace, Phase: string(pod.Status.Phase), PodIP: pod.Status.PodIP,
		ReadyContainers: readyContainers(pod), Restarts: podRestartCount(pod), Labels: cloneMap(pod.Labels),
		Requests: formatTotals(requests), Limits: formatTotals(limits), AgeSeconds: secondsSince(pod.CreationTimestamp.Time),
	}
}

func podResourceTotals(pod corev1.Pod) (resourceTotals, resourceTotals) {
	var requests, limits resourceTotals
	for _, container := range pod.Spec.Containers {
		requests.add(resourceTotalsFromList(container.Resources.Requests))
		limits.add(resourceTotalsFromList(container.Resources.Limits))
	}
	var initRequests, initLimits resourceTotals
	for _, container := range pod.Spec.InitContainers {
		initRequests.max(resourceTotalsFromList(container.Resources.Requests))
		initLimits.max(resourceTotalsFromList(container.Resources.Limits))
	}
	requests.max(initRequests)
	limits.max(initLimits)
	requests.add(resourceTotalsFromList(pod.Spec.Overhead))
	limits.add(resourceTotalsFromList(pod.Spec.Overhead))
	return requests, limits
}

func resourceTotalsFromList(items corev1.ResourceList) resourceTotals {
	var totals resourceTotals
	if q, ok := items[corev1.ResourceCPU]; ok {
		totals.cpuMilli = q.MilliValue()
	}
	if q, ok := items[corev1.ResourceMemory]; ok {
		totals.memoryBytes = q.Value()
	}
	if q, ok := items[corev1.ResourceEphemeralStorage]; ok {
		totals.ephemeralBytes = q.Value()
	}
	if q, ok := items[corev1.ResourcePods]; ok {
		totals.pods = q.Value()
	}
	return totals
}
func (t *resourceTotals) add(o resourceTotals) {
	t.cpuMilli += o.cpuMilli
	t.memoryBytes += o.memoryBytes
	t.ephemeralBytes += o.ephemeralBytes
	t.pods += o.pods
}
func (t *resourceTotals) max(o resourceTotals) {
	if o.cpuMilli > t.cpuMilli {
		t.cpuMilli = o.cpuMilli
	}
	if o.memoryBytes > t.memoryBytes {
		t.memoryBytes = o.memoryBytes
	}
	if o.ephemeralBytes > t.ephemeralBytes {
		t.ephemeralBytes = o.ephemeralBytes
	}
	if o.pods > t.pods {
		t.pods = o.pods
	}
}

func formatTotals(t resourceTotals) domainresource.ResourceQuantityView {
	view := domainresource.ResourceQuantityView{}
	if t.cpuMilli > 0 {
		view.CPU = apiresource.NewMilliQuantity(t.cpuMilli, apiresource.DecimalSI).String()
	}
	if t.memoryBytes > 0 {
		view.Memory = apiresource.NewQuantity(t.memoryBytes, apiresource.BinarySI).String()
	}
	if t.ephemeralBytes > 0 {
		view.EphemeralStorage = apiresource.NewQuantity(t.ephemeralBytes, apiresource.BinarySI).String()
	}
	if t.pods > 0 {
		view.Pods = fmt.Sprintf("%d", t.pods)
	}
	return view
}
func percentages(current, total resourceTotals) domainresource.ResourcePercentageView {
	return domainresource.ResourcePercentageView{CPU: percentage(current.cpuMilli, total.cpuMilli), Memory: percentage(current.memoryBytes, total.memoryBytes), EphemeralStorage: percentage(current.ephemeralBytes, total.ephemeralBytes), Pods: percentage(current.pods, total.pods)}
}
func percentage(current, total int64) float64 {
	if current <= 0 || total <= 0 {
		return 0
	}
	return math.Round((float64(current)/float64(total))*1000) / 10
}

func mapNamespace(item corev1.Namespace) domainresource.NamespaceView {
	return domainresource.NamespaceView{Name: item.Name, Status: string(item.Status.Phase), Labels: item.Labels, Annotations: item.Annotations, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}
func sanitizeStringMap(input map[string]string) map[string]string {
	out := make(map[string]string)
	for k, v := range input {
		if strings.TrimSpace(k) != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}
func nodeRoles(item corev1.Node) []string {
	roles := []string{}
	for key := range item.Labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(key, "node-role.kubernetes.io/"))
		}
	}
	sort.Strings(roles)
	return roles
}
func nodeInternalIP(item corev1.Node) string {
	for _, a := range item.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			return a.Address
		}
	}
	return ""
}
func nodeStatus(item corev1.Node) string {
	for _, c := range item.Status.Conditions {
		if c.Type == corev1.NodeReady {
			if c.Status == corev1.ConditionTrue {
				return "ready"
			}
			return "not_ready"
		}
	}
	return "unknown"
}
func mapNodeTaints(items []corev1.Taint) []domainresource.NodeTaintView {
	out := make([]domainresource.NodeTaintView, 0, len(items))
	for _, i := range items {
		out = append(out, domainresource.NodeTaintView{Key: i.Key, Value: i.Value, Effect: string(i.Effect)})
	}
	return out
}
func mapNodeConditions(item corev1.Node) []domainresource.WorkloadConditionView {
	out := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, c := range item.Status.Conditions {
		at := ""
		if !c.LastTransitionTime.IsZero() {
			at = c.LastTransitionTime.UTC().Format(time.RFC3339)
		}
		out = append(out, domainresource.WorkloadConditionView{Type: string(c.Type), Status: string(c.Status), Reason: c.Reason, Message: c.Message, LastTransitionTime: at})
	}
	return out
}
func cloneMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
func readyContainers(pod corev1.Pod) string {
	ready := 0
	for _, s := range pod.Status.ContainerStatuses {
		if s.Ready {
			ready++
		}
	}
	return fmt.Sprintf("%d/%d", ready, len(pod.Status.ContainerStatuses))
}
func podRestartCount(pod corev1.Pod) int32 {
	var total int32
	for _, s := range pod.Status.ContainerStatuses {
		total += s.RestartCount
	}
	return total
}

var _ appresource.DirectInventory = (*Direct)(nil)
