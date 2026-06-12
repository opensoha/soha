package resource

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListPersistentVolumeClaims(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PersistentVolumeClaimView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "PersistentVolumeClaim", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.PersistentVolumeClaimView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListPersistentVolumeClaims(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectPersistentVolumeClaims(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.PersistentVolumeClaimView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapPersistentVolumeClaim(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.PersistentVolumeClaimView) string { return item.Namespace })
	populateAllowedActionsPersistentVolumeClaims(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "PersistentVolumeClaim", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed pvc via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) GetPersistentVolumeClaimDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.PersistentVolumeClaimDetailView, error) {
	if strings.TrimSpace(namespace) == "" {
		return domainresource.PersistentVolumeClaimDetailView{}, fmt.Errorf("%w: namespace is required for persistentvolumeclaim detail", apperrors.ErrInvalidArgument)
	}
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "PersistentVolumeClaim", domainaccess.ActionView)
	if err != nil {
		return domainresource.PersistentVolumeClaimDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.PersistentVolumeClaimDetailView{}, unsupportedAgentOperation("persistentvolumeclaim detail is not supported for agent-connected clusters yet")
	}
	rawItem, err := s.getDirectPersistentVolumeClaim(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.PersistentVolumeClaimDetailView{}, err
	}
	item := mapPersistentVolumeClaimDetail(*rawItem, decision)
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "PersistentVolumeClaim", name, string(domainaccess.ActionView), "success", "viewed persistentvolumeclaim detail")
	return item, nil
}
func (s *Service) ListPersistentVolumes(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.PersistentVolumeView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "PersistentVolume", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.PersistentVolumeView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListPersistentVolumes(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectPersistentVolumes(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.PersistentVolumeView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapPersistentVolume(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsPersistentVolumes(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "PersistentVolume", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed pv via %s", source))
	return items, nil
}
func (s *Service) GetPersistentVolumeDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.PersistentVolumeDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "PersistentVolume", domainaccess.ActionView)
	if err != nil {
		return domainresource.PersistentVolumeDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.PersistentVolumeDetailView{}, unsupportedAgentOperation("persistentvolume detail is not supported for agent-connected clusters yet")
	}
	rawItem, err := s.getDirectPersistentVolume(ctx, clusterID, name)
	if err != nil {
		return domainresource.PersistentVolumeDetailView{}, err
	}
	item := mapPersistentVolumeDetail(*rawItem, decision)
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "PersistentVolume", name, string(domainaccess.ActionView), "success", "viewed persistentvolume detail")
	return item, nil
}
func (s *Service) ListStorageClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.StorageClassView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "StorageClass", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.StorageClassView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListStorageClasses(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectStorageClasses(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.StorageClassView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapStorageClass(item, decision))
		}
		source = "live"
	}
	populateAllowedActionsStorageClasses(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "StorageClass", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed storageclasses via %s", source))
	return items, nil
}
func (s *Service) GetStorageClassDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.StorageClassDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, "", "StorageClass", domainaccess.ActionView)
	if err != nil {
		return domainresource.StorageClassDetailView{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		return domainresource.StorageClassDetailView{}, unsupportedAgentOperation("storageclass detail is not supported for agent-connected clusters yet")
	}
	rawItem, err := s.getDirectStorageClass(ctx, clusterID, name)
	if err != nil {
		return domainresource.StorageClassDetailView{}, err
	}
	item := mapStorageClassDetail(*rawItem, decision)
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, "", "StorageClass", name, string(domainaccess.ActionView), "success", "viewed storageclass detail")
	return item, nil
}
func (s *Service) listDirectPersistentVolumeClaims(ctx context.Context, clusterID, namespace string) ([]corev1.PersistentVolumeClaim, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.CoreV1().PersistentVolumeClaims(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) getDirectPersistentVolumeClaim(ctx context.Context, clusterID, namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().PersistentVolumeClaims(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) listDirectPersistentVolumes(ctx context.Context, clusterID string) ([]corev1.PersistentVolume, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.CoreV1().PersistentVolumes().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) getDirectPersistentVolume(ctx context.Context, clusterID, name string) (*corev1.PersistentVolume, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().PersistentVolumes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) listDirectStorageClasses(ctx context.Context, clusterID string) ([]storagev1.StorageClass, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.StorageV1().StorageClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) getDirectStorageClass(ctx context.Context, clusterID, name string) (*storagev1.StorageClass, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.StorageV1().StorageClasses().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func mapPersistentVolumeClaim(item corev1.PersistentVolumeClaim, decision domainaccess.Decision) domainresource.PersistentVolumeClaimView {
	requested := ""
	if quantity, ok := item.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		requested = quantity.String()
	}
	accessModes := make([]string, 0, len(item.Spec.AccessModes))
	for _, mode := range item.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}
	storageClass := ""
	if item.Spec.StorageClassName != nil {
		storageClass = *item.Spec.StorageClassName
	}
	return domainresource.PersistentVolumeClaimView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Status:         string(item.Status.Phase),
		VolumeName:     item.Spec.VolumeName,
		StorageClass:   storageClass,
		AccessModes:    accessModes,
		Requested:      requested,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}
func mapPersistentVolumeClaimDetail(item corev1.PersistentVolumeClaim, decision domainaccess.Decision) domainresource.PersistentVolumeClaimDetailView {
	requested := ""
	if quantity, ok := item.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		requested = quantity.String()
	}
	capacity := ""
	if quantity, ok := item.Status.Capacity[corev1.ResourceStorage]; ok {
		capacity = quantity.String()
	}
	accessModes := make([]string, 0, len(item.Spec.AccessModes))
	for _, mode := range item.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}
	storageClass := ""
	if item.Spec.StorageClassName != nil {
		storageClass = *item.Spec.StorageClassName
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	return domainresource.PersistentVolumeClaimDetailView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Status:         string(item.Status.Phase),
		VolumeName:     item.Spec.VolumeName,
		StorageClass:   storageClass,
		AccessModes:    accessModes,
		Requested:      requested,
		VolumeMode:     volumeMode,
		Capacity:       capacity,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}
func mapPersistentVolume(item corev1.PersistentVolume, decision domainaccess.Decision) domainresource.PersistentVolumeView {
	capacity := ""
	if quantity, ok := item.Spec.Capacity[corev1.ResourceStorage]; ok {
		capacity = quantity.String()
	}
	accessModes := make([]string, 0, len(item.Spec.AccessModes))
	for _, mode := range item.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}
	claimRef := ""
	if item.Spec.ClaimRef != nil {
		claimRef = fmt.Sprintf("%s/%s", item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name)
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	return domainresource.PersistentVolumeView{
		Name:           item.Name,
		Status:         string(item.Status.Phase),
		StorageClass:   item.Spec.StorageClassName,
		ClaimRef:       claimRef,
		AccessModes:    accessModes,
		Capacity:       capacity,
		ReclaimPolicy:  string(item.Spec.PersistentVolumeReclaimPolicy),
		VolumeMode:     volumeMode,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}
func mapPersistentVolumeDetail(item corev1.PersistentVolume, decision domainaccess.Decision) domainresource.PersistentVolumeDetailView {
	capacity := ""
	if quantity, ok := item.Spec.Capacity[corev1.ResourceStorage]; ok {
		capacity = quantity.String()
	}
	accessModes := make([]string, 0, len(item.Spec.AccessModes))
	for _, mode := range item.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}
	claimRef := ""
	if item.Spec.ClaimRef != nil {
		claimRef = fmt.Sprintf("%s/%s", item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name)
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	return domainresource.PersistentVolumeDetailView{
		Name:           item.Name,
		Status:         string(item.Status.Phase),
		StorageClass:   item.Spec.StorageClassName,
		ClaimRef:       claimRef,
		AccessModes:    accessModes,
		Capacity:       capacity,
		ReclaimPolicy:  string(item.Spec.PersistentVolumeReclaimPolicy),
		VolumeMode:     volumeMode,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}
func mapStorageClass(item storagev1.StorageClass, decision domainaccess.Decision) domainresource.StorageClassView {
	reclaimPolicy := ""
	if item.ReclaimPolicy != nil {
		reclaimPolicy = string(*item.ReclaimPolicy)
	}
	volumeBindingMode := ""
	if item.VolumeBindingMode != nil {
		volumeBindingMode = string(*item.VolumeBindingMode)
	}
	allowVolumeExpansion := false
	if item.AllowVolumeExpansion != nil {
		allowVolumeExpansion = *item.AllowVolumeExpansion
	}
	return domainresource.StorageClassView{
		Name:                 item.Name,
		Provisioner:          item.Provisioner,
		ReclaimPolicy:        reclaimPolicy,
		VolumeBindingMode:    volumeBindingMode,
		AllowVolumeExpansion: allowVolumeExpansion,
		Parameters:           item.Parameters,
		AgeSeconds:           secondsSince(item.CreationTimestamp.Time),
		AllowedActions:       stringifyActions(decision.AllowedActions),
	}
}
func mapStorageClassDetail(item storagev1.StorageClass, decision domainaccess.Decision) domainresource.StorageClassDetailView {
	reclaimPolicy := ""
	if item.ReclaimPolicy != nil {
		reclaimPolicy = string(*item.ReclaimPolicy)
	}
	volumeBindingMode := ""
	if item.VolumeBindingMode != nil {
		volumeBindingMode = string(*item.VolumeBindingMode)
	}
	allowVolumeExpansion := false
	if item.AllowVolumeExpansion != nil {
		allowVolumeExpansion = *item.AllowVolumeExpansion
	}
	return domainresource.StorageClassDetailView{
		Name:                 item.Name,
		Provisioner:          item.Provisioner,
		ReclaimPolicy:        reclaimPolicy,
		VolumeBindingMode:    volumeBindingMode,
		AllowVolumeExpansion: allowVolumeExpansion,
		Parameters:           item.Parameters,
		Labels:               item.Labels,
		Annotations:          item.Annotations,
		CreatedAt:            item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:           secondsSince(item.CreationTimestamp.Time),
		AllowedActions:       stringifyActions(decision.AllowedActions),
	}
}
func populateAllowedActionsPersistentVolumeClaims(items []domainresource.PersistentVolumeClaimView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsPersistentVolumes(items []domainresource.PersistentVolumeView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsStorageClasses(items []domainresource.StorageClassView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
