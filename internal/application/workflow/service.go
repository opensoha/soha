package workflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
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
	apprepo "github.com/opensoha/soha/internal/repository/application"
	"go.uber.org/zap"
)

const (
	defaultAsyncWorkflowWorkers   = 4
	defaultAsyncWorkflowQueueSize = 64
	defaultDAGNodeConcurrency     = 4
	workflowStatusWaitingApproval = "waiting_approval"
)

var gatewayApprovalWorkflowMetadataKeys = []string{
	"aiGatewayApprovalRequestId",
	"aiGatewayApprovalPolicyRef",
	"aiGatewayPolicyId",
	"aiGatewayToolName",
	"aiGatewaySkillId",
	"aiGatewayAIClientId",
}

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

type ResourceExecutor interface {
	GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error)
	ListDeploymentRolloutHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.RolloutHistoryView, error)
	RollbackDeployment(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.DeploymentRollbackView, error)
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
	RestartDeployment(context.Context, domainidentity.Principal, string, string, string) error
	ScaleDeployment(context.Context, domainidentity.Principal, string, string, string, int32) error
	DeletePod(context.Context, domainidentity.Principal, string, string, string) error
}

type AlertMutator interface {
	CreateWorkflowSilence(context.Context, domainidentity.Principal, domainalert.SilenceInput) (domainalert.AlertSilence, error)
}

type ArtifactStore interface {
	UpsertExecutionArtifact(context.Context, domaindelivery.ExecutionArtifact) (domaindelivery.ExecutionArtifact, error)
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
	httpClient  *http.Client
	logger      *zap.Logger
	metrics     *runtimeobs.Registry

	runnerMu          sync.Mutex
	runnerCtx         context.Context
	runnerCancel      context.CancelFunc
	runnerQueue       chan dagRunTask
	runnerClosed      bool
	runnerStarted     bool
	runnerWorkerCount int
	runnerQueueSize   int
	nodeParallelism   int
	runnerWG          sync.WaitGroup

	runStateMu   sync.Mutex
	runSnapshots map[string]domainworkflow.Run
	runWaiters   map[string]map[chan struct{}]struct{}
}

type workflowPruner interface {
	DeleteByIDs(context.Context, []string) error
}

type dagRunTask struct {
	principal   domainidentity.Principal
	app         domainapp.App
	input       domainworkflow.Input
	binding     domaincatalog.ApplicationEnvironment
	definition  dagWorkflowDefinition
	run         domainworkflow.Run
	requestMeta requestctx.Metadata
}

func New(repo Repository, apps ApplicationReader, authorizer domainaccess.Authorizer, permissions *appaccess.PermissionResolver, catalog CatalogReader, builds BuildExecutor, releases ReleaseExecutor, resources ResourceExecutor) *Service {
	return &Service{
		repo:              repo,
		apps:              apps,
		authorizer:        authorizer,
		permissions:       permissions,
		catalog:           catalog,
		builds:            builds,
		releases:          releases,
		resources:         resources,
		httpClient:        &http.Client{Timeout: 10 * time.Second},
		runnerWorkerCount: defaultAsyncWorkflowWorkers,
		runnerQueueSize:   defaultAsyncWorkflowQueueSize,
		nodeParallelism:   defaultDAGNodeConcurrency,
	}
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
}

func (s *Service) SetRuntimeOptions(workerCount, queueSize, nodeParallelism int) {
	if workerCount > 0 {
		s.runnerWorkerCount = workerCount
	}
	if queueSize > 0 {
		s.runnerQueueSize = queueSize
	}
	if nodeParallelism > 0 {
		s.nodeParallelism = nodeParallelism
	}
}

func (s *Service) Start(ctx context.Context) {
	s.ensureRunner(ctx)
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.runnerMu.Lock()
	if !s.runnerStarted {
		s.runnerClosed = true
		s.runnerMu.Unlock()
		return nil
	}
	s.runnerClosed = true
	cancel := s.runnerCancel
	queue := s.runnerQueue
	s.runnerMu.Unlock()

	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		s.runnerWG.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	for {
		select {
		case task := <-queue:
			s.failRun(ctx, task.run, task.definition, "workflow execution canceled before start")
		default:
			return nil
		}
	}
}

func (s *Service) ensureRunner(ctx context.Context) {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()

	if s.runnerStarted || s.runnerClosed {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	runnerCtx, cancel := context.WithCancel(ctx)
	s.runnerCtx = runnerCtx
	s.runnerCancel = cancel
	queueSize := s.runnerQueueSize
	if queueSize <= 0 {
		queueSize = defaultAsyncWorkflowQueueSize
	}
	s.runnerQueue = make(chan dagRunTask, queueSize)
	s.runnerStarted = true

	workerCount := s.runnerWorkerCount
	if workerCount <= 0 {
		workerCount = defaultAsyncWorkflowWorkers
	}
	for i := 0; i < workerCount; i++ {
		s.runnerWG.Add(1)
		go s.runAsyncWorker(runnerCtx)
	}
}

func (s *Service) runAsyncWorker(ctx context.Context) {
	defer s.runnerWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-s.runnerQueue:
			if !ok {
				return
			}
			taskCtx := requestctx.WithMetadata(ctx, task.requestMeta)
			s.runDAGAsync(taskCtx, task.principal, task.app, task.input, task.binding, task.definition, task.run)
		}
	}
}

func (s *Service) enqueueDAGRun(ctx context.Context, task dagRunTask) error {
	s.ensureRunner(context.Background())

	s.runnerMu.Lock()
	queue := s.runnerQueue
	runnerCtx := s.runnerCtx
	closed := s.runnerClosed
	s.runnerMu.Unlock()

	if closed || queue == nil || runnerCtx == nil {
		if s.metrics != nil {
			s.metrics.RecordFinish(runtimeobs.ComponentWorkflowRunner, "enqueue", 0, 0, 0, runtimeobs.OutcomeFailed, fmt.Errorf("workflow runner is not available"))
		}
		return fmt.Errorf("workflow runner is not available")
	}

	select {
	case queue <- task:
		if s.metrics != nil {
			s.metrics.SetQueueDepth(runtimeobs.ComponentWorkflowRunner, len(queue))
		}
		s.logDebugCtx(ctx, "workflow queued", zap.String("runID", task.run.ID), zap.String("applicationID", task.run.ApplicationID), zap.Int("queueDepth", len(queue)))
		return nil
	case <-runnerCtx.Done():
		if s.metrics != nil {
			s.metrics.RecordFinish(runtimeobs.ComponentWorkflowRunner, task.run.ID, 0, len(queue), 1, runtimeobs.OutcomeFailed, fmt.Errorf("workflow runner stopped"))
		}
		return fmt.Errorf("workflow runner stopped")
	case <-ctx.Done():
		if s.metrics != nil {
			s.metrics.RecordFinish(runtimeobs.ComponentWorkflowRunner, task.run.ID, 0, len(queue), 1, runtimeobs.OutcomeCanceled, ctx.Err())
		}
		return ctx.Err()
	}
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
	parsed, ok := parseDAGWorkflowDefinition(definition)
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
	if err := validateDAGExecutionDefinition(parsed, syntheticBinding, domainapp.App{ID: applicationID, Name: workflowName}, input); err != nil {
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

func (s *Service) resolveApproval(ctx context.Context, principal domainidentity.Principal, workflowRunID, action, comment string) (domainworkflow.Run, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return domainworkflow.Run{}, err
	}
	run, err := s.repo.Get(ctx, strings.TrimSpace(workflowRunID))
	if err != nil {
		return domainworkflow.Run{}, err
	}
	app, err := s.apps.Get(ctx, run.ApplicationID)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionTrigger, app, run.ApplicationID); err != nil {
		return domainworkflow.Run{}, err
	}
	if run.Status != workflowStatusWaitingApproval {
		return domainworkflow.Run{}, fmt.Errorf("%w: workflow is not waiting for approval", apperrors.ErrInvalidArgument)
	}
	definition, ok := definitionFromRunMetadata(run)
	if !ok {
		return domainworkflow.Run{}, fmt.Errorf("%w: workflow definition is missing", apperrors.ErrInvalidArgument)
	}
	nodeRuns := run.NodeRuns
	if len(nodeRuns) == 0 {
		return domainworkflow.Run{}, fmt.Errorf("%w: workflow has no node runs", apperrors.ErrInvalidArgument)
	}
	pendingNodeID := ""
	for _, item := range nodeRuns {
		if item.Type == "manual_approval" && item.Status == workflowStatusWaitingApproval {
			pendingNodeID = item.NodeID
			break
		}
	}
	if pendingNodeID == "" {
		return domainworkflow.Run{}, fmt.Errorf("%w: approval node not found", apperrors.ErrInvalidArgument)
	}
	approval := domainworkflow.Approval{
		ID:            uuid.NewString(),
		WorkflowRunID: run.ID,
		NodeID:        pendingNodeID,
		Action:        action,
		Comment:       strings.TrimSpace(comment),
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Metadata:      workflowApprovalGatewayMetadata(run, pendingNodeID),
		CreatedAt:     time.Now().UTC(),
	}
	if err := s.repo.CreateApproval(ctx, approval); err != nil {
		return domainworkflow.Run{}, err
	}
	nextStatus := "completed"
	nextSummary := fmt.Sprintf("approved by %s", principal.UserName)
	runStatus := "running"
	if action == "rejected" {
		nextStatus = "failed"
		nextSummary = fmt.Sprintf("rejected by %s", principal.UserName)
		runStatus = "failed"
	}
	for index := range nodeRuns {
		if nodeRuns[index].NodeID == pendingNodeID {
			nodeRuns[index].Status = nextStatus
			nodeRuns[index].Summary = nextSummary
			nodeRuns[index].FinishedAt = time.Now().UTC().Format(time.RFC3339)
		}
	}
	run.NodeRuns = nodeRuns
	run.Status = runStatus
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if run.Metadata == nil {
		run.Metadata = map[string]any{}
	}
	run.Metadata["approvalDecision"] = action
	run.Metadata["approvalComment"] = strings.TrimSpace(comment)
	run = syncRunNodeState(run, definition, restoreNodeRuns(definition, run.NodeRuns))
	updated := s.updateRun(ctx, run)
	if action == "rejected" {
		return updated, nil
	}

	appInput := domainworkflow.Input{
		ApplicationID:  run.ApplicationID,
		WorkflowName:   run.WorkflowName,
		ClusterID:      run.ClusterID,
		Namespace:      run.Namespace,
		DeploymentName: run.DeploymentName,
	}
	appBinding, err := s.findApplicationEnvironmentBinding(ctx, appInput)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	if appBinding == nil {
		return domainworkflow.Run{}, fmt.Errorf("%w: application environment binding is missing", apperrors.ErrInvalidArgument)
	}
	updated.Status = "queued"
	updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	updated = s.updateRun(ctx, updated)
	if err := s.enqueueDAGRun(ctx, dagRunTask{
		principal:   principal,
		app:         app,
		input:       appInput,
		binding:     *appBinding,
		definition:  definition,
		run:         updated,
		requestMeta: requestctx.FromContext(ctx),
	}); err != nil {
		return s.failRun(context.Background(), updated, definition, fmt.Sprintf("workflow runner enqueue failed after approval: %v", err)), nil
	}
	return updated, nil
}

type dagWorkflowNode struct {
	ID                  string
	Name                string
	Type                string
	TimeoutSeconds      int
	ContinueOnFailure   bool
	Config              map[string]any
	Inputs              []string
	Outputs             []string
	ServiceSelector     map[string]any
	EnvironmentSelector map[string]any
	TargetSelector      map[string]any
	ArtifactOutputs     []map[string]any
	RunCondition        string
	FailurePolicy       string
	FanOutStrategy      string
	FanOutBatchSize     int
	FanOutFailurePolicy string
	Observability       map[string]any
}

type dagWorkflowEdge struct {
	ID        string
	Source    string
	Target    string
	Condition string
}

type dagWorkflowDefinition struct {
	SchemaVersion int
	Mode          string
	Nodes         []dagWorkflowNode
	Edges         []dagWorkflowEdge
}

type dagExecutionResult struct {
	nodeID    string
	step      domainworkflow.Step
	status    string
	summary   string
	inputs    map[string]any
	outputs   map[string]any
	artifacts map[string]any
	selectors map[string]any
	events    []map[string]any
}

type dagNodeRun = domainworkflow.NodeRun

func (s *Service) prepareBoundDAGRun(ctx context.Context, app domainapp.App, input domainworkflow.Input) (domainworkflow.Run, *domaincatalog.ApplicationEnvironment, dagWorkflowDefinition, bool, error) {
	binding, err := s.findApplicationEnvironmentBinding(ctx, input)
	if err != nil {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, false, err
	}
	if binding == nil || binding.WorkflowTemplate == nil || len(binding.WorkflowTemplate.Definition) == 0 {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, false, nil
	}
	definition, ok := parseDAGWorkflowDefinition(binding.WorkflowTemplate.Definition)
	if !ok {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, false, nil
	}
	if err := validateDAGExecutionDefinition(definition, *binding, app, input); err != nil {
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
	sourceDefinition, ok := parseDAGWorkflowDefinition(binding.WorkflowTemplate.Definition)
	if !ok {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow definition is not a supported DAG", apperrors.ErrInvalidArgument)
	}
	definition := validationOnlyDAGDefinition(sourceDefinition)
	if len(definition.Nodes) == 0 {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow template has no validation nodes", apperrors.ErrInvalidArgument)
	}
	if err := validateDAGExecutionDefinition(definition, *binding, app, input); err != nil {
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
	sourceDefinition, ok := parseDAGWorkflowDefinition(binding.WorkflowTemplate.Definition)
	if !ok {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow definition is not a supported DAG", apperrors.ErrInvalidArgument)
	}
	definition := rollbackOnlyDAGDefinition(sourceDefinition)
	if len(definition.Nodes) == 0 {
		return domainworkflow.Run{}, nil, dagWorkflowDefinition{}, fmt.Errorf("%w: workflow template has no rollback nodes", apperrors.ErrInvalidArgument)
	}
	if err := validateDAGExecutionDefinition(definition, *binding, app, input); err != nil {
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
	run = cloneRunForAsyncWorker(run)

	startedAt := time.Now()
	if s.metrics != nil {
		s.metrics.RecordStart(runtimeobs.ComponentWorkflowRunner, run.ID, s.queueDepth(), len(definition.Nodes))
	}
	s.logDebugCtx(ctx, "workflow execution started", zap.String("runID", run.ID), zap.String("applicationID", run.ApplicationID))
	nodeRuns := restoreNodeRuns(definition, run.NodeRuns)
	statuses := collectNodeStatuses(nodeRuns)
	artifactState := make(map[string]any)
	stopFailureSources := make(map[string]struct{})

	run.Status = "running"
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	ensureDAGExecutionMetadata(&run)
	appendDAGRunEvent(&run, map[string]any{
		"type":   "workflow_started",
		"status": run.Status,
		"mode":   definition.Mode,
	})
	run = syncRunNodeState(run, definition, nodeRuns)
	run = s.updateRun(ctx, run)

	for len(statuses) < len(definition.Nodes) {
		if err := ctx.Err(); err != nil {
			s.finalizeRunCancellation(ctx, run, definition, nodeRuns, statuses, err)
			if s.metrics != nil {
				s.metrics.RecordFinish(runtimeobs.ComponentWorkflowRunner, run.ID, time.Since(startedAt), s.queueDepth(), len(definition.Nodes), runtimeobs.OutcomeCanceled, err)
			}
			s.logWarnCtx(ctx, "workflow execution canceled", zap.String("runID", run.ID), zap.Error(err))
			return
		}

		ready := make([]dagWorkflowNode, 0)
		progressed := false
		for _, node := range definition.Nodes {
			if statuses[node.ID] != "" {
				continue
			}
			isReady, skipped := resolveDAGNodeReadiness(definition, node, incomingEdgesForNode(definition, node.ID), statuses)
			switch {
			case skipped:
				entry := nodeRuns[node.ID]
				entry.Status = "skipped"
				entry.Summary = "conditions not met"
				entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				nodeRuns[node.ID] = entry
				statuses[node.ID] = entry.Status
				appendDAGNodeEvent(&run, node.ID, "node_skipped", entry.Status, entry.Summary, map[string]any{"reason": "dependency_condition"})
				progressed = true
			case isReady:
				if shouldStopDAGNodeAfterFailure(definition, node.ID, stopFailureSources) {
					entry := nodeRuns[node.ID]
					entry.Status = "skipped"
					entry.Summary = "stopped after failure policy"
					entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
					nodeRuns[node.ID] = entry
					statuses[node.ID] = entry.Status
					appendDAGNodeEvent(&run, node.ID, "node_skipped", entry.Status, entry.Summary, map[string]any{"reason": "failure_policy_stop"})
					progressed = true
					continue
				}
				runConditionMatched, runConditionReason := evaluateDAGRunCondition(node.RunCondition, app, input, binding, artifactState)
				if !runConditionMatched {
					entry := nodeRuns[node.ID]
					entry.Status = "skipped"
					entry.Summary = runConditionReason
					entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
					nodeRuns[node.ID] = entry
					statuses[node.ID] = entry.Status
					appendDAGNodeEvent(&run, node.ID, "node_skipped", entry.Status, entry.Summary, map[string]any{"reason": "run_condition", "runCondition": node.RunCondition})
					recordDAGNodeOutputs(&run, node.ID, map[string]any{
						"runCondition": map[string]any{
							"expression": node.RunCondition,
							"matched":    false,
							"reason":     runConditionReason,
						},
					})
					progressed = true
					continue
				}
				ready = append(ready, node)
			}
		}
		if progressed {
			run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			run = syncRunNodeState(run, definition, nodeRuns)
			run = s.updateRun(ctx, run)
		}
		if len(ready) == 0 {
			if hasWaitingApproval(nodeRuns) {
				run.Status = workflowStatusWaitingApproval
				run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				run = syncRunNodeState(run, definition, nodeRuns)
				s.updateRun(ctx, run)
				return
			}
			if !progressed {
				break
			}
			continue
		}

		for _, node := range ready {
			entry := nodeRuns[node.ID]
			entry.Status = "running"
			entry.StartedAt = time.Now().UTC().Format(time.RFC3339)
			nodeRuns[node.ID] = entry
			appendDAGNodeEvent(&run, node.ID, "node_started", entry.Status, "", nil)
		}
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		run = syncRunNodeState(run, definition, nodeRuns)
		run = s.updateRun(ctx, run)

		for _, result := range s.executeReadyDAGNodes(ctx, principal, app, input, binding, ready, run, artifactState) {
			entry := nodeRuns[result.nodeID]
			entry.Status = result.status
			entry.Summary = result.summary
			entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			nodeRuns[result.nodeID] = entry
			statuses[result.nodeID] = result.status
			recordDAGNodeOutputs(&run, result.nodeID, metadataDAGExecutionResult(result))
			for _, event := range result.events {
				appendDAGNodeEvent(&run, result.nodeID, mapString(event, "type"), result.status, result.summary, event)
			}
			for key, value := range result.outputs {
				artifactState[key] = value
			}
			for key, value := range result.artifacts {
				artifactState[key] = value
			}
			if len(result.outputs) > 0 || len(result.artifacts) > 0 {
				if run.Metadata == nil {
					run.Metadata = map[string]any{}
				}
				run.Metadata["artifacts"] = metadataDAGArtifactState(artifactState)
			}
			if result.status == "failed" {
				policy := effectiveDAGFailurePolicy(definition, nodeByID(definition, result.nodeID))
				if policy == "stop" {
					stopFailureSources[result.nodeID] = struct{}{}
				}
				recordDAGFailurePolicy(&run, result.nodeID, policy, result.summary)
			}
		}
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		run = syncRunNodeState(run, definition, nodeRuns)
		run = s.updateRun(ctx, run)
	}

	finalStatus := "completed"
	for _, node := range definition.Nodes {
		status := statuses[node.ID]
		if status == "failed" && dagFailureCountsAsWorkflowFailure(definition, node) {
			finalStatus = "failed"
			break
		}
		if status == "" {
			entry := nodeRuns[node.ID]
			entry.Status = "skipped"
			entry.Summary = "unresolved DAG dependency"
			entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			nodeRuns[node.ID] = entry
			statuses[node.ID] = entry.Status
		}
	}
	run.Status = finalStatus
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	run = syncRunNodeState(run, definition, nodeRuns)
	s.updateRun(ctx, run)
	if s.metrics != nil {
		outcome := runtimeobs.OutcomeSucceeded
		var err error
		if finalStatus == "failed" {
			outcome = runtimeobs.OutcomeFailed
			err = fmt.Errorf("workflow finished with status %s", finalStatus)
		}
		s.metrics.RecordFinish(runtimeobs.ComponentWorkflowRunner, run.ID, time.Since(startedAt), s.queueDepth(), len(definition.Nodes), outcome, err)
	}
	if finalStatus == "failed" {
		s.logWarnCtx(ctx, "workflow execution failed", zap.String("runID", run.ID), zap.String("applicationID", run.ApplicationID), zap.Duration("duration", time.Since(startedAt)))
		return
	}
	s.logDebugCtx(ctx, "workflow execution completed", zap.String("runID", run.ID), zap.String("applicationID", run.ApplicationID), zap.Duration("duration", time.Since(startedAt)))
}

func (s *Service) executeBoundDAGWorkflow(ctx context.Context, principal domainidentity.Principal, app domainapp.App, input domainworkflow.Input) (domainworkflow.Run, bool, error) {
	binding, err := s.findApplicationEnvironmentBinding(ctx, input)
	if err != nil {
		return domainworkflow.Run{}, false, err
	}
	if binding == nil || binding.WorkflowTemplate == nil || len(binding.WorkflowTemplate.Definition) == 0 {
		return domainworkflow.Run{}, false, nil
	}
	definition, ok := parseDAGWorkflowDefinition(binding.WorkflowTemplate.Definition)
	if !ok {
		return domainworkflow.Run{}, false, nil
	}
	if err := validateDAGExecutionDefinition(definition, *binding, app, input); err != nil {
		return domainworkflow.Run{}, true, err
	}

	steps, metadata, status, err := s.executeDAGWorkflow(ctx, principal, app, input, *binding, definition)
	if err != nil {
		return domainworkflow.Run{}, true, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return domainworkflow.Run{
		ID:             "workflow:" + uuid.NewString(),
		ApplicationID:  input.ApplicationID,
		WorkflowName:   strings.TrimSpace(input.WorkflowName),
		ClusterID:      strings.TrimSpace(input.ClusterID),
		Namespace:      strings.TrimSpace(input.Namespace),
		DeploymentName: strings.TrimSpace(input.DeploymentName),
		Status:         status,
		Steps:          steps,
		Metadata:       metadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, true, nil
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

func parseDAGWorkflowDefinition(definition map[string]any) (dagWorkflowDefinition, bool) {
	mode, _ := definition["mode"].(string)
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "release_dag"
	}
	if mode != "release_dag" && mode != "delivery_dag" {
		return dagWorkflowDefinition{}, false
	}
	nodeItems, ok := toMapSlice(definition["nodes"])
	if !ok || len(nodeItems) == 0 {
		return dagWorkflowDefinition{}, false
	}
	edgeItems, _ := toMapSlice(definition["edges"])
	nodes := make([]dagWorkflowNode, 0, len(nodeItems))
	for _, item := range nodeItems {
		fanOut := toConfigMap(item["fanOut"])
		nodes = append(nodes, dagWorkflowNode{
			ID:                  strings.TrimSpace(fmt.Sprint(item["id"])),
			Name:                strings.TrimSpace(fmt.Sprint(item["name"])),
			Type:                strings.TrimSpace(fmt.Sprint(item["type"])),
			TimeoutSeconds:      toInt(item["timeoutSeconds"], 300),
			ContinueOnFailure:   toBool(item["continueOnFailure"]),
			Config:              toConfigMap(item["config"]),
			Inputs:              toStringSlice(item["inputs"]),
			Outputs:             toStringSlice(item["outputs"]),
			ServiceSelector:     toConfigMap(item["serviceSelector"]),
			EnvironmentSelector: toConfigMap(item["environmentSelector"]),
			TargetSelector:      toConfigMap(item["targetSelector"]),
			ArtifactOutputs:     toMapSliceOrEmpty(item["artifactOutputs"]),
			RunCondition:        mapString(item, "runCondition"),
			FailurePolicy:       mapString(item, "failurePolicy"),
			FanOutStrategy:      firstNonEmpty(mapString(item, "fanOutStrategy"), mapString(fanOut, "strategy")),
			FanOutBatchSize:     firstPositiveInt(toInt(item["fanOutBatchSize"], 0), toInt(fanOut["batchSize"], 0)),
			FanOutFailurePolicy: firstNonEmpty(mapString(item, "fanOutFailurePolicy"), mapString(fanOut, "failurePolicy")),
			Observability:       toConfigMap(item["observability"]),
		})
	}
	edges := make([]dagWorkflowEdge, 0, len(edgeItems))
	for _, item := range edgeItems {
		edges = append(edges, dagWorkflowEdge{
			ID:        strings.TrimSpace(fmt.Sprint(item["id"])),
			Source:    strings.TrimSpace(fmt.Sprint(item["source"])),
			Target:    strings.TrimSpace(fmt.Sprint(item["target"])),
			Condition: strings.TrimSpace(fmt.Sprint(item["condition"])),
		})
	}
	return dagWorkflowDefinition{
		SchemaVersion: toInt(definition["schemaVersion"], 2),
		Mode:          mode,
		Nodes:         nodes,
		Edges:         edges,
	}, true
}

func validationOnlyDAGDefinition(definition dagWorkflowDefinition) dagWorkflowDefinition {
	return filterDAGDefinition(definition, isValidationDAGNode)
}

func rollbackOnlyDAGDefinition(definition dagWorkflowDefinition) dagWorkflowDefinition {
	return filterDAGDefinition(definition, isRollbackDAGNode)
}

func filterDAGDefinition(definition dagWorkflowDefinition, keep func(string) bool) dagWorkflowDefinition {
	allowed := make(map[string]struct{})
	nodes := make([]dagWorkflowNode, 0)
	for _, node := range definition.Nodes {
		if !keep(node.Type) {
			continue
		}
		nodes = append(nodes, node)
		allowed[node.ID] = struct{}{}
	}
	edges := make([]dagWorkflowEdge, 0)
	for _, edge := range definition.Edges {
		if _, ok := allowed[edge.Source]; !ok {
			continue
		}
		if _, ok := allowed[edge.Target]; !ok {
			continue
		}
		edges = append(edges, edge)
	}
	return dagWorkflowDefinition{
		SchemaVersion: definition.SchemaVersion,
		Mode:          definition.Mode,
		Nodes:         nodes,
		Edges:         edges,
	}
}

func workflowMetadataMode(metadata map[string]any) string {
	mode := strings.TrimSpace(fmt.Sprint(metadata["mode"]))
	if mode == "delivery_dag" {
		return mode
	}
	return "release_dag"
}

func mapString(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func validateDAGExecutionDefinition(definition dagWorkflowDefinition, binding domaincatalog.ApplicationEnvironment, app domainapp.App, input domainworkflow.Input) error {
	nodeIDs := make(map[string]struct{}, len(definition.Nodes))
	availableRefs := initialDAGInputReferences(app, input, binding)
	producersByRef := map[string][]string{}
	for _, node := range definition.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("%w: workflow node id is required", apperrors.ErrInvalidArgument)
		}
		if _, exists := nodeIDs[node.ID]; exists {
			return fmt.Errorf("%w: duplicate workflow node id %s", apperrors.ErrInvalidArgument, node.ID)
		}
		nodeIDs[node.ID] = struct{}{}
		for _, output := range node.Outputs {
			output = strings.TrimSpace(output)
			if output == "" {
				continue
			}
			dagRegisterProducedRef(producersByRef, output, node.ID)
			dagRegisterProducedRef(producersByRef, node.ID+"."+output, node.ID)
		}
		for _, artifact := range node.ArtifactOutputs {
			name := strings.TrimSpace(fmt.Sprint(artifact["name"]))
			kind := strings.TrimSpace(fmt.Sprint(artifact["kind"]))
			if name != "" {
				dagRegisterProducedRef(producersByRef, name, node.ID)
				dagRegisterProducedRef(producersByRef, node.ID+"."+name, node.ID)
			}
			if kind != "" {
				dagRegisterProducedRef(producersByRef, kind, node.ID)
				dagRegisterProducedRef(producersByRef, node.ID+"."+kind, node.ID)
			}
		}
	}
	for _, edge := range definition.Edges {
		if _, ok := nodeIDs[edge.Source]; !ok {
			return fmt.Errorf("%w: edge source %s not found", apperrors.ErrInvalidArgument, edge.Source)
		}
		if _, ok := nodeIDs[edge.Target]; !ok {
			return fmt.Errorf("%w: edge target %s not found", apperrors.ErrInvalidArgument, edge.Target)
		}
	}
	for _, node := range definition.Nodes {
		for _, inputRef := range node.Inputs {
			if dagInputReferenceDeclared(inputRef, availableRefs) {
				continue
			}
			producers := dagProducedRefProducers(inputRef, producersByRef)
			if len(producers) == 0 {
				return fmt.Errorf("%w: delivery_dag node %s input reference %s not found", apperrors.ErrInvalidArgument, node.ID, inputRef)
			}
			if !dagProducedRefHasUpstreamProducer(definition, node.ID, producers) {
				return fmt.Errorf("%w: delivery_dag node %s input reference %s must come from an upstream node", apperrors.ErrInvalidArgument, node.ID, inputRef)
			}
		}
		for _, artifact := range node.ArtifactOutputs {
			name := strings.TrimSpace(fmt.Sprint(artifact["name"]))
			if name == "" {
				return fmt.Errorf("%w: delivery_dag node %s artifact output requires name", apperrors.ErrInvalidArgument, node.ID)
			}
			kind := strings.TrimSpace(fmt.Sprint(artifact["kind"]))
			if !isAllowedDeliveryArtifactKind(kind) {
				return fmt.Errorf("%w: unsupported delivery_dag artifact output kind %s", apperrors.ErrInvalidArgument, kind)
			}
		}
		if strategy := normalizeDAGFanOutStrategy(node.FanOutStrategy); strings.TrimSpace(node.FanOutStrategy) != "" && strategy == "" {
			return fmt.Errorf("%w: delivery_dag node %s fanOut strategy %s is not supported", apperrors.ErrInvalidArgument, node.ID, node.FanOutStrategy)
		}
		if _, err := resolveDAGNodeSelectors(app, input, binding, node); err != nil {
			return err
		}
	}
	return nil
}

func dagRegisterProducedRef(producersByRef map[string][]string, ref, producerNodeID string) {
	ref = strings.TrimSpace(ref)
	if ref == "" || producerNodeID == "" {
		return
	}
	producersByRef[ref] = append(producersByRef[ref], producerNodeID)
}

func dagProducedRefProducers(inputRef string, producersByRef map[string][]string) []string {
	ref := strings.TrimSpace(inputRef)
	if ref == "" {
		return nil
	}
	if producers := producersByRef[ref]; len(producers) > 0 {
		return producers
	}
	return nil
}

func dagProducedRefHasUpstreamProducer(definition dagWorkflowDefinition, nodeID string, producers []string) bool {
	for _, producerNodeID := range producers {
		if dagNodeHasUpstreamPath(definition, nodeID, producerNodeID, map[string]bool{}) {
			return true
		}
	}
	return false
}

func dagNodeHasUpstreamPath(definition dagWorkflowDefinition, nodeID, upstreamNodeID string, seen map[string]bool) bool {
	if nodeID == upstreamNodeID {
		return false
	}
	if seen[nodeID] {
		return false
	}
	seen[nodeID] = true
	for _, edge := range incomingEdgesForNode(definition, nodeID) {
		if edge.Source == upstreamNodeID {
			return true
		}
		if dagNodeHasUpstreamPath(definition, edge.Source, upstreamNodeID, seen) {
			return true
		}
	}
	return false
}

func initialDAGInputReferences(app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment) map[string]struct{} {
	refs := map[string]struct{}{
		"source":                   {},
		"application":              {},
		"app":                      {},
		"applicationId":            {},
		"application.id":           {},
		"branch":                   {},
		"ref":                      {},
		"refName":                  {},
		"commit":                   {},
		"image":                    {},
		"imageTag":                 {},
		"environment":              {},
		"environmentId":            {},
		"environmentKey":           {},
		"target":                   {},
		"cluster":                  {},
		"clusterId":                {},
		"namespace":                {},
		"deployment":               {},
		"deploymentName":           {},
		"applicationEnvironment":   {},
		"applicationEnvironmentId": {},
	}
	if app.ID != "" {
		refs[app.ID] = struct{}{}
	}
	if app.Key != "" {
		refs[app.Key] = struct{}{}
	}
	if input.BuildSourceID != "" {
		refs["buildSource"] = struct{}{}
		refs["buildSourceId"] = struct{}{}
	}
	if binding.ID != "" {
		refs[binding.ID] = struct{}{}
	}
	return refs
}

func dagInputReferenceDeclared(inputRef string, declared map[string]struct{}) bool {
	ref := strings.TrimSpace(inputRef)
	if ref == "" {
		return false
	}
	_, ok := declared[ref]
	return ok
}

func dagInputReferenceNodeID(inputRef string) (string, bool) {
	ref := strings.TrimSpace(inputRef)
	if ref == "" {
		return "", false
	}
	parts := strings.Split(ref, ".")
	if len(parts) < 2 {
		return "", false
	}
	nodeID := strings.TrimSpace(parts[0])
	if nodeID == "" {
		return "", false
	}
	return nodeID, true
}

func isAllowedDeliveryArtifactKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "image", "test_report", "scan_report", "sbom":
		return true
	default:
		return false
	}
}

func evaluateDAGRunCondition(condition string, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, artifactState map[string]any) (bool, string) {
	expr := strings.TrimSpace(condition)
	if expr == "" || strings.EqualFold(expr, "always") || strings.EqualFold(expr, "true") {
		return true, ""
	}
	if strings.EqualFold(expr, "never") || strings.EqualFold(expr, "false") {
		return false, fmt.Sprintf("runCondition %q is false", condition)
	}
	for _, operator := range []string{"==", "!="} {
		if !strings.Contains(expr, operator) {
			continue
		}
		parts := strings.SplitN(expr, operator, 2)
		left := strings.TrimSpace(parts[0])
		right := trimDAGConditionLiteral(parts[1])
		actual := dagConditionValue(left, app, input, binding, artifactState)
		matched := actual == right
		if operator == "!=" {
			matched = actual != right
		}
		if matched {
			return true, ""
		}
		return false, fmt.Sprintf("runCondition %q not met: %s is %q", condition, left, actual)
	}
	actual := dagConditionValue(expr, app, input, binding, artifactState)
	if actual == "" || strings.EqualFold(actual, "false") || actual == "0" {
		return false, fmt.Sprintf("runCondition %q not met", condition)
	}
	return true, ""
}

func trimDAGConditionLiteral(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return value
}

func dagConditionValue(key string, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, artifactState map[string]any) string {
	key = strings.TrimSpace(key)
	switch key {
	case "branch", "ref", "refName":
		return firstNonEmpty(input.RefName, binding.BuildPolicy.RefValue, app.DefaultBranch, "main")
	case "refType":
		return firstNonEmpty(input.RefType, binding.BuildPolicy.RefType)
	case "applicationId", "app.id", "application.id":
		return app.ID
	case "application", "app", "app.key", "application.key":
		return firstNonEmpty(app.Key, app.Name, app.ID)
	case "environment", "environmentKey":
		return binding.EnvironmentKey
	case "environmentId":
		return binding.EnvironmentID
	case "image", "artifact.image":
		return dagArtifactRuntimeString(artifactState["image"])
	case "namespace":
		return input.Namespace
	case "cluster", "clusterId":
		return input.ClusterID
	case "deployment", "deploymentName":
		return input.DeploymentName
	default:
		if strings.HasPrefix(key, "variables.") {
			return workflowMetadataString(input.Variables, strings.TrimPrefix(key, "variables."))
		}
		if value, ok := artifactState[key]; ok {
			return dagArtifactRuntimeString(value)
		}
		return workflowMetadataString(input.Variables, key)
	}
}

func resolveDAGNodeInputs(node dagWorkflowNode, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, artifactState map[string]any) map[string]any {
	if len(node.Inputs) == 0 {
		return nil
	}
	resolved := make(map[string]any, len(node.Inputs))
	for _, ref := range node.Inputs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if value, ok := resolveDAGInputReference(ref, app, input, binding, artifactState); ok {
			resolved[ref] = value
		}
	}
	return resolved
}

func resolveDAGInputReference(ref string, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, artifactState map[string]any) (any, bool) {
	if value, ok := artifactState[ref]; ok {
		return value, true
	}
	parts := strings.Split(ref, ".")
	for _, part := range parts {
		if value, ok := artifactState[strings.TrimSpace(part)]; ok {
			return value, true
		}
	}
	switch ref {
	case "source":
		return map[string]any{
			"refType":       firstNonEmpty(input.RefType, binding.BuildPolicy.RefType, "branch"),
			"refName":       firstNonEmpty(input.RefName, binding.BuildPolicy.RefValue, app.DefaultBranch, "main"),
			"buildSourceId": firstNonEmpty(input.BuildSourceID, binding.BuildPolicy.SourceID),
		}, true
	case "application", "app":
		return map[string]any{"id": app.ID, "key": app.Key, "name": app.Name}, true
	case "applicationId", "application.id", "app.id":
		return app.ID, true
	case "branch", "ref", "refName":
		return firstNonEmpty(input.RefName, binding.BuildPolicy.RefValue, app.DefaultBranch, "main"), true
	case "image", "imageTag":
		return input.ImageTag, true
	case "environment", "applicationEnvironment":
		return map[string]any{"id": binding.ID, "environmentId": binding.EnvironmentID, "environmentKey": binding.EnvironmentKey}, true
	case "environmentId":
		return binding.EnvironmentID, true
	case "environmentKey":
		return binding.EnvironmentKey, true
	case "target":
		return map[string]any{"clusterId": input.ClusterID, "namespace": input.Namespace, "workloadName": input.DeploymentName}, true
	case "cluster", "clusterId":
		return input.ClusterID, true
	case "namespace":
		return input.Namespace, true
	case "deployment", "deploymentName":
		return input.DeploymentName, true
	default:
		if strings.HasPrefix(ref, "variables.") {
			key := strings.TrimPrefix(ref, "variables.")
			if value, ok := input.Variables[key]; ok {
				return value, true
			}
		}
		if value, ok := input.Variables[ref]; ok {
			return value, true
		}
		return nil, false
	}
}

func resolveDAGNodeSelectors(app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode) (map[string]any, error) {
	return resolveDAGNodeSelectorsWithServices(app, input, binding, node, nil)
}

func (s *Service) resolveDAGNodeSelectors(ctx context.Context, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode) (map[string]any, error) {
	return resolveDAGNodeSelectorsWithServices(app, input, binding, node, s.applicationServicesForSelectors(ctx, app.ID))
}

func resolveDAGNodeSelectorsWithServices(app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode, services []domainapp.Service) (map[string]any, error) {
	resolved := map[string]any{}
	if len(node.ServiceSelector) > 0 {
		service, err := resolveDAGServiceSelector(app, binding, services, node.ServiceSelector)
		if err != nil {
			return nil, fmt.Errorf("%w: delivery_dag node %s serviceSelector cannot be resolved: %v", apperrors.ErrInvalidArgument, node.ID, err)
		}
		resolved["service"] = service
	}
	if len(node.EnvironmentSelector) > 0 {
		environment, err := resolveDAGEnvironmentSelector(binding, node.EnvironmentSelector)
		if err != nil {
			return nil, fmt.Errorf("%w: delivery_dag node %s environmentSelector cannot be resolved: %v", apperrors.ErrInvalidArgument, node.ID, err)
		}
		resolved["environment"] = environment
	}
	targets, err := resolveDAGTargets(binding, input, node.TargetSelector, normalizeDAGFanOutStrategy(node.FanOutStrategy) != "")
	if err != nil {
		return nil, fmt.Errorf("%w: delivery_dag node %s targetSelector cannot be resolved: %v", apperrors.ErrInvalidArgument, node.ID, err)
	}
	if len(targets) > 1 {
		strategy := normalizeDAGFanOutStrategy(node.FanOutStrategy)
		if strategy == "" {
			return nil, fmt.Errorf("%w: targetSelector matched %d targets but fanOut strategy is not declared", apperrors.ErrInvalidArgument, len(targets))
		}
		targetMetadata := make([]map[string]any, 0, len(targets))
		for _, target := range targets {
			targetMetadata = append(targetMetadata, releaseTargetMetadata(target))
		}
		resolved["target"] = targetMetadata[0]
		resolved["targets"] = targetMetadata
		resolved["fanOut"] = map[string]any{
			"strategy":      strategy,
			"batchSize":     node.FanOutBatchSize,
			"failurePolicy": firstNonEmpty(normalizeDAGFanOutFailurePolicy(node.FanOutFailurePolicy), effectiveDAGFailurePolicy(dagWorkflowDefinition{}, node)),
			"targetCount":   len(targetMetadata),
		}
	} else if len(targets) == 1 {
		resolved["target"] = releaseTargetMetadata(targets[0])
	}
	if len(resolved) == 0 {
		return nil, nil
	}
	return resolved, nil
}

func resolveDAGServiceSelector(app domainapp.App, binding domaincatalog.ApplicationEnvironment, services []domainapp.Service, selector map[string]any) (map[string]any, error) {
	labels := dagAppLabels(app, binding)
	if match, ok := selector["matchLabels"]; ok {
		matchLabels := stringMapFromAny(match)
		if len(matchLabels) == 0 {
			return nil, fmt.Errorf("matchLabels is empty")
		}
		if serviceMatches := matchDAGApplicationServices(services, matchLabels, "matchLabels"); len(serviceMatches) > 0 {
			return serviceDiscoveryMetadata(app, serviceMatches, "application.services"), nil
		}
		if serviceKey := firstNonEmpty(matchLabels["service"], matchLabels["serviceKey"], matchLabels["app"], matchLabels["application"]); serviceKey != "" {
			return map[string]any{"serviceKey": serviceKey, "applicationId": app.ID, "source": "matchLabels"}, nil
		}
		if !labelsMatch(labels, matchLabels) {
			return nil, fmt.Errorf("matchLabels did not match application labels")
		}
	}
	key := selectorString(selector, "service", "serviceKey", "key", "name")
	if key != "" {
		if serviceMatches := matchDAGApplicationServicesByKey(services, key); len(serviceMatches) > 0 {
			return serviceDiscoveryMetadata(app, serviceMatches, "application.services"), nil
		}
		if key != app.ID && key != app.Key && key != app.Name && key != labels["service"] {
			return map[string]any{"serviceKey": key, "applicationId": app.ID, "source": "selector"}, nil
		}
	}
	if id := selectorString(selector, "id", "applicationId"); id != "" && id != app.ID {
		return nil, fmt.Errorf("application id %s does not match %s", id, app.ID)
	}
	return map[string]any{
		"applicationId": app.ID,
		"serviceKey":    firstNonEmpty(key, app.Key, app.Name, app.ID),
		"serviceName":   firstNonEmpty(app.Name, app.Key, app.ID),
	}, nil
}

func resolveDAGEnvironmentSelector(binding domaincatalog.ApplicationEnvironment, selector map[string]any) (map[string]any, error) {
	if id := selectorString(selector, "id", "applicationEnvironmentId", "bindingId"); id != "" && id != binding.ID {
		return nil, fmt.Errorf("binding id %s does not match %s", id, binding.ID)
	}
	if environmentID := selectorString(selector, "environmentId"); environmentID != "" && environmentID != binding.EnvironmentID {
		return nil, fmt.Errorf("environment id %s does not match %s", environmentID, binding.EnvironmentID)
	}
	if key := selectorString(selector, "key", "environmentKey", "environment"); key != "" && key != binding.EnvironmentKey && key != binding.EnvironmentID {
		return nil, fmt.Errorf("environment key %s does not match %s", key, firstNonEmpty(binding.EnvironmentKey, binding.EnvironmentID))
	}
	if match, ok := selector["matchLabels"]; ok {
		matchLabels := stringMapFromAny(match)
		if len(matchLabels) == 0 {
			return nil, fmt.Errorf("matchLabels is empty")
		}
		if !labelsMatch(dagBindingLabels(binding), matchLabels) {
			return nil, fmt.Errorf("matchLabels did not match environment labels")
		}
	}
	return map[string]any{
		"bindingId":      binding.ID,
		"environmentId":  binding.EnvironmentID,
		"environmentKey": binding.EnvironmentKey,
		"applicationId":  binding.ApplicationID,
	}, nil
}

func selectDAGTarget(binding domaincatalog.ApplicationEnvironment, input domainworkflow.Input, selector map[string]any) (*domaincatalog.ReleaseTarget, error) {
	targets, err := resolveDAGTargets(binding, input, selector, false)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}
	copyTarget := targets[0]
	return &copyTarget, nil
}

func resolveDAGTargets(binding domaincatalog.ApplicationEnvironment, input domainworkflow.Input, selector map[string]any, fanOut bool) ([]domaincatalog.ReleaseTarget, error) {
	if len(selector) == 0 {
		if fanOut {
			targets := make([]domaincatalog.ReleaseTarget, 0, len(binding.Targets))
			for _, target := range binding.Targets {
				if target.Enabled {
					targets = append(targets, target)
				}
			}
			if len(targets) > 0 {
				return targets, nil
			}
		}
		if target := matchBindingTarget(binding, input); target != nil {
			return []domaincatalog.ReleaseTarget{*target}, nil
		}
		return nil, nil
	}
	matched := make([]domaincatalog.ReleaseTarget, 0)
	for _, target := range binding.Targets {
		if !target.Enabled {
			continue
		}
		if dagTargetMatchesSelector(target, binding, selector) {
			matched = append(matched, target)
		}
	}
	if len(matched) > 0 {
		return matched, nil
	}
	if input.ClusterID != "" || input.Namespace != "" || input.DeploymentName != "" {
		inputTarget := domaincatalog.ReleaseTarget{
			ID:            "input",
			ClusterID:     input.ClusterID,
			Namespace:     input.Namespace,
			WorkloadName:  input.DeploymentName,
			ContainerName: input.ContainerName,
			Enabled:       true,
		}
		if dagTargetMatchesSelector(inputTarget, binding, selector) {
			return []domaincatalog.ReleaseTarget{inputTarget}, nil
		}
	}
	return nil, fmt.Errorf("no enabled target matched selector")
}

func dagTargetMatchesSelector(target domaincatalog.ReleaseTarget, binding domaincatalog.ApplicationEnvironment, selector map[string]any) bool {
	for _, key := range []string{"id", "targetId"} {
		if value := selectorString(selector, key); value != "" && value != target.ID {
			return false
		}
	}
	if key := selectorString(selector, "key"); key != "" {
		if key != target.ID && key != target.GroupKey && key != target.WaveKey && key != target.RegionKey && key != binding.EnvironmentKey && key != strings.TrimSpace(fmt.Sprint(target.Metadata["key"])) {
			return false
		}
	}
	for selectorKey, targetValue := range map[string]string{
		"clusterId":    target.ClusterID,
		"namespace":    target.Namespace,
		"workloadName": target.WorkloadName,
		"deployment":   target.WorkloadName,
		"groupKey":     target.GroupKey,
		"waveKey":      target.WaveKey,
		"regionKey":    target.RegionKey,
	} {
		if value := selectorString(selector, selectorKey); value != "" && value != targetValue {
			return false
		}
	}
	if match, ok := selector["matchLabels"]; ok {
		matchLabels := stringMapFromAny(match)
		if len(matchLabels) == 0 || !labelsMatch(dagTargetLabels(target, binding), matchLabels) {
			return false
		}
	}
	return true
}

func releaseTargetMetadata(target domaincatalog.ReleaseTarget) map[string]any {
	return map[string]any{
		"id":             target.ID,
		"clusterId":      target.ClusterID,
		"namespace":      target.Namespace,
		"targetKind":     target.TargetKind,
		"executorKind":   target.ExecutorKind,
		"groupKey":       target.GroupKey,
		"waveKey":        target.WaveKey,
		"regionKey":      target.RegionKey,
		"workloadKind":   target.WorkloadKind,
		"workloadName":   target.WorkloadName,
		"containerName":  target.ContainerName,
		"configRef":      target.ConfigRef,
		"metadata":       target.Metadata,
		"resolvedSource": "applicationEnvironment.targets",
	}
}

func (s *Service) applicationServicesForSelectors(ctx context.Context, applicationID string) []domainapp.Service {
	if s == nil || s.apps == nil || strings.TrimSpace(applicationID) == "" {
		return nil
	}
	items, err := s.apps.ListServices(ctx, strings.TrimSpace(applicationID))
	if err != nil {
		return nil
	}
	return items
}

func matchDAGApplicationServicesByKey(services []domainapp.Service, key string) []domainapp.Service {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	matches := make([]domainapp.Service, 0)
	for _, service := range services {
		if !service.Enabled {
			continue
		}
		if key == service.ID || key == service.Key || key == service.Name || key == strings.TrimSpace(fmt.Sprint(service.Metadata["service"])) || key == strings.TrimSpace(fmt.Sprint(service.Metadata["serviceKey"])) {
			matches = append(matches, service)
		}
	}
	return matches
}

func matchDAGApplicationServices(services []domainapp.Service, labels map[string]string, source string) []domainapp.Service {
	if len(services) == 0 || len(labels) == 0 {
		return nil
	}
	matches := make([]domainapp.Service, 0)
	for _, service := range services {
		if !service.Enabled {
			continue
		}
		if labelsMatch(dagServiceLabels(service), labels) {
			matches = append(matches, service)
		}
	}
	_ = source
	return matches
}

func serviceDiscoveryMetadata(app domainapp.App, services []domainapp.Service, source string) map[string]any {
	items := make([]map[string]any, 0, len(services))
	for _, service := range services {
		containers := make([]map[string]any, 0, len(service.Containers))
		for _, container := range service.Containers {
			containers = append(containers, map[string]any{
				"id":              container.ID,
				"name":            container.Name,
				"imageRepository": container.ImageRepository,
				"runtimePorts":    container.RuntimePorts,
			})
		}
		items = append(items, map[string]any{
			"id":            service.ID,
			"key":           service.Key,
			"name":          service.Name,
			"serviceKind":   service.ServiceKind,
			"buildSourceId": service.BuildSourceID,
			"metadata":      service.Metadata,
			"containers":    containers,
		})
	}
	result := map[string]any{
		"applicationId": app.ID,
		"source":        source,
		"services":      items,
		"serviceCount":  len(items),
	}
	if len(items) > 0 {
		result["serviceKey"] = items[0]["key"]
		result["serviceName"] = items[0]["name"]
	}
	return result
}

func dagServiceLabels(service domainapp.Service) map[string]string {
	labels := map[string]string{
		"serviceId":   service.ID,
		"serviceKey":  service.Key,
		"service":     service.Key,
		"key":         service.Key,
		"name":        service.Name,
		"serviceKind": string(service.ServiceKind),
	}
	for key, value := range service.Metadata {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			labels[key] = text
		}
	}
	return labels
}

func dagAppLabels(app domainapp.App, binding domaincatalog.ApplicationEnvironment) map[string]string {
	labels := map[string]string{
		"applicationId":   app.ID,
		"applicationKey":  app.Key,
		"app":             app.Key,
		"service":         app.Key,
		"name":            app.Name,
		"group":           app.Group,
		"businessLineId":  app.BusinessLineID,
		"environmentKey":  binding.EnvironmentKey,
		"environmentId":   binding.EnvironmentID,
		"applicationName": app.Name,
	}
	for key, value := range app.Metadata {
		labels[key] = strings.TrimSpace(fmt.Sprint(value))
	}
	return labels
}

func dagBindingLabels(binding domaincatalog.ApplicationEnvironment) map[string]string {
	labels := map[string]string{
		"bindingId":        binding.ID,
		"applicationId":    binding.ApplicationID,
		"environmentId":    binding.EnvironmentID,
		"environmentKey":   binding.EnvironmentKey,
		"businessLineId":   binding.BusinessLineID,
		"applicationGroup": binding.ApplicationGroup,
	}
	for key, value := range binding.ResourceSelector.MatchLabels {
		labels[key] = value
	}
	return labels
}

func dagTargetLabels(target domaincatalog.ReleaseTarget, binding domaincatalog.ApplicationEnvironment) map[string]string {
	labels := dagBindingLabels(binding)
	labels["targetId"] = target.ID
	labels["clusterId"] = target.ClusterID
	labels["namespace"] = target.Namespace
	labels["workloadKind"] = target.WorkloadKind
	labels["workloadName"] = target.WorkloadName
	labels["deployment"] = target.WorkloadName
	labels["groupKey"] = target.GroupKey
	labels["waveKey"] = target.WaveKey
	labels["regionKey"] = target.RegionKey
	for key, value := range target.Metadata {
		labels[key] = strings.TrimSpace(fmt.Sprint(value))
	}
	return labels
}

func labelsMatch(labels, matchLabels map[string]string) bool {
	for key, expected := range matchLabels {
		if strings.TrimSpace(expected) == "" {
			return false
		}
		if labels[key] != expected {
			return false
		}
	}
	return true
}

func selectorString(selector map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := mapString(selector, key); value != "" {
			return value
		}
	}
	return ""
}

func stringMapFromAny(value any) map[string]string {
	out := map[string]string{}
	switch current := value.(type) {
	case map[string]string:
		for key, item := range current {
			if text := strings.TrimSpace(item); text != "" {
				out[key] = text
			}
		}
	case map[string]any:
		for key, item := range current {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out[key] = text
			}
		}
	}
	return out
}

func ensureDAGExecutionMetadata(run *domainworkflow.Run) {
	if run.Metadata == nil {
		run.Metadata = map[string]any{}
	}
	if _, ok := run.Metadata["events"]; !ok {
		run.Metadata["events"] = []map[string]any{}
	}
	if _, ok := run.Metadata["nodeOutputs"]; !ok {
		run.Metadata["nodeOutputs"] = map[string]any{}
	}
}

func appendDAGRunEvent(run *domainworkflow.Run, event map[string]any) {
	if event == nil {
		return
	}
	ensureDAGExecutionMetadata(run)
	if _, ok := event["occurredAt"]; !ok {
		event["occurredAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	items := metadataMapSlice(run.Metadata["events"])
	items = append(items, event)
	run.Metadata["events"] = items
}

func appendDAGNodeEvent(run *domainworkflow.Run, nodeID, eventType, status, summary string, details map[string]any) {
	if strings.TrimSpace(eventType) == "" {
		eventType = "node_event"
	}
	event := map[string]any{
		"type":   eventType,
		"nodeId": nodeID,
		"status": status,
	}
	if summary != "" {
		event["summary"] = summary
	}
	for key, value := range details {
		if key == "type" || key == "nodeId" || key == "occurredAt" {
			continue
		}
		event[key] = value
	}
	appendDAGRunEvent(run, event)
}

func recordDAGNodeOutputs(run *domainworkflow.Run, nodeID string, values map[string]any) {
	if len(values) == 0 {
		return
	}
	ensureDAGExecutionMetadata(run)
	nodeOutputs := metadataMap(run.Metadata["nodeOutputs"])
	existing := metadataMap(nodeOutputs[nodeID])
	for key, value := range values {
		if value == nil {
			continue
		}
		if mapped, ok := value.(map[string]any); ok && len(mapped) == 0 {
			continue
		}
		existing[key] = value
	}
	nodeOutputs[nodeID] = existing
	run.Metadata["nodeOutputs"] = nodeOutputs
}

func recordDAGFailurePolicy(run *domainworkflow.Run, nodeID, policy, summary string) {
	ensureDAGExecutionMetadata(run)
	policies := metadataMap(run.Metadata["failurePolicies"])
	effect := "workflow_failed"
	switch policy {
	case "continue":
		effect = "workflow_continues"
	case "rollback":
		effect = "failure_branch_can_rollback"
	case "notify":
		effect = "failure_branch_can_notify"
	case "stop":
		effect = "stop_successors"
	}
	policies[nodeID] = map[string]any{
		"policy":  policy,
		"effect":  effect,
		"summary": summary,
	}
	run.Metadata["failurePolicies"] = policies
	appendDAGNodeEvent(run, nodeID, "failure_policy_applied", "failed", summary, map[string]any{"failurePolicy": policy, "effect": effect})
}

func metadataMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func metadataMapSlice(value any) []map[string]any {
	if typed, ok := value.([]map[string]any); ok {
		return typed
	}
	if items, ok := value.([]any); ok {
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	}
	return []map[string]any{}
}

func nodeByID(definition dagWorkflowDefinition, nodeID string) dagWorkflowNode {
	for _, node := range definition.Nodes {
		if node.ID == nodeID {
			return node
		}
	}
	return dagWorkflowNode{ID: nodeID}
}

func effectiveDAGFailurePolicy(definition dagWorkflowDefinition, node dagWorkflowNode) string {
	if node.ContinueOnFailure {
		return "continue"
	}
	policy := strings.ToLower(strings.TrimSpace(node.FailurePolicy))
	switch policy {
	case "continue", "rollback", "notify", "stop":
		return policy
	default:
		return "stop"
	}
}

func shouldStopDAGNodeAfterFailure(definition dagWorkflowDefinition, nodeID string, stoppedSources map[string]struct{}) bool {
	if len(stoppedSources) == 0 {
		return false
	}
	if _, failed := stoppedSources[nodeID]; failed {
		return false
	}
	return !hasFailureBranchPath(definition, nodeID, stoppedSources, map[string]bool{})
}

func hasFailureBranchPath(definition dagWorkflowDefinition, nodeID string, stoppedSources map[string]struct{}, seen map[string]bool) bool {
	if seen[nodeID] {
		return false
	}
	seen[nodeID] = true
	for _, edge := range definition.Edges {
		if edge.Target != nodeID || !dagEdgeAllowsFailureBranch(edge.Condition) {
			continue
		}
		if _, ok := stoppedSources[edge.Source]; ok {
			return true
		}
		if hasFailureBranchPath(definition, edge.Source, stoppedSources, seen) {
			return true
		}
	}
	return false
}

func dagEdgeAllowsFailureBranch(condition string) bool {
	switch strings.TrimSpace(condition) {
	case "failure", "always":
		return true
	default:
		return false
	}
}

func dagFailureCountsAsWorkflowFailure(definition dagWorkflowDefinition, node dagWorkflowNode) bool {
	return effectiveDAGFailurePolicy(definition, node) != "continue"
}

func isValidationDAGNode(nodeType string) bool {
	switch strings.TrimSpace(nodeType) {
	case "check_http", "check_k8s_event", "smoke_test", "verify", "check":
		return true
	default:
		return false
	}
}

func isRollbackDAGNode(nodeType string) bool {
	switch strings.TrimSpace(nodeType) {
	case "rollback_to_previous":
		return true
	default:
		return false
	}
}

func (s *Service) executeDAGWorkflow(
	ctx context.Context,
	principal domainidentity.Principal,
	app domainapp.App,
	input domainworkflow.Input,
	binding domaincatalog.ApplicationEnvironment,
	definition dagWorkflowDefinition,
) ([]domainworkflow.Step, map[string]any, string, error) {
	nodeMap := make(map[string]dagWorkflowNode, len(definition.Nodes))
	incoming := make(map[string][]dagWorkflowEdge, len(definition.Nodes))
	for _, node := range definition.Nodes {
		if node.Name == "" {
			node.Name = node.Type
		}
		if node.TimeoutSeconds <= 0 {
			node.TimeoutSeconds = 300
		}
		nodeMap[node.ID] = node
	}
	for _, edge := range definition.Edges {
		incoming[edge.Target] = append(incoming[edge.Target], edge)
	}

	statuses := make(map[string]string, len(definition.Nodes))
	steps := make([]domainworkflow.Step, 0, len(definition.Nodes))
	artifactState := make(map[string]any)
	stopFailureSources := make(map[string]struct{})
	metadata := map[string]any{
		"mode":                 definition.Mode,
		"executionMode":        "release_dag",
		"schemaVersion":        definition.SchemaVersion,
		"workflowTemplateId":   binding.WorkflowTemplateID,
		"workflowTemplateKey":  binding.WorkflowTemplate.Key,
		"workflowTemplateName": binding.WorkflowTemplate.Name,
		"bindingId":            binding.ID,
		"nodes":                definition.Nodes,
		"edges":                definition.Edges,
	}
	metadata = withGatewayApprovalWorkflowMetadata(metadata, input)
	runForMetadata := domainworkflow.Run{Metadata: metadata}
	ensureDAGExecutionMetadata(&runForMetadata)
	appendDAGRunEvent(&runForMetadata, map[string]any{
		"type":   "workflow_started",
		"status": "running",
		"mode":   definition.Mode,
	})
	metadata = runForMetadata.Metadata

	for len(statuses) < len(definition.Nodes) {
		ready := make([]dagWorkflowNode, 0)
		progressed := false
		for _, node := range definition.Nodes {
			if statuses[node.ID] != "" {
				continue
			}
			readyState, skipped := resolveDAGNodeReadiness(definition, node, incoming[node.ID], statuses)
			switch {
			case skipped:
				statuses[node.ID] = "skipped"
				steps = append(steps, domainworkflow.Step{Name: node.Name, Status: "skipped", Summary: "conditions not met"})
				runForMetadata.Metadata = metadata
				appendDAGNodeEvent(&runForMetadata, node.ID, "node_skipped", "skipped", "conditions not met", map[string]any{"reason": "dependency_condition"})
				metadata = runForMetadata.Metadata
				progressed = true
			case readyState:
				if shouldStopDAGNodeAfterFailure(definition, node.ID, stopFailureSources) {
					statuses[node.ID] = "skipped"
					steps = append(steps, domainworkflow.Step{Name: node.Name, Status: "skipped", Summary: "stopped after failure policy"})
					runForMetadata.Metadata = metadata
					appendDAGNodeEvent(&runForMetadata, node.ID, "node_skipped", "skipped", "stopped after failure policy", map[string]any{"reason": "failure_policy_stop"})
					metadata = runForMetadata.Metadata
					progressed = true
					continue
				}
				runConditionMatched, runConditionReason := evaluateDAGRunCondition(node.RunCondition, app, input, binding, artifactState)
				if !runConditionMatched {
					statuses[node.ID] = "skipped"
					steps = append(steps, domainworkflow.Step{Name: node.Name, Status: "skipped", Summary: runConditionReason})
					runForMetadata.Metadata = metadata
					appendDAGNodeEvent(&runForMetadata, node.ID, "node_skipped", "skipped", runConditionReason, map[string]any{"reason": "run_condition", "runCondition": node.RunCondition})
					recordDAGNodeOutputs(&runForMetadata, node.ID, map[string]any{
						"runCondition": map[string]any{
							"expression": node.RunCondition,
							"matched":    false,
							"reason":     runConditionReason,
						},
					})
					metadata = runForMetadata.Metadata
					progressed = true
					continue
				}
				ready = append(ready, node)
			}
		}
		if len(ready) == 0 {
			if !progressed {
				break
			}
			continue
		}

		run := domainworkflow.Run{
			ID:             "workflow:" + uuid.NewString(),
			ApplicationID:  input.ApplicationID,
			WorkflowName:   strings.TrimSpace(input.WorkflowName),
			ClusterID:      strings.TrimSpace(input.ClusterID),
			Namespace:      strings.TrimSpace(input.Namespace),
			DeploymentName: strings.TrimSpace(input.DeploymentName),
		}
		for _, result := range s.executeReadyDAGNodes(ctx, principal, app, input, binding, ready, run, artifactState) {
			statuses[result.nodeID] = result.status
			steps = append(steps, result.step)
			runForMetadata.Metadata = metadata
			recordDAGNodeOutputs(&runForMetadata, result.nodeID, metadataDAGExecutionResult(result))
			for _, event := range result.events {
				appendDAGNodeEvent(&runForMetadata, result.nodeID, mapString(event, "type"), result.status, result.summary, event)
			}
			for key, value := range result.outputs {
				artifactState[key] = value
			}
			for key, value := range result.artifacts {
				artifactState[key] = value
			}
			if len(result.outputs) > 0 || len(result.artifacts) > 0 {
				runForMetadata.Metadata["artifacts"] = metadataDAGArtifactState(artifactState)
			}
			if result.status == "failed" {
				node := nodeByID(definition, result.nodeID)
				policy := effectiveDAGFailurePolicy(definition, node)
				if policy == "stop" {
					stopFailureSources[result.nodeID] = struct{}{}
				}
				recordDAGFailurePolicy(&runForMetadata, result.nodeID, policy, result.summary)
			}
			metadata = runForMetadata.Metadata
		}
	}

	finalStatus := "completed"
	for _, node := range definition.Nodes {
		status := statuses[node.ID]
		if status == "failed" && dagFailureCountsAsWorkflowFailure(definition, node) {
			finalStatus = "failed"
			break
		}
		if status == "" {
			steps = append(steps, domainworkflow.Step{Name: node.Name, Status: "skipped", Summary: "unresolved DAG dependency"})
		}
	}
	metadata["nodeStatus"] = statuses
	if len(artifactState) > 0 {
		metadata["artifacts"] = metadataDAGArtifactState(artifactState)
	}
	return steps, metadata, finalStatus, nil
}

func (s *Service) executeReadyDAGNodes(
	ctx context.Context,
	principal domainidentity.Principal,
	app domainapp.App,
	input domainworkflow.Input,
	binding domaincatalog.ApplicationEnvironment,
	ready []dagWorkflowNode,
	run domainworkflow.Run,
	artifactState map[string]any,
) []dagExecutionResult {
	if len(ready) == 0 {
		return nil
	}

	workerCount := s.nodeParallelism
	if workerCount <= 0 {
		workerCount = defaultDAGNodeConcurrency
	}
	if workerCount > len(ready) {
		workerCount = len(ready)
	}

	jobs := make(chan dagWorkflowNode)
	results := make(chan dagExecutionResult, len(ready))
	var wait sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for node := range jobs {
				results <- s.executeDAGNode(ctx, principal, app, input, binding, node, run, artifactState)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, node := range ready {
			select {
			case <-ctx.Done():
				return
			case jobs <- node:
			}
		}
	}()

	wait.Wait()
	close(results)

	byNodeID := make(map[string]dagExecutionResult, len(ready))
	for result := range results {
		byNodeID[result.nodeID] = result
	}

	ordered := make([]dagExecutionResult, 0, len(byNodeID))
	for _, node := range ready {
		if result, ok := byNodeID[node.ID]; ok {
			ordered = append(ordered, result)
		}
	}
	return ordered
}

func resolveDAGNodeReadiness(definition dagWorkflowDefinition, node dagWorkflowNode, incoming []dagWorkflowEdge, statuses map[string]string) (ready bool, skipped bool) {
	if len(incoming) == 0 {
		return true, false
	}
	allPredicatesResolved := true
	anySatisfied := false
	for _, edge := range incoming {
		predStatus := statuses[edge.Source]
		if predStatus == "" || predStatus == "pending" || predStatus == "running" || predStatus == workflowStatusWaitingApproval {
			allPredicatesResolved = false
			continue
		}
		if dagEdgeSatisfied(definition, edge, predStatus) {
			anySatisfied = true
		}
	}
	if allPredicatesResolved && !anySatisfied {
		return false, true
	}
	return allPredicatesResolved && anySatisfied, false
}

func dagEdgeSatisfied(definition dagWorkflowDefinition, edge dagWorkflowEdge, status string) bool {
	switch strings.TrimSpace(edge.Condition) {
	case "failure":
		return status == "failed"
	case "always":
		return status == "completed" || status == "failed" || status == "skipped"
	default:
		if status == "completed" {
			return true
		}
		return status == "failed" && effectiveDAGFailurePolicy(definition, nodeByID(definition, edge.Source)) == "continue"
	}
}

func initializeNodeRuns(definition dagWorkflowDefinition) map[string]dagNodeRun {
	items := make(map[string]dagNodeRun, len(definition.Nodes))
	for _, node := range definition.Nodes {
		items[node.ID] = dagNodeRun{
			NodeID: node.ID,
			Name:   node.Name,
			Type:   node.Type,
			Status: "pending",
		}
	}
	return items
}

func restoreNodeRuns(definition dagWorkflowDefinition, existing []domainworkflow.NodeRun) map[string]dagNodeRun {
	items := initializeNodeRuns(definition)
	for _, item := range existing {
		if _, ok := items[item.NodeID]; !ok {
			continue
		}
		items[item.NodeID] = item
	}
	return items
}

func collectNodeStatuses(items map[string]dagNodeRun) map[string]string {
	statuses := make(map[string]string, len(items))
	for nodeID, item := range items {
		if item.Status == "" || item.Status == "pending" {
			continue
		}
		statuses[nodeID] = item.Status
	}
	return statuses
}

func hasWaitingApproval(items map[string]dagNodeRun) bool {
	for _, item := range items {
		if item.Status == workflowStatusWaitingApproval {
			return true
		}
	}
	return false
}

func buildStepsFromNodeRuns(definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun) []domainworkflow.Step {
	steps := make([]domainworkflow.Step, 0, len(definition.Nodes))
	for _, node := range definition.Nodes {
		entry := nodeRuns[node.ID]
		status := entry.Status
		if status == "" {
			status = "pending"
		}
		steps = append(steps, domainworkflow.Step{
			Name:    node.Name,
			Status:  status,
			Summary: entry.Summary,
		})
	}
	return steps
}

func mapNodeRunsToSlice(definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun) []dagNodeRun {
	items := make([]dagNodeRun, 0, len(definition.Nodes))
	for _, node := range definition.Nodes {
		items = append(items, nodeRuns[node.ID])
	}
	return items
}

func withNodeRunsMetadata(metadata map[string]any, definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	next := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		next[key] = value
	}
	nodeRunItems := mapNodeRunsToSlice(definition, nodeRuns)
	next["nodeRuns"] = nodeRunItems
	statuses := make(map[string]string, len(nodeRunItems))
	for _, item := range nodeRunItems {
		statuses[item.NodeID] = item.Status
	}
	next["nodeStatus"] = statuses
	return next
}

func withGatewayApprovalWorkflowMetadata(metadata map[string]any, input domainworkflow.Input) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	for _, key := range gatewayApprovalWorkflowMetadataKeys {
		if value := workflowMetadataString(input.Variables, key); value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

func workflowApprovalGatewayMetadata(run domainworkflow.Run, nodeID string) map[string]any {
	metadata := map[string]any{
		"workflowRunId": run.ID,
		"nodeId":        nodeID,
	}
	for _, key := range gatewayApprovalWorkflowMetadataKeys {
		if value := workflowMetadataString(run.Metadata, key); value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

func workflowMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func syncRunNodeState(run domainworkflow.Run, definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun) domainworkflow.Run {
	run.Steps = buildStepsFromNodeRuns(definition, nodeRuns)
	run.NodeRuns = mapNodeRunsToSlice(definition, nodeRuns)
	run.Metadata = withNodeRunsMetadata(run.Metadata, definition, nodeRuns)
	return run
}

func cloneRunForAsyncWorker(run domainworkflow.Run) domainworkflow.Run {
	run.Steps = append([]domainworkflow.Step(nil), run.Steps...)
	run.NodeRuns = append([]domainworkflow.NodeRun(nil), run.NodeRuns...)
	run.Metadata = cloneRunMetadataForAsyncWorker(run.Metadata)
	return run
}

func cloneRunMetadataForAsyncWorker(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = cloneRunMetadataValueForAsyncWorker(value)
	}
	return out
}

func cloneRunMetadataValueForAsyncWorker(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneRunMetadataForAsyncWorker(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneRunMetadataForAsyncWorker(item))
		}
		return out
	case []domainworkflow.NodeRun:
		return append([]domainworkflow.NodeRun(nil), typed...)
	case []domainworkflow.Step:
		return append([]domainworkflow.Step(nil), typed...)
	case []dagWorkflowNode:
		return append([]dagWorkflowNode(nil), typed...)
	case []dagWorkflowEdge:
		return append([]dagWorkflowEdge(nil), typed...)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneRunMetadataValueForAsyncWorker(item))
		}
		return out
	default:
		return value
	}
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
	snapshot := cloneRunForAsyncWorker(run)

	s.runStateMu.Lock()
	if s.runSnapshots == nil {
		s.runSnapshots = make(map[string]domainworkflow.Run)
	}
	s.runSnapshots[run.ID] = snapshot
	for watcher := range s.runWaiters[run.ID] {
		select {
		case watcher <- struct{}{}:
		default:
		}
	}
	s.runStateMu.Unlock()
}

func (s *Service) latestRunSnapshot(runID string) (domainworkflow.Run, bool) {
	s.runStateMu.Lock()
	defer s.runStateMu.Unlock()
	if s.runSnapshots == nil {
		return domainworkflow.Run{}, false
	}
	run, ok := s.runSnapshots[runID]
	if !ok {
		return domainworkflow.Run{}, false
	}
	return cloneRunForAsyncWorker(run), true
}

func (s *Service) registerRunWatcher(runID string) chan struct{} {
	watcher := make(chan struct{}, 1)
	s.runStateMu.Lock()
	if s.runWaiters == nil {
		s.runWaiters = make(map[string]map[chan struct{}]struct{})
	}
	if s.runWaiters[runID] == nil {
		s.runWaiters[runID] = make(map[chan struct{}]struct{})
	}
	s.runWaiters[runID][watcher] = struct{}{}
	s.runStateMu.Unlock()
	return watcher
}

func (s *Service) unregisterRunWatcher(runID string, watcher chan struct{}) {
	s.runStateMu.Lock()
	defer s.runStateMu.Unlock()
	waiters := s.runWaiters[runID]
	if waiters == nil {
		return
	}
	delete(waiters, watcher)
	if len(waiters) == 0 {
		delete(s.runWaiters, runID)
	}
}

func (s *Service) waitForRunStatus(ctx context.Context, runID string, statuses ...string) (domainworkflow.Run, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	allowed := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	watcher := s.registerRunWatcher(runID)
	defer s.unregisterRunWatcher(runID, watcher)

	for {
		run, ok := s.latestRunSnapshot(runID)
		if ok {
			if len(allowed) == 0 {
				return run, nil
			}
			if _, matched := allowed[run.Status]; matched {
				return run, nil
			}
		}

		select {
		case <-watcher:
		case <-ctx.Done():
			return domainworkflow.Run{}, ctx.Err()
		}
	}
}

func (s *Service) queueDepth() int {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()
	if s.runnerQueue == nil {
		return 0
	}
	return len(s.runnerQueue)
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

func (s *Service) executeDAGNode(
	ctx context.Context,
	principal domainidentity.Principal,
	app domainapp.App,
	input domainworkflow.Input,
	binding domaincatalog.ApplicationEnvironment,
	node dagWorkflowNode,
	run domainworkflow.Run,
	artifactState map[string]any,
) dagExecutionResult {
	status := "completed"
	summary := ""
	outputs := map[string]any{}
	artifactOutputs := map[string]any{}
	resolvedInputs := resolveDAGNodeInputs(node, app, input, binding, artifactState)
	resolvedSelectors, selectorErr := s.resolveDAGNodeSelectors(ctx, app, input, binding, node)
	events := []map[string]any{
		{"type": "node_inputs_resolved", "inputs": metadataDAGInputState(resolvedInputs)},
	}
	if len(resolvedSelectors) > 0 {
		events = append(events, map[string]any{"type": "node_selectors_resolved", "selectors": resolvedSelectors})
	}
	if selectorErr != nil {
		status = "failed"
		summary = selectorErr.Error()
		events = append(events, map[string]any{"type": "node_selector_resolution_failed", "error": summary})
		return dagExecutionResult{
			nodeID:    node.ID,
			status:    status,
			summary:   summary,
			inputs:    resolvedInputs,
			outputs:   outputs,
			artifacts: artifactOutputs,
			selectors: resolvedSelectors,
			events:    events,
			step: domainworkflow.Step{
				Name:    node.Name,
				Status:  status,
				Summary: summary,
			},
		}
	}
	selectedTargets := selectedDAGTargetsFromSelectors(resolvedSelectors)
	var selectedTarget *domaincatalog.ReleaseTarget
	if len(selectedTargets) > 0 {
		selectedTarget = &selectedTargets[0]
	}
	switch node.Type {
	case "manual_approval":
		status = workflowStatusWaitingApproval
		summary = "waiting for approval"
	case "deploy_update_image", "release":
		if input.ValidationOnly {
			status = "skipped"
			summary = "release node skipped in validation mode"
			break
		}
		if s.releases == nil {
			status = "failed"
			summary = "release executor is not configured"
			break
		}
		targets := selectedTargets
		if len(targets) == 0 {
			if target := matchBindingTarget(binding, input); target != nil {
				targets = append(targets, *target)
			}
		}
		containerName := configString(node.Config, "containerName")
		if containerName == "" && len(targets) > 0 {
			containerName = targets[0].ContainerName
		}
		actionKind := strings.TrimSpace(binding.ReleasePolicy.ActionKind)
		if actionKind == "" {
			actionKind = configString(node.Config, "actionKind")
		}
		if actionKind == "" {
			actionKind = "deploy"
		}
		resolvedImage := dagArtifactRuntimeString(artifactState["image"])
		imageTag := configString(node.Config, "imageTag")
		imageTagSource := configString(node.Config, "imageTagSource")
		if imageTagSource == "build_artifact" && resolvedImage == "" {
			status = "failed"
			summary = "build artifact image is not available"
			break
		}
		if len(targets) == 0 {
			targets = append(targets, domaincatalog.ReleaseTarget{
				ID:            "input",
				ClusterID:     input.ClusterID,
				Namespace:     input.Namespace,
				WorkloadName:  input.DeploymentName,
				ContainerName: input.ContainerName,
				Enabled:       true,
			})
		}
		fanOutItems := make([]map[string]any, 0, len(targets))
		failedTargets := 0
		for index, target := range targets {
			record, err := s.releases.Trigger(ctx, principal, domainrelease.TriggerInput{
				ApplicationID:            app.ID,
				ApplicationEnvironmentID: binding.ID,
				ClusterID:                targetClusterID(&target, input),
				Namespace:                targetNamespace(&target, input),
				DeploymentName:           targetWorkloadName(&target, input),
				ContainerName:            firstNonEmpty(input.ContainerName, containerName, target.ContainerName),
				Image:                    resolvedImage,
				ImageTag:                 firstNonEmpty(input.ImageTag, imageTag),
				ReleaseName:              input.ReleaseName,
				ActionKind:               actionKind,
				WorkflowRunID:            run.ID,
			})
			targetStatus := "completed"
			targetSummary := ""
			releaseID := ""
			if err != nil {
				targetStatus = "failed"
				targetSummary = err.Error()
				failedTargets++
			} else {
				releaseID = record.ID
				targetStatus = record.Status
				targetSummary = fmt.Sprintf("release %s finished with status %s", record.ID, record.Status)
			}
			fanOutItems = append(fanOutItems, map[string]any{
				"index":     index,
				"target":    releaseTargetMetadata(target),
				"status":    targetStatus,
				"summary":   targetSummary,
				"releaseId": releaseID,
			})
			if err != nil && normalizeDAGFanOutFailurePolicy(node.FanOutFailurePolicy) != "continue" && effectiveDAGFailurePolicy(dagWorkflowDefinition{}, node) != "continue" {
				break
			}
		}
		if len(targets) > 1 {
			outputs["fanOut"] = map[string]any{
				"strategy":      firstNonEmpty(normalizeDAGFanOutStrategy(node.FanOutStrategy), "parallel"),
				"targetCount":   len(targets),
				"failurePolicy": firstNonEmpty(normalizeDAGFanOutFailurePolicy(node.FanOutFailurePolicy), effectiveDAGFailurePolicy(dagWorkflowDefinition{}, node)),
				"targets":       fanOutItems,
			}
			events = append(events, map[string]any{"type": "node_fan_out_completed", "fanOut": outputs["fanOut"]})
		}
		if failedTargets > 0 {
			status = "failed"
			summary = fmt.Sprintf("release fan-out failed on %d/%d targets", failedTargets, len(targets))
			break
		}
		if len(fanOutItems) == 1 {
			summary = strings.TrimSpace(fmt.Sprint(fanOutItems[0]["summary"]))
		} else {
			summary = fmt.Sprintf("release fan-out completed on %d targets", len(fanOutItems))
		}
	case "build":
		if input.ValidationOnly {
			status = "skipped"
			summary = "build node skipped in validation mode"
			break
		}
		if s.builds == nil {
			status = "failed"
			summary = "build executor is not configured"
			break
		}
		refType := firstNonEmpty(input.RefType, binding.BuildPolicy.RefType, "branch")
		refName := firstNonEmpty(input.RefName, binding.BuildPolicy.RefValue, app.DefaultBranch, "main")
		record, err := s.builds.Execute(ctx, principal, domainbuild.TriggerInput{
			ApplicationID:            app.ID,
			ApplicationEnvironmentID: binding.ID,
			BuildSourceID:            firstNonEmpty(input.BuildSourceID, binding.BuildPolicy.SourceID),
			RefType:                  refType,
			RefName:                  refName,
			ImageTag:                 firstNonEmpty(input.ImageTag, app.DefaultTag),
			BuildArgs:                mergeDAGMaps(binding.BuildPolicy.BuildArgs, input.BuildArgs),
			Variables:                mergeDAGMaps(binding.BuildPolicy.Variables, input.Variables),
			TriggeredByWorkflowRunID: run.ID,
		})
		if err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = fmt.Sprintf("build %s queued", record.ID)
		if record.Metadata != nil {
			if artifact, ok := record.Metadata["artifact"]; ok {
				outputs["artifact"] = artifact
			}
			if image, ok := record.Metadata["image"]; ok {
				outputs["image"] = image
			}
		}
	case "wait_rollout":
		if s.resources == nil {
			status = "failed"
			summary = "resource executor is not configured"
			break
		}
		timeoutSeconds := node.TimeoutSeconds
		if configured := toInt(node.Config["timeoutSeconds"], 0); configured > 0 {
			timeoutSeconds = configured
		}
		waitSummary, err := s.waitForRollout(ctx, principal, targetClusterID(selectedTarget, input), targetNamespace(selectedTarget, input), targetWorkloadName(selectedTarget, input), timeoutSeconds)
		if err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = waitSummary
	case "check_http", "smoke_test", "verify", "check":
		checkURL := configString(node.Config, "url")
		if checkURL == "" {
			checkURL = configString(node.Config, "endpoint")
		}
		if checkURL == "" {
			status = "skipped"
			summary = "HTTP check skipped because url is not configured"
			break
		}
		expectedStatus := toInt(node.Config["expectedStatus"], 200)
		if err := s.checkHTTP(ctx, checkURL, expectedStatus); err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = fmt.Sprintf("HTTP check %s returned %d", checkURL, expectedStatus)
	case "check_k8s_event":
		if s.resources == nil {
			status = "failed"
			summary = "resource executor is not configured"
			break
		}
		if err := s.checkK8sEvents(ctx, principal, input.ClusterID, input.Namespace, input.DeploymentName, node.Config); err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = "no blocking kubernetes event detected"
	case "rollback_to_previous":
		if s.resources == nil {
			status = "failed"
			summary = "resource executor is not configured"
			break
		}
		result, err := s.rollbackToPreviousRevision(ctx, principal, targetClusterID(selectedTarget, input), targetNamespace(selectedTarget, input), targetWorkloadName(selectedTarget, input))
		if err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = result.Message
	case "restart_workload":
		if s.resources == nil {
			status = "failed"
			summary = "resource executor is not configured"
			break
		}
		deploymentName := firstNonEmpty(strings.TrimSpace(fmt.Sprint(node.Config["deploymentName"])), selectedTargetWorkloadName(selectedTarget), strings.TrimSpace(input.DeploymentName))
		if deploymentName == "" {
			status = "failed"
			summary = "restart workload requires deploymentName"
			break
		}
		if err := s.resources.RestartDeployment(ctx, principal, targetClusterID(selectedTarget, input), targetNamespace(selectedTarget, input), deploymentName); err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = fmt.Sprintf("restarted deployment %s", deploymentName)
	case "scale_workload":
		if s.resources == nil {
			status = "failed"
			summary = "resource executor is not configured"
			break
		}
		deploymentName := firstNonEmpty(strings.TrimSpace(fmt.Sprint(node.Config["deploymentName"])), selectedTargetWorkloadName(selectedTarget), strings.TrimSpace(input.DeploymentName))
		if deploymentName == "" {
			status = "failed"
			summary = "scale workload requires deploymentName"
			break
		}
		replicas := int32(toInt(node.Config["replicas"], 1))
		if err := s.resources.ScaleDeployment(ctx, principal, targetClusterID(selectedTarget, input), targetNamespace(selectedTarget, input), deploymentName, replicas); err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = fmt.Sprintf("scaled deployment %s to %d", deploymentName, replicas)
	case "delete_pod", "evict_pod":
		if s.resources == nil {
			status = "failed"
			summary = "resource executor is not configured"
			break
		}
		podName := firstNonEmpty(strings.TrimSpace(fmt.Sprint(node.Config["podName"])), strings.TrimSpace(fmt.Sprint(node.Config["name"])))
		if podName == "" {
			status = "failed"
			summary = "delete pod requires podName"
			break
		}
		if err := s.resources.DeletePod(ctx, principal, targetClusterID(selectedTarget, input), targetNamespace(selectedTarget, input), podName); err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = fmt.Sprintf("%s executed for pod %s", node.Type, podName)
	case "http_callback":
		callbackURL := strings.TrimSpace(fmt.Sprint(node.Config["url"]))
		if callbackURL == "" {
			status = "failed"
			summary = "http callback requires url"
			break
		}
		method := firstNonEmpty(strings.TrimSpace(fmt.Sprint(node.Config["method"])), http.MethodPost)
		body := strings.TrimSpace(fmt.Sprint(node.Config["body"]))
		expectedStatus := toInt(node.Config["expectedStatus"], 200)
		if err := s.callHTTPCallback(ctx, callbackURL, method, body, expectedStatus, toConfigMap(node.Config["headers"])); err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = fmt.Sprintf("HTTP callback %s %s completed", method, callbackURL)
	case "create_silence":
		if s.alerts == nil {
			status = "failed"
			summary = "alert mutator is not configured"
			break
		}
		name := firstNonEmpty(strings.TrimSpace(fmt.Sprint(node.Config["name"])), "workflow-generated-silence")
		reason := strings.TrimSpace(fmt.Sprint(node.Config["reason"]))
		durationMinutes := toInt(node.Config["durationMinutes"], 60)
		if durationMinutes <= 0 {
			durationMinutes = 60
		}
		startsAt := time.Now().UTC()
		endsAt := startsAt.Add(time.Duration(durationMinutes) * time.Minute)
		matchers := toConfigMap(node.Config["matchers"])
		if len(matchers) == 0 {
			matchers = map[string]any{
				"clusterId": input.ClusterID,
				"namespace": input.Namespace,
			}
		}
		silence, err := s.alerts.CreateWorkflowSilence(ctx, principal, domainalert.SilenceInput{
			Name:     name,
			Matchers: matchers,
			Reason:   reason,
			StartsAt: startsAt,
			EndsAt:   endsAt,
			Enabled:  true,
		})
		if err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		outputs["silenceId"] = silence.ID
		summary = fmt.Sprintf("created silence %s", silence.ID)
	case "notify":
		channel := strings.TrimSpace(fmt.Sprint(node.Config["channel"]))
		if channel == "" {
			status = "skipped"
			summary = "notification skipped because channel is not configured"
			break
		}
		summary = fmt.Sprintf("notification queued for channel %s", channel)
	default:
		status = "skipped"
		summary = fmt.Sprintf("node type %s is not executable yet", node.Type)
	}
	if status == "failed" && node.ContinueOnFailure {
		summary = fmt.Sprintf("continued after failure: %s", summary)
		status = "completed"
	}
	if status == "failed" && effectiveDAGFailurePolicy(dagWorkflowDefinition{}, node) == "continue" {
		events = append(events, map[string]any{"type": "node_failure_continued", "originalStatus": "failed", "summary": summary})
	}
	if len(node.ArtifactOutputs) > 0 {
		resolvedArtifactOutputs, artifactErr := materializeDAGArtifactOutputs(node, outputs, artifactState, status)
		if artifactErr != nil {
			status = "failed"
			summary = artifactErr.Error()
			artifactOutputs = map[string]any{}
			outputs = map[string]any{}
			events = append(events, map[string]any{"type": "artifact_outputs_missing", "error": summary})
		} else {
			artifactOutputs = resolvedArtifactOutputs
		}
		for key, value := range artifactOutputs {
			outputs[key] = value
		}
		if len(artifactOutputs) > 0 {
			events = append(events, map[string]any{"type": "artifact_outputs_recorded", "artifacts": metadataDAGArtifactState(artifactOutputs)})
			if references := s.persistDAGArtifactOutputs(ctx, app, binding, node, run, artifactOutputs, status); len(references) > 0 {
				outputs["artifactReferences"] = references
				events = append(events, map[string]any{"type": "artifact_outputs_persisted", "artifacts": references})
			}
		}
	}
	for _, outputName := range node.Outputs {
		outputName = strings.TrimSpace(outputName)
		if outputName == "" {
			continue
		}
		if _, ok := outputs[outputName]; !ok {
			if value, ok := artifactState[outputName]; ok {
				outputs[outputName] = value
			}
		}
	}
	events = append(events, map[string]any{"type": "node_finished", "status": status, "summary": summary})

	return dagExecutionResult{
		nodeID:    node.ID,
		status:    status,
		summary:   summary,
		inputs:    resolvedInputs,
		outputs:   outputs,
		artifacts: artifactOutputs,
		selectors: resolvedSelectors,
		events:    events,
		step: domainworkflow.Step{
			Name:    node.Name,
			Status:  status,
			Summary: summary,
		},
	}
}

func (s *Service) waitForRollout(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, deploymentName string, timeoutSeconds int) (string, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		status, err := s.resources.GetDeploymentRolloutStatus(ctx, principal, clusterID, namespace, deploymentName)
		if err != nil {
			return "", err
		}
		switch status.Status {
		case "healthy":
			return status.Message, nil
		case "degraded":
			return "", fmt.Errorf("rollout degraded: %s", status.Message)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("rollout timed out after %d seconds", timeoutSeconds)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) checkHTTP(ctx context.Context, targetURL string, expectedStatus int) error {
	if s.httpClient == nil {
		s.httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("HTTP check got status %d, want %d", resp.StatusCode, expectedStatus)
	}
	return nil
}

func (s *Service) callHTTPCallback(ctx context.Context, targetURL, method, body string, expectedStatus int, headers map[string]any) error {
	if s.httpClient == nil {
		s.httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, fmt.Sprint(value))
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if expectedStatus > 0 && resp.StatusCode != expectedStatus {
		return fmt.Errorf("HTTP callback got status %d, want %d", resp.StatusCode, expectedStatus)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("HTTP callback got status %d", resp.StatusCode)
	}
	return nil
}

func (s *Service) checkK8sEvents(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, deploymentName string, config map[string]any) error {
	events, err := s.resources.ListClusterEvents(ctx, principal, clusterID, namespace, 50)
	if err != nil {
		return err
	}
	eventType := strings.TrimSpace(fmt.Sprint(config["eventType"]))
	reasonContains := strings.TrimSpace(fmt.Sprint(config["reasonContains"]))
	for _, item := range events {
		if item.InvolvedName != deploymentName {
			continue
		}
		if eventType != "" && item.Type != eventType {
			continue
		}
		if reasonContains != "" && !strings.Contains(strings.ToLower(item.Reason), strings.ToLower(reasonContains)) {
			continue
		}
		if item.Type == "Warning" {
			return fmt.Errorf("found warning event %s: %s", item.Reason, item.Message)
		}
	}
	return nil
}

func (s *Service) rollbackToPreviousRevision(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, deploymentName string) (domainresource.DeploymentRollbackView, error) {
	history, err := s.resources.ListDeploymentRolloutHistory(ctx, principal, clusterID, namespace, deploymentName)
	if err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	if len(history) < 2 {
		return domainresource.DeploymentRollbackView{}, fmt.Errorf("no previous revision available for rollback")
	}
	current, err := s.resources.GetDeploymentRolloutStatus(ctx, principal, clusterID, namespace, deploymentName)
	if err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	targetRevision := ""
	for _, item := range history {
		if item.Revision != "" && item.Revision != current.Revision {
			targetRevision = item.Revision
			break
		}
	}
	if targetRevision == "" {
		return domainresource.DeploymentRollbackView{}, fmt.Errorf("unable to resolve previous deployment revision")
	}
	return s.resources.RollbackDeployment(ctx, principal, clusterID, namespace, deploymentName, targetRevision)
}

func matchBindingTarget(binding domaincatalog.ApplicationEnvironment, input domainworkflow.Input) *domaincatalog.ReleaseTarget {
	for _, target := range binding.Targets {
		if !target.Enabled {
			continue
		}
		if bindingID := strings.TrimSpace(input.ApplicationEnvironmentID); bindingID != "" && binding.ID != bindingID {
			continue
		}
		if target.ClusterID == strings.TrimSpace(input.ClusterID) &&
			target.Namespace == strings.TrimSpace(input.Namespace) &&
			target.WorkloadName == strings.TrimSpace(input.DeploymentName) {
			copyTarget := target
			return &copyTarget
		}
	}
	return nil
}

func selectedDAGTargetFromSelectors(selectors map[string]any) *domaincatalog.ReleaseTarget {
	targets := selectedDAGTargetsFromSelectors(selectors)
	if len(targets) == 0 {
		return nil
	}
	return &targets[0]
}

func selectedDAGTargetsFromSelectors(selectors map[string]any) []domaincatalog.ReleaseTarget {
	if len(selectors) == 0 {
		return nil
	}
	if rawTargets, ok := selectors["targets"].([]map[string]any); ok {
		targets := make([]domaincatalog.ReleaseTarget, 0, len(rawTargets))
		for _, item := range rawTargets {
			targets = append(targets, dagTargetFromMetadata(item))
		}
		return targets
	}
	if rawTargets, ok := selectors["targets"].([]any); ok {
		targets := make([]domaincatalog.ReleaseTarget, 0, len(rawTargets))
		for _, item := range rawTargets {
			if mapped, ok := item.(map[string]any); ok {
				targets = append(targets, dagTargetFromMetadata(mapped))
			}
		}
		if len(targets) > 0 {
			return targets
		}
	}
	targetMap, ok := selectors["target"].(map[string]any)
	if !ok || len(targetMap) == 0 {
		return nil
	}
	return []domaincatalog.ReleaseTarget{dagTargetFromMetadata(targetMap)}
}

func dagTargetFromMetadata(targetMap map[string]any) domaincatalog.ReleaseTarget {
	target := domaincatalog.ReleaseTarget{
		ID:            workflowMetadataString(targetMap, "id"),
		ClusterID:     workflowMetadataString(targetMap, "clusterId"),
		Namespace:     workflowMetadataString(targetMap, "namespace"),
		TargetKind:    workflowMetadataString(targetMap, "targetKind"),
		ExecutorKind:  workflowMetadataString(targetMap, "executorKind"),
		GroupKey:      workflowMetadataString(targetMap, "groupKey"),
		WaveKey:       workflowMetadataString(targetMap, "waveKey"),
		RegionKey:     workflowMetadataString(targetMap, "regionKey"),
		ConfigRef:     workflowMetadataString(targetMap, "configRef"),
		WorkloadKind:  workflowMetadataString(targetMap, "workloadKind"),
		WorkloadName:  workflowMetadataString(targetMap, "workloadName"),
		ContainerName: workflowMetadataString(targetMap, "containerName"),
		Enabled:       true,
	}
	if metadata, ok := targetMap["metadata"].(map[string]any); ok {
		target.Metadata = metadata
	}
	return target
}

func selectedTargetWorkloadName(target *domaincatalog.ReleaseTarget) string {
	if target == nil {
		return ""
	}
	return strings.TrimSpace(target.WorkloadName)
}

func targetClusterID(target *domaincatalog.ReleaseTarget, input domainworkflow.Input) string {
	if target == nil {
		return strings.TrimSpace(input.ClusterID)
	}
	return firstNonEmpty(target.ClusterID, input.ClusterID)
}

func targetNamespace(target *domaincatalog.ReleaseTarget, input domainworkflow.Input) string {
	if target == nil {
		return strings.TrimSpace(input.Namespace)
	}
	return firstNonEmpty(target.Namespace, input.Namespace)
}

func targetWorkloadName(target *domaincatalog.ReleaseTarget, input domainworkflow.Input) string {
	if target == nil {
		return strings.TrimSpace(input.DeploymentName)
	}
	return firstNonEmpty(target.WorkloadName, input.DeploymentName)
}

func normalizeDAGFanOutStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "":
		return ""
	case "parallel", "serial", "batch":
		return strings.ToLower(strings.TrimSpace(strategy))
	default:
		return ""
	}
}

func normalizeDAGFanOutFailurePolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "continue", "stop", "rollback":
		return strings.ToLower(strings.TrimSpace(policy))
	default:
		return ""
	}
}

func materializeDAGArtifactOutputs(node dagWorkflowNode, outputs map[string]any, artifactState map[string]any, status string) (map[string]any, error) {
	if len(node.ArtifactOutputs) == 0 {
		return nil, nil
	}
	artifacts := make(map[string]any, len(node.ArtifactOutputs))
	for _, artifact := range node.ArtifactOutputs {
		name := strings.TrimSpace(fmt.Sprint(artifact["name"]))
		kind := strings.TrimSpace(fmt.Sprint(artifact["kind"]))
		if name == "" || !isAllowedDeliveryArtifactKind(kind) {
			continue
		}
		value := firstArtifactValue(name, kind, outputs, artifactState)
		if value == nil && toBool(artifact["required"]) && status == "completed" {
			return nil, fmt.Errorf("required artifact output %s (%s) is missing", name, kind)
		}
		if value == nil {
			continue
		}
		artifactRecord := map[string]any{
			"name":   name,
			"kind":   kind,
			"value":  value,
			"nodeId": node.ID,
		}
		for _, key := range []string{"ref", "digest", "path", "retentionUntil", "retention"} {
			if raw, ok := artifact[key]; ok && raw != nil {
				artifactRecord[key] = raw
			}
		}
		if status != "" {
			artifactRecord["status"] = status
		}
		artifacts[name] = artifactRecord
		if kind != name {
			artifacts[kind] = artifactRecord
		}
	}
	return artifacts, nil
}

func (s *Service) persistDAGArtifactOutputs(ctx context.Context, app domainapp.App, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode, run domainworkflow.Run, artifacts map[string]any, status string) map[string]any {
	if s == nil || s.artifacts == nil || len(artifacts) == 0 || strings.TrimSpace(run.ID) == "" {
		return nil
	}
	references := map[string]any{}
	seen := map[string]struct{}{}
	for key, raw := range artifacts {
		recordMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(workflowMetadataString(recordMap, "name"), key)
		kind := workflowMetadataString(recordMap, "kind")
		if name == "" || kind == "" {
			continue
		}
		dedupeKey := kind + ":" + name
		if _, exists := seen[dedupeKey]; exists {
			continue
		}
		seen[dedupeKey] = struct{}{}
		item := dagArtifactStoreItem(app, binding, node, run, recordMap, status)
		stored, err := s.artifacts.UpsertExecutionArtifact(ctx, item)
		if err != nil {
			continue
		}
		references[name] = map[string]any{
			"id":             stored.ID,
			"kind":           stored.Kind,
			"name":           stored.Name,
			"ref":            stored.Ref,
			"digest":         stored.Digest,
			"path":           stored.Path,
			"status":         stored.Status,
			"workflowRunId":  stored.WorkflowRunID,
			"workflowNodeId": stored.WorkflowNodeID,
		}
	}
	if len(references) == 0 {
		return nil
	}
	return references
}

func metadataDAGExecutionResult(result dagExecutionResult) map[string]any {
	outputs := metadataDAGOutputState(result.outputs)
	artifacts := metadataDAGArtifactState(result.artifacts)
	if references := metadataDAGArtifactReferences(result.outputs); len(references) > 0 {
		for key, value := range references {
			if outputs == nil {
				outputs = map[string]any{}
			}
			outputs[key] = value
			if artifacts == nil {
				artifacts = map[string]any{}
			}
			artifacts[key] = value
		}
	}
	return map[string]any{
		"inputs":    metadataDAGInputState(result.inputs),
		"outputs":   outputs,
		"artifacts": artifacts,
		"selectors": result.selectors,
		"status":    result.status,
		"summary":   result.summary,
	}
}

func metadataDAGInputState(inputs map[string]any) map[string]any {
	if len(inputs) == 0 {
		return nil
	}
	metadata := make(map[string]any, len(inputs))
	for key, value := range inputs {
		metadata[key] = metadataDAGArtifactValue(key, value)
	}
	return metadata
}

func metadataDAGOutputState(outputs map[string]any) map[string]any {
	if len(outputs) == 0 {
		return nil
	}
	metadata := make(map[string]any, len(outputs))
	for key, value := range outputs {
		if key == "artifactReferences" {
			metadata[key] = value
			continue
		}
		metadata[key] = metadataDAGArtifactValue(key, value)
	}
	return metadata
}

func metadataDAGArtifactReferences(outputs map[string]any) map[string]any {
	if len(outputs) == 0 {
		return nil
	}
	references, ok := outputs["artifactReferences"].(map[string]any)
	if !ok || len(references) == 0 {
		return nil
	}
	metadata := make(map[string]any, len(references))
	for key, value := range references {
		metadata[key] = metadataDAGArtifactValue(key, value)
	}
	return metadata
}

func metadataDAGArtifactState(artifactState map[string]any) map[string]any {
	if len(artifactState) == 0 {
		return nil
	}
	metadata := make(map[string]any, len(artifactState))
	for key, value := range artifactState {
		if key == "artifactReferences" {
			continue
		}
		metadata[key] = metadataDAGArtifactValue(key, value)
	}
	if references, ok := artifactState["artifactReferences"].(map[string]any); ok {
		for key, value := range references {
			metadata[key] = metadataDAGArtifactValue(key, value)
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func metadataDAGArtifactValue(key string, value any) any {
	recordMap, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if references, ok := recordMap["artifactReferences"].(map[string]any); ok {
		for refKey, refValue := range references {
			if strings.TrimSpace(refKey) == strings.TrimSpace(key) {
				return metadataDAGArtifactValue(refKey, refValue)
			}
		}
	}
	lightweight := map[string]any{}
	for _, field := range []string{"id", "kind", "name", "ref", "digest", "path", "status", "workflowRunId", "workflowNodeId", "nodeId", "retentionUntil", "retention"} {
		if raw, ok := recordMap[field]; ok && raw != nil {
			lightweight[field] = raw
		}
	}
	if raw, ok := recordMap["value"]; ok {
		if lightweight["ref"] == nil {
			if ref := artifactValueString(raw, "ref"); ref != "" {
				lightweight["ref"] = ref
			} else if text := strings.TrimSpace(fmt.Sprint(raw)); text != "" && text != "<nil>" {
				lightweight["ref"] = text
			}
		}
		if lightweight["digest"] == nil {
			if digest := artifactValueString(raw, "digest"); digest != "" {
				lightweight["digest"] = digest
			}
		}
		if lightweight["path"] == nil {
			if path := artifactValueString(raw, "path"); path != "" {
				lightweight["path"] = path
			}
		}
	}
	if len(lightweight) == 0 {
		return value
	}
	if lightweight["name"] == nil && strings.TrimSpace(key) != "" {
		lightweight["name"] = strings.TrimSpace(key)
	}
	return lightweight
}

func dagArtifactStoreItem(app domainapp.App, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode, run domainworkflow.Run, artifact map[string]any, status string) domaindelivery.ExecutionArtifact {
	name := workflowMetadataString(artifact, "name")
	kind := workflowMetadataString(artifact, "kind")
	value := artifact["value"]
	ref := firstNonEmpty(workflowMetadataString(artifact, "ref"), artifactValueString(value, "ref"))
	digest := firstNonEmpty(workflowMetadataString(artifact, "digest"), artifactValueString(value, "digest"))
	path := firstNonEmpty(workflowMetadataString(artifact, "path"), artifactValueString(value, "path"))
	if ref == "" && path == "" {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			ref = text
		}
	}
	metadata := map[string]any{
		"value":              value,
		"workflowName":       run.WorkflowName,
		"nodeName":           node.Name,
		"nodeType":           node.Type,
		"environmentKey":     binding.EnvironmentKey,
		"workflowTemplateId": binding.WorkflowTemplateID,
	}
	retentionUntil := artifactTimeValue(artifact["retentionUntil"])
	if retentionUntil == nil {
		retentionUntil = artifactRetentionDeadline(artifact["retention"])
	}
	return domaindelivery.ExecutionArtifact{
		ID:                       "artifact:" + run.ID + ":" + node.ID + ":" + name,
		WorkflowRunID:            run.ID,
		WorkflowNodeID:           node.ID,
		ApplicationID:            app.ID,
		ApplicationEnvironmentID: binding.ID,
		Kind:                     kind,
		Name:                     name,
		Ref:                      ref,
		Digest:                   digest,
		Path:                     path,
		Status:                   firstNonEmpty(workflowMetadataString(artifact, "status"), status),
		SizeBytes:                int64(toInt(firstNonNil(artifact["sizeBytes"], artifactValue(value, "sizeBytes")), 0)),
		Metadata:                 metadata,
		RetentionUntil:           retentionUntil,
	}
}

func artifactValue(value any, key string) any {
	if mapped, ok := value.(map[string]any); ok {
		return mapped[key]
	}
	return nil
}

func artifactValueString(value any, key string) string {
	raw := artifactValue(value, key)
	if raw == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(raw))
	if text == "<nil>" {
		return ""
	}
	return text
}

func dagArtifactRuntimeString(value any) string {
	if mapped, ok := value.(map[string]any); ok {
		for _, key := range []string{"ref", "digest", "path"} {
			if text := workflowMetadataString(mapped, key); text != "" {
				return text
			}
		}
		if raw, ok := mapped["value"]; ok {
			if text := dagArtifactRuntimeString(raw); text != "" {
				return text
			}
		}
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func artifactTimeValue(value any) *time.Time {
	switch typed := value.(type) {
	case time.Time:
		copyValue := typed.UTC()
		return &copyValue
	case string:
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed)); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	return nil
}

func artifactRetentionDeadline(value any) *time.Time {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return nil
	}
	if duration, err := time.ParseDuration(text); err == nil {
		deadline := time.Now().UTC().Add(duration)
		return &deadline
	}
	return artifactTimeValue(text)
}

func firstArtifactValue(name, kind string, outputs map[string]any, artifactState map[string]any) any {
	for _, key := range []string{name, kind} {
		if key == "" {
			continue
		}
		if value, ok := outputs[key]; ok {
			return value
		}
		if value, ok := artifactState[key]; ok {
			return value
		}
	}
	return nil
}

func definitionFromRunMetadata(run domainworkflow.Run) (dagWorkflowDefinition, bool) {
	if len(run.Metadata) == 0 {
		return dagWorkflowDefinition{}, false
	}
	if nodes, ok := run.Metadata["nodes"].([]dagWorkflowNode); ok && len(nodes) > 0 {
		definition := dagWorkflowDefinition{
			SchemaVersion: toInt(run.Metadata["schemaVersion"], 2),
			Mode:          workflowMetadataMode(run.Metadata),
			Nodes:         nodes,
		}
		if edges, edgeOK := run.Metadata["edges"].([]dagWorkflowEdge); edgeOK {
			definition.Edges = edges
		}
		return definition, true
	}
	return parseDAGWorkflowDefinition(map[string]any{
		"mode":          workflowMetadataMode(run.Metadata),
		"schemaVersion": run.Metadata["schemaVersion"],
		"nodes":         run.Metadata["nodes"],
		"edges":         run.Metadata["edges"],
	})
}

func toMapSlice(value any) ([]map[string]any, bool) {
	items, ok := value.([]any)
	if !ok {
		items2, ok2 := value.([]map[string]any)
		if !ok2 {
			return nil, false
		}
		out := make([]map[string]any, 0, len(items2))
		for _, item := range items2 {
			out = append(out, item)
		}
		return out, true
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		valueMap, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, valueMap)
	}
	return out, true
}

func toMapSliceOrEmpty(value any) []map[string]any {
	items, ok := toMapSlice(value)
	if !ok {
		return nil
	}
	return items
}

func toConfigMap(value any) map[string]any {
	if valueMap, ok := value.(map[string]any); ok {
		return valueMap
	}
	return map[string]any{}
}

func toStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if text := strings.TrimSpace(item); text != "" {
					out = append(out, text)
				}
			}
			return out
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func configString(config map[string]any, key string) string {
	if len(config) == 0 {
		return ""
	}
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func mergeDAGMaps(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	result := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		result[key] = value
	}
	return result
}

func toInt(value any, fallback int) int {
	switch current := value.(type) {
	case int:
		return current
	case int32:
		return int(current)
	case int64:
		return int(current)
	case float64:
		return int(current)
	case float32:
		return int(current)
	default:
		return fallback
	}
}

func toBool(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}

func isApplicationMissing(err error) bool {
	return errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apprepo.ErrNotFound)
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
