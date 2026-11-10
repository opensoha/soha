package delivery_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	dr "github.com/opensoha/soha/internal/domain/resource"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	apprepo "github.com/opensoha/soha/internal/repository/application"
	catalogrepo "github.com/opensoha/soha/internal/repository/catalog"
	deliveryrepo "github.com/opensoha/soha/internal/repository/delivery"
	"gorm.io/gorm"
)

type referenceCandidatePGFixture struct {
	ctx              context.Context
	store            *dbstore.Store
	repo             *deliveryrepo.Repository
	source, consumer dd.KubernetesServiceImportResult
	clusterID        string
}

func newReferenceCandidatePGFixture(t *testing.T, ctx context.Context, store *dbstore.Store) referenceCandidatePGFixture {
	t.Helper()
	repo := deliveryrepo.New(store.DB())
	suffix := uuid.NewString()
	cluster := "reference-candidate-" + suffix
	if err := store.DB().Exec(`INSERT INTO clusters(id,name) VALUES (?, 'Reference candidate acceptance')`, cluster).Error; err != nil {
		t.Fatal(err)
	}
	input := dd.KubernetesServiceImportInput{ClusterID: cluster, Namespace: "source", ApplicationKey: "reference-" + suffix, ApplicationName: "Reference candidate", EnvironmentKey: "reference-source-" + suffix, EnvironmentName: "Test B", OwnershipMode: "observe_only",
		Workloads: []dd.KubernetesWorkloadImportInput{{WorkloadKind: "Deployment", WorkloadName: "api", Source: &dr.ResourceSourceSnapshot{Resource: dr.ResourceRef{ClusterID: cluster, Namespace: "source", APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: "source-" + suffix, ScopeMode: dr.ResourceScopeModeNamespace}, SpecDigest: strings.Repeat("a", 64), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}}
	resolve := func(i dd.KubernetesServiceImportInput) dd.KubernetesServiceImportInput {
		d, err := repo.ResolveImportDestination(ctx, i.ApplicationKey, i.EnvironmentKey)
		if err != nil {
			t.Fatal(err)
		}
		i.Destination = d
		return i
	}
	source, err := repo.ImportKubernetesServices(ctx, resolve(input))
	if err != nil {
		t.Fatal(err)
	}
	input.Namespace, input.EnvironmentKey, input.EnvironmentName = "consumer", "reference-consumer-"+suffix, "Test A"
	workload := input.Workloads[0]
	snapshot := *workload.Source
	snapshot.Resource.Namespace, snapshot.Resource.UID = "consumer", "consumer-"+suffix
	workload.Source = &snapshot
	workload.ServiceID = source.Services[0].ID
	input.Workloads = []dd.KubernetesWorkloadImportInput{workload}
	consumer, err := repo.ImportKubernetesServices(ctx, resolve(input))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB().Exec(`UPDATE delivery_environments SET tier='test',stage_level=20 WHERE id IN (?,?)`, source.Environment.ID, consumer.Environment.ID).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM applications WHERE id=?`, source.Application.ID).Error
		_ = store.DB().Exec(`DELETE FROM delivery_environments WHERE id IN (?,?)`, source.Environment.ID, consumer.Environment.ID).Error
		_ = store.DB().Exec(`DELETE FROM clusters WHERE id=?`, cluster).Error
	})
	return referenceCandidatePGFixture{ctx: ctx, store: store, repo: repo, source: source, consumer: consumer, clusterID: cluster}
}

func (f referenceCandidatePGFixture) configuration(ctx context.Context) (dd.ServiceReferenceConfiguration, error) {
	var state dd.ServiceReferenceConfiguration
	catalog := catalogrepo.New(f.store.DB())
	var err error
	state.Consumer, err = catalog.GetApplicationEnvironment(ctx, f.consumer.ApplicationEnvironmentID)
	if err != nil {
		return state, err
	}
	state.Source, err = catalog.GetApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID)
	if err != nil {
		return state, err
	}
	state.Service, err = apprepo.New(f.store.DB()).GetService(ctx, f.source.Application.ID, f.source.Services[0].ID)
	if err != nil {
		return state, err
	}
	environments, err := catalog.ListEnvironments(ctx)
	if err != nil {
		return state, err
	}
	for _, e := range environments {
		if e.ID == state.Consumer.EnvironmentID {
			state.ConsumerEnvironment = e
		}
		if e.ID == state.Source.EnvironmentID {
			state.SourceEnvironment = e
		}
	}
	return state, nil
}

func (f referenceCandidatePGFixture) write(t *testing.T) dd.ServiceReferenceCandidateWrite {
	t.Helper()
	configuration, err := f.configuration(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	consumer, source, service := configuration.Consumer, configuration.Source, configuration.Service
	scope := func(b dc.ApplicationEnvironment, e dc.Environment) dd.ServiceReferenceScope {
		return dd.ServiceReferenceScope{ApplicationEnvironmentID: b.ID, EnvironmentID: e.ID, EnvironmentKey: e.Key, Tier: e.Tier, StageLevel: e.StageLevel, IsProduction: e.IsProduction, UpdatedAt: b.UpdatedAt}
	}
	preview := dd.ServiceReferencePreview{ApplicationID: source.ApplicationID, ServiceID: service.ID, Consumer: scope(consumer, configuration.ConsumerEnvironment), Source: scope(source, configuration.SourceEnvironment), SourceRevision: strings.Repeat("a", 64), Targets: make([]dd.ServiceReferenceTarget, 0), CurrentLocalTargetIDs: []string{consumer.Targets[0].ID}, ObservedAt: time.Now().UTC()}
	for _, target := range source.Targets {
		resource := f.source.Targets[0].Source.Resource
		preview.Targets = append(preview.Targets, dd.ServiceReferenceTarget{TargetID: target.ID, ClusterID: target.ClusterID, Namespace: target.Namespace, WorkloadKind: target.WorkloadKind, WorkloadName: target.WorkloadName, OwnershipMode: "observe_only", Resources: []dd.ServiceReferenceResource{{Resource: resource, SpecDigest: strings.Repeat("a", 64)}}})
	}
	contextRevision, err := configuration.Revision()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := dd.ServiceReferenceIdentityRevision(preview)
	if err != nil {
		t.Fatal(err)
	}
	return dd.ServiceReferenceCandidateWrite{Candidate: dd.ServiceReferenceCandidate{Preview: preview, ContextRevision: contextRevision, SourceIdentityRevision: identity}}
}

func candidatePGAuthorize(context.Context) error { return nil }

func (f referenceCandidatePGFixture) save(t *testing.T) dd.ServiceReferenceCandidate {
	t.Helper()
	saved, err := f.repo.SaveServiceReferenceCandidate(f.ctx, f.write(t), f.configuration)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}
func (f referenceCandidatePGFixture) remove(saved dd.ServiceReferenceCandidate) dd.ServiceReferenceCandidateDeleteInput {
	return dd.ServiceReferenceCandidateDeleteInput{ExpectedCandidateID: saved.ID, ExpectedRevision: saved.Revision, ExpectedConsumerUpdatedAt: saved.Preview.Consumer.UpdatedAt}
}
func (f referenceCandidatePGFixture) snapshot(t *testing.T) string {
	t.Helper()
	var result string
	if err := f.store.DB().Raw(`SELECT json_build_object('bindings',(SELECT json_agg(row_to_json(b) ORDER BY b.id) FROM application_environments b WHERE application_id=?),'targets',(SELECT json_agg(row_to_json(t) ORDER BY t.id) FROM release_targets t WHERE cluster_id=?))::text`, f.source.Application.ID, f.clusterID).Row().Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestServiceReferenceCandidatesWithPostgres(t *testing.T) {
	store := openDraftReceiptTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	t.Run("save_restore_no_runtime_changes", func(t *testing.T) {
		testReferenceCandidatePGSaveRestoreNoRuntimeChanges(t, ctx, store)
	})
	t.Run("update_and_stale_cas", func(t *testing.T) {
		testReferenceCandidatePGUpdateAndStaleCas(t, ctx, store)
	})
	t.Run("delete_recreate_aba", func(t *testing.T) {
		testReferenceCandidatePGDeleteRecreateAba(t, ctx, store)
	})
	t.Run("context_drift", func(t *testing.T) {
		testReferenceCandidatePGContextDrift(t, ctx, store)
	})
	t.Run("reauthorization_rollback", func(t *testing.T) {
		testReferenceCandidatePGReauthorizationRollback(t, ctx, store)
	})
	for _, mode := range []string{"concurrent_create", "concurrent_update"} {
		t.Run(mode, func(t *testing.T) {
			testReferenceCandidatePGConcurrent(t, ctx, store, mode)
		})
	}
	t.Run("source_deleted_candidate_cleanup", func(t *testing.T) {
		testReferenceCandidatePGSourceDeletedCandidateCleanup(t, ctx, store)
	})
	t.Run("binding_lock_serializes_source_mutation", func(t *testing.T) {
		testReferenceCandidatePGBindingLockSerializesSourceMutation(t, ctx, store)
	})
	t.Run("consumer_binding_cascade", func(t *testing.T) {
		testReferenceCandidatePGConsumerBindingCascade(t, ctx, store)
	})
	t.Run("application_cascade_unrelated_preserved", func(t *testing.T) {
		testReferenceCandidatePGApplicationCascadeUnrelatedPreserved(t, ctx, store)
	})
	t.Run("cancellation_no_partial_write", func(t *testing.T) {
		testReferenceCandidatePGCancellationNoPartialWrite(t, ctx, store)
	})
	t.Run("metadata_pagination_without_source", func(t *testing.T) {
		testReferenceCandidatePGMetadataPaginationWithoutSource(t, ctx, store)
	})
}

func testReferenceCandidatePGSaveRestoreNoRuntimeChanges(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	before := f.snapshot(t)
	saved := f.save(t)
	restored, err := deliveryrepo.New(store.DB()).GetServiceReferenceCandidate(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID)
	if err != nil || saved.ID != restored.ID || restored.Revision != 1 || restored.State != "candidate" || restored.ContextRevision != saved.ContextRevision {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	a, err := json.Marshal(saved.Preview)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(restored.Preview)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) || f.snapshot(t) != before {
		t.Fatal("candidate persistence changed proposal or runtime configuration")
	}
}

func testReferenceCandidatePGUpdateAndStaleCas(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	saved := f.save(t)
	write := f.write(t)
	write.ExpectedCandidateID, write.ExpectedRevision = saved.ID, saved.Revision
	next, err := f.repo.SaveServiceReferenceCandidate(ctx, write, f.configuration)
	if err != nil || next.ID != saved.ID || next.Revision != 2 {
		t.Fatalf("update=%+v err=%v", next, err)
	}
	if _, err := f.repo.SaveServiceReferenceCandidate(ctx, write, f.configuration); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale=%v", err)
	}
	if _, err := f.repo.SaveServiceReferenceCandidate(ctx, f.write(t), f.configuration); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate create=%v", err)
	}
}

func testReferenceCandidatePGDeleteRecreateAba(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	old := f.save(t)
	if err := f.repo.DeleteServiceReferenceCandidate(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(old), candidatePGAuthorize); err != nil {
		t.Fatal(err)
	}
	current := f.save(t)
	if current.ID == old.ID || current.Revision != 1 {
		t.Fatal("recreation reused candidate identity")
	}
	stale := f.write(t)
	stale.ExpectedCandidateID, stale.ExpectedRevision = old.ID, old.Revision
	if _, err := f.repo.SaveServiceReferenceCandidate(ctx, stale, f.configuration); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("ABA update=%v", err)
	}
	if err := f.repo.DeleteServiceReferenceCandidate(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(old), candidatePGAuthorize); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("ABA delete=%v", err)
	}
}

func testReferenceCandidatePGContextDrift(t *testing.T, ctx context.Context, store *dbstore.Store) {
	for _, kind := range []string{"consumer_binding", "source_binding", "service_version_without_timestamp", "source_target_without_timestamp", "environment_classification"} {
		t.Run(kind, func(t *testing.T) {
			f := newReferenceCandidatePGFixture(t, ctx, store)
			write := f.write(t)
			var err error
			switch kind {
			case "consumer_binding":
				err = store.DB().Exec(`UPDATE application_environments SET alias='changed' WHERE id=?`, f.consumer.ApplicationEnvironmentID).Error
			case "source_binding":
				err = store.DB().Exec(`UPDATE application_environments SET namespace='changed' WHERE id=?`, f.source.ApplicationEnvironmentID).Error
			case "service_version_without_timestamp":
				err = store.DB().Exec(`UPDATE application_services SET version=version+1 WHERE id=?`, f.source.Services[0].ID).Error
			case "source_target_without_timestamp":
				err = store.DB().Exec(`UPDATE release_targets SET metadata=metadata||'{"ownershipMode":"managed"}'::jsonb WHERE id=?`, f.source.Targets[0].ID).Error
			case "environment_classification":
				err = store.DB().Exec(`UPDATE delivery_environments SET tier='pre' WHERE id=?`, f.source.Environment.ID).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.repo.SaveServiceReferenceCandidate(ctx, write, f.configuration); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("drift=%v", err)
			}
			if _, err := f.repo.GetServiceReferenceCandidate(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("failed save persisted=%v", err)
			}
		})
	}
}

func testReferenceCandidatePGReauthorizationRollback(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	before := f.snapshot(t)
	_, err := f.repo.SaveServiceReferenceCandidate(ctx, f.write(t), func(txctx context.Context) (dd.ServiceReferenceConfiguration, error) {
		if err := dbtx.DB(txctx, store.DB()).Exec(`UPDATE application_environments SET alias='must-rollback' WHERE id=?`, f.consumer.ApplicationEnvironmentID).Error; err != nil {
			return dd.ServiceReferenceConfiguration{}, err
		}
		return dd.ServiceReferenceConfiguration{}, apperrors.ErrAccessDenied
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) || f.snapshot(t) != before {
		t.Fatalf("reauthorization=%v", err)
	}
}

func testReferenceCandidatePGSourceDeletedCandidateCleanup(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	saved := f.save(t)
	if err := catalogrepo.New(store.DB()).DeleteApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID); err != nil {
		t.Fatal(err)
	}
	kept, err := f.repo.GetServiceReferenceCandidate(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID)
	if err != nil || kept.Preview.Source.ApplicationEnvironmentID != f.source.ApplicationEnvironmentID {
		t.Fatalf("lost invalid candidate=%v", err)
	}
	before := f.snapshot(t)
	if err := f.repo.DeleteServiceReferenceCandidate(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID, f.remove(saved), candidatePGAuthorize); err != nil {
		t.Fatal(err)
	}
	if f.snapshot(t) != before {
		t.Fatal("candidate cleanup touched runtime metadata")
	}
}

func testReferenceCandidatePGBindingLockSerializesSourceMutation(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	write := f.write(t)
	entered, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := f.repo.SaveServiceReferenceCandidate(ctx, write, func(txctx context.Context) (dd.ServiceReferenceConfiguration, error) {
			close(entered)
			<-release
			return f.configuration(txctx)
		})
		result <- err
	}()
	<-entered
	err := store.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SET LOCAL lock_timeout='200ms'`).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE application_environments SET alias='concurrent' WHERE id=?`, f.source.ApplicationEnvironmentID).Error
	})
	close(release)
	if saveErr := <-result; saveErr != nil {
		t.Fatal(saveErr)
	}
	if err == nil {
		t.Fatal("source mutation bypassed candidate save binding lock")
	}
}

func testReferenceCandidatePGConsumerBindingCascade(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	f.save(t)
	if err := catalogrepo.New(store.DB()).DeleteApplicationEnvironment(ctx, f.consumer.ApplicationEnvironmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.GetServiceReferenceCandidate(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("candidate retained=%v", err)
	}
	if _, err := catalogrepo.New(store.DB()).GetApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID); err != nil {
		t.Fatalf("consumer cleanup removed source: %v", err)
	}
}

func testReferenceCandidatePGApplicationCascadeUnrelatedPreserved(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	f.save(t)
	other := newReferenceCandidatePGFixture(t, ctx, store)
	otherSaved := other.save(t)
	if err := apprepo.New(store.DB()).Delete(ctx, f.source.Application.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.GetServiceReferenceCandidate(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted app candidate retained=%v", err)
	}
	kept, err := other.repo.GetServiceReferenceCandidate(ctx, other.consumer.ApplicationEnvironmentID, other.source.Services[0].ID)
	if err != nil || kept.ID != otherSaved.ID {
		t.Fatalf("unrelated candidate affected: %v", err)
	}
}

func testReferenceCandidatePGCancellationNoPartialWrite(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	write := f.write(t)
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := f.repo.SaveServiceReferenceCandidate(canceled, write, f.configuration); err == nil {
		t.Fatal("canceled save succeeded")
	}
	if _, err := f.repo.GetServiceReferenceCandidate(ctx, f.consumer.ApplicationEnvironmentID, f.source.Services[0].ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("partial save=%v", err)
	}
}

func testReferenceCandidatePGMetadataPaginationWithoutSource(t *testing.T, ctx context.Context, store *dbstore.Store) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	saved := f.save(t)
	other, err := apprepo.New(store.DB()).CreateService(ctx, f.source.Application.ID, domainapp.ServiceInput{ID: "zz-" + uuid.NewString(), Key: "other", Name: "Other", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// Retain a stale proposal too: metadata must remain usable for cleanup.
	if err := store.DB().Exec(`INSERT INTO environment_service_reference_candidates
 (id, application_id, consumer_binding_id, service_id, revision, preview, source_identity_revision, context_revision, created_at, updated_at)
 SELECT ?, application_id, consumer_binding_id, ?, revision, jsonb_set(preview, '{serviceId}', to_jsonb(?::text)), source_identity_revision, context_revision, created_at, updated_at
 FROM environment_service_reference_candidates WHERE id=?`, uuid.NewString(), other.ID, other.ID, saved.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := catalogrepo.New(store.DB()).DeleteApplicationEnvironment(ctx, f.source.ApplicationEnvironmentID); err != nil {
		t.Fatal(err)
	}
	page, err := f.repo.ListServiceReferenceCandidates(ctx, f.consumer.ApplicationEnvironmentID, f.source.Application.ID, "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" || page.Items[0].ID != saved.ID {
		t.Fatalf("first metadata page=%+v err=%v", page, err)
	}
	next, err := f.repo.ListServiceReferenceCandidates(ctx, f.consumer.ApplicationEnvironmentID, f.source.Application.ID, page.NextCursor, 1)
	if err != nil || len(next.Items) != 1 || next.Items[0].ServiceID != other.ID || next.NextCursor != "" {
		t.Fatalf("second metadata page=%+v err=%v", next, err)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "source") || strings.Contains(string(raw), "preview") {
		t.Fatalf("source content in metadata: %s", raw)
	}
	denied, err := f.repo.ListServiceReferenceCandidates(ctx, f.consumer.ApplicationEnvironmentID, "another-application", "", 1)
	if err != nil || len(denied.Items) != 0 {
		t.Fatalf("cross application metadata=%+v err=%v", denied, err)
	}
}

func testReferenceCandidatePGConcurrent(t *testing.T, ctx context.Context, store *dbstore.Store, mode string) {
	f := newReferenceCandidatePGFixture(t, ctx, store)
	write := f.write(t)
	if mode == "concurrent_update" {
		saved := f.save(t)
		write.ExpectedCandidateID, write.ExpectedRevision = saved.ID, saved.Revision
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := f.repo.SaveServiceReferenceCandidate(ctx, write, f.configuration)
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, apperrors.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}
