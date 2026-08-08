package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const (
	defaultImportCandidateLimit = 100
	maxImportCandidateLimit     = 200
	maxImportWorkloads          = 100
	observeOnlyOwnershipMode    = "observe_only"
	managedOwnershipMode        = "managed"
)

func (s *Service) ListKubernetesImportCandidates(ctx context.Context, principal domainidentity.Principal, filter domaindelivery.KubernetesImportCandidateFilter) (domaindelivery.KubernetesImportCandidatePage, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationEnvView); err != nil {
		return domaindelivery.KubernetesImportCandidatePage{}, err
	}
	filter.ClusterID = strings.TrimSpace(filter.ClusterID)
	filter.Namespace = strings.TrimSpace(filter.Namespace)
	filter.Search = strings.ToLower(strings.TrimSpace(filter.Search))
	if filter.ClusterID == "" || filter.Namespace == "" {
		return domaindelivery.KubernetesImportCandidatePage{}, fmt.Errorf("%w: clusterId and namespace are required", apperrors.ErrInvalidArgument)
	}
	items, err := s.collectKubernetesImportCandidates(ctx, principal, filter.ClusterID, filter.Namespace)
	if err != nil {
		return domaindelivery.KubernetesImportCandidatePage{}, err
	}
	matched := make([]domaindelivery.KubernetesImportCandidate, 0, len(items))
	for _, item := range items {
		if filter.Search == "" || strings.Contains(strings.ToLower(item.WorkloadKind+"/"+item.WorkloadName), filter.Search) {
			matched = append(matched, item)
		}
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultImportCandidateLimit
	}
	if limit > maxImportCandidateLimit {
		limit = maxImportCandidateLimit
	}
	page := domaindelivery.KubernetesImportCandidatePage{Items: matched}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.Truncated = true
	}
	return page, nil
}

func (s *Service) ImportKubernetesServices(ctx context.Context, principal domainidentity.Principal, input domaindelivery.KubernetesServiceImportInput) (domaindelivery.KubernetesServiceImportResult, error) {
	if err := s.authorizeApplicationImport(ctx, principal); err != nil {
		return domaindelivery.KubernetesServiceImportResult{}, err
	}
	input = normalizeKubernetesServiceImportInput(input)
	if err := validateKubernetesServiceImportInput(input); err != nil {
		return domaindelivery.KubernetesServiceImportResult{}, err
	}
	candidates, err := s.collectKubernetesImportCandidates(ctx, principal, input.ClusterID, input.Namespace)
	if err != nil {
		return domaindelivery.KubernetesServiceImportResult{}, err
	}
	available := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		available[candidate.WorkloadKind+"/"+candidate.WorkloadName] = struct{}{}
	}
	for _, workload := range input.Workloads {
		if _, ok := available[workload.WorkloadKind+"/"+workload.WorkloadName]; !ok {
			return domaindelivery.KubernetesServiceImportResult{}, fmt.Errorf("%w: workload %s/%s was not found", apperrors.ErrNotFound, workload.WorkloadKind, workload.WorkloadName)
		}
	}
	if s.importRepository == nil {
		return domaindelivery.KubernetesServiceImportResult{}, fmt.Errorf("%w: Kubernetes import persistence is unavailable", apperrors.ErrUnsupportedOperation)
	}
	result, err := s.importRepository.ImportKubernetesServices(ctx, input)
	if err != nil {
		return domaindelivery.KubernetesServiceImportResult{}, err
	}
	s.recordKubernetesServiceImport(ctx, principal, input, result)
	return result, nil
}

func (s *Service) authorizeApplicationImport(ctx context.Context, principal domainidentity.Principal) error {
	for _, permission := range []string{
		appaccess.PermDeliveryApplicationEnvView,
		appaccess.PermDeliveryApplicationsCreate,
		appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationServicesManage, "create"),
		appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationEnvManage, "create"),
	} {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permission); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ImportHelmReleases(ctx context.Context, principal domainidentity.Principal, input domaindelivery.HelmReleaseImportInput) (domaindelivery.HelmReleaseImportResult, error) {
	if err := s.authorizeApplicationImport(ctx, principal); err != nil {
		return domaindelivery.HelmReleaseImportResult{}, err
	}
	input = normalizeHelmReleaseImportInput(input)
	if err := validateHelmReleaseImportInput(input); err != nil {
		return domaindelivery.HelmReleaseImportResult{}, err
	}
	if s.helmImportTargets == nil {
		return domaindelivery.HelmReleaseImportResult{}, fmt.Errorf("%w: Helm import discovery is unavailable", apperrors.ErrUnsupportedOperation)
	}
	items, err := s.helmImportTargets.ListHelmReleases(ctx, principal, input.ClusterID, input.Namespace)
	if err != nil {
		return domaindelivery.HelmReleaseImportResult{}, err
	}
	available := make(map[string]domainresource.HelmReleaseView, len(items))
	for _, item := range items {
		available[item.Name] = item
	}
	for index, release := range input.Releases {
		item, ok := available[release.ReleaseName]
		if !ok {
			return domaindelivery.HelmReleaseImportResult{}, fmt.Errorf("%w: Helm release %s was not found", apperrors.ErrNotFound, release.ReleaseName)
		}
		input.Releases[index].Revision = item.Revision
		input.Releases[index].Status = item.Status
		input.Releases[index].Chart = item.Chart
		input.Releases[index].AppVersion = item.AppVersion
		input.Releases[index].StorageDriver = item.StorageDriver
	}
	if s.importRepository == nil {
		return domaindelivery.HelmReleaseImportResult{}, fmt.Errorf("%w: Helm import persistence is unavailable", apperrors.ErrUnsupportedOperation)
	}
	result, err := s.importRepository.ImportHelmReleases(ctx, input)
	if err != nil {
		return domaindelivery.HelmReleaseImportResult{}, err
	}
	s.recordHelmReleaseImport(ctx, principal, input, result)
	return result, nil
}

func (s *Service) collectKubernetesImportCandidates(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domaindelivery.KubernetesImportCandidate, error) {
	if s.importTargets == nil {
		return nil, fmt.Errorf("%w: Kubernetes import discovery is unavailable", apperrors.ErrUnsupportedOperation)
	}
	deployments, err := s.importTargets.ListDeployments(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	statefulSets, err := s.importTargets.ListStatefulSets(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	daemonSets, err := s.importTargets.ListDaemonSets(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	services, err := s.importTargets.ListServices(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	ingresses, err := s.importTargets.ListIngresses(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	hpas, err := s.importTargets.ListHorizontalPodAutoscalers(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}

	items := make([]domaindelivery.KubernetesImportCandidate, 0, len(deployments)+len(statefulSets)+len(daemonSets))
	for _, item := range deployments {
		candidate := kubernetesImportCandidate(clusterID, item.Namespace, "Deployment", item.Name, int(item.DesiredReplicas), int(item.ReadyReplicas), item.Labels)
		if detail, detailErr := s.importTargets.GetDeploymentDetail(ctx, principal, clusterID, item.Namespace, item.Name); detailErr == nil {
			candidate.Containers = make([]string, 0, len(detail.Containers))
			for _, container := range detail.Containers {
				candidate.Containers = append(candidate.Containers, container.Name)
			}
		}
		candidate.RelatedResources = relatedKubernetesResources(candidate, "", services, ingresses, hpas)
		items = append(items, candidate)
	}
	for _, item := range statefulSets {
		candidate := kubernetesImportCandidate(clusterID, item.Namespace, "StatefulSet", item.Name, int(item.DesiredReplicas), int(item.ReadyReplicas), nil)
		candidate.RelatedResources = relatedKubernetesResources(candidate, item.ServiceName, services, ingresses, hpas)
		items = append(items, candidate)
	}
	for _, item := range daemonSets {
		candidate := kubernetesImportCandidate(clusterID, item.Namespace, "DaemonSet", item.Name, int(item.DesiredNumber), int(item.ReadyNumber), nil)
		candidate.RelatedResources = relatedKubernetesResources(candidate, "", services, ingresses, hpas)
		items = append(items, candidate)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := workloadKindOrder(items[i].WorkloadKind), workloadKindOrder(items[j].WorkloadKind)
		if left != right {
			return left < right
		}
		return items[i].WorkloadName < items[j].WorkloadName
	})
	return items, nil
}

func kubernetesImportCandidate(clusterID, namespace, kind, name string, desired, ready int, labels map[string]string) domaindelivery.KubernetesImportCandidate {
	return domaindelivery.KubernetesImportCandidate{
		ClusterID: clusterID, Namespace: namespace, WorkloadKind: kind, WorkloadName: name,
		DesiredReplicas: desired, ReadyReplicas: ready, Labels: labels,
		RelatedResources: []domaindelivery.KubernetesRelatedResource{},
	}
}

func relatedKubernetesResources(candidate domaindelivery.KubernetesImportCandidate, statefulServiceName string, services []domainresource.ServiceView, ingresses []domainresource.IngressView, hpas []domainresource.HorizontalPodAutoscalerView) []domaindelivery.KubernetesRelatedResource {
	related := make([]domaindelivery.KubernetesRelatedResource, 0)
	serviceNames := map[string]struct{}{}
	for _, service := range services {
		matchesDeployment := candidate.WorkloadKind == "Deployment" && selectorMatchesLabels(service.Selector, candidate.Labels)
		matchesStatefulSet := candidate.WorkloadKind == "StatefulSet" && statefulServiceName != "" && service.Name == statefulServiceName
		if !matchesDeployment && !matchesStatefulSet {
			continue
		}
		serviceNames[service.Name] = struct{}{}
		related = append(related, domaindelivery.KubernetesRelatedResource{Kind: "Service", Name: service.Name})
	}
	for _, ingress := range ingresses {
		if intersectsServiceNames(ingress.BackendServices, serviceNames) {
			related = append(related, domaindelivery.KubernetesRelatedResource{Kind: "Ingress", Name: ingress.Name})
		}
	}
	for _, hpa := range hpas {
		if hpa.TargetRef == candidate.WorkloadKind+"/"+candidate.WorkloadName {
			related = append(related, domaindelivery.KubernetesRelatedResource{Kind: "HorizontalPodAutoscaler", Name: hpa.Name})
		}
	}
	sort.Slice(related, func(i, j int) bool {
		if related[i].Kind != related[j].Kind {
			return related[i].Kind < related[j].Kind
		}
		return related[i].Name < related[j].Name
	})
	return related
}

func intersectsServiceNames(names []string, candidates map[string]struct{}) bool {
	for _, name := range names {
		if _, ok := candidates[name]; ok {
			return true
		}
	}
	return false
}

func workloadKindOrder(kind string) int {
	switch kind {
	case "Deployment":
		return 0
	case "StatefulSet":
		return 1
	default:
		return 2
	}
}

func normalizeKubernetesServiceImportInput(input domaindelivery.KubernetesServiceImportInput) domaindelivery.KubernetesServiceImportInput {
	input.ClusterID = strings.TrimSpace(input.ClusterID)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.ApplicationKey = strings.TrimSpace(input.ApplicationKey)
	input.ApplicationName = strings.TrimSpace(input.ApplicationName)
	input.EnvironmentKey = strings.TrimSpace(input.EnvironmentKey)
	input.EnvironmentName = strings.TrimSpace(input.EnvironmentName)
	input.OwnershipMode = strings.TrimSpace(input.OwnershipMode)
	for index := range input.Workloads {
		input.Workloads[index].WorkloadKind = strings.TrimSpace(input.Workloads[index].WorkloadKind)
		input.Workloads[index].WorkloadName = strings.TrimSpace(input.Workloads[index].WorkloadName)
	}
	return input
}

func validateKubernetesServiceImportInput(input domaindelivery.KubernetesServiceImportInput) error {
	if input.ClusterID == "" || input.Namespace == "" || input.ApplicationKey == "" || input.ApplicationName == "" || input.EnvironmentKey == "" || input.EnvironmentName == "" {
		return fmt.Errorf("%w: cluster, namespace, application, and environment are required", apperrors.ErrInvalidArgument)
	}
	if input.OwnershipMode != observeOnlyOwnershipMode && input.OwnershipMode != managedOwnershipMode {
		return fmt.Errorf("%w: ownershipMode must be observe_only or managed", apperrors.ErrInvalidArgument)
	}
	if len(input.Workloads) == 0 || len(input.Workloads) > maxImportWorkloads {
		return fmt.Errorf("%w: workloads must contain between 1 and %d items", apperrors.ErrInvalidArgument, maxImportWorkloads)
	}
	seen := make(map[string]struct{}, len(input.Workloads))
	for _, workload := range input.Workloads {
		if workload.WorkloadKind != "Deployment" && workload.WorkloadKind != "StatefulSet" && workload.WorkloadKind != "DaemonSet" {
			return fmt.Errorf("%w: unsupported workload kind %q", apperrors.ErrInvalidArgument, workload.WorkloadKind)
		}
		key := workload.WorkloadKind + "/" + workload.WorkloadName
		if workload.WorkloadName == "" {
			return fmt.Errorf("%w: workloadName is required", apperrors.ErrInvalidArgument)
		}
		if input.OwnershipMode == managedOwnershipMode && workload.WorkloadKind != "Deployment" {
			return fmt.Errorf("%w: managed imports only support Deployment workloads", apperrors.ErrInvalidArgument)
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate workload %s", apperrors.ErrInvalidArgument, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func normalizeHelmReleaseImportInput(input domaindelivery.HelmReleaseImportInput) domaindelivery.HelmReleaseImportInput {
	input.ClusterID = strings.TrimSpace(input.ClusterID)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.ApplicationKey = strings.TrimSpace(input.ApplicationKey)
	input.ApplicationName = strings.TrimSpace(input.ApplicationName)
	input.EnvironmentKey = strings.TrimSpace(input.EnvironmentKey)
	input.EnvironmentName = strings.TrimSpace(input.EnvironmentName)
	input.OwnershipMode = strings.TrimSpace(input.OwnershipMode)
	for index := range input.Releases {
		input.Releases[index].ReleaseName = strings.TrimSpace(input.Releases[index].ReleaseName)
	}
	return input
}

func validateHelmReleaseImportInput(input domaindelivery.HelmReleaseImportInput) error {
	if input.ClusterID == "" || input.Namespace == "" || input.ApplicationKey == "" || input.ApplicationName == "" || input.EnvironmentKey == "" || input.EnvironmentName == "" {
		return fmt.Errorf("%w: cluster, namespace, application, and environment are required", apperrors.ErrInvalidArgument)
	}
	if input.OwnershipMode != observeOnlyOwnershipMode && input.OwnershipMode != managedOwnershipMode {
		return fmt.Errorf("%w: ownershipMode must be observe_only or managed", apperrors.ErrInvalidArgument)
	}
	if len(input.Releases) == 0 || len(input.Releases) > maxImportWorkloads {
		return fmt.Errorf("%w: releases must contain between 1 and %d items", apperrors.ErrInvalidArgument, maxImportWorkloads)
	}
	seen := make(map[string]struct{}, len(input.Releases))
	for _, release := range input.Releases {
		if release.ReleaseName == "" {
			return fmt.Errorf("%w: releaseName is required", apperrors.ErrInvalidArgument)
		}
		if _, ok := seen[release.ReleaseName]; ok {
			return fmt.Errorf("%w: duplicate Helm release %s", apperrors.ErrInvalidArgument, release.ReleaseName)
		}
		seen[release.ReleaseName] = struct{}{}
	}
	return nil
}

func (s *Service) recordKubernetesServiceImport(ctx context.Context, principal domainidentity.Principal, input domaindelivery.KubernetesServiceImportInput, result domaindelivery.KubernetesServiceImportResult) {
	meta := requestctx.FromContext(ctx)
	metadata := map[string]any{
		"clusterId": input.ClusterID, "namespace": input.Namespace, "applicationId": result.Application.ID,
		"applicationEnvironmentId": result.ApplicationEnvironmentID, "ownershipMode": result.OwnershipMode,
		"workloadCount": len(input.Workloads),
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
			ClusterID: input.ClusterID, Namespace: input.Namespace, ResourceKind: "ApplicationEnvironment", ResourceName: result.ApplicationEnvironmentID,
			Action: "kubernetes.import", Result: "success", Summary: "imported Kubernetes services in observe-only mode",
			RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: metadata,
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(ctx, principal, "delivery.kubernetes.import", map[string]any{
			"module": "delivery", "resourceKind": "ApplicationEnvironment", "targetId": result.ApplicationEnvironmentID,
		}, "success", "imported Kubernetes services in observe-only mode", metadata))
	}
}

func (s *Service) recordHelmReleaseImport(ctx context.Context, principal domainidentity.Principal, input domaindelivery.HelmReleaseImportInput, result domaindelivery.HelmReleaseImportResult) {
	meta := requestctx.FromContext(ctx)
	metadata := map[string]any{
		"clusterId": input.ClusterID, "namespace": input.Namespace, "applicationId": result.Application.ID,
		"applicationEnvironmentId": result.ApplicationEnvironmentID, "ownershipMode": result.OwnershipMode,
		"releaseCount": len(input.Releases),
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
			ClusterID: input.ClusterID, Namespace: input.Namespace, ResourceKind: "ApplicationEnvironment", ResourceName: result.ApplicationEnvironmentID,
			Action: "helm.import", Result: "success", Summary: "imported Helm releases",
			RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: metadata,
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(ctx, principal, "delivery.helm.import", map[string]any{
			"module": "delivery", "resourceKind": "ApplicationEnvironment", "targetId": result.ApplicationEnvironmentID,
		}, "success", "imported Helm releases", metadata))
	}
}
