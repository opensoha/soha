package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
)

const ResourceGraphSourceLimit int64 = 500
const ResourceGraphNodeLimit = 500
const ResourceGraphEdgeLimit = 1000
const ResourceGraphDepth = 3
const ResourceGraphTimeout = 8 * time.Second

var graphSecretResource = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// BuildKubernetesResourceGraph is the common Direct/Agent read-only entry point.
// Root failures remain wrapped Kubernetes errors. Partial source failures are
// explicit warnings; cancellation and the total collection deadline are errors.
func BuildKubernetesResourceGraph(ctx context.Context, client kubernetes.Interface, root cr.ResourceRef) (cr.ResourceGraph, error) {
	return BuildKubernetesResourceGraphWithMetadata(ctx, client, nil, root)
}

// BuildKubernetesResourceGraphWithMetadata optionally confirms Secret identities
// using metadata-only HTTP negotiation. Callers must supply a metadata client
// whose transport excludes the client-go full-JSON fallback from Accept headers.
// It never calls the typed Secret API.
func BuildKubernetesResourceGraphWithMetadata(ctx context.Context, client kubernetes.Interface, metadataClient metadata.Interface, root cr.ResourceRef) (cr.ResourceGraph, error) {
	root, err := NormalizeResourceGraphRoot(root)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, ResourceGraphTimeout)
	defer cancel()
	sources := graphSources(client, root.Namespace)
	source, ok := sources[root.Kind]
	if !ok && (root.Kind != "Secret" || metadataClient == nil) {
		return cr.ResourceGraph{}, fmt.Errorf("%w: %s; Secret roots require metadata-only collection", ErrUnsupportedResourceGraphRoot, root.Kind)
	}
	if root.APIVersion != "" && root.APIVersion != graphBuiltinAPIs[root.Kind] {
		return cr.ResourceGraph{}, invalidGraphRoot("resource graph root apiVersion mismatch")
	}
	var object kruntime.Object
	if root.Kind == "Secret" {
		object, err = metadataClient.Resource(graphSecretResource).Namespace(root.Namespace).Get(ctx, root.Name, metav1.GetOptions{})
	} else {
		object, err = source.get(ctx, root.Name, metav1.GetOptions{})
	}
	if err != nil {
		return cr.ResourceGraph{}, fmt.Errorf("get resource graph root %s/%s: %w", root.Kind, root.Name, err)
	}
	if err = ctx.Err(); err != nil {
		return cr.ResourceGraph{}, err
	}
	projected, err := graphObjectProjection(object, graphBuiltinAPIs[root.Kind], root.Kind)
	if err != nil {
		if graphStorageRootKind(root.Kind) {
			return cr.ResourceGraph{}, invalidGraphRoot("storage resource graph root projection unavailable")
		}
		return cr.ResourceGraph{}, err
	}
	if root.UID != "" && root.UID != string(projected.GetUID()) {
		return cr.ResourceGraph{}, invalidGraphRoot("resource graph root UID changed")
	}
	if (graphStorageRootKind(root.Kind) || graphRBACRootKind(root.Kind)) && (projected.GetName() != root.Name || projected.GetNamespace() != root.Namespace) {
		return cr.ResourceGraph{}, invalidGraphRoot("resource graph root identity changed")
	}
	objects := []*unstructured.Unstructured{projected}
	warnings := []string{}
	kinds := make([]string, 0, len(sources))
	for kind := range sources {
		if graphCollectKind(root.Kind, kind) {
			kinds = append(kinds, kind)
		}
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		if err = sources[kind].collect(ctx, &objects, &warnings); err != nil {
			return cr.ResourceGraph{}, err
		}
	}
	if err = collectGraphCrossNamespaceAccounts(ctx, client, projected, &objects, &warnings); err != nil {
		return cr.ResourceGraph{}, err
	}
	if err = collectGraphStorage(ctx, client, sources, projected, &objects, &warnings); err != nil {
		return cr.ResourceGraph{}, err
	}
	metadataReads := 0
	if err = collectGraphConfigurationTargets(ctx, metadataClient, root, projected, &metadataReads, &objects, &warnings); err != nil {
		return cr.ResourceGraph{}, err
	}
	graph, err := buildResourceGraphWithContext(ctx, root, objects)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	if metadataClient != nil {
		changed, metadataErr := collectGraphSecretMetadata(ctx, metadataClient, graph, &metadataReads, &objects, &warnings)
		if metadataErr != nil {
			return cr.ResourceGraph{}, metadataErr
		}
		if changed {
			graph, err = buildResourceGraphWithContext(ctx, root, objects)
		}
	}
	if canceled := ctx.Err(); canceled != nil {
		return cr.ResourceGraph{}, canceled
	}
	graph.Warnings = append(graph.Warnings, warnings...)
	for _, node := range graph.Nodes {
		if node.Resource.APIVersion == "v1" && node.Resource.Kind == "Secret" && node.Status == "unresolved" {
			graph.Warnings = append(graph.Warnings, "Secret targets are declared references only; metadata was not read and existence is unverified")
			break
		}
	}
	return graph, err
}

type graphSourceDefinition struct {
	get     func(context.Context, string, metav1.GetOptions) (kruntime.Object, error)
	collect func(context.Context, *[]*unstructured.Unstructured, *[]string) error
}

func graphSource[O kruntime.Object, L kruntime.Object](kind string, get func(context.Context, string, metav1.GetOptions) (O, error), list func(context.Context, metav1.ListOptions) (L, error)) graphSourceDefinition {
	return graphSourceDefinition{
		get: func(ctx context.Context, name string, options metav1.GetOptions) (kruntime.Object, error) {
			return get(ctx, name, options)
		},
		collect: func(ctx context.Context, objects *[]*unstructured.Unstructured, warnings *[]string) error {
			return collectGraphSource(ctx, list, kind, func(obj kruntime.Object) (*unstructured.Unstructured, error) {
				return graphObjectProjection(obj, graphBuiltinAPIs[kind], kind)
			}, objects, warnings)
		},
	}
}
func graphSources(client kubernetes.Interface, namespace string) map[string]graphSourceDefinition {
	return map[string]graphSourceDefinition{
		"Pod":                            graphSource("Pod", client.CoreV1().Pods(namespace).Get, client.CoreV1().Pods(namespace).List),
		"Deployment":                     graphSource("Deployment", client.AppsV1().Deployments(namespace).Get, client.AppsV1().Deployments(namespace).List),
		"StatefulSet":                    graphSource("StatefulSet", client.AppsV1().StatefulSets(namespace).Get, client.AppsV1().StatefulSets(namespace).List),
		"DaemonSet":                      graphSource("DaemonSet", client.AppsV1().DaemonSets(namespace).Get, client.AppsV1().DaemonSets(namespace).List),
		"Job":                            graphSource("Job", client.BatchV1().Jobs(namespace).Get, client.BatchV1().Jobs(namespace).List),
		"CronJob":                        graphSource("CronJob", client.BatchV1().CronJobs(namespace).Get, client.BatchV1().CronJobs(namespace).List),
		"ReplicaSet":                     graphSource("ReplicaSet", client.AppsV1().ReplicaSets(namespace).Get, client.AppsV1().ReplicaSets(namespace).List),
		"ReplicationController":          graphSource("ReplicationController", client.CoreV1().ReplicationControllers(namespace).Get, client.CoreV1().ReplicationControllers(namespace).List),
		"Service":                        graphSource("Service", client.CoreV1().Services(namespace).Get, client.CoreV1().Services(namespace).List),
		"Ingress":                        graphSource("Ingress", client.NetworkingV1().Ingresses(namespace).Get, client.NetworkingV1().Ingresses(namespace).List),
		"HorizontalPodAutoscaler":        graphSource("HorizontalPodAutoscaler", client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get, client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List),
		"PodDisruptionBudget":            graphSource("PodDisruptionBudget", client.PolicyV1().PodDisruptionBudgets(namespace).Get, client.PolicyV1().PodDisruptionBudgets(namespace).List),
		"ConfigMap":                      graphSource("ConfigMap", client.CoreV1().ConfigMaps(namespace).Get, client.CoreV1().ConfigMaps(namespace).List),
		"ResourceQuota":                  graphSource("ResourceQuota", client.CoreV1().ResourceQuotas(namespace).Get, client.CoreV1().ResourceQuotas(namespace).List),
		"LimitRange":                     graphSource("LimitRange", client.CoreV1().LimitRanges(namespace).Get, client.CoreV1().LimitRanges(namespace).List),
		"PriorityClass":                  graphSource("PriorityClass", client.SchedulingV1().PriorityClasses().Get, client.SchedulingV1().PriorityClasses().List),
		"RuntimeClass":                   graphSource("RuntimeClass", client.NodeV1().RuntimeClasses().Get, client.NodeV1().RuntimeClasses().List),
		"Lease":                          graphSource("Lease", client.CoordinationV1().Leases(namespace).Get, client.CoordinationV1().Leases(namespace).List),
		"MutatingWebhookConfiguration":   graphSource("MutatingWebhookConfiguration", client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get, client.AdmissionregistrationV1().MutatingWebhookConfigurations().List),
		"ValidatingWebhookConfiguration": graphSource("ValidatingWebhookConfiguration", client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get, client.AdmissionregistrationV1().ValidatingWebhookConfigurations().List),
		"PersistentVolumeClaim":          graphSource("PersistentVolumeClaim", client.CoreV1().PersistentVolumeClaims(namespace).Get, client.CoreV1().PersistentVolumeClaims(namespace).List),
		"PersistentVolume":               graphSource("PersistentVolume", client.CoreV1().PersistentVolumes().Get, client.CoreV1().PersistentVolumes().List),
		"StorageClass":                   graphSource("StorageClass", client.StorageV1().StorageClasses().Get, client.StorageV1().StorageClasses().List),
		"ServiceAccount":                 graphSource("ServiceAccount", client.CoreV1().ServiceAccounts(namespace).Get, client.CoreV1().ServiceAccounts(namespace).List),
		"Role":                           graphSource("Role", client.RbacV1().Roles(namespace).Get, client.RbacV1().Roles(namespace).List),
		"RoleBinding":                    graphSource("RoleBinding", client.RbacV1().RoleBindings(namespace).Get, client.RbacV1().RoleBindings(namespace).List),
		"ClusterRole":                    graphSource("ClusterRole", client.RbacV1().ClusterRoles().Get, client.RbacV1().ClusterRoles().List),
		"ClusterRoleBinding":             graphSource("ClusterRoleBinding", client.RbacV1().ClusterRoleBindings().Get, client.RbacV1().ClusterRoleBindings().List),
		"Node":                           graphSource("Node", client.CoreV1().Nodes().Get, client.CoreV1().Nodes().List),
		"Event":                          graphSource("Event", client.CoreV1().Events(namespace).Get, client.CoreV1().Events(namespace).List),
	}
}

// Projection immediately discards payloads outside relationship inputs; Secret
// data, ConfigMap data and annotations are never inspected or retained.
func graphObjectProjection(obj kruntime.Object, api, kind string) (*unstructured.Unstructured, error) {
	if _, metadataOnly := obj.(*metav1.PartialObjectMetadata); graphRBACRootKind(kind) && !metadataOnly {
		if gvk := obj.GetObjectKind().GroupVersionKind(); !gvk.Empty() && (gvk.GroupVersion().String() != api || gvk.Kind != kind) {
			return nil, invalidGraphRoot("RBAC resource graph object apiVersion or kind mismatch")
		}
	}
	storage := graphStorageRootKind(kind)
	u, err := graphObjectMetadataProjection(obj, api, kind, !storage || kind == "PersistentVolumeClaim")
	if err != nil {
		return nil, err
	}
	if storage {
		return u, graphStorageProjection(obj, u)
	}
	if graphRBACRootKind(kind) {
		return u, graphRBACProjection(obj, u)
	}
	if kind == "Secret" || kind == "ConfigMap" || kind == "Node" || kind == "Namespace" {
		return u, nil
	}
	full, err := kruntime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	if spec, ok := full["spec"]; ok {
		u.Object["spec"] = spec
	}
	if status, ok := full["status"]; ok {
		u.Object["status"] = status
	}
	if kind == "Event" {
		for _, key := range []string{"involvedObject", "reason", "message", "type", "lastTimestamp", "eventTime"} {
			u.Object[key] = full[key]
		}
	}
	if kind == "MutatingWebhookConfiguration" || kind == "ValidatingWebhookConfiguration" {
		// Admission rules, CA bundles and expressions are not relationship inputs.
		u.Object["webhooks"] = graphWebhookProjection(full)
	}
	return u, nil
}
func graphObjectMetadataProjection(obj kruntime.Object, api, kind string, includeRelationships bool) (*unstructured.Unstructured, error) {
	accessor, err := metav1Object(obj)
	if err != nil {
		return nil, err
	}
	metadata := map[string]interface{}{"name": accessor.GetName(), "namespace": accessor.GetNamespace(), "uid": string(accessor.GetUID()), "resourceVersion": accessor.GetResourceVersion()}
	if created := accessor.GetCreationTimestamp(); kind == "Event" && !created.IsZero() {
		metadata["creationTimestamp"] = created.UTC().Format(time.RFC3339Nano)
	}
	if includeRelationships {
		labels := map[string]interface{}{}
		for k, v := range accessor.GetLabels() {
			labels[k] = v
		}
		metadata["labels"] = labels
		owners := []interface{}{}
		for _, owner := range accessor.GetOwnerReferences() {
			projected, err := kruntime.DefaultUnstructuredConverter.ToUnstructured(&owner)
			if err != nil {
				return nil, err
			}
			owners = append(owners, projected)
		}
		metadata["ownerReferences"] = owners
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": api, "kind": kind, "metadata": metadata}}, nil
}
func metav1Object(obj kruntime.Object) (metav1.Object, error) {
	o, ok := obj.(metav1.Object)
	if !ok {
		return nil, fmt.Errorf("resource lacks metadata")
	}
	return o, nil
}

// Only one bounded page is read. A continuation token is an explicit partial
// observation, never permission to issue an unbounded namespace scan.
func collectGraphSource[T kruntime.Object](ctx context.Context, list func(context.Context, metav1.ListOptions) (T, error), kind string, project func(kruntime.Object) (*unstructured.Unstructured, error), objects *[]*unstructured.Unstructured, warnings *[]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := list(ctx, metav1.ListOptions{Limit: ResourceGraphSourceLimit})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		*warnings = append(*warnings, kind+" source "+graphSourceErrorClass(err))
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	items, err := meta.ExtractList(result)
	if err != nil {
		*warnings = append(*warnings, kind+" source projection unavailable")
		return nil
	}
	accessor, err := meta.ListAccessor(result)
	if err != nil {
		return err
	}
	count := len(items)
	if count > int(ResourceGraphSourceLimit) {
		count = int(ResourceGraphSourceLimit)
	}
	for _, item := range items[:count] {
		if err := ctx.Err(); err != nil {
			return err
		}
		projected, err := project(item)
		if err != nil {
			*warnings = append(*warnings, kind+" source projection unavailable")
			continue
		}
		if projected != nil {
			*objects = append(*objects, projected)
		}
	}
	if accessor.GetContinue() != "" || len(items) > int(ResourceGraphSourceLimit) || graphHasRemaining(accessor) {
		*warnings = append(*warnings, kind+" source truncated at 500 objects")
	}
	return nil
}

func graphHasRemaining(accessor metav1.ListInterface) bool {
	remaining := accessor.GetRemainingItemCount()
	return remaining != nil && *remaining > 0
}
func graphSourceErrorClass(err error) string {
	switch {
	case apierrors.IsForbidden(err):
		return "Forbidden"
	case apierrors.IsNotFound(err):
		return "NotFound"
	default:
		return "unavailable"
	}
}
