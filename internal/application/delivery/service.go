package delivery

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type ApplicationReader interface {
	List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error)
	Get(context.Context, domainidentity.Principal, string) (domainapp.App, error)
	Create(context.Context, domainidentity.Principal, domainapp.UpsertInput) (domainapp.App, error)
	Update(context.Context, domainidentity.Principal, string, domainapp.UpsertInput) (domainapp.App, error)
	ListServices(context.Context, domainidentity.Principal, string) ([]domainapp.Service, error)
	CreateService(context.Context, domainidentity.Principal, string, domainapp.ServiceInput) (domainapp.Service, error)
	UpdateService(context.Context, domainidentity.Principal, string, string, domainapp.ServiceInput) (domainapp.Service, error)
}

type CatalogReader interface {
	ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error)
	ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error)
	GetApplicationEnvironment(context.Context, domainidentity.Principal, string) (domaincatalog.ApplicationEnvironment, error)
	CreateApplicationEnvironment(context.Context, domainidentity.Principal, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error)
	UpdateApplicationEnvironment(context.Context, domainidentity.Principal, string, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error)
}

type BuildReader interface {
	List(context.Context, domainidentity.Principal, domainbuild.Filter) ([]domainbuild.Record, error)
	Get(context.Context, domainidentity.Principal, string) (domainbuild.Record, error)
	Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error)
}

type WorkflowReader interface {
	List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error)
	Get(context.Context, domainidentity.Principal, string) (domainworkflow.Run, error)
	Trigger(context.Context, domainidentity.Principal, domainworkflow.Input) (domainworkflow.Run, error)
	TriggerValidation(context.Context, domainidentity.Principal, domainworkflow.Input) (domainworkflow.Run, error)
	TriggerRollback(context.Context, domainidentity.Principal, domainworkflow.Input) (domainworkflow.Run, error)
}

type ReleaseReader interface {
	List(context.Context, domainidentity.Principal, domainrelease.Filter) ([]domainrelease.Record, error)
	Get(context.Context, domainidentity.Principal, string) (domainrelease.Record, error)
	Trigger(context.Context, domainidentity.Principal, domainrelease.TriggerInput) (domainrelease.Record, error)
}

type ExecutionController interface {
	ClaimExecutionTask(context.Context, []string, string, string) (domaindelivery.ExecutionTask, error)
	GetExecutionTask(context.Context, domainidentity.Principal, string) (domaindelivery.ExecutionTask, error)
	ListExecutionArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error)
	ListReleaseBundleArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error)
	RecordCallback(context.Context, domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionTask, error)
	CancelExecutionTask(context.Context, string, domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error)
	RetryExecutionTask(context.Context, string, domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error)
}

type TargetReader interface {
	ListPods(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodView, error)
	ListDeployments(context.Context, domainidentity.Principal, string, string) ([]domainresource.DeploymentView, error)
	GetDeploymentDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentDetailView, error)
	ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error)
	ListIngresses(context.Context, domainidentity.Principal, string, string) ([]domainresource.IngressView, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	applications ApplicationReader
	catalog      CatalogReader
	builds       BuildReader
	workflows    WorkflowReader
	releases     ReleaseReader
	repository   domaindelivery.Repository
	execution    ExecutionController
	targets      TargetReader
	permissions  *appaccess.PermissionResolver
	audit        AuditRecorder
	operations   OperationRecorder
}

func New(applications ApplicationReader, catalog CatalogReader, builds BuildReader, workflows WorkflowReader, releases ReleaseReader, repository domaindelivery.Repository, execution ExecutionController, targets TargetReader, permissions *appaccess.PermissionResolver) *Service {
	return &Service{
		applications: applications,
		catalog:      catalog,
		builds:       builds,
		workflows:    workflows,
		releases:     releases,
		repository:   repository,
		execution:    execution,
		targets:      targets,
		permissions:  permissions,
	}
}

func (s *Service) SetRecorders(audit AuditRecorder, operations OperationRecorder) {
	s.audit = audit
	s.operations = operations
}

func (s *Service) GetApplicationDetail(ctx context.Context, principal domainidentity.Principal, applicationID string) (domaindelivery.ApplicationDetail, error) {
	app, err := s.applications.Get(ctx, principal, strings.TrimSpace(applicationID))
	if err != nil {
		return domaindelivery.ApplicationDetail{}, err
	}
	bindings, environments, bundles, tasks, builds, workflows, releases, err := s.loadDeliveryContext(ctx, principal, app.ID)
	if err != nil {
		return domaindelivery.ApplicationDetail{}, err
	}
	envByID := make(map[string]domaincatalog.Environment, len(environments))
	for _, item := range environments {
		envByID[item.ID] = item
	}
	items := make([]domaindelivery.ApplicationBindingSummary, 0)
	for _, binding := range bindings {
		if binding.ApplicationID != app.ID {
			continue
		}
		environment := envByID[binding.EnvironmentID]
		items = append(items, domaindelivery.ApplicationBindingSummary{
			ApplicationEnvironmentID: binding.ID,
			EnvironmentID:            binding.EnvironmentID,
			EnvironmentName:          environment.Name,
			EnvironmentKey:           binding.EnvironmentKey,
			ActionKind:               actionKindForBinding(binding, environment),
			RequiresApproval:         requiresApproval(binding, environment),
			WorkflowTemplateID:       binding.WorkflowTemplateID,
			WorkflowTemplateName:     workflowTemplateName(binding),
			WorkflowTemplate:         binding.WorkflowTemplate,
			TargetCount:              len(binding.Targets),
			Targets:                  binding.Targets,
			BuildSourceID:            binding.BuildPolicy.SourceID,
			BuildSource:              resolveBuildSource(app, binding.BuildPolicy.SourceID),
			BuildPolicy:              binding.BuildPolicy,
			LatestBundle:             latestBundleForBinding(binding, bundles),
			LatestExecutionTask:      latestExecutionTaskForBinding(binding, tasks),
			LatestBuild:              latestBuildForBinding(binding, builds),
			LatestWorkflow:           latestWorkflowForBinding(binding, workflows),
			LatestRelease:            latestReleaseForBinding(binding, releases),
		})
	}
	return domaindelivery.ApplicationDetail{
		Application:         app,
		Bindings:            items,
		LatestBundle:        latestBundleForApplication(app.ID, bundles),
		LatestExecutionTask: latestExecutionTaskForApplication(app.ID, tasks),
		LatestBuild:         latestBuildForApplication(app.ID, builds),
		LatestWorkflow:      latestWorkflowForApplication(app.ID, workflows),
		LatestRelease:       latestReleaseForApplication(app.ID, releases),
	}, nil
}

func (s *Service) GetApplicationRuntimeDetail(ctx context.Context, principal domainidentity.Principal, applicationID string) (domaindelivery.ApplicationRuntimeDetail, error) {
	app, err := s.applications.Get(ctx, principal, strings.TrimSpace(applicationID))
	if err != nil {
		return domaindelivery.ApplicationRuntimeDetail{}, err
	}
	bindings, environments, bundles, tasks, builds, workflows, releases, err := s.loadDeliveryContext(ctx, principal, app.ID)
	if err != nil {
		return domaindelivery.ApplicationRuntimeDetail{}, err
	}
	envByID := make(map[string]domaincatalog.Environment, len(environments))
	for _, item := range environments {
		envByID[item.ID] = item
	}
	items := make([]domaindelivery.ApplicationRuntimeEnvironment, 0)
	for _, binding := range bindings {
		if binding.ApplicationID != app.ID {
			continue
		}
		environment := envByID[binding.EnvironmentID]
		workloads, workloadsErr := s.listRuntimeWorkloadsForBinding(ctx, principal, app, binding, bundles, tasks, builds, workflows, releases)
		if workloadsErr != nil {
			return domaindelivery.ApplicationRuntimeDetail{}, workloadsErr
		}
		items = append(items, domaindelivery.ApplicationRuntimeEnvironment{
			ApplicationEnvironmentID: binding.ID,
			EnvironmentID:            binding.EnvironmentID,
			EnvironmentName:          environment.Name,
			EnvironmentKey:           binding.EnvironmentKey,
			ActionKind:               actionKindForBinding(binding, environment),
			RequiresApproval:         requiresApproval(binding, environment),
			ResourceSelector:         binding.ResourceSelector,
			Targets:                  binding.Targets,
			Workloads:                workloads,
		})
	}
	return domaindelivery.ApplicationRuntimeDetail{
		Application:  app,
		Environments: items,
	}, nil
}

func (s *Service) GetApplicationWorkloadRuntimeDetail(ctx context.Context, principal domainidentity.Principal, applicationID, bindingID, workloadName string) (domaindelivery.ApplicationWorkloadRuntimeDetail, error) {
	app, err := s.applications.Get(ctx, principal, strings.TrimSpace(applicationID))
	if err != nil {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, strings.TrimSpace(bindingID))
	if err != nil {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, err
	}
	if binding.ApplicationID != app.ID {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, fmt.Errorf("application environment does not belong to application")
	}
	_, environments, bundles, tasks, builds, workflows, releases, err := s.loadDeliveryContext(ctx, principal, app.ID)
	if err != nil {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, err
	}
	var environment *domaincatalog.Environment
	for _, item := range environments {
		if item.ID == binding.EnvironmentID {
			copyItem := item
			environment = &copyItem
			break
		}
	}
	workloads, err := s.listRuntimeWorkloadsForBinding(ctx, principal, app, binding, bundles, tasks, builds, workflows, releases)
	if err != nil {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, err
	}
	var selected *domaindelivery.ApplicationRuntimeWorkload
	for _, item := range workloads {
		if item.WorkloadName == strings.TrimSpace(workloadName) {
			copyItem := item
			selected = &copyItem
			break
		}
	}
	if selected == nil {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, fmt.Errorf("%w: workload not found", apperrors.ErrNotFound)
	}
	deployment, err := s.targets.GetDeploymentDetail(ctx, principal, selected.ClusterID, selected.Namespace, selected.WorkloadName)
	if err != nil {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, err
	}
	pods, err := s.targets.ListPods(ctx, principal, selected.ClusterID, selected.Namespace)
	if err != nil {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, err
	}
	services, err := s.targets.ListServices(ctx, principal, selected.ClusterID, selected.Namespace)
	if err != nil {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, err
	}
	ingresses, err := s.targets.ListIngresses(ctx, principal, selected.ClusterID, selected.Namespace)
	if err != nil {
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, err
	}
	return domaindelivery.ApplicationWorkloadRuntimeDetail{
		Application: app,
		Binding:     binding,
		Environment: environment,
		Workload:    *selected,
		Deployment:  deployment,
		Pods:        filterPodsBySelector(pods, deployment.Selector),
		Services:    filterServicesBySelector(services, deployment.Selector),
		Ingresses:   filterIngressesByServices(ingresses, filterServicesBySelector(services, deployment.Selector)),
	}, nil
}

func (s *Service) GetApplicationEnvironmentDetail(ctx context.Context, principal domainidentity.Principal, bindingID string) (domaindelivery.ApplicationEnvironmentDetail, error) {
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, strings.TrimSpace(bindingID))
	if err != nil {
		return domaindelivery.ApplicationEnvironmentDetail{}, err
	}
	app, err := s.applications.Get(ctx, principal, binding.ApplicationID)
	if err != nil {
		return domaindelivery.ApplicationEnvironmentDetail{}, err
	}
	_, environments, bundles, tasks, builds, workflows, releases, err := s.loadDeliveryContext(ctx, principal, app.ID)
	if err != nil {
		return domaindelivery.ApplicationEnvironmentDetail{}, err
	}
	var environment *domaincatalog.Environment
	for _, item := range environments {
		if item.ID == binding.EnvironmentID {
			copyItem := item
			environment = &copyItem
			break
		}
	}
	return domaindelivery.ApplicationEnvironmentDetail{
		Binding:             binding,
		Application:         app,
		Environment:         environment,
		ActionKind:          actionKindForBinding(binding, derefEnvironment(environment)),
		RequiresApproval:    requiresApproval(binding, derefEnvironment(environment)),
		BuildSource:         resolveBuildSource(app, binding.BuildPolicy.SourceID),
		LatestBundle:        latestBundleForBinding(binding, bundles),
		LatestExecutionTask: latestExecutionTaskForBinding(binding, tasks),
		LatestBuild:         latestBuildForBinding(binding, builds),
		LatestWorkflow:      latestWorkflowForBinding(binding, workflows),
		LatestRelease:       latestReleaseForBinding(binding, releases),
	}, nil
}

func (s *Service) ListReleaseBoard(ctx context.Context, principal domainidentity.Principal) ([]domaindelivery.ReleaseBoardEntry, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryReleaseBoardView); err != nil {
		return nil, err
	}
	apps, err := s.applications.List(ctx, principal, domainapp.Filter{Limit: 200})
	if err != nil {
		return nil, err
	}
	bindings, err := s.catalog.ListApplicationEnvironments(ctx, principal)
	if err != nil {
		return nil, err
	}
	environments, err := s.catalog.ListEnvironments(ctx, principal)
	if err != nil {
		return nil, err
	}
	envByID := make(map[string]domaincatalog.Environment, len(environments))
	for _, item := range environments {
		envByID[item.ID] = item
	}
	appByID := make(map[string]domainapp.App, len(apps))
	for _, item := range apps {
		appByID[item.ID] = item
	}
	items := make([]domaindelivery.ReleaseBoardEntry, 0, len(bindings))
	for _, binding := range bindings {
		app, ok := appByID[binding.ApplicationID]
		if !ok {
			continue
		}
		bundles, _ := s.repository.ListReleaseBundles(ctx, domaindelivery.ReleaseBundleFilter{ApplicationID: app.ID, Limit: 20})
		tasks, _ := s.repository.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: app.ID, Limit: 20})
		tasks = domaindelivery.WithOperationStates(tasks, time.Now().UTC())
		builds, _ := s.builds.List(ctx, principal, domainbuild.Filter{ApplicationID: app.ID, Limit: 20})
		workflows, _ := s.workflows.List(ctx, principal, app.ID, 20)
		releases, _ := s.releases.List(ctx, principal, domainrelease.Filter{ApplicationID: app.ID, Limit: 20})
		environment := envByID[binding.EnvironmentID]
		items = append(items, domaindelivery.ReleaseBoardEntry{
			ApplicationEnvironmentID: binding.ID,
			ApplicationID:            app.ID,
			ApplicationName:          app.Name,
			BusinessLineID:           binding.BusinessLineID,
			EnvironmentID:            binding.EnvironmentID,
			EnvironmentName:          environment.Name,
			EnvironmentKey:           binding.EnvironmentKey,
			ActionKind:               actionKindForBinding(binding, environment),
			RequiresApproval:         requiresApproval(binding, environment),
			WorkflowTemplateID:       binding.WorkflowTemplateID,
			WorkflowTemplateName:     workflowTemplateName(binding),
			BuildSourceID:            binding.BuildPolicy.SourceID,
			BuildSource:              resolveBuildSource(app, binding.BuildPolicy.SourceID),
			BuildPolicy:              binding.BuildPolicy,
			LatestBundle:             latestBundleForBinding(binding, bundles),
			LatestExecutionTask:      latestExecutionTaskForBinding(binding, tasks),
			Targets:                  binding.Targets,
			LatestBuild:              latestBuildForBinding(binding, builds),
			LatestWorkflow:           latestWorkflowForBinding(binding, workflows),
			LatestRelease:            latestReleaseForBinding(binding, releases),
		})
	}
	return items, nil
}

func (s *Service) ListTargetCandidates(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, search string) ([]domaindelivery.TargetCandidate, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationEnvManage); err != nil {
		return nil, err
	}
	items, err := s.targets.ListDeployments(ctx, principal, strings.TrimSpace(clusterID), strings.TrimSpace(namespace))
	if err != nil {
		return nil, err
	}
	matched := make([]domaindelivery.TargetCandidate, 0)
	for _, item := range items {
		if !matchesSearch(item, search) {
			continue
		}
		detail, detailErr := s.targets.GetDeploymentDetail(ctx, principal, strings.TrimSpace(clusterID), item.Namespace, item.Name)
		if detailErr != nil {
			continue
		}
		containers := make([]string, 0, len(detail.Containers))
		for _, container := range detail.Containers {
			containers = append(containers, container.Name)
		}
		matched = append(matched, domaindelivery.TargetCandidate{
			ClusterID:    strings.TrimSpace(clusterID),
			Namespace:    item.Namespace,
			WorkloadKind: "Deployment",
			WorkloadName: item.Name,
			Containers:   containers,
			Labels:       item.Labels,
		})
	}
	return matched, nil
}

func (s *Service) ListReleaseBundles(ctx context.Context, principal domainidentity.Principal, filter domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryReleaseBundlesView); err != nil {
		return nil, err
	}
	return s.repository.ListReleaseBundles(ctx, filter)
}

func (s *Service) GetReleaseBundle(ctx context.Context, principal domainidentity.Principal, bundleID string) (domaindelivery.ReleaseBundle, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryReleaseBundlesView); err != nil {
		return domaindelivery.ReleaseBundle{}, err
	}
	bundle, err := s.repository.GetReleaseBundle(ctx, strings.TrimSpace(bundleID))
	if err != nil {
		return domaindelivery.ReleaseBundle{}, err
	}
	artifacts, artifactErr := s.repository.ListExecutionArtifactsByBundle(ctx, bundle.ID)
	if artifactErr == nil {
		bundle.Artifacts = artifacts
	}
	return bundle, nil
}

func (s *Service) ListExecutionTasks(ctx context.Context, principal domainidentity.Principal, filter domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksView); err != nil {
		return nil, err
	}
	items, err := s.repository.ListExecutionTasks(ctx, filter)
	return domaindelivery.WithOperationStates(items, time.Now().UTC()), err
}

func (s *Service) GetExecutionTask(ctx context.Context, principal domainidentity.Principal, taskID string) (domaindelivery.ExecutionTask, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksView); err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	task, err := s.repository.GetExecutionTask(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	artifacts, artifactErr := s.repository.ListExecutionArtifacts(ctx, task.ID)
	if artifactErr == nil && len(artifacts) > 0 {
		task.Artifacts = artifacts
	}
	return domaindelivery.WithOperationState(task, time.Now().UTC()), nil
}

func (s *Service) ListExecutionLogs(ctx context.Context, principal domainidentity.Principal, taskID string, limit int) ([]domaindelivery.ExecutionLog, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksView); err != nil {
		return nil, err
	}
	return s.repository.ListExecutionLogs(ctx, strings.TrimSpace(taskID), limit)
}

func (s *Service) ListArtifacts(ctx context.Context, principal domainidentity.Principal, filter domaindelivery.ArtifactFilter) ([]domaindelivery.ExecutionArtifact, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksView); err != nil {
		return nil, err
	}
	return s.repository.ListArtifacts(ctx, filter)
}

func (s *Service) ListExecutionArtifacts(ctx context.Context, principal domainidentity.Principal, taskID string) ([]domaindelivery.ExecutionArtifact, error) {
	if s.execution != nil {
		return s.execution.ListExecutionArtifacts(ctx, principal, strings.TrimSpace(taskID))
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksView); err != nil {
		return nil, err
	}
	return s.repository.ListExecutionArtifacts(ctx, strings.TrimSpace(taskID))
}

func (s *Service) ListReleaseBundleArtifacts(ctx context.Context, principal domainidentity.Principal, bundleID string) ([]domaindelivery.ExecutionArtifact, error) {
	if s.execution != nil {
		return s.execution.ListReleaseBundleArtifacts(ctx, principal, strings.TrimSpace(bundleID))
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryReleaseBundlesView); err != nil {
		return nil, err
	}
	return s.repository.ListExecutionArtifactsByBundle(ctx, strings.TrimSpace(bundleID))
}

func (s *Service) GetBuildRuntimeDetail(ctx context.Context, principal domainidentity.Principal, buildID string) (domaindelivery.RuntimeObjectDetail, error) {
	record, err := s.builds.Get(ctx, principal, strings.TrimSpace(buildID))
	if err != nil {
		return domaindelivery.RuntimeObjectDetail{}, err
	}
	artifacts := s.artifactsForRuntimeObject(ctx, domaindelivery.ArtifactFilter{
		ApplicationID:   record.ApplicationID,
		ExecutionTaskID: metadataString(record.Metadata, "executionTaskId"),
		ReleaseBundleID: metadataString(record.Metadata, "releaseBundleId"),
		WorkflowRunID:   metadataString(record.Metadata, "triggeredByWorkflowRunId"),
		Limit:           100,
	})
	return s.buildRuntimeObjectDetail(ctx, principal, "build", record.ID, record.ApplicationID, metadataString(record.Metadata, "applicationEnvironmentId"), record, record.Metadata, artifacts, map[string]any{
		"sourceSystem": record.SourceSystem,
		"metadata":     record.Metadata,
		"startedAt":    record.StartedAt,
		"finishedAt":   record.FinishedAt,
	})
}

func (s *Service) GetWorkflowRuntimeDetail(ctx context.Context, principal domainidentity.Principal, workflowRunID string) (domaindelivery.RuntimeObjectDetail, error) {
	run, err := s.workflows.Get(ctx, principal, strings.TrimSpace(workflowRunID))
	if err != nil {
		return domaindelivery.RuntimeObjectDetail{}, err
	}
	artifacts := s.artifactsForRuntimeObject(ctx, domaindelivery.ArtifactFilter{WorkflowRunID: run.ID, Limit: 500})
	return s.buildRuntimeObjectDetail(ctx, principal, "workflow", run.ID, run.ApplicationID, metadataString(run.Metadata, "bindingId"), run, run.Metadata, artifacts, map[string]any{
		"workflowName": run.WorkflowName,
		"steps":        run.Steps,
		"nodeRuns":     run.NodeRuns,
		"nodeOutputs":  run.Metadata["nodeOutputs"],
		"events":       run.Metadata["events"],
	})
}

func (s *Service) GetReleaseRuntimeDetail(ctx context.Context, principal domainidentity.Principal, releaseID string) (domaindelivery.RuntimeObjectDetail, error) {
	record, err := s.releases.Get(ctx, principal, strings.TrimSpace(releaseID))
	if err != nil {
		return domaindelivery.RuntimeObjectDetail{}, err
	}
	artifacts := s.artifactsForRuntimeObject(ctx, domaindelivery.ArtifactFilter{
		ApplicationID:   record.ApplicationID,
		ExecutionTaskID: metadataString(record.Metadata, "executionTaskId"),
		ReleaseBundleID: metadataString(record.Metadata, "releaseBundleId"),
		WorkflowRunID:   metadataString(record.Metadata, "workflowRunId"),
		Limit:           100,
	})
	return s.buildRuntimeObjectDetail(ctx, principal, "release", record.ID, record.ApplicationID, metadataString(record.Metadata, "applicationEnvironmentId"), record, record.Metadata, artifacts, map[string]any{
		"clusterId":      record.ClusterID,
		"namespace":      record.Namespace,
		"deploymentName": record.DeploymentName,
		"deployedAt":     record.DeployedAt,
		"metadata":       record.Metadata,
	})
}

func (s *Service) GetReleaseBundleRuntimeDetail(ctx context.Context, principal domainidentity.Principal, bundleID string) (domaindelivery.RuntimeObjectDetail, error) {
	bundle, err := s.GetReleaseBundle(ctx, principal, strings.TrimSpace(bundleID))
	if err != nil {
		return domaindelivery.RuntimeObjectDetail{}, err
	}
	artifacts := bundle.Artifacts
	if len(artifacts) == 0 {
		artifacts = s.artifactsForRuntimeObject(ctx, domaindelivery.ArtifactFilter{ReleaseBundleID: bundle.ID, Limit: 500})
	}
	return s.buildRuntimeObjectDetail(ctx, principal, "release_bundle", bundle.ID, bundle.ApplicationID, bundle.ApplicationEnvironmentID, bundle, bundle.Metadata, artifacts, map[string]any{
		"version":        bundle.Version,
		"sourceType":     bundle.SourceType,
		"artifactRef":    bundle.ArtifactRef,
		"artifactDigest": bundle.ArtifactDigest,
		"metadata":       bundle.Metadata,
	})
}

func (s *Service) GetExecutionTaskRuntimeDetail(ctx context.Context, principal domainidentity.Principal, taskID string) (domaindelivery.RuntimeObjectDetail, error) {
	task, err := s.GetExecutionTask(ctx, principal, strings.TrimSpace(taskID))
	if err != nil {
		return domaindelivery.RuntimeObjectDetail{}, err
	}
	artifacts := task.Artifacts
	if len(artifacts) == 0 {
		artifacts = s.artifactsForRuntimeObject(ctx, domaindelivery.ArtifactFilter{ExecutionTaskID: task.ID, Limit: 500})
	}
	logs, _ := s.repository.ListExecutionLogs(ctx, task.ID, 50)
	return s.buildRuntimeObjectDetail(ctx, principal, "execution_task", task.ID, task.ApplicationID, task.ApplicationEnvironmentID, task, mergeMaps(task.Payload, task.Result), artifacts, map[string]any{
		"taskKind":       task.TaskKind,
		"providerKind":   task.ProviderKind,
		"targetKind":     task.TargetKind,
		"operationState": task.OperationState,
		"payload":        task.Payload,
		"result":         task.Result,
		"logs":           logs,
	})
}

func (s *Service) ListDeliveryBlueprints(ctx context.Context, principal domainidentity.Principal) ([]domaindelivery.DeliveryBlueprint, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return nil, err
	}
	return s.repository.ListDeliveryBlueprints(ctx)
}

func (s *Service) CreateDeliveryBlueprint(ctx context.Context, principal domainidentity.Principal, input domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domaindelivery.DeliveryBlueprint{}, err
	}
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaindelivery.DeliveryBlueprint{}, fmt.Errorf("delivery blueprint key and name are required")
	}
	item, err := s.repository.CreateDeliveryBlueprint(ctx, input)
	if err == nil {
		usage, usageErr := s.GetDeliveryBlueprintUsage(ctx, principal, item.ID)
		if usageErr != nil {
			usage = domaincatalog.TemplateUsageSummary{}
		}
		s.recordTemplateUsageChange(ctx, principal, "delivery.delivery_blueprint.create", "DeliveryBlueprint", item.ID, item.Name, "created delivery blueprint", templateUsageAuditSnapshot(usage))
	}
	return item, err
}

func (s *Service) UpdateDeliveryBlueprint(ctx context.Context, principal domainidentity.Principal, blueprintID string, input domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domaindelivery.DeliveryBlueprint{}, err
	}
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaindelivery.DeliveryBlueprint{}, fmt.Errorf("delivery blueprint key and name are required")
	}
	beforeUsage, beforeUsageErr := s.GetDeliveryBlueprintUsage(ctx, principal, strings.TrimSpace(blueprintID))
	var beforeSnapshot *domaincatalog.TemplateUsageSummary
	if beforeUsageErr == nil {
		beforeSnapshot = &beforeUsage
	}
	item, err := s.repository.UpdateDeliveryBlueprint(ctx, strings.TrimSpace(blueprintID), input)
	if err == nil {
		usage, usageErr := s.GetDeliveryBlueprintUsage(ctx, principal, strings.TrimSpace(blueprintID))
		if usageErr != nil {
			usage = domaincatalog.TemplateUsageSummary{}
		}
		s.recordTemplateUsageChange(ctx, principal, "delivery.delivery_blueprint.update", "DeliveryBlueprint", item.ID, item.Name, "updated delivery blueprint", templateUsageAuditChangeSnapshot(beforeSnapshot, &usage))
	}
	return item, err
}

func (s *Service) GetDeliveryBlueprintUsage(ctx context.Context, principal domainidentity.Principal, blueprintID string) (domaincatalog.TemplateUsageSummary, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	blueprint, err := s.repository.GetDeliveryBlueprint(ctx, strings.TrimSpace(blueprintID))
	if err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	return s.deliveryBlueprintUsage(ctx, principal, blueprint)
}

func (s *Service) RenderDeliveryBlueprintSpec(ctx context.Context, principal domainidentity.Principal, blueprintID string) (domaindelivery.RenderedDeliverySpec, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domaindelivery.RenderedDeliverySpec{}, err
	}
	blueprint, err := s.repository.GetDeliveryBlueprint(ctx, strings.TrimSpace(blueprintID))
	if err != nil {
		return domaindelivery.RenderedDeliverySpec{}, err
	}
	return renderedSpecFromBlueprint(blueprint), nil
}

func (s *Service) BootstrapApplicationFromBlueprint(ctx context.Context, principal domainidentity.Principal, blueprintID string) (domaindelivery.BlueprintBootstrapResult, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domaindelivery.BlueprintBootstrapResult{}, err
	}
	blueprint, err := s.repository.GetDeliveryBlueprint(ctx, strings.TrimSpace(blueprintID))
	if err != nil {
		return domaindelivery.BlueprintBootstrapResult{}, err
	}
	spec := renderedSpecFromBlueprint(blueprint)
	app, services, bindings, err := s.applyRenderedDeliverySpec(ctx, principal, spec)
	if err != nil {
		return domaindelivery.BlueprintBootstrapResult{}, err
	}
	return domaindelivery.BlueprintBootstrapResult{
		Application:         app,
		Services:            services,
		EnvironmentBindings: bindings,
		Spec:                spec,
	}, nil
}

func (s *Service) CreateDeliveryDraft(ctx context.Context, principal domainidentity.Principal, input domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraft, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domaindelivery.DeliveryDraft{}, err
	}
	if strings.TrimSpace(input.ApplicationDraft.Name) == "" || strings.TrimSpace(input.ApplicationDraft.Key) == "" {
		return domaindelivery.DeliveryDraft{}, fmt.Errorf("%w: delivery draft application name and key are required", apperrors.ErrInvalidArgument)
	}
	return s.repository.CreateDeliveryDraft(ctx, input, principal.UserID)
}

func (s *Service) GetDeliveryDraft(ctx context.Context, principal domainidentity.Principal, draftID string) (domaindelivery.DeliveryDraft, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domaindelivery.DeliveryDraft{}, err
	}
	return s.repository.GetDeliveryDraft(ctx, strings.TrimSpace(draftID))
}

func (s *Service) ConfirmDeliveryDraft(ctx context.Context, principal domainidentity.Principal, draftID string) (domaindelivery.DeliveryDraftConfirmResult, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domaindelivery.DeliveryDraftConfirmResult{}, err
	}
	draft, err := s.repository.GetDeliveryDraft(ctx, strings.TrimSpace(draftID))
	if err != nil {
		return domaindelivery.DeliveryDraftConfirmResult{}, err
	}
	switch draft.Status {
	case domaindelivery.DeliveryDraftStatusConfirmed:
		return domaindelivery.DeliveryDraftConfirmResult{}, fmt.Errorf("%w: delivery draft is already confirmed", apperrors.ErrInvalidArgument)
	case domaindelivery.DeliveryDraftStatusConfirming:
		return domaindelivery.DeliveryDraftConfirmResult{}, fmt.Errorf("%w: delivery draft is already being confirmed", apperrors.ErrInvalidArgument)
	case domaindelivery.DeliveryDraftStatusDraft:
	default:
		return domaindelivery.DeliveryDraftConfirmResult{}, fmt.Errorf("%w: delivery draft status %s cannot be confirmed", apperrors.ErrInvalidArgument, draft.Status)
	}
	now := time.Now().UTC()
	draft.Status = domaindelivery.DeliveryDraftStatusConfirming
	draft.UpdatedAt = now
	draft, err = s.repository.UpdateDeliveryDraft(ctx, draft)
	if err != nil {
		return domaindelivery.DeliveryDraftConfirmResult{}, err
	}
	spec := renderedSpecFromDraft(draft)
	app, services, bindings, err := s.applyRenderedDeliverySpec(ctx, principal, spec)
	if err != nil {
		return domaindelivery.DeliveryDraftConfirmResult{}, s.restoreDeliveryDraftConfirmFailure(ctx, draft, err)
	}
	now = time.Now().UTC()
	draft.Status = domaindelivery.DeliveryDraftStatusConfirmed
	draft.ConfirmedAt = &now
	draft.UpdatedAt = now
	draft, err = s.repository.UpdateDeliveryDraft(ctx, draft)
	if err != nil {
		return domaindelivery.DeliveryDraftConfirmResult{}, err
	}
	return domaindelivery.DeliveryDraftConfirmResult{
		Draft:               draft,
		Application:         app,
		Services:            services,
		EnvironmentBindings: bindings,
		Spec:                spec,
	}, nil
}

func (s *Service) CreateDeliveryPlan(ctx context.Context, principal domainidentity.Principal, input domaindelivery.DeliveryPlanInput) (domaindelivery.DeliveryPlan, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domaindelivery.DeliveryPlan{}, err
	}
	action := normalizeApplicationDeliveryAction(input.Action)
	app, err := s.applications.Get(ctx, principal, strings.TrimSpace(input.ApplicationID))
	if err != nil {
		return domaindelivery.DeliveryPlan{}, err
	}
	bindingID := strings.TrimSpace(input.ApplicationEnvironmentID)
	if bindingID == "" {
		return domaindelivery.DeliveryPlan{}, fmt.Errorf("%w: applicationEnvironmentId is required", apperrors.ErrInvalidArgument)
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, bindingID)
	if err != nil {
		return domaindelivery.DeliveryPlan{}, err
	}
	if binding.ApplicationID != app.ID {
		return domaindelivery.DeliveryPlan{}, fmt.Errorf("%w: application environment does not belong to application", apperrors.ErrInvalidArgument)
	}
	target, err := selectReleaseTarget(binding, input.TargetID)
	if err != nil {
		return domaindelivery.DeliveryPlan{}, err
	}
	if action != domaindelivery.ApplicationDeliveryActionBuild && target == nil {
		return domaindelivery.DeliveryPlan{}, fmt.Errorf("%w: no enabled release target is configured", apperrors.ErrInvalidArgument)
	}
	environment := s.environmentForBinding(ctx, principal, binding)
	requiresApproval := requiresApproval(binding, environment)
	planInput := input
	planInput.Action = action
	planInput.ApplicationID = app.ID
	planInput.ApplicationName = app.Name
	planInput.ApplicationEnvironmentID = binding.ID
	planInput.EnvironmentKey = firstNonEmpty(environment.Key, binding.EnvironmentKey)
	planInput.TargetSummary = deliveryPlanTargetSummary(target)
	planInput.RiskLevel = deliveryPlanRiskLevel(action, environment, requiresApproval)
	planInput.RequiresApproval = requiresApproval
	planInput.Impact = deliveryPlanImpact(app, binding, environment, target, planInput)
	planInput.RollbackStrategy = deliveryPlanRollbackStrategy(action, binding)
	return s.repository.CreateDeliveryPlan(ctx, planInput, principal.UserID)
}

func (s *Service) GetDeliveryPlan(ctx context.Context, principal domainidentity.Principal, planID string) (domaindelivery.DeliveryPlan, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domaindelivery.DeliveryPlan{}, err
	}
	return s.repository.GetDeliveryPlan(ctx, strings.TrimSpace(planID))
}

func (s *Service) ConfirmDeliveryPlan(ctx context.Context, principal domainidentity.Principal, planID string) (domaindelivery.DeliveryPlanConfirmResult, error) {
	plan, err := s.repository.GetDeliveryPlan(ctx, strings.TrimSpace(planID))
	if err != nil {
		return domaindelivery.DeliveryPlanConfirmResult{}, err
	}
	switch plan.Status {
	case domaindelivery.DeliveryPlanStatusConfirmed:
		return domaindelivery.DeliveryPlanConfirmResult{}, fmt.Errorf("%w: delivery plan is already confirmed", apperrors.ErrInvalidArgument)
	case domaindelivery.DeliveryPlanStatusConfirming:
		return domaindelivery.DeliveryPlanConfirmResult{}, fmt.Errorf("%w: delivery plan is already being confirmed", apperrors.ErrInvalidArgument)
	case domaindelivery.DeliveryPlanStatusDraft:
	default:
		return domaindelivery.DeliveryPlanConfirmResult{}, fmt.Errorf("%w: delivery plan status %s cannot be confirmed", apperrors.ErrInvalidArgument, plan.Status)
	}
	if err := s.authorizeApplicationDeliveryAction(ctx, principal, plan.Action); err != nil {
		return domaindelivery.DeliveryPlanConfirmResult{}, err
	}
	now := time.Now().UTC()
	plan.Status = domaindelivery.DeliveryPlanStatusConfirming
	plan.UpdatedAt = now
	plan, err = s.repository.UpdateDeliveryPlan(ctx, plan)
	if err != nil {
		return domaindelivery.DeliveryPlanConfirmResult{}, err
	}
	result, err := s.TriggerApplicationDeliveryAction(ctx, principal, plan.ApplicationID, deliveryActionInputFromPlan(plan))
	if err != nil {
		return domaindelivery.DeliveryPlanConfirmResult{}, s.restoreDeliveryPlanConfirmFailure(ctx, plan, err)
	}
	now = time.Now().UTC()
	plan.Status = domaindelivery.DeliveryPlanStatusConfirmed
	plan.ConfirmedAt = &now
	plan.UpdatedAt = now
	plan, err = s.repository.UpdateDeliveryPlan(ctx, plan)
	if err != nil {
		return domaindelivery.DeliveryPlanConfirmResult{}, err
	}
	return domaindelivery.DeliveryPlanConfirmResult{
		Plan:   plan,
		Result: result,
	}, nil
}

func (s *Service) restoreDeliveryDraftConfirmFailure(ctx context.Context, draft domaindelivery.DeliveryDraft, cause error) error {
	draft.Status = domaindelivery.DeliveryDraftStatusDraft
	draft.UpdatedAt = time.Now().UTC()
	if _, err := s.repository.UpdateDeliveryDraft(ctx, draft); err != nil {
		return errors.Join(cause, fmt.Errorf("restore delivery draft confirmation state: %w", err))
	}
	return cause
}

func (s *Service) restoreDeliveryPlanConfirmFailure(ctx context.Context, plan domaindelivery.DeliveryPlan, cause error) error {
	plan.Status = domaindelivery.DeliveryPlanStatusDraft
	plan.UpdatedAt = time.Now().UTC()
	if _, err := s.repository.UpdateDeliveryPlan(ctx, plan); err != nil {
		return errors.Join(cause, fmt.Errorf("restore delivery plan confirmation state: %w", err))
	}
	return cause
}

func (s *Service) TriggerApplicationDeliveryAction(ctx context.Context, principal domainidentity.Principal, applicationID string, input domaindelivery.ApplicationDeliveryActionInput) (domaindelivery.ApplicationDeliveryActionResult, error) {
	action := normalizeApplicationDeliveryAction(input.Action)
	app, err := s.applications.Get(ctx, principal, strings.TrimSpace(applicationID))
	if err != nil {
		return domaindelivery.ApplicationDeliveryActionResult{}, err
	}
	bindingID := strings.TrimSpace(input.ApplicationEnvironmentID)
	if bindingID == "" {
		return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: applicationEnvironmentId is required", apperrors.ErrInvalidArgument)
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, bindingID)
	if err != nil {
		return domaindelivery.ApplicationDeliveryActionResult{}, err
	}
	if binding.ApplicationID != app.ID {
		return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: application environment does not belong to application", apperrors.ErrInvalidArgument)
	}
	target, err := selectReleaseTarget(binding, input.TargetID)
	if err != nil {
		return domaindelivery.ApplicationDeliveryActionResult{}, err
	}
	if action != domaindelivery.ApplicationDeliveryActionBuild && target == nil {
		return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: no enabled release target is configured", apperrors.ErrInvalidArgument)
	}
	if err := s.authorizeApplicationDeliveryAction(ctx, principal, action); err != nil {
		return domaindelivery.ApplicationDeliveryActionResult{}, err
	}
	result := domaindelivery.ApplicationDeliveryActionResult{
		Action:                   action,
		ApplicationID:            app.ID,
		ApplicationEnvironmentID: binding.ID,
		Target:                   target,
	}

	switch action {
	case domaindelivery.ApplicationDeliveryActionBuild:
		buildRecord, buildErr := s.triggerApplicationBuild(ctx, principal, app, binding, input)
		if buildErr != nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, buildErr
		}
		result.Build = &buildRecord
		applyBuildRelatedIDs(&result, buildRecord)
	case domaindelivery.ApplicationDeliveryActionDeploy:
		if target == nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: no enabled release target is configured", apperrors.ErrInvalidArgument)
		}
		releaseRecord, releaseErr := s.triggerApplicationRelease(ctx, principal, app, binding, *target, input)
		if releaseErr != nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, releaseErr
		}
		result.Release = &releaseRecord
		applyReleaseRelatedIDs(&result, releaseRecord)
	case domaindelivery.ApplicationDeliveryActionWorkflow:
		if target == nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: no enabled release target is configured", apperrors.ErrInvalidArgument)
		}
		run, runErr := s.workflows.Trigger(ctx, principal, workflowInputForDeliveryAction(app, binding, *target, input, action, false))
		if runErr != nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, runErr
		}
		result.Workflow = &run
		applyWorkflowRelatedIDs(&result, run)
	case domaindelivery.ApplicationDeliveryActionBuildDeploy:
		if target == nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: no enabled release target is configured", apperrors.ErrInvalidArgument)
		}
		if binding.WorkflowTemplate == nil || len(binding.WorkflowTemplate.Definition) == 0 {
			return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: workflow template is required", apperrors.ErrInvalidArgument)
		}
		run, runErr := s.workflows.Trigger(ctx, principal, workflowInputForDeliveryAction(app, binding, *target, input, action, false))
		if runErr != nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, runErr
		}
		result.Workflow = &run
		applyWorkflowRelatedIDs(&result, run)
	case domaindelivery.ApplicationDeliveryActionVerify:
		if target == nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: no enabled release target is configured", apperrors.ErrInvalidArgument)
		}
		if binding.WorkflowTemplate == nil || len(binding.WorkflowTemplate.Definition) == 0 {
			return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: workflow template is required", apperrors.ErrInvalidArgument)
		}
		run, runErr := s.workflows.TriggerValidation(ctx, principal, workflowInputForDeliveryAction(app, binding, *target, input, action, true))
		if runErr != nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, runErr
		}
		result.Workflow = &run
		applyWorkflowRelatedIDs(&result, run)
	case domaindelivery.ApplicationDeliveryActionRollback:
		if target == nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: no enabled release target is configured", apperrors.ErrInvalidArgument)
		}
		if binding.WorkflowTemplate == nil || len(binding.WorkflowTemplate.Definition) == 0 {
			return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: rollback workflow template is required", apperrors.ErrInvalidArgument)
		}
		run, runErr := s.workflows.TriggerRollback(ctx, principal, workflowInputForDeliveryAction(app, binding, *target, input, action, false))
		if runErr != nil {
			return domaindelivery.ApplicationDeliveryActionResult{}, runErr
		}
		result.Workflow = &run
		applyWorkflowRelatedIDs(&result, run)
	default:
		return domaindelivery.ApplicationDeliveryActionResult{}, fmt.Errorf("%w: unsupported application delivery action %q", apperrors.ErrInvalidArgument, action)
	}
	return result, nil
}

func normalizeApplicationDeliveryAction(action domaindelivery.ApplicationDeliveryActionKind) domaindelivery.ApplicationDeliveryActionKind {
	normalized := domaindelivery.ApplicationDeliveryActionKind(strings.TrimSpace(string(action)))
	if normalized == "" {
		return domaindelivery.ApplicationDeliveryActionBuildDeploy
	}
	return normalized
}

func (s *Service) authorizeApplicationDeliveryAction(ctx context.Context, principal domainidentity.Principal, action domaindelivery.ApplicationDeliveryActionKind) error {
	switch action {
	case domaindelivery.ApplicationDeliveryActionBuild:
		return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryBuildsTrigger)
	case domaindelivery.ApplicationDeliveryActionDeploy:
		return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryReleasesTrigger)
	case domaindelivery.ApplicationDeliveryActionWorkflow, domaindelivery.ApplicationDeliveryActionVerify, domaindelivery.ApplicationDeliveryActionRollback:
		return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryWorkflowsTrigger)
	case domaindelivery.ApplicationDeliveryActionBuildDeploy:
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryBuildsTrigger); err != nil {
			return err
		}
		return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryWorkflowsTrigger)
	default:
		return fmt.Errorf("%w: unsupported application delivery action %q", apperrors.ErrInvalidArgument, action)
	}
}

func (s *Service) triggerApplicationBuild(ctx context.Context, principal domainidentity.Principal, app domainapp.App, binding domaincatalog.ApplicationEnvironment, input domaindelivery.ApplicationDeliveryActionInput) (domainbuild.Record, error) {
	buildSourceID, _, refType, refName, imageTag := resolveDeliveryBuildDefaults(app, binding, input)
	return s.builds.Trigger(ctx, principal, domainbuild.TriggerInput{
		ApplicationID:            app.ID,
		ApplicationEnvironmentID: binding.ID,
		BuildSourceID:            buildSourceID,
		RefType:                  refType,
		RefName:                  refName,
		ImageTag:                 imageTag,
		BuildArgs:                mergeActionMaps(binding.BuildPolicy.BuildArgs, input.BuildArgs),
		Variables:                mergeActionMaps(binding.BuildPolicy.Variables, input.Variables),
	})
}

func (s *Service) triggerApplicationRelease(ctx context.Context, principal domainidentity.Principal, app domainapp.App, binding domaincatalog.ApplicationEnvironment, target domaincatalog.ReleaseTarget, input domaindelivery.ApplicationDeliveryActionInput) (domainrelease.Record, error) {
	_, buildSource, _, _, imageTag := resolveDeliveryBuildDefaults(app, binding, input)
	if imageTag == "" {
		return domainrelease.Record{}, fmt.Errorf("%w: imageTag or defaultTag is required", apperrors.ErrInvalidArgument)
	}
	return s.releases.Trigger(ctx, principal, domainrelease.TriggerInput{
		ApplicationID:            app.ID,
		ApplicationEnvironmentID: binding.ID,
		ReleaseBundleID:          metadataString(input.Variables, "releaseBundleId"),
		ClusterID:                target.ClusterID,
		Namespace:                target.Namespace,
		DeploymentName:           target.WorkloadName,
		ContainerName:            firstNonEmpty(input.ContainerName, target.ContainerName),
		ImageTag:                 imageTag,
		Image:                    resolveDeliveryImageRef(app, buildSource, imageTag),
		ReleaseName:              firstNonEmpty(input.ReleaseName, imageTag, binding.ID),
		ActionKind:               actionKindForBinding(binding, domaincatalog.Environment{}),
	})
}

func workflowInputForDeliveryAction(app domainapp.App, binding domaincatalog.ApplicationEnvironment, target domaincatalog.ReleaseTarget, input domaindelivery.ApplicationDeliveryActionInput, action domaindelivery.ApplicationDeliveryActionKind, validationOnly bool) domainworkflow.Input {
	workflowName := workflowTemplateName(binding)
	if workflowName == "" {
		workflowName = "build-release-verify"
	}
	buildSourceID, _, refType, refName, imageTag := resolveDeliveryBuildDefaults(app, binding, input)
	variables := mergeActionMaps(binding.BuildPolicy.Variables, input.Variables)
	if strings.TrimSpace(input.ReleaseBundleID) != "" {
		variables["releaseBundleId"] = strings.TrimSpace(input.ReleaseBundleID)
	}
	return domainworkflow.Input{
		ApplicationID:            app.ID,
		ApplicationEnvironmentID: binding.ID,
		WorkflowName:             workflowName,
		ClusterID:                target.ClusterID,
		Namespace:                target.Namespace,
		DeploymentName:           target.WorkloadName,
		BuildSourceID:            buildSourceID,
		RefType:                  refType,
		RefName:                  refName,
		ImageTag:                 imageTag,
		ReleaseName:              firstNonEmpty(input.ReleaseName, imageTag, binding.ID),
		ContainerName:            firstNonEmpty(input.ContainerName, target.ContainerName),
		Variables:                variables,
		BuildArgs:                mergeActionMaps(binding.BuildPolicy.BuildArgs, input.BuildArgs),
		TriggerBuild:             action == domaindelivery.ApplicationDeliveryActionBuildDeploy || action == domaindelivery.ApplicationDeliveryActionWorkflow,
		TriggerRelease:           action == domaindelivery.ApplicationDeliveryActionBuildDeploy,
		ValidationOnly:           validationOnly,
		RollbackOnly:             action == domaindelivery.ApplicationDeliveryActionRollback,
	}
}

func resolveDeliveryBuildDefaults(app domainapp.App, binding domaincatalog.ApplicationEnvironment, input domaindelivery.ApplicationDeliveryActionInput) (string, *domainapp.BuildSource, string, string, string) {
	buildSourceID := firstNonEmpty(input.BuildSourceID, binding.BuildPolicy.SourceID)
	buildSource := resolveBuildSource(app, buildSourceID)
	if buildSourceID == "" && buildSource != nil {
		buildSourceID = strings.TrimSpace(buildSource.ID)
	}
	refType := firstNonEmpty(input.RefType, binding.BuildPolicy.RefType, "branch")
	refName := firstNonEmpty(input.RefName, binding.BuildPolicy.RefValue, app.DefaultBranch, "main")
	imageTag := firstNonEmpty(input.ImageTag)
	if imageTag == "" && buildSource != nil {
		imageTag = strings.TrimSpace(buildSource.DefaultTag)
	}
	if imageTag == "" {
		imageTag = strings.TrimSpace(app.DefaultTag)
	}
	return buildSourceID, buildSource, refType, refName, imageTag
}

func resolveDeliveryImageRef(app domainapp.App, source *domainapp.BuildSource, imageTag string) string {
	base := strings.TrimSpace(app.BuildImage)
	if source != nil && strings.TrimSpace(source.BuildImage) != "" {
		base = strings.TrimSpace(source.BuildImage)
	}
	if base == "" {
		return ""
	}
	if strings.TrimSpace(imageTag) == "" {
		return base
	}
	return fmt.Sprintf("%s:%s", base, strings.TrimSpace(imageTag))
}

func selectReleaseTarget(binding domaincatalog.ApplicationEnvironment, targetID string) (*domaincatalog.ReleaseTarget, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID != "" {
		for _, item := range binding.Targets {
			if item.ID != targetID {
				continue
			}
			if !item.Enabled {
				return nil, fmt.Errorf("%w: release target is disabled", apperrors.ErrInvalidArgument)
			}
			copyItem := item
			return &copyItem, nil
		}
		return nil, fmt.Errorf("%w: release target %q not found", apperrors.ErrInvalidArgument, targetID)
	}
	for _, item := range binding.Targets {
		if item.Enabled {
			copyItem := item
			return &copyItem, nil
		}
	}
	return nil, nil
}

func (s *Service) environmentForBinding(ctx context.Context, principal domainidentity.Principal, binding domaincatalog.ApplicationEnvironment) domaincatalog.Environment {
	environments, err := s.catalog.ListEnvironments(ctx, principal)
	if err != nil {
		return domaincatalog.Environment{ID: binding.EnvironmentID, Key: binding.EnvironmentKey}
	}
	for _, item := range environments {
		if item.ID == binding.EnvironmentID {
			return item
		}
	}
	return domaincatalog.Environment{ID: binding.EnvironmentID, Key: binding.EnvironmentKey}
}

func deliveryPlanTargetSummary(target *domaincatalog.ReleaseTarget) string {
	if target == nil {
		return ""
	}
	return strings.Join([]string{
		strings.TrimSpace(target.ClusterID),
		strings.TrimSpace(target.Namespace),
		strings.TrimSpace(target.WorkloadName),
	}, " / ")
}

func deliveryPlanRiskLevel(action domaindelivery.ApplicationDeliveryActionKind, environment domaincatalog.Environment, requiresApproval bool) string {
	if action == domaindelivery.ApplicationDeliveryActionRollback || environment.IsProduction || requiresApproval {
		return "high"
	}
	switch action {
	case domaindelivery.ApplicationDeliveryActionBuild:
		return "low"
	case domaindelivery.ApplicationDeliveryActionVerify:
		return "medium"
	default:
		return "medium"
	}
}

func deliveryPlanRollbackStrategy(action domaindelivery.ApplicationDeliveryActionKind, binding domaincatalog.ApplicationEnvironment) string {
	switch action {
	case domaindelivery.ApplicationDeliveryActionBuild:
		return "Build only; no runtime rollback required."
	case domaindelivery.ApplicationDeliveryActionVerify:
		return "Verification only; retry validation after fixing evidence gaps."
	case domaindelivery.ApplicationDeliveryActionRollback:
		return "Rollback workflow uses the selected release bundle or previous runtime image."
	default:
		if binding.ReleasePolicy.AutoRollback {
			return "Auto rollback is enabled by release policy; confirm previous release bundle before manual rollback."
		}
		return "Use rollback context and previous release bundle if validation or rollout fails."
	}
}

func deliveryPlanImpact(app domainapp.App, binding domaincatalog.ApplicationEnvironment, environment domaincatalog.Environment, target *domaincatalog.ReleaseTarget, input domaindelivery.DeliveryPlanInput) map[string]any {
	impact := map[string]any{
		"applicationId":            app.ID,
		"applicationName":          app.Name,
		"applicationEnvironmentId": binding.ID,
		"environmentId":            binding.EnvironmentID,
		"environmentKey":           firstNonEmpty(environment.Key, binding.EnvironmentKey),
		"action":                   string(input.Action),
		"buildSourceId":            firstNonEmpty(input.BuildSourceID, binding.BuildPolicy.SourceID),
		"workflowTemplateId":       binding.WorkflowTemplateID,
		"requiresApproval":         input.RequiresApproval,
	}
	if strings.TrimSpace(input.RefName) != "" {
		impact["ref"] = map[string]any{"type": firstNonEmpty(input.RefType, "branch"), "name": strings.TrimSpace(input.RefName)}
	}
	if target != nil {
		impact["target"] = map[string]any{
			"id":           target.ID,
			"clusterId":    target.ClusterID,
			"namespace":    target.Namespace,
			"workloadKind": target.WorkloadKind,
			"workloadName": target.WorkloadName,
			"container":    firstNonEmpty(input.ContainerName, target.ContainerName),
		}
	}
	return impact
}

func (s *Service) deliveryBlueprintUsage(ctx context.Context, principal domainidentity.Principal, blueprint domaindelivery.DeliveryBlueprint) (domaincatalog.TemplateUsageSummary, error) {
	templateID := strings.TrimSpace(blueprint.ID)
	if templateID == "" {
		return domaincatalog.TemplateUsageSummary{}, fmt.Errorf("%w: blueprintID is required", apperrors.ErrInvalidArgument)
	}
	apps, err := s.applications.List(ctx, principal, domainapp.Filter{Limit: 500})
	if err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	bindings, err := s.catalog.ListApplicationEnvironments(ctx, principal)
	if err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	environments, err := s.catalog.ListEnvironments(ctx, principal)
	if err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	envByID := mapDeliveryEnvironmentsByID(environments)
	matchedApps := matchedBlueprintApplications(apps, blueprint)
	matchedAppIDs := make(map[string]struct{}, len(matchedApps))
	for _, app := range matchedApps {
		matchedAppIDs[app.ID] = struct{}{}
	}
	summary := domaincatalog.TemplateUsageSummary{
		TemplateKind:         domaincatalog.TemplateUsageKindBlueprint,
		TemplateID:           templateID,
		Applications:         make([]domaincatalog.TemplateUsageApplication, 0, len(matchedApps)),
		Bindings:             []domaincatalog.TemplateUsageBinding{},
		BuildSources:         []domaincatalog.TemplateUsageBuildSource{},
		FileKindCounts:       blueprintFileKindCounts(blueprint.Files),
		LastExecutionSummary: map[string]any{},
	}
	for _, app := range matchedApps {
		summary.Applications = append(summary.Applications, deliveryTemplateUsageApplication(app))
		for _, source := range app.BuildSources {
			summary.BuildSources = append(summary.BuildSources, domaincatalog.TemplateUsageBuildSource{
				ApplicationID:   app.ID,
				BuildSourceID:   source.ID,
				BuildSourceName: source.Name,
				Application:     deliveryTemplateUsageApplication(app),
				BindingCount:    bindingCountForApplication(bindings, app.ID),
				RiskLevel:       domaincatalog.TemplateUsageRiskLow,
			})
		}
	}
	environmentIDs := map[string]struct{}{}
	for _, binding := range bindings {
		if _, ok := matchedAppIDs[binding.ApplicationID]; !ok {
			continue
		}
		env := envByID[binding.EnvironmentID]
		requiresApproval := binding.ReleasePolicy.RequiresApproval || env.RequiresApproval
		risk := deliveryBindingUsageRisk(env, requiresApproval, len(binding.Targets))
		app := appByID(matchedApps, binding.ApplicationID)
		summary.Bindings = append(summary.Bindings, domaincatalog.TemplateUsageBinding{
			ID:               binding.ID,
			ApplicationID:    binding.ApplicationID,
			EnvironmentID:    binding.EnvironmentID,
			EnvironmentKey:   firstNonEmpty(strings.TrimSpace(binding.EnvironmentKey), env.Key),
			RequiresApproval: requiresApproval,
			TargetCount:      len(binding.Targets),
			RiskLevel:        risk,
			Application:      deliveryTemplateUsageApplication(app),
			Environment:      deliveryTemplateUsageEnvironment(env),
		})
		if binding.EnvironmentID != "" {
			environmentIDs[binding.EnvironmentID] = struct{}{}
		}
		if env.IsProduction {
			summary.ProductionEnvironmentCount++
		}
		if requiresApproval {
			summary.ApprovalBindingCount++
		}
		summary.TargetCount += len(binding.Targets)
	}
	summary.ApplicationCount = len(matchedApps)
	summary.EnvironmentCount = len(environmentIDs)
	summary.UsageCount = summary.ApplicationCount
	finalizeDeliveryTemplateUsageSummary(&summary, len(blueprint.EnvironmentBindings), len(blueprint.Files))
	summary.LastExecutionSummary = s.deliveryBlueprintRuntimeSummary(ctx, principal, matchedApps, summary.Bindings, summary.BuildSources)
	return summary, nil
}

func matchedBlueprintApplications(apps []domainapp.App, blueprint domaindelivery.DeliveryBlueprint) []domainapp.App {
	key := strings.TrimSpace(blueprint.ApplicationDraft.Key)
	name := strings.TrimSpace(blueprint.ApplicationDraft.Name)
	out := make([]domainapp.App, 0)
	for _, app := range apps {
		if key != "" && strings.TrimSpace(app.Key) == key {
			out = append(out, app)
			continue
		}
		if key == "" && name != "" && strings.TrimSpace(app.Name) == name {
			out = append(out, app)
		}
	}
	return out
}

func blueprintFileKindCounts(files []domaindelivery.BlueprintFileTemplate) map[string]int {
	if len(files) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, file := range files {
		kind := firstNonEmpty(strings.TrimSpace(file.Kind), "other")
		counts[kind]++
	}
	return counts
}

func mapDeliveryEnvironmentsByID(items []domaincatalog.Environment) map[string]domaincatalog.Environment {
	out := make(map[string]domaincatalog.Environment, len(items))
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}

func appByID(items []domainapp.App, id string) domainapp.App {
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	return domainapp.App{ID: id}
}

func bindingCountForApplication(bindings []domaincatalog.ApplicationEnvironment, applicationID string) int {
	count := 0
	for _, binding := range bindings {
		if binding.ApplicationID == applicationID {
			count++
		}
	}
	return count
}

func deliveryTemplateUsageApplication(app domainapp.App) domaincatalog.TemplateUsageApplication {
	return domaincatalog.TemplateUsageApplication{
		ID:             app.ID,
		Name:           app.Name,
		Key:            app.Key,
		BusinessLineID: app.BusinessLineID,
		Group:          app.Group,
	}
}

func deliveryTemplateUsageEnvironment(env domaincatalog.Environment) domaincatalog.TemplateUsageEnvironment {
	return domaincatalog.TemplateUsageEnvironment{
		ID:               env.ID,
		Key:              env.Key,
		Name:             env.Name,
		IsProduction:     env.IsProduction,
		RequiresApproval: env.RequiresApproval,
	}
}

func deliveryBindingUsageRisk(env domaincatalog.Environment, requiresApproval bool, targetCount int) domaincatalog.TemplateUsageRiskLevel {
	if env.IsProduction || requiresApproval {
		return domaincatalog.TemplateUsageRiskHigh
	}
	if targetCount > 1 {
		return domaincatalog.TemplateUsageRiskMedium
	}
	return domaincatalog.TemplateUsageRiskLow
}

func maxDeliveryTemplateUsageRisk(current, next domaincatalog.TemplateUsageRiskLevel) domaincatalog.TemplateUsageRiskLevel {
	if deliveryTemplateRiskWeight(next) > deliveryTemplateRiskWeight(current) {
		return next
	}
	return current
}

func deliveryTemplateRiskWeight(risk domaincatalog.TemplateUsageRiskLevel) int {
	switch risk {
	case domaincatalog.TemplateUsageRiskHigh:
		return 3
	case domaincatalog.TemplateUsageRiskMedium:
		return 2
	case domaincatalog.TemplateUsageRiskLow:
		return 1
	default:
		return 0
	}
}

func finalizeDeliveryTemplateUsageSummary(summary *domaincatalog.TemplateUsageSummary, plannedBindingCount, fileCount int) {
	risk := domaincatalog.TemplateUsageRiskLow
	for _, binding := range summary.Bindings {
		risk = maxDeliveryTemplateUsageRisk(risk, binding.RiskLevel)
	}
	for _, source := range summary.BuildSources {
		risk = maxDeliveryTemplateUsageRisk(risk, source.RiskLevel)
	}
	if summary.UsageCount == 0 {
		risk = domaincatalog.TemplateUsageRiskLow
	}
	summary.RiskLevel = risk
	reasons := make([]string, 0)
	if summary.ProductionEnvironmentCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d production environment bindings", summary.ProductionEnvironmentCount))
	}
	if summary.ApprovalBindingCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d approval-gated bindings", summary.ApprovalBindingCount))
	}
	if summary.TargetCount > summary.UsageCount && summary.TargetCount > 1 {
		reasons = append(reasons, fmt.Sprintf("%d release targets", summary.TargetCount))
	}
	if plannedBindingCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d default environment binding templates", plannedBindingCount))
	}
	if fileCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d spec file templates", fileCount))
	}
	if summary.UsageCount == 0 {
		reasons = append(reasons, "no created applications matched this blueprint")
	}
	summary.RiskReasons = reasons
	switch risk {
	case domaincatalog.TemplateUsageRiskHigh:
		summary.RecommendedAction = "copy_template_before_editing"
	case domaincatalog.TemplateUsageRiskMedium:
		summary.RecommendedAction = "review_impact_before_saving"
	default:
		summary.RecommendedAction = "save_with_standard_review"
	}
	summary.LastExecutionSummary = staticDeliveryTemplateUsageRuntimeSummary("delivery_blueprint_matched_applications", "blueprint usage is inferred from application key and application environment bindings")
}

func staticDeliveryTemplateUsageRuntimeSummary(source, note string) map[string]any {
	return map[string]any{
		"source":       source,
		"note":         note,
		"items":        []map[string]any{},
		"statusCounts": map[string]int{},
		"stateCounts":  map[string]int{},
	}
}

type deliveryTemplateUsageRuntimeCollector struct {
	source       string
	items        []map[string]any
	statusCounts map[string]int
	stateCounts  map[string]int
	kindCounts   map[string]int
	latest       map[string]any
	latestAt     time.Time
}

func newDeliveryTemplateUsageRuntimeCollector(source string) *deliveryTemplateUsageRuntimeCollector {
	return &deliveryTemplateUsageRuntimeCollector{
		source:       source,
		items:        []map[string]any{},
		statusCounts: map[string]int{},
		stateCounts:  map[string]int{},
		kindCounts:   map[string]int{},
	}
}

func (c *deliveryTemplateUsageRuntimeCollector) add(kind, id, status string, eventAt time.Time, fields map[string]any) {
	if c == nil || strings.TrimSpace(id) == "" {
		return
	}
	item := map[string]any{
		"kind":   strings.TrimSpace(kind),
		"id":     strings.TrimSpace(id),
		"status": strings.TrimSpace(status),
	}
	if !eventAt.IsZero() {
		item["observedAt"] = eventAt.Format(time.RFC3339)
	}
	for key, value := range fields {
		if value == nil {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			continue
		}
		item[key] = value
	}
	statusKey := firstNonEmpty(strings.TrimSpace(status), "unknown")
	stateKey := deliveryTemplateUsageRuntimeState(statusKey)
	c.statusCounts[statusKey]++
	c.stateCounts[stateKey]++
	c.kindCounts[firstNonEmpty(strings.TrimSpace(kind), "unknown")]++
	if len(c.items) < 20 {
		c.items = append(c.items, item)
	}
	if c.latest == nil || (!eventAt.IsZero() && (c.latestAt.IsZero() || eventAt.After(c.latestAt))) {
		c.latest = item
		c.latestAt = eventAt
	}
}

func (c *deliveryTemplateUsageRuntimeCollector) summary(note string) map[string]any {
	if c == nil {
		return nil
	}
	out := map[string]any{
		"source":       c.source,
		"items":        c.items,
		"statusCounts": c.statusCounts,
		"stateCounts":  c.stateCounts,
		"kindCounts":   c.kindCounts,
	}
	if strings.TrimSpace(note) != "" {
		out["note"] = strings.TrimSpace(note)
	}
	if c.latest != nil {
		out["latest"] = c.latest
	}
	if !c.latestAt.IsZero() {
		out["latestAt"] = c.latestAt.Format(time.RFC3339)
	}
	return out
}

func deliveryTemplateUsageRuntimeState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "succeeded", "success", "ready", "deployed":
		return "succeeded"
	case "failed", "failure", "error", "errored", "canceled", "cancelled", "callback_timeout", "timeout", "timed_out":
		return "failed"
	case "running", "dispatching", "building", "releasing", "deploying", "in_progress", "processing":
		return "running"
	case "queued", "pending", "waiting", "waiting_approval", "blocked":
		return "pending"
	default:
		if strings.TrimSpace(status) == "" {
			return "unknown"
		}
		return "unknown"
	}
}

func (s *Service) deliveryBlueprintRuntimeSummary(ctx context.Context, principal domainidentity.Principal, apps []domainapp.App, bindings []domaincatalog.TemplateUsageBinding, sources []domaincatalog.TemplateUsageBuildSource) map[string]any {
	if len(apps) == 0 {
		return staticDeliveryTemplateUsageRuntimeSummary("delivery_blueprint_matched_applications", "no created applications matched this blueprint")
	}
	collector := newDeliveryTemplateUsageRuntimeCollector("delivery_blueprint_runtime")
	for _, app := range apps {
		if strings.TrimSpace(app.ID) == "" {
			continue
		}
		if builds, err := s.builds.List(ctx, principal, domainbuild.Filter{ApplicationID: app.ID, Limit: 20}); err == nil {
			for _, build := range builds {
				collector.add("build", build.ID, build.Status, build.CreatedAt, map[string]any{
					"applicationId":            build.ApplicationID,
					"applicationEnvironmentId": metadataString(build.Metadata, "applicationEnvironmentId"),
					"buildSourceId":            metadataString(build.Metadata, "buildSourceId"),
					"releaseBundleId":          metadataString(build.Metadata, "releaseBundleId"),
					"executionTaskId":          metadataString(build.Metadata, "executionTaskId"),
					"sourceSystem":             build.SourceSystem,
				})
			}
		}
		if workflows, err := s.workflows.List(ctx, principal, app.ID, 20); err == nil {
			for _, run := range workflows {
				collector.add("workflow", run.ID, run.Status, parseDeliveryTemplateUsageTime(run.UpdatedAt, run.CreatedAt), map[string]any{
					"applicationId":            run.ApplicationID,
					"applicationEnvironmentId": metadataString(run.Metadata, "bindingId"),
					"workflowName":             run.WorkflowName,
					"workflowTemplateId":       metadataString(run.Metadata, "workflowTemplateId"),
				})
			}
		}
		if releases, err := s.releases.List(ctx, principal, domainrelease.Filter{ApplicationID: app.ID, Limit: 20}); err == nil {
			for _, release := range releases {
				if !deliveryReleaseMatchesUsageBindings(release, bindings) {
					continue
				}
				collector.add("release", release.ID, release.Status, release.CreatedAt, map[string]any{
					"applicationId":            release.ApplicationID,
					"applicationEnvironmentId": metadataString(release.Metadata, "applicationEnvironmentId"),
					"releaseBundleId":          metadataString(release.Metadata, "releaseBundleId"),
					"executionTaskId":          metadataString(release.Metadata, "executionTaskId"),
					"clusterId":                release.ClusterID,
					"namespace":                release.Namespace,
					"deploymentName":           release.DeploymentName,
				})
			}
		}
		if bundles, err := s.repository.ListReleaseBundles(ctx, domaindelivery.ReleaseBundleFilter{ApplicationID: app.ID, Limit: 20}); err == nil {
			for _, bundle := range bundles {
				if !deliveryBundleMatchesUsageBindings(bundle, bindings) && !deliveryBundleMatchesBuildSources(bundle, sources) {
					continue
				}
				collector.add("release_bundle", bundle.ID, bundle.Status, bundle.UpdatedAt, map[string]any{
					"applicationId":            bundle.ApplicationID,
					"applicationEnvironmentId": bundle.ApplicationEnvironmentID,
					"version":                  bundle.Version,
					"sourceType":               bundle.SourceType,
					"artifactRef":              bundle.ArtifactRef,
				})
			}
		}
		if tasks, err := s.repository.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: app.ID, Limit: 20}); err == nil {
			tasks = domaindelivery.WithOperationStates(tasks, time.Now().UTC())
			for _, task := range tasks {
				if !deliveryTaskMatchesUsageBindings(task, bindings) && !deliveryTaskMatchesBuildSources(task, sources) {
					continue
				}
				collector.add("execution_task", task.ID, task.Status, task.UpdatedAt, map[string]any{
					"applicationId":            task.ApplicationID,
					"applicationEnvironmentId": task.ApplicationEnvironmentID,
					"releaseBundleId":          task.ReleaseBundleID,
					"taskKind":                 task.TaskKind,
					"providerKind":             task.ProviderKind,
					"operationState":           task.OperationState,
				})
			}
		}
	}
	return collector.summary("latest onboarding, build, workflow, release, and execution evidence for matched applications")
}

func deliveryReleaseMatchesUsageBindings(record domainrelease.Record, bindings []domaincatalog.TemplateUsageBinding) bool {
	bindingID := metadataString(record.Metadata, "applicationEnvironmentId")
	for _, binding := range bindings {
		if strings.TrimSpace(record.ApplicationID) != strings.TrimSpace(binding.ApplicationID) {
			continue
		}
		if bindingID != "" && bindingID == strings.TrimSpace(binding.ID) {
			return true
		}
		if bindingID == "" && strings.TrimSpace(record.ClusterID) != "" && strings.TrimSpace(record.Namespace) != "" && strings.TrimSpace(record.DeploymentName) != "" {
			return true
		}
	}
	return false
}

func deliveryBundleMatchesUsageBindings(bundle domaindelivery.ReleaseBundle, bindings []domaincatalog.TemplateUsageBinding) bool {
	for _, binding := range bindings {
		if strings.TrimSpace(bundle.ApplicationID) != strings.TrimSpace(binding.ApplicationID) {
			continue
		}
		if strings.TrimSpace(bundle.ApplicationEnvironmentID) == "" || strings.TrimSpace(bundle.ApplicationEnvironmentID) == strings.TrimSpace(binding.ID) {
			return true
		}
	}
	return false
}

func deliveryBundleMatchesBuildSources(bundle domaindelivery.ReleaseBundle, sources []domaincatalog.TemplateUsageBuildSource) bool {
	buildSourceID := metadataString(bundle.Metadata, "buildSourceId")
	if buildSourceID == "" {
		return false
	}
	for _, source := range sources {
		if strings.TrimSpace(bundle.ApplicationID) == strings.TrimSpace(source.ApplicationID) && buildSourceID == strings.TrimSpace(source.BuildSourceID) {
			return true
		}
	}
	return false
}

func deliveryTaskMatchesUsageBindings(task domaindelivery.ExecutionTask, bindings []domaincatalog.TemplateUsageBinding) bool {
	for _, binding := range bindings {
		if strings.TrimSpace(task.ApplicationID) != strings.TrimSpace(binding.ApplicationID) {
			continue
		}
		if strings.TrimSpace(task.ApplicationEnvironmentID) == "" || strings.TrimSpace(task.ApplicationEnvironmentID) == strings.TrimSpace(binding.ID) {
			return true
		}
	}
	return false
}

func deliveryTaskMatchesBuildSources(task domaindelivery.ExecutionTask, sources []domaincatalog.TemplateUsageBuildSource) bool {
	buildSourceID := metadataString(task.Payload, "buildSourceId")
	if buildSourceID == "" {
		buildSourceID = metadataString(task.Result, "buildSourceId")
	}
	if buildSourceID == "" {
		return false
	}
	for _, source := range sources {
		if strings.TrimSpace(task.ApplicationID) == strings.TrimSpace(source.ApplicationID) && buildSourceID == strings.TrimSpace(source.BuildSourceID) {
			return true
		}
	}
	return false
}

func parseDeliveryTemplateUsageTime(values ...string) time.Time {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func (s *Service) recordTemplateUsageChange(ctx context.Context, principal domainidentity.Principal, operationType, resourceKind, targetID, targetLabel, summary string, usageSnapshot map[string]any) {
	meta := requestctx.FromContext(ctx)
	auditMetadata := map[string]any{
		"targetId": targetID,
		"source":   meta.Source,
	}
	operationMetadata := map[string]any{
		"targetId": targetID,
	}
	if usageSnapshot != nil {
		auditMetadata["usageSnapshot"] = usageSnapshot
		operationMetadata["usageSnapshot"] = usageSnapshot
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID:       principal.UserID,
			ActorName:     principal.UserName,
			Roles:         principal.Roles,
			Teams:         principal.Teams,
			ResourceKind:  resourceKind,
			ResourceName:  targetLabel,
			Action:        strings.TrimPrefix(operationType, "delivery."),
			Result:        "success",
			Summary:       summary,
			RequestPath:   meta.Path,
			RequestMethod: meta.Method,
			RequestID:     meta.RequestID,
			SourceIP:      meta.SourceIP,
			Metadata:      auditMetadata,
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(
			ctx,
			principal,
			operationType,
			map[string]any{
				"module":       "delivery",
				"resourceKind": resourceKind,
				"targetId":     targetID,
				"targetLabel":  targetLabel,
			},
			"success",
			summary,
			operationMetadata,
		))
	}
}

func templateUsageAuditSnapshot(usage domaincatalog.TemplateUsageSummary) map[string]any {
	if strings.TrimSpace(usage.TemplateID) == "" {
		return nil
	}
	return map[string]any{
		"templateKind":               usage.TemplateKind,
		"templateId":                 usage.TemplateID,
		"usageCount":                 usage.UsageCount,
		"applicationCount":           usage.ApplicationCount,
		"environmentCount":           usage.EnvironmentCount,
		"productionEnvironmentCount": usage.ProductionEnvironmentCount,
		"approvalBindingCount":       usage.ApprovalBindingCount,
		"targetCount":                usage.TargetCount,
		"riskLevel":                  usage.RiskLevel,
		"riskReasons":                usage.RiskReasons,
		"recommendedAction":          usage.RecommendedAction,
		"lastExecutionSummary":       usage.LastExecutionSummary,
	}
}

func templateUsageAuditChangeSnapshot(before, after *domaincatalog.TemplateUsageSummary) map[string]any {
	if after == nil && before == nil {
		return nil
	}
	if after == nil {
		return templateUsageAuditSnapshot(*before)
	}
	snapshot := templateUsageAuditSnapshot(*after)
	if before != nil && snapshot != nil {
		snapshot["before"] = templateUsageAuditSnapshot(*before)
		snapshot["after"] = templateUsageAuditSnapshot(*after)
	}
	return snapshot
}

func deliveryActionInputFromPlan(plan domaindelivery.DeliveryPlan) domaindelivery.ApplicationDeliveryActionInput {
	return domaindelivery.ApplicationDeliveryActionInput{
		Action:                   plan.Action,
		ApplicationEnvironmentID: plan.ApplicationEnvironmentID,
		TargetID:                 plan.TargetID,
		BuildSourceID:            plan.BuildSourceID,
		ReleaseBundleID:          plan.ReleaseBundleID,
		RefType:                  plan.RefType,
		RefName:                  plan.RefName,
		ImageTag:                 plan.ImageTag,
		ReleaseName:              plan.ReleaseName,
		ContainerName:            plan.ContainerName,
		Variables:                ensureMap(plan.Variables),
		BuildArgs:                ensureMap(plan.BuildArgs),
	}
}

func (s *Service) ClaimExecutionTask(ctx context.Context, providerKinds []string, agentID, runtimeEndpoint string) (domaindelivery.ExecutionTask, error) {
	if s.execution != nil {
		return s.execution.ClaimExecutionTask(ctx, providerKinds, strings.TrimSpace(agentID), strings.TrimSpace(runtimeEndpoint))
	}
	task, err := s.repository.ClaimExecutionTask(ctx, providerKinds, strings.TrimSpace(agentID), strings.TrimSpace(runtimeEndpoint))
	return domaindelivery.WithOperationState(task, time.Now().UTC()), err
}

func (s *Service) RecordCallback(ctx context.Context, input domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionTask, error) {
	if s.execution != nil {
		return s.execution.RecordCallback(ctx, input)
	}
	task, err := s.repository.GetExecutionTaskByCallbackToken(ctx, strings.TrimSpace(input.CallbackToken))
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	callback := domaindelivery.ExecutionCallback{
		ID:              uuid.NewString(),
		ExecutionTaskID: task.ID,
		ProviderKind:    task.ProviderKind,
		Status:          strings.TrimSpace(input.Status),
		Payload:         ensureMap(input.Payload),
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.repository.CreateExecutionCallback(ctx, callback); err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	now := time.Now().UTC()
	if task.Status == "queued" || task.Status == "dispatching" {
		task.StartedAt = &now
	}
	task.Status = firstNonEmpty(strings.TrimSpace(input.Status), task.Status)
	task.AttemptCount = maxInt(task.AttemptCount, 1)
	task.Result = mergeMaps(task.Result, ensureMap(input.Payload))
	if task.Status == "completed" || task.Status == "failed" {
		task.FinishedAt = &now
	}
	task.UpdatedAt = now
	updated, err := s.repository.UpdateExecutionTask(ctx, task)
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if strings.TrimSpace(updated.ReleaseBundleID) != "" && (updated.Status == "completed" || updated.Status == "failed") {
		bundle, bundleErr := s.repository.GetReleaseBundle(ctx, updated.ReleaseBundleID)
		if bundleErr == nil {
			bundle.Status = updated.Status
			bundle.Metadata = mergeMaps(bundle.Metadata, updated.Result)
			bundle.UpdatedAt = now
			_, _ = s.repository.UpdateReleaseBundle(ctx, bundle)
		}
	}
	return domaindelivery.WithOperationState(updated, time.Now().UTC()), nil
}

func (s *Service) GetExecutionTaskForRunner(ctx context.Context, taskID string) (domaindelivery.ExecutionTask, error) {
	task, err := s.repository.GetExecutionTask(ctx, strings.TrimSpace(taskID))
	return domaindelivery.WithOperationState(task, time.Now().UTC()), err
}

func (s *Service) CancelExecutionTask(ctx context.Context, principal domainidentity.Principal, taskID string, input domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksManage); err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if s.execution == nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("execution task controller is not configured")
	}
	return s.execution.CancelExecutionTask(ctx, strings.TrimSpace(taskID), input)
}

func (s *Service) RetryExecutionTask(ctx context.Context, principal domainidentity.Principal, taskID string, input domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksManage); err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	if s.execution == nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("execution task controller is not configured")
	}
	return s.execution.RetryExecutionTask(ctx, strings.TrimSpace(taskID), input)
}

func ensureMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func mergeActionMaps(base, override map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		result[key] = value
	}
	return result
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	next := ensureMap(base)
	for key, value := range ensureMap(overlay) {
		next[key] = value
	}
	return next
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt(values ...int) int {
	result := 0
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func applyBuildRelatedIDs(result *domaindelivery.ApplicationDeliveryActionResult, record domainbuild.Record) {
	applyRelatedIDsFromMetadata(result, record.Metadata)
}

func applyReleaseRelatedIDs(result *domaindelivery.ApplicationDeliveryActionResult, record domainrelease.Record) {
	applyRelatedIDsFromMetadata(result, record.Metadata)
}

func applyWorkflowRelatedIDs(result *domaindelivery.ApplicationDeliveryActionResult, run domainworkflow.Run) {
	if result == nil {
		return
	}
	if strings.TrimSpace(run.ID) != "" {
		result.RelatedIDs.WorkflowRunID = strings.TrimSpace(run.ID)
	}
}

func applyRelatedIDsFromMetadata(result *domaindelivery.ApplicationDeliveryActionResult, metadata map[string]any) {
	if result == nil {
		return
	}
	if bundleID := metadataString(metadata, "releaseBundleId"); bundleID != "" {
		result.RelatedIDs.ReleaseBundleID = bundleID
	}
	if taskID := metadataString(metadata, "executionTaskId"); taskID != "" {
		result.RelatedIDs.ExecutionTaskID = taskID
	}
}

func (s *Service) artifactsForRuntimeObject(ctx context.Context, filter domaindelivery.ArtifactFilter) []domaindelivery.ExecutionArtifact {
	if strings.TrimSpace(filter.ExecutionTaskID) == "" &&
		strings.TrimSpace(filter.ReleaseBundleID) == "" &&
		strings.TrimSpace(filter.WorkflowRunID) == "" &&
		strings.TrimSpace(filter.ApplicationID) == "" {
		return nil
	}
	items, err := s.repository.ListArtifacts(ctx, filter)
	if err != nil {
		return nil
	}
	return items
}

func (s *Service) buildRuntimeObjectDetail(
	ctx context.Context,
	principal domainidentity.Principal,
	kind string,
	id string,
	applicationID string,
	bindingID string,
	object any,
	metadata map[string]any,
	artifacts []domaindelivery.ExecutionArtifact,
	evidence map[string]any,
) (domaindelivery.RuntimeObjectDetail, error) {
	applicationID = strings.TrimSpace(applicationID)
	bindingID = firstNonEmpty(bindingID, metadataString(metadata, "applicationEnvironmentId"), metadataString(metadata, "bindingId"))
	app, err := s.applications.Get(ctx, principal, applicationID)
	if err != nil {
		return domaindelivery.RuntimeObjectDetail{}, err
	}
	binding, environment := s.runtimeBindingContext(ctx, principal, app.ID, bindingID)
	var buildSource *domainapp.BuildSource
	buildSourceID := firstNonEmpty(metadataString(metadata, "buildSourceId"))
	if binding != nil {
		buildSourceID = firstNonEmpty(buildSourceID, binding.BuildPolicy.SourceID)
	}
	buildSource = resolveBuildSource(app, buildSourceID)
	var workflowTemplate *domaincatalog.WorkflowTemplate
	if binding != nil {
		workflowTemplate = binding.WorkflowTemplate
	}
	if evidence == nil {
		evidence = map[string]any{}
	}
	if len(artifacts) > 0 {
		evidence["artifactCount"] = len(artifacts)
	}
	return domaindelivery.RuntimeObjectDetail{
		Kind:             kind,
		ID:               strings.TrimSpace(id),
		Object:           object,
		Application:      &app,
		Binding:          binding,
		Environment:      environment,
		BuildSource:      buildSource,
		WorkflowTemplate: workflowTemplate,
		Evidence:         evidence,
		Artifacts:        artifacts,
		Links:            runtimeObjectLinks(kind, strings.TrimSpace(id), app.ID),
		Permissions:      s.runtimeObjectPermissions(ctx, principal, kind),
	}, nil
}

func (s *Service) runtimeBindingContext(ctx context.Context, principal domainidentity.Principal, applicationID, bindingID string) (*domaincatalog.ApplicationEnvironment, *domaincatalog.Environment) {
	bindings, err := s.catalog.ListApplicationEnvironments(ctx, principal)
	if err != nil {
		return nil, nil
	}
	environments, err := s.catalog.ListEnvironments(ctx, principal)
	if err != nil {
		environments = nil
	}
	envByID := make(map[string]domaincatalog.Environment, len(environments))
	for _, item := range environments {
		envByID[item.ID] = item
	}
	var fallback *domaincatalog.ApplicationEnvironment
	for _, item := range bindings {
		if strings.TrimSpace(item.ApplicationID) != strings.TrimSpace(applicationID) {
			continue
		}
		copyItem := item
		if fallback == nil {
			fallback = &copyItem
		}
		if strings.TrimSpace(bindingID) != "" && strings.TrimSpace(item.ID) == strings.TrimSpace(bindingID) {
			env := envByID[item.EnvironmentID]
			if env.ID == "" {
				return &copyItem, nil
			}
			return &copyItem, &env
		}
	}
	if strings.TrimSpace(bindingID) == "" && fallback != nil {
		env := envByID[fallback.EnvironmentID]
		if env.ID == "" {
			return fallback, nil
		}
		return fallback, &env
	}
	return nil, nil
}

func runtimeObjectLinks(kind, id, applicationID string) domaindelivery.RuntimeObjectLinks {
	focusKey := map[string]string{
		"build":          "buildId",
		"workflow":       "workflowRunId",
		"release":        "releaseId",
		"release_bundle": "releaseBundleId",
		"execution_task": "executionTaskId",
	}[kind]
	encodedID := url.QueryEscape(strings.TrimSpace(id))
	application := "/applications/" + url.PathEscape(strings.TrimSpace(applicationID)) + "?tab=delivery"
	if focusKey != "" && strings.TrimSpace(id) != "" {
		application += "&" + focusKey + "=" + encodedID
	}
	return domaindelivery.RuntimeObjectLinks{
		Application: application,
		Audit:       "/system/audit?metadataKey=runtime." + url.QueryEscape(kind) + ".id&metadataValue=" + encodedID,
		Operations:  "/system/operations?metadataKey=runtime." + url.QueryEscape(kind) + ".id&metadataValue=" + encodedID,
		Artifacts:   runtimeArtifactLink(kind, id),
	}
}

func runtimeArtifactLink(kind, id string) string {
	switch kind {
	case "workflow":
		return "/delivery/artifacts?workflowRunId=" + url.QueryEscape(strings.TrimSpace(id))
	case "release_bundle":
		return "/delivery/artifacts?releaseBundleId=" + url.QueryEscape(strings.TrimSpace(id))
	case "execution_task":
		return "/delivery/artifacts?executionTaskId=" + url.QueryEscape(strings.TrimSpace(id))
	default:
		return "/delivery/artifacts"
	}
}

func (s *Service) runtimeObjectPermissions(ctx context.Context, principal domainidentity.Principal, kind string) domaindelivery.RuntimeObjectPermissions {
	return domaindelivery.RuntimeObjectPermissions{
		CanViewArtifacts:  s.hasRuntimePermission(ctx, principal, appaccess.PermDeliveryExecutionTasksView),
		CanViewAudit:      s.hasRuntimePermission(ctx, principal, appaccess.PermSystemAuditView),
		CanViewOperations: s.hasRuntimePermission(ctx, principal, appaccess.PermSystemOperationsView),
		CanRetry:          kind == "execution_task" && s.hasRuntimePermission(ctx, principal, appaccess.PermDeliveryExecutionTasksManage),
		CanCancel:         kind == "execution_task" && s.hasRuntimePermission(ctx, principal, appaccess.PermDeliveryExecutionTasksManage),
	}
}

func (s *Service) hasRuntimePermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) bool {
	if s.permissions == nil {
		return true
	}
	allowed, err := s.permissions.HasPermission(ctx, principal, permissionKey)
	return err == nil && allowed
}

func (s *Service) loadDeliveryContext(ctx context.Context, principal domainidentity.Principal, applicationID string) ([]domaincatalog.ApplicationEnvironment, []domaincatalog.Environment, []domaindelivery.ReleaseBundle, []domaindelivery.ExecutionTask, []domainbuild.Record, []domainworkflow.Run, []domainrelease.Record, error) {
	bindings, err := s.catalog.ListApplicationEnvironments(ctx, principal)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}
	environments, err := s.catalog.ListEnvironments(ctx, principal)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}
	bundles, err := s.repository.ListReleaseBundles(ctx, domaindelivery.ReleaseBundleFilter{ApplicationID: applicationID, Limit: 20})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}
	tasks, err := s.repository.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: applicationID, Limit: 20})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}
	tasks = domaindelivery.WithOperationStates(tasks, time.Now().UTC())
	builds, err := s.builds.List(ctx, principal, domainbuild.Filter{ApplicationID: applicationID, Limit: 20})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}
	workflows, err := s.workflows.List(ctx, principal, applicationID, 20)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}
	releases, err := s.releases.List(ctx, principal, domainrelease.Filter{ApplicationID: applicationID, Limit: 20})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, err
	}
	return bindings, environments, bundles, tasks, builds, workflows, releases, nil
}

func resolveBuildSource(app domainapp.App, sourceID string) *domainapp.BuildSource {
	sourceID = strings.TrimSpace(sourceID)
	for _, item := range app.BuildSources {
		if sourceID != "" && item.ID == sourceID {
			copyItem := item
			return &copyItem
		}
	}
	for _, item := range app.BuildSources {
		if item.IsDefault {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func actionKindForBinding(binding domaincatalog.ApplicationEnvironment, environment domaincatalog.Environment) string {
	if strings.TrimSpace(binding.ReleasePolicy.ActionKind) != "" {
		return strings.TrimSpace(binding.ReleasePolicy.ActionKind)
	}
	if environment.IsProduction {
		return "release"
	}
	return "deploy"
}

func requiresApproval(binding domaincatalog.ApplicationEnvironment, environment domaincatalog.Environment) bool {
	return binding.ReleasePolicy.RequiresApproval || environment.RequiresApproval
}

func workflowTemplateName(binding domaincatalog.ApplicationEnvironment) string {
	if binding.WorkflowTemplate != nil && strings.TrimSpace(binding.WorkflowTemplate.Name) != "" {
		return binding.WorkflowTemplate.Name
	}
	return strings.TrimSpace(binding.WorkflowTemplateID)
}

func latestBuildForApplication(applicationID string, items []domainbuild.Record) *domainbuild.Record {
	for _, item := range items {
		if item.ApplicationID == applicationID {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func latestWorkflowForApplication(applicationID string, items []domainworkflow.Run) *domainworkflow.Run {
	for _, item := range items {
		if item.ApplicationID == applicationID {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func latestReleaseForApplication(applicationID string, items []domainrelease.Record) *domainrelease.Record {
	for _, item := range items {
		if item.ApplicationID == applicationID {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func latestBundleForApplication(applicationID string, items []domaindelivery.ReleaseBundle) *domaindelivery.ReleaseBundle {
	for _, item := range items {
		if item.ApplicationID == applicationID {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func latestExecutionTaskForApplication(applicationID string, items []domaindelivery.ExecutionTask) *domaindelivery.ExecutionTask {
	for _, item := range items {
		if item.ApplicationID == applicationID {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func latestBuildForBinding(binding domaincatalog.ApplicationEnvironment, items []domainbuild.Record) *domainbuild.Record {
	for _, item := range items {
		if item.ApplicationID != binding.ApplicationID {
			continue
		}
		if bindingID := strings.TrimSpace(fmt.Sprint(item.Metadata["applicationEnvironmentId"])); bindingID != "" && bindingID == binding.ID {
			copyItem := item
			return &copyItem
		}
	}
	return latestBuildForApplication(binding.ApplicationID, items)
}

func latestWorkflowForBinding(binding domaincatalog.ApplicationEnvironment, items []domainworkflow.Run) *domainworkflow.Run {
	for _, item := range items {
		if item.ApplicationID != binding.ApplicationID {
			continue
		}
		if matchesBindingTarget(binding, item.ClusterID, item.Namespace, item.DeploymentName) {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func latestReleaseForBinding(binding domaincatalog.ApplicationEnvironment, items []domainrelease.Record) *domainrelease.Record {
	for _, item := range items {
		if item.ApplicationID != binding.ApplicationID {
			continue
		}
		if matchesBindingTarget(binding, item.ClusterID, item.Namespace, item.DeploymentName) {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func latestBundleForBinding(binding domaincatalog.ApplicationEnvironment, items []domaindelivery.ReleaseBundle) *domaindelivery.ReleaseBundle {
	for _, item := range items {
		if item.ApplicationID != binding.ApplicationID {
			continue
		}
		if strings.TrimSpace(item.ApplicationEnvironmentID) == binding.ID {
			copyItem := item
			return &copyItem
		}
	}
	return latestBundleForApplication(binding.ApplicationID, items)
}

func latestExecutionTaskForBinding(binding domaincatalog.ApplicationEnvironment, items []domaindelivery.ExecutionTask) *domaindelivery.ExecutionTask {
	for _, item := range items {
		if item.ApplicationID != binding.ApplicationID {
			continue
		}
		if strings.TrimSpace(item.ApplicationEnvironmentID) == binding.ID {
			copyItem := item
			return &copyItem
		}
	}
	return latestExecutionTaskForApplication(binding.ApplicationID, items)
}

func selectorMatchesLabels(selector map[string]string, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if labels[key] != value {
			return false
		}
	}
	return true
}

func filterPodsBySelector(items []domainresource.PodView, selector map[string]string) []domainresource.PodView {
	if len(selector) == 0 {
		return nil
	}
	filtered := make([]domainresource.PodView, 0)
	for _, item := range items {
		if selectorMatchesLabels(selector, item.Labels) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterServicesBySelector(items []domainresource.ServiceView, selector map[string]string) []domainresource.ServiceView {
	if len(selector) == 0 {
		return nil
	}
	filtered := make([]domainresource.ServiceView, 0)
	for _, item := range items {
		if selectorMatchesLabels(item.Selector, selector) || selectorMatchesLabels(selector, item.Selector) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterIngressesByServices(items []domainresource.IngressView, services []domainresource.ServiceView) []domainresource.IngressView {
	if len(services) == 0 {
		return nil
	}
	serviceNames := make(map[string]struct{}, len(services))
	for _, item := range services {
		serviceNames[item.Name] = struct{}{}
	}
	filtered := make([]domainresource.IngressView, 0)
	for _, item := range items {
		for _, backend := range item.BackendServices {
			if _, ok := serviceNames[backend]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

func (s *Service) listRuntimeWorkloadsForBinding(
	ctx context.Context,
	principal domainidentity.Principal,
	app domainapp.App,
	binding domaincatalog.ApplicationEnvironment,
	bundles []domaindelivery.ReleaseBundle,
	tasks []domaindelivery.ExecutionTask,
	builds []domainbuild.Record,
	workflows []domainworkflow.Run,
	releases []domainrelease.Record,
) ([]domaindelivery.ApplicationRuntimeWorkload, error) {
	workloads := make([]domaindelivery.ApplicationRuntimeWorkload, 0)
	seen := make(map[string]struct{})
	for _, target := range binding.Targets {
		clusterID := strings.TrimSpace(target.ClusterID)
		namespace := strings.TrimSpace(target.Namespace)
		if clusterID == "" || namespace == "" {
			continue
		}
		items, err := s.targets.ListDeployments(ctx, principal, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if len(binding.ResourceSelector.MatchLabels) > 0 {
				if !selectorMatchesLabels(binding.ResourceSelector.MatchLabels, item.Labels) {
					continue
				}
			} else if strings.TrimSpace(item.Name) != strings.TrimSpace(target.WorkloadName) {
				continue
			}
			key := fmt.Sprintf("%s/%s/%s", clusterID, namespace, item.Name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			workloads = append(workloads, domaindelivery.ApplicationRuntimeWorkload{
				ApplicationEnvironmentID: binding.ID,
				ClusterID:                clusterID,
				Namespace:                namespace,
				WorkloadKind:             "Deployment",
				WorkloadName:             item.Name,
				Labels:                   item.Labels,
				DesiredReplicas:          item.DesiredReplicas,
				ReadyReplicas:            item.ReadyReplicas,
				UpdatedReplicas:          item.UpdatedReplicas,
				AvailableReplicas:        item.Available,
				BuildSource:              resolveBuildSource(app, binding.BuildPolicy.SourceID),
				LatestBundle:             latestBundleForBinding(binding, bundles),
				LatestExecutionTask:      latestExecutionTaskForBinding(binding, tasks),
				LatestBuild:              latestBuildForBinding(binding, builds),
				LatestWorkflow:           latestWorkflowForBinding(binding, workflows),
				LatestRelease:            latestReleaseForBinding(binding, releases),
			})
		}
	}
	return workloads, nil
}

func matchesBindingTarget(binding domaincatalog.ApplicationEnvironment, clusterID, namespace, workloadName string) bool {
	for _, item := range binding.Targets {
		if !item.Enabled {
			continue
		}
		if item.ClusterID == clusterID && item.Namespace == namespace && item.WorkloadName == workloadName && strings.EqualFold(item.WorkloadKind, "deployment") {
			return true
		}
	}
	return false
}

func matchesSearch(item domainresource.DeploymentView, search string) bool {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return true
	}
	if strings.Contains(strings.ToLower(item.Name), search) {
		return true
	}
	for key, value := range item.Labels {
		if strings.Contains(strings.ToLower(key), search) || strings.Contains(strings.ToLower(value), search) {
			return true
		}
	}
	return false
}

func derefEnvironment(item *domaincatalog.Environment) domaincatalog.Environment {
	if item == nil {
		return domaincatalog.Environment{}
	}
	return *item
}

func renderedSpecFromBlueprint(blueprint domaindelivery.DeliveryBlueprint) domaindelivery.RenderedDeliverySpec {
	return domaindelivery.RenderedDeliverySpec{
		ApplicationDraft:    blueprint.ApplicationDraft,
		BuildSources:        append([]domainapp.BuildSourceInput(nil), blueprint.BuildSources...),
		EnvironmentBindings: append([]domaindelivery.BlueprintEnvironmentBindingTemplate(nil), blueprint.EnvironmentBindings...),
		Files:               append([]domaindelivery.BlueprintFileTemplate(nil), blueprint.Files...),
		ExecutionHints:      ensureMap(blueprint.ExecutionHints),
		PostCreateActions:   append([]string(nil), blueprint.PostCreateActions...),
	}
}

func renderedSpecFromDraft(draft domaindelivery.DeliveryDraft) domaindelivery.RenderedDeliverySpec {
	return domaindelivery.RenderedDeliverySpec{
		ApplicationDraft:    draft.ApplicationDraft,
		Services:            append([]domaindelivery.DeliveryDraftService(nil), draft.Services...),
		BuildSources:        append([]domainapp.BuildSourceInput(nil), draft.BuildSources...),
		EnvironmentBindings: append([]domaindelivery.BlueprintEnvironmentBindingTemplate(nil), draft.EnvironmentBindings...),
		Files:               append([]domaindelivery.BlueprintFileTemplate(nil), draft.Files...),
		ExecutionHints:      ensureMap(draft.ExecutionHints),
		PostCreateActions:   append([]string(nil), draft.PostCreateActions...),
	}
}

func applicationInputFromDraft(draft domaindelivery.BlueprintApplicationDraft, buildSources []domainapp.BuildSourceInput) domainapp.UpsertInput {
	return domainapp.UpsertInput{
		ID:                  strings.TrimSpace(draft.ID),
		Name:                strings.TrimSpace(draft.Name),
		Key:                 strings.TrimSpace(draft.Key),
		Group:               strings.TrimSpace(draft.Group),
		BusinessLineID:      strings.TrimSpace(draft.BusinessLineID),
		Language:            strings.TrimSpace(draft.Language),
		Description:         strings.TrimSpace(draft.Description),
		OwnerTeam:           strings.TrimSpace(draft.OwnerTeam),
		RepositoryProvider:  strings.TrimSpace(draft.RepositoryProvider),
		RepositoryProjectID: strings.TrimSpace(draft.RepositoryProjectID),
		RepositoryPath:      strings.TrimSpace(draft.RepositoryPath),
		DefaultBranch:       strings.TrimSpace(draft.DefaultBranch),
		DefaultTag:          strings.TrimSpace(draft.DefaultTag),
		BuildImage:          strings.TrimSpace(draft.BuildImage),
		BuildContextDir:     strings.TrimSpace(draft.BuildContextDir),
		DockerfilePath:      strings.TrimSpace(draft.DockerfilePath),
		Enabled:             draft.Enabled,
		Metadata:            ensureMap(draft.Metadata),
		BuildSources:        buildSources,
	}
}

func (s *Service) upsertApplication(ctx context.Context, principal domainidentity.Principal, input domainapp.UpsertInput) (domainapp.App, error) {
	if strings.TrimSpace(input.ID) != "" {
		return s.applications.Update(ctx, principal, input.ID, input)
	}
	items, err := s.applications.List(ctx, principal, domainapp.Filter{Limit: 200})
	if err != nil {
		return domainapp.App{}, err
	}
	for _, item := range items {
		if strings.TrimSpace(item.Key) == strings.TrimSpace(input.Key) {
			return s.applications.Update(ctx, principal, item.ID, input)
		}
	}
	return s.applications.Create(ctx, principal, input)
}

func (s *Service) applyRenderedDeliverySpec(ctx context.Context, principal domainidentity.Principal, spec domaindelivery.RenderedDeliverySpec) (domainapp.App, []domainapp.Service, []domaincatalog.ApplicationEnvironment, error) {
	appInput := applicationInputFromDraft(spec.ApplicationDraft, spec.BuildSources)
	app, err := s.upsertApplication(ctx, principal, appInput)
	if err != nil {
		return domainapp.App{}, nil, nil, err
	}
	services, err := s.upsertApplicationServices(ctx, principal, app.ID, spec.Services)
	if err != nil {
		return domainapp.App{}, nil, nil, err
	}
	bindings, err := s.upsertEnvironmentBindings(ctx, principal, app, spec)
	if err != nil {
		return domainapp.App{}, nil, nil, err
	}
	return app, services, bindings, nil
}

func (s *Service) upsertApplicationServices(ctx context.Context, principal domainidentity.Principal, applicationID string, services []domaindelivery.DeliveryDraftService) ([]domainapp.Service, error) {
	if len(services) == 0 {
		return nil, nil
	}
	existing, err := s.applications.ListServices(ctx, principal, applicationID)
	if err != nil {
		return nil, err
	}
	existingByKey := make(map[string]domainapp.Service, len(existing))
	for _, item := range existing {
		existingByKey[strings.TrimSpace(item.Key)] = item
	}
	result := make([]domainapp.Service, 0, len(services))
	for _, service := range services {
		input := serviceInputFromDraft(service)
		existingID := strings.TrimSpace(input.ID)
		if existingID == "" {
			if item, ok := existingByKey[strings.TrimSpace(input.Key)]; ok {
				existingID = item.ID
			}
		}
		var saved domainapp.Service
		if existingID != "" {
			saved, err = s.applications.UpdateService(ctx, principal, applicationID, existingID, input)
		} else {
			saved, err = s.applications.CreateService(ctx, principal, applicationID, input)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, saved)
	}
	return result, nil
}

func serviceInputFromDraft(draft domaindelivery.DeliveryDraftService) domainapp.ServiceInput {
	serviceKind := draft.ServiceKind
	if serviceKind == "" {
		serviceKind = domainapp.ServiceKindKubernetesWorkload
	}
	return domainapp.ServiceInput{
		ID:                  strings.TrimSpace(draft.ID),
		Key:                 strings.TrimSpace(draft.Key),
		Name:                strings.TrimSpace(draft.Name),
		Description:         strings.TrimSpace(draft.Description),
		ServiceKind:         serviceKind,
		OwnerTeam:           strings.TrimSpace(draft.OwnerTeam),
		RepositoryProvider:  strings.TrimSpace(draft.RepositoryProvider),
		RepositoryProjectID: strings.TrimSpace(draft.RepositoryProjectID),
		RepositoryPath:      strings.TrimSpace(draft.RepositoryPath),
		DefaultBranch:       strings.TrimSpace(draft.DefaultBranch),
		BuildSourceID:       strings.TrimSpace(draft.BuildSourceID),
		Enabled:             draft.Enabled,
		Metadata:            ensureMap(draft.Metadata),
		Containers:          draft.Containers,
	}
}

func (s *Service) upsertEnvironmentBindings(ctx context.Context, principal domainidentity.Principal, app domainapp.App, spec domaindelivery.RenderedDeliverySpec) ([]domaincatalog.ApplicationEnvironment, error) {
	environments, err := s.catalog.ListEnvironments(ctx, principal)
	if err != nil {
		return nil, err
	}
	envByKey := make(map[string]domaincatalog.Environment, len(environments))
	for _, item := range environments {
		envByKey[item.Key] = item
	}
	existingBindings, err := s.catalog.ListApplicationEnvironments(ctx, principal)
	if err != nil {
		return nil, err
	}
	resultBindings := make([]domaincatalog.ApplicationEnvironment, 0, len(spec.EnvironmentBindings))
	for _, binding := range spec.EnvironmentBindings {
		environmentID := strings.TrimSpace(binding.EnvironmentID)
		if environmentID == "" {
			if item, ok := envByKey[strings.TrimSpace(binding.EnvironmentKey)]; ok {
				environmentID = item.ID
			}
		}
		if environmentID == "" {
			return nil, fmt.Errorf("%w: delivery draft binding is missing environment mapping", apperrors.ErrInvalidArgument)
		}
		if binding.BuildPolicy.SourceID == "" && len(spec.BuildSources) > 0 {
			binding.BuildPolicy.SourceID = spec.BuildSources[0].ID
		}
		input := domaincatalog.ApplicationEnvironmentInput{
			ApplicationID:      app.ID,
			EnvironmentID:      environmentID,
			StrategyProfileID:  binding.StrategyProfileID,
			PromotionPolicyID:  binding.PromotionPolicyID,
			ArtifactPolicyID:   binding.ArtifactPolicyID,
			WorkflowTemplateID: binding.WorkflowTemplateID,
			BuildPolicy:        binding.BuildPolicy,
			ReleasePolicy:      binding.ReleasePolicy,
			ResourceSelector:   binding.ResourceSelector,
			Targets:            binding.Targets,
		}
		existingID := ""
		for _, item := range existingBindings {
			if item.ApplicationID == app.ID && item.EnvironmentID == environmentID {
				existingID = item.ID
				break
			}
		}
		var saved domaincatalog.ApplicationEnvironment
		if existingID != "" {
			saved, err = s.catalog.UpdateApplicationEnvironment(ctx, principal, existingID, input)
		} else {
			saved, err = s.catalog.CreateApplicationEnvironment(ctx, principal, input)
		}
		if err != nil {
			return nil, err
		}
		resultBindings = append(resultBindings, saved)
	}
	return resultBindings, nil
}
