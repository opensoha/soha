package resource

import (
	"context"
	"fmt"
	"strings"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Storage) ListPersistentVolumeClaims(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PersistentVolumeClaimView, error) {
	request := namespacedListRequest(clusterID, namespace, "PersistentVolumeClaim", "pvc")
	return listRoutedModeResources(ctx, s.resourceAccess, principal, request, s.storageAgentClient, s.directStorage,
		bindNamespacedAgentList(ctx, namespace, StorageAgent.ListPersistentVolumeClaims),
		bindNamespacedDirectList(ctx, clusterID, namespace, DirectStorageReader.ListPersistentVolumeClaims),
		func(item domainresource.PersistentVolumeClaimView) string { return item.Namespace },
		func(item domainresource.PersistentVolumeClaimView) []string { return item.AllowedActions },
		func(item *domainresource.PersistentVolumeClaimView, actions []string) { item.AllowedActions = actions },
	)
}

func (s *Storage) GetPersistentVolumeClaimDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.PersistentVolumeClaimDetailView, error) {
	if strings.TrimSpace(namespace) == "" {
		return domainresource.PersistentVolumeClaimDetailView{}, fmt.Errorf("%w: namespace is required for persistentvolumeclaim detail", apperrors.ErrInvalidArgument)
	}
	request := resourceDetailRequest{clusterID: clusterID, namespace: namespace, kind: "PersistentVolumeClaim", name: name, summary: func(source string) string { return fmt.Sprintf("viewed persistentvolumeclaim detail via %s", source) }}
	return getModeResource(ctx, s.resourceAccess, principal, request,
		func(connection domaincluster.Connection) (domainresource.PersistentVolumeClaimDetailView, string, error) {
			return routeModeValue(connection, s.storageAgentClient, s.directStorage,
				func(client StorageAgent) (domainresource.PersistentVolumeClaimDetailView, error) {
					return client.GetPersistentVolumeClaimDetail(ctx, namespace, name)
				},
				func(direct DirectStorageReader) (domainresource.PersistentVolumeClaimDetailView, error) {
					return direct.GetPersistentVolumeClaimDetail(ctx, clusterID, namespace, name)
				},
			)
		},
		func(item *domainresource.PersistentVolumeClaimDetailView, actions []string) {
			item.AllowedActions = actions
			item.Pods = filterStoragePods(ctx, s.resourceAccess, principal, clusterID, item.Pods)
		},
	)
}

func (s *Storage) ListPersistentVolumes(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.PersistentVolumeView, error) {
	request := resourceListRequest{clusterID: clusterID, kind: "PersistentVolume", summary: func(source string) string { return fmt.Sprintf("listed pv via %s", source) }}
	return listRoutedModeResources(ctx, s.resourceAccess, principal, request, s.storageAgentClient, s.directStorage,
		bindClusterAgentList(ctx, StorageAgent.ListPersistentVolumes),
		bindClusterDirectList(ctx, clusterID, DirectStorageReader.ListPersistentVolumes), nil,
		func(item domainresource.PersistentVolumeView) []string { return item.AllowedActions },
		func(item *domainresource.PersistentVolumeView, actions []string) { item.AllowedActions = actions },
	)
}

func (s *Storage) GetPersistentVolumeDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.PersistentVolumeDetailView, error) {
	request := resourceDetailRequest{clusterID: clusterID, kind: "PersistentVolume", name: name, summary: func(source string) string { return fmt.Sprintf("viewed persistentvolume detail via %s", source) }}
	return getModeResource(ctx, s.resourceAccess, principal, request,
		func(connection domaincluster.Connection) (domainresource.PersistentVolumeDetailView, string, error) {
			return routeModeValue(connection, s.storageAgentClient, s.directStorage,
				func(client StorageAgent) (domainresource.PersistentVolumeDetailView, error) {
					return client.GetPersistentVolumeDetail(ctx, name)
				},
				func(direct DirectStorageReader) (domainresource.PersistentVolumeDetailView, error) {
					return direct.GetPersistentVolumeDetail(ctx, clusterID, name)
				},
			)
		},
		func(item *domainresource.PersistentVolumeDetailView, actions []string) {
			item.AllowedActions = actions
			if item.ClaimName != "" && !canViewRelatedResource(ctx, s.resourceAccess, principal, clusterID, item.ClaimNamespace, "PersistentVolumeClaim") {
				item.ClaimRef, item.ClaimNamespace, item.ClaimName = "", "", ""
			}
		},
	)
}

func (s *Storage) ListStorageClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.StorageClassView, error) {
	request := resourceListRequest{clusterID: clusterID, kind: "StorageClass", summary: func(source string) string { return fmt.Sprintf("listed storageclasses via %s", source) }}
	return listRoutedModeResources(ctx, s.resourceAccess, principal, request, s.storageAgentClient, s.directStorage,
		bindClusterAgentList(ctx, StorageAgent.ListStorageClasses),
		bindClusterDirectList(ctx, clusterID, DirectStorageReader.ListStorageClasses), nil,
		func(item domainresource.StorageClassView) []string { return item.AllowedActions },
		func(item *domainresource.StorageClassView, actions []string) { item.AllowedActions = actions },
	)
}

func (s *Storage) GetStorageClassDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.StorageClassDetailView, error) {
	request := resourceDetailRequest{clusterID: clusterID, kind: "StorageClass", name: name, summary: func(source string) string { return fmt.Sprintf("viewed storageclass detail via %s", source) }}
	return getModeResource(ctx, s.resourceAccess, principal, request,
		func(connection domaincluster.Connection) (domainresource.StorageClassDetailView, string, error) {
			return routeModeValue(connection, s.storageAgentClient, s.directStorage,
				func(client StorageAgent) (domainresource.StorageClassDetailView, error) {
					return client.GetStorageClassDetail(ctx, name)
				},
				func(direct DirectStorageReader) (domainresource.StorageClassDetailView, error) {
					return direct.GetStorageClassDetail(ctx, clusterID, name)
				},
			)
		},
		func(item *domainresource.StorageClassDetailView, actions []string) {
			item.AllowedActions = actions
			item.Volumes = filterStorageVolumes(ctx, s.resourceAccess, principal, clusterID, item.Volumes)
			item.Claims = filterStorageClaims(ctx, s.resourceAccess, principal, clusterID, item.Claims)
		},
	)
}

func filterStoragePods(ctx context.Context, access *resourceAccess, principal domainidentity.Principal, clusterID string, items []domainresource.StoragePodReferenceView) []domainresource.StoragePodReferenceView {
	filtered := make([]domainresource.StoragePodReferenceView, 0, len(items))
	decisions := make(map[string]bool)
	for _, item := range items {
		allowed, ok := decisions[item.Namespace]
		if !ok {
			allowed = canViewRelatedResource(ctx, access, principal, clusterID, item.Namespace, "Pod")
			decisions[item.Namespace] = allowed
		}
		if allowed {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterStorageVolumes(ctx context.Context, access *resourceAccess, principal domainidentity.Principal, clusterID string, items []domainresource.PersistentVolumeView) []domainresource.PersistentVolumeView {
	if !canViewRelatedResource(ctx, access, principal, clusterID, "", "PersistentVolume") {
		return nil
	}
	return items
}

func filterStorageClaims(ctx context.Context, access *resourceAccess, principal domainidentity.Principal, clusterID string, items []domainresource.PersistentVolumeClaimView) []domainresource.PersistentVolumeClaimView {
	filtered := make([]domainresource.PersistentVolumeClaimView, 0, len(items))
	decisions := make(map[string]bool)
	for _, item := range items {
		allowed, ok := decisions[item.Namespace]
		if !ok {
			allowed = canViewRelatedResource(ctx, access, principal, clusterID, item.Namespace, "PersistentVolumeClaim")
			decisions[item.Namespace] = allowed
		}
		if allowed {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (s *Storage) directStorage() (DirectStorageReader, error) {
	return requireDirect(s.direct, s.direct != nil, "storage reader")
}
