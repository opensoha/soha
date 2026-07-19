package delivery

import (
	"context"
	"fmt"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

const (
	runtimeHealthHealthy     = "healthy"
	runtimeHealthProgressing = "progressing"
	runtimeHealthUnhealthy   = "unhealthy"
	runtimeHealthUnknown     = "unknown"
)

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
	for environmentIndex := range environments {
		for workloadIndex := range environments[environmentIndex].Workloads {
			workload := &environments[environmentIndex].Workloads[workloadIndex]
			service := runtimeServiceForWorkload(*workload, services)
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
			default:
				summary.UnhealthyWorkloads++
			}
		}
	}
	switch {
	case summary.UnhealthyWorkloads > 0:
		summary.HealthStatus = runtimeHealthUnhealthy
	case summary.ProgressingWorkloads > 0:
		summary.HealthStatus = runtimeHealthProgressing
	case summary.WorkloadCount > 0:
		summary.HealthStatus = runtimeHealthHealthy
	}
	return summary
}

func runtimeWorkloadHealth(workload domaindelivery.ApplicationRuntimeWorkload) string {
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

func runtimeServiceForWorkload(workload domaindelivery.ApplicationRuntimeWorkload, services []domainapp.Service) *domainapp.Service {
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
				copyService := service
				return &copyService
			}
		}
	}
	return nil
}
