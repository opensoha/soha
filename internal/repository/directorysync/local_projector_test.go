package directorysync

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestTrustedEmailRequiresExactConfiguredDomain(t *testing.T) {
	if !trustedEmail("User@Example.com", []string{"example.com"}) {
		t.Fatal("expected configured domain to be trusted")
	}
	for _, email := range []string{"", "missing-at", "user@evil-example.com"} {
		if trustedEmail(email, []string{"example.com"}) {
			t.Fatalf("trustedEmail(%q) = true", email)
		}
	}
}

func TestApplyRollsBackLocalOrganizationsWhenProjectionFails(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	projector := NewDatabaseProjector(db)
	connection := domain.Connection{ID: "connection-1", ProviderType: "feishu"}
	plan := domain.Plan{Organizations: []domain.Organization{{ExternalID: "department-1", Name: "研发部", Status: domain.ProjectionActive}}}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO teams").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE teams SET metadata").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE directory_organizations").WillReturnError(errors.New("projection failed"))
	mock.ExpectRollback()

	if err := projector.Apply(context.Background(), connection, domain.DefaultPolicy(connection.ID), plan); err == nil {
		t.Fatal("Apply() error = nil, want projection failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePersonReturnsRowsIterationError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	projector := NewDatabaseProjector(db)
	connection := domain.Connection{ID: "connection-1", ProviderType: "feishu", LoginProviderID: "provider-1"}
	policy := domain.DefaultPolicy(connection.ID)
	policy.VerifiedEmailAutoLink = true
	policy.TrustedEmailDomains = []string{"example.com"}
	person := domain.Person{ProviderSubject: "ou-1", Email: "user@example.com", EmailVerified: true}

	mock.ExpectQuery("SELECT user_id FROM identity_link_suppressions").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}))
	mock.ExpectQuery("SELECT user_id FROM user_identities").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}))
	mock.ExpectQuery("SELECT id FROM users").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("user-1").RowError(0, sqlmock.ErrCancelled))

	if _, _, err := projector.resolvePerson(db, connection, policy, person, person.ProviderSubject); err == nil {
		t.Fatal("resolvePerson() error = nil, want query error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStableIDIsDeterministicAndConnectionScoped(t *testing.T) {
	first := stableID("team", "connection-a", "department-1")
	if first != stableID("team", "connection-a", "department-1") {
		t.Fatal("stableID is not deterministic")
	}
	if first == stableID("team", "connection-b", "department-1") {
		t.Fatal("stableID must be scoped to a connection")
	}
}

func TestApplyOrganizationsMergesJSONMetadataWithoutJSONBTypeMismatch(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	projector := NewDatabaseProjector(db)
	projector.now = func() time.Time { return now }
	connection := domain.Connection{ID: "connection-1", ProviderType: "feishu"}
	organizations := []domain.Organization{{ExternalID: "department-1", Name: "研发部", Status: domain.ProjectionActive}}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("metadata=(COALESCE(teams.metadata,'{}'::json)::jsonb || EXCLUDED.metadata::jsonb)::json")).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("metadata=(COALESCE(metadata,'{}'::json)::jsonb || '{\"directoryStatus\":\"archived\"}'::jsonb)::json")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if err := projector.ApplyOrganizations(context.Background(), connection, organizations, false); err != nil {
		t.Fatalf("ApplyOrganizations() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
