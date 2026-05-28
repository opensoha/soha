package workflow

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	appaccess "github.com/soha/soha/internal/application/access"
	domainapp "github.com/soha/soha/internal/domain/application"
	domainbuild "github.com/soha/soha/internal/domain/build"
	domaincatalog "github.com/soha/soha/internal/domain/catalog"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainrelease "github.com/soha/soha/internal/domain/release"
	domainresource "github.com/soha/soha/internal/domain/resource"
	domainworkflow "github.com/soha/soha/internal/domain/workflow"
	"github.com/soha/soha/internal/platform/apperrors"
	apprepo "github.com/soha/soha/internal/repository/application"
)

type stubWorkflowRepository struct {
	items       []domainworkflow.Run
	deletedIDs  []string
	createCalls int
	updated     []domainworkflow.Run
	approvals   []domainworkflow.Approval
}

func (r *stubWorkflowRepository) List(context.Context, string, int) ([]domainworkflow.Run, error) {
	return append([]domainworkflow.Run(nil), r.items...), nil
}

func (r *stubWorkflowRepository) Create(_ context.Context, item domainworkflow.Run) (domainworkflow.Run, error) {
	r.createCalls++
	return item, nil
}

func (r *stubWorkflowRepository) Get(_ context.Context, runID string) (domainworkflow.Run, error) {
	for _, item := range r.items {
		if item.ID == runID {
			return item, nil
		}
	}
	return domainworkflow.Run{}, errors.New("workflow run not found")
}

func (r *stubWorkflowRepository) Update(_ context.Context, item domainworkflow.Run) (domainworkflow.Run, error) {
	r.updated = append(r.updated, item)
	return item, nil
}

func (r *stubWorkflowRepository) CreateApproval(_ context.Context, item domainworkflow.Approval) error {
	r.approvals = append(r.approvals, item)
	return nil
}

func (r *stubWorkflowRepository) DeleteByIDs(_ context.Context, ids []string) error {
	r.deletedIDs = append(r.deletedIDs, ids...)
	return nil
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
	count *int
}

func (s countingWorkflowReleaseExecutor) Trigger(_ context.Context, _ domainidentity.Principal, input domainrelease.TriggerInput) (domainrelease.Record, error) {
	if s.count != nil {
		*s.count = *s.count + 1
	}
	return domainrelease.Record{ID: "release-1", Status: "deployed", ApplicationID: input.ApplicationID}, nil
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

type stubWorkflowBuildExecutor struct{}

func (stubWorkflowBuildExecutor) Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	return domainbuild.Record{ID: "build-1", Status: "queued"}, nil
}

func (stubWorkflowBuildExecutor) Execute(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	return domainbuild.Record{ID: "build-1", Status: "completed", Metadata: map[string]any{"image": "repo/demo:latest", "artifact": map[string]any{"ref": "repo/demo:latest"}}}, nil
}

type countingWorkflowBuildExecutor struct {
	count *int
}

func (s countingWorkflowBuildExecutor) Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	if s.count != nil {
		*s.count = *s.count + 1
	}
	return domainbuild.Record{ID: "build-1", Status: "queued"}, nil
}

func (s countingWorkflowBuildExecutor) Execute(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	if s.count != nil {
		*s.count = *s.count + 1
	}
	return domainbuild.Record{ID: "build-1", Status: "completed", Metadata: map[string]any{"image": "repo/demo:latest", "artifact": map[string]any{"ref": "repo/demo:latest"}}}, nil
}

type stubWorkflowRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubWorkflowRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
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

	sort.Strings(repo.deletedIDs)
	expected := []string{"stale-empty-app", "stale-missing-app"}
	sort.Strings(expected)
	if len(repo.deletedIDs) != len(expected) {
		t.Fatalf("deletedIDs len = %d, want %d (%v)", len(repo.deletedIDs), len(expected), repo.deletedIDs)
	}
	for i := range expected {
		if repo.deletedIDs[i] != expected[i] {
			t.Fatalf("deletedIDs = %v, want %v", repo.deletedIDs, expected)
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
	if repo.createCalls != 0 {
		t.Fatalf("Create() called %d times, want 0", repo.createCalls)
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
	if repo.createCalls != 0 {
		t.Fatalf("Create() called %d times, want 0", repo.createCalls)
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
	deadline := time.Now().Add(300 * time.Millisecond)
	for (len(repo.updated) == 0 || repo.updated[len(repo.updated)-1].Status == "running") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(repo.updated) == 0 {
		t.Fatalf("expected async workflow runner to persist updates")
	}
	if repo.updated[len(repo.updated)-1].Status != workflowStatusWaitingApproval {
		t.Fatalf("final updated status = %q, want waiting_approval", repo.updated[len(repo.updated)-1].Status)
	}
	repo.items = []domainworkflow.Run{repo.updated[len(repo.updated)-1]}
	if _, err := service.Approve(context.Background(), domainidentity.Principal{UserID: "u-1", UserName: "approver", Roles: []string{"developer"}}, run.ID, "approved"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	deadline = time.Now().Add(300 * time.Millisecond)
	for repo.updated[len(repo.updated)-1].Status != "completed" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if repo.updated[len(repo.updated)-1].Status != "completed" {
		t.Fatalf("approved workflow final status = %q, want completed", repo.updated[len(repo.updated)-1].Status)
	}
}

func TestTriggerValidationExecutesOnlyValidationNodes(t *testing.T) {
	repo := &stubWorkflowRepository{}
	buildCount := 0
	releaseCount := 0
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
	deadline := time.Now().Add(300 * time.Millisecond)
	for len(repo.updated) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if buildCount != 0 || releaseCount != 0 {
		t.Fatalf("build/release counts = %d/%d, want 0/0 for validation run", buildCount, releaseCount)
	}
	if len(repo.updated) == 0 {
		t.Fatalf("expected validation workflow runner to persist updates")
	}
	if repo.updated[len(repo.updated)-1].Status != "completed" {
		t.Fatalf("validation final status = %q, nodeRuns = %+v, want completed", repo.updated[len(repo.updated)-1].Status, repo.updated[len(repo.updated)-1].NodeRuns)
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
	if repo.createCalls != 0 {
		t.Fatalf("Create() called %d times, want 0", repo.createCalls)
	}
}
