package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/sync/errgroup"
)

type importSourceYAMLReader interface {
	GetApplicationSourceYAML(context.Context, domainidentity.Principal, domainresource.ResourceSourceSnapshot) (domainresource.ResourceYAMLView, error)
}

func (s *Service) verifyImportedRuntimeIdentity(ctx context.Context, principal domainidentity.Principal, workload domaindelivery.ApplicationRuntimeWorkload) error {
	if workload.Source == nil {
		return nil
	}
	reader, ok := s.targets.(KubernetesImportSourceReader)
	if !ok {
		return fmt.Errorf("%w: stable resource reads unavailable", apperrors.ErrUnsupportedOperation)
	}
	current, err := reader.GetImportSource(ctx, principal, workload.ClusterID, workload.Namespace, workload.WorkloadKind, workload.WorkloadName, workload.Source.CRDName)
	if err != nil {
		return err
	}
	if current.Resource.UID != workload.Source.Resource.UID || current.Resource.APIVersion != workload.Source.Resource.APIVersion || current.Resource.Kind != workload.Source.Resource.Kind {
		return fmt.Errorf("%w: linked workload was replaced during observation", apperrors.ErrConflict)
	}
	return nil
}

func importedTargetSource(target domaincatalog.ReleaseTarget) (domainresource.ResourceSourceSnapshot, error) {
	var source domainresource.ResourceSourceSnapshot
	err := json.Unmarshal([]byte(metadataString(target.Metadata, "resourceSource")), &source)
	if err != nil || source.Resource.UID == "" || source.Resource.ClusterID != target.ClusterID || source.Resource.Namespace != target.Namespace || source.Resource.Name != target.WorkloadName {
		return source, fmt.Errorf("%w: linked source requires identity review", apperrors.ErrConflict)
	}
	if (target.WorkloadKind == "CustomResource") != (source.CRDName != "") || (source.CRDName == "" && target.WorkloadKind != source.Resource.Kind) {
		return source, fmt.Errorf("%w: linked source kind does not match", apperrors.ErrConflict)
	}
	return source, nil
}

func (s *Service) GetImportedTargetSource(ctx context.Context, principal domainidentity.Principal, bindingID, targetID string) (domainresource.ResourceYAMLView, error) {
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, bindingID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if _, err := s.applications.Get(ctx, principal, binding.ApplicationID); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	reader, ok := s.targets.(importSourceYAMLReader)
	if !ok {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: source YAML reader unavailable", apperrors.ErrUnsupportedOperation)
	}
	for _, target := range binding.Targets {
		if target.ID != targetID || metadataString(target.Metadata, "importSource") != "kubernetes" {
			continue
		}
		source, err := importedTargetSource(target)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		return reader.GetApplicationSourceYAML(ctx, principal, source)
	}
	return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: linked target not found", apperrors.ErrNotFound)
}

func (s *Service) importedRuntimeWorkload(ctx context.Context, principal domainidentity.Principal, bindingID string, target domaincatalog.ReleaseTarget) domaindelivery.ApplicationRuntimeWorkload {
	item := domaindelivery.ApplicationRuntimeWorkload{
		ApplicationEnvironmentID: bindingID, TargetID: target.ID, ClusterID: target.ClusterID, Namespace: target.Namespace,
		WorkloadKind: target.WorkloadKind, WorkloadName: target.WorkloadName, ServiceID: metadataString(target.Metadata, "serviceId"),
		SourceStatus: "unavailable", HealthStatus: runtimeHealthUnknown,
	}
	source, err := importedTargetSource(target)
	if err != nil {
		return item
	}
	item.Source = &source
	reader, ok := s.targets.(KubernetesImportSourceReader)
	if !ok {
		return item
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	current, err := reader.GetImportSource(ctx, principal, target.ClusterID, target.Namespace, target.WorkloadKind, target.WorkloadName, source.CRDName)
	switch {
	case errors.Is(err, apperrors.ErrAccessDenied):
		item.SourceStatus = "forbidden"
	case errors.Is(err, apperrors.ErrNotFound):
		item.SourceStatus = "missing"
	case err != nil:
		item.SourceStatus = "unavailable"
	case current.Resource.UID != source.Resource.UID || current.Resource.APIVersion != source.Resource.APIVersion || current.Resource.Kind != source.Resource.Kind:
		item.SourceStatus = "replaced"
	case current.SpecDigest != source.SpecDigest:
		item.SourceStatus = "changed"
	default:
		item.SourceStatus = "linked"
	}
	if (item.SourceStatus == "linked" || item.SourceStatus == "changed") && current.Replicas != nil {
		item.DesiredReplicas, item.ReadyReplicas = current.Replicas.Desired, current.Replicas.Ready
		item.UpdatedReplicas, item.AvailableReplicas = current.Replicas.Updated, current.Replicas.Available
		item.NativeObservationAvailable = true
	}
	return item
}

func (s *Service) importedRuntimeWorkloads(ctx context.Context, principal domainidentity.Principal, binding domaincatalog.ApplicationEnvironment) map[string]domaindelivery.ApplicationRuntimeWorkload {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	items := make(map[string]domaindelivery.ApplicationRuntimeWorkload)
	var mu sync.Mutex
	var group errgroup.Group
	group.SetLimit(4)
	for _, target := range binding.Targets {
		if metadataString(target.Metadata, "importSource") != "kubernetes" {
			continue
		}
		group.Go(func() error {
			item := s.importedRuntimeWorkload(ctx, principal, binding.ID, target)
			mu.Lock()
			items[target.ID] = item
			mu.Unlock()
			return nil
		})
	}
	_ = group.Wait()
	return items
}

func selectRuntimeWorkload(items []domaindelivery.ApplicationRuntimeWorkload, name string) (*domaindelivery.ApplicationRuntimeWorkload, error) {
	var selected *domaindelivery.ApplicationRuntimeWorkload
	for _, item := range items {
		if item.WorkloadName != name {
			continue
		}
		if selected != nil {
			if !sameRuntimeResourceIdentity(*selected, item) {
				return nil, fmt.Errorf("%w: workload name is ambiguous; use the target-scoped source view", apperrors.ErrConflict)
			}
			// A Helm release and a native import may observe the same physical object.
			if item.SourceStatus != "" {
				copyItem := item
				selected = &copyItem
			}
			continue
		}
		selected = &item
	}
	if selected == nil {
		return nil, fmt.Errorf("%w: workload not found", apperrors.ErrNotFound)
	}
	return selected, nil
}

func sameRuntimeResourceIdentity(left, right domaindelivery.ApplicationRuntimeWorkload) bool {
	if left.Source == nil || right.Source == nil {
		return false
	}
	a, b := left.Source.Resource, right.Source.Resource
	return a.UID != "" && a.UID == b.UID && a.ClusterID == b.ClusterID && a.Namespace == b.Namespace &&
		a.APIVersion == b.APIVersion && a.Kind == b.Kind && a.Name == b.Name
}
