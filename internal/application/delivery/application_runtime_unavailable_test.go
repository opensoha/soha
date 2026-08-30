package delivery

import (
	"context"
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type unavailableRuntimeTargetReader struct {
	stubTargetReader
	clusterID string
	err       error
}

func (s unavailableRuntimeTargetReader) ListDeployments(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.DeploymentView, error) {
	if clusterID == s.clusterID {
		return nil, s.err
	}
	return s.stubTargetReader.ListDeployments(ctx, principal, clusterID, namespace)
}

func TestGetApplicationRuntimeDetailKeepsUnavailableEnvironment(t *testing.T) {
	app := domainapp.App{ID: "app-1", Name: "demo"}
	bindings := []domaincatalog.ApplicationEnvironment{
		{
			ID:             "binding-ready",
			ApplicationID:  app.ID,
			EnvironmentID:  "env-ready",
			EnvironmentKey: "ready",
			Targets: []domaincatalog.ReleaseTarget{{
				ClusterID:    "cluster-ready",
				Namespace:    "default",
				WorkloadName: "demo",
				Enabled:      true,
			}},
		},
		{
			ID:             "binding-down",
			ApplicationID:  app.ID,
			EnvironmentID:  "env-down",
			EnvironmentKey: "down",
			Targets: []domaincatalog.ReleaseTarget{{
				ClusterID:    "cluster-down",
				Namespace:    "default",
				WorkloadName: "demo",
				Enabled:      true,
			}},
		},
	}
	service := New(
		stubApplicationReader{app: app},
		stubCatalogReader{
			bindings: bindings,
			envs: []domaincatalog.Environment{
				{ID: "env-ready", Name: "Ready"},
				{ID: "env-down", Name: "Down"},
			},
		},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		stubRepository{},
		nil,
		unavailableRuntimeTargetReader{
			stubTargetReader: stubTargetReader{deployments: map[string][]domainresource.DeploymentView{
				"cluster-ready/default": {{Name: "demo"}},
			}},
			clusterID: "cluster-down",
			err:       context.DeadlineExceeded,
		},
		nil,
	)

	detail, err := service.GetApplicationRuntimeDetail(context.Background(), domainidentity.Principal{}, app.ID)
	if err != nil {
		t.Fatalf("GetApplicationRuntimeDetail returned error: %v", err)
	}
	if len(detail.Environments) != 2 {
		t.Fatalf("environment count = %d, want 2", len(detail.Environments))
	}
	if len(detail.Environments[0].Workloads) != 1 {
		t.Fatalf("ready workload count = %d, want 1", len(detail.Environments[0].Workloads))
	}
	if detail.Environments[0].Status != runtimeEnvironmentAvailable {
		t.Fatalf("ready environment status = %q, want %q", detail.Environments[0].Status, runtimeEnvironmentAvailable)
	}
	if len(detail.Environments[1].Workloads) != 0 {
		t.Fatalf("unavailable workload count = %d, want 0", len(detail.Environments[1].Workloads))
	}
	if detail.Environments[1].Status != runtimeEnvironmentUnavailable {
		t.Fatalf("unavailable environment status = %q, want %q", detail.Environments[1].Status, runtimeEnvironmentUnavailable)
	}
	if detail.Summary.HealthStatus != runtimeHealthUnhealthy {
		t.Fatalf("summary health status = %q, want %q", detail.Summary.HealthStatus, runtimeHealthUnhealthy)
	}
}
