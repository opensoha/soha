package resource

import (
	"context"
	"fmt"
	"strings"

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
	request := directDetailRequest{clusterID: clusterID, namespace: namespace, kind: "PersistentVolumeClaim", name: name, unsupported: "persistentvolumeclaim detail is not supported for agent-connected clusters yet", summary: "viewed persistentvolumeclaim detail"}
	return getDirectDetail(ctx, s.resourceAccess, principal, request, s.directStorage,
		func(direct DirectStorageReader) (domainresource.PersistentVolumeClaimDetailView, error) {
			return direct.GetPersistentVolumeClaimDetail(ctx, clusterID, namespace, name)
		},
		func(item *domainresource.PersistentVolumeClaimDetailView, actions []string) {
			item.AllowedActions = actions
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
	request := directDetailRequest{clusterID: clusterID, kind: "PersistentVolume", name: name, unsupported: "persistentvolume detail is not supported for agent-connected clusters yet", summary: "viewed persistentvolume detail"}
	return getDirectDetail(ctx, s.resourceAccess, principal, request, s.directStorage,
		func(direct DirectStorageReader) (domainresource.PersistentVolumeDetailView, error) {
			return direct.GetPersistentVolumeDetail(ctx, clusterID, name)
		},
		func(item *domainresource.PersistentVolumeDetailView, actions []string) { item.AllowedActions = actions },
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
	request := directDetailRequest{clusterID: clusterID, kind: "StorageClass", name: name, unsupported: "storageclass detail is not supported for agent-connected clusters yet", summary: "viewed storageclass detail"}
	return getDirectDetail(ctx, s.resourceAccess, principal, request, s.directStorage,
		func(direct DirectStorageReader) (domainresource.StorageClassDetailView, error) {
			return direct.GetStorageClassDetail(ctx, clusterID, name)
		},
		func(item *domainresource.StorageClassDetailView, actions []string) { item.AllowedActions = actions },
	)
}

func (s *Storage) directStorage() (DirectStorageReader, error) {
	return requireDirect(s.direct, s.direct != nil, "storage reader")
}
