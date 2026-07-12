package resourcebackend

import (
	"context"
	"fmt"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	return mapPersistentVolumeClaimDetail(*item), nil
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
	items, err := bundle.Typed.StorageV1().StorageClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.StorageClassView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapStorageClass(item))
	}
	return views, nil
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
	return mapStorageClassDetail(*item), nil
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

func mapPersistentVolumeClaimDetail(item corev1.PersistentVolumeClaim) domainresource.PersistentVolumeClaimDetailView {
	storageClass := ""
	if item.Spec.StorageClassName != nil {
		storageClass = *item.Spec.StorageClassName
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	return domainresource.PersistentVolumeClaimDetailView{
		Name: item.Name, Namespace: item.Namespace, Status: string(item.Status.Phase),
		VolumeName: item.Spec.VolumeName, StorageClass: storageClass,
		AccessModes: accessModes(item.Spec.AccessModes), Requested: storageRequest(item.Spec.Resources.Requests),
		VolumeMode: volumeMode, Capacity: storageRequest(item.Status.Capacity),
		Labels: item.Labels, Annotations: item.Annotations,
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapPersistentVolume(item corev1.PersistentVolume) domainresource.PersistentVolumeView {
	claimRef, volumeMode := persistentVolumeReferences(item)
	return domainresource.PersistentVolumeView{
		Name: item.Name, Status: string(item.Status.Phase), StorageClass: item.Spec.StorageClassName,
		ClaimRef: claimRef, AccessModes: accessModes(item.Spec.AccessModes),
		Capacity: storageRequest(item.Spec.Capacity), ReclaimPolicy: string(item.Spec.PersistentVolumeReclaimPolicy),
		VolumeMode: volumeMode, AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapPersistentVolumeDetail(item corev1.PersistentVolume) domainresource.PersistentVolumeDetailView {
	claimRef, volumeMode := persistentVolumeReferences(item)
	return domainresource.PersistentVolumeDetailView{
		Name: item.Name, Status: string(item.Status.Phase), StorageClass: item.Spec.StorageClassName,
		ClaimRef: claimRef, AccessModes: accessModes(item.Spec.AccessModes),
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
		Parameters: item.Parameters, AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapStorageClassDetail(item storagev1.StorageClass) domainresource.StorageClassDetailView {
	reclaimPolicy, volumeBindingMode, allowVolumeExpansion := storageClassOptions(item)
	return domainresource.StorageClassDetailView{
		Name: item.Name, Provisioner: item.Provisioner, ReclaimPolicy: reclaimPolicy,
		VolumeBindingMode: volumeBindingMode, AllowVolumeExpansion: allowVolumeExpansion,
		Parameters: item.Parameters, Labels: item.Labels, Annotations: item.Annotations,
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
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
