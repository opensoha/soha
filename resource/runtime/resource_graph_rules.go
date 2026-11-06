package runtime

import (
	"context"
	"errors"
	"fmt"
	cr "github.com/opensoha/soha-contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sort"
	"strings"
	"time"
)

// ErrInvalidResourceGraphRoot identifies invalid scope or stale root identity.
var ErrInvalidResourceGraphRoot = errors.New("invalid resource graph root")

// ErrUnsupportedResourceGraphRoot identifies roots requiring another collector.
var ErrUnsupportedResourceGraphRoot = errors.New("unsupported resource graph root")

func invalidGraphRoot(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidResourceGraphRoot, message)
}

var graphBuiltinAPIs = map[string]string{
	"Pod": "v1", "ReplicationController": "v1", "Service": "v1", "Secret": "v1", "ConfigMap": "v1", "PersistentVolumeClaim": "v1", "PersistentVolume": "v1", "ServiceAccount": "v1", "Node": "v1", "Namespace": "v1", "Event": "v1",
	"Deployment": "apps/v1", "StatefulSet": "apps/v1", "DaemonSet": "apps/v1", "ReplicaSet": "apps/v1", "Job": "batch/v1", "CronJob": "batch/v1",
	"Ingress": "networking.k8s.io/v1", "HorizontalPodAutoscaler": "autoscaling/v2", "PodDisruptionBudget": "policy/v1",
	"ResourceQuota": "v1", "LimitRange": "v1", "PriorityClass": "scheduling.k8s.io/v1", "RuntimeClass": "node.k8s.io/v1", "Lease": "coordination.k8s.io/v1",
	"MutatingWebhookConfiguration": "admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration": "admissionregistration.k8s.io/v1",
	"StorageClass": "storage.k8s.io/v1",
	"Role":         graphRBACAPI, "RoleBinding": graphRBACAPI, "ClusterRole": graphRBACAPI, "ClusterRoleBinding": graphRBACAPI,
}

// NormalizeResourceGraphRoot canonicalizes known wire kinds and trims user input.
// It never supplies a default namespace or relaxes namespaced scope validation.
func NormalizeResourceGraphRoot(root cr.ResourceRef) (cr.ResourceRef, error) {
	root.ClusterID = strings.TrimSpace(root.ClusterID)
	root.APIVersion = strings.TrimSpace(root.APIVersion)
	root.Kind = strings.TrimSpace(root.Kind)
	root.Name = strings.TrimSpace(root.Name)
	root.Namespace = strings.TrimSpace(root.Namespace)
	for _, definition := range cr.NetworkTopologySources() {
		if networkGraphRootKind(definition.Kind) && strings.EqualFold(root.Kind, definition.Kind) {
			root.Kind = definition.Kind
			break
		}
	}
	for kind := range graphBuiltinAPIs {
		if strings.EqualFold(root.Kind, kind) {
			root.Kind = kind
			break
		}
	}
	return root, validateGraphRoot(root)
}
func validateGraphRoot(root cr.ResourceRef) error {
	if root.ClusterID == "" || root.Kind == "" || root.Name == "" {
		return invalidGraphRoot("resource graph root requires cluster, kind and name")
	}
	if definition, ok := networkGraphRootSource(root); ok {
		return validateNetworkGraphRootScope(root, definition)
	}
	builtinAPI, kindKnown := graphBuiltinAPIs[root.Kind]
	known := kindKnown && (root.APIVersion == "" || root.APIVersion == builtinAPI)
	if known && graphStorageRootKind(root.Kind) && root.ScopeMode != "" && root.ScopeMode != topologyScope(root.Kind == "PersistentVolumeClaim") {
		return invalidGraphRoot("storage resource graph root scope mismatch")
	}
	if known && graphRBACRootKind(root.Kind) && root.ScopeMode != "" && root.ScopeMode != topologyScope(!graphRBACClusterKind(root.Kind)) {
		return invalidGraphRoot("RBAC resource graph root scope mismatch")
	}
	coreAPI := root.APIVersion == "" || root.APIVersion == "v1"
	clusterKind := coreAPI && (root.Kind == "Node" || root.Kind == "Namespace") || known && (graphClusterConfigurationKind(root.Kind) || root.Kind == "PersistentVolume" || root.Kind == "StorageClass" || graphRBACClusterKind(root.Kind))
	if root.Namespace == "" && !clusterKind && (known || root.ScopeMode != cr.ResourceScopeModeCluster) {
		return invalidGraphRoot("namespaced resource graph root requires namespace")
	}
	if clusterKind && root.Namespace != "" {
		return invalidGraphRoot("cluster resource graph root cannot have namespace")
	}
	return nil
}

const ResourceGraphWorkingNodeLimit = 10000
const ResourceGraphWorkingEdgeLimit = 10000

var ErrResourceGraphBudget = errors.New("resource graph working set budget exceeded")

type graphRules struct {
	ctx          context.Context
	err          error
	partial      bool
	pods         []*unstructured.Unstructured
	clusterRoles []*unstructured.Unstructured

	cluster string
	objects map[string]*unstructured.Unstructured
	nodes   map[string]cr.ResourceGraphNode
	edges   map[string]cr.ResourceGraphEdge
}

func graphIdentity(api, kind, namespace, name string) string {
	return api + ":" + kind + ":" + namespace + "/" + name
}

var graphIDSegmentEscaper = strings.NewReplacer("%", "%25", ":", "%3A")

func graphNodeID(ref cr.ResourceRef) string {
	id := graphIdentity(ref.APIVersion, ref.Kind, ref.Namespace, graphIDSegmentEscaper.Replace(ref.Name))
	if ref.UID != "" {
		id += ":" + graphIDSegmentEscaper.Replace(ref.UID)
	}
	return id
}
func (b *graphRules) ref(o *unstructured.Unstructured) cr.ResourceRef {
	scope := cr.ResourceScopeModeNamespace
	if o.GetNamespace() == "" {
		scope = cr.ResourceScopeModeCluster
	}
	return cr.ResourceRef{ClusterID: b.cluster, APIVersion: o.GetAPIVersion(), Kind: o.GetKind(), Namespace: o.GetNamespace(), Name: o.GetName(), UID: string(o.GetUID()), ResourceVersion: o.GetResourceVersion(), ScopeMode: scope}
}
func (b *graphRules) active() bool {
	if b.err != nil {
		return false
	}
	if err := b.ctx.Err(); err != nil {
		b.err = err
		return false
	}
	return !b.partial
}
func (b *graphRules) node(ref cr.ResourceRef, status string) string {
	return b.addNode(graphNodeID(ref), ref, status)
}
func (b *graphRules) addNode(id string, ref cr.ResourceRef, status string) string {
	if !b.active() {
		return ""
	}
	if _, ok := b.nodes[id]; !ok {
		if len(b.nodes) >= ResourceGraphWorkingNodeLimit {
			b.partial = true
			return ""
		}
		b.nodes[id] = cr.ResourceGraphNode{ID: id, Resource: ref, Status: status}
	}
	return id
}
func (b *graphRules) target(api, kind, namespace, name, uid string) string {
	if name == "" {
		return ""
	}
	ref := cr.ResourceRef{ClusterID: b.cluster, APIVersion: api, Kind: kind, Namespace: namespace, Name: name, UID: uid, ScopeMode: cr.ResourceScopeModeNamespace}
	if namespace == "" {
		ref.ScopeMode = cr.ResourceScopeModeCluster
	}
	if o := b.objects[graphIdentity(api, kind, namespace, name)]; o != nil && (uid == "" || string(o.GetUID()) == uid) {
		return b.node(b.ref(o), graphStatus(o))
	}
	return b.node(ref, "unresolved")
}
func (b *graphRules) edge(source, target, relation string) {
	b.addEdge(cr.ResourceGraphEdge{SourceID: source, TargetID: target, Relation: relation})
}
func (b *graphRules) addEdge(edge cr.ResourceGraphEdge) {
	if !b.active() {
		return
	}
	if edge.SourceID == "" || edge.TargetID == "" || edge.SourceID == edge.TargetID {
		return
	}
	edge.ID = edge.Relation + ":" + edge.SourceID + ":" + edge.TargetID
	if _, exists := b.edges[edge.ID]; !exists && len(b.edges) >= ResourceGraphWorkingEdgeLimit {
		b.partial = true
		return
	}
	b.edges[edge.ID] = edge
}
func graphIsEvent(o *unstructured.Unstructured) bool {
	return o.GetAPIVersion() == "v1" && o.GetKind() == "Event"
}
func graphStatus(o *unstructured.Unstructured) string {
	if !graphBuiltinSpec(o) {
		return ""
	}
	switch o.GetKind() {
	case "Deployment":
		return graphReplicaStatus(o, "availableReplicas")
	case "ReplicaSet":
		return graphReplicaStatus(o, "readyReplicas")
	case "Ingress":
		ingress, _, _ := graphNestedSlice(o.Object, "status", "loadBalancer", "ingress")
		if len(ingress) > 0 {
			return "ready"
		}
		return "pending"
	case "Service":
		value, _, _ := unstructured.NestedString(o.Object, "spec", "type")
		return value
	default:
		phase, _, _ := unstructured.NestedString(o.Object, "status", "phase")
		return phase
	}
}
func graphReplicaStatus(o *unstructured.Unstructured, readyField string) string {
	replicas, found, _ := unstructured.NestedInt64(o.Object, "status", "replicas")
	ready, readyFound, _ := unstructured.NestedInt64(o.Object, "status", readyField)
	if found && readyFound && replicas > 0 && ready >= replicas {
		return "healthy"
	}
	return "progressing"
}

// BuildResourceGraphFromObjects applies the same pure rules to typed projections
// or arbitrary Kubernetes objects. Unknown kinds contribute only metadata
// ownerReferences; their spec is never inferred. Callers own discovery and scope.
func BuildResourceGraphFromObjects(root cr.ResourceRef, objects []*unstructured.Unstructured) (cr.ResourceGraph, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ResourceGraphTimeout)
	defer cancel()
	return buildResourceGraphWithContext(ctx, root, objects)
}
func buildResourceGraphWithContext(ctx context.Context, root cr.ResourceRef, objects []*unstructured.Unstructured) (cr.ResourceGraph, error) {
	if err := ctx.Err(); err != nil {
		return cr.ResourceGraph{}, err
	}
	if len(objects) > ResourceGraphWorkingNodeLimit {
		return cr.ResourceGraph{}, ErrResourceGraphBudget
	}
	root, err := NormalizeResourceGraphRoot(root)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	index, err := graphObjectIndex(ctx, root.Namespace, objects)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	b := &graphRules{ctx: ctx, cluster: root.ClusterID, objects: index, nodes: map[string]cr.ResourceGraphNode{}, edges: map[string]cr.ResourceGraphEdge{}}
	rootObject, err := b.rootObject(root)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	rootID := b.node(b.ref(rootObject), graphStatus(rootObject))
	keys := b.objectKeys(rootObject)
	storage := graphBuiltinSpec(rootObject) && graphStorageRootKind(root.Kind)
	rbac := graphBuiltinSpec(rootObject) && graphRBACRootKind(root.Kind)
	if err := indexGraphCrossNamespaceAccounts(ctx, rootObject, objects, index); err != nil {
		return cr.ResourceGraph{}, err
	}
	if !storage {
		b.priorityOwners(rootObject)
		for _, key := range keys {
			if !b.active() {
				break
			}
			o := b.objects[key]
			if graphIsEvent(o) {
				continue
			}
			id := b.node(b.ref(o), graphStatus(o))
			b.ownerRelations(o, id)
		}
	}
	for _, key := range keys {
		if !b.active() {
			break
		}
		o := b.objects[key]
		if graphIsEvent(o) {
			continue
		}
		id := b.node(b.ref(o), graphStatus(o))
		if storage {
			b.storageRelations(o, id)
			continue
		}
		if rbac {
			b.rbacRelations(o, id)
			continue
		}
		b.podReferences(o, id)
		b.selectorRelations(o, id)
		b.configurationRelations(o, id)
	}
	if b.err != nil {
		return cr.ResourceGraph{}, b.err
	}
	graph, err := b.bounded(rootID, root.Namespace)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	if b.partial {
		graph.Warnings = append(graph.Warnings, "resource graph working set truncated at 10000 nodes or 10000 edges")
	}
	if rbac && root.Kind == "ServiceAccount" {
		graph.Warnings = append(graph.Warnings, "RoleBinding source limited to the ServiceAccount namespace")
	}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return cr.ResourceGraph{}, err
		}
		o := b.objects[key]
		if graphIsEvent(o) {
			b.event(&graph, o)
		}
	}
	sort.SliceStable(graph.Evidence, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, graph.Evidence[i].ObservedAt)
		right, _ := time.Parse(time.RFC3339Nano, graph.Evidence[j].ObservedAt)
		return left.After(right)
	})
	return graph, ctx.Err()
}
func (b *graphRules) objectKeys(root *unstructured.Unstructured) []string {
	rootKey := graphIdentity(root.GetAPIVersion(), root.GetKind(), root.GetNamespace(), root.GetName())
	keys := make([]string, 0, len(b.objects))
	for key := range b.objects {
		if key != rootKey {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	// Pod selector iteration is sorted separately from the root-first rule order.
	for _, key := range keys {
		o := b.objects[key]
		if o.GetKind() == "Pod" && o.GetAPIVersion() == "v1" {
			b.pods = append(b.pods, o)
		}
		if o.GetKind() == "ClusterRole" && o.GetAPIVersion() == graphRBACAPI && o.GetNamespace() == "" {
			b.clusterRoles = append(b.clusterRoles, o)
		}
	}
	if root.GetKind() == "Pod" && root.GetAPIVersion() == "v1" {
		b.pods = append(b.pods, root)
	}
	if root.GetKind() == "ClusterRole" && root.GetAPIVersion() == graphRBACAPI && root.GetNamespace() == "" {
		b.clusterRoles = append(b.clusterRoles, root)
	}
	return append([]string{rootKey}, keys...)
}
func (b *graphRules) priorityOwners(root *unstructured.Unstructured) {
	queue := []*unstructured.Unstructured{root}
	seen := map[string]bool{}
	for len(queue) > 0 {
		if !b.active() {
			return
		}
		current := queue[0]
		queue = queue[1:]
		id := graphNodeID(b.ref(current))
		if seen[id] {
			continue
		}
		seen[id] = true
		b.ownerRelations(current, b.node(b.ref(current), graphStatus(current)))
		for _, owner := range current.GetOwnerReferences() {
			if !b.active() {
				return
			}
			if actual := b.observedOwner(current.GetNamespace(), owner); actual != nil {
				queue = append(queue, actual)
			}
		}
	}
}
func graphObjectIndex(ctx context.Context, namespace string, objects []*unstructured.Unstructured) (map[string]*unstructured.Unstructured, error) {
	index := map[string]*unstructured.Unstructured{}
	for _, o := range objects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if o == nil || o.GetName() == "" || o.GetKind() == "" || o.GetAPIVersion() == "" {
			continue
		}
		if namespace != "" && o.GetNamespace() != "" && o.GetNamespace() != namespace {
			continue
		}
		key := graphIdentity(o.GetAPIVersion(), o.GetKind(), o.GetNamespace(), o.GetName())
		if _, exists := index[key]; !exists {
			index[key] = o
		}
	}
	return index, nil
}
func (b *graphRules) rootObject(root cr.ResourceRef) (*unstructured.Unstructured, error) {
	var result *unstructured.Unstructured
	for _, o := range b.objects {
		if err := b.ctx.Err(); err != nil {
			return nil, err
		}
		if o.GetKind() != root.Kind || o.GetName() != root.Name || o.GetNamespace() != root.Namespace {
			continue
		}
		if root.APIVersion != "" && o.GetAPIVersion() != root.APIVersion {
			continue
		}
		if result != nil {
			return nil, invalidGraphRoot("ambiguous root apiVersion")
		}
		result = o
	}
	if result == nil {
		return nil, invalidGraphRoot("resource graph root not present")
	}
	if root.UID != "" && string(result.GetUID()) != root.UID {
		return nil, invalidGraphRoot("resource graph root UID changed")
	}
	return result, nil
}
func (b *graphRules) ownerRelations(o *unstructured.Unstructured, id string) {
	for _, owner := range o.GetOwnerReferences() {
		if !b.active() {
			return
		}
		relation := "owns"
		if owner.Controller != nil && *owner.Controller {
			relation = "controls"
		}
		b.edge(b.ownerTarget(o.GetNamespace(), owner), id, relation)
	}
}

// Kubernetes permits a cluster-scoped owner of a namespaced dependent. Only
// observed exact identities confirm this; another namespace is never searched.
func (b *graphRules) observedOwner(namespace string, owner metav1.OwnerReference) *unstructured.Unstructured {
	if owner.UID == "" {
		return nil
	}
	for _, scope := range []string{namespace, ""} {
		candidate := b.objects[graphIdentity(owner.APIVersion, owner.Kind, scope, owner.Name)]
		if candidate != nil && candidate.GetUID() == owner.UID {
			return candidate
		}
	}
	return nil
}
func (b *graphRules) ownerTarget(namespace string, owner metav1.OwnerReference) string {
	if actual := b.observedOwner(namespace, owner); actual != nil {
		return b.node(b.ref(actual), graphStatus(actual))
	}
	ref := cr.ResourceRef{ClusterID: b.cluster, APIVersion: owner.APIVersion, Kind: owner.Kind, Namespace: namespace, Name: owner.Name, UID: string(owner.UID), ScopeMode: cr.ResourceScopeModeNamespace}
	// The wire requires a scope enum. Use the dependent lookup context for this
	// unresolved candidate; it does not confirm the target or its actual scope.
	if namespace == "" {
		ref.ScopeMode = cr.ResourceScopeModeCluster
	}
	return b.node(ref, "unresolved")
}

// A custom object may reuse a built-in kind name; spec rules require exact GVK.
func graphBuiltinSpec(o *unstructured.Unstructured) bool {
	return graphBuiltinAPIs[o.GetKind()] != "" && graphBuiltinAPIs[o.GetKind()] == o.GetAPIVersion()
}
func graphNestedMap(object map[string]interface{}, fields ...string) (map[string]interface{}, bool, error) {
	value, found, err := unstructured.NestedFieldNoCopy(object, fields...)
	result, _ := value.(map[string]interface{})
	return result, found, err
}
func graphNestedSlice(object map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	value, found, err := unstructured.NestedFieldNoCopy(object, fields...)
	result, _ := value.([]interface{})
	return result, found, err
}
func graphMap(m map[string]interface{}, key string) map[string]interface{} {
	value, _ := m[key].(map[string]interface{})
	return value
}
func graphString(m map[string]interface{}, key string) string {
	value, _ := m[key].(string)
	return value
}
func graphSlice(m map[string]interface{}, key string) []interface{} {
	value, _ := m[key].([]interface{})
	return value
}
func graphPodSpec(o *unstructured.Unstructured) map[string]interface{} {
	if !graphBuiltinSpec(o) {
		return nil
	}
	path := []string{"spec", "template", "spec"}
	switch o.GetKind() {
	case "Pod":
		path = []string{"spec"}
	case "CronJob":
		path = []string{"spec", "jobTemplate", "spec", "template", "spec"}
	case "Deployment", "StatefulSet", "DaemonSet", "Job", "ReplicaSet", "ReplicationController":
	default:
		return nil
	}
	spec, _, _ := graphNestedMap(o.Object, path...)
	return spec
}
func (b *graphRules) podReferences(o *unstructured.Unstructured, id string) {
	spec := graphPodSpec(o)
	if spec == nil {
		return
	}
	ref := func(kind, name, relation string) {
		b.edge(id, b.target("v1", kind, o.GetNamespace(), name, ""), relation)
	}
	for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
		for _, raw := range graphSlice(spec, field) {
			if !b.active() {
				return
			}
			container, _ := raw.(map[string]interface{})
			graphContainerRefs(container, ref, b.active)
		}
	}
	for _, raw := range graphSlice(spec, "volumes") {
		if !b.active() {
			return
		}
		volume, _ := raw.(map[string]interface{})
		graphVolumeRefs(volume, ref, b.active)
	}
	ref("ServiceAccount", graphString(spec, "serviceAccountName"), ResourceGraphRelationUsesServiceAccount)
	for _, raw := range graphSlice(spec, "imagePullSecrets") {
		if !b.active() {
			return
		}
		entry, _ := raw.(map[string]interface{})
		ref("Secret", graphString(entry, "name"), "uses-secret")
	}
	if node := graphString(spec, "nodeName"); node != "" {
		b.edge(id, b.target("v1", "Node", "", node, ""), "scheduled-on")
	}
	b.edge(id, b.target("scheduling.k8s.io/v1", "PriorityClass", "", graphString(spec, "priorityClassName"), ""), ResourceGraphRelationUsesPriorityClass)
	b.edge(id, b.target("node.k8s.io/v1", "RuntimeClass", "", graphString(spec, "runtimeClassName"), ""), ResourceGraphRelationUsesRuntimeClass)
}
func graphContainerRefs(container map[string]interface{}, ref func(string, string, string), active func() bool) {
	for _, raw := range graphSlice(container, "env") {
		if !active() {
			return
		}
		entry, _ := raw.(map[string]interface{})
		value := graphMap(entry, "valueFrom")
		ref("ConfigMap", graphString(graphMap(value, "configMapKeyRef"), "name"), "uses-config")
		ref("Secret", graphString(graphMap(value, "secretKeyRef"), "name"), "uses-secret")
	}
	for _, raw := range graphSlice(container, "envFrom") {
		if !active() {
			return
		}
		entry, _ := raw.(map[string]interface{})
		ref("ConfigMap", graphString(graphMap(entry, "configMapRef"), "name"), "uses-config")
		ref("Secret", graphString(graphMap(entry, "secretRef"), "name"), "uses-secret")
	}
}
func graphVolumeRefs(volume map[string]interface{}, ref func(string, string, string), active func() bool) {
	ref("ConfigMap", graphString(graphMap(volume, "configMap"), "name"), "uses-config")
	ref("Secret", graphString(graphMap(volume, "secret"), "secretName"), "uses-secret")
	ref("Secret", graphString(graphMap(graphMap(volume, "csi"), "nodePublishSecretRef"), "name"), "uses-secret")
	ref("PersistentVolumeClaim", graphVolumeClaimName(volume), ResourceGraphRelationMounts)
	for _, raw := range graphSlice(graphMap(volume, "projected"), "sources") {
		if !active() {
			return
		}
		source, _ := raw.(map[string]interface{})
		ref("ConfigMap", graphString(graphMap(source, "configMap"), "name"), "uses-config")
		ref("Secret", graphString(graphMap(source, "secret"), "name"), "uses-secret")
	}
}

func (b *graphRules) selectorRelations(o *unstructured.Unstructured, id string) {
	if !graphBuiltinSpec(o) {
		return
	}
	switch o.GetKind() {
	case "Service", "PodDisruptionBudget":
		b.podSelectorRelations(o, id)
	case "HorizontalPodAutoscaler":
		target, _, _ := graphNestedMap(o.Object, "spec", "scaleTargetRef")
		if graphString(target, "apiVersion") != "" && graphString(target, "kind") != "" {
			b.edge(id, b.target(graphString(target, "apiVersion"), graphString(target, "kind"), o.GetNamespace(), graphString(target, "name"), ""), "scales")
		}
	case "Ingress":
		b.ingressRelations(o, id)
	}
}
func graphPodSelector(o *unstructured.Unstructured) labels.Selector {
	selector, found, _ := graphNestedMap(o.Object, "spec", "selector")
	if !found || selector == nil {
		return nil
	}
	if o.GetKind() == "Service" {
		return servicePodSelector(selector)
	}
	var parsed metav1.LabelSelector
	if kruntime.DefaultUnstructuredConverter.FromUnstructured(selector, &parsed) != nil {
		return nil
	}
	query, err := metav1.LabelSelectorAsSelector(&parsed)
	if err != nil {
		return nil
	}
	return query
}
func (b *graphRules) podSelectorRelations(o *unstructured.Unstructured, id string) {
	query := graphPodSelector(o)
	if query == nil {
		return
	}
	relation := "selects"
	if o.GetKind() == "PodDisruptionBudget" {
		relation = "protects"
	}
	for _, pod := range b.pods {
		if !b.active() {
			return
		}
		if pod.GetAPIVersion() == "v1" && pod.GetKind() == "Pod" && pod.GetNamespace() == o.GetNamespace() && query.Matches(labels.Set(pod.GetLabels())) {
			b.edge(id, b.node(b.ref(pod), graphStatus(pod)), relation)
		}
	}
}
func (b *graphRules) ingressRelations(o *unstructured.Unstructured, id string) {
	forEachIngressBackendDeclaration(o.Object, b.active, func(ref ingressBackendDeclaration) {
		b.edge(id, b.target("v1", "Service", o.GetNamespace(), ref.serviceName, ""), "routes-to")
	})
}

// Traverse each direction independently. In particular, arriving at a shared
// config/Secret never reverses direction and discovers unrelated consumers.
type graphTraversalState struct {
	id               string
	depth, direction int
}
type graphTraversal struct {
	selected, chosen               map[string]bool
	depthTruncated, limitTruncated bool
}

func graphNext(current graphTraversalState, edge cr.ResourceGraphEdge) (graphTraversalState, bool) {
	next := graphTraversalState{depth: current.depth + 1, direction: current.direction}
	if current.direction == 1 && edge.SourceID == current.id {
		next.id = edge.TargetID
	}
	if current.direction == -1 && edge.TargetID == current.id {
		next.id = edge.SourceID
	}
	supporting := edge.Relation == "selects" || edge.Relation == "protects" || edge.Relation == "scales"
	if current.direction == 1 && edge.TargetID == current.id && supporting {
		next.id = edge.SourceID
		next.direction = -1
	}
	return next, next.id != ""
}
func (t *graphTraversal) include(edge cr.ResourceGraphEdge, next graphTraversalState) bool {
	if (!t.selected[next.id] && len(t.selected) >= ResourceGraphNodeLimit) || (!t.chosen[edge.ID] && len(t.chosen) >= ResourceGraphEdgeLimit) {
		t.limitTruncated = true
		return false
	}
	t.selected[next.id] = true
	t.chosen[edge.ID] = true
	return true
}
func graphAdjacency(ctx context.Context, edges []cr.ResourceGraphEdge) (map[string][]cr.ResourceGraphEdge, map[string][]cr.ResourceGraphEdge, error) {
	forward := map[string][]cr.ResourceGraphEdge{}
	reverse := map[string][]cr.ResourceGraphEdge{}
	for _, edge := range edges {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		forward[edge.SourceID] = append(forward[edge.SourceID], edge)
		reverse[edge.TargetID] = append(reverse[edge.TargetID], edge)
		if edge.Relation == "selects" || edge.Relation == "protects" || edge.Relation == "scales" {
			forward[edge.TargetID] = append(forward[edge.TargetID], edge)
		}
	}
	return forward, reverse, nil
}

// Reserve the root's owner chain before selector fanout consumes output slots.
func graphSeedOwners(ctx context.Context, rootID string, reverse map[string][]cr.ResourceGraphEdge, result *graphTraversal) error {
	queue := []graphTraversalState{{id: rootID, direction: -1}}
	seen := map[string]bool{}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		current := queue[0]
		queue = queue[1:]
		if seen[current.id] || current.depth >= ResourceGraphDepth {
			continue
		}
		seen[current.id] = true
		for _, edge := range reverse[current.id] {
			if err := ctx.Err(); err != nil {
				return err
			}
			if edge.Relation != "owns" && edge.Relation != "controls" {
				continue
			}
			next, ok := graphNext(current, edge)
			if ok && result.include(edge, next) {
				queue = append(queue, next)
			}
		}
	}
	return nil
}
func graphTraverse(ctx context.Context, rootID string, edges []cr.ResourceGraphEdge) (*graphTraversal, error) {
	forward, reverse, err := graphAdjacency(ctx, edges)
	if err != nil {
		return nil, err
	}
	result := &graphTraversal{selected: map[string]bool{rootID: true}, chosen: map[string]bool{}}
	if err := graphSeedOwners(ctx, rootID, reverse, result); err != nil {
		return nil, err
	}
	queue := []graphTraversalState{{id: rootID, direction: 1}, {id: rootID, direction: -1}}
	seen := map[graphTraversalState]bool{}
	omitted := map[string]bool{}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := queue[0]
		queue = queue[1:]
		if seen[current] {
			continue
		}
		seen[current] = true
		adjacent := forward[current.id]
		if current.direction == -1 {
			adjacent = reverse[current.id]
		}
		for _, edge := range adjacent {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			next, ok := graphNext(current, edge)
			if !ok {
				continue
			}
			if current.depth >= ResourceGraphDepth {
				omitted[edge.ID] = true
				continue
			}
			if result.include(edge, next) {
				queue = append(queue, next)
			}
		}
	}
	for id := range omitted {
		if !result.chosen[id] {
			result.depthTruncated = true
			break
		}
	}
	return result, nil
}
func (b *graphRules) bounded(rootID, namespace string) (cr.ResourceGraph, error) {
	graph := cr.ResourceGraph{ClusterID: b.cluster, Namespace: namespace, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), RootID: rootID, Nodes: []cr.ResourceGraphNode{}, Edges: []cr.ResourceGraphEdge{}, Evidence: []cr.ResourceEvidence{}, Warnings: []string{}}
	edges := make([]cr.ResourceGraphEdge, 0, len(b.edges))
	for _, edge := range b.edges {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	selected, err := graphTraverse(b.ctx, rootID, edges)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	for id := range selected.selected {
		graph.Nodes = append(graph.Nodes, b.nodes[id])
	}
	for _, edge := range edges {
		if selected.chosen[edge.ID] {
			graph.Edges = append(graph.Edges, edge)
		}
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	if selected.limitTruncated {
		graph.Warnings = append(graph.Warnings, "resource graph truncated at 500 nodes or 1000 edges")
	}
	if selected.depthTruncated {
		graph.Warnings = append(graph.Warnings, "resource graph truncated by depth at 3 hops")
	}
	return graph, b.ctx.Err()
}
func (b *graphRules) event(graph *cr.ResourceGraph, o *unstructured.Unstructured) {
	involved, _, _ := graphNestedMap(o.Object, "involvedObject")
	str := func(key string) string { s, _ := involved[key].(string); return s }
	var id string
	for _, node := range graph.Nodes {
		if err := b.ctx.Err(); err != nil {
			b.err = err
			return
		}
		r := node.Resource
		if r.Kind == str("kind") && r.Name == str("name") && r.Namespace == str("namespace") && (str("apiVersion") == "" || r.APIVersion == str("apiVersion")) && (str("uid") == "" || r.UID == str("uid")) {
			id = node.ID
			break
		}
	}
	if id == "" {
		return
	}
	reason, _, _ := unstructured.NestedString(o.Object, "reason")
	message, _, _ := unstructured.NestedString(o.Object, "message")
	typ, _, _ := unstructured.NestedString(o.Object, "type")
	severity := "info"
	if typ == "Warning" {
		severity = "warning"
	}
	observed := graphEventObservedAt(o)
	if observed.IsZero() {
		return
	}
	graph.Evidence = append(graph.Evidence, cr.ResourceEvidence{ID: "event/" + o.GetNamespace() + "/" + o.GetName(), Type: "kubernetes-event", Severity: severity, Summary: strings.TrimSpace(reason + " " + message), ObservedAt: observed.UTC().Format(time.RFC3339Nano), ResourceID: id, SourceRef: "Event/" + o.GetNamespace() + "/" + o.GetName()})
}

func graphEventObservedAt(o *unstructured.Unstructured) time.Time {
	for _, field := range []string{"lastTimestamp", "eventTime"} {
		value, _, _ := unstructured.NestedString(o.Object, field)
		if observed, err := time.Parse(time.RFC3339Nano, value); err == nil && !observed.IsZero() {
			return observed
		}
	}
	return o.GetCreationTimestamp().Time
}

func validateNetworkGraphRootScope(root cr.ResourceRef, definition cr.NetworkTopologySourceDefinition) error {
	if root.ScopeMode != "" && root.ScopeMode != topologyScope(definition.Namespaced) {
		return invalidGraphRoot("network resource graph root scope mismatch")
	}
	if definition.Namespaced && root.Namespace == "" {
		return invalidGraphRoot("namespaced resource graph root requires namespace")
	}
	if !definition.Namespaced && root.Namespace != "" {
		return invalidGraphRoot("cluster resource graph root cannot have namespace")
	}
	return nil
}
