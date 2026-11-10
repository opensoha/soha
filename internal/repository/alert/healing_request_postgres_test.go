package alert_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	alertrepo "github.com/opensoha/soha/internal/repository/alert"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
)

func healingRequestStore(t *testing.T) *dbstore.Store {
	t.Helper()
	text := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if text == "" {
		t.Skip("requires this run's disposable PostgreSQL")
	}
	port, err := strconv.Atoi(text)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testpostgres.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 4, MaxIdleConns: 2})
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "healing_request_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

func TestHealingOriginalRequestWithPostgres(t *testing.T) {
	store := healingRequestStore(t)
	repo := alertrepo.New(store.DB())
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	now := time.Now().UTC()
	request := domainalert.HealingRequest{ID: uuid.NewString(), ActorID: "owned-actor", CredentialHash: "sha256:" + strings.Repeat("a", 64), Key: uuid.NewString(), EventID: "event:owned", PolicyID: "policy:owned", InputDigest: strings.Repeat("b", 64), RunID: "healing-run:" + uuid.NewString(), CreatedAt: now}
	if err := store.Exec(ctx, `INSERT INTO healing_policies(id,name,trigger_mode,workflow_template_id) VALUES($1,'Owned','manual','')`, request.PolicyID); err != nil {
		t.Fatal(err)
	}
	if err := store.Exec(ctx, `INSERT INTO alert_events(id,source_type,fingerprint,title,summary,severity,status,current_state) VALUES($1,'owned','owned','Owned','Owned fixture','warning','firing','firing')`, request.EventID); err != nil {
		t.Fatal(err)
	}
	input := domainalert.HealingRunInput{ID: request.RunID, EventID: request.EventID, PolicyID: request.PolicyID, RequestedBy: request.ActorID, Status: "pending_approval", ApprovalStatus: "pending", ExecutionGeneration: 1, SubmissionStatus: "not_submitted", IntentDigest: "sha256:" + strings.Repeat("c", 64)}
	var original domainalert.HealingRun
	t.Run("atomic_request_and_duplicate", func(t *testing.T) {
		healingOriginalRequestWithPostgresPhase1(t, &store, &repo, &ctx, &request, &input, &original)
	})
	t.Run("different_input_or_credential_denied", func(t *testing.T) { healingOriginalRequestWithPostgresPhase2(t, &repo, &ctx, &request, &input) })
	t.Run("concurrent_decision_single_winner", func(t *testing.T) { healingOriginalRequestWithPostgresPhase3(t, &repo, &ctx, &original) })
	t.Run("stale_generation_cannot_overwrite", func(t *testing.T) { healingOriginalRequestWithPostgresPhase4(t, &repo, &ctx, &original) })
	t.Run("failed_creation_rolls_back_claim", func(t *testing.T) { healingOriginalRequestWithPostgresPhase5(t, &repo, &ctx, &request, &input) })
	t.Run("receipt_tombstone_no_recreate", func(t *testing.T) {
		healingOriginalRequestWithPostgresPhase6(t, &store, &repo, &ctx, &request, &input, &original)
	})
}

func healingOriginalRequestWithPostgresPhase1(t *testing.T, sharedStore **dbstore.Store, sharedRepo **alertrepo.Repository, sharedCtx *context.Context, sharedRequest *domainalert.HealingRequest, sharedInput *domainalert.HealingRunInput, sharedOriginal *domainalert.HealingRun) {

	var err error
	(*sharedOriginal), err = (*sharedRepo).ClaimHealingRequest((*sharedCtx), (*sharedRequest), (*sharedInput))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		duplicate, err := (*sharedRepo).ClaimHealingRequest((*sharedCtx), (*sharedRequest), (*sharedInput))
		if err != nil || duplicate.ID != (*sharedOriginal).ID {
			t.Fatalf("duplicate ID=%s err=%v", duplicate.ID, err)
		}
	}
	var count int
	if err := (*sharedStore).SQLDB().QueryRowContext((*sharedCtx), `SELECT COUNT(*) FROM healing_runs WHERE id=$1`, (*sharedOriginal).ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("run count=%d err=%v", count, err)
	}
	(*sharedOriginal), err = (*sharedRepo).GetHealingRun((*sharedCtx), (*sharedOriginal).ID)
	if err != nil {
		t.Fatal(err)
	}
	if (*sharedOriginal).ExecutionGeneration != 1 || (*sharedOriginal).SubmissionStatus != "not_submitted" || (*sharedOriginal).IntentDigest != (*sharedInput).IntentDigest {
		t.Fatal("fence not stored")
	}
}

func healingOriginalRequestWithPostgresPhase2(t *testing.T, sharedRepo **alertrepo.Repository, sharedCtx *context.Context, sharedRequest *domainalert.HealingRequest, sharedInput *domainalert.HealingRunInput) {

	changed := (*sharedRequest)
	changed.InputDigest = strings.Repeat("d", 64)
	if _, err := (*sharedRepo).ClaimHealingRequest((*sharedCtx), changed, (*sharedInput)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed input=%v", err)
	}
	changed = (*sharedRequest)
	changed.CredentialHash = "sha256:" + strings.Repeat("e", 64)
	if _, err := (*sharedRepo).ClaimHealingRequest((*sharedCtx), changed, (*sharedInput)); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("other credential=%v", err)
	}
	if _, err := (*sharedRepo).GetHealingRequest((*sharedCtx), "other-actor", (*sharedRequest).Key); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("other actor=%v", err)
	}
}

func healingOriginalRequestWithPostgresPhase3(t *testing.T, sharedRepo **alertrepo.Repository, sharedCtx *context.Context, sharedOriginal *domainalert.HealingRun) {

	var wins atomic.Int32
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			next := (*sharedOriginal)
			next.ApprovalStatus = "approved"
			next.SubmissionStatus = "unknown"
			next.Status = "unknown"
			next.WorkflowRunID = "workflow:" + uuid.NewString()
			_, won, err := (*sharedRepo).CompareAndSwapHealingRun((*sharedCtx), (*sharedOriginal), next)
			if err != nil {
				t.Error(err)
			}
			if won {
				wins.Add(1)
			}
		})
	}
	group.Wait()
	if wins.Load() != 1 {
		t.Fatalf("dispatch winners=%d", wins.Load())
	}
}

func healingOriginalRequestWithPostgresPhase4(t *testing.T, sharedRepo **alertrepo.Repository, sharedCtx *context.Context, sharedOriginal *domainalert.HealingRun) {

	claimed, err := (*sharedRepo).GetHealingRun((*sharedCtx), (*sharedOriginal).ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.SubmissionStatus != "unknown" || claimed.WorkflowRunID == "" {
		t.Fatal("missing unknown fence")
	}
	stale := (*sharedOriginal)
	stale.Status = "rejected"
	stale.ApprovalStatus = "rejected"
	if _, won, err := (*sharedRepo).CompareAndSwapHealingRun((*sharedCtx), (*sharedOriginal), stale); err != nil || won {
		t.Fatalf("stale decision won=%t err=%v", won, err)
	}
	accepted := claimed
	accepted.SubmissionStatus = "accepted"
	accepted.Status = "failed"
	accepted, won, err := (*sharedRepo).CompareAndSwapHealingRun((*sharedCtx), claimed, accepted)
	if err != nil || !won {
		t.Fatalf("finish won=%t err=%v", won, err)
	}
	retry := accepted
	retry.ExecutionGeneration++
	retry.ApprovalStatus = "pending"
	retry.SubmissionStatus = "not_submitted"
	retry.WorkflowRunID = ""
	retry.Status = "pending_approval"
	if _, won, err := (*sharedRepo).CompareAndSwapHealingRun((*sharedCtx), accepted, retry); err != nil || !won {
		t.Fatalf("retry won=%t err=%v", won, err)
	}
	if _, won, err := (*sharedRepo).CompareAndSwapHealingRun((*sharedCtx), claimed, accepted); err != nil || won {
		t.Fatalf("old completion won=%t err=%v", won, err)
	}
}

func healingOriginalRequestWithPostgresPhase5(t *testing.T, sharedRepo **alertrepo.Repository, sharedCtx *context.Context, sharedRequest *domainalert.HealingRequest, sharedInput *domainalert.HealingRunInput) {

	collision := (*sharedRequest)
	collision.ID = uuid.NewString()
	collision.Key = uuid.NewString()
	if _, err := (*sharedRepo).ClaimHealingRequest((*sharedCtx), collision, (*sharedInput)); err == nil {
		t.Fatal("colliding run creation succeeded")
	}
	if _, err := (*sharedRepo).GetHealingRequest((*sharedCtx), collision.ActorID, collision.Key); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("failed transaction left claim=%v", err)
	}
}

func healingOriginalRequestWithPostgresPhase6(t *testing.T, sharedStore **dbstore.Store, sharedRepo **alertrepo.Repository, sharedCtx *context.Context, sharedRequest *domainalert.HealingRequest, sharedInput *domainalert.HealingRunInput, sharedOriginal *domainalert.HealingRun) {

	if err := (*sharedStore).Exec((*sharedCtx), `DELETE FROM healing_runs WHERE id=$1`, (*sharedOriginal).ID); err != nil {
		t.Fatal(err)
	}
	if _, err := (*sharedRepo).ClaimHealingRequest((*sharedCtx), (*sharedRequest), (*sharedInput)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("tombstone recreated=%v", err)
	}
	if _, err := (*sharedRepo).GetHealingRequest((*sharedCtx), (*sharedRequest).ActorID, (*sharedRequest).Key); err != nil {
		t.Fatalf("tombstone lost=%v", err)
	}
}
