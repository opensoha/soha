package workflow

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainbuild "github.com/kubecrux/kubecrux/internal/domain/build"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainrelease "github.com/kubecrux/kubecrux/internal/domain/release"
	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
	domainworkflow "github.com/kubecrux/kubecrux/internal/domain/workflow"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	apprepo "github.com/kubecrux/kubecrux/internal/repository/application"
)

type stubWorkflowRepository struct {
	items       []domainworkflow.Run
	deletedIDs  []string
	createCalls int
	updated     []domainworkflow.Run
}

func (r *stubWorkflowRepository) List(context.Context, string, int) ([]domainworkflow.Run, error) {
	return append([]domainworkflow.Run(nil), r.items...), nil
}

func (r *stubWorkflowRepository) Create(_ context.Context, item domainworkflow.Run) (domainworkflow.Run, error) {
	r.createCalls++
	return item, nil
}

func (r *stubWorkflowRepository) Update(_ context.Context, item domainworkflow.Run) (domainworkflow.Run, error) {
	r.updated = append(r.updated, item)
	return item, nil
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

type stubWorkflowBuildExecutor struct{}

func (stubWorkflowBuildExecutor) Trigger(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Record, error) {
	return domainbuild.Record{ID: "build-1", Status: "queued"}, nil
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
		repo: repo,
		apps: &stubWorkflowApps{missing: map[string]bool{"app-missing": true}},
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
		repo: repo,
		apps: &stubWorkflowApps{missing: map[string]bool{"missing-app": true}},
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
		repo: repo,
		apps: &stubWorkflowApps{},
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
		repo:      repo,
		apps:      &stubWorkflowApps{},
		catalog:   &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}},
		releases:  stubWorkflowReleaseExecutor{},
		resources: stubWorkflowResourceExecutor{},
		builds:    stubWorkflowBuildExecutor{},
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
	for len(repo.updated) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(repo.updated) == 0 {
		t.Fatalf("expected async workflow runner to persist updates")
	}
	if repo.updated[len(repo.updated)-1].Status != "completed" {
		t.Fatalf("final updated status = %q, want completed", repo.updated[len(repo.updated)-1].Status)
	}
}
