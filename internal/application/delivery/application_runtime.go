package delivery

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/sync/errgroup"
)

const (
	runtimeEnvironmentAvailable   = "available"
	runtimeEnvironmentUnavailable = "unavailable"
	runtimeHealthHealthy          = "healthy"
	runtimeHealthProgressing      = "progressing"
	runtimeHealthUnhealthy        = "unhealthy"
	runtimeHealthUnknown          = "unknown"
)

func (s *Service) ListApplicationEnvironmentNavigation(ctx context.Context, principal domainidentity.Principal, applicationID string) ([]domaindelivery.ApplicationEnvironmentNavigation, error) {
	app, err := s.applications.Get(ctx, principal, strings.TrimSpace(applicationID))
	if err != nil {
		return nil, err
	}
	rows, err := s.catalog.ListEnvironmentNavigation(ctx, principal, app.ID)
	if err != nil {
		return nil, err
	}
	items := make([]domaindelivery.ApplicationEnvironmentNavigation, 0, len(rows))
	for _, row := range rows {
		binding, environment := row.Binding, row.Environment
		items = append(items, domaindelivery.ApplicationEnvironmentNavigation{
			ApplicationEnvironmentID: binding.ID, EnvironmentID: binding.EnvironmentID,
			EnvironmentName: environment.Name, EnvironmentKey: binding.EnvironmentKey,
			IsProduction: environment.IsProduction, RequiresApproval: requiresApproval(binding, environment),
			ActionKind: actionKindForBinding(binding, environment), ClusterID: binding.ClusterID, Namespace: binding.Namespace,
		})
	}
	return items, nil
}

func (s *Service) GetApplicationEnvironmentRuntimeSummary(ctx context.Context, principal domainidentity.Principal, applicationID, bindingID string) (domaindelivery.ApplicationEnvironmentRuntimeSummary, error) {
	app, err := s.applications.Get(ctx, principal, strings.TrimSpace(applicationID))
	if err != nil {
		return domaindelivery.ApplicationEnvironmentRuntimeSummary{}, err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, strings.TrimSpace(bindingID))
	if err != nil {
		return domaindelivery.ApplicationEnvironmentRuntimeSummary{}, err
	}
	if binding.ApplicationID != app.ID {
		return domaindelivery.ApplicationEnvironmentRuntimeSummary{}, fmt.Errorf("%w: application environment not found", apperrors.ErrNotFound)
	}
	services, err := s.applications.ListServiceIdentities(ctx, principal, app.ID)
	if err != nil {
		return domaindelivery.ApplicationEnvironmentRuntimeSummary{}, err
	}
	environment, err := s.catalog.GetEnvironment(ctx, principal, binding.EnvironmentID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return domaindelivery.ApplicationEnvironmentRuntimeSummary{}, err
	}

	observeCtx, cancel := context.WithTimeout(ctx, 2400*time.Millisecond)
	defer cancel()
	observeCtx = s.withDeliveryObservation(observeCtx)
	// Leave time to observe healthy live targets even when a manifest snapshot stalls.
	manifestCtx, stopManifest := context.WithTimeout(observeCtx, 600*time.Millisecond)
	runtimeBinding, deployments, manifestErr := s.resolveManifestRuntimeBinding(manifestCtx, principal, binding, true)
	manifestErr = errors.Join(manifestErr, manifestCtx.Err())
	stopManifest()
	workloads, workloadsErr := s.observeScopedRuntimeWorkloads(observeCtx, principal, app, runtimeBinding)
	if err := ctx.Err(); err != nil {
		return domaindelivery.ApplicationEnvironmentRuntimeSummary{}, err
	}
	status := runtimeObservationStatus(workloads, len(deployments), len(binding.Targets), errors.Join(manifestErr, workloadsErr, observeCtx.Err()))
	for i := range deployments {
		deployments[i].Spec.DeliverySnapshot = nil
	}
	for i := range workloads {
		if service := runtimeServiceForWorkload(workloads[i], services); service != nil {
			workloads[i].ServiceID, workloads[i].ServiceKey = service.ID, service.Key
		}
		workloads[i].HealthStatus = runtimeWorkloadHealth(workloads[i])
		workloads[i].BuildSource = nil
	}
	result := domaindelivery.ApplicationRuntimeEnvironment{
		ApplicationEnvironmentID: binding.ID, EnvironmentID: binding.EnvironmentID,
		EnvironmentName: environment.Name, EnvironmentKey: binding.EnvironmentKey,
		ActionKind: actionKindForBinding(binding, environment), RequiresApproval: requiresApproval(binding, environment),
		Status: runtimeEnvironmentAvailable, ResourceSelector: binding.ResourceSelector, Targets: binding.Targets,
		Workloads: workloads, ManifestDeployments: deployments,
	}
	if status != runtimeEnvironmentAvailable && status != "partial" && status != "unconfigured" {
		result.Status = runtimeEnvironmentUnavailable
	}
	return domaindelivery.ApplicationEnvironmentRuntimeSummary{
		Environment: result, ObservationStatus: status, ObservedAt: time.Now().UTC(),
	}, nil
}

func (s *Service) withDeliveryObservation(ctx context.Context) context.Context {
	if observer, ok := s.targets.(interface {
		WithDeliveryObservation(context.Context) context.Context
	}); ok {
		return observer.WithDeliveryObservation(ctx)
	}
	return ctx
}

func (s *Service) observeScopedRuntimeWorkloads(ctx context.Context, principal domainidentity.Principal, app domainapp.App, binding domaincatalog.ApplicationEnvironment) ([]domaindelivery.ApplicationRuntimeWorkload, error) {
	groups := groupRuntimeTargets(binding.Targets)
	type observation struct {
		workloads []domaindelivery.ApplicationRuntimeWorkload
		err       error
	}
	observations := make([]observation, len(groups))
	var workers errgroup.Group
	workers.SetLimit(6)
	for i, targets := range groups {
		workers.Go(func() error {
			targetCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			groupBinding := binding
			groupBinding.Targets = targets
			items, err := s.collectRuntimeWorkloadsForBinding(targetCtx, principal, app, groupBinding, nil, nil, nil, nil, nil, true)
			observations[i] = observation{items, errors.Join(err, targetCtx.Err())}
			return nil
		})
	}
	_ = workers.Wait()
	var workloads []domaindelivery.ApplicationRuntimeWorkload
	var joinedErr error
	for _, item := range observations {
		workloads = append(workloads, item.workloads...)
		joinedErr = errors.Join(joinedErr, item.err)
	}
	return workloads, joinedErr
}

func groupRuntimeTargets(targets []domaincatalog.ReleaseTarget) [][]domaincatalog.ReleaseTarget {
	var groups [][]domaincatalog.ReleaseTarget
	namespaces := make(map[[2]string]int)
	for _, target := range targets {
		source := metadataString(target.Metadata, "importSource")
		if source == "kubernetes" || source == "helm" || target.Helm != nil {
			groups = append(groups, []domaincatalog.ReleaseTarget{target})
			continue
		}
		if !target.Enabled {
			continue
		}
		key := [2]string{strings.TrimSpace(target.ClusterID), strings.TrimSpace(target.Namespace)}
		index, exists := namespaces[key]
		if !exists {
			index = len(groups)
			namespaces[key] = index
			groups = append(groups, nil)
		}
		groups[index] = append(groups[index], target)
	}
	for _, targets := range groups {
		slices.SortStableFunc(targets, func(left, right domaincatalog.ReleaseTarget) int {
			leftWildcard := strings.TrimSpace(left.WorkloadName) == ""
			rightWildcard := strings.TrimSpace(right.WorkloadName) == ""
			switch {
			case leftWildcard == rightWildcard:
				return 0
			case leftWildcard:
				return 1
			default:
				return -1
			}
		})
	}
	return groups
}

func runtimeObservationStatus(workloads []domaindelivery.ApplicationRuntimeWorkload, deploymentCount, targetCount int, err error) string {
	observed := false
	failure := ""
	for _, workload := range workloads {
		switch workload.SourceStatus {
		case "forbidden":
			failure = "forbidden"
		case "replaced", "missing":
			failure = "identity_mismatch"
		case "unavailable":
			failure = runtimeEnvironmentUnavailable
		default:
			observed = true
		}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		failure = "timeout"
	case errors.Is(err, apperrors.ErrAccessDenied), errors.Is(err, apperrors.ErrUnauthorized):
		failure = "forbidden"
	case errors.Is(err, apperrors.ErrNotFound), errors.Is(err, apperrors.ErrConflict):
		failure = "identity_mismatch"
	case errors.Is(err, apperrors.ErrClusterUnready):
		failure = "cluster_unready"
	case err != nil:
		failure = runtimeEnvironmentUnavailable
	}
	if failure != "" {
		if observed {
			return "partial"
		}
		return failure
	}
	if !observed && deploymentCount == 0 {
		if targetCount == 0 {
			return "unconfigured"
		}
		return runtimeEnvironmentUnavailable
	}
	return runtimeEnvironmentAvailable
}

func (s *Service) enrichApplicationRuntimeDetail(
	ctx context.Context,
	principal domainidentity.Principal,
	detail *domaindelivery.ApplicationRuntimeDetail,
	bundles []domaindelivery.ReleaseBundle,
	tasks []domaindelivery.ExecutionTask,
) error {
	services, err := s.applications.ListServices(ctx, principal, detail.Application.ID)
	if err != nil {
		return fmt.Errorf("list application services: %w", err)
	}
	detail.Services = services
	detail.LatestBundle = latestBundleForApplication(detail.Application.ID, bundles)
	detail.LatestExecutionTask = latestExecutionTaskForApplication(detail.Application.ID, tasks)

	summary := applicationRuntimeSummaryFor(detail.Environments, services)
	if detail.LatestBundle != nil {
		summary.LatestVersion = detail.LatestBundle.Version
	}
	if detail.LatestExecutionTask != nil {
		summary.LatestExecutionStatus = detail.LatestExecutionTask.Status
	}
	detail.Summary = summary
	return nil
}

func applicationRuntimeSummaryFor(environments []domaindelivery.ApplicationRuntimeEnvironment, services []domainapp.Service) domaindelivery.ApplicationRuntimeSummary {
	summary := domaindelivery.ApplicationRuntimeSummary{
		ServiceCount:     len(services),
		EnvironmentCount: len(environments),
		HealthStatus:     runtimeHealthUnknown,
	}
	identities := make([]domainapp.ServiceIdentity, len(services))
	for i, service := range services {
		identities[i] = domainapp.ServiceIdentity{ID: service.ID, Key: service.Key, Name: service.Name}
	}
	for environmentIndex := range environments {
		if environments[environmentIndex].Status == runtimeEnvironmentUnavailable {
			summary.HealthStatus = runtimeHealthUnhealthy
		}
		for workloadIndex := range environments[environmentIndex].Workloads {
			workload := &environments[environmentIndex].Workloads[workloadIndex]
			service := runtimeServiceForWorkload(*workload, identities)
			if service != nil {
				workload.ServiceID = service.ID
				workload.ServiceKey = service.Key
			}
			workload.HealthStatus = runtimeWorkloadHealth(*workload)
			summary.WorkloadCount++
			switch workload.HealthStatus {
			case runtimeHealthHealthy:
				summary.HealthyWorkloadCount++
			case runtimeHealthProgressing:
				summary.ProgressingWorkloads++
			case runtimeHealthUnhealthy:
				summary.UnhealthyWorkloads++
			}
		}
	}
	switch {
	case summary.HealthStatus == runtimeHealthUnhealthy || summary.UnhealthyWorkloads > 0:
		summary.HealthStatus = runtimeHealthUnhealthy
	case summary.ProgressingWorkloads > 0:
		summary.HealthStatus = runtimeHealthProgressing
	case summary.WorkloadCount > 0 && summary.HealthyWorkloadCount == summary.WorkloadCount:
		summary.HealthStatus = runtimeHealthHealthy
	}
	return summary
}

func runtimeWorkloadHealth(workload domaindelivery.ApplicationRuntimeWorkload) string {
	if (workload.SourceStatus != "" && !workload.NativeObservationAvailable) || workload.WorkloadKind == "CustomResource" || workload.WorkloadKind == "Job" || workload.WorkloadKind == "CronJob" {
		return runtimeHealthUnknown
	}
	if workload.DesiredReplicas == 0 {
		return runtimeHealthHealthy
	}
	if workload.ReadyReplicas == workload.DesiredReplicas &&
		workload.UpdatedReplicas == workload.DesiredReplicas &&
		workload.AvailableReplicas == workload.DesiredReplicas {
		return runtimeHealthHealthy
	}
	if workload.ReadyReplicas > 0 || workload.UpdatedReplicas > 0 || workload.AvailableReplicas > 0 {
		return runtimeHealthProgressing
	}
	return runtimeHealthUnhealthy
}

func runtimeServiceForWorkload(workload domaindelivery.ApplicationRuntimeWorkload, services []domainapp.ServiceIdentity) *domainapp.ServiceIdentity {
	if workload.ServiceID != "" {
		for _, service := range services {
			if service.ID == workload.ServiceID {
				return &service
			}
		}
		return nil
	}
	candidates := []string{
		workload.Labels["serviceId"],
		workload.Labels["service"],
		workload.Labels["serviceKey"],
		workload.Labels["app.kubernetes.io/name"],
		workload.Labels["app"],
		workload.WorkloadName,
	}
	for _, service := range services {
		for _, candidate := range candidates {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" && (candidate == service.ID || candidate == service.Key || candidate == service.Name) {
				return &service
			}
		}
	}
	return nil
}
