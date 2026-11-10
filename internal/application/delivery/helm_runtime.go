package delivery

import (
	"context"
	"errors"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type helmWorkloadReader interface {
	GetHelmReleaseWorkloadSources(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.ResourceSourceSnapshot, error)
}

func (s *Service) helmRuntimeWorkloads(ctx context.Context, principal domainidentity.Principal, bindingID string, target domaincatalog.ReleaseTarget) []domaindelivery.ApplicationRuntimeWorkload {
	root := domaindelivery.ApplicationRuntimeWorkload{
		ApplicationEnvironmentID: bindingID, TargetID: target.ID, ClusterID: target.ClusterID, Namespace: target.Namespace,
		WorkloadKind: "HelmRelease", WorkloadName: target.WorkloadName, ServiceID: metadataString(target.Metadata, "serviceId"),
		SourceStatus: "unavailable", HealthStatus: runtimeHealthUnknown,
	}
	releaseName := target.WorkloadName
	if target.Helm != nil {
		releaseName = target.Helm.ReleaseName
	}
	root.WorkloadName = releaseName
	reader, ok := s.targets.(helmWorkloadReader)
	if !ok {
		return []domaindelivery.ApplicationRuntimeWorkload{root}
	}
	sources, err := reader.GetHelmReleaseWorkloadSources(ctx, principal, target.ClusterID, target.Namespace, releaseName)
	if err != nil {
		if errors.Is(err, apperrors.ErrAccessDenied) {
			root.SourceStatus = "forbidden"
		}
		if errors.Is(err, apperrors.ErrNotFound) {
			root.SourceStatus = "missing"
		}
		return []domaindelivery.ApplicationRuntimeWorkload{root}
	}
	if len(sources) == 0 {
		root.SourceStatus = "linked"
		return []domaindelivery.ApplicationRuntimeWorkload{root}
	}
	var result []domaindelivery.ApplicationRuntimeWorkload
	for _, source := range sources {
		if !validHelmRuntimeSource(source, target) {
			continue
		}
		item := root
		item.WorkloadKind, item.WorkloadName = source.Resource.Kind, source.Resource.Name
		item.Source = &source
		if source.Replicas != nil {
			item.SourceStatus = ""
			item.DesiredReplicas, item.ReadyReplicas = source.Replicas.Desired, source.Replicas.Ready
			item.UpdatedReplicas, item.AvailableReplicas = source.Replicas.Updated, source.Replicas.Available
			item.NativeObservationAvailable = true
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return []domaindelivery.ApplicationRuntimeWorkload{root}
	}
	return result
}

func validHelmRuntimeSource(source domainresource.ResourceSourceSnapshot, target domaincatalog.ReleaseTarget) bool {
	r := source.Resource
	return r.UID != "" && r.Name != "" && r.ClusterID == target.ClusterID && r.Namespace == target.Namespace &&
		r.APIVersion == "apps/v1" && (r.Kind == "Deployment" || r.Kind == "StatefulSet" || r.Kind == "DaemonSet")
}
