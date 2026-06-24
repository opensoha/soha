package user

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestRepositoryListTeamsDetailedAggregatesUserCountsWithoutGroupingJSON(t *testing.T) {
	repo, mock := newUserRepository(t)
	rows := sqlmock.NewRows([]string{
		"id",
		"parent_id",
		"name",
		"slug",
		"org_path",
		"source",
		"external_id",
		"metadata",
		"user_count",
	}).AddRow(
		"platform",
		"",
		"Platform",
		"platform",
		"/platform",
		"local",
		"",
		[]byte(`{"costCenter":"eng"}`),
		2,
	)
	mock.ExpectQuery(`(?s)SELECT\s+t\.id,.*COALESCE\(user_counts\.user_count, 0\) AS user_count\s+FROM teams t\s+LEFT JOIN \(\s+SELECT team_id, COUNT\(DISTINCT user_id\) AS user_count\s+FROM user_team_bindings\s+GROUP BY team_id\s+\) user_counts ON user_counts\.team_id = t\.id\s+ORDER BY`).
		WillReturnRows(rows)

	items, err := repo.ListTeamsDetailed(context.Background())
	if err != nil {
		t.Fatalf("ListTeamsDetailed returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
	item := items[0]
	if item.ID != "platform" || item.UserCount != 2 || item.Metadata["costCenter"] != "eng" {
		t.Fatalf("unexpected team item: %#v", item)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func newUserRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm postgres mock: %v", err)
	}
	return New(db), mock
}

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatalf("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}

func TestRepositoryMigrateOIDCIdentityMovesProviderKey(t *testing.T) {
	repo, mock := newUserRepository(t)
	lastLoginAt := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)

	mock.ExpectExec(`(?s)UPDATE user_identities\s+SET provider_id = \$1, profile = \$2, last_login_at = \$3, updated_at = \$4\s+WHERE provider_type = \$5 AND provider_id = \$6 AND provider_user_id = \$7`).
		WithArgs(
			"new-provider",
			sqlmock.AnyArg(),
			lastLoginAt,
			sqlmock.AnyArg(),
			"oidc",
			"legacy-provider",
			"sub-1",
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.MigrateOIDCIdentity(context.Background(), OIDCIdentity{
		ID:             "identity-1",
		UserID:         "u1",
		ProviderType:   "oidc",
		ProviderID:     "legacy-provider",
		ProviderUserID: "sub-1",
		Profile:        map[string]any{"email": "user@example.com"},
		LastLoginAt:    lastLoginAt,
	}, "new-provider")
	if err != nil {
		t.Fatalf("MigrateOIDCIdentity returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
