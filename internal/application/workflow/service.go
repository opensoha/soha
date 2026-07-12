package workflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
	"go.uber.org/zap"
)

const (
	defaultAsyncWorkflowWorkers    = 4
	defaultAsyncWorkflowQueueSize  = 64
	defaultDAGNodeConcurrency      = 4
	workflowStatusWaitingApproval  = "waiting_approval"
	workflowStatusWaitingExecution = "waiting_execution"
)

type Repository interface {
	List(context.Context, string, int) ([]domainworkflow.Run, error)
	Get(context.Context, string) (domainworkflow.Run, error)
	Create(context.Context, domainworkflow.Run) (domainworkflow.Run, error)
	Update(context.Context, domainworkflow.Run) (domainworkflow.Run, error)
	CreateApproval(context.Context, domainworkflow.Approval) error
}

type ApplicationReader interface {
	Get(context.Context, string) (domainapp.App, error)
	ListServices(context.Context, string) ([]domainapp.Service, error)
}

type CatalogReader interface {
	ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error)
}

type BuildExecutor interface {
	Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error)
	Execute(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error)
}

type ReleaseExecutor interface {
	Trigger(context.Context, domainidentity.Principal, domainrelease.TriggerInput) (domainrelease.Record, error)
}

type DeploymentRolloutReader interface {
	GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error)
	ListDeploymentRolloutHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.RolloutHistoryView, error)
}

type DeploymentActionExecutor interface {
	RollbackDeployment(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.DeploymentRollbackView, error)
	RestartDeployment(context.Context, domainidentity.Principal, string, string, string) error
	ScaleDeployment(context.Context, domainidentity.Principal, string, string, string, int32) error
}

type PodActionExecutor interface {
	DeletePod(context.Context, domainidentity.Principal, string, string, string) error
}

type ClusterEventReader interface {
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
}

type ResourceExecutor interface {
	DeploymentRolloutReader
	DeploymentActionExecutor
	PodActionExecutor
	ClusterEventReader
}

type AlertMutator interface {
	CreateWorkflowSilence(context.Context, domainidentity.Principal, domainalert.SilenceInput) (domainalert.AlertSilence, error)
}

type ArtifactStore interface {
	UpsertExecutionArtifact(context.Context, domaindelivery.ExecutionArtifact) (domaindelivery.ExecutionArtifact, error)
}

type ExecutionTaskStore interface {
	CreateExecutionTask(context.Context, domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error)
	CreateExecutionLog(context.Context, domaindelivery.ExecutionLog) error
}

type Service struct {
	repo        Repository
	apps        ApplicationReader
	authorizer  domainaccess.Authorizer
	permissions *appaccess.PermissionResolver
	catalog     CatalogReader
	builds      BuildExecutor
	releases    ReleaseExecutor
	resources   ResourceExecutor
	alerts      AlertMutator
	artifacts   ArtifactStore
	taskStore   ExecutionTaskStore
	httpClient  *http.Client
	logger      *zap.Logger
	metrics     *runtimeobs.Registry

	scheduler        dagScheduler
	executorSettings dagExecutorSettings
	runState         runStateStore
}

type workflowPruner interface {
	DeleteByIDs(context.Context, []string) error
}

func New(repo Repository, apps ApplicationReader, authorizer domainaccess.Authorizer, permissions *appaccess.PermissionResolver, catalog CatalogReader, builds BuildExecutor, releases ReleaseExecutor, resources ResourceExecutor) *Service {
	service := &Service{
		repo:        repo,
		apps:        apps,
		authorizer:  authorizer,
		permissions: permissions,
		catalog:     catalog,
		builds:      builds,
		releases:    releases,
		resources:   resources,
		httpClient:  &http.Client{Timeout: defaultDAGHTTPTimeout},
	}
	service.scheduler.configure(defaultAsyncWorkflowWorkers, defaultAsyncWorkflowQueueSize)
	service.executorSettings.configure(defaultDAGNodeConcurrency)
	return service
}

func (s *Service) SetInstrumentation(logger *zap.Logger, metrics *runtimeobs.Registry) {
	s.logger = logger
	s.metrics = metrics
}

func (s *Service) SetAlertMutator(alerts AlertMutator) {
	s.alerts = alerts
}

func (s *Service) SetArtifactStore(artifacts ArtifactStore) {
	s.artifacts = artifacts
	if store, ok := artifacts.(ExecutionTaskStore); ok {
		s.taskStore = store
	}
}

func (s *Service) SetExecutionTaskStore(store ExecutionTaskStore) {
	s.taskStore = store
}

func (s *Service) SetRuntimeOptions(workerCount, queueSize, nodeParallelism int) {
	s.scheduler.configure(workerCount, queueSize)
	s.executorSettings.configure(nodeParallelism)
}

func (s *Service) Start(ctx context.Context) {
	s.ensureRunner(ctx)
}

func (s *Service) Shutdown(ctx context.Context) error {
	pending, err := s.scheduler.shutdown(ctx)
	if err != nil {
		return err
	}
	for _, task := range pending {
		s.failRun(ctx, task.run, task.definition, "workflow execution canceled before start")
	}
	return nil
}

func (s *Service) ensureRunner(ctx context.Context) {
	s.scheduler.start(ctx, func(runnerCtx context.Context, task dagRunTask) {
		taskCtx := requestctx.WithMetadata(runnerCtx, task.requestMeta)
		s.runDAGAsync(taskCtx, task.principal, task.app, task.input, task.binding, task.definition, task.run)
	})
}

func (s *Service) enqueueDAGRun(ctx context.Context, task dagRunTask) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.ensureRunner(context.WithoutCancel(ctx))
	depth, err := s.scheduler.enqueue(ctx, task)
	if err == nil {
		if s.metrics != nil {
			s.metrics.SetQueueDepth(runtimeobs.ComponentWorkflowRunner, depth)
		}
		s.logDebugCtx(ctx, "workflow queued", zap.String("runID", task.run.ID), zap.String("applicationID", task.run.ApplicationID), zap.Int("queueDepth", depth))
		return nil
	}
	if s.metrics != nil {
		outcome := runtimeobs.OutcomeFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			outcome = runtimeobs.OutcomeCanceled
		}
		s.metrics.RecordFinish(runtimeobs.ComponentWorkflowRunner, task.run.ID, 0, depth, 1, outcome, err)
	}
	return err
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, applicationID string, limit int) ([]domainworkflow.Run, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx, strings.TrimSpace(applicationID), limit)
	if err != nil {
		return nil, err
	}

	allowed := make([]domainworkflow.Run, 0, len(items))
	staleIDs := make([]string, 0)
	for _, item := range items {
		if strings.TrimSpace(item.ApplicationID) == "" {
			staleIDs = append(staleIDs, item.ID)
			continue
		}
		app, err := s.apps.Get(ctx, item.ApplicationID)
		if err != nil {
			if isApplicationMissing(err) {
				staleIDs = append(staleIDs, item.ID)
			}
			continue
		}
		if err := s.authorize(ctx, principal, domainaccess.ActionList, app, item.ApplicationID); err != nil {
			continue
		}
		allowed = append(allowed, item)
	}

	if len(staleIDs) > 0 {
		if pruner, ok := s.repo.(workflowPruner); ok {
			_ = pruner.DeleteByIDs(ctx, uniqueIDs(staleIDs))
		}
	}
	return allowed, nil
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, workflowRunID string) (domainworkflow.Run, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return domainworkflow.Run{}, err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(workflowRunID))
	if err != nil {
		return domainworkflow.Run{}, err
	}
	app, err := s.apps.Get(ctx, item.ApplicationID)
	if err != nil {
		if isApplicationMissing(err) {
			return domainworkflow.Run{}, fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
		}
		return domainworkflow.Run{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionView, app, item.ApplicationID); err != nil {
		return domainworkflow.Run{}, err
	}
	return item, nil
}

func (s *Service) Trigger(ctx context.Context, principal domainidentity.Principal, input domainworkflow.Input) (domainworkflow.Run, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return domainworkflow.Run{}, err
	}
	if strings.TrimSpace(input.ApplicationID) == "" {
		return domainworkflow.Run{}, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	app, err := s.apps.Get(ctx, input.ApplicationID)
	if err != nil {
		if isApplicationMissing(err) {
			return domainworkflow.Run{}, fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
		}
		return domainworkflow.Run{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionTrigger, app, input.ApplicationID); err != nil {
		return domainworkflow.Run{}, err
	}
	if run, binding, definition, ok, err := s.prepareBoundDAGRun(ctx, app, input); err != nil {
		return domainworkflow.Run{}, err
	} else if ok {
		created, err := s.repo.Create(ctx, run)
		if err != nil {
			return domainworkflow.Run{}, err
		}
		if err := s.enqueueDAGRun(ctx, dagRunTask{
			principal:   principal,
			app:         app,
			input:       input,
			binding:     *binding,
			definition:  definition,
			run:         created,
			requestMeta: requestctx.FromContext(ctx),
		}); err != nil {
			return s.failRun(context.Background(), created, definition, fmt.Sprintf("workflow runner enqueue failed: %v", err)), nil
		}
		return created, nil
	}
	steps := []domainworkflow.Step{{Name: "preflight", Status: "completed", Summary: "scope and inputs validated"}}
	if input.TriggerBuild {
		steps = append(steps, domainworkflow.Step{Name: "build", Status: "queued", Summary: "manual build handoff prepared"})
	}
	if input.TriggerRelease {
		steps = append(steps, domainworkflow.Step{Name: "release", Status: "queued", Summary: "release handoff prepared"})
	}
	steps = append(steps, domainworkflow.Step{Name: "verify", Status: "pending", Summary: "post-release verification is waiting"})
	now := time.Now().UTC().Format(time.RFC3339)
	metadata := withGatewayApprovalWorkflowMetadata(map[string]any{
		"applicationName": app.Name,
		"triggerBuild":    input.TriggerBuild,
		"triggerRelease":  input.TriggerRelease,
	}, input)
	item := domainworkflow.Run{
		ID:             "workflow:" + uuid.NewString(),
		ApplicationID:  input.ApplicationID,
		WorkflowName:   strings.TrimSpace(input.WorkflowName),
		ClusterID:      strings.TrimSpace(input.ClusterID),
		Namespace:      strings.TrimSpace(input.Namespace),
		DeploymentName: strings.TrimSpace(input.DeploymentName),
		Status:         "running",
		Steps:          steps,
		Metadata:       metadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if item.WorkflowName == "" {
		item.WorkflowName = "build-release-verify"
	}
	return s.repo.Create(ctx, item)
}

func (s *Service) TriggerValidation(ctx context.Context, principal domainidentity.Principal, input domainworkflow.Input) (domainworkflow.Run, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return domainworkflow.Run{}, err
	}
	if strings.TrimSpace(input.ApplicationID) == "" {
		return domainworkflow.Run{}, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	input.ValidationOnly = true
	input.TriggerBuild = false
	input.TriggerRelease = false
	app, err := s.apps.Get(ctx, input.ApplicationID)
	if err != nil {
		if isApplicationMissing(err) {
			return domainworkflow.Run{}, fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
		}
		return domainworkflow.Run{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionTrigger, app, input.ApplicationID); err != nil {
		return domainworkflow.Run{}, err
	}
	run, binding, definition, err := s.prepareValidationDAGRun(ctx, app, input)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	created, err := s.repo.Create(ctx, run)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	if err := s.enqueueDAGRun(ctx, dagRunTask{
		principal:   principal,
		app:         app,
		input:       input,
		binding:     *binding,
		definition:  definition,
		run:         created,
		requestMeta: requestctx.FromContext(ctx),
	}); err != nil {
		return s.failRun(context.Background(), created, definition, fmt.Sprintf("workflow runner enqueue failed: %v", err)), nil
	}
	return created, nil
}

func (s *Service) TriggerRollback(ctx context.Context, principal domainidentity.Principal, input domainworkflow.Input) (domainworkflow.Run, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return domainworkflow.Run{}, err
	}
	if strings.TrimSpace(input.ApplicationID) == "" {
		return domainworkflow.Run{}, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	input.RollbackOnly = true
	input.TriggerBuild = false
	input.TriggerRelease = false
	input.ValidationOnly = false
	app, err := s.apps.Get(ctx, input.ApplicationID)
	if err != nil {
		if isApplicationMissing(err) {
			return domainworkflow.Run{}, fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
		}
		return domainworkflow.Run{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionTrigger, app, input.ApplicationID); err != nil {
		return domainworkflow.Run{}, err
	}
	run, binding, definition, err := s.prepareRollbackDAGRun(ctx, app, input)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	created, err := s.repo.Create(ctx, run)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	if err := s.enqueueDAGRun(ctx, dagRunTask{
		principal:   principal,
		app:         app,
		input:       input,
		binding:     *binding,
		definition:  definition,
		run:         created,
		requestMeta: requestctx.FromContext(ctx),
	}); err != nil {
		return s.failRun(context.Background(), created, definition, fmt.Sprintf("workflow runner enqueue failed: %v", err)), nil
	}
	return created, nil
}

func (s *Service) ExecuteSystemDAG(ctx context.Context, principal domainidentity.Principal, applicationID, workflowName, workflowTemplateID string, definition map[string]any, input domainworkflow.Input, extraMetadata map[string]any) (domainworkflow.Run, error) {
	parsed, ok := s.dagPlanner().parse(definition)
	if !ok {
		return domainworkflow.Run{}, fmt.Errorf("%w: workflow definition is not a supported DAG", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(applicationID) == "" {
		applicationID = "healing:" + firstNonEmpty(strings.TrimSpace(workflowTemplateID), "adhoc")
	}
	if strings.TrimSpace(workflowName) == "" {
		workflowName = firstNonEmpty(strings.TrimSpace(workflowTemplateID), "healing-dag")
	}
	syntheticBinding := domaincatalog.ApplicationEnvironment{
		ID:                 firstNonEmpty(strings.TrimSpace(workflowTemplateID), "healing-binding"),
		ApplicationID:      applicationID,
		WorkflowTemplateID: strings.TrimSpace(workflowTemplateID),
		WorkflowTemplate: &domaincatalog.WorkflowTemplate{
			ID:         strings.TrimSpace(workflowTemplateID),
			Key:        strings.TrimSpace(workflowTemplateID),
			Name:       workflowName,
			Definition: definition,
		},
	}
	if err := s.dagPlanner().validate(parsed, syntheticBinding, domainapp.App{ID: applicationID, Name: workflowName}, input); err != nil {
		return domainworkflow.Run{}, err
	}
	nodeRuns := initializeNodeRuns(parsed)
	runMetadata := map[string]any{
		"applicationName":      workflowName,
		"mode":                 parsed.Mode,
		"executionMode":        "release_dag",
		"schemaVersion":        parsed.SchemaVersion,
		"workflowTemplateId":   syntheticBinding.WorkflowTemplateID,
		"workflowTemplateKey":  syntheticBinding.WorkflowTemplate.Key,
		"workflowTemplateName": syntheticBinding.WorkflowTemplate.Name,
		"bindingId":            syntheticBinding.ID,
		"nodes":                parsed.Nodes,
		"edges":                parsed.Edges,
	}
	for key, value := range extraMetadata {
		runMetadata[key] = value
	}
	runMetadata = withGatewayApprovalWorkflowMetadata(runMetadata, input)
	run := domainworkflow.Run{
		ID:             "workflow:" + uuid.NewString(),
		ApplicationID:  applicationID,
		WorkflowName:   workflowName,
		ClusterID:      strings.TrimSpace(input.ClusterID),
		Namespace:      strings.TrimSpace(input.Namespace),
		DeploymentName: strings.TrimSpace(input.DeploymentName),
		Status:         "queued",
		Steps:          buildStepsFromNodeRuns(parsed, nodeRuns),
		NodeRuns:       mapNodeRunsToSlice(parsed, nodeRuns),
		Metadata:       withNodeRunsMetadata(runMetadata, parsed, nodeRuns),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, err := s.repo.Create(ctx, run)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	if err := s.enqueueDAGRun(ctx, dagRunTask{
		principal: principal,
		app: domainapp.App{
			ID:   applicationID,
			Name: workflowName,
		},
		input:       input,
		binding:     syntheticBinding,
		definition:  parsed,
		run:         created,
		requestMeta: requestctx.FromContext(ctx),
	}); err != nil {
		return s.failRun(context.Background(), created, parsed, fmt.Sprintf("workflow runner enqueue failed: %v", err)), nil
	}
	return created, nil
}

func (s *Service) GetSystemRun(ctx context.Context, runID string) (domainworkflow.Run, error) {
	return s.repo.Get(ctx, strings.TrimSpace(runID))
}

func (s *Service) Approve(ctx context.Context, principal domainidentity.Principal, workflowRunID, comment string) (domainworkflow.Run, error) {
	return s.resolveApproval(ctx, principal, workflowRunID, "approved", comment)
}

func (s *Service) Reject(ctx context.Context, principal domainidentity.Principal, workflowRunID, comment string) (domainworkflow.Run, error) {
	return s.resolveApproval(ctx, principal, workflowRunID, "rejected", comment)
}

func (s *Service) RecordExecutionTaskResult(ctx context.Context, task domaindelivery.ExecutionTask) error {
	runID := workflowMetadataString(task.Payload, "workflowRunId")
	nodeID := workflowMetadataString(task.Payload, "workflowNodeId")
	if runID == "" || nodeID == "" {
		return nil
	}
	nextStatus, ok := workflowStatusFromExecutionTask(task.Status)
	if !ok {
		return nil
	}
	run, err := s.repo.Get(ctx, runID)
	if err != nil {
		return err
	}
	definition, ok := definitionFromRunMetadata(run)
	if !ok {
		return fmt.Errorf("%w: workflow definition is missing", apperrors.ErrInvalidArgument)
	}
	nodeExists := false
	for _, node := range definition.Nodes {
		if node.ID == nodeID {
			nodeExists = true
			break
		}
	}
	if !nodeExists {
		return nil
	}
	nodeRuns := restoreNodeRuns(definition, run.NodeRuns)
	entry := nodeRuns[nodeID]
	if entry.Status == nextStatus && (nextStatus == "completed" || nextStatus == "failed") {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if entry.StartedAt == "" {
		entry.StartedAt = now
	}
	entry.Status = nextStatus
	entry.Summary = executionTaskWorkflowSummary(task)
	entry.FinishedAt = now
	nodeRuns[nodeID] = entry

	executionOutputs := map[string]any{
		"executionTaskId":     task.ID,
		"executionTaskStatus": strings.TrimSpace(task.Status),
		"executionTaskResult": task.Result,
	}
	recordDAGNodeOutputs(&run, nodeID, map[string]any{"outputs": executionOutputs})
	artifactState := restoredDAGArtifactState(run.Metadata)
	for key, value := range executionOutputs {
		artifactState[key] = value
	}
	run.Metadata["artifacts"] = metadataDAGArtifactState(artifactState)
	appendDAGNodeEvent(&run, nodeID, "execution_task_callback", nextStatus, entry.Summary, map[string]any{
		"executionTaskId":     task.ID,
		"executionTaskStatus": strings.TrimSpace(task.Status),
	})
	run.Status = "queued"
	run.UpdatedAt = now
	run = syncRunNodeState(run, definition, nodeRuns)
	updated := s.updateRun(ctx, run)

	app, err := s.apps.Get(ctx, updated.ApplicationID)
	if err != nil {
		return err
	}
	appInput := domainworkflow.Input{
		ApplicationID:            updated.ApplicationID,
		ApplicationEnvironmentID: strings.TrimSpace(task.ApplicationEnvironmentID),
		WorkflowName:             updated.WorkflowName,
		ClusterID:                updated.ClusterID,
		Namespace:                updated.Namespace,
		DeploymentName:           updated.DeploymentName,
		Variables:                map[string]any{},
	}
	if releaseBundleID := firstNonEmpty(task.ReleaseBundleID, workflowMetadataString(updated.Metadata, "releaseBundleId")); releaseBundleID != "" {
		appInput.Variables["releaseBundleId"] = releaseBundleID
	}
	appBinding, err := s.findApplicationEnvironmentBinding(ctx, appInput)
	if err != nil {
		return err
	}
	if appBinding == nil {
		return fmt.Errorf("%w: application environment binding is missing", apperrors.ErrInvalidArgument)
	}
	if err := s.enqueueDAGRun(ctx, dagRunTask{
		principal:   workflowSystemPrincipal(),
		app:         app,
		input:       appInput,
		binding:     *appBinding,
		definition:  definition,
		run:         updated,
		requestMeta: requestctx.FromContext(ctx),
	}); err != nil {
		s.failRun(context.Background(), updated, definition, fmt.Sprintf("workflow runner enqueue failed after execution task callback: %v", err))
		return err
	}
	return nil
}

func (s *Service) resolveApproval(ctx context.Context, principal domainidentity.Principal, workflowRunID, action, comment string) (domainworkflow.Run, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return domainworkflow.Run{}, err
	}
	approvalState, err := s.loadDAGApprovalState(ctx, principal, workflowRunID)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	if err := s.repo.CreateApproval(ctx, newDAGApproval(principal, approvalState, action, comment)); err != nil {
		return domainworkflow.Run{}, err
	}
	updated := s.applyDAGApprovalDecision(ctx, approvalState, action, comment, principal.UserName)
	if action == "rejected" {
		return updated, nil
	}
	return s.resumeDAGAfterApproval(ctx, principal, approvalState, updated)
}

func (s *Service) prepareBoundDAGRun(ctx context.Context, app domainapp.App, input domainworkflow.Input) (domainworkflow.Run, *domaincatalog.ApplicationEnvironment, dagWorkflowDefinition, bool, error) {
	binding, err := s.findApplicationEnvironmentBinding(ctx, input)
	if err != nil {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, false, err
	}
	if binding == nil || binding.WorkflowTemplate == nil || len(binding.WorkflowTemplate.Definition) == 0 {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, false, nil
	}
	definition, ok := s.dagPlanner().parse(binding.WorkflowTemplate.Definition)
	if !ok {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, false, nil
	}
	if err := s.dagPlanner().validate(definition, *binding, app, input); err != nil {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, false, err
	}
	nodeRuns := initializeNodeRuns(definition)
	now := time.Now().UTC().Format(time.RFC3339)
	workflowName := strings.TrimSpace(input.WorkflowName)
	if workflowName == "" {
		workflowName = strings.TrimSpace(binding.WorkflowTemplate.Key)
	}
	if workflowName == "" {
		workflowName = strings.TrimSpace(binding.WorkflowTemplate.Name)
	}
	if workflowName == "" {
		workflowName = "release-dag"
	}

	runMetadata := withGatewayApprovalWorkflowMetadata(map[string]any{
		"applicationName":      app.Name,
		"triggerBuild":         input.TriggerBuild,
		"triggerRelease":       input.TriggerRelease,
		"mode":                 definition.Mode,
		"executionMode":        "release_dag",
		"schemaVersion":        definition.SchemaVersion,
		"workflowTemplateId":   binding.WorkflowTemplateID,
		"workflowTemplateKey":  binding.WorkflowTemplate.Key,
		"workflowTemplateName": binding.WorkflowTemplate.Name,
		"bindingId":            binding.ID,
		"nodes":                definition.Nodes,
		"edges":                definition.Edges,
	}, input)
	run := domainworkflow.Run{
		ID:             "workflow:" + uuid.NewString(),
		ApplicationID:  input.ApplicationID,
		WorkflowName:   workflowName,
		ClusterID:      strings.TrimSpace(input.ClusterID),
		Namespace:      strings.TrimSpace(input.Namespace),
		DeploymentName: strings.TrimSpace(input.DeploymentName),
		Status:         "queued",
		Steps:          buildStepsFromNodeRuns(definition, nodeRuns),
		NodeRuns:       mapNodeRunsToSlice(definition, nodeRuns),
		Metadata:       runMetadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	run.Metadata = withNodeRunsMetadata(run.Metadata, definition, nodeRuns)
	return run, binding, definition, true, nil
}

func (s *Service) prepareValidationDAGRun(ctx context.Context, app domainapp.App, input domainworkflow.Input) (domainworkflow.Run, *domaincatalog.ApplicationEnvironment, dagWorkflowDefinition, error) {
	binding, err := s.findApplicationEnvironmentBinding(ctx, input)
	if err != nil {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, err
	}
	if binding == nil {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: application environment binding is missing", apperrors.ErrInvalidArgument)
	}
	if binding.WorkflowTemplate == nil || len(binding.WorkflowTemplate.Definition) == 0 {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow template is required", apperrors.ErrInvalidArgument)
	}
	sourceDefinition, ok := s.dagPlanner().parse(binding.WorkflowTemplate.Definition)
	if !ok {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow definition is not a supported DAG", apperrors.ErrInvalidArgument)
	}
	definition := validationOnlyDAGDefinition(sourceDefinition)
	if len(definition.Nodes) == 0 {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow template has no validation nodes", apperrors.ErrInvalidArgument)
	}
	if err := s.dagPlanner().validate(definition, *binding, app, input); err != nil {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, err
	}
	nodeRuns := initializeNodeRuns(definition)
	now := time.Now().UTC().Format(time.RFC3339)
	workflowName := strings.TrimSpace(input.WorkflowName)
	if workflowName == "" {
		workflowName = firstNonEmpty(strings.TrimSpace(binding.WorkflowTemplate.Key), strings.TrimSpace(binding.WorkflowTemplate.Name), "validation")
	}
	if !strings.Contains(strings.ToLower(workflowName), "validation") {
		workflowName = workflowName + "-validation"
	}

	runMetadata := withGatewayApprovalWorkflowMetadata(map[string]any{
		"applicationName":      app.Name,
		"triggerBuild":         false,
		"triggerRelease":       false,
		"mode":                 definition.Mode,
		"executionMode":        "release_dag",
		"runMode":              "validation",
		"schemaVersion":        definition.SchemaVersion,
		"workflowTemplateId":   binding.WorkflowTemplateID,
		"workflowTemplateKey":  binding.WorkflowTemplate.Key,
		"workflowTemplateName": binding.WorkflowTemplate.Name,
		"bindingId":            binding.ID,
		"nodes":                definition.Nodes,
		"edges":                definition.Edges,
		"sourceNodeCount":      len(sourceDefinition.Nodes),
		"sourceEdgeCount":      len(sourceDefinition.Edges),
	}, input)
	run := domainworkflow.Run{
		ID:             "workflow:" + uuid.NewString(),
		ApplicationID:  input.ApplicationID,
		WorkflowName:   workflowName,
		ClusterID:      strings.TrimSpace(input.ClusterID),
		Namespace:      strings.TrimSpace(input.Namespace),
		DeploymentName: strings.TrimSpace(input.DeploymentName),
		Status:         "queued",
		Steps:          buildStepsFromNodeRuns(definition, nodeRuns),
		NodeRuns:       mapNodeRunsToSlice(definition, nodeRuns),
		Metadata:       runMetadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	run.Metadata = withNodeRunsMetadata(run.Metadata, definition, nodeRuns)
	return run, binding, definition, nil
}

func (s *Service) prepareRollbackDAGRun(ctx context.Context, app domainapp.App, input domainworkflow.Input) (domainworkflow.Run, *domaincatalog.ApplicationEnvironment, dagWorkflowDefinition, error) {
	binding, err := s.findApplicationEnvironmentBinding(ctx, input)
	if err != nil {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, err
	}
	if binding == nil {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: application environment binding is missing", apperrors.ErrInvalidArgument)
	}
	if binding.WorkflowTemplate == nil || len(binding.WorkflowTemplate.Definition) == 0 {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow template is required", apperrors.ErrInvalidArgument)
	}
	sourceDefinition, ok := s.dagPlanner().parse(binding.WorkflowTemplate.Definition)
	if !ok {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow definition is not a supported DAG", apperrors.ErrInvalidArgument)
	}
	definition := rollbackOnlyDAGDefinition(sourceDefinition)
	if len(definition.Nodes) == 0 {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow template has no rollback nodes", apperrors.ErrInvalidArgument)
	}
	if err := s.dagPlanner().validate(definition, *binding, app, input); err != nil {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, err
	}
	nodeRuns := initializeNodeRuns(definition)
	now := time.Now().UTC().Format(time.RFC3339)
	workflowName := strings.TrimSpace(input.WorkflowName)
	if workflowName == "" {
		workflowName = firstNonEmpty(strings.TrimSpace(binding.WorkflowTemplate.Key), strings.TrimSpace(binding.WorkflowTemplate.Name), "rollback")
	}
	if !strings.Contains(strings.ToLower(workflowName), "rollback") {
		workflowName = workflowName + "-rollback"
	}

	runMetadata := withGatewayApprovalWorkflowMetadata(map[string]any{
		"applicationName":      app.Name,
		"triggerBuild":         false,
		"triggerRelease":       false,
		"rollbackOnly":         true,
		"mode":                 definition.Mode,
		"executionMode":        "release_dag",
		"runMode":              "rollback",
		"schemaVersion":        definition.SchemaVersion,
		"workflowTemplateId":   binding.WorkflowTemplateID,
		"workflowTemplateKey":  binding.WorkflowTemplate.Key,
		"workflowTemplateName": binding.WorkflowTemplate.Name,
		"bindingId":            binding.ID,
		"nodes":                definition.Nodes,
		"edges":                definition.Edges,
		"sourceNodeCount":      len(sourceDefinition.Nodes),
		"sourceEdgeCount":      len(sourceDefinition.Edges),
	}, input)
	if releaseBundleID := workflowMetadataString(input.Variables, "releaseBundleId"); releaseBundleID != "" {
		runMetadata["releaseBundleId"] = releaseBundleID
	}
	run := domainworkflow.Run{
		ID:             "workflow:" + uuid.NewString(),
		ApplicationID:  input.ApplicationID,
		WorkflowName:   workflowName,
		ClusterID:      strings.TrimSpace(input.ClusterID),
		Namespace:      strings.TrimSpace(input.Namespace),
		DeploymentName: strings.TrimSpace(input.DeploymentName),
		Status:         "queued",
		Steps:          buildStepsFromNodeRuns(definition, nodeRuns),
		NodeRuns:       mapNodeRunsToSlice(definition, nodeRuns),
		Metadata:       runMetadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	run.Metadata = withNodeRunsMetadata(run.Metadata, definition, nodeRuns)
	return run, binding, definition, nil
}

func (s *Service) runDAGAsync(ctx context.Context, principal domainidentity.Principal, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, definition dagWorkflowDefinition, run domainworkflow.Run) {
	newDAGRunExecutor(s).run(ctx, principal, app, input, binding, definition, run)
}

func (s *Service) findApplicationEnvironmentBinding(ctx context.Context, input domainworkflow.Input) (*domaincatalog.ApplicationEnvironment, error) {
	if s.catalog == nil {
		return nil, nil
	}
	items, err := s.catalog.ListApplicationEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	if bindingID := strings.TrimSpace(input.ApplicationEnvironmentID); bindingID != "" {
		for _, item := range items {
			if item.ID == bindingID && item.ApplicationID == input.ApplicationID {
				copyItem := item
				return &copyItem, nil
			}
		}
		return nil, fmt.Errorf("%w: application environment binding not found", apperrors.ErrInvalidArgument)
	}
	for _, item := range items {
		if item.ApplicationID != input.ApplicationID {
			continue
		}
		for _, target := range item.Targets {
			if !target.Enabled {
				continue
			}
			if target.ClusterID == strings.TrimSpace(input.ClusterID) &&
				target.Namespace == strings.TrimSpace(input.Namespace) &&
				target.WorkloadName == strings.TrimSpace(input.DeploymentName) {
				copyItem := item
				return &copyItem, nil
			}
		}
	}
	return nil, nil
}

func (s *Service) finalizeRunCancellation(ctx context.Context, run domainworkflow.Run, definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun, statuses map[string]string, err error) {
	reason := "workflow execution canceled"
	if err != nil {
		reason = err.Error()
	}
	for _, node := range definition.Nodes {
		entry := nodeRuns[node.ID]
		switch entry.Status {
		case "completed", "failed", "skipped":
			statuses[node.ID] = entry.Status
		case "running":
			entry.Status = "failed"
			entry.Summary = reason
			entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			nodeRuns[node.ID] = entry
			statuses[node.ID] = entry.Status
		default:
			entry.Status = "skipped"
			entry.Summary = reason
			entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			nodeRuns[node.ID] = entry
			statuses[node.ID] = entry.Status
		}
	}
	run.Status = "failed"
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	run = syncRunNodeState(run, definition, nodeRuns)
	s.updateRun(context.WithoutCancel(ctx), run)
}

func (s *Service) failRun(ctx context.Context, run domainworkflow.Run, definition dagWorkflowDefinition, reason string) domainworkflow.Run {
	nodeRuns := initializeNodeRuns(definition)
	for _, node := range definition.Nodes {
		entry := nodeRuns[node.ID]
		entry.Status = "skipped"
		entry.Summary = reason
		entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		nodeRuns[node.ID] = entry
	}
	run.Status = "failed"
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	run = syncRunNodeState(run, definition, nodeRuns)
	return s.updateRun(ctx, run)
}

func (s *Service) updateRun(ctx context.Context, run domainworkflow.Run) domainworkflow.Run {
	persistCtx := ctx
	if persistCtx == nil {
		persistCtx = context.Background()
	}
	if err := persistCtx.Err(); err != nil {
		persistCtx = context.WithoutCancel(persistCtx)
	}
	updated, err := s.repo.Update(persistCtx, run)
	if err != nil {
		return run
	}
	s.publishRunUpdate(updated)
	return updated
}

func (s *Service) publishRunUpdate(run domainworkflow.Run) {
	s.runState.publish(run)
}

func (s *Service) waitForRunStatus(ctx context.Context, runID string, statuses ...string) (domainworkflow.Run, error) {
	return s.runState.wait(ctx, runID, statuses...)
}

func (s *Service) queueDepth() int {
	return s.scheduler.depth()
}

func (s *Service) logWarnCtx(ctx context.Context, message string, fields ...zap.Field) {
	if s.logger != nil {
		s.logger.Warn(message, append(requestctx.LoggerFields(requestctx.FromContext(ctx)), fields...)...)
	}
}

func (s *Service) logDebugCtx(ctx context.Context, message string, fields ...zap.Field) {
	if s.logger != nil {
		s.logger.Debug(message, append(requestctx.LoggerFields(requestctx.FromContext(ctx)), fields...)...)
	}
}

func incomingEdgesForNode(definition dagWorkflowDefinition, nodeID string) []dagWorkflowEdge {
	items := make([]dagWorkflowEdge, 0)
	for _, edge := range definition.Edges {
		if edge.Target == nodeID {
			items = append(items, edge)
		}
	}
	return items
}

func isExternalDAGExecutionNode(node dagWorkflowNode) bool {
	switch strings.TrimSpace(firstNonEmpty(node.ExecutorKind, configString(node.Config, "executorKind"))) {
	case "mcp", "webhook_callback", "external_pipeline", "external_pipeline_adapter", "ci_agent_runner":
		return true
	default:
		return false
	}
}

func isApplicationMissing(err error) bool {
	return errors.Is(err, apperrors.ErrNotFound)
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, app domainapp.App, resourceName string) error {
	if s.authorizer == nil {
		return nil
	}
	decision, err := s.authorizer.Authorize(ctx, domainaccess.Request{
		Principal: principal,
		Action:    action,
		Subject: domainaccess.SubjectAttributes{
			UserID:   principal.UserID,
			Roles:    principal.Roles,
			Teams:    principal.Teams,
			Projects: principal.Projects,
			Tags:     principal.Tags,
		},
		Resource: domainaccess.ResourceAttributes{
			Kind:  "Workflow",
			Name:  resourceName,
			Owner: app.Key,
		},
		Delivery: domainaccess.DeliveryAttributes{
			BusinessLineID:   app.BusinessLineID,
			ApplicationGroup: app.Group,
			ApplicationID:    app.ID,
		},
		Context: domainaccess.ContextAttributes{
			Source:     requestctx.FromContext(ctx).Source,
			OccurredAt: time.Now().UTC(),
		},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return nil
}

func (s *Service) authorizePermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func uniqueIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
