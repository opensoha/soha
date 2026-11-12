package networkruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func passwordTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	g, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return g, mock
}

func TestPasswordReceiptConsumptionBoundaries(t *testing.T) {
	for _, scenario := range []string{"success", "cross-nas", "cross-station", "expired", "cross-request-replay", "same-request-retry", "changed-password", "mfa", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock := passwordTestDB(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			receipt := domain.PasswordReceipt{TokenHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RuntimeID: "runtime-1", NASID: "nas-1", SiteID: "site-1", StationID: "02:00:00:00:00:01", SubjectID: "user-1", PasswordUpdatedAt: now.Add(-time.Hour), CreatedAt: now, ExpiresAt: now.Add(30 * time.Second)}
			status := "active"
			if scenario == "disabled" {
				status = "disabled"
			}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT status FROM users.*FOR UPDATE`).WithArgs("user-1").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(status))
			if scenario != "disabled" {
				version := receipt.PasswordUpdatedAt
				if scenario == "changed-password" {
					version = now
				}
				mock.ExpectQuery(`SELECT password_updated_at.*FOR SHARE`).WithArgs("user-1").WillReturnRows(sqlmock.NewRows([]string{"password_updated_at"}).AddRow(version))
				if scenario != "changed-password" {
					mock.ExpectQuery(`SELECT EXISTS.*identity_mfa_credentials`).WithArgs("user-1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(scenario == "mfa"))
					if scenario != "mfa" {
						nas, station, expires := receipt.NASID, receipt.StationID, receipt.ExpiresAt
						var consumed any
						switch scenario {
						case "cross-nas":
							nas = "other"
						case "cross-station":
							station = "02:00:00:00:00:02"
						case "expired":
							expires = now
						case "cross-request-replay":
							consumed = "other-request"
						case "same-request-retry":
							consumed = "request-1"
							expires = now.Add(-time.Second)
						}
						mock.ExpectQuery(`SELECT \* FROM network_password_receipts.*FOR UPDATE`).WithArgs(receipt.TokenHash).WillReturnRows(sqlmock.NewRows([]string{"token_hash", "runtime_id", "nas_id", "site_id", "station_id", "subject_id", "password_updated_at", "expires_at", "authorization_request_id"}).AddRow(receipt.TokenHash, receipt.RuntimeID, nas, receipt.SiteID, station, receipt.SubjectID, receipt.PasswordUpdatedAt, expires, consumed))
					}
				}
			}
			success := scenario == "success" || scenario == "same-request-retry"
			if scenario == "success" {
				mock.ExpectExec(`UPDATE "network_password_receipts" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if success {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := db.WithContext(context.Background()).Transaction(func(tx *gorm.DB) error { return consumePasswordReceipt(tx, receipt, "request-1", now) })
			if success && err != nil {
				t.Fatal(err)
			}
			if !success && !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Fatalf("error=%v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
