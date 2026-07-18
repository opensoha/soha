package resourcebackend

import (
	"context"
	"fmt"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (d *Direct) ListPersistentVolumeClaims(ctx context.Context, clusterID, namespace string) ([]domainresource.PersistentVolumeClaimView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().PersistentVolumeClaims(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.PersistentVolumeClaimView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapPersistentVolumeClaim(item))
	}
	return views, nil
}

func (d *Direct) GetPersistentVolumeClaimDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.PersistentVolumeClaimDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.PersistentVolumeClaimDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().PersistentVolumeClaims(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.PersistentVolumeClaimDetailView{}, err
	}
	pods, err := d.listReferencePods(queryCtx, bundle, clusterID, namespace)
	if err != nil {
		return mapPersistentVolumeClaimDetail(*item, nil), nil
	}
	return mapPersistentVolumeClaimDetail(*item, pods), nil
}

func (d *Direct) ListPersistentVolumes(ctx context.Context, clusterID string) ([]domainresource.PersistentVolumeView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().PersistentVolumes().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.PersistentVolumeView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapPersistentVolume(item))
	}
	return views, nil
}

func (d *Direct) GetPersistentVolumeDetail(ctx context.Context, clusterID, name string) (domainresource.PersistentVolumeDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.PersistentVolumeDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().PersistentVolumes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.PersistentVolumeDetailView{}, err
	}
	return mapPersistentVolumeDetail(*item), nil
}

func (d *Direct) ListStorageClasses(ctx context.Context, clusterID string) ([]domainresource.StorageClassView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	table, err := listTable(queryCtx, bundle, schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}, false, "")
	if err != nil {
		return nil, err
	}
	return mapStorageClassTable(table)
}

func (d *Direct) GetStorageClassDetail(ctx context.Context, clusterID, name string) (domainresource.StorageClassDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.StorageClassDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := bundle.Typed.StorageV1().StorageClasses().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.StorageClassDetailView{}, err
	}
	volumes, claims, volumesTruncated, claimsTruncated, err := listStorageClassRelations(queryCtx, bundle, name)
	if err != nil {
		return mapStorageClassDetail(*item, nil, nil, false, false), nil
	}
	return mapStorageClassDetail(*item, volumes, claims, volumesTruncated, claimsTruncated), nil
}

func mapPersistentVolumeClaim(item corev1.PersistentVolumeClaim) domainresource.PersistentVolumeClaimView {
	requested := storageRequest(item.Spec.Resources.Requests)
	storageClass := ""
	if item.Spec.StorageClassName != nil {
		storageClass = *item.Spec.StorageClassName
	}
	return domainresource.PersistentVolumeClaimView{
		Name: item.Name, Namespace: item.Namespace, Status: string(item.Status.Phase),
		VolumeName: item.Spec.VolumeName, StorageClass: storageClass,
		AccessModes: accessModes(item.Spec.AccessModes), Requested: requested,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapPersistentVolumeClaimDetail(item corev1.PersistentVolumeClaim, pods []corev1.Pod) domainresource.PersistentVolumeClaimDetailView {
	storageClass := ""
	if item.Spec.StorageClassName != nil {
		storageClass = *item.Spec.StorageClassName
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	podRefs, podsTruncated := storageClaimPods(pods, item.Name)
	return domainresource.PersistentVolumeClaimDetailView{
		Name: item.Name, Namespace: item.Namespace, Status: string(item.Status.Phase),
		VolumeName: item.Spec.VolumeName, StorageClass: storageClass,
		AccessModes: accessModes(item.Spec.AccessModes), Requested: storageRequest(item.Spec.Resources.Requests),
		VolumeMode: volumeMode, Capacity: storageRequest(item.Status.Capacity),
		Labels: item.Labels, Annotations: item.Annotations,
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
		Pods: podRefs, PodsTruncated: podsTruncated,
	}
}

func mapPersistentVolume(item corev1.PersistentVolume) domainresource.PersistentVolumeView {
	claimRef, volumeMode := persistentVolumeReferences(item)
	claimNamespace, claimName := "", ""
	if item.Spec.ClaimRef != nil {
		claimNamespace, claimName = item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name
	}
	return domainresource.PersistentVolumeView{
		Name: item.Name, Status: string(item.Status.Phase), StorageClass: item.Spec.StorageClassName,
		ClaimRef: claimRef, ClaimNamespace: claimNamespace, ClaimName: claimName, AccessModes: accessModes(item.Spec.AccessModes),
		Capacity: storageRequest(item.Spec.Capacity), ReclaimPolicy: string(item.Spec.PersistentVolumeReclaimPolicy),
		VolumeMode: volumeMode, AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapPersistentVolumeDetail(item corev1.PersistentVolume) domainresource.PersistentVolumeDetailView {
	claimRef, volumeMode := persistentVolumeReferences(item)
	claimNamespace, claimName := "", ""
	if item.Spec.ClaimRef != nil {
		claimNamespace, claimName = item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name
	}
	return domainresource.PersistentVolumeDetailView{
		Name: item.Name, Status: string(item.Status.Phase), StorageClass: item.Spec.StorageClassName,
		ClaimRef: claimRef, ClaimNamespace: claimNamespace, ClaimName: claimName, AccessModes: accessModes(item.Spec.AccessModes),
		Capacity: storageRequest(item.Spec.Capacity), ReclaimPolicy: string(item.Spec.PersistentVolumeReclaimPolicy),
		VolumeMode: volumeMode, Labels: item.Labels, Annotations: item.Annotations,
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapStorageClass(item storagev1.StorageClass) domainresource.StorageClassView {
	reclaimPolicy, volumeBindingMode, allowVolumeExpansion := storageClassOptions(item)
	return domainresource.StorageClassView{
		Name: item.Name, Provisioner: item.Provisioner, ReclaimPolicy: reclaimPolicy,
		VolumeBindingMode: volumeBindingMode, AllowVolumeExpansion: allowVolumeExpansion,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapStorageClassTable(table metav1.Table) ([]domainresource.StorageClassView, error) {
	provisionerColumn := tableColumnIndex(table.ColumnDefinitions, "Provisioner")
	reclaimPolicyColumn := tableColumnIndex(table.ColumnDefinitions, "ReclaimPolicy")
	volumeBindingModeColumn := tableColumnIndex(table.ColumnDefinitions, "VolumeBindingMode")
	allowExpansionColumn := tableColumnIndex(table.ColumnDefinitions, "AllowVolumeExpansion")
	if provisionerColumn < 0 || reclaimPolicyColumn < 0 || volumeBindingModeColumn < 0 || allowExpansionColumn < 0 {
		return nil, fmt.Errorf("storage class table is missing required columns")
	}
	return tableViews(table, func(row metav1.TableRow, metadata metav1.Object) (domainresource.StorageClassView, error) {
		provisioner, err := tableStringCell(row.Cells, provisionerColumn)
		if err != nil {
			return domainresource.StorageClassView{}, err
		}
		reclaimPolicy, err := tableStringCell(row.Cells, reclaimPolicyColumn)
		if err != nil {
			return domainresource.StorageClassView{}, err
		}
		volumeBindingMode, err := tableStringCell(row.Cells, volumeBindingModeColumn)
		if err != nil {
			return domainresource.StorageClassView{}, err
		}
		allowExpansion, err := tableBoolCell(row.Cells, allowExpansionColumn)
		if err != nil {
			return domainresource.StorageClassView{}, err
		}
		return domainresource.StorageClassView{Name: metadata.GetName(), Provisioner: provisioner, ReclaimPolicy: reclaimPolicy, VolumeBindingMode: volumeBindingMode, AllowVolumeExpansion: allowExpansion, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time)}, nil
	})
}

func mapStorageClassDetail(item storagev1.StorageClass, volumes []domainresource.PersistentVolumeView, claims []domainresource.PersistentVolumeClaimView, volumesTruncated, claimsTruncated bool) domainresource.StorageClassDetailView {
	reclaimPolicy, volumeBindingMode, allowVolumeExpansion := storageClassOptions(item)
	return domainresource.StorageClassDetailView{
		Name: item.Name, Provisioner: item.Provisioner, ReclaimPolicy: reclaimPolicy,
		VolumeBindingMode: volumeBindingMode, AllowVolumeExpansion: allowVolumeExpansion,
		Parameters: item.Parameters, Labels: item.Labels, Annotations: item.Annotations,
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
		Volumes: volumes, Claims: claims, VolumesTruncated: volumesTruncated, ClaimsTruncated: claimsTruncated,
	}
}

func storageClaimPods(pods []corev1.Pod, claimName string) ([]domainresource.StoragePodReferenceView, bool) {
	refs := make([]domainresource.StoragePodReferenceView, 0)
	for _, pod := range pods {
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName != claimName {
				continue
			}
			if len(refs) == domainresource.StorageRelationLimit {
				return refs, true
			}
			refs = append(refs, domainresource.StoragePodReferenceView{Name: pod.Name, Namespace: pod.Namespace, Phase: string(pod.Status.Phase), NodeName: pod.Spec.NodeName})
			break
		}
	}
	return refs, false
}

func listStorageClassRelations(ctx context.Context, bundle *k8sinfra.Bundle, name string) ([]domainresource.PersistentVolumeView, []domainresource.PersistentVolumeClaimView, bool, bool, error) {
	volumes := make([]domainresource.PersistentVolumeView, 0)
	volumeOptions := metav1.ListOptions{Limit: tableListPageSize}
	volumesTruncated := false
volumePages:
	for {
		page, err := bundle.Typed.CoreV1().PersistentVolumes().List(ctx, volumeOptions)
		if err != nil {
			return nil, nil, false, false, err
		}
		for _, item := range page.Items {
			if item.Spec.StorageClassName != name {
				continue
			}
			if len(volumes) == domainresource.StorageRelationLimit {
				volumesTruncated = true
				break volumePages
			}
			volumes = append(volumes, mapPersistentVolume(item))
		}
		if page.Continue == "" {
			break
		}
		if page.Continue == volumeOptions.Continue {
			return nil, nil, false, false, fmt.Errorf("persistentvolume listing returned a repeated continue token")
		}
		volumeOptions.Continue = page.Continue
	}

	claims := make([]domainresource.PersistentVolumeClaimView, 0)
	claimOptions := metav1.ListOptions{Limit: tableListPageSize}
	claimsTruncated := false
claimPages:
	for {
		page, err := bundle.Typed.CoreV1().PersistentVolumeClaims("").List(ctx, claimOptions)
		if err != nil {
			return nil, nil, false, false, err
		}
		for _, item := range page.Items {
			if item.Spec.StorageClassName == nil || *item.Spec.StorageClassName != name {
				continue
			}
			if len(claims) == domainresource.StorageRelationLimit {
				claimsTruncated = true
				break claimPages
			}
			claims = append(claims, mapPersistentVolumeClaim(item))
		}
		if page.Continue == "" {
			break
		}
		if page.Continue == claimOptions.Continue {
			return nil, nil, false, false, fmt.Errorf("persistentvolumeclaim listing returned a repeated continue token")
		}
		claimOptions.Continue = page.Continue
	}
	return volumes, claims, volumesTruncated, claimsTruncated, nil
}

func storageRequest(resources corev1.ResourceList) string {
	if quantity, ok := resources[corev1.ResourceStorage]; ok {
		return quantity.String()
	}
	return ""
}

func accessModes(items []corev1.PersistentVolumeAccessMode) []string {
	modes := make([]string, 0, len(items))
	for _, mode := range items {
		modes = append(modes, string(mode))
	}
	return modes
}

func persistentVolumeReferences(item corev1.PersistentVolume) (string, string) {
	claimRef := ""
	if item.Spec.ClaimRef != nil {
		claimRef = fmt.Sprintf("%s/%s", item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name)
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	return claimRef, volumeMode
}

func storageClassOptions(item storagev1.StorageClass) (string, string, bool) {
	reclaimPolicy := ""
	if item.ReclaimPolicy != nil {
		reclaimPolicy = string(*item.ReclaimPolicy)
	}
	volumeBindingMode := ""
	if item.VolumeBindingMode != nil {
		volumeBindingMode = string(*item.VolumeBindingMode)
	}
	allowVolumeExpansion := item.AllowVolumeExpansion != nil && *item.AllowVolumeExpansion
	return reclaimPolicy, volumeBindingMode, allowVolumeExpansion
}

var _ appresource.DirectStorageReader = (*Direct)(nil)
