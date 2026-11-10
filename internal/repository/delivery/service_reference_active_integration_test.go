package delivery_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	catalogrepo "github.com/opensoha/soha/internal/repository/catalog"
	deliveryrepo "github.com/opensoha/soha/internal/repository/delivery"
	manifestrepo "github.com/opensoha/soha/internal/repository/manifest"
	"github.com/opensoha/soha/internal/repository/servicereferencelock"
)

type activeReferencePGFixture struct {
	referenceCandidatePGFixture
	write dd.ServiceReferenceWrite
}

func newActiveReferencePGFixture(t *testing.T, ctx context.Context, store *dbstore.Store) activeReferencePGFixture {
	t.Helper()
	base := newReferenceCandidatePGFixture(t, ctx, store)
	candidate := base.write(t)
	if err := store.DB().Exec(`DELETE FROM release_targets WHERE application_environment_id=?`, base.consumer.ApplicationEnvironmentID).Error; err != nil {
		t.Fatal(err)
	}
	configuration, err := base.configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := configuration.Revision()
	if err != nil {
		t.Fatal(err)
	}
	candidate.Candidate.ContextRevision = revision
	candidate.Candidate.Preview.CurrentLocalTargetIDs = []string{}
	saved, err := base.repo.SaveServiceReferenceCandidate(ctx, candidate, base.configuration)
	if err != nil {
		t.Fatal(err)
	}
	control, err := dd.ServiceReferenceControlRevision(configuration.Source, configuration.Service.ID)
	if err != nil {
		t.Fatal(err)
	}
	consumerRevision, err := (dd.ServiceReferenceConfiguration{Consumer: configuration.Consumer}).Revision()
	if err != nil {
		t.Fatal(err)
	}
	record := dd.ServiceReferenceRecord{Reference: dd.ServiceReference{Preview: saved.Preview, SourceIdentityRevision: saved.SourceIdentityRevision}, ApplicationID: configuration.Consumer.ApplicationID, ConsumerBindingID: configuration.Consumer.ID, ServiceID: configuration.Service.ID, SourceBindingID: configuration.Source.ID, SourceTargetIDs: []string{configuration.Source.Targets[0].ID}, ControlRevision: control, ConsumerConfigurationRevision: consumerRevision}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM execution_tasks WHERE application_id=?`, record.ApplicationID).Error
		_ = store.DB().Exec(`DELETE FROM delivery_plans WHERE application_id=?`, record.ApplicationID).Error
		_ = store.DB().Exec(`DELETE FROM environment_service_references WHERE application_id=?`, record.ApplicationID).Error
	})
	return activeReferencePGFixture{base, dd.ServiceReferenceWrite{Record: record, ExpectedCandidateID: saved.ID, ExpectedCandidateRevision: saved.Revision, ConfigurationRevision: revision}}
}
func (f activeReferencePGFixture) activate(t *testing.T) dd.ServiceReferenceRecord {
	t.Helper()
	saved, err := f.repo.SaveServiceReference(f.ctx, f.write, f.configuration)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}
func (f activeReferencePGFixture) remove(saved dd.ServiceReferenceRecord) dd.ServiceReferenceDeleteInput {
	return dd.ServiceReferenceDeleteInput{ExpectedReferenceID: saved.Reference.ID, ExpectedRevision: saved.Reference.Revision, ExpectedConsumerUpdatedAt: saved.Reference.Preview.Consumer.UpdatedAt}
}
func (f activeReferencePGFixture) assertIndex(t *testing.T, count int64) {
	t.Helper()
	var n int64
	if err := f.store.DB().Raw(`SELECT count(*) FROM environment_service_reference_targets i JOIN environment_service_references r ON r.id=i.reference_id WHERE r.application_id=?`, f.source.Application.ID).Row().Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != count {
		t.Fatalf("index count=%d want=%d", n, count)
	}
}

func TestServiceReferencesPostgres(t *testing.T) {
	store := openDraftReceiptTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func(*testing.T, context.Context, *dbstore.Store)
	}{
		{"ActivationAndRestart", activePGActivationAndRestart},
		{"CASAndABA", activePGCASAndABA},
		{"SourceLifecycle", activePGSourceLifecycle},
		{"ConsumerSlotAndDirectSource", activePGConsumerSlotAndDirectSource},
		{"BusyPublication", activePGBusyPublication},
		{"ConcurrentActivation", activePGConcurrentActivation},
		{"RebindVsDetachCAS", activePGRebindVsDetachCAS},
		{"ActivationVsSourceMutation", activePGActivationVsSourceMutation},
		{"CatalogFenceCancellation", activePGCatalogFenceCancellation},
		{"CatalogFenceReassignment", activePGCatalogFenceReassignment},
		{"ConsumerCleanup", activePGConsumerCleanup},
		{"Pagination", activePGPagination},
		{"TransactionRollback", activePGTransactionRollback},
		{"BoundCompleteSet", activePGBoundCompleteSet},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { test.run(t, ctx, store) })
	}
}

func activePGActivationAndRestart(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	before := f.snapshot(t)
	saved := f.activate(t)
	restored, err := deliveryrepo.New(store.DB()).GetServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID)
	if err != nil || saved.Reference.ID != restored.Reference.ID || restored.Reference.State != "active" || restored.ControlRevision != saved.ControlRevision {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	if f.snapshot(t) != before {
		t.Fatal("activation mutated source/consumer target configuration")
	}
	f.assertIndex(t, 1)
	consumers, err := f.repo.ListSourceServiceReferences(ctx, f.source.ApplicationEnvironmentID, f.source.Services[0].ID)
	if err != nil || len(consumers) != 1 || consumers[0].Reference.ID != saved.Reference.ID {
		t.Fatalf("consumers=%+v err=%v", consumers, err)
	}
	environment := consumers[0].ConsumerEnvironment
	if environment.ID != f.consumer.Environment.ID || environment.Key != f.consumer.Environment.Key || environment.Tier != "test" || !environment.Enabled || environment.IsProduction || environment.StageLevel != 20 {
		t.Fatalf("current private consumer environment=%+v", environment)
	}
	initialDigest := consumers[0].ConsumerConfigurationRevision
	if err := store.DB().Exec(`UPDATE application_environments SET updated_at=updated_at+interval '1 second' WHERE id=?`, f.consumer.ApplicationEnvironmentID).Error; err != nil {
		t.Fatal(err)
	}
	consumers, err = f.repo.ListSourceServiceReferences(ctx, f.source.ApplicationEnvironmentID, "")
	if err != nil || len(consumers) != 1 || consumers[0].ConsumerConfigurationRevision != initialDigest {
		t.Fatalf("ordinary timestamp changed digest or wildcard lost records: %v", err)
	}
	if err := store.DB().Exec(`UPDATE application_environments SET alias='changed consumer policy' WHERE id=?`, f.consumer.ApplicationEnvironmentID).Error; err != nil {
		t.Fatal(err)
	}
	consumers, err = f.repo.ListSourceServiceReferences(ctx, f.source.ApplicationEnvironmentID, "")
	if err != nil || consumers[0].ConsumerConfigurationRevision == initialDigest {
		t.Fatalf("consumer configuration drift not reflected: %v", err)
	}
}

func activePGCASAndABA(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	saved := f.activate(t)
	stale := f.write
	stale.ExpectedCandidateRevision++
	if _, err := f.repo.SaveServiceReference(ctx, stale, f.configuration); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("candidate CAS=%v", err)
	}
	stale = f.write
	stale.ExpectedRevision = saved.Reference.Revision
	stale.ExpectedReferenceID = saved.Reference.ID
	next, err := f.repo.SaveServiceReference(ctx, stale, f.configuration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.SaveServiceReference(ctx, stale, f.configuration); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("reference CAS=%v", err)
	}
	if err := f.repo.DeleteServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(next), candidatePGAuthorize); err != nil {
		t.Fatal(err)
	}
	replacement := f.activate(t)
	if replacement.Reference.ID == saved.Reference.ID {
		t.Fatal("ABA identity reused")
	}
	if err := f.repo.DeleteServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(saved), candidatePGAuthorize); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("ABA delete=%v", err)
	}
	f.assertIndex(t, 1)
}

func activePGSourceLifecycle(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	packageID, bindingID := uuid.NewString(), uuid.NewString()
	if err := store.DB().Exec(`INSERT INTO manifest_packages(id,name,application_id,service_id,current_revision,status) VALUES(?,'Reference source',?,?,1,'published')`, packageID, f.source.Application.ID, f.source.Services[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`INSERT INTO manifest_bindings(id,package_id,application_environment_id,environment_key,cluster_id,namespace) VALUES(?,?,?,?,?,'source')`, bindingID, packageID, f.source.ApplicationEnvironmentID, f.source.Environment.Key, f.clusterID).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM environment_service_references WHERE application_id=?`, f.source.Application.ID).Error
		_ = store.DB().Exec(`DELETE FROM manifest_packages WHERE id=?`, packageID).Error
	})
	if err := store.DB().Exec(`UPDATE release_targets SET config_ref=? WHERE id=?`, bindingID, f.source.Targets[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	configuration, err := f.configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := configuration.Revision()
	if err != nil {
		t.Fatal(err)
	}
	// Candidate must reflect the latest database configuration before activation.
	if err := store.DB().Exec(`UPDATE environment_service_reference_candidates SET context_revision=? WHERE id=?`, revision, f.write.ExpectedCandidateID).Error; err != nil {
		t.Fatal(err)
	}
	f.write.ConfigurationRevision = revision
	f.write.Record.ControlRevision, err = dd.ServiceReferenceControlRevision(configuration.Source, configuration.Service.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved := f.activate(t)
	for _, query := range []string{`UPDATE manifest_packages SET archived_at=now() WHERE id=?`, `DELETE FROM manifest_packages WHERE id=?`, `UPDATE manifest_packages SET renderer='kustomize' WHERE id=?`} {
		if err := store.DB().Exec(query, packageID).Error; err == nil {
			t.Fatalf("Manifest structural mutation accepted: %s", query)
		}
	}
	if err := store.DB().Exec(`UPDATE manifest_bindings SET namespace='relocated' WHERE id=?`, bindingID).Error; err == nil {
		t.Fatal("Manifest binding relocation accepted")
	}
	if err := store.DB().Exec(`UPDATE manifest_packages SET current_revision=2,updated_at=now() WHERE id=?`, packageID).Error; err != nil {
		t.Fatalf("ordinary Manifest publish blocked: %v", err)
	}
	if err := store.DB().Exec(`UPDATE manifest_bindings SET overlay='{"replicas":"2"}'::jsonb,version=version+1 WHERE id=?`, bindingID).Error; err != nil {
		t.Fatalf("ordinary Manifest config blocked: %v", err)
	}
	binding, err := manifestrepo.New(store.DB()).GetBinding(ctx, bindingID)
	if err != nil || binding.Overlay["replicas"] != "2" {
		t.Fatalf("ordinary Manifest config typed roundtrip=%+v err=%v", binding, err)
	}
	targetID := f.source.Targets[0].ID
	for _, query := range []string{`DELETE FROM release_targets WHERE id=?`, `UPDATE release_targets SET namespace='relocated' WHERE id=?`, `UPDATE release_targets SET executor_kind='upgraded' WHERE id=?`, `UPDATE release_targets SET config_ref='replacement' WHERE id=?`, `UPDATE release_targets SET metadata=jsonb_set(metadata,'{ownershipMode}','"managed"') WHERE id=?`} {
		if err := store.DB().Exec(query, targetID).Error; err == nil {
			t.Fatalf("source structural mutation accepted: %s", query)
		}
	}
	catalog := catalogrepo.New(store.DB())
	if err := catalog.DeleteApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("source deletion=%v", err)
	}
	source, err := catalog.GetApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID)
	if err != nil {
		t.Fatal(err)
	}
	input := dc.ApplicationEnvironmentInput{ApplicationID: source.ApplicationID, EnvironmentID: source.EnvironmentID, Alias: source.Alias, ClusterID: source.ClusterID, Namespace: source.Namespace, Targets: []dc.ReleaseTargetInput{source.Targets[0].Input()}, ExpectedUpdatedAt: &source.UpdatedAt}
	updated, err := catalog.UpdateApplicationEnvironment(ctx, source.ID, input)
	if err != nil {
		t.Fatalf("safe configuration save=%v", err)
	}
	if len(updated.Targets) != 1 || updated.Targets[0].ID != targetID || !updated.Targets[0].CreatedAt.Equal(source.Targets[0].CreatedAt) {
		t.Fatal("safe save replaced source identity")
	}
	input.ExpectedUpdatedAt = &updated.UpdatedAt
	input.Targets = []dc.ReleaseTargetInput{}
	if _, err := catalog.UpdateApplicationEnvironment(ctx, source.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("replacement=%v", err)
	}
	if err := f.repo.DeleteServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(saved), candidatePGAuthorize); err != nil {
		t.Fatal(err)
	}
	// Detaching a reference does not erase the Manifest library's independent
	// RESTRICT dependency; remove that binding through its owning repository.
	if err := catalog.DeleteApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("Manifest dependency after detach=%v", err)
	}
	if err := manifestrepo.New(store.DB()).DeleteBinding(ctx, bindingID); err != nil {
		t.Fatalf("delete independent Manifest binding after detach=%v", err)
	}
	if err := store.DB().Exec(`UPDATE manifest_packages SET archived_at=now() WHERE id=?`, packageID).Error; err != nil {
		t.Fatalf("archive after detach=%v", err)
	}
	if err := catalog.DeleteApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID); err != nil {
		t.Fatalf("source deletion after detach=%v", err)
	}
}

func activePGConsumerSlotAndDirectSource(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	f.activate(t)
	if err := store.DB().Exec(`INSERT INTO release_targets(id,application_environment_id,namespace,target_kind,executor_kind,workload_kind,workload_name,metadata,enabled) SELECT ?,?,namespace,target_kind,executor_kind,workload_kind,workload_name,metadata,enabled FROM release_targets WHERE id=?`, uuid.NewString(), f.consumer.ApplicationEnvironmentID, f.source.Targets[0].ID).Error; err == nil {
		t.Fatal("owning target coexists with active slot")
	}
	var raw string
	if err := store.DB().Raw(`SELECT preview::text FROM environment_service_references WHERE consumer_binding_id=?`, f.consumer.ApplicationEnvironmentID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	// Direct SQL cannot turn a consumer into a source, even with a forged preview.
	err := store.DB().Exec(`INSERT INTO environment_service_references SELECT ?,application_id,source_binding_id,service_id,consumer_binding_id,1,jsonb_set(jsonb_set(preview,'{consumer,applicationEnvironmentId}',to_jsonb(source_binding_id)),'{source,applicationEnvironmentId}',to_jsonb(consumer_binding_id)),source_identity_revision,control_revision,consumer_configuration_revision,created_at,updated_at FROM environment_service_references WHERE consumer_binding_id=?`, uuid.NewString(), f.consumer.ApplicationEnvironmentID).Error
	if err == nil {
		t.Fatal("ref to ref accepted")
	}
	f.assertIndex(t, 1)
}

func activePGBusyPublication(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	saved := f.activate(t)
	planID := uuid.NewString()
	if err := store.DB().Exec(`INSERT INTO delivery_plans(id,application_id,application_environment_id,status,action) VALUES(?,?,?,'confirming','deploy')`, planID, f.source.Application.ID, f.source.ApplicationEnvironmentID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.repo.DeleteServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(saved), candidatePGAuthorize); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("confirming detach=%v", err)
	}
	if err := store.DB().Exec(`UPDATE delivery_plans SET status='confirmed' WHERE id=?`, planID).Error; err != nil {
		t.Fatal(err)
	}
	taskID := uuid.NewString()
	for _, status := range []string{"dispatching", "running", "canceling"} {
		if err := store.DB().Exec(`INSERT INTO execution_tasks(id,application_id,application_environment_id,task_kind,provider_kind,status) VALUES(?,?,?,'manifest_deploy','kubernetes',?) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status`, taskID, f.source.Application.ID, f.source.ApplicationEnvironmentID, status).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.repo.DeleteServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(saved), candidatePGAuthorize); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("%s detach=%v", status, err)
		}
	}
	if err := store.DB().Exec(`UPDATE execution_tasks SET status='queued' WHERE id=?`, taskID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.repo.DeleteServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(saved), candidatePGAuthorize); err != nil {
		t.Fatalf("queued and old confirmed history must not block=%v", err)
	}
}

func activePGConcurrentActivation(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	entered := make(chan struct{})
	release := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := f.repo.SaveServiceReference(ctx, f.write, func(txctx context.Context) (dd.ServiceReferenceConfiguration, error) {
			close(entered)
			<-release
			return f.configuration(txctx)
		})
		results <- err
	}()
	<-entered
	go func() {
		defer wg.Done()
		_, err := f.repo.SaveServiceReference(ctx, f.write, f.configuration)
		results <- err
	}()
	close(release)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, apperrors.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	f.assertIndex(t, 1)
}

func activePGConsumerCleanup(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	saved := f.activate(t)
	if err := store.DB().Exec(`UPDATE application_services SET enabled=false WHERE id=?`, f.source.Services[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.repo.DeleteServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(saved), candidatePGAuthorize); err != nil {
		t.Fatal(err)
	}
	f.assertIndex(t, 0)
}

func activePGPagination(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	saved := f.activate(t)
	page, err := f.repo.ListServiceReferences(ctx, f.consumer.ApplicationEnvironmentID, f.source.Application.ID, "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != saved.Reference.ID || page.NextCursor != "" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	empty, err := f.repo.ListServiceReferences(ctx, f.consumer.ApplicationEnvironmentID, "other-app", "", 1)
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("cross app page=%+v err=%v", empty, err)
	}
	empty, err = f.repo.ListServiceReferences(ctx, f.consumer.ApplicationEnvironmentID, f.source.Application.ID, page.Items[0].ServiceID, 1)
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("cursor page=%+v err=%v", empty, err)
	}
}

func activePGTransactionRollback(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	before := f.snapshot(t)
	_, err := f.repo.SaveServiceReference(ctx, f.write, func(txctx context.Context) (dd.ServiceReferenceConfiguration, error) {
		if err := dbtx.DB(txctx, store.DB()).Exec(`UPDATE application_environments SET alias='must rollback' WHERE id=?`, f.consumer.ApplicationEnvironmentID).Error; err != nil {
			return dd.ServiceReferenceConfiguration{}, err
		}
		return dd.ServiceReferenceConfiguration{}, apperrors.ErrAccessDenied
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal(err)
	}
	if f.snapshot(t) != before {
		t.Fatal("failed reauthorization committed changes")
	}
	f.assertIndex(t, 0)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.repo.SaveServiceReference(canceled, f.write, f.configuration); err == nil {
		t.Fatal("canceled activation accepted")
	}
}

func activePGBoundCompleteSet(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, ctx, store)
	f.activate(t)
	// Clone independent consumer bindings to exercise a complete, bounded private set.
	if err := store.DB().Exec(`INSERT INTO delivery_environments(id,environment_key,name,tier,stage_level) SELECT ?||n,?||n,'Bound consumer','test',20 FROM generate_series(1,1000)n`, f.clusterID, f.clusterID).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM application_environments WHERE id LIKE ?`, f.clusterID+"-bound-%").Error
		_ = store.DB().Exec(`DELETE FROM delivery_environments WHERE id LIKE ?`, f.clusterID+"%").Error
	})
	if err := store.DB().Exec(`INSERT INTO application_environments(id,application_id,environment_id) SELECT ?||'-bound-'||n,?,?||n FROM generate_series(1,1000)n`, f.clusterID, f.source.Application.ID, f.clusterID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`INSERT INTO environment_service_references SELECT ?||'-ref-'||n,application_id,?||'-bound-'||n,service_id,source_binding_id,revision,jsonb_set(preview,'{consumer,applicationEnvironmentId}',to_jsonb(?||'-bound-'||n)),source_identity_revision,control_revision,consumer_configuration_revision,created_at,updated_at FROM environment_service_references CROSS JOIN generate_series(1,1000)n WHERE consumer_binding_id=?`, f.clusterID, f.clusterID, f.clusterID, f.consumer.ApplicationEnvironmentID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ListSourceServiceReferences(ctx, f.source.ApplicationEnvironmentID, f.source.Services[0].ID); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("overflow=%v", err)
	}
	if err := store.DB().Exec(`DELETE FROM environment_service_references WHERE id=?`, fmt.Sprintf("%s-ref-1000", f.clusterID)).Error; err != nil {
		t.Fatal(err)
	}
	items, err := f.repo.ListSourceServiceReferences(ctx, f.source.ApplicationEnvironmentID, f.source.Services[0].ID)
	if err != nil || len(items) != 1000 {
		t.Fatalf("bounded complete count=%d err=%v", len(items), err)
	}
}

// The session label and backend PID let the observer prove a real database wait,
// rather than infer ordering from goroutine scheduling or an elapsed delay.
type activePGHeldReference struct {
	pid       int
	reference dd.ServiceReferenceRecord
}

func activePGIdentifySession(ctx context.Context, store *dbstore.Store, label string) (int, error) {
	tx := dbtx.DB(ctx, store.DB())
	if err := tx.Exec(`SELECT set_config('application_name',?,true)`, label).Error; err != nil {
		return 0, err
	}
	var pid int
	err := tx.Raw(`SELECT pg_backend_pid()`).Row().Scan(&pid)
	return pid, err
}

func activePGWaitForHeld(t *testing.T, ctx context.Context, held <-chan activePGHeldReference, done <-chan error) activePGHeldReference {
	t.Helper()
	select {
	case value := <-held:
		return value
	case err := <-done:
		t.Fatalf("transaction finished before holding its scope: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return activePGHeldReference{}
}

func activePGWaitForPID(t *testing.T, ctx context.Context, pids <-chan int, done <-chan error) int {
	t.Helper()
	select {
	case pid := <-pids:
		return pid
	case err := <-done:
		t.Fatalf("contender finished before identifying its session: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return 0
}

func activePGWaitForBlocking(t *testing.T, ctx context.Context, store *dbstore.Store, waiterPID, blockerPID int, label string, done <-chan error) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err := store.DB().WithContext(ctx).Raw(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=? AND application_name=? AND wait_event_type='Lock' AND ? = ANY(pg_blocking_pids(pid)))`, waiterPID, label, blockerPID).Row().Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("contender completed without the required database lock wait: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func activePGWaitForResult(t *testing.T, ctx context.Context, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return ctx.Err()
}

func activePGRebindVsDetachCAS(t *testing.T, ctx context.Context, store *dbstore.Store) {
	// Committing revision 2 fences an already-read revision-1 detach. Rolling the
	// rebind back restores revision 1 and its source configuration atomically.
	for _, rollback := range []bool{false, true} {
		activePGRebindVsDetachOutcome(t, ctx, store, rollback)
	}
}

func activePGRebindVsDetachOutcome(t *testing.T, parent context.Context, store *dbstore.Store, rollback bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	f := newActiveReferencePGFixture(t, ctx, store)
	original := f.activate(t)
	before := f.snapshot(t)
	write := f.write
	write.ExpectedReferenceID = original.Reference.ID
	write.ExpectedRevision = original.Reference.Revision
	held := make(chan activePGHeldReference, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unlock := func() { releaseOnce.Do(func() { close(release) }) }
	var workers sync.WaitGroup
	defer func() { unlock(); cancel(); workers.Wait() }()
	firstDone := make(chan error, 1)
	rollbackErr := errors.New("intentional rebind rollback")
	firstLabel := "r5-rebind-" + uuid.NewString()
	workers.Add(1)
	go func() {
		defer workers.Done()
		firstDone <- f.repo.WithServiceReferenceScope(ctx, f.source.Application.ID, func(txctx context.Context) error {
			pid, err := activePGIdentifySession(txctx, store, firstLabel)
			if err != nil {
				return err
			}
			saved, err := f.repo.SaveServiceReference(txctx, write, f.configuration)
			if err != nil {
				return err
			}
			if rollback {
				if err := dbtx.DB(txctx, store.DB()).Exec(`UPDATE application_environments SET alias='rollback source with rebind' WHERE id=?`, f.source.ApplicationEnvironmentID).Error; err != nil {
					return err
				}
			}
			held <- activePGHeldReference{pid: pid, reference: saved}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			if rollback {
				return rollbackErr
			}
			return nil
		})
	}()
	first := activePGWaitForHeld(t, ctx, held, firstDone)
	if first.reference.Reference.Revision != original.Reference.Revision+1 {
		t.Fatal("rebind did not stage revision 2")
	}
	pids := make(chan int, 1)
	secondDone := make(chan error, 1)
	secondLabel := "r5-detach-" + uuid.NewString()
	workers.Add(1)
	go func() {
		defer workers.Done()
		secondDone <- dbtx.Within(ctx, store.DB(), func(txctx context.Context) error {
			pid, err := activePGIdentifySession(txctx, store, secondLabel)
			if err != nil {
				return err
			}
			// This MVCC read must still see committed revision 1 while revision 2 is
			// uncommitted; DeleteServiceReference then waits for the application scope.
			committed, err := f.repo.GetServiceReference(txctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID)
			if err != nil {
				return err
			}
			if committed.Reference.ID != original.Reference.ID || committed.Reference.Revision != original.Reference.Revision {
				return fmt.Errorf("detach did not read the expected committed reference")
			}
			pids <- pid
			authorize := candidatePGAuthorize
			if rollback {
				authorize = func(context.Context) error { return apperrors.ErrAccessDenied }
			}
			return f.repo.DeleteServiceReference(txctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(original), authorize)
		})
	}()
	secondPID := activePGWaitForPID(t, ctx, pids, secondDone)
	activePGWaitForBlocking(t, ctx, store, secondPID, first.pid, secondLabel, secondDone)
	unlock()
	firstErr := activePGWaitForResult(t, ctx, firstDone)
	secondErr := activePGWaitForResult(t, ctx, secondDone)
	expectedRevision := first.reference.Reference.Revision
	if rollback {
		if !errors.Is(firstErr, rollbackErr) || !errors.Is(secondErr, apperrors.ErrAccessDenied) {
			t.Fatalf("rollback rebind=%v detach=%v", firstErr, secondErr)
		}
		expectedRevision = original.Reference.Revision
	} else if firstErr != nil || !errors.Is(secondErr, apperrors.ErrConflict) {
		t.Fatalf("committed rebind=%v stale detach=%v", firstErr, secondErr)
	}
	current, err := f.repo.GetServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID)
	if err != nil || current.Reference.ID != original.Reference.ID || current.Reference.Revision != expectedRevision || current.SourceBindingID != original.SourceBindingID {
		t.Fatalf("reference after interleaving=%+v err=%v", current, err)
	}
	if f.snapshot(t) != before {
		t.Fatal("detach conflict or rollback changed source/consumer configuration")
	}
	f.assertIndex(t, 1)
}

func activePGSourceMutationInput(t *testing.T, f activeReferencePGFixture) dc.ApplicationEnvironmentInput {
	t.Helper()
	configuration, err := f.configuration(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	source := configuration.Source
	target := source.Targets[0].Input()
	target.Namespace = "relocated-source"
	return dc.ApplicationEnvironmentInput{ApplicationID: source.ApplicationID, EnvironmentID: source.EnvironmentID, Alias: source.Alias, ClusterID: source.ClusterID, Namespace: source.Namespace, Targets: []dc.ReleaseTargetInput{target}, ExpectedUpdatedAt: &source.UpdatedAt}
}

func activePGActivationVsSourceMutation(t *testing.T, ctx context.Context, store *dbstore.Store) {
	activePGActivationCommitsBeforeSourceMutation(t, ctx, store)
	activePGSourceMutationCommitsBeforeActivation(t, ctx, store)
}

func activePGActivationCommitsBeforeSourceMutation(t *testing.T, parent context.Context, store *dbstore.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	f := newActiveReferencePGFixture(t, ctx, store)
	before := f.snapshot(t)
	mutation := activePGSourceMutationInput(t, f)
	held := make(chan activePGHeldReference, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unlock := func() { releaseOnce.Do(func() { close(release) }) }
	var workers sync.WaitGroup
	defer func() { unlock(); cancel(); workers.Wait() }()
	firstDone := make(chan error, 1)
	firstLabel := "r5-activate-" + uuid.NewString()
	workers.Add(1)
	go func() {
		defer workers.Done()
		firstDone <- f.repo.WithServiceReferenceScope(ctx, f.source.Application.ID, func(txctx context.Context) error {
			pid, err := activePGIdentifySession(txctx, store, firstLabel)
			if err != nil {
				return err
			}
			saved, err := f.repo.SaveServiceReference(txctx, f.write, f.configuration)
			if err != nil {
				return err
			}
			held <- activePGHeldReference{pid: pid, reference: saved}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	first := activePGWaitForHeld(t, ctx, held, firstDone)
	pids := make(chan int, 1)
	secondDone := make(chan error, 1)
	secondLabel := "r5-edit-after-" + uuid.NewString()
	workers.Add(1)
	go func() {
		defer workers.Done()
		secondDone <- dbtx.Within(ctx, store.DB(), func(txctx context.Context) error {
			pid, err := activePGIdentifySession(txctx, store, secondLabel)
			if err != nil {
				return err
			}
			pids <- pid
			_, err = catalogrepo.New(store.DB()).UpdateApplicationEnvironment(txctx, f.source.ApplicationEnvironmentID, mutation)
			return err
		})
	}()
	secondPID := activePGWaitForPID(t, ctx, pids, secondDone)
	activePGWaitForBlocking(t, ctx, store, secondPID, first.pid, secondLabel, secondDone)
	unlock()
	if err := activePGWaitForResult(t, ctx, firstDone); err != nil {
		t.Fatal(err)
	}
	if err := activePGWaitForResult(t, ctx, secondDone); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("source mutation after activation=%v", err)
	}
	current, err := f.repo.GetServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID)
	if err != nil || current.Reference.ID != first.reference.Reference.ID || current.Reference.Revision != 1 {
		t.Fatalf("committed activation=%+v err=%v", current, err)
	}
	if f.snapshot(t) != before {
		t.Fatal("rejected source edit changed source identity or consumer configuration")
	}
	f.assertIndex(t, 1)
}

func activePGSourceMutationCommitsBeforeActivation(t *testing.T, parent context.Context, store *dbstore.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	f := newActiveReferencePGFixture(t, ctx, store)
	mutation := activePGSourceMutationInput(t, f)
	held := make(chan activePGHeldReference, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unlock := func() { releaseOnce.Do(func() { close(release) }) }
	var workers sync.WaitGroup
	defer func() { unlock(); cancel(); workers.Wait() }()
	firstDone := make(chan error, 1)
	firstLabel := "r5-edit-before-" + uuid.NewString()
	workers.Add(1)
	go func() {
		defer workers.Done()
		firstDone <- dbtx.Within(ctx, store.DB(), func(txctx context.Context) error {
			pid, err := activePGIdentifySession(txctx, store, firstLabel)
			if err != nil {
				return err
			}
			if _, err := catalogrepo.New(store.DB()).UpdateApplicationEnvironment(txctx, f.source.ApplicationEnvironmentID, mutation); err != nil {
				return err
			}
			held <- activePGHeldReference{pid: pid}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	first := activePGWaitForHeld(t, ctx, held, firstDone)
	pids := make(chan int, 1)
	secondDone := make(chan error, 1)
	secondLabel := "r5-activate-after-" + uuid.NewString()
	workers.Add(1)
	go func() {
		defer workers.Done()
		secondDone <- dbtx.Within(ctx, store.DB(), func(txctx context.Context) error {
			pid, err := activePGIdentifySession(txctx, store, secondLabel)
			if err != nil {
				return err
			}
			pids <- pid
			_, err = f.repo.SaveServiceReference(txctx, f.write, f.configuration)
			return err
		})
	}()
	secondPID := activePGWaitForPID(t, ctx, pids, secondDone)
	activePGWaitForBlocking(t, ctx, store, secondPID, first.pid, secondLabel, secondDone)
	unlock()
	if err := activePGWaitForResult(t, ctx, firstDone); err != nil {
		t.Fatal(err)
	}
	if err := activePGWaitForResult(t, ctx, secondDone); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("old candidate after source mutation=%v", err)
	}
	source, err := catalogrepo.New(store.DB()).GetApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID)
	if err != nil || len(source.Targets) != 1 || source.Targets[0].ID != f.source.Targets[0].ID || source.Targets[0].Namespace != mutation.Targets[0].Namespace {
		t.Fatalf("source edit not retained=%+v err=%v", source, err)
	}
	if _, err := f.repo.GetServiceReference(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("stale activation left a reference: %v", err)
	}
	f.assertIndex(t, 0)
}

func activePGCatalogFenceCancellation(t *testing.T, parent context.Context, store *dbstore.Store) {
	f := newActiveReferencePGFixture(t, parent, store)
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	mutation := activePGSourceMutationInput(t, f)
	err := f.repo.WithServiceReferenceScope(ctx, f.source.Application.ID, func(txctx context.Context) error {
		blocker, err := activePGIdentifySession(txctx, store, "reference-fence-holder")
		if err != nil {
			return err
		}
		waitCtx, cancelWait := context.WithCancel(ctx)
		defer cancelWait()
		label := "reference-fence-waiter-" + uuid.NewString()
		pids, done := make(chan int, 1), make(chan error, 1)
		go func() {
			done <- dbtx.Within(waitCtx, store.DB(), func(waitCtx context.Context) error {
				pid, err := activePGIdentifySession(waitCtx, store, label)
				if err != nil {
					return err
				}
				pids <- pid
				_, err = catalogrepo.New(store.DB()).UpdateApplicationEnvironment(waitCtx, f.source.ApplicationEnvironmentID, mutation)
				return err
			})
		}()
		waiter := activePGWaitForPID(t, ctx, pids, done)
		activePGWaitForBlocking(t, ctx, store, waiter, blocker, label, done)
		// The old path waits in its row trigger and already holds this row.
		// An activation must still be able to lock it while Catalog waits.
		var id string
		if err := dbtx.DB(txctx, store.DB()).Raw(`SELECT id FROM application_environments WHERE id = ? FOR UPDATE NOWAIT`, f.source.ApplicationEnvironmentID).Row().Scan(&id); err != nil {
			return fmt.Errorf("waiting Catalog edit acquired the binding row before its application fence: %w", err)
		}
		cancelWait()
		if err := activePGWaitForResult(t, ctx, done); err == nil {
			return errors.New("canceled Catalog edit unexpectedly committed")
		}
		_, err = f.repo.SaveServiceReference(txctx, f.write, f.configuration)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Retrying the canceled mutation sees the newly activated protection.
	if _, err := catalogrepo.New(store.DB()).UpdateApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID, mutation); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("Catalog retry did not re-read the activated reference: %v", err)
	}
	f.assertIndex(t, 1)
}

func activePGCatalogFenceReassignment(t *testing.T, parent context.Context, store *dbstore.Store) {
	left := newReferenceCandidatePGFixture(t, parent, store)
	right := newReferenceCandidatePGFixture(t, parent, store)
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	bindings := []string{left.source.ApplicationEnvironmentID, right.source.ApplicationEnvironmentID}
	t.Cleanup(func() {
		if err := store.DB().Exec(`DELETE FROM application_environments WHERE id IN ?`, bindings).Error; err != nil {
			t.Error(err)
		}
	})
	apps := []string{left.source.Application.ID, right.source.Application.ID}
	ordered := slices.Clone(apps)
	slices.Sort(ordered)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	results := make([]chan error, len(apps))
	err := left.repo.WithServiceReferenceScope(ctx, ordered[0], func(txctx context.Context) error {
		blocker, err := activePGIdentifySession(txctx, store, "reference-reassignment-holder")
		if err != nil {
			return err
		}
		envs := []string{left.source.Environment.ID, right.source.Environment.ID}
		for i := range apps {
			label := "reference-reassignment-" + uuid.NewString()
			pids, done := make(chan int, 1), make(chan error, 1)
			results[i] = done
			workers.Add(1)
			go func() {
				defer workers.Done()
				done <- dbtx.Within(ctx, store.DB(), func(editCtx context.Context) error {
					pid, err := activePGIdentifySession(editCtx, store, label)
					if err != nil {
						return err
					}
					pids <- pid
					_, err = catalogrepo.New(store.DB()).UpdateApplicationEnvironment(editCtx, bindings[i], dc.ApplicationEnvironmentInput{
						ApplicationID: apps[1-i], EnvironmentID: envs[i],
					})
					return err
				})
			}()
			waiter := activePGWaitForPID(t, ctx, pids, done)
			activePGWaitForBlocking(t, ctx, store, waiter, blocker, label, done)
		}
		// Opposite old/new input order must leave the second key available.
		probeCtx, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		return dbtx.Within(probeCtx, store.DB(), func(probeCtx context.Context) error {
			return servicereferencelock.Applications(dbtx.DB(probeCtx, store.DB()), ordered[1])
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, done := range results {
		if err := activePGWaitForResult(t, ctx, done); err != nil {
			t.Fatalf("reassignment %d: %v", i, err)
		}
		binding, err := catalogrepo.New(store.DB()).GetApplicationEnvironment(ctx, bindings[i])
		if err != nil || binding.ApplicationID != apps[1-i] {
			t.Fatalf("reassignment %d did not commit expected application: %v", i, err)
		}
	}
}
