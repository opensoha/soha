package delivery_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apprepo "github.com/opensoha/soha/internal/repository/application"
	auditrepo "github.com/opensoha/soha/internal/repository/auditlog"
	catalogrepo "github.com/opensoha/soha/internal/repository/catalog"
	deliveryrepo "github.com/opensoha/soha/internal/repository/delivery"
	manifestrepo "github.com/opensoha/soha/internal/repository/manifest"
	operationrepo "github.com/opensoha/soha/internal/repository/operationlog"
	"go.uber.org/zap"
)

func TestDeliveryDraftAtomicReceiptsWithPostgres(t *testing.T) {
	store := openDraftReceiptTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	repo := deliveryrepo.New(store.DB())
	suffix := uuid.NewString()
	input := domaindelivery.DeliveryDraftInput{IdempotencyKey: "draft-" + suffix, ApplicationDraft: domaindelivery.BlueprintApplicationDraft{Key: "app-" + suffix, Name: "Atomic draft"}}
	serviceVersion := int64(4)
	template := &domaincatalog.DeploymentTemplateBinding{TemplateID: "http", Version: 2, Parameters: map[string]any{"replicas": float64(0), "enabled": false}}
	input.Services = []domaindelivery.DeliveryDraftService{{Key: "api", Name: "API", ExpectedVersion: &serviceVersion, DeploymentTemplate: template}}
	const actor = "draft-receipt-test"
	draft := createDraftConcurrently(t, ctx, repo, input, actor)
	t.Cleanup(func() { _ = store.DB().Exec(`DELETE FROM delivery_drafts WHERE id = ?`, draft.ID).Error })
	stored, err := repo.GetDeliveryDraft(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Services) != 1 || !reflect.DeepEqual(stored.Services[0].DeploymentTemplate, template) || stored.Services[0].ExpectedVersion == nil || *stored.Services[0].ExpectedVersion != serviceVersion {
		t.Fatalf("persisted draft lost service template or version: %+v", stored.Services)
	}
	if _, err := repo.CreateDeliveryDraftIdempotent(ctx, input, actor, "changed"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed input reused key: %v", err)
	}

	apps, catalog, manifests := apprepo.New(store.DB()), catalogrepo.New(store.DB()), manifestrepo.New(store.DB())
	audits, operations := auditrepo.New(store.DB()), operationrepo.New(store.DB())
	appID, envID, manifestID, auditID, operationID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := store.DB().Exec(`INSERT INTO delivery_environments (id,environment_key,name) VALUES (?,?,'Draft test')`, envID, "draft-"+suffix).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM manifest_packages WHERE id = ?`, manifestID).Error
		_ = store.DB().Exec(`DELETE FROM application_environments WHERE application_id = ?`, appID).Error
		_ = store.DB().Exec(`DELETE FROM applications WHERE id = ?`, appID).Error
		_ = store.DB().Exec(`DELETE FROM delivery_environments WHERE id = ?`, envID).Error
		_ = store.DB().Exec(`DELETE FROM audit_logs WHERE id = ?`, auditID).Error
		_ = store.DB().Exec(`DELETE FROM operation_logs WHERE id = ?`, operationID).Error
	})
	var applies atomic.Int32
	fail := true
	injected := errors.New("fail after all domain writes")
	apply := func(ctx context.Context, draft domaindelivery.DeliveryDraft, receipt *domaindelivery.DeliveryDraftConfirmResult) (domaindelivery.DeliveryDraftConfirmResult, error) {
		if receipt != nil {
			return *receipt, nil
		}
		applies.Add(1)
		app, err := apps.Create(ctx, domainapp.UpsertInput{ID: appID, Name: "Atomic draft", Key: input.ApplicationDraft.Key, Enabled: true})
		if err != nil {
			return domaindelivery.DeliveryDraftConfirmResult{}, err
		}
		service, err := apps.CreateService(ctx, app.ID, domainapp.ServiceInput{Key: "api", Name: "API", ServiceKind: domainapp.ServiceKindKubernetesWorkload, Enabled: true})
		if err != nil {
			return domaindelivery.DeliveryDraftConfirmResult{}, err
		}
		binding, err := catalog.CreateApplicationEnvironment(ctx, domaincatalog.ApplicationEnvironmentInput{ApplicationID: app.ID, EnvironmentID: envID})
		if err != nil {
			return domaindelivery.DeliveryDraftConfirmResult{}, err
		}
		now := time.Now().UTC()
		_, err = manifests.Create(ctx, domainmanifest.Package{ID: manifestID, ApplicationID: app.ID, Name: "Seed", Renderer: domainmanifest.RendererRaw, Status: domainmanifest.StatusDraft, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return domaindelivery.DeliveryDraftConfirmResult{}, err
		}
		if err = audits.Create(ctx, domainaudit.Entry{ID: auditID, Action: "create", CreatedAt: now}); err != nil {
			return domaindelivery.DeliveryDraftConfirmResult{}, err
		}
		if err = operations.Create(ctx, domainoperation.Entry{ID: operationID, OperationType: "draft.create", CreatedAt: now}); err != nil {
			return domaindelivery.DeliveryDraftConfirmResult{}, err
		}
		if _, err = apps.Get(ctx, app.ID); err != nil {
			return domaindelivery.DeliveryDraftConfirmResult{}, err
		}
		if fail {
			return domaindelivery.DeliveryDraftConfirmResult{}, injected
		}
		draft.Status = domaindelivery.DeliveryDraftStatusConfirmed
		draft.ConfirmedAt = &now
		return domaindelivery.DeliveryDraftConfirmResult{Draft: draft, Application: app, Services: []domainapp.Service{service}, EnvironmentBindings: []domaincatalog.ApplicationEnvironment{binding}}, nil
	}
	verifyDraftConfirmationRollback(t, ctx, store, repo, draft.ID, apply, injected, map[string]string{"applications": appID, "manifest_packages": manifestID, "audit_logs": auditID, "operation_logs": operationID})
	fail = false
	confirmDraftConcurrently(t, ctx, repo, draft.ID, appID, apply)
	if applies.Load() != 2 {
		t.Fatalf("concurrent confirmation applied %d times after one rollback", applies.Load())
	}
	verifyDraftReceiptAfterEdit(t, ctx, store, repo, appID, draft.ID, actor, input.IdempotencyKey)
}

func createDraftConcurrently(t *testing.T, ctx context.Context, repo *deliveryrepo.Repository, input domaindelivery.DeliveryDraftInput, actor string) domaindelivery.DeliveryDraft {
	t.Helper()
	var wg sync.WaitGroup
	results := make(chan domaindelivery.DeliveryDraft, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := repo.CreateDeliveryDraftIdempotent(ctx, input, actor, "intent")
			results <- item
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var draft domaindelivery.DeliveryDraft
	for item := range results {
		if draft.ID != "" && draft.ID != item.ID {
			t.Fatal("duplicate draft")
		}
		draft = item
	}
	return draft
}

func verifyDraftConfirmationRollback(t *testing.T, ctx context.Context, store *dbstore.Store, repo *deliveryrepo.Repository, draftID string, apply func(context.Context, domaindelivery.DeliveryDraft, *domaindelivery.DeliveryDraftConfirmResult) (domaindelivery.DeliveryDraftConfirmResult, error), injected error, rows map[string]string) {
	t.Helper()
	if _, err := repo.WithDeliveryDraftConfirmation(ctx, draftID, apply); !errors.Is(err, injected) {
		t.Fatalf("failed write: %v", err)
	}
	for table, id := range rows {
		var count int64
		if err := store.DB().Table(table).Where("id = ?", id).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rollback retained %s: count=%d err=%v", table, count, err)
		}
	}
	if _, err := repo.GetDeliveryDraftConfirmation(ctx, draftID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("failure stored a receipt: %v", err)
	}
	current, err := repo.GetDeliveryDraft(ctx, draftID)
	if err != nil || current.Status != domaindelivery.DeliveryDraftStatusDraft {
		t.Fatalf("failed draft not retryable: %+v %v", current, err)
	}
}

func confirmDraftConcurrently(t *testing.T, ctx context.Context, repo *deliveryrepo.Repository, draftID, appID string, apply func(context.Context, domaindelivery.DeliveryDraft, *domaindelivery.DeliveryDraftConfirmResult) (domaindelivery.DeliveryDraftConfirmResult, error)) {
	t.Helper()
	var wg sync.WaitGroup
	confirmations := make(chan domaindelivery.DeliveryDraftConfirmResult, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := repo.WithDeliveryDraftConfirmation(ctx, draftID, apply)
			confirmations <- receipt
			failures <- err
		}()
	}
	wg.Wait()
	close(confirmations)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for receipt := range confirmations {
		if receipt.Application.ID != appID || receipt.Application.Version != 1 || receipt.Draft.Status != domaindelivery.DeliveryDraftStatusConfirmed {
			t.Fatalf("inconsistent receipt: %+v", receipt)
		}
	}
}

func verifyDraftReceiptAfterEdit(t *testing.T, ctx context.Context, store *dbstore.Store, repo *deliveryrepo.Repository, appID, draftID, actor, key string) {
	t.Helper()
	if err := store.DB().Exec(`UPDATE applications SET name = 'Edited later', version = version + 1 WHERE id = ?`, appID).Error; err != nil {
		t.Fatal(err)
	}
	replay, err := deliveryrepo.New(store.DB()).GetDeliveryDraftConfirmation(ctx, draftID)
	if err != nil || replay.Application.Name != "Atomic draft" || replay.Application.Version != 1 {
		t.Fatalf("receipt followed a later edit: %+v %v", replay, err)
	}
	original, err := repo.FindDeliveryDraftCreation(ctx, actor, key, "intent")
	if err != nil || original.Status != domaindelivery.DeliveryDraftStatusDraft {
		t.Fatalf("creation receipt changed on confirmation: %+v %v", original, err)
	}
}

func openDraftReceiptTestStore(t *testing.T) *dbstore.Store {
	t.Helper()
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 10, MaxIdleConns: 8}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
