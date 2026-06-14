package user

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
