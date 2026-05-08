package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainbuild "github.com/kubecrux/kubecrux/internal/domain/build"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	domaindelivery "github.com/kubecrux/kubecrux/internal/domain/delivery"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainrelease "github.com/kubecrux/kubecrux/internal/domain/release"
	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
	domainworkflow "github.com/kubecrux/kubecrux/internal/domain/workflow"
)

type ApplicationReader interface {
	List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error)
	Get(context.Context, domainidentity.Principal, string) (domainapp.App, error)
}

type CatalogReader interface {
	ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error)
	ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error)
	GetApplicationEnvironment(context.Context, domainidentity.Principal, string) (domaincatalog.ApplicationEnvironment, error)
}

type BuildReader interface {
	List(context.Context, domainidentity.Principal, domainbuild.Filter) ([]domainbuild.Record, error)
}

type WorkflowReader interface {
	List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error)
}

type ReleaseReader interface {
	List(context.Context, domainidentity.Principal, domainrelease.Filter) ([]domainrelease.Record, error)
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
	ListDeployments(context.Context, domainidentity.Principal, string, string) ([]domainresource.DeploymentView, error)
	GetDeploymentDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentDetailView, error)
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
			TargetCount:              len(binding.Targets),
			BuildSourceID:            binding.BuildPolicy.SourceID,
			BuildSource:              resolveBuildSource(app, binding.BuildPolicy.SourceID),
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
