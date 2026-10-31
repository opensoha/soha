package directorysync_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	appdirectory "github.com/opensoha/soha/internal/application/directorysync"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	directoryrepo "github.com/opensoha/soha/internal/repository/directorysync"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
)

func originalDirectoryStore(t *testing.T) *dbstore.Store {
	t.Helper()
	text := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if text == "" {
		t.Skip("requires this run's disposable PostgreSQL")
	}
	port, err := strconv.Atoi(text)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testpostgres.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 12, MaxIdleConns: 4})
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "directory_original_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(t.Context(), "CREATE DATABASE "+cfg.Name); err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
		if err := admin.Exec(context.Background(), "DROP DATABASE "+cfg.Name); err != nil {
			t.Error(err)
		}
	})
	if err := store.MigrateFromFile(t.Context(), "../../../migrations/postgres/0001_init.sql"); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestOriginalDirectorySyncWithPostgres(t *testing.T) {
	store := originalDirectoryStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	repo := directoryrepo.New(store.DB(), keyring.Ring{})
	must := func(tb *testing.T, err error) { originalDirectorySyncWithPostgresPhase1(tb, err) }
	input := func(tb *testing.T) domain.Run { return originalDirectorySyncWithPostgresPhase2(tb, &ctx, &repo, &must) }
	claim := func(tb *testing.T, run domain.Run) domain.Run {
		return originalDirectorySyncWithPostgresPhase3(tb, run, &ctx, &repo, &must)
	}
	t.Run("concurrent_original_claim", func(t *testing.T) { originalDirectorySyncWithPostgresPhase4(t, &store, &ctx, &repo, &must, &input) })
	t.Run("different_active_intent_conflicts", func(t *testing.T) { originalDirectorySyncWithPostgresPhase5(t, &ctx, &repo, &input, &claim) })
	t.Run("actor_and_credential_binding", func(t *testing.T) { originalDirectorySyncWithPostgresPhase6(t, &ctx, &repo, &must, &input, &claim) })
	t.Run("terminal_reopen_retains_original", func(t *testing.T) {
		originalDirectorySyncWithPostgresPhase7(t, &store, &ctx, &repo, &must, &input, &claim)
	})
	t.Run("running_cancel_keeps_active_slot", func(t *testing.T) { originalDirectorySyncWithPostgresPhase8(t, &ctx, &repo, &must, &input, &claim) })
	t.Run("legacy_cancel_writer_cannot_release_original_slot", func(t *testing.T) { originalDirectorySyncWithPostgresPhase9(t, &ctx, &repo, &must, &input, &claim) })
	t.Run("queued_cancel_wins_dispatch_cas", func(t *testing.T) { originalDirectorySyncWithPostgresPhase10(t, &ctx, &repo, &must, &input, &claim) })
	t.Run("heartbeat_and_stale_key_retention", func(t *testing.T) {
		originalDirectorySyncWithPostgresPhase11(t, &store, &ctx, &repo, &must, &input, &claim)
	})
	t.Run("migration_and_legacy_runs_compatible", func(t *testing.T) { originalDirectorySyncWithPostgresPhase12(t, &store, &ctx, &repo, &must, &input) })
}

func originalDirectorySyncWithPostgresPhase1(tb *testing.T, err error) {

	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}

func originalDirectorySyncWithPostgresPhase2(tb *testing.T, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error)) domain.Run {

	tb.Helper()
	id := uuid.NewString()
	_, err := (*sharedRepo).CreateConnection((*sharedCtx), domain.Connection{ID: id, Name: "owned", ProviderType: domain.ProviderLDAP, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, domain.DefaultPolicy(id))
	(*sharedMust)(tb, err)
	// #nosec G101 -- synthetic credential identity hash, not an authentication credential.
	return domain.Run{ID: uuid.NewString(), ConnectionID: id, Trigger: "manual", Mode: "full", Status: domain.RunQueued, IdempotencyKey: "manual-v1:" + uuid.NewString(), RequestedBy: "actor", RequestCredentialID: "sha256:test-identity", CreatedAt: time.Now().UTC()}
}

func originalDirectorySyncWithPostgresPhase3(tb *testing.T, run domain.Run, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error)) domain.Run {

	tb.Helper()
	got, created, err := (*sharedRepo).ClaimSyncRequest((*sharedCtx), run)
	(*sharedMust)(tb, err)
	if !created {
		tb.Fatal("original was not created")
	}
	return got
}

func originalDirectorySyncWithPostgresPhase4(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedInput *func(tb *testing.T) domain.Run) {

	in := (*sharedInput)(t)
	var winners atomic.Int32
	var wait sync.WaitGroup
	for range 32 {
		wait.Go(func() {
			attempt := in
			attempt.ID = uuid.NewString()
			got, created, err := (*sharedRepo).ClaimSyncRequest((*sharedCtx), attempt)
			if err != nil || got.IdempotencyKey != in.IdempotencyKey {
				t.Errorf("original claim failed: %v", err)
				return
			}
			if created {
				winners.Add(1)
			}
		})
	}
	wait.Wait()
	var count int64
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Raw(`SELECT count(*) FROM directory_sync_runs WHERE connection_id=?`, in.ConnectionID).Scan(&count).Error)
	if winners.Load() != 1 || count != 1 {
		t.Fatalf("claim winners/rows=%d/%d", winners.Load(), count)
	}
}

func originalDirectorySyncWithPostgresPhase5(t *testing.T, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedInput *func(tb *testing.T) domain.Run, sharedClaim *func(tb *testing.T, run domain.Run) domain.Run) {

	in := (*sharedClaim)(t, (*sharedInput)(t))
	other := in
	other.ID, other.IdempotencyKey = uuid.NewString(), "manual-v1:"+uuid.NewString()
	if _, _, err := (*sharedRepo).ClaimSyncRequest((*sharedCtx), other); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("active slot was bypassed", err)
	}
}

func originalDirectorySyncWithPostgresPhase6(t *testing.T, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedInput *func(tb *testing.T) domain.Run, sharedClaim *func(tb *testing.T, run domain.Run) domain.Run) {

	in := (*sharedInput)(t)
	key := uuid.NewString()
	in.IdempotencyKey = fmt.Sprintf("manual-v1:%x", sha256.Sum256([]byte("actor\x00"+key)))
	in.RequestCredentialID = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("session:original")))
	(*sharedClaim)(t, in)
	service := appdirectory.New((*sharedRepo), nil)
	_, err := service.GetOriginalSync((*sharedCtx), in.ConnectionID, "actor", "session:original", key)
	(*sharedMust)(t, err)
	if _, err := service.GetOriginalSync((*sharedCtx), in.ConnectionID, "actor", "token:foreign", key); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("foreign credential read original", err)
	}
	if _, err := service.GetOriginalSync((*sharedCtx), in.ConnectionID, "other", "session:original", key); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("actor namespace not isolated", err)
	}
}

func originalDirectorySyncWithPostgresPhase7(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedInput *func(tb *testing.T) domain.Run, sharedClaim *func(tb *testing.T, run domain.Run) domain.Run) {

	in := (*sharedClaim)(t, (*sharedInput)(t))
	(*sharedMust)(t, (*sharedRepo).TransitionRun((*sharedCtx), in.ID, domain.RunRunning, domain.RunStats{}, "", ""))
	(*sharedMust)(t, (*sharedRepo).TransitionRun((*sharedCtx), in.ID, domain.RunSucceeded, domain.RunStats{OrganizationsCreated: 1}, "", ""))
	in.ID = uuid.NewString()
	got, created, err := directoryrepo.New((*sharedStore).DB(), keyring.Ring{}).ClaimSyncRequest((*sharedCtx), in)
	(*sharedMust)(t, err)
	if created || got.ID == in.ID || got.Status != domain.RunSucceeded || got.FinishedAt == nil || got.Stats.OrganizationsCreated != 1 {
		t.Fatal("reopen replayed or lost durable completion")
	}
}

func originalDirectorySyncWithPostgresPhase8(t *testing.T, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedInput *func(tb *testing.T) domain.Run, sharedClaim *func(tb *testing.T, run domain.Run) domain.Run) {

	in := (*sharedClaim)(t, (*sharedInput)(t))
	(*sharedMust)(t, (*sharedRepo).TransitionRun((*sharedCtx), in.ID, domain.RunRunning, domain.RunStats{}, "", ""))
	requested, err := (*sharedRepo).RequestSyncCancellation((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	if requested.Status != domain.RunRunning || !requested.CancelRequested || requested.FinishedAt != nil {
		t.Fatal("request prematurely claimed stopped execution")
	}
	other := in
	other.ID, other.IdempotencyKey = uuid.NewString(), "manual-v1:"+uuid.NewString()
	if _, _, err := (*sharedRepo).ClaimSyncRequest((*sharedCtx), other); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("cancellation freed active slot", err)
	}
	flag, err := (*sharedRepo).TouchSyncRequest((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	if !flag {
		t.Fatal("creator did not observe cancellation")
	}
	(*sharedMust)(t, (*sharedRepo).FinishCanceledSyncRequest((*sharedCtx), in.ID, "creator stopped"))
	(*sharedClaim)(t, other)
}

func originalDirectorySyncWithPostgresPhase9(t *testing.T, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedInput *func(tb *testing.T) domain.Run, sharedClaim *func(tb *testing.T, run domain.Run) domain.Run) {

	in := (*sharedClaim)(t, (*sharedInput)(t))
	(*sharedMust)(t, (*sharedRepo).TransitionRun((*sharedCtx), in.ID, domain.RunRunning, domain.RunStats{}, "", ""))
	// This is the unchanged old writer SQL, with no new cancellation fields.
	if err := (*sharedRepo).TransitionRun((*sharedCtx), in.ID, domain.RunCanceled, domain.RunStats{}, "canceled", "canceled by user"); !errors.Is(err, domain.ErrInvalidRunState) {
		t.Fatal("old writer was allowed to report a successful cancellation", err)
	}
	guarded, err := (*sharedRepo).GetRun((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	if guarded.Status != domain.RunRunning || guarded.CancelRequested || guarded.FinishedAt != nil || guarded.ErrorCode != "" {
		t.Fatal("legacy cancellation freed slot or fabricated stopped execution")
	}
	other := in
	other.ID, other.IdempotencyKey = uuid.NewString(), "manual-v1:"+uuid.NewString()
	if _, _, err := (*sharedRepo).ClaimSyncRequest((*sharedCtx), other); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("legacy writer admitted another creator", err)
	}
	_, err = (*sharedRepo).RequestSyncCancellation((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	(*sharedMust)(t, (*sharedRepo).FinishCanceledSyncRequest((*sharedCtx), in.ID, "creator stopped"))
	finished, err := (*sharedRepo).GetRun((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	if finished.Status != domain.RunCanceled || finished.FinishedAt == nil {
		t.Fatal("creator stop acknowledgement not persisted")
	}
	(*sharedClaim)(t, other)
}

func originalDirectorySyncWithPostgresPhase10(t *testing.T, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedInput *func(tb *testing.T) domain.Run, sharedClaim *func(tb *testing.T, run domain.Run) domain.Run) {

	in := (*sharedClaim)(t, (*sharedInput)(t))
	stopped, err := (*sharedRepo).RequestSyncCancellation((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	if stopped.Status != domain.RunCanceled || stopped.FinishedAt == nil {
		t.Fatal("queued cancellation not terminal")
	}
	if err := (*sharedRepo).TransitionRun((*sharedCtx), in.ID, domain.RunRunning, domain.RunStats{}, "", ""); err == nil {
		t.Fatal("canceled queued request dispatched")
	}
}

func originalDirectorySyncWithPostgresPhase11(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedInput *func(tb *testing.T) domain.Run, sharedClaim *func(tb *testing.T, run domain.Run) domain.Run) {

	in := (*sharedClaim)(t, (*sharedInput)(t))
	(*sharedMust)(t, (*sharedRepo).TransitionRun((*sharedCtx), in.ID, domain.RunRunning, domain.RunStats{}, "", ""))
	_, err := (*sharedRepo).TouchSyncRequest((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	_, err = (*sharedRepo).RecoverStaleRuns((*sharedCtx), time.Now().Add(-10*time.Minute), time.Now())
	(*sharedMust)(t, err)
	live, err := (*sharedRepo).GetRun((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	if live.Status != domain.RunRunning {
		t.Fatal("live heartbeat recovered as stale")
	}
	(*sharedMust)(t, (*sharedStore).DB().WithContext((*sharedCtx)).Exec(`UPDATE directory_sync_runs SET heartbeat_at=? WHERE id=?`, time.Now().Add(-time.Hour), in.ID).Error)
	_, err = (*sharedRepo).RecoverStaleRuns((*sharedCtx), time.Now().Add(-10*time.Minute), time.Now())
	(*sharedMust)(t, err)
	got, created, err := (*sharedRepo).ClaimSyncRequest((*sharedCtx), in)
	(*sharedMust)(t, err)
	if created || got.Status != domain.RunFailed || got.ErrorCode != "worker_stale" {
		t.Fatal("stale original became replayable")
	}
}

func originalDirectorySyncWithPostgresPhase12(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedRepo **directoryrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedInput *func(tb *testing.T) domain.Run) {

	in := (*sharedInput)(t)
	in.IdempotencyKey, in.RequestCredentialID = "", ""
	_, err := (*sharedRepo).CreateRun((*sharedCtx), in)
	(*sharedMust)(t, err)
	(*sharedMust)(t, (*sharedStore).MigrateFromFile((*sharedCtx), "../../../migrations/postgres/0135_directory_sync_request_credential.sql"))
	got, err := (*sharedRepo).GetRun((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	if got.RequestCredentialID != "" || got.CancelRequested || got.Status != domain.RunQueued {
		t.Fatal("migration changed legacy record")
	}
	(*sharedMust)(t, (*sharedRepo).TransitionRun((*sharedCtx), in.ID, domain.RunRunning, domain.RunStats{}, "", ""))
	(*sharedMust)(t, (*sharedRepo).TransitionRun((*sharedCtx), in.ID, domain.RunCanceled, domain.RunStats{}, "canceled", "legacy cancellation"))
	got, err = (*sharedRepo).GetRun((*sharedCtx), in.ID)
	(*sharedMust)(t, err)
	if got.Status != domain.RunCanceled || got.FinishedAt == nil {
		t.Fatal("legacy unbound run behavior changed")
	}
}
