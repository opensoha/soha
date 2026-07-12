package directorysync

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/platform/keyring"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type captureString struct{ value string }

func (c *captureString) Match(value driver.Value) bool {
	c.value, _ = value.(string)
	return c.value != ""
}

func newRepositoryMock(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	key, err := keyring.NewKey("directory-test", "01234567890123456789012345678901", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return New(db, keys), mock
}

func TestUnlinkIdentityRollsBackWhenSuppressionInsertFails(t *testing.T) {
	repo, mock := newRepositoryMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT user_id,provider_type,provider_id,provider_user_id FROM user_identities`).WithArgs("identity-1").WillReturnRows(sqlmock.NewRows([]string{"user_id", "provider_type", "provider_id", "provider_user_id"}).AddRow("user-1", "feishu", "provider-1", "ou-1"))
	mock.ExpectQuery(`SELECT \(SELECT COUNT\(\*\) FROM user_password_credentials`).WithArgs("user-1", "user-1", "identity-1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec(`DELETE FROM user_identities`).WithArgs("identity-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO identity_link_suppressions`).WillReturnError(errors.New("suppression write failed"))
	mock.ExpectRollback()
	_, err := repo.UnlinkIdentity(context.Background(), "identity-1", "admin", time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "suppression write failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookCredentialEncryptedRoundTrip(t *testing.T) {
	repo, mock := newRepositoryMock(t)
	verification, encryption := &captureString{}, &captureString{}
	mock.ExpectExec(`INSERT INTO directory_webhook_credentials`).WithArgs("connection-1", verification, encryption, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	input := domain.WebhookCredential{ConnectionID: "connection-1", VerificationToken: "verify-secret", EncryptKey: "event-secret"}
	if err := repo.SetWebhookCredential(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(verification.value, "soha:v2:") || strings.Contains(verification.value, input.VerificationToken) {
		t.Fatalf("verification token not encrypted: %q", verification.value)
	}
	mock.ExpectQuery(`SELECT verification_token_encrypted,encrypt_key_encrypted`).WithArgs("connection-1").WillReturnRows(sqlmock.NewRows([]string{"verification_token_encrypted", "encrypt_key_encrypted"}).AddRow(verification.value, encryption.value))
	got, err := repo.GetWebhookCredential(context.Background(), "connection-1")
	if err != nil || got != input {
		t.Fatalf("round trip = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLDAPCredentialEncryptedRoundTrip(t *testing.T) {
	repo, mock := newRepositoryMock(t)
	username, password := &captureString{}, &captureString{}
	mock.ExpectExec(`INSERT INTO directory_connection_credentials`).WithArgs("connection-1", username, password, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	input := domain.ConnectionCredential{ConnectionID: "connection-1", Username: "cn=sync,dc=example,dc=com", Password: "ldap-secret"}
	if err := repo.SetConnectionCredential(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(password.value, "soha:v2:") || strings.Contains(password.value, input.Password) {
		t.Fatalf("password not encrypted: %q", password.value)
	}
	mock.ExpectQuery(`SELECT username_encrypted,password_encrypted`).WithArgs("connection-1").WillReturnRows(sqlmock.NewRows([]string{"username_encrypted", "password_encrypted"}).AddRow(username.value, password.value))
	got, err := repo.GetConnectionCredential(context.Background(), "connection-1")
	if err != nil || got != input {
		t.Fatalf("round trip = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueEventDeduplicatesProviderEvent(t *testing.T) {
	repo, mock := newRepositoryMock(t)
	mock.ExpectExec(`INSERT INTO directory_event_inbox`).WillReturnResult(sqlmock.NewResult(0, 0))
	inserted, err := repo.EnqueueEvent(context.Background(), domain.EventEnvelope{ID: "event-2", ConnectionID: "c", ProviderEventID: "provider-event-1", EventType: "updated", OccurredAt: time.Now(), ReceivedAt: time.Now()})
	if err != nil || inserted {
		t.Fatalf("inserted=%v err=%v", inserted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimEventsClaimsRetryableEventAtomically(t *testing.T) {
	repo, mock := newRepositoryMock(t)
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id,connection_id,provider_event_id.*FOR UPDATE SKIP LOCKED`).WithArgs(sqlmock.AnyArg(), 20).WillReturnRows(sqlmock.NewRows([]string{"id", "connection_id", "provider_event_id", "event_type", "occurred_at", "received_at", "status", "error_summary", "processed_at", "attempts", "claimed_at", "next_attempt_at"}).AddRow("e1", "c1", "pe1", "user.updated", now, now, "failed", "temporary", nil, 1, nil, now.Add(-time.Minute)))
	mock.ExpectExec(`UPDATE directory_event_inbox SET status='processing'`).WithArgs(sqlmock.AnyArg(), "e1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	items, err := repo.ClaimEvents(context.Background(), 20)
	if err != nil || len(items) != 1 || items[0].ID != "e1" {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryReleasesStaleWork(t *testing.T) {
	repo, mock := newRepositoryMock(t)
	before, at := time.Now().Add(-time.Minute), time.Now()
	mock.ExpectExec(`UPDATE directory_event_inbox SET status='failed'`).WithArgs(at, before).WillReturnResult(sqlmock.NewResult(0, 2))
	count, err := repo.RecoverStaleEvents(context.Background(), before, at)
	if err != nil || count != 2 {
		t.Fatalf("events count=%d err=%v", count, err)
	}
	mock.ExpectExec(`UPDATE directory_sync_runs SET status='failed'`).WithArgs(at, before, before).WillReturnResult(sqlmock.NewResult(0, 1))
	count, err = repo.RecoverStaleRuns(context.Background(), before, at)
	if err != nil || count != 1 {
		t.Fatalf("runs count=%d err=%v", count, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveSCIMConnectionRequiresScope(t *testing.T) {
	repo, mock := newRepositoryMock(t)
	mock.ExpectQuery(`t.scopes @>`).WithArgs("hash", `["scim.write"]`, domain.ProviderSCIM).WillReturnRows(sqlmock.NewRows([]string{"connection_id"}))
	_, err := repo.ResolveSCIMConnectionForScope(context.Background(), "hash", domain.SCIMScopeWrite)
	if err == nil {
		t.Fatal("expected unauthorized for missing scope")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
