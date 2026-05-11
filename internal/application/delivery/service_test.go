package delivery

import (
	"context"
	"testing"

	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainbuild "github.com/kubecrux/kubecrux/internal/domain/build"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	domaindelivery "github.com/kubecrux/kubecrux/internal/domain/delivery"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainrelease "github.com/kubecrux/kubecrux/internal/domain/release"
	domainworkflow "github.com/kubecrux/kubecrux/internal/domain/workflow"
)

type stubApplicationReader struct {
	app domainapp.App
}

func (s stubApplicationReader) List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error) {
	return []domainapp.App{s.app}, nil
}

func (s stubApplicationReader) Get(context.Context, domainidentity.Principal, string) (domainapp.App, error) {
	return s.app, nil
}

type stubCatalogReader struct {
	bindings []domaincatalog.ApplicationEnvironment
	envs     []domaincatalog.Environment
}

func (s stubCatalogReader) ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error) {
	return s.envs, nil
}

func (s stubCatalogReader) ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error) {
	return s.bindings, nil
}

func (s stubCatalogReader) GetApplicationEnvironment(context.Context, domainidentity.Principal, string) (domaincatalog.ApplicationEnvironment, error) {
	if len(s.bindings) == 0 {
		return domaincatalog.ApplicationEnvironment{}, nil
	}
	return s.bindings[0], nil
}

type stubBuildReader struct{}

func (stubBuildReader) List(context.Context, domainidentity.Principal, domainbuild.Filter) ([]domainbuild.Record, error) {
	return nil, nil
}

type stubWorkflowReader struct{}

func (stubWorkflowReader) List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error) {
	return nil, nil
}

type stubReleaseReader struct{}

func (stubReleaseReader) List(context.Context, domainidentity.Principal, domainrelease.Filter) ([]domainrelease.Record, error) {
	return nil, nil
}

type stubRepository struct{}

func (stubRepository) ListReleaseBundles(context.Context, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	return nil, nil
}

func (stubRepository) GetReleaseBundle(context.Context, string) (domaindelivery.ReleaseBundle, error) {
	return domaindelivery.ReleaseBundle{}, nil
}

func (stubRepository) CreateReleaseBundle(context.Context, domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	return domaindelivery.ReleaseBundle{}, nil
}

func (stubRepository) UpdateReleaseBundle(context.Context, domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	return domaindelivery.ReleaseBundle{}, nil
}

func (stubRepository) ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	return nil, nil
}

func (stubRepository) GetExecutionTask(context.Context, string) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) GetExecutionTaskByCallbackToken(context.Context, string) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) ClaimExecutionTask(context.Context, []string, string, string) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) CreateExecutionTask(context.Context, domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) UpdateExecutionTask(context.Context, domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) ListExecutionLogs(context.Context, string, int) ([]domaindelivery.ExecutionLog, error) {
	return nil, nil
}

func (stubRepository) CreateExecutionLog(context.Context, domaindelivery.ExecutionLog) error {
	return nil
}

func (stubRepository) CreateExecutionCallback(context.Context, domaindelivery.ExecutionCallback) error {
	return nil
}

func (stubRepository) ListExecutionArtifacts(context.Context, string) ([]domaindelivery.ExecutionArtifact, error) {
	return nil, nil
}

func (stubRepository) ListExecutionArtifactsByBundle(context.Context, string) ([]domaindelivery.ExecutionArtifact, error) {
	return nil, nil
}

func (stubRepository) UpsertExecutionArtifact(context.Context, domaindelivery.ExecutionArtifact) (domaindelivery.ExecutionArtifact, error) {
	return domaindelivery.ExecutionArtifact{}, nil
}

func (stubRepository) ListApprovalPolicies(context.Context) ([]domaindelivery.ApprovalPolicy, error) {
	return nil, nil
}

func (stubRepository) GetApprovalPolicy(context.Context, string) (domaindelivery.ApprovalPolicy, error) {
	return domaindelivery.ApprovalPolicy{}, nil
}

func (stubRepository) CreateApprovalPolicy(context.Context, domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	return domaindelivery.ApprovalPolicy{}, nil
}

func (stubRepository) UpdateApprovalPolicy(context.Context, string, domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	return domaindelivery.ApprovalPolicy{}, nil
}

func (stubRepository) DeleteApprovalPolicy(context.Context, string) error {
	return nil
}

func TestGetApplicationDetailIncludesBindingTargets(t *testing.T) {
	service := New(
		stubApplicationReader{
			app: domainapp.App{
				ID:   "app-1",
				Name: "demo",
				BuildSources: []domainapp.BuildSource{
					{ID: "build-source-1", Name: "Repo Dockerfile", IsDefault: true},
				},
			},
		},
		stubCatalogReader{
			envs: []domaincatalog.Environment{
				{ID: "env-1", Name: "test"},
			},
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "binding-1",
					ApplicationID:  "app-1",
					EnvironmentID:  "env-1",
					EnvironmentKey: "test",
					BuildPolicy:    domaincatalog.BuildPolicy{SourceID: "build-source-1"},
					Targets: []domaincatalog.ReleaseTarget{
						{
							ID:           "target-1",
							ClusterID:    "cluster-a",
							Namespace:    "namespace-a",
							WorkloadKind: "Deployment",
							WorkloadName: "demo-api",
							Enabled:      true,
						},
					},
				},
			},
		},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		stubRepository{},
		nil,
		nil,
		nil,
	)

	result, err := service.GetApplicationDetail(context.Background(), domainidentity.Principal{}, "app-1")
	if err != nil {
		t.Fatalf("GetApplicationDetail returned error: %v", err)
	}
	if len(result.Bindings) != 1 {
		t.Fatalf("Bindings length = %d, want 1", len(result.Bindings))
	}
	if result.Bindings[0].TargetCount != 1 {
		t.Fatalf("TargetCount = %d, want 1", result.Bindings[0].TargetCount)
	}
	if len(result.Bindings[0].Targets) != 1 {
		t.Fatalf("Targets length = %d, want 1", len(result.Bindings[0].Targets))
	}
	target := result.Bindings[0].Targets[0]
	if target.ClusterID != "cluster-a" || target.Namespace != "namespace-a" || target.WorkloadName != "demo-api" {
		t.Fatalf("Targets = %+v, want cluster/namespace/workload summary", target)
	}
}
