package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainapp "github.com/soha/soha/internal/domain/application"
	domainbuild "github.com/soha/soha/internal/domain/build"
	domaincatalog "github.com/soha/soha/internal/domain/catalog"
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainrelease "github.com/soha/soha/internal/domain/release"
	domainresource "github.com/soha/soha/internal/domain/resource"
	domainworkflow "github.com/soha/soha/internal/domain/workflow"
	"github.com/soha/soha/internal/platform/apperrors"
)

type ApplicationReader interface {
	List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error)
	Get(context.Context, domainidentity.Principal, string) (domainapp.App, error)
	Create(context.Context, domainidentity.Principal, domainapp.UpsertInput) (domainapp.App, error)
	Update(context.Context, domainidentity.Principal, string, domainapp.UpsertInput) (domainapp.App, error)
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
	Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error)
}

type WorkflowReader interface {
	List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error)
	Trigger(context.Context, domainidentity.Principal, domainworkflow.Input) (domainworkflow.Run, error)
	TriggerValidation(context.Context, domainidentity.Principal, domainworkflow.Input) (domainworkflow.Run, error)
	TriggerRollback(context.Context, domainidentity.Principal, domainworkflow.Input) (domainworkflow.Run, error)
}

type ReleaseReader interface {
	List(context.Context, domainidentity.Principal, domainrelease.Filter) ([]domainrelease.Record, error)
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
		return domaindelivery.ApplicationWorkloadRuntimeDetail{}, fmt.Errorf("workload not found")
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
	return s.repository.GetReleaseBundle(ctx, strings.TrimSpace(bundleID))
}

func (s *Service) ListExecutionTasks(ctx context.Context, principal domainidentity.Principal, filter domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksView); err != nil {
		return nil, err
	}
	return s.repository.ListExecutionTasks(ctx, filter)
}

func (s *Service) GetExecutionTask(ctx context.Context, principal domainidentity.Principal, taskID string) (domaindelivery.ExecutionTask, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksView); err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	return s.repository.GetExecutionTask(ctx, strings.TrimSpace(taskID))
}

func (s *Service) ListExecutionLogs(ctx context.Context, principal domainidentity.Principal, taskID string, limit int) ([]domaindelivery.ExecutionLog, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryExecutionTasksView); err != nil {
		return nil, err
	}
	return s.repository.ListExecutionLogs(ctx, strings.TrimSpace(taskID), limit)
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

func (s *Service) ListApprovalPolicies(ctx context.Context, principal domainidentity.Principal) ([]domaindelivery.ApprovalPolicy, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApprovalPoliciesView); err != nil {
		return nil, err
	}
	return s.repository.ListApprovalPolicies(ctx)
}

func (s *Service) GetApprovalPolicy(ctx context.Context, policyID string) (domaindelivery.ApprovalPolicy, error) {
	return s.repository.GetApprovalPolicy(ctx, strings.TrimSpace(policyID))
}

func (s *Service) CreateApprovalPolicy(ctx context.Context, principal domainidentity.Principal, input domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApprovalPoliciesManage); err != nil {
		return domaindelivery.ApprovalPolicy{}, err
	}
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("approval policy key and name are required")
	}
	return s.repository.CreateApprovalPolicy(ctx, input)
}

func (s *Service) UpdateApprovalPolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApprovalPoliciesManage); err != nil {
		return domaindelivery.ApprovalPolicy{}, err
	}
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("approval policy key and name are required")
	}
	return s.repository.UpdateApprovalPolicy(ctx, strings.TrimSpace(policyID), input)
}

func (s *Service) DeleteApprovalPolicy(ctx context.Context, principal domainidentity.Principal, policyID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApprovalPoliciesManage); err != nil {
		return err
	}
	return s.repository.DeleteApprovalPolicy(ctx, strings.TrimSpace(policyID))
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
	return s.repository.CreateDeliveryBlueprint(ctx, input)
}

func (s *Service) UpdateDeliveryBlueprint(ctx context.Context, principal domainidentity.Principal, blueprintID string, input domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domaindelivery.DeliveryBlueprint{}, err
	}
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaindelivery.DeliveryBlueprint{}, fmt.Errorf("delivery blueprint key and name are required")
	}
	return s.repository.UpdateDeliveryBlueprint(ctx, strings.TrimSpace(blueprintID), input)
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
	appInput := applicationInputFromDraft(spec.ApplicationDraft, spec.BuildSources)
	app, err := s.upsertApplication(ctx, principal, appInput)
	if err != nil {
		return domaindelivery.BlueprintBootstrapResult{}, err
	}
	environments, err := s.catalog.ListEnvironments(ctx, principal)
	if err != nil {
		return domaindelivery.BlueprintBootstrapResult{}, err
	}
	envByKey := make(map[string]domaincatalog.Environment, len(environments))
	for _, item := range environments {
		envByKey[item.Key] = item
	}
	existingBindings, err := s.catalog.ListApplicationEnvironments(ctx, principal)
	if err != nil {
		return domaindelivery.BlueprintBootstrapResult{}, err
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
			return domaindelivery.BlueprintBootstrapResult{}, fmt.Errorf("delivery blueprint binding is missing environment mapping")
		}
		if binding.BuildPolicy.SourceID == "" && len(spec.BuildSources) > 0 {
			binding.BuildPolicy.SourceID = spec.BuildSources[0].ID
		}
		input := domaincatalog.ApplicationEnvironmentInput{
			ApplicationID:      app.ID,
			EnvironmentID:      environmentID,
			StrategyProfileID:  binding.StrategyProfileID,
			PromotionPolicyID:  binding.PromotionPolicyID,
			ApprovalPolicyID:   binding.ApprovalPolicyID,
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
			return domaindelivery.BlueprintBootstrapResult{}, err
		}
		resultBindings = append(resultBindings, saved)
	}
	return domaindelivery.BlueprintBootstrapResult{
		Application:         app,
		EnvironmentBindings: resultBindings,
		Spec:                spec,
	}, nil
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

func (s *Service) ClaimExecutionTask(ctx context.Context, providerKinds []string, agentID, runtimeEndpoint string) (domaindelivery.ExecutionTask, error) {
	if s.execution != nil {
		return s.execution.ClaimExecutionTask(ctx, providerKinds, strings.TrimSpace(agentID), strings.TrimSpace(runtimeEndpoint))
	}
	return s.repository.ClaimExecutionTask(ctx, providerKinds, strings.TrimSpace(agentID), strings.TrimSpace(runtimeEndpoint))
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
	return updated, nil
}

func (s *Service) GetExecutionTaskForRunner(ctx context.Context, taskID string) (domaindelivery.ExecutionTask, error) {
	if s.execution != nil {
		return s.repository.GetExecutionTask(ctx, strings.TrimSpace(taskID))
	}
	return s.repository.GetExecutionTask(ctx, strings.TrimSpace(taskID))
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
