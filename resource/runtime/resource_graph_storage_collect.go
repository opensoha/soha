package runtime

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

func graphStorageProjection(obj kruntime.Object, projected *unstructured.Unstructured) error {
	kind := projected.GetKind()
	_, metadataOnly := obj.(*metav1.PartialObjectMetadata)
	if gvk := obj.GetObjectKind().GroupVersionKind(); !metadataOnly && !gvk.Empty() && gvk != schema.FromAPIVersionAndKind(projected.GetAPIVersion(), kind) {
		return errors.New("storage relationship observation GVK mismatch")
	}
	if projected.GetName() == "" || (kind == "PersistentVolumeClaim") != (projected.GetNamespace() != "") {
		return errors.New("storage relationship observation scope mismatch")
	}
	switch item := obj.(type) {
	case *corev1.PersistentVolumeClaim:
		spec := map[string]interface{}{"volumeName": item.Spec.VolumeName}
		if item.Spec.StorageClassName != nil {
			spec["storageClassName"] = *item.Spec.StorageClassName
		}
		projected.Object["spec"] = spec
		projected.Object["status"] = map[string]interface{}{"phase": string(item.Status.Phase)}
		if item.DeletionTimestamp != nil {
			projected.SetDeletionTimestamp(item.DeletionTimestamp)
		}
	case *corev1.PersistentVolume:
		spec := map[string]interface{}{"storageClassName": item.Spec.StorageClassName}
		if ref := item.Spec.ClaimRef; ref != nil {
			spec["claimRef"] = map[string]interface{}{"apiVersion": ref.APIVersion, "kind": ref.Kind, "namespace": ref.Namespace, "name": ref.Name, "uid": string(ref.UID)}
		}
		projected.Object["spec"] = spec
		projected.Object["status"] = map[string]interface{}{"phase": string(item.Status.Phase)}
		if item.DeletionTimestamp != nil {
			projected.SetDeletionTimestamp(item.DeletionTimestamp)
		}
	}
	return nil
}

func collectGraphStorage(ctx context.Context, client kubernetes.Interface, sources map[string]graphSourceDefinition, root *unstructured.Unstructured, objects *[]*unstructured.Unstructured, warnings *[]string) error {
	var claim, volume *unstructured.Unstructured
	var err error
	switch root.GetKind() {
	case "PersistentVolumeClaim":
		claim = root
		volume, err = collectGraphStorageTarget(ctx, "PersistentVolume", "", graphClaimVolumeName(claim), client.CoreV1().PersistentVolumes().Get, objects, warnings)
	case "PersistentVolume":
		volume = root
		ref, _ := graphVolumeClaimRef(volume)
		if ref.namespace != "" {
			claim, err = collectGraphStorageTarget(ctx, "PersistentVolumeClaim", ref.namespace, ref.name, client.CoreV1().PersistentVolumeClaims(ref.namespace).Get, objects, warnings)
		}
	case "StorageClass":
		return collectGraphStorageClassSources(ctx, client, sources, root.GetName(), objects, warnings)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	class := graphStorageClassName(root)
	getClass := client.StorageV1().StorageClasses().Get
	if _, err = collectGraphStorageTarget(ctx, "StorageClass", "", class, getClass, objects, warnings); err != nil {
		return err
	}
	bound := graphStorageBound(claim, volume)
	if bound && root == claim && graphStorageClassName(volume) != class {
		if _, err = collectGraphStorageTarget(ctx, "StorageClass", "", graphStorageClassName(volume), getClass, objects, warnings); err != nil {
			return err
		}
	}
	if root != claim && !bound {
		return ctx.Err()
	}
	ref := graphNamespacedName{namespace: claim.GetNamespace(), name: claim.GetName()}
	return collectGraphStoragePods(ctx, client, ref.namespace, map[graphNamespacedName]bool{ref: true}, objects, warnings)
}

func collectGraphStorageTarget[T kruntime.Object](ctx context.Context, kind, namespace, name string, get func(context.Context, string, metav1.GetOptions) (T, error), objects *[]*unstructured.Unstructured, warnings *[]string) (*unstructured.Unstructured, error) {
	if err := ctx.Err(); err != nil || name == "" {
		return nil, err
	}
	item, err := get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		*warnings = append(*warnings, kind+" reference source "+graphSourceErrorClass(err))
		return nil, ctx.Err()
	}
	projected, err := graphObjectProjection(item, graphBuiltinAPIs[kind], kind)
	if err != nil || projected.GetName() != name || projected.GetNamespace() != namespace {
		*warnings = append(*warnings, kind+" reference source projection unavailable")
		return nil, ctx.Err()
	}
	*objects = append(*objects, projected)
	return projected, ctx.Err()
}

func collectGraphStorageClassSources(ctx context.Context, client kubernetes.Interface, sources map[string]graphSourceDefinition, name string, objects *[]*unstructured.Unstructured, warnings *[]string) error {
	volumesStart := len(*objects)
	if err := sources["PersistentVolume"].collect(ctx, objects, warnings); err != nil {
		return err
	}
	classVolumes := map[string]*unstructured.Unstructured{}
	for _, volume := range (*objects)[volumesStart:] {
		if graphStorageClassName(volume) == name && classVolumes[volume.GetName()] == nil {
			classVolumes[volume.GetName()] = volume
		}
	}
	claimsStart := len(*objects)
	if err := sources["PersistentVolumeClaim"].collect(ctx, objects, warnings); err != nil {
		return err
	}
	claims := map[graphNamespacedName]bool{}
	for _, claim := range (*objects)[claimsStart:] {
		if graphStorageClassName(claim) == name || graphStorageBound(claim, classVolumes[graphClaimVolumeName(claim)]) {
			claims[graphNamespacedName{namespace: claim.GetNamespace(), name: claim.GetName()}] = true
		}
	}
	if len(claims) == 0 {
		return ctx.Err()
	}
	return collectGraphStoragePods(ctx, client, "", claims, objects, warnings)
}

func collectGraphStoragePods(ctx context.Context, client kubernetes.Interface, namespace string, claims map[graphNamespacedName]bool, objects *[]*unstructured.Unstructured, warnings *[]string) error {
	project := func(obj kruntime.Object) (*unstructured.Unstructured, error) {
		pod, ok := obj.(*corev1.Pod)
		if !ok || pod.Namespace == "" || (namespace != "" && pod.Namespace != namespace) {
			return nil, errors.New("storage relationship Pod scope mismatch")
		}
		volumes := []interface{}{}
		for _, volume := range pod.Spec.Volumes {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if claim := volume.PersistentVolumeClaim; claim != nil && claims[graphNamespacedName{namespace: pod.Namespace, name: claim.ClaimName}] {
				volumes = append(volumes, map[string]interface{}{"persistentVolumeClaim": map[string]interface{}{"claimName": claim.ClaimName}})
			}
		}
		if len(volumes) == 0 {
			return nil, nil
		}
		projected, err := graphObjectMetadataProjection(pod, "v1", "Pod", false)
		if err != nil {
			return nil, err
		}
		projected.Object["spec"] = map[string]interface{}{"volumes": volumes}
		projected.Object["status"] = map[string]interface{}{"phase": string(pod.Status.Phase)}
		return projected, nil
	}
	return collectGraphSource(ctx, client.CoreV1().Pods(namespace).List, "Pod", project, objects, warnings)
}
