package identitymfa_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	domainmfa "github.com/opensoha/soha/internal/domain/identitymfa"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	mfarepo "github.com/opensoha/soha/internal/repository/identitymfa"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
)

func mfaSessionStore(t *testing.T) *dbstore.Store {
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
	cfg.Name = "mfa_session_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

func TestMFASessionWithPostgres(t *testing.T) {
	store := mfaSessionStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	repo := mfarepo.New(store.DB())
	now := time.Now().UTC()
	user, session, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := store.Exec(ctx, `INSERT INTO users(id,username,email,status) VALUES($1,'owned-mfa','owned-mfa@example.test','active')`, user); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{session, other} {
		if err := store.Exec(ctx, `INSERT INTO sessions(id,user_id,refresh_token_id,provider_type,status,expires_at,metadata) VALUES($1,$2,$3,'cli_v2','active',$4,'{"cliProtocolVersion":2,"cliPermissionKeys":["network_access.access_grants.create"],"preserved":"owned-marker"}'::json)`, id, user, uuid.NewString(), now.Add(10*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	challenge := domainmfa.Challenge{ID: uuid.NewString(), UserID: user, SessionID: session, Type: domainmfa.ChallengeTypeTOTPEnrollment, SecretCiphertext: "owned-encrypted-fixture", MaxAttempts: 5, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
	if err := repo.CreateChallenge(ctx, challenge); err != nil {
		t.Fatal(err)
	}
	credential := &domainmfa.Credential{ID: uuid.NewString(), UserID: user, Type: "totp", DisplayName: "Owned fixture", SecretCiphertext: challenge.SecretCiphertext, CreatedAt: now}
	t.Run("complete_totp_preserves_cli_metadata", func(t *testing.T) {
		mFASessionWithPostgresPhase1(t, &store, &ctx, &repo, &now, &session, &challenge, &credential)
	})
	t.Run("recent_mfa_is_session_bound", func(t *testing.T) { mFASessionWithPostgresPhase2(t, &ctx, &repo, &now, &user, &session, &other) })
	t.Run("revoked_session_rolls_back_challenge_consumption", func(t *testing.T) {
		mFASessionWithPostgresPhase3(t, &store, &ctx, &repo, &now, &user, &other, &challenge)
	})
}

func mFASessionWithPostgresPhase1(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedRepo **mfarepo.Repository, sharedNow *time.Time, sharedSession *string, sharedChallenge *domainmfa.Challenge, sharedCredential **domainmfa.Credential) {

	if err := (*sharedRepo).CompleteChallenge((*sharedCtx), (*sharedChallenge), (*sharedCredential), "totp", (*sharedNow)); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			t.Fatalf("MFA transaction SQLSTATE=%s column=%s constraint=%s", pg.Code, pg.ColumnName, pg.ConstraintName)
		}
		t.Fatal(err)
	}
	var raw []byte
	if err := (*sharedStore).SQLDB().QueryRowContext((*sharedCtx), `SELECT metadata FROM sessions WHERE id=$1`, (*sharedSession)).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	permissionKeys, ok := metadata["cliPermissionKeys"].([]any)
	if !ok || metadata["mfa"] != true || metadata["preserved"] != "owned-marker" || metadata["cliProtocolVersion"] != float64(2) || len(permissionKeys) != 1 {
		t.Fatalf("MFA damaged existing session metadata: %s", raw)
	}
	if err := (*sharedRepo).CompleteChallenge((*sharedCtx), (*sharedChallenge), (*sharedCredential), "totp", (*sharedNow)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("consumed challenge err=%v", err)
	}
}

func mFASessionWithPostgresPhase2(t *testing.T, sharedCtx *context.Context, sharedRepo **mfarepo.Repository, sharedNow *time.Time, sharedUser *string, sharedSession *string, sharedOther *string) {

	ok, err := (*sharedRepo).HasRecentStepUp((*sharedCtx), (*sharedSession), (*sharedUser), (*sharedNow).Add(-time.Minute))
	if err != nil || !ok {
		t.Fatalf("original step-up ok=%t err=%v", ok, err)
	}
	ok, err = (*sharedRepo).HasRecentStepUp((*sharedCtx), (*sharedOther), (*sharedUser), (*sharedNow).Add(-time.Minute))
	if err != nil || ok {
		t.Fatalf("other session inherited MFA ok=%t err=%v", ok, err)
	}
	ok, err = (*sharedRepo).HasRecentStepUp((*sharedCtx), (*sharedSession), (*sharedUser), (*sharedNow).Add(time.Second))
	if err != nil || ok {
		t.Fatalf("old MFA accepted ok=%t err=%v", ok, err)
	}
}

func mFASessionWithPostgresPhase3(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedRepo **mfarepo.Repository, sharedNow *time.Time, sharedUser *string, sharedOther *string, sharedChallenge *domainmfa.Challenge) {

	(*sharedChallenge).ID, (*sharedChallenge).SessionID, (*sharedChallenge).Type = uuid.NewString(), (*sharedOther), domainmfa.ChallengeTypeTOTPStepUp
	if err := (*sharedRepo).CreateChallenge((*sharedCtx), (*sharedChallenge)); err != nil {
		t.Fatal(err)
	}
	if err := (*sharedStore).Exec((*sharedCtx), `UPDATE sessions SET status='revoked' WHERE id=$1`, (*sharedOther)); err != nil {
		t.Fatal(err)
	}
	if err := (*sharedRepo).CompleteChallenge((*sharedCtx), (*sharedChallenge), nil, "totp", (*sharedNow)); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("revoked session err=%v", err)
	}
	if _, err := (*sharedRepo).GetChallenge((*sharedCtx), (*sharedChallenge).ID, (*sharedUser), (*sharedOther), (*sharedNow)); err != nil {
		t.Fatalf("failed transaction consumed challenge: %v", err)
	}
}
