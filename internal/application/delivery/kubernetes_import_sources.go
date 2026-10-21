package delivery

import (
	"context"
	"fmt"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/sync/errgroup"
)

type importApplicationAuthorizer interface {
	AuthorizeServiceImport(context.Context, domainidentity.Principal, string, domainapp.UpsertInput, []domainapp.ServiceInput) error
}

type importBindingAuthorizer interface {
	AuthorizeImportBinding(context.Context, domainidentity.Principal, string, string, string) error
}

func (s *Service) authorizeKubernetesImportDestination(ctx context.Context, principal domainidentity.Principal, input domaindelivery.KubernetesServiceImportInput) (domaindelivery.ImportDestination, error) {
	destination, err := s.importRepository.ResolveImportDestination(ctx, input.ApplicationKey, input.EnvironmentKey)
	if err != nil {
		return destination, err
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationServicesManage, "create")); err != nil {
		return destination, err
	}
	if destination.ApplicationID == "" {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsCreate); err != nil {
			return destination, err
		}
	}
	apps, appOK := s.applications.(importApplicationAuthorizer)
	bindings, bindingOK := s.catalog.(importBindingAuthorizer)
	if !appOK || !bindingOK {
		return destination, fmt.Errorf("%w: import scope authorization is unavailable", apperrors.ErrUnsupportedOperation)
	}
	services := make([]domainapp.ServiceInput, 0, len(input.Workloads))
	for _, workload := range input.Workloads {
		services = append(services, domainapp.ServiceInput{Key: domaindelivery.KubernetesImportServiceKey(input.ClusterID, input.Namespace, workload), Name: workload.WorkloadName})
	}
	err = apps.AuthorizeServiceImport(ctx, principal, destination.ApplicationID, domainapp.UpsertInput{Key: input.ApplicationKey, Name: input.ApplicationName}, services)
	if err == nil {
		err = bindings.AuthorizeImportBinding(ctx, principal, destination.ApplicationID, input.EnvironmentKey, destination.BindingID)
	}
	return destination, err
}

func (s *Service) populateImportSources(ctx context.Context, principal domainidentity.Principal, items []domaindelivery.KubernetesImportCandidate) error {
	reader, ok := s.targets.(KubernetesImportSourceReader)
	if !ok {
		return fmt.Errorf("%w: stable source reads are unavailable", apperrors.ErrUnsupportedOperation)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	group, queryCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for index := range items {
		group.Go(func() error {
			item := &items[index]
			crdName := ""
			if item.Source != nil {
				crdName = item.Source.CRDName
			}
			source, err := reader.GetImportSource(queryCtx, principal, item.ClusterID, item.Namespace, item.WorkloadKind, item.WorkloadName, crdName)
			if err != nil {
				return err
			}
			item.Source = &source
			if item.WorkloadKind == "Deployment" {
				if detail, detailErr := s.targets.GetDeploymentDetail(queryCtx, principal, item.ClusterID, item.Namespace, item.WorkloadName); detailErr == nil {
					for _, container := range detail.Containers {
						item.Containers = append(item.Containers, container.Name)
					}
				}
			}
			return nil
		})
	}
	return group.Wait()
}

func (s *Service) resolveSelectedImportSources(ctx context.Context, principal domainidentity.Principal, input *domaindelivery.KubernetesServiceImportInput) error {
	reader, ok := s.targets.(KubernetesImportSourceReader)
	if !ok {
		return fmt.Errorf("%w: stable source reads are unavailable", apperrors.ErrUnsupportedOperation)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for index := range input.Workloads {
		workload := &input.Workloads[index]
		source, err := reader.GetImportSource(ctx, principal, input.ClusterID, input.Namespace, workload.WorkloadKind, workload.WorkloadName, workload.CRDName)
		if err != nil {
			return err
		}
		if workload.ExpectedUID != "" && (workload.ExpectedUID != source.Resource.UID || workload.ExpectedSpecDigest != source.SpecDigest) {
			return fmt.Errorf("%w: workload changed since preview; select it again", apperrors.ErrConflict)
		}
		if hasCronJobOwner(&source) && workload.WorkloadKind == "Job" {
			return fmt.Errorf("%w: import the owning CronJob instead of its generated Job", apperrors.ErrInvalidArgument)
		}
		if input.OwnershipMode == managedOwnershipMode {
			if len(source.Owners) != 0 || source.ExternallyManaged {
				return fmt.Errorf("%w: controller-owned workloads require an explicit handoff", apperrors.ErrConflict)
			}
			if err := reader.AuthorizeImportManagement(ctx, principal, input.ClusterID, input.Namespace, workload.WorkloadName); err != nil {
				return err
			}
		}
		workload.Source = &source
	}
	// shortcut: enable managed import only after release snapshots and runners enforce source UIDs.
	if input.OwnershipMode == managedOwnershipMode {
		return fmt.Errorf("%w: managed import requires a separate UID-aware handoff; use observe_only", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

func hasCronJobOwner(source *domainresource.ResourceSourceSnapshot) bool {
	if source == nil {
		return false
	}
	for _, owner := range source.Owners {
		if owner.Kind == "CronJob" && owner.APIVersion == "batch/v1" && owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}

func (s *Service) collectImportKind(ctx context.Context, principal domainidentity.Principal, filter domaindelivery.KubernetesImportCandidateFilter) ([]domaindelivery.KubernetesImportCandidate, error) {
	if filter.WorkloadKind == "" {
		return s.collectKubernetesImportCandidates(ctx, principal, filter.ClusterID, filter.Namespace)
	}
	items := make([]domaindelivery.KubernetesImportCandidate, 0)
	add := func(name, namespace string, desired, ready int) {
		items = append(items, kubernetesImportCandidate(filter.ClusterID, namespace, filter.WorkloadKind, name, desired, ready, nil))
	}
	switch filter.WorkloadKind {
	case "Deployment":
		rows, err := s.targets.ListDeployments(ctx, principal, filter.ClusterID, filter.Namespace)
		for _, row := range rows {
			add(row.Name, row.Namespace, int(row.DesiredReplicas), int(row.ReadyReplicas))
		}
		return items, err
	case "StatefulSet":
		if s.importTargets == nil {
			break
		}
		rows, err := s.importTargets.ListStatefulSets(ctx, principal, filter.ClusterID, filter.Namespace)
		for _, row := range rows {
			add(row.Name, row.Namespace, int(row.DesiredReplicas), int(row.ReadyReplicas))
		}
		return items, err
	case "DaemonSet":
		if s.importTargets == nil {
			break
		}
		rows, err := s.importTargets.ListDaemonSets(ctx, principal, filter.ClusterID, filter.Namespace)
		for _, row := range rows {
			add(row.Name, row.Namespace, int(row.DesiredNumber), int(row.ReadyNumber))
		}
		return items, err
	default:
		return s.collectExtendedImportKind(ctx, principal, filter)
	}
	return nil, fmt.Errorf("%w: import discovery is unavailable", apperrors.ErrUnsupportedOperation)
}

func (s *Service) collectExtendedImportKind(ctx context.Context, principal domainidentity.Principal, filter domaindelivery.KubernetesImportCandidateFilter) ([]domaindelivery.KubernetesImportCandidate, error) {
	reader, ok := s.targets.(KubernetesImportExtendedReader)
	if !ok {
		return nil, fmt.Errorf("%w: import discovery is unavailable", apperrors.ErrUnsupportedOperation)
	}
	items := make([]domaindelivery.KubernetesImportCandidate, 0)
	add := func(name, namespace string) {
		items = append(items, kubernetesImportCandidate(filter.ClusterID, namespace, filter.WorkloadKind, name, 0, 0, nil))
	}
	switch filter.WorkloadKind {
	case "Job":
		rows, err := reader.ListJobs(ctx, principal, filter.ClusterID, filter.Namespace)
		for _, row := range rows {
			add(row.Name, row.Namespace)
		}
		return items, err
	case "CronJob":
		rows, err := reader.ListCronJobs(ctx, principal, filter.ClusterID, filter.Namespace)
		for _, row := range rows {
			add(row.Name, row.Namespace)
		}
		return items, err
	case "CustomResource":
		rows, err := reader.ListCRDResources(ctx, principal, filter.ClusterID, filter.CRDName, filter.Namespace)
		for _, row := range rows {
			add(row.Name, row.Namespace)
			items[len(items)-1].Source = &domainresource.ResourceSourceSnapshot{CRDName: filter.CRDName}
		}
		return items, err
	}
	return nil, fmt.Errorf("%w: unsupported import kind", apperrors.ErrInvalidArgument)
}
