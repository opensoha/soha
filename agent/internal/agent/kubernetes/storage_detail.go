package kubernetes

import (
	"context"
	"fmt"
	"time"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) GetPersistentVolumeClaimDetail(ctx context.Context, namespace, name string) (domainresource.PersistentVolumeClaimDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().PersistentVolumeClaims(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.PersistentVolumeClaimDetailView{}, err
	}
	pods, err := c.listStorageClaimPods(queryCtx, namespace)
	if err != nil {
		return mapPersistentVolumeClaimDetail(*item, nil), nil
	}
	return mapPersistentVolumeClaimDetail(*item, pods), nil
}

func (c *Client) GetPersistentVolumeDetail(ctx context.Context, name string) (domainresource.PersistentVolumeDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().PersistentVolumes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.PersistentVolumeDetailView{}, err
	}
	return mapPersistentVolumeDetail(*item), nil
}

func (c *Client) GetStorageClassDetail(ctx context.Context, name string) (domainresource.StorageClassDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.StorageV1().StorageClasses().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.StorageClassDetailView{}, err
	}
	volumes, claims, volumesTruncated, claimsTruncated, err := c.listStorageClassRelations(queryCtx, name)
	if err != nil {
		return mapStorageClassDetail(*item, nil, nil, false, false), nil
	}
	return mapStorageClassDetail(*item, volumes, claims, volumesTruncated, claimsTruncated), nil
}

func mapPersistentVolumeClaimDetail(item corev1.PersistentVolumeClaim, pods []corev1.Pod) domainresource.PersistentVolumeClaimDetailView {
	view := mapPersistentVolumeClaim(item)
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	podRefs, podsTruncated := storageClaimPods(pods, item.Name)
	return domainresource.PersistentVolumeClaimDetailView{
		Name: view.Name, Namespace: view.Namespace, Status: view.Status, VolumeName: view.VolumeName,
		StorageClass: view.StorageClass, AccessModes: view.AccessModes, Requested: view.Requested,
		VolumeMode: volumeMode, Capacity: storageRequest(item.Status.Capacity), Labels: item.Labels,
		Annotations: item.Annotations, CreatedAt: item.CreationTimestamp.Format(time.RFC3339),
		AgeSeconds: view.AgeSeconds, Pods: podRefs, PodsTruncated: podsTruncated,
	}
}

func mapPersistentVolumeDetail(item corev1.PersistentVolume) domainresource.PersistentVolumeDetailView {
	view := mapPersistentVolume(item)
	claimNamespace, claimName := "", ""
	if item.Spec.ClaimRef != nil {
		claimNamespace, claimName = item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name
	}
	return domainresource.PersistentVolumeDetailView{
		Name: view.Name, Status: view.Status, StorageClass: view.StorageClass, ClaimRef: view.ClaimRef,
		ClaimNamespace: claimNamespace, ClaimName: claimName, AccessModes: view.AccessModes,
		Capacity: view.Capacity, ReclaimPolicy: view.ReclaimPolicy, VolumeMode: view.VolumeMode,
		Labels: item.Labels, Annotations: item.Annotations,
		CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: view.AgeSeconds,
	}
}

func mapStorageClassDetail(item storagev1.StorageClass, volumes []domainresource.PersistentVolumeView, claims []domainresource.PersistentVolumeClaimView, volumesTruncated, claimsTruncated bool) domainresource.StorageClassDetailView {
	reclaimPolicy, bindingMode, allowExpansion := storageClassOptions(item)
	return domainresource.StorageClassDetailView{
		Name: item.Name, Provisioner: item.Provisioner, ReclaimPolicy: reclaimPolicy,
		VolumeBindingMode: bindingMode, AllowVolumeExpansion: allowExpansion, Parameters: item.Parameters,
		Labels: item.Labels, Annotations: item.Annotations,
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

func (c *Client) listStorageClaimPods(ctx context.Context, namespace string) ([]corev1.Pod, error) {
	items := make([]corev1.Pod, 0)
	options := metav1.ListOptions{Limit: agentTablePageSize}
	for {
		page, err := c.typed.CoreV1().Pods(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.Continue == "" {
			return items, nil
		}
		if page.Continue == options.Continue {
			return nil, fmt.Errorf("pod listing returned a repeated continue token")
		}
		options.Continue = page.Continue
	}
}

func storageRequest(resources corev1.ResourceList) string {
	if quantity, ok := resources[corev1.ResourceStorage]; ok {
		return quantity.String()
	}
	return ""
}

func storageClassOptions(item storagev1.StorageClass) (string, string, bool) {
	reclaimPolicy, bindingMode := "", ""
	if item.ReclaimPolicy != nil {
		reclaimPolicy = string(*item.ReclaimPolicy)
	}
	if item.VolumeBindingMode != nil {
		bindingMode = string(*item.VolumeBindingMode)
	}
	return reclaimPolicy, bindingMode, item.AllowVolumeExpansion != nil && *item.AllowVolumeExpansion
}

func (c *Client) listStorageClassRelations(ctx context.Context, name string) ([]domainresource.PersistentVolumeView, []domainresource.PersistentVolumeClaimView, bool, bool, error) {
	volumes := make([]domainresource.PersistentVolumeView, 0)
	volumeOptions := metav1.ListOptions{Limit: agentTablePageSize}
	volumesTruncated := false
volumePages:
	for {
		page, err := c.typed.CoreV1().PersistentVolumes().List(ctx, volumeOptions)
		if err != nil {
			return nil, nil, false, false, err
		}
		for _, item := range page.Items {
			if item.Spec.StorageClassName == name {
				if len(volumes) == domainresource.StorageRelationLimit {
					volumesTruncated = true
					break volumePages
				}
				volumes = append(volumes, mapPersistentVolume(item))
			}
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
	claimOptions := metav1.ListOptions{Limit: agentTablePageSize}
	claimsTruncated := false
claimPages:
	for {
		page, err := c.typed.CoreV1().PersistentVolumeClaims("").List(ctx, claimOptions)
		if err != nil {
			return nil, nil, false, false, err
		}
		for _, item := range page.Items {
			if item.Spec.StorageClassName != nil && *item.Spec.StorageClassName == name {
				if len(claims) == domainresource.StorageRelationLimit {
					claimsTruncated = true
					break claimPages
				}
				claims = append(claims, mapPersistentVolumeClaim(item))
			}
		}
		if page.Continue == "" {
			return volumes, claims, volumesTruncated, claimsTruncated, nil
		}
		if page.Continue == claimOptions.Continue {
			return nil, nil, false, false, fmt.Errorf("persistentvolumeclaim listing returned a repeated continue token")
		}
		claimOptions.Continue = page.Continue
	}
	return volumes, claims, volumesTruncated, claimsTruncated, nil
}
