package conditionalmutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
)

func mutationPostgres(t *testing.T) (*Repository, func() *Repository) {
	t.Helper()
	value := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if value == "" {
		t.Skip("requires an explicitly isolated PostgreSQL test port")
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testpostgres.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 16, MaxIdleConns: 4})
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "conditional_mutation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.DB().Exec(`CREATE DATABASE "` + cfg.Name + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.DB().Exec(`DROP DATABASE "` + cfg.Name + `" WITH (FORCE)`).Error; err != nil {
			t.Error(err)
		}
	})
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	codec, _ := mutationCodecFixture(t)
	return New(store.DB(), codec.keys), func() *Repository {
		other, err := dbstore.New(cfg, zap.NewNop())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = other.Close() })
		return New(other.DB(), codec.keys)
	}
}

func TestConditionalMutationReceiptsWithPostgres(t *testing.T) {
	repo, reopen := mutationPostgres(t)
	_, record := mutationCodecFixture(t)
	ctx := t.Context()
	t.Run("concurrent_original_claim", func(t *testing.T) { conditionalMutationReceiptsWithPostgresPhase1(t, &repo, &reopen, &record, &ctx) })
	t.Run("changed_input_and_credential", func(t *testing.T) { conditionalMutationReceiptsWithPostgresPhase2(t, &repo, &record, &ctx) })
	t.Run("actor_isolation", func(t *testing.T) {
		if _, err := repo.Get(ctx, "other", record.KeyHash); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("encrypted_reopen", func(t *testing.T) { conditionalMutationReceiptsWithPostgresPhase3(t, &repo, &reopen, &record, &ctx) })
	t.Run("terminal_compare_and_set", func(t *testing.T) {
		assertConditionalMutationTerminalCAS(ctx, t, &record, repo, reopen)
	})
	t.Run("crashed_unknown_no_replay", func(t *testing.T) { conditionalMutationReceiptsWithPostgresPhase4(t, &repo, &reopen, &ctx) })
	t.Run("expired_payload_tombstone", func(t *testing.T) { conditionalMutationReceiptsWithPostgresPhase5(t, &repo, &record, &ctx) })
	t.Run("bounded_admission", func(t *testing.T) { conditionalMutationReceiptsWithPostgresPhase6(t, &repo, &ctx) })
}

func assertConditionalMutationTerminalCAS(ctx context.Context, t *testing.T, record *domainresource.ConditionalMutationRecord, repo *Repository, reopen func() *Repository) {
	t.Helper()
	finished := time.Now().UTC()
	record.Status, record.FinishedAt, record.Output = "succeeded", &finished, map[string]any{"applied": true, "kind": "ConfigMap"}
	if err := repo.Finish(ctx, *record); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(ctx, *record); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("late outcome replaced result", err)
	}
	item, err := reopen().Get(ctx, record.ActorID, record.KeyHash)
	if err != nil || item.Output["applied"] != true {
		t.Fatal("lost committed result", err)
	}
}

func conditionalMutationReceiptsWithPostgresPhase1(t *testing.T, sharedRepo **Repository, sharedReopen *func() *Repository, sharedRecord *domainresource.ConditionalMutationRecord, sharedCtx *context.Context) {

	other := (*sharedReopen)()
	var winners atomic.Int64
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() {
			selected := (*sharedRepo)
			if i%2 == 1 {
				selected = other
			}
			item, won, err := selected.Claim((*sharedCtx), (*sharedRecord))
			if err != nil {
				t.Error(err)
				return
			}
			if item.ID != (*sharedRecord).ID {
				t.Error("original receipt identity changed")
			}
			if won {
				winners.Add(1)
			}
		})
	}
	workers.Wait()
	if winners.Load() != 1 {
		t.Fatal("original claimed more than once", winners.Load())
	}
}

func conditionalMutationReceiptsWithPostgresPhase2(t *testing.T, sharedRepo **Repository, sharedRecord *domainresource.ConditionalMutationRecord, sharedCtx *context.Context) {

	changed := (*sharedRecord)
	changed.InputHash = strings.Repeat("3", 64)
	if _, won, err := (*sharedRepo).Claim((*sharedCtx), changed); won || !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("changed input accepted", err)
	}
	changed = (*sharedRecord)
	changed.CredentialID = "token:another-token"
	changed.Authorization.ActorTokenID = "another-token"
	if _, won, err := (*sharedRepo).Claim((*sharedCtx), changed); won || !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("changed credential accepted", err)
	}
}

func conditionalMutationReceiptsWithPostgresPhase3(t *testing.T, sharedRepo **Repository, sharedReopen *func() *Repository, sharedRecord *domainresource.ConditionalMutationRecord, sharedCtx *context.Context) {

	item, err := (*sharedReopen)().Get((*sharedCtx), (*sharedRecord).ActorID, (*sharedRecord).KeyHash)
	if err != nil || item.CredentialID != (*sharedRecord).CredentialID || item.Authorization.Call.SessionID != "server-session" {
		t.Fatal("lost protected original", err)
	}
	var row mutationRow
	if err := (*sharedRepo).db.Where("id = ?", (*sharedRecord).ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row.EncryptedPayload, (*sharedRecord).InputHash) || strings.Contains(row.EncryptedPayload, "server-session") {
		t.Fatal("plaintext binding persisted")
	}
	var columns int64
	if err := (*sharedRepo).db.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_name='platform_conditional_mutation_receipts' AND column_name IN ('input_hash','content','yaml')`).Scan(&columns).Error; err != nil || columns != 0 {
		t.Fatal("private digest/input has a plaintext column", err)
	}
}

func conditionalMutationReceiptsWithPostgresPhase4(t *testing.T, sharedRepo **Repository, sharedReopen *func() *Repository, sharedCtx *context.Context) {

	_, crashed := mutationCodecFixture(t)
	crashed.ID = uuid.NewString()
	crashed.KeyHash = strings.Repeat("4", 64)
	if _, won, err := (*sharedRepo).Claim((*sharedCtx), crashed); err != nil || !won {
		t.Fatal(err)
	}
	if err := (*sharedRepo).db.Exec(`UPDATE platform_conditional_mutation_receipts SET deadline_at=now()-interval '1 second' WHERE id=?`, crashed.ID).Error; err != nil {
		t.Fatal(err)
	}
	item, err := (*sharedReopen)().Get((*sharedCtx), crashed.ActorID, crashed.KeyHash)
	if err != nil || item.Status != "unknown" {
		t.Fatal("crash falsely completed", err)
	}
	if _, won, err := (*sharedRepo).Claim((*sharedCtx), crashed); err != nil || won {
		t.Fatal("crashed original was dispatched again", err)
	}
	finished := time.Now().UTC()
	crashed.Status, crashed.FinishedAt = "succeeded", &finished
	if err := (*sharedRepo).Finish((*sharedCtx), crashed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("late result overwrote uncertainty", err)
	}
}

func conditionalMutationReceiptsWithPostgresPhase5(t *testing.T, sharedRepo **Repository, sharedRecord *domainresource.ConditionalMutationRecord, sharedCtx *context.Context) {

	if err := (*sharedRepo).db.Exec(`UPDATE platform_conditional_mutation_receipts SET expires_at=now()-interval '1 second' WHERE id=?`, (*sharedRecord).ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := (*sharedRepo).Cleanup((*sharedCtx)); err != nil {
		t.Fatal(err)
	}
	var row mutationRow
	if err := (*sharedRepo).db.Where("id=?", (*sharedRecord).ID).Take(&row).Error; err != nil || row.EncryptedPayload != "" {
		t.Fatal("expired payload retained", err)
	}
	if _, err := (*sharedRepo).Get((*sharedCtx), (*sharedRecord).ActorID, (*sharedRecord).KeyHash); !errors.Is(err, apperrors.ErrGone) {
		t.Fatal(err)
	}
	(*sharedRecord).Status = "running"
	(*sharedRecord).FinishedAt = nil
	if _, won, err := (*sharedRepo).Claim((*sharedCtx), (*sharedRecord)); won || !errors.Is(err, apperrors.ErrGone) {
		t.Fatal("expired key replayed", err)
	}
}

func conditionalMutationReceiptsWithPostgresPhase6(t *testing.T, sharedRepo **Repository, sharedCtx *context.Context) {

	if err := (*sharedRepo).Cleanup((*sharedCtx)); err != nil {
		t.Fatal(err)
	}
	for i := range 64 {
		_, next := mutationCodecFixture(t)
		next.ID = uuid.NewString()
		next.KeyHash = strings.Repeat("5", 63) + "a"
		if i < 4 {
			next.ActorID = "capacity-actor"
			next.KeyHash = strings.Repeat("0", 63) + strconv.Itoa(i)
		} else {
			next.ActorID = "capacity-actor-" + strconv.Itoa(i)
		}
		next.Authorization.ActorID = next.ActorID
		if _, won, err := (*sharedRepo).Claim((*sharedCtx), next); err != nil || !won {
			t.Fatalf("claim %d failed: %v", i, err)
		}
		if i == 3 {
			extra := next
			extra.ID = uuid.NewString()
			extra.KeyHash = strings.Repeat("9", 64)
			if _, won, err := (*sharedRepo).Claim((*sharedCtx), extra); won || !errors.Is(err, apperrors.ErrServiceUnavailable) {
				t.Fatal("actor admission exceeded", err)
			}
		}
	}
	_, extra := mutationCodecFixture(t)
	extra.ID = uuid.NewString()
	extra.ActorID = "global-overflow"
	extra.Authorization.ActorID = extra.ActorID
	extra.KeyHash = strings.Repeat("8", 64)
	if _, won, err := (*sharedRepo).Claim((*sharedCtx), extra); won || !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatal("global admission exceeded", err)
	}
}
