package apphosting

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMigration0154AppHostingSchema(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../migrations/postgres/0154_app_hosting.sql")
	if err != nil {
		t.Fatalf("read 0154_app_hosting.sql: %v", err)
	}
	ddl := string(raw)
	for _, token := range []string{
		"CREATE TABLE IF NOT EXISTS public.app_hosting_settings",
		"gateway_token_sha256 text NOT NULL",
		"CREATE TABLE IF NOT EXISTS public.hosted_apps",
		"dns_label text NOT NULL UNIQUE",
		"route_generation bigint NOT NULL DEFAULT 1",
		"idx_hosted_apps_owner_idempotency",
		"CREATE TABLE IF NOT EXISTS public.hosted_app_revisions",
		"REFERENCES public.hosted_apps(id) ON DELETE CASCADE",
		"manifest_json jsonb NOT NULL",
		"UNIQUE (app_id, revision_number)",
		"CREATE TABLE IF NOT EXISTS public.hosted_app_auth_tickets",
		"ticket_sha256 text PRIMARY KEY",
		"source_session_id text NOT NULL",
		"gateway_state_sha256 text NOT NULL",
		"CREATE TABLE IF NOT EXISTS public.hosted_app_rollback_idempotency",
	} {
		if !strings.Contains(ddl, token) {
			t.Fatalf("migration 0154 missing %q", token)
		}
	}
}

func newSQLMockGorm(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return gdb, mock
}

func TestPostgresRepositorySettingsQuotaTicketAndDNSConflict(t *testing.T) {
	gdb, mock := newSQLMockGorm(t)
	repo, err := NewPostgresRepository(gdb)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	// Default settings when singleton row does not exist yet.
	mock.ExpectQuery(`SELECT enabled, apps_domain.*FROM public\.app_hosting_settings`).
		WillReturnRows(sqlmock.NewRows([]string{"enabled"}))
	defaults, err := repo.GetSettings(ctx)
	if err != nil || defaults.MaxBundleCompressedBytes != domainapphosting.DefaultMaxBundleCompressedBytes {
		t.Fatalf("GetSettings defaults = %#v, %v", defaults, err)
	}

	// Quota exceeded on CreateApp.
	candidate := domainapphosting.App{
		ID:                 "app_1",
		Name:               "Demo",
		Kind:               sohaapi.HostedAppKindStatic,
		DNSLabel:           "demo-a1b2",
		OwnerUserID:        "usr_1",
		Visibility:         sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		Status:             sohaapi.HostedAppStatusDraft,
		RouteGeneration:    1,
		RequestFingerprint: "fp1",
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("hosted_apps/owner/usr_1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT COUNT\(1\) FROM public\.hosted_apps WHERE owner_user_id = \$1`).
		WithArgs("usr_1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectRollback()

	if _, _, err := repo.CreateApp(ctx, candidate, 2); !errors.Is(err, domainapphosting.ErrQuotaExceeded) {
		t.Fatalf("CreateApp quota = %v, want ErrQuotaExceeded", err)
	}

	// Unique DNS label violation maps to ErrDNSLabelConflict.
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("hosted_apps/owner/usr_1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT COUNT\(1\) FROM public\.hosted_apps WHERE owner_user_id = \$1`).
		WithArgs("usr_1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`INSERT INTO public\.hosted_apps`).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "hosted_apps_dns_label_key"})
	mock.ExpectRollback()

	if _, _, err := repo.CreateApp(ctx, candidate, 5); !errors.Is(err, appapphosting.ErrDNSLabelConflict) {
		t.Fatalf("CreateApp DNS collision = %v, want ErrDNSLabelConflict", err)
	}

	// ConsumeAuthTicket validates host and marks consumed_at once.
	now := time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC)
	ticketHash := strings.Repeat("f", 64)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT ticket_sha256.*FROM public\.hosted_app_auth_tickets.*FOR UPDATE`).
		WithArgs(ticketHash).
		WillReturnRows(sqlmock.NewRows([]string{
			"ticket_sha256", "app_id", "dns_label", "host", "route_generation", "return_path",
			"source_session_id", "gateway_state_sha256",
			"user_id", "username", "display_name", "email", "teams_json", "expires_at", "consumed_at", "created_at",
		}).AddRow(
			ticketHash, "app_1", "demo-a1b2", "demo-a1b2.apps.local:18090", int64(2), "/dashboard",
			"sess_1", "statehash_1",
			"usr_1", "alice", "Alice", "alice@example.com", []byte(`["team_eng"]`),
			now.Add(25*time.Second), nil, now,
		))
	mock.ExpectExec(`UPDATE public\.hosted_app_auth_tickets SET consumed_at`).
		WithArgs(now, ticketHash).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	consumed, err := repo.ConsumeAuthTicket(ctx, ticketHash, "demo-a1b2.apps.local:18090", now)
	if err != nil || consumed.ReturnPath != "/dashboard" || consumed.SourceSessionID != "sess_1" || len(consumed.Teams) != 1 {
		t.Fatalf("ConsumeAuthTicket = %#v, %v", consumed, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestPostgresAudienceDirectorySearchAndLiveVisitorResolution(t *testing.T) {
	gdb, mock := newSQLMockGorm(t)
	dir, err := NewPostgresAudienceDirectory(gdb)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	mock.ExpectQuery(`SELECT u\.id::text, u\.username.*FROM public\.users u`).
		WithArgs(`fin_100%`, `%fin\_100\%%`, `%fin\_100\%%`, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "display_name", "department"}).
			AddRow("usr_bob", "bob", "Bob", "Finance"))
	mock.ExpectQuery(`SELECT t\.id, t\.name, t\.slug FROM public\.teams t`).
		WithArgs(`fin_100%`, `%fin\_100\%%`, `%fin\_100\%%`, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug"}).
			AddRow("team_fin", "Finance", "finance"))

	candidates, err := dir.SearchCandidates(ctx, `fin_100%`, 10)
	if err != nil || len(candidates.Users) != 1 || len(candidates.Teams) != 1 {
		t.Fatalf("SearchCandidates = %#v, %v", candidates, err)
	}

	mock.ExpectQuery(`SELECT u\.id::text.*FROM public\.users u WHERE u\.id::text = \$1`).
		WithArgs("usr_disabled").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "display_name", "email", "status", "teams"}).
			AddRow("usr_disabled", "dave", "Dave", "dave@example.com", "disabled", []byte(`["team_fin"]`)))

	if _, err := dir.ResolveVisitor(ctx, "usr_disabled"); !errors.Is(err, domainapphosting.ErrVisitorDisabled) {
		t.Fatalf("ResolveVisitor disabled = %v, want ErrVisitorDisabled", err)
	}

	memRepo := NewMemoryRepository()
	now := time.Now().UTC()
	if err := memRepo.CreateAuthTicket(ctx, domainapphosting.AuthTicket{
		TicketSHA256: strings.Repeat("c", 64),
		AppID:        "app_1",
		DNSLabel:     "demo-a1b2",
		Host:         "demo-a1b2.apps.local",
		ReturnPath:   "/",
		ExpiresAt:    now.Add(30 * time.Second),
		CreatedAt:    now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := memRepo.ConsumeAuthTicket(ctx, strings.Repeat("c", 64), "wrong.apps.local", now); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("wrong host ConsumeAuthTicket = %v, want ErrAccessDenied", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
