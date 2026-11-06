package runtime

import (
	"context"
	"errors"
	"sort"

	cr "github.com/opensoha/soha-contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/metadata"
)

const ResourceGraphMetadataTargetLimit = 16

func graphWorkloadKind(kind string) bool {
	switch kind {
	case "Pod", "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "ReplicaSet", "ReplicationController":
		return true
	}
	return false
}

func graphCollectKind(rootKind, kind string) bool {
	if graphRBACRootKind(rootKind) {
		return graphCollectRBACKind(rootKind, kind)
	}
	switch rootKind {
	case "PriorityClass", "RuntimeClass":
		// One cluster-wide page per workload source, never a loop over namespaces.
		return graphWorkloadKind(kind)
	case "ResourceQuota", "LimitRange", "Lease", "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration", "PersistentVolumeClaim", "PersistentVolume", "StorageClass":
		return false
	}
	switch kind {
	case "ResourceQuota", "LimitRange", "Lease", "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration", "PersistentVolume", "StorageClass",
		"Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding":
		return false
	}
	return true
}

func graphWebhookProjection(full map[string]interface{}) []interface{} {
	result := []interface{}{}
	for _, raw := range graphSlice(full, "webhooks") {
		webhook, _ := raw.(map[string]interface{})
		config := graphMap(webhook, "clientConfig")
		service := graphMap(config, "service")
		if graphString(config, "url") != "" || service == nil {
			continue
		}
		result = append(result, map[string]interface{}{"clientConfig": map[string]interface{}{"service": map[string]interface{}{
			"namespace": graphString(service, "namespace"), "name": graphString(service, "name"),
		}}})
	}
	return result
}

func collectGraphConfigurationTargets(ctx context.Context, client metadata.Interface, root cr.ResourceRef, object *unstructured.Unstructured, reads *int, objects *[]*unstructured.Unstructured, warnings *[]string) error {
	targets := []cr.ResourceRef{}
	switch root.Kind {
	case "ResourceQuota", "LimitRange":
		targets = append(targets, cr.ResourceRef{APIVersion: "v1", Kind: "Namespace", Name: root.Namespace})
	case "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration":
		for _, target := range graphWebhookServices(object, func() bool { return ctx.Err() == nil }) {
			targets = append(targets, cr.ResourceRef{APIVersion: "v1", Kind: "Service", Namespace: target.namespace, Name: target.name})
		}
	case "Lease":
		for _, owner := range object.GetOwnerReferences() {
			if owner.Name == "" || owner.UID == "" || graphBuiltinAPIs[owner.Kind] != owner.APIVersion {
				continue
			}
			namespace := root.Namespace
			if owner.Kind == "Node" || owner.Kind == "Namespace" || graphClusterConfigurationKind(owner.Kind) {
				namespace = ""
			}
			targets = append(targets, cr.ResourceRef{APIVersion: owner.APIVersion, Kind: owner.Kind, Namespace: namespace, Name: owner.Name, UID: string(owner.UID)})
		}
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := graphIdentity(target.APIVersion, target.Kind, target.Namespace, target.Name)
		if seen[key] {
			continue
		}
		if *reads >= ResourceGraphMetadataTargetLimit {
			*warnings = append(*warnings, "configuration reference metadata truncated at 16 targets")
			break
		}
		seen[key] = true
		resource, ok := graphMetadataResource(target.APIVersion, target.Kind)
		if !ok {
			continue
		}
		if client == nil {
			*warnings = append(*warnings, target.Kind+" reference metadata unavailable")
			continue
		}
		*reads += 1
		obj, err := client.Resource(resource).Namespace(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			if fatal := graphConfigurationReadError(ctx, err, target.Kind, warnings); fatal != nil {
				return fatal
			}
			continue
		}
		projected, err := graphObjectProjection(obj, target.APIVersion, target.Kind)
		if err != nil {
			*warnings = append(*warnings, target.Kind+" reference metadata projection unavailable")
			continue
		}
		if projected.GetName() != target.Name || projected.GetNamespace() != target.Namespace || (target.UID != "" && string(projected.GetUID()) != target.UID) {
			*warnings = append(*warnings, target.Kind+" reference metadata identity changed")
			continue
		}
		*objects = append(*objects, projected)
	}
	return ctx.Err()
}

func graphConfigurationReadError(ctx context.Context, err error, kind string, warnings *[]string) error {
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	*warnings = append(*warnings, kind+" reference metadata "+graphSourceErrorClass(err))
	return nil
}

func collectGraphSecretMetadata(ctx context.Context, client metadata.Interface, graph cr.ResourceGraph, reads *int, objects *[]*unstructured.Unstructured, warnings *[]string) (bool, error) {
	targets := []cr.ResourceRef{}
	for _, node := range graph.Nodes {
		if node.Status == "unresolved" && node.Resource.APIVersion == "v1" && node.Resource.Kind == "Secret" && node.Resource.Namespace != "" {
			targets = append(targets, node.Resource)
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		return graphNodeID(targets[i]) < graphNodeID(targets[j])
	})
	changed := false
	seen := map[string]bool{}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		key := graphIdentity("v1", "Secret", target.Namespace, target.Name)
		if seen[key] {
			continue
		}
		if *reads >= ResourceGraphMetadataTargetLimit {
			*warnings = append(*warnings, "Secret reference metadata truncated at 16 targets")
			break
		}
		seen[key] = true
		*reads += 1
		object, err := client.Resource(graphSecretResource).Namespace(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			if fatal := graphConfigurationReadError(ctx, err, "Secret", warnings); fatal != nil {
				return changed, fatal
			}
			continue
		}
		projected, err := graphObjectProjection(object, "v1", "Secret")
		if err != nil {
			*warnings = append(*warnings, "Secret reference metadata projection unavailable")
			continue
		}
		if projected.GetName() != target.Name || projected.GetNamespace() != target.Namespace || (target.UID != "" && string(projected.GetUID()) != target.UID) {
			*warnings = append(*warnings, "Secret reference metadata identity changed")
			continue
		}
		*objects = append(*objects, projected)
		changed = true
	}
	return changed, ctx.Err()
}

func graphMetadataResource(api, kind string) (schema.GroupVersionResource, bool) {
	// Exact built-in resource names only; CR owners use the existing discovery path.
	resources := map[string]string{
		"Pod": "pods", "Deployment": "deployments", "StatefulSet": "statefulsets", "DaemonSet": "daemonsets", "ReplicaSet": "replicasets", "ReplicationController": "replicationcontrollers", "Job": "jobs", "CronJob": "cronjobs",
		"ConfigMap": "configmaps", "Secret": "secrets", "Service": "services", "PersistentVolumeClaim": "persistentvolumeclaims", "ServiceAccount": "serviceaccounts", "Node": "nodes", "Namespace": "namespaces",
		"Ingress": "ingresses", "HorizontalPodAutoscaler": "horizontalpodautoscalers", "PodDisruptionBudget": "poddisruptionbudgets", "ResourceQuota": "resourcequotas", "LimitRange": "limitranges", "PriorityClass": "priorityclasses", "RuntimeClass": "runtimeclasses", "Lease": "leases",
		"MutatingWebhookConfiguration": "mutatingwebhookconfigurations", "ValidatingWebhookConfiguration": "validatingwebhookconfigurations",
	}
	resource, ok := resources[kind]
	gv, err := schema.ParseGroupVersion(api)
	if !ok || err != nil || graphBuiltinAPIs[kind] != api {
		return schema.GroupVersionResource{}, false
	}
	return gv.WithResource(resource), true
}
