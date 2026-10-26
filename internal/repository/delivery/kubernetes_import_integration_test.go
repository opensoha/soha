package delivery_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	deliveryrepo "github.com/opensoha/soha/internal/repository/delivery"
)

func TestKubernetesImportIdentityWithPostgres(t *testing.T) {
	store := openDraftReceiptTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	repo := deliveryrepo.New(store.DB())
	suffix := uuid.NewString()
	input := domaindelivery.KubernetesServiceImportInput{
		ClusterID: "import-cluster-" + suffix, Namespace: "team", ApplicationKey: "import-" + suffix, ApplicationName: "Import test", EnvironmentKey: "import-env-" + suffix, EnvironmentName: "Test", OwnershipMode: "observe_only",
		Workloads: []domaindelivery.KubernetesWorkloadImportInput{{WorkloadKind: "Deployment", WorkloadName: "api", Source: &domainresource.ResourceSourceSnapshot{Resource: domainresource.ResourceRef{ClusterID: "import-cluster-" + suffix, Namespace: "team", APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "uid-1"}, SpecDigest: strings.Repeat("a", 64), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}}},
	}
	if err := store.DB().Exec(`INSERT INTO clusters (id,name) VALUES (?, 'Import acceptance')`, input.ClusterID).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM release_targets WHERE cluster_id = ?`, input.ClusterID).Error
		_ = store.DB().Exec(`DELETE FROM application_environments WHERE application_id IN (SELECT id FROM applications WHERE app_key LIKE ?)`, "import-"+suffix+"%").Error
		_ = store.DB().Exec(`DELETE FROM applications WHERE app_key LIKE ?`, "import-"+suffix+"%").Error
		_ = store.DB().Exec(`DELETE FROM delivery_environments WHERE environment_key LIKE ?`, input.EnvironmentKey+"%").Error
		_ = store.DB().Exec(`DELETE FROM clusters WHERE id = ?`, input.ClusterID).Error
	})

	fixture := &importIdentityFixture{ctx: ctx, store: store, repo: repo, input: input}
	t.Run("create_and_idempotent", fixture.checkCreateAndIdempotent)
	if fixture.first.Application.ID == "" {
		t.Fatal("initial import did not run")
	}
	t.Run("uid_replacement_rollback", fixture.checkUIDReplacementRollback)
	t.Run("ownership_mode_no_upgrade", fixture.checkOwnershipModeNoUpgrade)
	t.Run("preserves_service_metadata", fixture.checkPreservesServiceMetadata)
	t.Run("explicit_service_across_environments", fixture.checkLogicalServiceMapping)
	t.Run("helm_explicit_service_mapping", fixture.checkHelmLogicalServiceMapping)
	t.Run("service_creation_identity_matches_persistence", fixture.checkServiceCreationIdentity)
	t.Run("explicit_service_invalid_rollback", fixture.checkInvalidLogicalServiceMapping)
	t.Run("import_fences_default_edit_cas", fixture.checkImportDefaultCAS)
	t.Run("destination_race_rollback", fixture.checkDestinationRaceRollback)
	t.Run("cross_namespace_identity", fixture.checkCrossNamespaceIdentity)
	t.Run("native_ownership_conflict", fixture.checkNativeOwnershipConflict)
	t.Run("crd_identity_and_shared_reference", fixture.checkCRDIdentityAndSharedReference)
	t.Run("legacy_missing_uid_conflict", fixture.checkLegacyMissingUIDConflict)
}

type importIdentityFixture struct {
	ctx   context.Context
	store *dbstore.Store
	repo  *deliveryrepo.Repository
	input domaindelivery.KubernetesServiceImportInput
	first domaindelivery.KubernetesServiceImportResult
}

func (fixture *importIdentityFixture) resolve(t *testing.T, in domaindelivery.KubernetesServiceImportInput) domaindelivery.KubernetesServiceImportInput {
	t.Helper()
	destination, err := fixture.repo.ResolveImportDestination(fixture.ctx, in.ApplicationKey, in.EnvironmentKey)
	if err != nil {
		t.Fatal(err)
	}
	in.Destination = destination
	return in
}

func (fixture *importIdentityFixture) checkCreateAndIdempotent(t *testing.T) {
	var err error
	fixture.first, err = fixture.repo.ImportKubernetesServices(fixture.ctx, fixture.resolve(t, fixture.input))
	if err != nil {
		t.Fatal(err)
	}
	if !fixture.first.Application.Created || !fixture.first.Services[0].Created || !fixture.first.Targets[0].Created || fixture.first.Targets[0].Source.Resource.UID != "uid-1" {
		t.Fatalf("fixture.first=%+v", fixture.first)
	}
	repeat, err := fixture.repo.ImportKubernetesServices(fixture.ctx, fixture.resolve(t, fixture.input))
	if err != nil {
		t.Fatal(err)
	}
	if repeat.Application.Created || repeat.Services[0].Created || repeat.Targets[0].Created || repeat.Targets[0].ID != fixture.first.Targets[0].ID || repeat.Services[0].ID != fixture.first.Services[0].ID {
		t.Fatalf("repeat=%+v", repeat)
	}
	var enabled bool
	var executor string
	if err := fixture.store.DB().Raw(`SELECT enabled, executor_kind FROM release_targets WHERE id = ?`, fixture.first.Targets[0].ID).Row().Scan(&enabled, &executor); err != nil {
		t.Fatal(err)
	}
	if enabled || executor != "observe_only" {
		t.Fatalf("observe-only enabled=%v executor=%s", enabled, executor)
	}
}

func (fixture *importIdentityFixture) checkUIDReplacementRollback(t *testing.T) {
	before := fixture.targetSnapshot(t)
	changed := fixture.resolve(t, fixture.input)
	workload := changed.Workloads[0]
	source := *workload.Source
	source.Resource.UID = "replacement"
	workload.Source = &source
	changed.Workloads = []domaindelivery.KubernetesWorkloadImportInput{workload}
	if _, err := fixture.repo.ImportKubernetesServices(fixture.ctx, changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("replacement=%v", err)
	}
	if fixture.targetSnapshot(t) != before {
		t.Fatal("denied import modified the linked target")
	}
}

func (fixture *importIdentityFixture) checkOwnershipModeNoUpgrade(t *testing.T) {
	before := fixture.targetSnapshot(t)
	changed := fixture.resolve(t, fixture.input)
	changed.OwnershipMode = "managed"
	if _, err := fixture.repo.ImportKubernetesServices(fixture.ctx, changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("upgraded observe-only=%v", err)
	}
	if fixture.targetSnapshot(t) != before {
		t.Fatal("denied import modified the linked target")
	}
}

func (fixture *importIdentityFixture) checkPreservesServiceMetadata(t *testing.T) {
	if err := fixture.store.DB().Exec(`UPDATE application_services SET metadata = metadata || '{"operatorNote":"keep"}'::jsonb WHERE id = ?`, fixture.first.Services[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.ImportKubernetesServices(fixture.ctx, fixture.resolve(t, fixture.input)); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := fixture.store.DB().Raw(`SELECT metadata FROM application_services WHERE id = ?`, fixture.first.Services[0].ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if json.Unmarshal(raw, &metadata) != nil || metadata["operatorNote"] != "keep" {
		t.Fatalf("metadata=%s", raw)
	}
}

func (fixture *importIdentityFixture) checkDestinationRaceRollback(t *testing.T) {
	changed := fixture.input
	changed.ApplicationKey += "-race"
	changed.EnvironmentKey += "-race"
	changed = fixture.resolve(t, changed)
	appID := uuid.NewString()
	if err := fixture.store.DB().Exec(`INSERT INTO applications (id,app_key,name,app_group,language) VALUES (?,?,'Unapproved concurrent app','imported','kubernetes')`, appID, changed.ApplicationKey).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.ImportKubernetesServices(fixture.ctx, changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("race=%v", err)
	}
	var count int
	if err := fixture.store.DB().Raw(`SELECT count(*) FROM delivery_environments WHERE environment_key = ?`, changed.EnvironmentKey).Row().Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed import left an environment")
	}
}

func (fixture *importIdentityFixture) checkCrossNamespaceIdentity(t *testing.T) {
	changed := fixture.resolve(t, fixture.input)
	changed.Namespace = "other"
	workload := changed.Workloads[0]
	source := *workload.Source
	source.Resource.Namespace = "other"
	source.Resource.UID = "other-ns"
	workload.Source = &source
	changed.Workloads = []domaindelivery.KubernetesWorkloadImportInput{workload}
	result, err := fixture.repo.ImportKubernetesServices(fixture.ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if result.Services[0].ID == fixture.first.Services[0].ID || result.Services[0].Key == fixture.first.Services[0].Key {
		t.Fatal("same-name resources collided across namespaces")
	}
}

func (fixture *importIdentityFixture) checkNativeOwnershipConflict(t *testing.T) {
	changed := fixture.input
	changed.ApplicationKey += "-other"
	if _, err := fixture.repo.ImportKubernetesServices(fixture.ctx, fixture.resolve(t, changed)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("native ownership conflict=%v", err)
	}
	var count int
	if err := fixture.store.DB().Raw(`SELECT count(*) FROM applications WHERE app_key = ?`, changed.ApplicationKey).Row().Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed ownership import left an application")
	}
}

func (fixture *importIdentityFixture) checkCRDIdentityAndSharedReference(t *testing.T) {
	changed := fixture.resolve(t, fixture.input)
	source := *fixture.input.Workloads[0].Source
	source.Resource.APIVersion = "example.com/v1"
	source.Resource.Kind = "Widget"
	source.Resource.UID = "cr-uid"
	source.CRDName = "widgets.example.com"
	source.Plural = "widgets"
	changed.Workloads = []domaindelivery.KubernetesWorkloadImportInput{{WorkloadKind: "CustomResource", WorkloadName: "api", CRDName: source.CRDName, Source: &source}}
	one, err := fixture.repo.ImportKubernetesServices(fixture.ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	changed.ApplicationKey += "-shared"
	changed = fixture.resolve(t, changed)
	two, err := fixture.repo.ImportKubernetesServices(fixture.ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if two.Targets[0].ID == one.Targets[0].ID || two.Targets[0].Source.Resource.UID != one.Targets[0].Source.Resource.UID {
		t.Fatal("shared reference lost binding isolation")
	}
	changed = fixture.resolve(t, fixture.input)
	otherSource := source
	otherSource.CRDName = "widgets.other.com"
	otherSource.Resource.APIVersion = "other.com/v1"
	otherSource.Resource.UID = "other-cr"
	changed.Workloads = []domaindelivery.KubernetesWorkloadImportInput{{WorkloadKind: "CustomResource", WorkloadName: "api", CRDName: otherSource.CRDName, Source: &otherSource}}
	other, err := fixture.repo.ImportKubernetesServices(fixture.ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if other.Services[0].Key == one.Services[0].Key || other.Targets[0].ID == one.Targets[0].ID {
		t.Fatal("different CRDs collided")
	}
}

func (fixture *importIdentityFixture) checkLegacyMissingUIDConflict(t *testing.T) {
	changed := fixture.resolve(t, fixture.input)
	if err := fixture.store.DB().Exec(`UPDATE release_targets SET metadata = metadata - 'resourceUid' - 'resourceSource' WHERE id = ?`, fixture.first.Targets[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repo.ImportKubernetesServices(fixture.ctx, changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("legacy source silently rebound=%v", err)
	}
}

func (fixture *importIdentityFixture) targetSnapshot(t *testing.T) string {
	t.Helper()
	var snapshot string
	if err := fixture.store.DB().Raw(`SELECT row_to_json(release_targets)::text FROM release_targets WHERE id = ?`, fixture.first.Targets[0].ID).Row().Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
