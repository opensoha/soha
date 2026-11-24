package delivery

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type unavailableRuntimeTargetReader struct {
	stubTargetReader
	clusterID string
	err       error
}

type scopedRuntimeApplicationReader struct {
	stubApplicationReader
	t *testing.T
}

func (r scopedRuntimeApplicationReader) ListServices(context.Context, domainidentity.Principal, string) ([]domainapp.Service, error) {
	r.t.Fatal("runtime summary read full service definitions")
	return nil, nil
}

type scopedRuntimeCatalogReader struct {
	stubCatalogReader
	t *testing.T
}

func (r scopedRuntimeCatalogReader) ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error) {
	r.t.Fatal("runtime summary listed the entire environment catalog")
	return nil, nil
}

func (s unavailableRuntimeTargetReader) ListDeployments(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.DeploymentView, error) {
	if clusterID == s.clusterID {
		return nil, s.err
	}
	return s.stubTargetReader.ListDeployments(ctx, principal, clusterID, namespace)
}

func TestGetApplicationRuntimeDetailKeepsUnavailableEnvironment(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		verifyUnavailableEnvironment(t, context.DeadlineExceeded)
	})
	t.Run("forbidden", func(t *testing.T) {
		verifyUnavailableEnvironment(t, apperrors.ErrAccessDenied)
	})
}

func verifyUnavailableEnvironment(t *testing.T, observationErr error) {
	t.Helper()
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
			err:       observationErr,
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

type scopedRuntimeTargetReader struct {
	stubTargetReader
	mu    sync.Mutex
	calls []string
}

func (r *scopedRuntimeTargetReader) ListDeployments(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.DeploymentView, error) {
	r.mu.Lock()
	r.calls = append(r.calls, clusterID+"/"+namespace)
	r.mu.Unlock()
	if clusterID == "denied" {
		return nil, apperrors.ErrAccessDenied
	}
	if clusterID == "timed-out" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return r.stubTargetReader.ListDeployments(ctx, principal, clusterID, namespace)
}

func TestApplicationEnvironmentRuntimeSummaryScopesAndDeduplicatesObservation(t *testing.T) {
	readyTarget := func(name string) domaincatalog.ReleaseTarget {
		return domaincatalog.ReleaseTarget{ClusterID: "ready", Namespace: "demo", WorkloadName: name, ServiceID: name + "-id", Enabled: true}
	}
	deniedTarget := domaincatalog.ReleaseTarget{ClusterID: "denied", Namespace: "demo", WorkloadName: "hidden", Enabled: true}
	timeoutTarget := domaincatalog.ReleaseTarget{ClusterID: "timed-out", Namespace: "demo", WorkloadName: "slow", Enabled: true}
	bindings := []domaincatalog.ApplicationEnvironment{
		{ID: "ready", ApplicationID: "app", EnvironmentID: "dev", Targets: []domaincatalog.ReleaseTarget{readyTarget("web"), readyTarget("api")},
			ResourceSelector: domaincatalog.ResourceSelector{MatchLabels: map[string]string{"shared": "true"}},
		},
		{ID: "denied", ApplicationID: "app", Targets: []domaincatalog.ReleaseTarget{deniedTarget}},
		{ID: "partial", ApplicationID: "app", Targets: []domaincatalog.ReleaseTarget{deniedTarget, readyTarget("web")}},
		{ID: "timeout", ApplicationID: "app", Targets: []domaincatalog.ReleaseTarget{timeoutTarget, readyTarget("web")}},
		{ID: "missing", ApplicationID: "app", Targets: []domaincatalog.ReleaseTarget{readyTarget("absent"), readyTarget("web")}},
		{ID: "manifest-unavailable", ApplicationID: "app", Targets: []domaincatalog.ReleaseTarget{
			{ExecutorKind: "manifest_ssa", Enabled: true}, readyTarget("web"),
		}},
		{ID: "unconfigured", ApplicationID: "app"},
		{ID: "foreign", ApplicationID: "other", Targets: []domaincatalog.ReleaseTarget{readyTarget("web")}},
	}
	for _, tc := range []struct {
		binding, status string
		calls           []string
		workloads       int
		wantErr         error
	}{
		{binding: "ready", status: "available", calls: []string{"ready/demo"}, workloads: 2},
		{binding: "denied", status: "forbidden", calls: []string{"denied/demo"}},
		{binding: "partial", status: "partial", calls: []string{"denied/demo", "ready/demo"}, workloads: 1},
		{binding: "timeout", status: "partial", calls: []string{"ready/demo", "timed-out/demo"}, workloads: 1},
		{binding: "missing", status: "partial", calls: []string{"ready/demo"}, workloads: 1},
		{binding: "manifest-unavailable", status: "partial", calls: []string{"ready/demo"}, workloads: 1},
		{binding: "unconfigured", status: "unconfigured"},
		{binding: "foreign", wantErr: apperrors.ErrNotFound},
		{binding: "binding-outside-grant", wantErr: apperrors.ErrAccessDenied},
	} {
		t.Run(tc.binding, func(t *testing.T) {
			reader := &scopedRuntimeTargetReader{stubTargetReader: stubTargetReader{
				deployments: map[string][]domainresource.DeploymentView{"ready/demo": {
					{Name: "web", Labels: map[string]string{"shared": "true"}},
					{Name: "api", Labels: map[string]string{"shared": "true"}},
				}},
			}}
			service := &Service{
				applications: scopedRuntimeApplicationReader{t: t, stubApplicationReader: stubApplicationReader{
					app: domainapp.App{ID: "app"}, services: []domainapp.Service{{ID: "web-id", Key: "web"}},
				}},
				catalog: scopedRuntimeCatalogReader{t: t, stubCatalogReader: stubCatalogReader{
					bindings: bindings, envs: []domaincatalog.Environment{{ID: "dev", Name: "Development", IsProduction: true, RequiresApproval: true}},
					getErrByBindingID: map[string]error{"binding-outside-grant": apperrors.ErrAccessDenied},
				}},
				targets: reader,
			}
			got, err := service.GetApplicationEnvironmentRuntimeSummary(t.Context(), domainidentity.Principal{}, "app", tc.binding)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("summary error = %v, want %v", err, tc.wantErr)
			}
			slices.Sort(reader.calls)
			if !reflect.DeepEqual(reader.calls, tc.calls) {
				t.Fatalf("cluster reads = %v, want %v", reader.calls, tc.calls)
			}
			if tc.wantErr != nil {
				return
			}
			if got.ObservationStatus != tc.status || len(got.Environment.Workloads) != tc.workloads || got.ObservedAt.IsZero() {
				t.Fatalf("summary = %+v, want %s with %d workloads and timestamp", got, tc.status, tc.workloads)
			}
			if tc.binding == "ready" && (got.Environment.EnvironmentName != "Development" || got.Environment.ActionKind != "release" ||
				!got.Environment.RequiresApproval || got.Environment.Workloads[0].ServiceID != "web-id" ||
				got.Environment.Workloads[0].ServiceKey != "web" || got.Environment.Workloads[1].ServiceID != "api-id") {
				t.Fatalf("environment/service identity lost: %+v", got.Environment)
			}
		})
	}
}

func TestRuntimeSummaryRequiresObservedManifestUID(t *testing.T) {
	target := domaincatalog.ReleaseTarget{
		ExecutorKind: "manifest_inventory", WorkloadName: "api",
		Metadata: map[string]any{runtimeInventoryUID: "original"},
	}
	resource := domainresource.DeploymentView{Name: "api", UID: "replacement"}
	if matchesRuntimeDeployment(domaincatalog.ApplicationEnvironment{}, target, resource, true) {
		t.Fatal("scoped runtime observation accepted a same-name replacement")
	}
	if got := runtimeObservationStatus(nil, 1, 1, apperrors.ErrNotFound); got != "identity_mismatch" {
		t.Fatalf("persisted snapshot hid the UID mismatch: %s", got)
	}
	resource.UID = "original"
	if !matchesRuntimeDeployment(domaincatalog.ApplicationEnvironment{}, target, resource, true) {
		t.Fatal("scoped runtime observation rejected the recorded identity")
	}
}

func TestRuntimeSummaryPrioritizesNamedDeploymentTargets(t *testing.T) {
	targets := []domaincatalog.ReleaseTarget{
		{Enabled: true, ClusterID: "cluster", Namespace: "team", ServiceID: "fallback"},
		{Enabled: true, ClusterID: "cluster", Namespace: "team", WorkloadName: "web", ServiceID: "web"},
	}
	groups := groupRuntimeTargets(targets)
	if len(groups) != 1 || len(groups[0]) != 2 || groups[0][0].ServiceID != "web" {
		t.Fatalf("exact target does not precede the selector: %+v", groups)
	}
	targets[1].WorkloadKind = "StatefulSet"
	if matchesRuntimeDeployment(domaincatalog.ApplicationEnvironment{}, targets[1], domainresource.DeploymentView{Name: "web"}, true) {
		t.Fatal("same-name Deployment attributed to a StatefulSet")
	}
}
