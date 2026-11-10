package podexecution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	contractresource "github.com/opensoha/soha-contracts/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
)

func podExecutionPostgres(t *testing.T) (*Repository, func() *Repository) {
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
	cfg.Name = "pod_exec_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	keys := codecRepository(t).keys
	return New(store.DB(), keys), func() *Repository {
		other, err := dbstore.New(cfg, zap.NewNop())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = other.Close() })
		return New(other.DB(), keys)
	}
}

func TestPodExecutionReceiptWithPostgres(t *testing.T) {
	repository, reopen := podExecutionPostgres(t)
	ctx := t.Context()
	record := codecRecord()
	t.Run("concurrent_original_claim", func(t *testing.T) { podExecutionReceiptWithPostgresPhase1(t, &repository, &ctx, &record) })
	t.Run("changed_input_conflicts", func(t *testing.T) { podExecutionReceiptWithPostgresPhase2(t, &repository, &ctx, &record) })
	t.Run("actor_isolation", func(t *testing.T) {
		if _, err := repository.Get(ctx, "another-actor", record.Receipt.ID, ""); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("reopened_encrypted_receipt", func(t *testing.T) { podExecutionReceiptWithPostgresPhase3(t, &repository, &reopen, &ctx, &record) })
	t.Run("terminal_compare_and_set", func(t *testing.T) { podExecutionReceiptWithPostgresPhase4(t, &repository, &reopen, &ctx, &record) })
	t.Run("expired_payload_tombstone", func(t *testing.T) { podExecutionReceiptWithPostgresPhase5(t, &repository, &ctx, &record) })
	t.Run("crashed_attempt_unknown", func(t *testing.T) { podExecutionReceiptWithPostgresPhase6(t, &repository, &reopen, &ctx) })
	t.Run("bounded_admission", func(t *testing.T) { verifyPodExecutionCapacity(t, repository) })
}

func verifyPodExecutionCapacity(t *testing.T, repository *Repository) {
	t.Helper()
	ctx := t.Context()
	makeRecord := func(actor string, number int) domainresource.PodExecutionRecord {
		record := codecRecord()
		record.ActorID = actor
		record.KeyHash = fmt.Sprintf("%064x", number)
		record.Receipt.ID = uuid.NewString()
		return record
	}
	for i := 0; i < 4; i++ {
		if _, won, err := repository.Claim(ctx, makeRecord("capacity-actor", i)); err != nil || !won {
			t.Fatal(err)
		}
	}
	if _, _, err := repository.Claim(ctx, makeRecord("capacity-actor", 4)); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatal("actor capacity bypass")
	}
	for i := 0; i < 60; i++ {
		if _, won, err := repository.Claim(ctx, makeRecord(fmt.Sprint("capacity-global-", i), i)); err != nil || !won {
			t.Fatal(err)
		}
	}
	if _, _, err := repository.Claim(ctx, makeRecord("overflow", 99)); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatal("global capacity bypass")
	}
	if _, won, err := repository.Claim(ctx, makeRecord("capacity-actor", 0)); err != nil || won {
		t.Fatal("original receipt blocked or repeated at capacity")
	}
}

func podExecutionReceiptWithPostgresPhase1(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context, sharedRecord *domainresource.PodExecutionRecord) {

	var created atomic.Int64
	var workers sync.WaitGroup
	for range 12 {
		workers.Go(func() {
			item, won, err := (*sharedRepository).Claim((*sharedCtx), (*sharedRecord))
			if err != nil {
				t.Error(err)
				return
			}
			if item.Receipt.ID != (*sharedRecord).Receipt.ID {
				t.Error("original receipt changed")
			}
			if won {
				created.Add(1)
			}
		})
	}
	workers.Wait()
	if created.Load() != 1 {
		t.Fatalf("claims=%d", created.Load())
	}
}

func podExecutionReceiptWithPostgresPhase2(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context, sharedRecord *domainresource.PodExecutionRecord) {

	changed := (*sharedRecord)
	changed.InputHash = strings.Repeat("3", 64)
	if _, _, err := (*sharedRepository).Claim((*sharedCtx), changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal(err)
	}
}

func podExecutionReceiptWithPostgresPhase3(t *testing.T, sharedRepository **Repository, sharedReopen *func() *Repository, sharedCtx *context.Context, sharedRecord *domainresource.PodExecutionRecord) {

	item, err := (*sharedReopen)().Get((*sharedCtx), (*sharedRecord).ActorID, "", (*sharedRecord).KeyHash)
	if err != nil || item.Input.Argv[1] != "private-argv-marker" || item.ConnectionHash != (*sharedRecord).ConnectionHash {
		t.Fatalf("lost original input: %v", err)
	}
	var row executionRow
	if err := (*sharedRepository).db.Where("id = ?", (*sharedRecord).Receipt.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row.EncryptedPayload, "private-") {
		t.Fatal("plaintext input persisted")
	}
	var columns int64
	if err := (*sharedRepository).db.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_name='platform_pod_executions' AND column_name='input_hash'`).Scan(&columns).Error; err != nil || columns != 0 {
		t.Fatal("input digest exposed outside encryption")
	}
}

func podExecutionReceiptWithPostgresPhase4(t *testing.T, sharedRepository **Repository, sharedReopen *func() *Repository, sharedCtx *context.Context, sharedRecord *domainresource.PodExecutionRecord) {

	finished := time.Now().UTC()
	code := int64(0)
	(*sharedRecord).Receipt.Status = "succeeded"
	(*sharedRecord).Receipt.FinishedAt = &finished
	(*sharedRecord).Receipt.OutputAvailable = true
	(*sharedRecord).Receipt.Output = &contractresource.PodExecutionOutput{Status: "succeeded", ExitCode: &code, Stdout: "private-output-marker", StdoutBytes: 21, StdoutEncoding: "utf-8", StderrEncoding: "utf-8", MaxOutputBytes: 65536, UIDVerifiedBeforeStart: true}
	if err := (*sharedRepository).Finish((*sharedCtx), (*sharedRecord)); err != nil {
		t.Fatal(err)
	}
	if err := (*sharedRepository).Finish((*sharedCtx), (*sharedRecord)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("late completion replaced original result")
	}
	item, err := (*sharedReopen)().Get((*sharedCtx), (*sharedRecord).ActorID, (*sharedRecord).Receipt.ID, "")
	if err != nil || item.Receipt.Output == nil || item.Receipt.Output.Stdout != "private-output-marker" {
		t.Fatalf("lost encrypted result: %v", err)
	}
}

func podExecutionReceiptWithPostgresPhase5(t *testing.T, sharedRepository **Repository, sharedCtx *context.Context, sharedRecord *domainresource.PodExecutionRecord) {

	if err := (*sharedRepository).db.Exec(`UPDATE platform_pod_executions SET expires_at=now()-interval '1 second' WHERE id=?`, (*sharedRecord).Receipt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := (*sharedRepository).Cleanup((*sharedCtx)); err != nil {
		t.Fatal(err)
	}
	var row executionRow
	if err := (*sharedRepository).db.Where("id = ?", (*sharedRecord).Receipt.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.EncryptedPayload != "" || row.ClusterID != "" || row.Namespace != "" || row.PodName != "" || row.ExpectedUID != "" || row.Container != "" {
		t.Fatal("expired payload or target metadata retained")
	}
	if _, err := (*sharedRepository).Get((*sharedCtx), (*sharedRecord).ActorID, (*sharedRecord).Receipt.ID, ""); !errors.Is(err, apperrors.ErrGone) {
		t.Fatal(err)
	}
	if _, won, err := (*sharedRepository).Claim((*sharedCtx), (*sharedRecord)); won || !errors.Is(err, apperrors.ErrGone) {
		t.Fatal("expired original key executed again")
	}
}

func podExecutionReceiptWithPostgresPhase6(t *testing.T, sharedRepository **Repository, sharedReopen *func() *Repository, sharedCtx *context.Context) {

	crashed := codecRecord()
	crashed.Receipt.ID = uuid.NewString()
	crashed.KeyHash = strings.Repeat("4", 64)
	if _, won, err := (*sharedRepository).Claim((*sharedCtx), crashed); err != nil || !won {
		t.Fatal(err)
	}
	if err := (*sharedRepository).db.Exec(`UPDATE platform_pod_executions SET deadline_at=now()-interval '1 second' WHERE id=?`, crashed.Receipt.ID).Error; err != nil {
		t.Fatal(err)
	}
	item, err := (*sharedReopen)().Get((*sharedCtx), crashed.ActorID, crashed.Receipt.ID, "")
	if err != nil || item.Receipt.Status != "unknown" || item.Receipt.OutputAvailable {
		t.Fatalf("crash falsely completed: %+v %v", item.Receipt, err)
	}
	finished := time.Now()
	crashed.Receipt.Status = "failed"
	crashed.Receipt.FinishedAt = &finished
	if err := (*sharedRepository).Finish((*sharedCtx), crashed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("late outcome overwrote uncertain attempt")
	}
	if err := (*sharedRepository).Cleanup((*sharedCtx)); err != nil {
		t.Fatal(err)
	}
}
