package cluster

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatalf("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}

func TestCreateRegistrationConflictsBeforeWritingCredentials(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO clusters .* ON CONFLICT \(id\) DO NOTHING$`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	err = New(db).CreateRegistration(t.Context(), domaincluster.Connection{Summary: domaincluster.Summary{ID: "duplicate"}})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate create = %v, want conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusSourceConflictChecksEquivalentEndpointURLs(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`WHERE c.id <> \$1 AND btrim\(ccm.metadata->>'prometheus_url'\) <> ''`).
		WithArgs("dedicated").
		WillReturnRows(sqlmock.NewRows([]string{"id", "url", "transport", "endpoint", "label", "value"}).
			AddRow("unrelated", "https://metrics-other.example", "direct", nil, nil, nil).
			AddRow("shared", "https://metrics.example:443/", "direct", nil, "cluster", "production"))
	conflict, err := New(db).HasPrometheusSourceConflict(t.Context(), domaincluster.Connection{
		Summary:  domaincluster.Summary{ID: "dedicated"},
		Metadata: map[string]any{"prometheus_url": "https://METRICS.example"},
	})
	if err != nil || !conflict {
		t.Fatalf("equivalent endpoint conflict = %v, err = %v", conflict, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
