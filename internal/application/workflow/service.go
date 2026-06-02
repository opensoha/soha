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
	appaccess "github.com/soha/soha/internal/application/access"
	domainaccess "github.com/soha/soha/internal/domain/access"
	domainalert "github.com/soha/soha/internal/domain/alert"
	domainapp "github.com/soha/soha/internal/domain/application"
	domainbuild "github.com/soha/soha/internal/domain/build"
	domaincatalog "github.com/soha/soha/internal/domain/catalog"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainrelease "github.com/soha/soha/internal/domain/release"
	domainresource "github.com/soha/soha/internal/domain/resource"
	domainworkflow "github.com/soha/soha/internal/domain/workflow"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/requestctx"
	"github.com/soha/soha/internal/platform/runtimeobs"
	apprepo "github.com/soha/soha/internal/repository/application"
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
		case task := <-s.runnerQueue:
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
		s.logDebug("workflow queued", zap.String("runID", task.run.ID), zap.String("applicationID", task.run.ApplicationID), zap.Int("queueDepth", len(queue)))
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
	nodeRuns := initializeNodeRuns(parsed)
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
	runMetadata := map[string]any{
		"applicationName":      workflowName,
		"mode":                 "release_dag",
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
	ID                string
	Name              string
	Type              string
	TimeoutSeconds    int
	ContinueOnFailure bool
	Config            map[string]any
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
	nodeID  string
	step    domainworkflow.Step
	status  string
	summary string
	outputs map[string]any
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
		"mode":                 "release_dag",
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
		"mode":                 "release_dag",
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
		"mode":                 "release_dag",
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
	startedAt := time.Now()
	if s.metrics != nil {
		s.metrics.RecordStart(runtimeobs.ComponentWorkflowRunner, run.ID, s.queueDepth(), len(definition.Nodes))
	}
	s.logDebug("workflow execution started", zap.String("runID", run.ID), zap.String("applicationID", run.ApplicationID))
	nodeRuns := restoreNodeRuns(definition, run.NodeRuns)
	statuses := collectNodeStatuses(nodeRuns)
	artifactState := make(map[string]any)

	run.Status = "running"
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	run = syncRunNodeState(run, definition, nodeRuns)
	run = s.updateRun(ctx, run)

	for len(statuses) < len(definition.Nodes) {
		if err := ctx.Err(); err != nil {
			s.finalizeRunCancellation(ctx, run, definition, nodeRuns, statuses, err)
			if s.metrics != nil {
				s.metrics.RecordFinish(runtimeobs.ComponentWorkflowRunner, run.ID, time.Since(startedAt), s.queueDepth(), len(definition.Nodes), runtimeobs.OutcomeCanceled, err)
			}
			s.logWarn("workflow execution canceled", zap.String("runID", run.ID), zap.Error(err))
			return
		}

		ready := make([]dagWorkflowNode, 0)
		progressed := false
		for _, node := range definition.Nodes {
			if statuses[node.ID] != "" {
				continue
			}
			isReady, skipped := resolveDAGNodeReadiness(node, incomingEdgesForNode(definition, node.ID), statuses)
			switch {
			case skipped:
				entry := nodeRuns[node.ID]
				entry.Status = "skipped"
				entry.Summary = "conditions not met"
				entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
				nodeRuns[node.ID] = entry
				statuses[node.ID] = entry.Status
				progressed = true
			case isReady:
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
			for key, value := range result.outputs {
				artifactState[key] = value
			}
			if len(result.outputs) > 0 {
				if run.Metadata == nil {
					run.Metadata = map[string]any{}
				}
				run.Metadata["artifacts"] = artifactState
			}
		}
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		run = syncRunNodeState(run, definition, nodeRuns)
		run = s.updateRun(ctx, run)
	}

	finalStatus := "completed"
	for _, node := range definition.Nodes {
		status := statuses[node.ID]
		if status == "failed" {
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
		s.logWarn("workflow execution failed", zap.String("runID", run.ID), zap.String("applicationID", run.ApplicationID), zap.Duration("duration", time.Since(startedAt)))
		return
	}
	s.logDebug("workflow execution completed", zap.String("runID", run.ID), zap.String("applicationID", run.ApplicationID), zap.Duration("duration", time.Since(startedAt)))
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
	if mode != "release_dag" {
		return dagWorkflowDefinition{}, false
	}
	nodeItems, ok := toMapSlice(definition["nodes"])
	if !ok || len(nodeItems) == 0 {
		return dagWorkflowDefinition{}, false
	}
	edgeItems, _ := toMapSlice(definition["edges"])
	nodes := make([]dagWorkflowNode, 0, len(nodeItems))
	for _, item := range nodeItems {
		nodes = append(nodes, dagWorkflowNode{
			ID:                strings.TrimSpace(fmt.Sprint(item["id"])),
			Name:              strings.TrimSpace(fmt.Sprint(item["name"])),
			Type:              strings.TrimSpace(fmt.Sprint(item["type"])),
			TimeoutSeconds:    toInt(item["timeoutSeconds"], 300),
			ContinueOnFailure: toBool(item["continueOnFailure"]),
			Config:            toConfigMap(item["config"]),
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
	metadata := map[string]any{
		"mode":                 "release_dag",
		"schemaVersion":        definition.SchemaVersion,
		"workflowTemplateId":   binding.WorkflowTemplateID,
		"workflowTemplateKey":  binding.WorkflowTemplate.Key,
		"workflowTemplateName": binding.WorkflowTemplate.Name,
		"bindingId":            binding.ID,
		"nodes":                definition.Nodes,
		"edges":                definition.Edges,
	}
	metadata = withGatewayApprovalWorkflowMetadata(metadata, input)

	for len(statuses) < len(definition.Nodes) {
		ready := make([]dagWorkflowNode, 0)
		progressed := false
		for _, node := range definition.Nodes {
			if statuses[node.ID] != "" {
				continue
			}
			readyState, skipped := resolveDAGNodeReadiness(node, incoming[node.ID], statuses)
			switch {
			case skipped:
				statuses[node.ID] = "skipped"
				steps = append(steps, domainworkflow.Step{Name: node.Name, Status: "skipped", Summary: "conditions not met"})
				progressed = true
			case readyState:
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
			for key, value := range result.outputs {
				artifactState[key] = value
			}
		}
	}

	finalStatus := "completed"
	for _, node := range definition.Nodes {
		status := statuses[node.ID]
		if status == "failed" {
			finalStatus = "failed"
			break
		}
		if status == "" {
			steps = append(steps, domainworkflow.Step{Name: node.Name, Status: "skipped", Summary: "unresolved DAG dependency"})
		}
	}
	metadata["nodeStatus"] = statuses
	if len(artifactState) > 0 {
		metadata["artifacts"] = artifactState
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

func resolveDAGNodeReadiness(node dagWorkflowNode, incoming []dagWorkflowEdge, statuses map[string]string) (ready bool, skipped bool) {
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
		if dagEdgeSatisfied(edge.Condition, predStatus) {
			anySatisfied = true
		}
	}
	if allPredicatesResolved && !anySatisfied {
		return false, true
	}
	return allPredicatesResolved && anySatisfied, false
}

func dagEdgeSatisfied(condition, status string) bool {
	switch strings.TrimSpace(condition) {
	case "failure":
		return status == "failed"
	case "always":
		return status == "completed" || status == "failed" || status == "skipped"
	default:
		return status == "completed"
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
	return updated
}

func (s *Service) queueDepth() int {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()
	if s.runnerQueue == nil {
		return 0
	}
	return len(s.runnerQueue)
}

func (s *Service) logWarn(message string, fields ...zap.Field) {
	if s.logger != nil {
		s.logger.Warn(message, fields...)
	}
}

func (s *Service) logDebug(message string, fields ...zap.Field) {
	if s.logger != nil {
		s.logger.Debug(message, fields...)
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
		target := matchBindingTarget(binding, input)
		containerName := configString(node.Config, "containerName")
		if containerName == "" && target != nil {
			containerName = target.ContainerName
		}
		actionKind := strings.TrimSpace(binding.ReleasePolicy.ActionKind)
		if actionKind == "" {
			actionKind = configString(node.Config, "actionKind")
		}
		if actionKind == "" {
			actionKind = "deploy"
		}
		resolvedImage := strings.TrimSpace(fmt.Sprint(artifactState["image"]))
		imageTag := configString(node.Config, "imageTag")
		imageTagSource := configString(node.Config, "imageTagSource")
		if imageTagSource == "build_artifact" && resolvedImage == "" {
			status = "failed"
			summary = "build artifact image is not available"
			break
		}
		record, err := s.releases.Trigger(ctx, principal, domainrelease.TriggerInput{
			ApplicationID:            app.ID,
			ApplicationEnvironmentID: binding.ID,
			ClusterID:                input.ClusterID,
			Namespace:                input.Namespace,
			DeploymentName:           input.DeploymentName,
			ContainerName:            firstNonEmpty(input.ContainerName, containerName),
			Image:                    resolvedImage,
			ImageTag:                 firstNonEmpty(input.ImageTag, imageTag),
			ReleaseName:              input.ReleaseName,
			ActionKind:               actionKind,
			WorkflowRunID:            run.ID,
		})
		if err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = fmt.Sprintf("release %s finished with status %s", record.ID, record.Status)
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
		waitSummary, err := s.waitForRollout(ctx, principal, input.ClusterID, input.Namespace, input.DeploymentName, timeoutSeconds)
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
		result, err := s.rollbackToPreviousRevision(ctx, principal, input.ClusterID, input.Namespace, input.DeploymentName)
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
		deploymentName := firstNonEmpty(strings.TrimSpace(fmt.Sprint(node.Config["deploymentName"])), strings.TrimSpace(input.DeploymentName))
		if deploymentName == "" {
			status = "failed"
			summary = "restart workload requires deploymentName"
			break
		}
		if err := s.resources.RestartDeployment(ctx, principal, input.ClusterID, input.Namespace, deploymentName); err != nil {
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
		deploymentName := firstNonEmpty(strings.TrimSpace(fmt.Sprint(node.Config["deploymentName"])), strings.TrimSpace(input.DeploymentName))
		if deploymentName == "" {
			status = "failed"
			summary = "scale workload requires deploymentName"
			break
		}
		replicas := int32(toInt(node.Config["replicas"], 1))
		if err := s.resources.ScaleDeployment(ctx, principal, input.ClusterID, input.Namespace, deploymentName, replicas); err != nil {
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
		if err := s.resources.DeletePod(ctx, principal, input.ClusterID, input.Namespace, podName); err != nil {
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

	return dagExecutionResult{
		nodeID:  node.ID,
		status:  status,
		summary: summary,
		outputs: outputs,
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

func definitionFromRunMetadata(run domainworkflow.Run) (dagWorkflowDefinition, bool) {
	if len(run.Metadata) == 0 {
		return dagWorkflowDefinition{}, false
	}
	if nodes, ok := run.Metadata["nodes"].([]dagWorkflowNode); ok && len(nodes) > 0 {
		definition := dagWorkflowDefinition{
			SchemaVersion: toInt(run.Metadata["schemaVersion"], 2),
			Mode:          "release_dag",
			Nodes:         nodes,
		}
		if edges, edgeOK := run.Metadata["edges"].([]dagWorkflowEdge); edgeOK {
			definition.Edges = edges
		}
		return definition, true
	}
	return parseDAGWorkflowDefinition(map[string]any{
		"mode":          "release_dag",
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

func toConfigMap(value any) map[string]any {
	if valueMap, ok := value.(map[string]any); ok {
		return valueMap
	}
	return map[string]any{}
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
