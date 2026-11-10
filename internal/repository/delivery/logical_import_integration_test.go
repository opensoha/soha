package delivery_test

import (
	"errors"
	"github.com/google/uuid"
	da "github.com/opensoha/soha/internal/domain/application"
	apprepo "github.com/opensoha/soha/internal/repository/application"
	"testing"
	"time"

	dc "github.com/opensoha/soha/internal/domain/catalog"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	catalogrepo "github.com/opensoha/soha/internal/repository/catalog"
)

func (f *importIdentityFixture) logicalInput(namespace, environmentSuffix, name, uid string) dd.KubernetesServiceImportInput {
	input := f.input
	input.Namespace = namespace
	input.EnvironmentKey += environmentSuffix
	workload := input.Workloads[0]
	source := *workload.Source
	source.Resource.Namespace = namespace
	source.Resource.Name = name
	source.Resource.UID = uid
	workload.ServiceID = f.first.Services[0].ID
	workload.WorkloadName = name
	workload.Source = &source
	input.Workloads = []dd.KubernetesWorkloadImportInput{workload}
	return input
}
func (f *importIdentityFixture) logicalServiceSnapshot(t *testing.T) string {
	t.Helper()
	var value string
	if err := f.store.DB().Raw(`SELECT row_to_json(s)::text FROM application_services s WHERE id=?`, f.first.Services[0].ID).Row().Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
func (f *importIdentityFixture) checkLogicalServiceMapping(t *testing.T) {
	before := f.logicalServiceSnapshot(t)
	input := f.logicalInput("logical-orders", "-logical", "api", "logical-uid")
	result, err := f.repo.ImportKubernetesServices(f.ctx, f.resolve(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if result.Services[0].ID != f.first.Services[0].ID || result.Services[0].Created || result.Targets[0].ServiceID != f.first.Services[0].ID || result.ApplicationEnvironmentID == f.first.ApplicationEnvironmentID {
		t.Fatalf("mapping=%+v", result)
	}
	if f.logicalServiceSnapshot(t) != before {
		t.Fatal("cross-environment mapping overwrote the logical service")
	}
	repeat, err := f.repo.ImportKubernetesServices(f.ctx, f.resolve(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if repeat.Targets[0].Created || repeat.Targets[0].ID != result.Targets[0].ID {
		t.Fatalf("idempotency=%+v", repeat)
	}
	var enabled bool
	var executor string
	if err := f.store.DB().Raw(`SELECT enabled,executor_kind FROM release_targets WHERE id=?`, result.Targets[0].ID).Row().Scan(&enabled, &executor); err != nil {
		t.Fatal(err)
	}
	if enabled || executor != "observe_only" {
		t.Fatal("logical mapping granted publishing control")
	}
}
func (f *importIdentityFixture) checkInvalidLogicalServiceMapping(t *testing.T) {
	input := f.logicalInput("invalid-logical", "-invalid-logical", "api", "logical-invalid")
	input.Workloads[0].ServiceID = "missing-or-foreign"
	if _, err := f.repo.ImportKubernetesServices(f.ctx, f.resolve(t, input)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("foreign mapping=%v", err)
	}
	var count int64
	if err := f.store.DB().Raw(`SELECT COUNT(*) FROM delivery_environments WHERE environment_key=?`, input.EnvironmentKey).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("denied mapping left a new environment")
	}
}
func (f *importIdentityFixture) checkImportDefaultCAS(t *testing.T) {
	repo := catalogrepo.New(f.store.DB())
	before, err := repo.GetApplicationEnvironment(f.ctx, f.first.ApplicationEnvironmentID)
	if err != nil {
		t.Fatal(err)
	}
	input := f.logicalInput("logical-cas", "", "cas-api", "logical-cas-uid")
	if _, err := f.repo.ImportKubernetesServices(f.ctx, f.resolve(t, input)); err != nil {
		t.Fatal(err)
	}
	next, err := repo.GetApplicationEnvironment(f.ctx, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !next.UpdatedAt.After(before.UpdatedAt) {
		t.Fatal("import did not invalidate the old target-set revision")
	}
	update := dc.ApplicationEnvironmentInput{ApplicationID: before.ApplicationID, EnvironmentID: before.EnvironmentID, ClusterID: f.input.ClusterID, Namespace: "new-default", ExpectedUpdatedAt: &before.UpdatedAt, BuildPolicy: before.BuildPolicy, ReleasePolicy: before.ReleasePolicy, ResourceSelector: before.ResourceSelector}
	if _, err := repo.UpdateApplicationEnvironment(f.ctx, before.ID, update); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale defaults=%v", err)
	}
	update.ExpectedUpdatedAt = &next.UpdatedAt
	saved, err := repo.UpdateApplicationEnvironment(f.ctx, before.ID, update)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Targets) != len(next.Targets) || saved.Namespace != "new-default" {
		t.Fatalf("default edit lost targets: %+v", saved)
	}
	for i, target := range saved.Targets {
		prior := next.Targets[i]
		if target.ID != prior.ID || target.ClusterID != prior.ClusterID || target.Namespace != prior.Namespace || target.ResolvedServiceID() != prior.ResolvedServiceID() {
			t.Fatalf("default edit moved target: %+v", target)
		}
	}
}

func (f *importIdentityFixture) checkHelmLogicalServiceMapping(t *testing.T) {
	before := f.logicalServiceSnapshot(t)
	input := dd.HelmReleaseImportInput{ClusterID: f.input.ClusterID, Namespace: "logical-helm", ApplicationKey: f.input.ApplicationKey, ApplicationName: f.input.ApplicationName, EnvironmentKey: f.input.EnvironmentKey + "-helm-logical", EnvironmentName: "Logical Helm", OwnershipMode: "observe_only", Releases: []dd.HelmReleaseImportItem{{ServiceID: f.first.Services[0].ID, ReleaseName: "orders", Revision: "1", Status: "deployed", Chart: "orders-1.0.0", StorageDriver: "secret"}}}
	destination, err := f.repo.ResolveImportDestination(f.ctx, input.ApplicationKey, input.EnvironmentKey)
	if err != nil {
		t.Fatal(err)
	}
	input.Destination = destination
	result, err := f.repo.ImportHelmReleases(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Services[0].ID != f.first.Services[0].ID || result.Services[0].Created || result.Targets[0].ServiceID != f.first.Services[0].ID || f.logicalServiceSnapshot(t) != before {
		t.Fatalf("Helm mapping overwrote service: %+v", result)
	}
	input.Destination, err = f.repo.ResolveImportDestination(f.ctx, input.ApplicationKey, input.EnvironmentKey)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := f.repo.ImportHelmReleases(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if repeat.Targets[0].ID != result.Targets[0].ID || repeat.Targets[0].Created {
		t.Fatalf("Helm mapping not idempotent: %+v", repeat)
	}
	input.OwnershipMode = "managed"
	if _, err = f.repo.ImportHelmReleases(f.ctx, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("mapping upgraded control: %v", err)
	}
}

func (f *importIdentityFixture) checkServiceCreationIdentity(t *testing.T) {
	repo := apprepo.New(f.store.DB())
	created, err := repo.CreateService(f.ctx, f.first.Application.ID, da.ServiceInput{Key: uuid.NewString(), Name: "Creation identity", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetService(f.ctx, f.first.Application.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !created.CreatedAt.Equal(stored.CreatedAt) || !created.UpdatedAt.Equal(stored.UpdatedAt) || created.Version != stored.Version {
		t.Fatalf("creation identity differs from persistence: returned=%s/%s stored=%s/%s", created.CreatedAt.Format(time.RFC3339Nano), created.UpdatedAt.Format(time.RFC3339Nano), stored.CreatedAt.Format(time.RFC3339Nano), stored.UpdatedAt.Format(time.RFC3339Nano))
	}
}
