package networkruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestEAPRevokeOwnershipAndIdempotency(t *testing.T) {
	for _, status := range []string{"active", "revoked", "not-owned"} {
		t.Run(status, func(t *testing.T) {
			db, mock := passwordTestDB(t)
			r := &Repository{db: db}
			now := time.Now().UTC()
			parent := domain.Credential{ID: "parent-1", RuntimeID: "endpoint-1", DeviceID: "device-1", SubjectID: "user-1"}
			request := networkprotocol.EAPCertificateRevokeRequest{RequestID: "revoke-1", CredentialID: "cert-1", Fingerprint: "sha256:" + strings.Repeat("a", 64), ReasonCode: "user_requested"}
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("network-endpoint-device:device-1").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT .*network_runtime_credentials`).WillReturnRows(sqlmock.NewRows([]string{"id", "runtime_id"}).AddRow("parent-1", "endpoint-1"))
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("network-runtime:endpoint-1").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`WITH current_control`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
			rows := sqlmock.NewRows([]string{"id", "status", "certificate_fingerprint"})
			if status != "not-owned" {
				rows.AddRow("cert-1", status, request.Fingerprint)
			}
			mock.ExpectQuery(`SELECT .*network_eap_credentials.*parent_credential_id.*runtime_id.*device_id.*subject_id.*certificate_fingerprint`).WithArgs("cert-1", "parent-1", "endpoint-1", "device-1", "user-1", request.Fingerprint, 1).WillReturnRows(rows)
			if status == "not-owned" {
				mock.ExpectRollback()
			} else {
				if status == "active" {
					mock.ExpectExec(`UPDATE "network_eap_credentials" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
				}
				mock.ExpectExec(`UPDATE network_runtime_sessions SET admission_restriction_reason`).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			result, err := r.RevokeEAPCertificate(context.Background(), parent, request, now)
			if status == "not-owned" {
				if !errors.Is(err, apperrors.ErrConflict) {
					t.Fatalf("error=%v", err)
				}
			} else if err != nil || result.Status != "revoked" {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
