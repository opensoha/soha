package resourcebackend

import (
	"fmt"
	"sort"
	"strings"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type resourceGraphSnapshot struct {
	deployments []appsv1.Deployment
	replicaSets []appsv1.ReplicaSet
	pods        []corev1.Pod
	services    []corev1.Service
	ingresses   []networkingv1.Ingress
	hpas        []autoscalingv2.HorizontalPodAutoscaler
	pdbs        []policyv1.PodDisruptionBudget
	events      []corev1.Event
	warnings    []string
}

type resourceGraphBuilder struct {
	clusterID string
	namespace string
	nodes     map[string]domainresource.ResourceGraphNode
	edges     map[string]domainresource.ResourceGraphEdge
	evidence  []domainresource.ResourceEvidence
}

func buildResourceGraph(clusterID, namespace, rootKind, rootName string, snapshot resourceGraphSnapshot) (domainresource.ResourceGraph, error) {
	builder := &resourceGraphBuilder{
		clusterID: clusterID, namespace: namespace,
		nodes: map[string]domainresource.ResourceGraphNode{}, edges: map[string]domainresource.ResourceGraphEdge{},
	}
	builder.addSnapshot(snapshot)
	rootID := builder.findNode(rootKind, namespace, rootName)
	if rootID == "" {
		return domainresource.ResourceGraph{}, fmt.Errorf("%w: %s %s was not found in the resource graph", apperrors.ErrNotFound, rootKind, rootName)
	}
	graph := builder.connectedGraph(rootID)
	graph.Warnings = append(graph.Warnings, snapshot.warnings...)
	return graph, nil
}

func (b *resourceGraphBuilder) addSnapshot(snapshot resourceGraphSnapshot) {
	deploymentIDs := b.addDeployments(snapshot.deployments)
	replicaSetIDs := b.addReplicaSets(snapshot.replicaSets, deploymentIDs)
	podIDs := b.addPods(snapshot.pods, replicaSetIDs)
	serviceIDs := b.addServices(snapshot.services, snapshot.pods, podIDs)
	b.addIngresses(snapshot.ingresses, serviceIDs)
	b.addHorizontalPodAutoscalers(snapshot.hpas)
	b.addPodDisruptionBudgets(snapshot.pdbs, snapshot.pods, podIDs)
	b.addEvents(snapshot.events)
}

func (b *resourceGraphBuilder) addDeployments(deployments []appsv1.Deployment) map[string]string {
	deploymentIDs := map[string]string{}
	for _, item := range deployments {
		id := b.addNode("apps/v1", "Deployment", item.Namespace, item.Name, deploymentStatus(item))
		deploymentIDs[string(item.UID)] = id
		deploymentIDs[item.Namespace+"/"+item.Name] = id
		b.addPodTemplateRefs(id, item.Namespace, item.Spec.Template)
	}
	return deploymentIDs
}

func (b *resourceGraphBuilder) addReplicaSets(replicaSets []appsv1.ReplicaSet, deploymentIDs map[string]string) map[string]string {
	replicaSetIDs := map[string]string{}
	for _, item := range replicaSets {
		id := b.addNode("apps/v1", "ReplicaSet", item.Namespace, item.Name, replicaSetStatus(item))
		replicaSetIDs[string(item.UID)] = id
		for _, owner := range item.OwnerReferences {
			if owner.Kind != "Deployment" {
				continue
			}
			ownerID := deploymentIDs[string(owner.UID)]
			if ownerID == "" {
				ownerID = deploymentIDs[item.Namespace+"/"+owner.Name]
			}
			b.addEdge(ownerID, id, "owns")
		}
	}
	return replicaSetIDs
}

func (b *resourceGraphBuilder) addPods(pods []corev1.Pod, replicaSetIDs map[string]string) map[string]string {
	podIDs := map[string]string{}
	for _, item := range pods {
		id := b.addNode("v1", "Pod", item.Namespace, item.Name, string(item.Status.Phase))
		podIDs[item.Namespace+"/"+item.Name] = id
		b.addPodSpecRefs(id, item.Namespace, item.Spec)
		for _, owner := range item.OwnerReferences {
			if owner.Kind != "ReplicaSet" {
				continue
			}
			ownerID := replicaSetIDs[string(owner.UID)]
			if ownerID == "" {
				ownerID = b.findNode("ReplicaSet", item.Namespace, owner.Name)
			}
			b.addEdge(ownerID, id, "owns")
		}
	}
	return podIDs
}

func (b *resourceGraphBuilder) addServices(services []corev1.Service, pods []corev1.Pod, podIDs map[string]string) map[string]string {
	serviceIDs := map[string]string{}
	for _, item := range services {
		id := b.addNode("v1", "Service", item.Namespace, item.Name, string(item.Spec.Type))
		serviceIDs[item.Namespace+"/"+item.Name] = id
		for _, pod := range pods {
			if pod.Namespace == item.Namespace && selectorMatchesLabels(item.Spec.Selector, pod.Labels) {
				b.addEdge(id, podIDs[pod.Namespace+"/"+pod.Name], "selects")
			}
		}
	}
	return serviceIDs
}

func (b *resourceGraphBuilder) addIngresses(ingresses []networkingv1.Ingress, serviceIDs map[string]string) {
	for _, item := range ingresses {
		id := b.addNode("networking.k8s.io/v1", "Ingress", item.Namespace, item.Name, ingressStatus(item))
		for _, serviceName := range extractIngressBackendServices(item) {
			b.addEdge(id, serviceIDs[item.Namespace+"/"+serviceName], "routes-to")
		}
	}
}

func (b *resourceGraphBuilder) addHorizontalPodAutoscalers(hpas []autoscalingv2.HorizontalPodAutoscaler) {
	for _, item := range hpas {
		id := b.addNode("autoscaling/v2", "HorizontalPodAutoscaler", item.Namespace, item.Name, "")
		targetID := b.findNode(item.Spec.ScaleTargetRef.Kind, item.Namespace, item.Spec.ScaleTargetRef.Name)
		b.addEdge(id, targetID, "scales")
	}
}

func (b *resourceGraphBuilder) addPodDisruptionBudgets(pdbs []policyv1.PodDisruptionBudget, pods []corev1.Pod, podIDs map[string]string) {
	for _, item := range pdbs {
		id := b.addNode("policy/v1", "PodDisruptionBudget", item.Namespace, item.Name, "")
		selector, err := metav1.LabelSelectorAsSelector(item.Spec.Selector)
		if err != nil {
			continue
		}
		for _, pod := range pods {
			if pod.Namespace == item.Namespace && selector.Matches(labels.Set(pod.Labels)) {
				b.addEdge(id, podIDs[pod.Namespace+"/"+pod.Name], "protects")
			}
		}
	}
}

func (b *resourceGraphBuilder) addPodTemplateRefs(rootID, namespace string, template corev1.PodTemplateSpec) {
	b.addPodSpecRefs(rootID, namespace, template.Spec)
}

func (b *resourceGraphBuilder) addPodSpecRefs(rootID, namespace string, spec corev1.PodSpec) {
	refs := buildPodSourceRefs(corev1.Pod{Spec: spec})
	for name := range refs.configMaps {
		if name == "kube-root-ca.crt" {
			continue
		}
		b.addEdge(rootID, b.addNode("v1", "ConfigMap", namespace, name, ""), "uses-config")
	}
	for name := range refs.secrets {
		b.addEdge(rootID, b.addNode("v1", "Secret", namespace, name, ""), "uses-secret")
	}
	for name := range refs.pvcs {
		b.addEdge(rootID, b.addNode("v1", "PersistentVolumeClaim", namespace, name, ""), "mounts")
	}
}

func (b *resourceGraphBuilder) addNode(apiVersion, kind, namespace, name, status string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	id := resourceGraphNodeID(apiVersion, kind, namespace, name)
	scopeMode := domainresource.ResourceScopeModeNamespace
	if namespace == "" {
		scopeMode = domainresource.ResourceScopeModeCluster
	}
	b.nodes[id] = domainresource.ResourceGraphNode{ID: id, Resource: domainresource.ResourceRef{
		ClusterID: b.clusterID, APIVersion: apiVersion, Kind: kind, Name: name, Namespace: namespace, ScopeMode: scopeMode,
	}, Status: status}
	return id
}

func (b *resourceGraphBuilder) addEdge(sourceID, targetID, relation string) {
	if sourceID == "" || targetID == "" || sourceID == targetID {
		return
	}
	id := relation + ":" + sourceID + ":" + targetID
	b.edges[id] = domainresource.ResourceGraphEdge{ID: id, SourceID: sourceID, TargetID: targetID, Relation: relation}
}

func (b *resourceGraphBuilder) addEvents(events []corev1.Event) {
	for _, item := range events {
		resourceID := b.findNode(item.InvolvedObject.Kind, item.InvolvedObject.Namespace, item.InvolvedObject.Name)
		if resourceID == "" {
			continue
		}
		observedAt := item.LastTimestamp.Time
		if observedAt.IsZero() {
			observedAt = item.EventTime.Time
		}
		if observedAt.IsZero() {
			observedAt = item.CreationTimestamp.Time
		}
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		severity := "info"
		if strings.EqualFold(item.Type, "Warning") {
			severity = "warning"
		}
		b.evidence = append(b.evidence, domainresource.ResourceEvidence{
			ID: "event/" + item.Namespace + "/" + item.Name, Type: "kubernetes-event", Severity: severity,
			Summary: strings.TrimSpace(strings.TrimSpace(item.Reason+" ") + item.Message), ObservedAt: observedAt.UTC().Format(time.RFC3339Nano),
			ResourceID: resourceID, SourceRef: "Event/" + item.Namespace + "/" + item.Name,
		})
	}
}

func (b *resourceGraphBuilder) findNode(kind, namespace, name string) string {
	for id, node := range b.nodes {
		if strings.EqualFold(node.Resource.Kind, kind) && node.Resource.Namespace == namespace && node.Resource.Name == name {
			return id
		}
	}
	return ""
}

func (b *resourceGraphBuilder) connectedGraph(rootID string) domainresource.ResourceGraph {
	connected := map[string]struct{}{rootID: {}}
	for changed := true; changed; {
		changed = false
		for _, edge := range b.edges {
			_, source := connected[edge.SourceID]
			_, target := connected[edge.TargetID]
			if source && !target {
				connected[edge.TargetID] = struct{}{}
				changed = true
			}
			if target && !source {
				connected[edge.SourceID] = struct{}{}
				changed = true
			}
		}
	}
	graph := domainresource.ResourceGraph{
		ClusterID: b.clusterID, Namespace: b.namespace, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), RootID: rootID,
		Nodes: []domainresource.ResourceGraphNode{}, Edges: []domainresource.ResourceGraphEdge{}, Evidence: []domainresource.ResourceEvidence{}, Warnings: []string{},
	}
	for id := range connected {
		graph.Nodes = append(graph.Nodes, b.nodes[id])
	}
	for _, edge := range b.edges {
		if _, ok := connected[edge.SourceID]; !ok {
			continue
		}
		if _, ok := connected[edge.TargetID]; ok {
			graph.Edges = append(graph.Edges, edge)
		}
	}
	for _, evidence := range b.evidence {
		if _, ok := connected[evidence.ResourceID]; ok {
			graph.Evidence = append(graph.Evidence, evidence)
		}
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool { return graph.Edges[i].ID < graph.Edges[j].ID })
	sort.Slice(graph.Evidence, func(i, j int) bool { return graph.Evidence[i].ObservedAt > graph.Evidence[j].ObservedAt })
	return graph
}

func resourceGraphNodeID(apiVersion, kind, namespace, name string) string {
	return apiVersion + ":" + kind + ":" + namespace + "/" + name
}

func deploymentStatus(item appsv1.Deployment) string {
	if item.Status.AvailableReplicas >= item.Status.Replicas && item.Status.Replicas > 0 {
		return "healthy"
	}
	return "progressing"
}

func replicaSetStatus(item appsv1.ReplicaSet) string {
	if item.Status.ReadyReplicas >= item.Status.Replicas && item.Status.Replicas > 0 {
		return "healthy"
	}
	return "progressing"
}

func ingressStatus(item networkingv1.Ingress) string {
	if len(item.Status.LoadBalancer.Ingress) > 0 {
		return "ready"
	}
	return "pending"
}
