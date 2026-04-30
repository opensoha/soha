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
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainbuild "github.com/kubecrux/kubecrux/internal/domain/build"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainrelease "github.com/kubecrux/kubecrux/internal/domain/release"
	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
	domainworkflow "github.com/kubecrux/kubecrux/internal/domain/workflow"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"github.com/kubecrux/kubecrux/internal/platform/requestctx"
	"github.com/kubecrux/kubecrux/internal/platform/runtimeobs"
	apprepo "github.com/kubecrux/kubecrux/internal/repository/application"
	"go.uber.org/zap"
)

const (
	defaultAsyncWorkflowWorkers   = 4
	defaultAsyncWorkflowQueueSize = 64
	defaultDAGNodeConcurrency     = 4
)

type Repository interface {
	List(context.Context, string, int) ([]domainworkflow.Run, error)
	Create(context.Context, domainworkflow.Run) (domainworkflow.Run, error)
	Update(context.Context, domainworkflow.Run) (domainworkflow.Run, error)
}

type ApplicationReader interface {
	Get(context.Context, string) (domainapp.App, error)
}

type CatalogReader interface {
	ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error)
}

type BuildExecutor interface {
	Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error)
}

type ReleaseExecutor interface {
	Trigger(context.Context, domainidentity.Principal, domainrelease.TriggerInput) (domainrelease.Record, error)
}

type ResourceExecutor interface {
	GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error)
	ListDeploymentRolloutHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.RolloutHistoryView, error)
	RollbackDeployment(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.DeploymentRollbackView, error)
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
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
	item := domainworkflow.Run{
		ID:             "workflow:" + uuid.NewString(),
		ApplicationID:  input.ApplicationID,
		WorkflowName:   strings.TrimSpace(input.WorkflowName),
		ClusterID:      strings.TrimSpace(input.ClusterID),
		Namespace:      strings.TrimSpace(input.Namespace),
		DeploymentName: strings.TrimSpace(input.DeploymentName),
		Status:         "running",
		Steps:          steps,
		Metadata: map[string]any{
			"applicationName": app.Name,
			"triggerBuild":    input.TriggerBuild,
			"triggerRelease":  input.TriggerRelease,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if item.WorkflowName == "" {
		item.WorkflowName = "build-release-verify"
	}
	return s.repo.Create(ctx, item)
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
		Metadata: map[string]any{
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
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	run.Metadata = withNodeRunsMetadata(run.Metadata, definition, nodeRuns)
	return run, binding, definition, true, nil
}

func (s *Service) runDAGAsync(ctx context.Context, principal domainidentity.Principal, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, definition dagWorkflowDefinition, run domainworkflow.Run) {
	startedAt := time.Now()
	if s.metrics != nil {
		s.metrics.RecordStart(runtimeobs.ComponentWorkflowRunner, run.ID, s.queueDepth(), len(definition.Nodes))
	}
	s.logDebug("workflow execution started", zap.String("runID", run.ID), zap.String("applicationID", run.ApplicationID))
	nodeRuns := initializeNodeRuns(definition)
	statuses := make(map[string]string, len(definition.Nodes))

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

		for _, result := range s.executeReadyDAGNodes(ctx, principal, app, input, binding, ready) {
			entry := nodeRuns[result.nodeID]
			entry.Status = result.status
			entry.Summary = result.summary
			entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			nodeRuns[result.nodeID] = entry
			statuses[result.nodeID] = result.status
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
				target.WorkloadName == strings.TrimSpace(input.DeploymentName) &&
				strings.EqualFold(target.WorkloadKind, "deployment") {
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

		for _, result := range s.executeReadyDAGNodes(ctx, principal, app, input, binding, ready) {
			statuses[result.nodeID] = result.status
			steps = append(steps, result.step)
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
	return steps, metadata, finalStatus, nil
}

func (s *Service) executeReadyDAGNodes(
	ctx context.Context,
	principal domainidentity.Principal,
	app domainapp.App,
	input domainworkflow.Input,
	binding domaincatalog.ApplicationEnvironment,
	ready []dagWorkflowNode,
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
				results <- s.executeDAGNode(ctx, principal, app, input, binding, node)
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
		if predStatus == "" {
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
) dagExecutionResult {
	status := "completed"
	summary := ""
	switch node.Type {
	case "manual_approval":
		summary = fmt.Sprintf("approved by trigger user %s", principal.UserName)
	case "deploy_update_image", "release":
		if s.releases == nil {
			status = "failed"
			summary = "release executor is not configured"
			break
		}
		target := matchBindingTarget(binding, input)
		containerName := strings.TrimSpace(fmt.Sprint(node.Config["containerName"]))
		if containerName == "" && target != nil {
			containerName = target.ContainerName
		}
		record, err := s.releases.Trigger(ctx, principal, domainrelease.TriggerInput{
			ApplicationID:  app.ID,
			ClusterID:      input.ClusterID,
			Namespace:      input.Namespace,
			DeploymentName: input.DeploymentName,
			ContainerName:  containerName,
		})
		if err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = fmt.Sprintf("release %s finished with status %s", record.ID, record.Status)
	case "build":
		if s.builds == nil {
			status = "failed"
			summary = "build executor is not configured"
			break
		}
		refName := app.DefaultBranch
		if refName == "" {
			refName = "main"
		}
		record, err := s.builds.Trigger(ctx, principal, domainbuild.TriggerInput{
			ApplicationID: app.ID,
			RefType:       "branch",
			RefName:       refName,
			ImageTag:      app.DefaultTag,
		})
		if err != nil {
			status = "failed"
			summary = err.Error()
			break
		}
		summary = fmt.Sprintf("build %s queued", record.ID)
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
		checkURL := strings.TrimSpace(fmt.Sprint(node.Config["url"]))
		if checkURL == "" {
			checkURL = strings.TrimSpace(fmt.Sprint(node.Config["endpoint"]))
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
		if target.ClusterID == strings.TrimSpace(input.ClusterID) &&
			target.Namespace == strings.TrimSpace(input.Namespace) &&
			target.WorkloadName == strings.TrimSpace(input.DeploymentName) &&
			strings.EqualFold(target.WorkloadKind, "deployment") {
			copyTarget := target
			return &copyTarget
		}
	}
	return nil
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
			BusinessLineID: app.BusinessLineID,
			ApplicationID:  app.ID,
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
