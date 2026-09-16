package deliverytrigger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"go.uber.org/zap"
)

func TestDeliveryTriggersWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	requireNoError(t, err)
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 6, MaxIdleConns: 4}, zap.NewNop())
	requireNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	requireNoError(t, store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")))
	key, err := keyring.NewKey("test", "test-only-key", time.Now(), nil)
	requireNoError(t, err)
	keys, err := keyring.New(key, nil)
	requireNoError(t, err)
	repo := New(store.DB(), keys)
	now := time.Now().UTC().Truncate(time.Microsecond)
	trigger, err := repo.Save(ctx, domain.StoredTrigger{Trigger: domain.Trigger{ID: uuid.NewString(), Revision: 1, Name: "test", Enabled: true, TargetKind: "workflow", TargetID: uuid.NewString(), Type: "webhook", CreatedAt: now, UpdatedAt: now}, ExecutionTokenID: "execution-reference", UpdatedByTokenID: "author-reference", TargetDigest: "target", SigningSecret: "test-signing-secret"}, 0)
	requireNoError(t, err)
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM delivery_trigger_events WHERE trigger_id=?`, trigger.ID).Error
		_ = store.DB().Exec(`DELETE FROM delivery_triggers WHERE id=?`, trigger.ID).Error
	})
	read, err := repo.Get(ctx, trigger.ID)
	requireNoError(t, err)
	if read.SigningSecret != trigger.SigningSecret || read.ExecutionTokenID != trigger.ExecutionTokenID || read.UpdatedByTokenID != trigger.UpdatedByTokenID {
		t.Fatal("execution references did not survive reload")
	}
	var definition, ciphertext string
	requireNoError(t, store.DB().Raw(`SELECT definition::text, signing_secret_ciphertext FROM delivery_triggers WHERE id=?`, trigger.ID).Row().Scan(&definition, &ciphertext))
	if strings.Contains(definition, trigger.SigningSecret) || strings.Contains(definition, trigger.ExecutionTokenID) || ciphertext == trigger.SigningSecret {
		t.Fatal("secret or execution token leaked")
	}
	event := domain.StoredEvent{Event: domain.Event{ID: uuid.NewString(), TriggerID: trigger.ID, TriggerRevision: 1, EventID: uuid.NewString(), EventType: "webhook", Status: "queued", OccurredAt: now, CreatedAt: now, UpdatedAt: now}, PayloadDigest: "payload"}
	assertDeliveryTriggerLifecycle(t, ctx, repo, trigger, event, now)
}

func assertDeliveryTriggerLifecycle(t *testing.T, ctx context.Context, repo *Repository, trigger domain.StoredTrigger, event domain.StoredEvent, now time.Time) {
	t.Helper()
	ids, errs := make(chan string, 2), make(chan error, 2)
	for range 2 {
		go func() { item, err := repo.Enqueue(ctx, event); ids <- item.ID; errs <- err }()
	}
	firstID, secondID := <-ids, <-ids
	if firstID != secondID {
		t.Fatal("duplicate event created")
	}
	requireNoError(t, <-errs)
	requireNoError(t, <-errs)
	event.PayloadDigest = "other"
	if _, err := repo.Enqueue(ctx, event); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed replay accepted: %v", err)
	}
	claims := make(chan domain.StoredEvent, 2)
	for range 2 {
		go func() { item, err := repo.Claim(ctx, uuid.NewString(), now); claims <- item; errs <- err }()
	}
	first, second := <-claims, <-claims
	err1, err2 := <-errs, <-errs
	if (err1 == nil) == (err2 == nil) {
		t.Fatalf("concurrent claims: %v, %v", err1, err2)
	}
	claimed := first
	if claimed.ID == "" {
		claimed = second
	}
	claimed.PreparedAt = &now
	claimed.SourceGeneration = 7
	claimed.ResolvedCommit = strings.Repeat("a", 40)
	requireNoError(t, repo.Checkpoint(ctx, claimed))
	recovered, err := repo.Claim(ctx, "recovered", now.Add(4*time.Minute))
	requireNoError(t, err)
	if recovered.ID != claimed.ID || recovered.Attempts != 2 || recovered.PreparedAt == nil || recovered.SourceGeneration != 7 || recovered.ResolvedCommit != claimed.ResolvedCommit {
		t.Fatal("lease recovery lost prepared inputs")
	}
	claimed.Status = "succeeded"
	if err := repo.Finish(ctx, claimed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale lease completed event: %v", err)
	}
	trigger.Revision = 2
	trigger.Enabled = false
	_, err = repo.Save(ctx, trigger, 1)
	requireNoError(t, err)
	if err := repo.Checkpoint(ctx, recovered); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("disabled trigger dispatched: %v", err)
	}
	if _, err := repo.Enqueue(ctx, event); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("disabled trigger enqueued: %v", err)
	}
	recovered, err = repo.Claim(ctx, "last-attempt", now.Add(8*time.Minute))
	requireNoError(t, err)
	if recovered.Attempts != 3 {
		t.Fatal("attempt count lost")
	}
	if _, err := repo.Claim(ctx, "exhausted", now.Add(12*time.Minute)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("exhausted event claimed: %v", err)
	}
	events, err := repo.Events(ctx, trigger.ID, 0, 50)
	requireNoError(t, err)
	if len(events) != 1 || events[0].Status != "failed" || events[0].Reason != "recovery_exhausted" {
		t.Fatalf("exhaustion not persisted: %+v", events)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
