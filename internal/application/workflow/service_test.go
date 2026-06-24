package workflow

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apprepo "github.com/opensoha/soha/internal/repository/application"
)

type stubWorkflowRepository struct {
	mu          sync.Mutex
	items       []domainworkflow.Run
	deletedIDs  []string
	createCalls int
	updated     []domainworkflow.Run
	approvals   []domainworkflow.Approval
}

func (r *stubWorkflowRepository) List(context.Context, string, int) ([]domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneWorkflowRuns(r.items), nil
}

func (r *stubWorkflowRepository) Create(_ context.Context, item domainworkflow.Run) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	return cloneWorkflowRun(item), nil
}

func (r *stubWorkflowRepository) Get(_ context.Context, runID string) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		if item.ID == runID {
			return cloneWorkflowRun(item), nil
		}
	}
	return domainworkflow.Run{}, errors.New("workflow run not found")
}

func (r *stubWorkflowRepository) Update(_ context.Context, item domainworkflow.Run) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cloned := cloneWorkflowRun(item)
	r.updated = append(r.updated, cloned)
	return cloneWorkflowRun(cloned), nil
}

func (r *stubWorkflowRepository) CreateApproval(_ context.Context, item domainworkflow.Approval) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.approvals = append(r.approvals, cloneWorkflowApproval(item))
	return nil
}

func (r *stubWorkflowRepository) DeleteByIDs(_ context.Context, ids []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deletedIDs = append(r.deletedIDs, ids...)
	return nil
}

func (r *stubWorkflowRepository) setItems(items []domainworkflow.Run) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = cloneWorkflowRuns(items)
}

func (r *stubWorkflowRepository) createCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createCalls
}

func (r *stubWorkflowRepository) deletedIDsSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.deletedIDs...)
}

func (r *stubWorkflowRepository) approvalsSnapshot() []domainworkflow.Approval {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domainworkflow.Approval, 0, len(r.approvals))
	for _, item := range r.approvals {
		items = append(items, cloneWorkflowApproval(item))
	}
	return items
}

func cloneWorkflowRuns(items []domainworkflow.Run) []domainworkflow.Run {
	out := make([]domainworkflow.Run, 0, len(items))
	for _, item := range items {
		out = append(out, cloneWorkflowRun(item))
	}
	return out
}

func cloneWorkflowRun(item domainworkflow.Run) domainworkflow.Run {
	item.Steps = append([]domainworkflow.Step(nil), item.Steps...)
	item.NodeRuns = append([]domainworkflow.NodeRun(nil), item.NodeRuns...)
	item.Metadata = cloneWorkflowMetadata(item.Metadata)
	return item
}

func cloneWorkflowApproval(item domainworkflow.Approval) domainworkflow.Approval {
	item.Metadata = cloneWorkflowMetadata(item.Metadata)
	return item
}

func cloneWorkflowMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = cloneWorkflowMetadataValue(value)
	}
	return out
}

func cloneWorkflowMetadataValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneWorkflowMetadata(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneWorkflowMetadata(item))
		}
		return out
	case []domainworkflow.NodeRun:
		return append([]domainworkflow.NodeRun(nil), typed...)
	case []domainworkflow.Step:
		return append([]domainworkflow.Step(nil), typed...)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneWorkflowMetadataValue(item))
		}
		return out
	default:
		return value
	}
}

type stubWorkflowApps struct {
	missing map[string]bool
}

func (a *stubWorkflowApps) Get(_ context.Context, applicationID string) (domainapp.App, error) {
	if a.missing[applicationID] {
		return domainapp.App{}, apprepo.ErrNotFound
	}
	return domainapp.App{ID: applicationID, Name: "ok", DefaultTag: "latest"}, nil
}

func (a *stubWorkflowApps) ListServices(context.Context, string) ([]domainapp.Service, error) {
	return []domainapp.Service{
		{ID: "svc-1", ApplicationID: "app-1", Key: "checkout", Name: "Checkout", ServiceKind: domainapp.ServiceKindKubernetesWorkload, Enabled: true, Metadata: map[string]any{"tier": "api"}},
	}, nil
}

type stubWorkflowCatalog struct {
	items []domaincatalog.ApplicationEnvironment
}

func (s *stubWorkflowCatalog) ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	return s.items, nil
}

type stubWorkflowReleaseExecutor struct{}

func (stubWorkflowReleaseExecutor) Trigger(_ context.Context, _ domainidentity.Principal, input domainrelease.TriggerInput) (domainrelease.Record, error) {
	return domainrelease.Record{ID: "release-1", Status: "deployed", ApplicationID: input.ApplicationID}, nil
}

type countingWorkflowReleaseExecutor struct {
	count *atomic.Int64
}

func (s countingWorkflowReleaseExecutor) Trigger(_ context.Context, _ domainidentity.Principal, input domainrelease.TriggerInput) (domainrelease.Record, error) {
	if s.count != nil {
		s.count.Add(1)
	}
	return domainrelease.Record{ID: "release-1", Status: "deployed", ApplicationID: input.ApplicationID}, nil
}

type recordingWorkflowReleaseExecutor struct {
	mu     sync.Mutex
	inputs []domainrelease.TriggerInput
}

func (s *recordingWorkflowReleaseExecutor) Trigger(_ context.Context, _ domainidentity.Principal, input domainrelease.TriggerInput) (domainrelease.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, input)
	return domainrelease.Record{ID: "release-" + input.DeploymentName, Status: "deployed", ApplicationID: input.ApplicationID}, nil
}

func (s *recordingWorkflowReleaseExecutor) inputsSnapshot() []domainrelease.TriggerInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domainrelease.TriggerInput(nil), s.inputs...)
}

type stubWorkflowResourceExecutor struct{}

func (stubWorkflowResourceExecutor) GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error) {
	return domainresource.DeploymentRolloutStatusView{Status: "healthy", Message: "deployment is fully available"}, nil
}

func (stubWorkflowResourceExecutor) ListDeploymentRolloutHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.RolloutHistoryView, error) {
	return []domainresource.RolloutHistoryView{{Revision: "2"}, {Revision: "1"}}, nil
}

func (stubWorkflowResourceExecutor) RollbackDeployment(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.DeploymentRollbackView, error) {
	return domainresource.DeploymentRollbackView{Message: "rollback requested"}, nil
}

func (stubWorkflowResourceExecutor) ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error) {
	return nil, nil
}

func (stubWorkflowResourceExecutor) RestartDeployment(context.Context, domainidentity.Principal, string, string, string) error {
	return nil
}

func (stubWorkflowResourceExecutor) ScaleDeployment(context.Context, domainidentity.Principal, string, string, string, int32) error {
	return nil
}

func (stubWorkflowResourceExecutor) DeletePod(context.Context, domainidentity.Principal, string, string, string) error {
	return nil
}

type countingWorkflowResourceExecutor struct {
	rollbackCount *atomic.Int64
}

func (s countingWorkflowResourceExecutor) GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error) {
	return domainresource.DeploymentRolloutStatusView{Status: "healthy", Message: "deployment is fully available"}, nil
}

func (s countingWorkflowResourceExecutor) ListDeploymentRolloutHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.RolloutHistoryView, error) {
	return []domainresource.RolloutHistoryView{{Revision: "2"}, {Revision: "1"}}, nil
}

func (s countingWorkflowResourceExecutor) RollbackDeployment(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.DeploymentRollbackView, error) {
	if s.rollbackCount != nil {
		s.rollbackCount.Add(1)
	}
	return domainresource.DeploymentRollbackView{Message: "rollback requested"}, nil
}

func (s countingWorkflowResourceExecutor) ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error) {
	return nil, nil
}

func (s countingWorkflowResourceExecutor) RestartDeployment(context.Context, domainidentity.Principal, string, string, string) error {
	return nil
}

func (s countingWorkflowResourceExecutor) ScaleDeployment(context.Context, domainidentity.Principal, string, string, string, int32) error {
	return nil
}

func (s countingWorkflowResourceExecutor) DeletePod(context.Context, domainidentity.Principal, string, string, string) error {
	return nil
}

type stubWorkflowBuildExecutor struct{}

func (stubWorkflowBuildExecutor) Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	return domainbuild.Record{ID: "build-1", Status: "queued"}, nil
}

func (stubWorkflowBuildExecutor) Execute(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	return domainbuild.Record{ID: "build-1", Status: "completed", Metadata: map[string]any{"image": "repo/demo:latest", "artifact": map[string]any{"ref": "repo/demo:latest"}}}, nil
}

type countingWorkflowBuildExecutor struct {
	count *atomic.Int64
}

func (s countingWorkflowBuildExecutor) Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	if s.count != nil {
		s.count.Add(1)
	}
	return domainbuild.Record{ID: "build-1", Status: "queued"}, nil
}

func (s countingWorkflowBuildExecutor) Execute(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	if s.count != nil {
		s.count.Add(1)
	}
	return domainbuild.Record{ID: "build-1", Status: "completed", Metadata: map[string]any{"image": "repo/demo:latest", "artifact": map[string]any{"ref": "repo/demo:latest"}}}, nil
}

type stubWorkflowBuildExecutorWithoutArtifacts struct{}

func (stubWorkflowBuildExecutorWithoutArtifacts) Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	return domainbuild.Record{ID: "build-1", Status: "queued"}, nil
}

func (stubWorkflowBuildExecutorWithoutArtifacts) Execute(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	return domainbuild.Record{ID: "build-1", Status: "completed", Metadata: map[string]any{}}, nil
}

type recordingWorkflowArtifactStore struct {
	mu    sync.Mutex
	items []domaindelivery.ExecutionArtifact
}

func (s *recordingWorkflowArtifactStore) UpsertExecutionArtifact(_ context.Context, item domaindelivery.ExecutionArtifact) (domaindelivery.ExecutionArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, item)
	return item, nil
}

func (s *recordingWorkflowArtifactStore) itemsSnapshot() []domaindelivery.ExecutionArtifact {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domaindelivery.ExecutionArtifact(nil), s.items...)
}

type stubWorkflowRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubWorkflowRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

func waitForWorkflowStatus(t *testing.T, service *Service, runID string, status string) domainworkflow.Run {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	run, err := service.waitForRunStatus(waitCtx, runID, status)
	if err != nil {
		t.Fatalf("waitForRunStatus(%q) error = %v", status, err)
	}
	if run.Status != status {
		t.Fatalf("waitForRunStatus(%q) = %q", status, run.Status)
	}
	return run
}

func TestListPrunesStaleApplications(t *testing.T) {
	repo := &stubWorkflowRepository{
		items: []domainworkflow.Run{
			{ID: "keep", ApplicationID: "app-ok"},
			{ID: "stale-missing-app", ApplicationID: "app-missing"},
			{ID: "stale-empty-app", ApplicationID: ""},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{missing: map[string]bool{"app-missing": true}},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsView}}}),
	}

	items, err := service.List(context.Background(), domainidentity.Principal{Roles: []string{"developer"}}, "", 50)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "keep" {
		t.Fatalf("List() items = %+v, want only keep", items)
	}

	deletedIDs := repo.deletedIDsSnapshot()
	sort.Strings(deletedIDs)
	expected := []string{"stale-empty-app", "stale-missing-app"}
	sort.Strings(expected)
	if len(deletedIDs) != len(expected) {
		t.Fatalf("deletedIDs len = %d, want %d (%v)", len(deletedIDs), len(expected), deletedIDs)
	}
	for i := range expected {
		if deletedIDs[i] != expected[i] {
			t.Fatalf("deletedIDs = %v, want %v", deletedIDs, expected)
		}
	}
}

func TestTriggerReturnsNotFoundWhenApplicationMissing(t *testing.T) {
	repo := &stubWorkflowRepository{}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{missing: map[string]bool{"missing-app": true}},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}

	_, err := service.Trigger(context.Background(), domainidentity.Principal{Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID: "missing-app",
		WorkflowName:  "build-release-verify",
		ClusterID:     "cluster-ok",
		Namespace:     "default",
	})
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Trigger() error = %v, want ErrNotFound", err)
	}
	if got := repo.createCallCount(); got != 0 {
		t.Fatalf("Create() called %d times, want 0", got)
	}
}

func TestTriggerRequiresWorkflowTriggerPermission(t *testing.T) {
	repo := &stubWorkflowRepository{}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"readonly": {}}}),
	}

	_, err := service.Trigger(context.Background(), domainidentity.Principal{Roles: []string{"readonly"}}, domainworkflow.Input{
		ApplicationID: "app-1",
		WorkflowName:  "build-release-verify",
		ClusterID:     "cluster-ok",
		Namespace:     "default",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Trigger() error = %v, want ErrAccessDenied", err)
	}
	if got := repo.createCallCount(); got != 0 {
		t.Fatalf("Create() called %d times, want 0", got)
	}
}

func TestTriggerExecutesDAGWorkflowTemplate(t *testing.T) {
	repo := &stubWorkflowRepository{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-1",
		Key:  "release-dag",
		Name: "release-dag",
		Definition: map[string]any{
			"mode": "release_dag",
			"nodes": []map[string]any{
				{"id": "approval", "name": "审批", "type": "manual_approval"},
				{"id": "deploy", "name": "发布", "type": "deploy_update_image"},
				{"id": "rollout", "name": "等待", "type": "wait_rollout"},
			},
			"edges": []map[string]any{
				{"id": "e1", "source": "approval", "target": "deploy", "condition": "success"},
				{"id": "e2", "source": "deploy", "target": "rollout", "condition": "success"},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		WorkflowTemplateID: "wf-1",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		releases:    stubWorkflowReleaseExecutor{},
		resources:   stubWorkflowResourceExecutor{},
		builds:      stubWorkflowBuildExecutor{},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}
	runnerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(runnerCtx)
	defer func() {
		_ = service.Shutdown(context.Background())
	}()

	run, err := service.Trigger(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:  "app-1",
		WorkflowName:   "release-dag",
		ClusterID:      "cluster-1",
		Namespace:      "default",
		DeploymentName: "demo",
		Variables: map[string]any{
			"aiGatewayApprovalRequestId": "approval-1",
			"aiGatewayApprovalPolicyRef": "policy-standard",
			"aiGatewayToolName":          "delivery.actions.trigger",
		},
	})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if run.Status != "queued" {
		t.Fatalf("run.Status = %q, want queued", run.Status)
	}
	if len(run.Steps) != 3 {
		t.Fatalf("run.Steps = %+v, want 3 queued steps", run.Steps)
	}
	waitingRun := waitForWorkflowStatus(t, service, run.ID, workflowStatusWaitingApproval)
	if waitingRun.Metadata["aiGatewayApprovalRequestId"] != "approval-1" {
		t.Fatalf("expected workflow run metadata to keep gateway approval id, got %#v", waitingRun.Metadata)
	}
	repo.setItems([]domainworkflow.Run{waitingRun})
	if _, err := service.Approve(context.Background(), domainidentity.Principal{UserID: "u-1", UserName: "approver", Roles: []string{"developer"}}, run.ID, "approved"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	approvals := repo.approvalsSnapshot()
	if len(approvals) != 1 {
		t.Fatalf("approvals len = %d, want 1", len(approvals))
	}
	if approvals[0].Metadata["aiGatewayApprovalRequestId"] != "approval-1" || approvals[0].Metadata["aiGatewayToolName"] != "delivery.actions.trigger" {
		t.Fatalf("expected workflow approval metadata to keep gateway linkage, got %#v", approvals[0].Metadata)
	}
	completedRun := waitForWorkflowStatus(t, service, run.ID, "completed")
	if completedRun.Status != "completed" {
		t.Fatalf("approved workflow final status = %q, want completed", completedRun.Status)
	}
}

func TestTriggerAcceptsDeliveryDAGWorkflowTemplate(t *testing.T) {
	repo := &stubWorkflowRepository{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-delivery",
		Key:  "delivery-dag",
		Name: "delivery-dag",
		Definition: map[string]any{
			"mode": "delivery_dag",
			"nodes": []map[string]any{
				{
					"id":              "build",
					"name":            "Build",
					"type":            "build",
					"inputs":          []any{"source"},
					"outputs":         []any{"image"},
					"serviceSelector": map[string]any{"matchLabels": map[string]any{"service": "checkout"}},
					"artifactOutputs": []any{map[string]any{"name": "image", "kind": "image", "required": true}},
					"runCondition":    "branch == main",
					"failurePolicy":   "rollback",
					"observability":   map[string]any{"events": []any{"started", "completed"}},
				},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		WorkflowTemplateID: "wf-delivery",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		builds:      stubWorkflowBuildExecutor{},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}
	runnerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(runnerCtx)
	defer func() {
		_ = service.Shutdown(context.Background())
	}()

	run, err := service.Trigger(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:  "app-1",
		WorkflowName:   "delivery-dag",
		ClusterID:      "cluster-1",
		Namespace:      "default",
		DeploymentName: "demo",
	})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if run.Metadata["mode"] != "delivery_dag" {
		t.Fatalf("run mode = %v, want delivery_dag", run.Metadata["mode"])
	}
	if run.Metadata["executionMode"] != "release_dag" {
		t.Fatalf("executionMode = %v, want release_dag", run.Metadata["executionMode"])
	}
	nodes, ok := run.Metadata["nodes"].([]dagWorkflowNode)
	if !ok || len(nodes) != 1 {
		t.Fatalf("metadata nodes = %#v, want one parsed node", run.Metadata["nodes"])
	}
	if len(nodes[0].Inputs) != 1 || nodes[0].Inputs[0] != "source" {
		t.Fatalf("node inputs = %#v, want source", nodes[0].Inputs)
	}
	if len(nodes[0].ArtifactOutputs) != 1 || nodes[0].ArtifactOutputs[0]["kind"] != "image" {
		t.Fatalf("node artifact outputs = %#v, want image", nodes[0].ArtifactOutputs)
	}
	if nodes[0].RunCondition != "branch == main" || nodes[0].FailurePolicy != "rollback" {
		t.Fatalf("node condition/policy = %q/%q", nodes[0].RunCondition, nodes[0].FailurePolicy)
	}
}

func TestTriggerRejectsDeliveryDAGInputReferenceFromNonUpstreamNode(t *testing.T) {
	repo := &stubWorkflowRepository{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-delivery",
		Key:  "delivery-dag",
		Name: "delivery-dag",
		Definition: map[string]any{
			"mode": "delivery_dag",
			"nodes": []map[string]any{
				{"id": "build", "name": "Build", "type": "build", "inputs": []any{"deploy.image"}},
				{"id": "deploy", "name": "Deploy", "type": "deploy_update_image", "outputs": []any{"image"}},
			},
			"edges": []map[string]any{
				{"id": "e1", "source": "build", "target": "deploy", "condition": "success"},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		WorkflowTemplateID: "wf-delivery",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		builds:      stubWorkflowBuildExecutor{},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}

	_, err := service.Trigger(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:  "app-1",
		WorkflowName:   "delivery-dag",
		ClusterID:      "cluster-1",
		Namespace:      "default",
		DeploymentName: "demo",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Trigger() error = %v, want ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "must come from an upstream node") {
		t.Fatalf("Trigger() error = %v, want upstream-node validation", err)
	}
	if got := repo.createCallCount(); got != 0 {
		t.Fatalf("Create() called %d times, want 0", got)
	}
}

func TestTriggerExecutesDeliveryDAGNativeMetadata(t *testing.T) {
	repo := &stubWorkflowRepository{}
	artifactStore := &recordingWorkflowArtifactStore{}
	releases := &recordingWorkflowReleaseExecutor{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-delivery",
		Key:  "delivery-dag",
		Name: "delivery-dag",
		Definition: map[string]any{
			"mode": "delivery_dag",
			"nodes": []map[string]any{
				{
					"id":                  "build",
					"name":                "Build",
					"type":                "build",
					"inputs":              []any{"source"},
					"outputs":             []any{"image"},
					"serviceSelector":     map[string]any{"matchLabels": map[string]any{"service": "checkout"}},
					"environmentSelector": map[string]any{"environmentKey": "staging"},
					"targetSelector":      map[string]any{"clusterId": "cluster-1", "namespace": "default", "workloadName": "demo"},
					"artifactOutputs":     []any{map[string]any{"name": "image", "kind": "image", "required": true}},
					"runCondition":        "branch == main",
				},
				{
					"id":              "deploy",
					"name":            "Deploy",
					"type":            "deploy_update_image",
					"inputs":          []any{"build.image"},
					"targetSelector":  map[string]any{"clusterId": "cluster-1", "namespace": "default", "workloadName": "demo"},
					"artifactOutputs": []any{map[string]any{"name": "sbom", "kind": "sbom"}},
				},
			},
			"edges": []map[string]any{
				{"id": "e1", "source": "build", "target": "deploy", "condition": "success"},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		EnvironmentID:      "env-staging",
		EnvironmentKey:     "staging",
		WorkflowTemplateID: "wf-delivery",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ID: "target-1", ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", ContainerName: "app", Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		builds:      stubWorkflowBuildExecutor{},
		releases:    releases,
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}
	service.SetArtifactStore(artifactStore)
	service.SetRuntimeOptions(1, 8, 1)
	runnerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(runnerCtx)
	defer func() {
		_ = service.Shutdown(context.Background())
	}()

	run, err := service.Trigger(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:  "app-1",
		WorkflowName:   "delivery-dag",
		ClusterID:      "cluster-1",
		Namespace:      "default",
		DeploymentName: "demo",
		RefName:        "main",
	})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	finalRun := waitForWorkflowStatus(t, service, run.ID, "completed")
	artifacts := finalRun.Metadata["artifacts"].(map[string]any)
	imageArtifact, ok := artifacts["image"].(map[string]any)
	if !ok {
		t.Fatalf("artifacts = %#v, want image artifact", artifacts)
	}
	if imageArtifact["id"] == nil || imageArtifact["workflowRunId"] == nil || imageArtifact["workflowNodeId"] == nil {
		t.Fatalf("image artifact metadata = %#v, want persisted artifact reference", imageArtifact)
	}
	if _, ok := imageArtifact["value"]; ok {
		t.Fatalf("image artifact metadata = %#v, want lightweight reference without value", imageArtifact)
	}
	storedArtifacts := artifactStore.itemsSnapshot()
	if len(storedArtifacts) != 1 || storedArtifacts[0].Metadata["value"] == nil {
		t.Fatalf("stored artifacts = %#v, want independently stored artifact value", storedArtifacts)
	}
	releaseInputs := releases.inputsSnapshot()
	if len(releaseInputs) != 1 || releaseInputs[0].Image != "repo/demo:latest" {
		t.Fatalf("release inputs = %+v, want image ref from artifact state", releaseInputs)
	}
	nodeOutputs := finalRun.Metadata["nodeOutputs"].(map[string]any)
	buildOutput := nodeOutputs["build"].(map[string]any)
	if buildOutput["inputs"] == nil || buildOutput["selectors"] == nil || buildOutput["artifacts"] == nil {
		t.Fatalf("build node metadata = %#v, want inputs/selectors/artifacts", buildOutput)
	}
	buildArtifacts := buildOutput["artifacts"].(map[string]any)
	buildImageArtifact := buildArtifacts["image"].(map[string]any)
	if buildImageArtifact["id"] == nil {
		t.Fatalf("build node artifacts = %#v, want persisted image reference", buildArtifacts)
	}
	if _, ok := buildImageArtifact["value"]; ok {
		t.Fatalf("build node image artifact = %#v, want no raw value", buildImageArtifact)
	}
	deployOutput := nodeOutputs["deploy"].(map[string]any)
	deployInputs := deployOutput["inputs"].(map[string]any)
	deployImageInput := deployInputs["build.image"].(map[string]any)
	if deployImageInput["ref"] != "repo/demo:latest" {
		t.Fatalf("deploy inputs = %#v, want build.image ref", deployInputs)
	}
	if _, ok := deployImageInput["value"]; ok {
		t.Fatalf("deploy image input = %#v, want no raw value", deployImageInput)
	}
	events := finalRun.Metadata["events"].([]map[string]any)
	if len(events) == 0 {
		t.Fatalf("events empty, metadata = %#v", finalRun.Metadata)
	}
	hasSelectorEvent := false
	hasArtifactEvent := false
	hasInputEvent := false
	for _, event := range events {
		switch event["type"] {
		case "node_selectors_resolved":
			hasSelectorEvent = true
		case "node_inputs_resolved":
			if event["nodeId"] == "deploy" {
				hasInputEvent = true
				eventInputs := event["inputs"].(map[string]any)
				eventImageInput := eventInputs["build.image"].(map[string]any)
				if _, ok := eventImageInput["value"]; ok {
					t.Fatalf("input event = %#v, want no raw value", event)
				}
			}
		case "artifact_outputs_recorded":
			hasArtifactEvent = true
			eventArtifacts := event["artifacts"].(map[string]any)
			eventImageArtifact := eventArtifacts["image"].(map[string]any)
			if _, ok := eventImageArtifact["value"]; ok {
				t.Fatalf("artifact event = %#v, want no raw value", event)
			}
		}
	}
	if !hasSelectorEvent || !hasArtifactEvent || !hasInputEvent {
		t.Fatalf("events = %#v, want selector, input, and artifact events", events)
	}
}

func TestTriggerDeliveryDAGRequiredArtifactOutputMissingFailsNodeAndWorkflow(t *testing.T) {
	repo := &stubWorkflowRepository{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-delivery",
		Key:  "delivery-dag",
		Name: "delivery-dag",
		Definition: map[string]any{
			"mode": "delivery_dag",
			"nodes": []map[string]any{
				{
					"id":              "build",
					"name":            "Build",
					"type":            "build",
					"artifactOutputs": []any{map[string]any{"name": "image", "kind": "image", "required": true}},
				},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		WorkflowTemplateID: "wf-delivery",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		builds:      stubWorkflowBuildExecutorWithoutArtifacts{},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}
	service.SetRuntimeOptions(1, 8, 1)
	runnerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(runnerCtx)
	defer func() {
		_ = service.Shutdown(context.Background())
	}()

	run, err := service.Trigger(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:  "app-1",
		WorkflowName:   "delivery-dag",
		ClusterID:      "cluster-1",
		Namespace:      "default",
		DeploymentName: "demo",
	})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	finalRun := waitForWorkflowStatus(t, service, run.ID, "failed")
	if len(finalRun.NodeRuns) != 1 || finalRun.NodeRuns[0].Status != "failed" {
		t.Fatalf("NodeRuns = %+v, want build failed", finalRun.NodeRuns)
	}
	if finalRun.Status != "failed" {
		t.Fatalf("final status = %q, want failed", finalRun.Status)
	}
	if _, ok := finalRun.Metadata["artifacts"].(map[string]any); ok {
		t.Fatalf("artifacts metadata = %#v, want missing or empty because required artifact was not produced", finalRun.Metadata["artifacts"])
	}
}

func TestTriggerDeliveryDAGRunConditionSkipsNode(t *testing.T) {
	repo := &stubWorkflowRepository{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-delivery",
		Key:  "delivery-dag",
		Name: "delivery-dag",
		Definition: map[string]any{
			"mode": "delivery_dag",
			"nodes": []map[string]any{
				{"id": "build", "name": "Build", "type": "build", "runCondition": "branch == main"},
				{"id": "notify", "name": "Notify", "type": "notify", "config": map[string]any{"channel": "ops"}},
			},
			"edges": []map[string]any{
				{"id": "e1", "source": "build", "target": "notify", "condition": "always"},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		EnvironmentKey:     "staging",
		WorkflowTemplateID: "wf-delivery",
		WorkflowTemplate:   template,
		Targets:            []domaincatalog.ReleaseTarget{{ClusterID: "cluster-1", Namespace: "default", WorkloadName: "demo", Enabled: true}},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		builds:      stubWorkflowBuildExecutor{},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}
	service.SetRuntimeOptions(1, 8, 1)
	runnerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(runnerCtx)
	defer func() {
		_ = service.Shutdown(context.Background())
	}()

	run, err := service.Trigger(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:  "app-1",
		WorkflowName:   "delivery-dag",
		ClusterID:      "cluster-1",
		Namespace:      "default",
		DeploymentName: "demo",
		RefName:        "feature/demo",
	})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	finalRun := waitForWorkflowStatus(t, service, run.ID, "completed")
	if finalRun.NodeRuns[0].Status != "skipped" || finalRun.NodeRuns[1].Status != "completed" {
		t.Fatalf("node runs = %+v, want build skipped and notify completed", finalRun.NodeRuns)
	}
	nodeOutputs := finalRun.Metadata["nodeOutputs"].(map[string]any)
	condition := nodeOutputs["build"].(map[string]any)["runCondition"].(map[string]any)
	if condition["matched"] != false {
		t.Fatalf("runCondition metadata = %#v, want matched false", condition)
	}
}

func TestTriggerDeliveryDAGFailurePolicyAndFailureBranch(t *testing.T) {
	repo := &stubWorkflowRepository{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-delivery",
		Key:  "delivery-dag",
		Name: "delivery-dag",
		Definition: map[string]any{
			"mode": "delivery_dag",
			"nodes": []map[string]any{
				{"id": "build", "name": "Build", "type": "build", "failurePolicy": "rollback"},
				{"id": "deploy", "name": "Deploy", "type": "deploy_update_image"},
				{"id": "rollback", "name": "Rollback", "type": "rollback_to_previous"},
				{"id": "notify", "name": "Notify", "type": "notify", "config": map[string]any{"channel": "ops"}},
			},
			"edges": []map[string]any{
				{"id": "e1", "source": "build", "target": "deploy", "condition": "success"},
				{"id": "e2", "source": "build", "target": "rollback", "condition": "failure"},
				{"id": "e3", "source": "rollback", "target": "notify", "condition": "always"},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		EnvironmentKey:     "staging",
		WorkflowTemplateID: "wf-delivery",
		WorkflowTemplate:   template,
		Targets:            []domaincatalog.ReleaseTarget{{ClusterID: "cluster-1", Namespace: "default", WorkloadName: "demo", Enabled: true}},
	}
	var rollbackCount atomic.Int64
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		resources:   countingWorkflowResourceExecutor{rollbackCount: &rollbackCount},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}
	service.SetRuntimeOptions(1, 8, 1)
	runnerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(runnerCtx)
	defer func() {
		_ = service.Shutdown(context.Background())
	}()

	run, err := service.Trigger(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:  "app-1",
		WorkflowName:   "delivery-dag",
		ClusterID:      "cluster-1",
		Namespace:      "default",
		DeploymentName: "demo",
	})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	finalRun := waitForWorkflowStatus(t, service, run.ID, "failed")
	if got := rollbackCount.Load(); got != 1 {
		t.Fatalf("rollbackCount = %d, want 1", got)
	}
	statuses := map[string]string{}
	for _, nodeRun := range finalRun.NodeRuns {
		statuses[nodeRun.NodeID] = nodeRun.Status
	}
	if statuses["build"] != "failed" || statuses["deploy"] != "skipped" || statuses["rollback"] != "completed" || statuses["notify"] != "completed" {
		t.Fatalf("node statuses = %#v, want failed build, skipped deploy, completed rollback/notify", statuses)
	}
	policies := finalRun.Metadata["failurePolicies"].(map[string]any)
	if policies["build"] == nil {
		t.Fatalf("failurePolicies = %#v, want build policy", policies)
	}
}

func TestTriggerDeliveryDAGReleaseFanOutTargets(t *testing.T) {
	repo := &stubWorkflowRepository{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-delivery",
		Key:  "delivery-dag",
		Name: "delivery-dag",
		Definition: map[string]any{
			"mode": "delivery_dag",
			"nodes": []map[string]any{
				{
					"id":                  "deploy",
					"name":                "Deploy",
					"type":                "deploy_update_image",
					"environmentSelector": map[string]any{"environmentKey": "staging"},
					"targetSelector":      map[string]any{"matchLabels": map[string]any{"wave": "blue"}},
					"fanOut":              map[string]any{"strategy": "batch", "batchSize": 2, "failurePolicy": "continue"},
				},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		EnvironmentKey:     "staging",
		WorkflowTemplateID: "wf-delivery",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ID: "target-a", ClusterID: "cluster-1", Namespace: "blue-a", WorkloadKind: "Deployment", WorkloadName: "demo-a", Enabled: true, Metadata: map[string]any{"wave": "blue"}},
			{ID: "target-b", ClusterID: "cluster-1", Namespace: "blue-b", WorkloadKind: "Deployment", WorkloadName: "demo-b", Enabled: true, Metadata: map[string]any{"wave": "blue"}},
			{ID: "target-c", ClusterID: "cluster-1", Namespace: "green", WorkloadKind: "Deployment", WorkloadName: "demo-c", Enabled: true, Metadata: map[string]any{"wave": "green"}},
		},
	}
	releases := &recordingWorkflowReleaseExecutor{}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		releases:    releases,
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}
	service.SetRuntimeOptions(1, 8, 1)
	runnerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(runnerCtx)
	defer func() {
		_ = service.Shutdown(context.Background())
	}()

	run, err := service.Trigger(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:  "app-1",
		WorkflowName:   "delivery-dag",
		ClusterID:      "cluster-1",
		Namespace:      "blue-a",
		DeploymentName: "demo-a",
	})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	finalRun := waitForWorkflowStatus(t, service, run.ID, "completed")
	releaseInputs := releases.inputsSnapshot()
	if len(releaseInputs) != 2 {
		t.Fatalf("release triggers = %+v, want 2 fan-out targets", releaseInputs)
	}
	namespaces := []string{releaseInputs[0].Namespace, releaseInputs[1].Namespace}
	sort.Strings(namespaces)
	if strings.Join(namespaces, ",") != "blue-a,blue-b" {
		t.Fatalf("release namespaces = %#v, want blue-a and blue-b", namespaces)
	}
	nodeOutputs := finalRun.Metadata["nodeOutputs"].(map[string]any)
	deployOutput := nodeOutputs["deploy"].(map[string]any)
	fanOut := deployOutput["outputs"].(map[string]any)["fanOut"].(map[string]any)
	if fanOut["targetCount"] != 2 || fanOut["strategy"] != "batch" || fanOut["failurePolicy"] != "continue" {
		t.Fatalf("fanOut output = %#v, want batch fan-out with two targets and continue policy", fanOut)
	}
	events := finalRun.Metadata["events"].([]map[string]any)
	hasFanOutEvent := false
	for _, event := range events {
		if event["type"] == "node_fan_out_completed" {
			hasFanOutEvent = true
			break
		}
	}
	if !hasFanOutEvent {
		t.Fatalf("events = %#v, want node_fan_out_completed", events)
	}
}

func TestTriggerDeliveryDAGRejectsInvalidExecutionSemantics(t *testing.T) {
	tests := []struct {
		name       string
		definition map[string]any
	}{
		{
			name: "missing input",
			definition: map[string]any{
				"mode":  "delivery_dag",
				"nodes": []map[string]any{{"id": "build", "name": "Build", "type": "build", "inputs": []any{"missing.artifact"}}},
			},
		},
		{
			name: "invalid artifact kind",
			definition: map[string]any{
				"mode":  "delivery_dag",
				"nodes": []map[string]any{{"id": "build", "name": "Build", "type": "build", "artifactOutputs": []any{map[string]any{"name": "bundle", "kind": "tarball"}}}},
			},
		},
		{
			name: "unresolved selector",
			definition: map[string]any{
				"mode":  "delivery_dag",
				"nodes": []map[string]any{{"id": "build", "name": "Build", "type": "build", "targetSelector": map[string]any{"clusterId": "missing"}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubWorkflowRepository{}
			template := &domaincatalog.WorkflowTemplate{ID: "wf-delivery", Key: "delivery-dag", Name: "delivery-dag", Definition: tt.definition}
			binding := domaincatalog.ApplicationEnvironment{
				ID:                 "binding-1",
				ApplicationID:      "app-1",
				WorkflowTemplateID: "wf-delivery",
				WorkflowTemplate:   template,
				Targets:            []domaincatalog.ReleaseTarget{{ClusterID: "cluster-1", Namespace: "default", WorkloadName: "demo", Enabled: true}},
			}
			service := &Service{
				repo:        repo,
				apps:        &stubWorkflowApps{},
				catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
				builds:      stubWorkflowBuildExecutor{},
				permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
			}
			_, err := service.Trigger(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
				ApplicationID:  "app-1",
				WorkflowName:   "delivery-dag",
				ClusterID:      "cluster-1",
				Namespace:      "default",
				DeploymentName: "demo",
			})
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("Trigger() error = %v, want ErrInvalidArgument", err)
			}
			if got := repo.createCallCount(); got != 0 {
				t.Fatalf("Create() called %d times, want 0", got)
			}
		})
	}
}

func TestTriggerValidationExecutesOnlyValidationNodes(t *testing.T) {
	repo := &stubWorkflowRepository{}
	var buildCount atomic.Int64
	var releaseCount atomic.Int64
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-verify",
		Key:  "release-dag",
		Name: "Release DAG",
		Definition: map[string]any{
			"mode": "release_dag",
			"nodes": []map[string]any{
				{"id": "build", "name": "Build", "type": "build"},
				{"id": "deploy", "name": "Deploy", "type": "deploy_update_image"},
				{"id": "verify", "name": "Verify", "type": "check"},
			},
			"edges": []map[string]any{
				{"id": "e1", "source": "build", "target": "deploy", "condition": "success"},
				{"id": "e2", "source": "deploy", "target": "verify", "condition": "success"},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		WorkflowTemplateID: "wf-verify",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		releases:    countingWorkflowReleaseExecutor{count: &releaseCount},
		resources:   stubWorkflowResourceExecutor{},
		builds:      countingWorkflowBuildExecutor{count: &buildCount},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}
	service.SetRuntimeOptions(1, 8, 1)
	runnerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(runnerCtx)
	defer func() {
		_ = service.Shutdown(context.Background())
	}()

	run, err := service.TriggerValidation(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:            "app-1",
		ApplicationEnvironmentID: "binding-1",
		WorkflowName:             "release-dag",
		ClusterID:                "cluster-1",
		Namespace:                "default",
		DeploymentName:           "demo",
	})
	if err != nil {
		t.Fatalf("TriggerValidation() error = %v", err)
	}
	if run.Metadata["runMode"] != "validation" {
		t.Fatalf("runMode = %v, want validation", run.Metadata["runMode"])
	}
	if len(run.NodeRuns) != 1 || run.NodeRuns[0].Type != "check" {
		t.Fatalf("NodeRuns = %+v, want only check validation node", run.NodeRuns)
	}
	finalRun := waitForWorkflowStatus(t, service, run.ID, "completed")
	if gotBuild, gotRelease := buildCount.Load(), releaseCount.Load(); gotBuild != 0 || gotRelease != 0 {
		t.Fatalf("build/release counts = %d/%d, want 0/0 for validation run", gotBuild, gotRelease)
	}
	if finalRun.Status != "completed" {
		t.Fatalf("validation final status = %q, nodeRuns = %+v, want completed", finalRun.Status, finalRun.NodeRuns)
	}
}

func TestTriggerValidationRequiresValidationNodes(t *testing.T) {
	repo := &stubWorkflowRepository{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-build",
		Key:  "release-dag",
		Name: "Release DAG",
		Definition: map[string]any{
			"mode": "release_dag",
			"nodes": []map[string]any{
				{"id": "build", "name": "Build", "type": "build"},
				{"id": "deploy", "name": "Deploy", "type": "deploy_update_image"},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		WorkflowTemplateID: "wf-build",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}

	_, err := service.TriggerValidation(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:            "app-1",
		ApplicationEnvironmentID: "binding-1",
		ClusterID:                "cluster-1",
		Namespace:                "default",
		DeploymentName:           "demo",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("TriggerValidation() error = %v, want invalid argument", err)
	}
	if got := repo.createCallCount(); got != 0 {
		t.Fatalf("Create() called %d times, want 0", got)
	}
}

func TestTriggerRollbackExecutesOnlyRollbackNodes(t *testing.T) {
	repo := &stubWorkflowRepository{}
	var buildCount atomic.Int64
	var releaseCount atomic.Int64
	var rollbackCount atomic.Int64
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-rollback",
		Key:  "release-dag",
		Name: "Release DAG",
		Definition: map[string]any{
			"mode": "release_dag",
			"nodes": []map[string]any{
				{"id": "build", "name": "Build", "type": "build"},
				{"id": "deploy", "name": "Deploy", "type": "deploy_update_image"},
				{"id": "rollback", "name": "Rollback", "type": "rollback_to_previous"},
			},
			"edges": []map[string]any{
				{"id": "e1", "source": "build", "target": "deploy", "condition": "success"},
				{"id": "e2", "source": "deploy", "target": "rollback", "condition": "failure"},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		WorkflowTemplateID: "wf-rollback",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		releases:    countingWorkflowReleaseExecutor{count: &releaseCount},
		resources:   countingWorkflowResourceExecutor{rollbackCount: &rollbackCount},
		builds:      countingWorkflowBuildExecutor{count: &buildCount},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}
	service.SetRuntimeOptions(1, 8, 1)
	runnerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(runnerCtx)
	defer func() {
		_ = service.Shutdown(context.Background())
	}()

	run, err := service.TriggerRollback(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:            "app-1",
		ApplicationEnvironmentID: "binding-1",
		WorkflowName:             "release-dag",
		ClusterID:                "cluster-1",
		Namespace:                "default",
		DeploymentName:           "demo",
		Variables:                map[string]any{"releaseBundleId": "bundle-prev"},
	})
	if err != nil {
		t.Fatalf("TriggerRollback() error = %v", err)
	}
	if run.Metadata["runMode"] != "rollback" || run.Metadata["releaseBundleId"] != "bundle-prev" {
		t.Fatalf("metadata = %#v, want rollback run mode and bundle", run.Metadata)
	}
	if len(run.NodeRuns) != 1 || run.NodeRuns[0].Type != "rollback_to_previous" {
		t.Fatalf("NodeRuns = %+v, want only rollback node", run.NodeRuns)
	}
	finalRun := waitForWorkflowStatus(t, service, run.ID, "completed")
	gotBuild, gotRelease, gotRollback := buildCount.Load(), releaseCount.Load(), rollbackCount.Load()
	if gotBuild != 0 || gotRelease != 0 || gotRollback != 1 {
		t.Fatalf("build/release/rollback counts = %d/%d/%d, want 0/0/1", gotBuild, gotRelease, gotRollback)
	}
	if finalRun.Status != "completed" {
		t.Fatalf("rollback final status = %q, nodeRuns = %+v, want completed", finalRun.Status, finalRun.NodeRuns)
	}
}

func TestTriggerRollbackRequiresRollbackNodes(t *testing.T) {
	repo := &stubWorkflowRepository{}
	template := &domaincatalog.WorkflowTemplate{
		ID:   "wf-build",
		Key:  "release-dag",
		Name: "Release DAG",
		Definition: map[string]any{
			"mode": "release_dag",
			"nodes": []map[string]any{
				{"id": "build", "name": "Build", "type": "build"},
				{"id": "deploy", "name": "Deploy", "type": "deploy_update_image"},
			},
		},
	}
	binding := domaincatalog.ApplicationEnvironment{
		ID:                 "binding-1",
		ApplicationID:      "app-1",
		WorkflowTemplateID: "wf-build",
		WorkflowTemplate:   template,
		Targets: []domaincatalog.ReleaseTarget{
			{ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		apps:        &stubWorkflowApps{},
		catalog:     &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger}}}),
	}

	_, err := service.TriggerRollback(context.Background(), domainidentity.Principal{UserName: "tester", Roles: []string{"developer"}}, domainworkflow.Input{
		ApplicationID:            "app-1",
		ApplicationEnvironmentID: "binding-1",
		ClusterID:                "cluster-1",
		Namespace:                "default",
		DeploymentName:           "demo",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("TriggerRollback() error = %v, want invalid argument", err)
	}
	if got := repo.createCallCount(); got != 0 {
		t.Fatalf("Create() called %d times, want 0", got)
	}
}
