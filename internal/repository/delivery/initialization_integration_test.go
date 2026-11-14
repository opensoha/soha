package delivery_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apprepo "github.com/opensoha/soha/internal/repository/application"
	deliveryrepo "github.com/opensoha/soha/internal/repository/delivery"
)

func TestInitializationHistoryWithPostgres(t *testing.T) {
	store := openDraftReceiptTestStore(t)
	ctx := context.Background()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	app, err := apprepo.New(store.DB()).Create(ctx, domainapp.UpsertInput{Key: "init-" + uuid.NewString(), Name: "Initialization acceptance", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	repo := deliveryrepo.New(store.DB())
	cluster := "init-" + uuid.NewString()
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM execution_tasks WHERE application_id = ?`, app.ID).Error
		_ = store.DB().Exec(`DELETE FROM applications WHERE id = ?`, app.ID).Error
	})
	doc := domainmanifest.RenderedDocument{APIVersion: "sample.example/v1", Kind: "Database", Namespace: "team", Name: "db"}
	for _, mode := range []string{"absent", "preflight-only", "same-plan", "same-plan-failed", "same-plan-replayed", "failed-write", "canceled-zero-attempt", "cross-owner-version", "helm-release-history"} {
		t.Run(mode, func(t *testing.T) {
			checkInitialWriteHistory(t, store, app.ID, cluster, doc, mode)
		})
	}
	t.Run("admission-rechecks-failed-physical-target", func(t *testing.T) {
		snapshot := sohaapi.HelmDeliverySnapshot{DeliveryPlanID: "first", ApplicationID: app.ID, ClusterID: cluster, Namespace: "team", ReleaseName: "new-db", Operation: sohaapi.HelmDeliverySnapshotOperationInstall, Migration: &sohaapi.TemplateMigrationAssessment{Initialization: true}, Resources: []sohaapi.HelmDeliveryResource{{APIVersion: "v1", Kind: "ConfigMap", Namespace: "team", Name: "db-config"}}}
		task := domaindelivery.ExecutionTask{ID: uuid.NewString(), ApplicationID: app.ID, TaskKind: "helm_apply", ProviderKind: "helm_direct", TargetKind: "helm_release", Status: "queued", QueueKey: "helm:" + cluster + ":team:new-db", Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: snapshot}}}
		if _, err := repo.CreateExecutionTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().Exec(`UPDATE execution_tasks SET status='failed' WHERE id=?`, task.ID).Error; err != nil {
			t.Fatal(err)
		}
		snapshot.DeliveryPlanID = "different-plan"
		task.ID = uuid.NewString()
		task.Payload = map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: snapshot}}
		if _, err := repo.CreateExecutionTask(ctx, task); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("failed first write bypassed serialized admission: %v", err)
		}
		found, err := repo.HasTargetWriteHistory(ctx, cluster, "team", domainworkflow.HelmResourceKeys(snapshot), "new-db", snapshot.DeliveryPlanID)
		if err != nil || !found {
			t.Fatalf("retained write missing: %v %v", found, err)
		}
	})
	t.Run("legacy-direct-workers-cannot-claim-initialization", func(t *testing.T) {
		for _, provider := range []string{domainmanifest.TaskProviderDirectInitialization, domaindelivery.HelmDirectInitializationProvider} {
			id := uuid.NewString()
			// Exercise the persisted queue predicate independently of admission and hydration.
			if err := store.DB().Exec(`INSERT INTO execution_tasks (id,application_id,task_kind,provider_kind,status,callback_token,payload) VALUES (?,?,'manifest_preflight',?,'queued',?,'{}'::jsonb)`, id, app.ID, provider, uuid.NewString()).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ClaimExecutionTask(ctx, []string{"manifest_direct", "manifest_git", "helm_direct"}, "legacy-worker", "local"); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("legacy worker could claim %s: %v", provider, err)
			}
			task, err := repo.ClaimExecutionTask(ctx, []string{provider}, "initialization-worker", "local")
			if err != nil || task.ID != id || task.ProviderKind != provider || task.AttemptCount != 1 {
				t.Fatalf("dedicated worker did not claim initialization: %+v %v", task, err)
			}
		}
	})
}

func checkInitialWriteHistory(t *testing.T, store *dbstore.Store, appID, cluster string, doc domainmanifest.RenderedDocument, mode string) {
	t.Helper()
	ctx := context.Background()
	repo := deliveryrepo.New(store.DB())
	keys := domainmanifest.ResourceKeys(cluster, []domainmanifest.RenderedDocument{doc})

	id := uuid.NewString()
	kind, status := "manifest_apply", "failed"
	attempts := 0
	p := domainmanifest.TaskPayload{ClusterID: cluster, Namespace: "team", Documents: []domainmanifest.RenderedDocument{doc}}
	var payload any = p
	switch mode {
	case "preflight-only":
		kind = "manifest_preflight"
	case "same-plan", "same-plan-failed", "same-plan-replayed":
		p.InitializationPlanID = "approved-first"
		payload = p
		status = "running"
		attempts = 1
		if mode == "same-plan-failed" {
			status = "failed"
		}
		if mode == "same-plan-replayed" {
			attempts = 2
		}
	case "canceled-zero-attempt":
		status = "canceled"
	case "cross-owner-version":
		other, err := apprepo.New(store.DB()).Create(ctx, domainapp.UpsertInput{Key: "other-init-" + uuid.NewString(), Name: "Other owner", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		appID = other.ID
		defer func() { _ = store.DB().Exec(`DELETE FROM applications WHERE id = ?`, other.ID).Error }()
		p.Documents = append([]domainmanifest.RenderedDocument(nil), p.Documents...)
		p.Documents[0].APIVersion = "sample.example/v1beta1"
		payload = p
	case "helm-release-history":
		kind = "helm_apply"
		payload = map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: sohaapi.HelmDeliverySnapshot{DeliveryPlanID: "older", ClusterID: cluster, Namespace: "team", ReleaseName: "db"}}}
	}
	if mode != "absent" {
		encoded, _ := json.Marshal(payload)
		if err := store.DB().Exec(`INSERT INTO execution_tasks (id,application_id,task_kind,provider_kind,status,payload,attempt_count) VALUES (?,?,?,'test',?,?::jsonb,?)`, id, appID, kind, status, string(encoded), attempts).Error; err != nil {
			t.Fatal(err)
		}
		defer func() { _ = store.DB().Exec(`DELETE FROM execution_tasks WHERE id = ?`, id).Error }()
	}
	found, err := repo.HasTargetWriteHistory(ctx, cluster, "team", keys, "db", "approved-first")
	if err != nil {
		t.Fatal(err)
	}
	want := mode == "same-plan-failed" || mode == "same-plan-replayed" || mode == "failed-write" || mode == "canceled-zero-attempt" || mode == "cross-owner-version" || mode == "helm-release-history"
	if found != want {
		t.Fatalf("history %s = %v want %v", mode, found, want)
	}
}
