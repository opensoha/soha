package identityprovider

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newOutpostSQLRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	})
	return New(gormDB), mock
}

func TestOutpostRuntimeGrantTransaction(t *testing.T) {
	for _, name := range []string{"persist", "rotation during claim", "configuration changed", "database failure"} {
		t.Run(name, func(t *testing.T) {
			repo, mock := newOutpostSQLRepository(t)
			now := time.Now().UTC()
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("identity-outpost-runtime:edge").WillReturnResult(sqlmock.NewResult(0, 1))
			lock := mock.ExpectQuery(`SELECT id FROM identity_outposts WHERE id = .* AND token_hash = .* FOR UPDATE`).WithArgs("edge", "token")
			if name == "rotation during claim" {
				lock.WillReturnError(sql.ErrNoRows)
				mock.ExpectRollback()
			} else {
				lock.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
				grant := mock.ExpectExec(`INSERT INTO identity_outpost_runtime_leases`).WithArgs("edge", "token", "digest", int64(2), now, now.Add(10*time.Minute), "edge", "digest", int64(2))
				switch name {
				case "persist":
					grant.WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit()
				case "configuration changed":
					grant.WillReturnResult(sqlmock.NewResult(0, 0))
					mock.ExpectRollback()
				case "database failure":
					grant.WillReturnError(errors.New("database unavailable"))
					mock.ExpectRollback()
				}
			}
			err := repo.RecordOutpostRuntimeLease(context.Background(), "edge", "token", "digest", 2, now, now.Add(10*time.Minute))
			if name == "persist" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("returned grant after failed transaction")
			}
		})
	}
}
func TestOutpostRuntimeLeaseValidation(t *testing.T) {
	for _, valid := range []bool{true, false} {
		repo, mock := newOutpostSQLRepository(t)
		expiry := time.Now().Add(time.Minute)
		mock.ExpectQuery(`SELECT EXISTS`).WithArgs("edge", "token", int64(2), expiry, expiry, expiry).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(valid))
		err := repo.ValidateOutpostRuntimeLease(context.Background(), "edge", "token", 2, &expiry)
		if valid && err != nil {
			t.Fatal(err)
		}
		if !valid && !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("invalid lease = %v", err)
		}
	}
}
func TestOutpostHeartbeatRejectsConcurrentLeaseOrCredentialChange(t *testing.T) {
	repo, mock := newOutpostSQLRepository(t)
	mock.ExpectExec(`UPDATE identity_outposts[\s\S]*AND token_hash = [\s\S]*EXISTS[\s\S]*identity_outpost_runtime_leases`).WillReturnResult(sqlmock.NewResult(0, 0))
	_, err := repo.RecordOutpostHeartbeat(context.Background(), domainprovider.Outpost{ID: "edge", TokenHash: "old-token", ProtocolVersion: "v1", AppliedConfigurationVersion: 1})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("late heartbeat = %v", err)
	}
}

func TestOutpostDeletionSerializesWithRuntimeBinding(t *testing.T) {
	for _, bound := range []bool{false, true} {
		repo, mock := newOutpostSQLRepository(t)
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT id FROM identity_outposts WHERE id = .*FOR UPDATE`).WithArgs("edge").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
		mock.ExpectQuery(`SELECT EXISTS .*identity_outpost_runtime_deployments`).WithArgs("edge").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(bound))
		if bound {
			mock.ExpectRollback()
		} else {
			mock.ExpectExec(`DELETE FROM identity_outposts`).WithArgs("edge").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
		}
		err := repo.DeleteOutpost(context.Background(), "edge")
		if bound && !errors.Is(err, apperrors.ErrConflict) || !bound && err != nil {
			t.Fatalf("bound=%t delete=%v", bound, err)
		}
	}
}

func TestLegacyOutpostAccessRequiresAnUnsignedExternalRegistration(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		repo, mock := newOutpostSQLRepository(t)
		mock.ExpectQuery(`SELECT EXISTS \([\s\S]*o.token_hash = [\s\S]*o.mode IN[\s\S]*o.protocol_version[\s\S]*NOT EXISTS \(SELECT 1 FROM identity_outpost_runtime_leases[\s\S]*d.state->>'owner' IS DISTINCT FROM 'external'`).
			WithArgs("edge", "token").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(allowed))
		err := repo.AuthorizeLegacyOutpostAccess(context.Background(), "edge", "token")
		if allowed && err != nil || !allowed && !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("allowed=%t authorize=%v", allowed, err)
		}
	}
}
