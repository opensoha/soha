package outpostruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func expectInstanceGrant(mock sqlmock.Sqlmock, epoch string, expiry time.Time) {
	mock.ExpectQuery(`SELECT process_epoch,control_credential_hash.*FOR UPDATE`).WithArgs("edge", "pod").WillReturnRows(sqlmock.NewRows([]string{"process_epoch", "control_credential_hash", "control_configuration_version", "control_generation", "control_issued_at", "control_expires_at"}).AddRow(epoch, "credential", int64(4), int64(7), time.Now().UTC().Add(-time.Second), expiry))
}
func expectCredential(mock sqlmock.Sqlmock, valid bool) {
	mock.ExpectQuery(`SELECT EXISTS \([\s\S]*JOIN identity_outpost_runtime_leases`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(valid))
}

func TestControlGrantEpochExpiryRetirementAndCredentialFences(t *testing.T) {
	for _, test := range []struct {
		name, epoch                           string
		leaseActive, retired, credentialValid bool
		want                                  error
	}{
		{"same epoch renews bounded grant", "current", true, false, true, nil},
		{"new epoch blocked by live old grant", "new", true, false, true, apperrors.ErrConflict},
		{"new epoch retires expired process", "new", false, false, true, nil},
		{"retired epoch cannot reclaim", "retired", false, true, true, apperrors.ErrAccessDenied},
		{"rotation after identity precheck rejects claim", "current", false, false, false, apperrors.ErrAccessDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, mock := sqlRepository(t)
			now := time.Now().UTC()
			expiry := now.Add(-time.Second)
			if test.leaseActive {
				expiry = now.Add(20 * time.Second)
			}
			credential := domain.InstanceCredential{TokenHash: "credential", ConfigurationVersion: 4, ExpiresAt: now.Add(5 * time.Second)}
			expectLock(mock)
			expectInstanceGrant(mock, "current", expiry)
			expectCredential(mock, test.credentialValid)
			if test.credentialValid {
				mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM identity_outpost_runtime_retired_epochs`).WithArgs("edge", "pod", test.epoch).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(test.retired))
				if test.epoch != "current" && !test.retired && !test.leaseActive {
					mock.ExpectExec(`INSERT INTO identity_outpost_runtime_retired_epochs`).WithArgs("edge", "pod", "current", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if test.want == nil {
				mock.ExpectQuery(`SELECT id FROM identity_outposts WHERE id =`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
				encoded, _ := json.Marshal(domain.State{OutpostID: "edge", Owner: "soha", DesiredReplicas: 0, LatestTaskID: "task", ProjectedTaskID: "task", Generation: 7, Phase: "converged"})
				mock.ExpectQuery(`SELECT state FROM identity_outpost_runtime_deployments`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(encoded))
				mock.ExpectExec(`INSERT INTO identity_outpost_runtime_instances AS existing`).WithArgs("edge", "pod", test.epoch, "credential", int64(4), int64(7), sqlmock.AnyArg(), credential.ExpiresAt).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			control, err := repo.GrantControl(context.Background(), "edge", "pod", test.epoch, credential)
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("got %v", err)
				}
			} else if err != nil || control.Action != "drain" || control.ExpiresAt.After(credential.ExpiresAt) || control.ExpiresAt.Sub(control.IssuedAt) > 30*time.Second {
				t.Fatalf("control=%+v err=%v", control, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInstanceACKRejectsOldEpochFutureGenerationExpiryAndRotatedCredential(t *testing.T) {
	for _, test := range []struct {
		name, epoch              string
		generation               int64
		expired, credentialValid bool
		want                     error
	}{
		{"valid ACK is clipped to control lease", "current", 7, false, true, nil},
		{"old process ACK", "old", 7, false, true, apperrors.ErrAccessDenied},
		{"unissued generation ACK", "current", 8, false, true, apperrors.ErrConflict},
		{"expired control lease", "current", 7, true, true, apperrors.ErrConflict},
		{"rotation between identity precheck and persistence", "current", 7, false, false, apperrors.ErrAccessDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, mock := sqlRepository(t)
			now := time.Now().UTC()
			grantExpiry := now.Add(20 * time.Second)
			if test.expired {
				grantExpiry = now.Add(-time.Second)
			}
			item := domain.InstanceObservation{OutpostID: "edge", InstanceID: "pod", ProcessEpoch: test.epoch, CredentialHash: "credential", ConfigurationVersion: 4, Generation: test.generation, ObservedAt: now, LeaseExpiresAt: now.Add(time.Minute)}
			expectLock(mock)
			expectInstanceGrant(mock, "current", grantExpiry)
			expectCredential(mock, test.credentialValid)
			if test.want == nil {
				mock.ExpectExec(`UPDATE identity_outpost_runtime_instances SET generation`).WithArgs(int64(7), "credential", int64(4), false, int64(0), item.ObservedAt, grantExpiry, "edge", "pod", "current", item.ObservedAt).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := repo.RecordInstance(context.Background(), item)
			if test.want == nil && err != nil || test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDrainStatusExcludesOldCredentialAndExpiredControlGrant(t *testing.T) {
	repo, mock := sqlRepository(t)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT count\(\*\)[\s\S]*o.token_hash=i.credential_hash[\s\S]*l.configuration_version=i.configuration_version[\s\S]*i.control_expires_at>[\s\S]*i.control_credential_hash=i.credential_hash`).WithArgs("edge", int64(7), now.Add(-domain.ControlLeaseTTL), now, now, now).WillReturnRows(sqlmock.NewRows([]string{"count", "active", "accepting"}).AddRow(0, 0, false))
	status, err := repo.DrainStatus(context.Background(), "edge", 7, now)
	if err != nil || status.Instances != 0 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestControlGrantRejectsIssuanceClockRegression(t *testing.T) {
	repo, mock := sqlRepository(t)
	expectLock(mock)
	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT process_epoch,control_credential_hash.*FOR UPDATE`).WithArgs("edge", "pod").WillReturnRows(sqlmock.NewRows([]string{"process_epoch", "control_credential_hash", "control_configuration_version", "control_generation", "control_issued_at", "control_expires_at"}).AddRow("epoch", "credential", int64(4), int64(7), now.Add(time.Minute), now.Add(2*time.Minute)))
	mock.ExpectRollback()
	_, err := repo.GrantControl(context.Background(), "edge", "pod", "epoch", domain.InstanceCredential{TokenHash: "credential", ConfigurationVersion: 4, ExpiresAt: now.Add(time.Minute)})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("regressing issuedAt accepted: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
