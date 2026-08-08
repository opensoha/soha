package companion

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestApplyInteractionReplaysReceiptBeforeCooldown(t *testing.T) {
	repository, mock := newTestRepository(t)
	now := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	profile := testProfile(now)
	receipt := domaincompanion.InteractionReceipt{
		IdempotencyKey: "interaction-0001", Applied: true, XpAwarded: 2, AffinityAwarded: 2, Profile: profile, UnlockedIDs: []string{},
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	mock.ExpectBegin()
	expectEnsureProfile(mock, now)
	expectProfile(mock, profile)
	mock.ExpectQuery(`(?s)SELECT response, input_hash FROM companion_idempotency_receipts`).
		WithArgs("user-1", "interaction-0001").
		WillReturnRows(sqlmock.NewRows([]string{"response", "input_hash"}).AddRow(raw, "input-hash"))
	mock.ExpectCommit()

	got, created, err := repository.ApplyInteraction(context.Background(), domaincompanion.ApplyInteraction{
		OwnerID: "user-1", IdempotencyKey: "interaction-0001", InputHash: "input-hash",
		PluginID: domaincompanion.BuiltinPluginID, Version: domaincompanion.BuiltinVersion,
		InteractionID: "pet", Cooldown: 5 * time.Second, Now: now,
	})
	if err != nil {
		t.Fatalf("ApplyInteraction() error = %v", err)
	}
	if created {
		t.Fatal("ApplyInteraction() created = true, want replay")
	}
	if got.IdempotencyKey != receipt.IdempotencyKey || got.Profile.Revision != receipt.Profile.Revision {
		t.Fatalf("ApplyInteraction() receipt = %#v, want %#v", got, receipt)
	}
	assertExpectations(t, mock)
}

func TestApplyInteractionRejectsCooldownInTransaction(t *testing.T) {
	repository, mock := newTestRepository(t)
	now := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	profile := testProfile(now)

	mock.ExpectBegin()
	expectEnsureProfile(mock, now)
	expectProfile(mock, profile)
	mock.ExpectQuery(`(?s)SELECT response, input_hash FROM companion_idempotency_receipts`).
		WithArgs("user-1", "interaction-0002").
		WillReturnRows(sqlmock.NewRows([]string{"response", "input_hash"}))
	mock.ExpectQuery(`(?s)SELECT last_interaction_at FROM companion_interaction_states`).
		WithArgs("user-1", domaincompanion.BuiltinPluginID, "pet").
		WillReturnRows(sqlmock.NewRows([]string{"last_interaction_at"}).AddRow(now.Add(-time.Second)))
	mock.ExpectRollback()

	_, _, err := repository.ApplyInteraction(context.Background(), domaincompanion.ApplyInteraction{
		OwnerID: "user-1", IdempotencyKey: "interaction-0002", InputHash: "input-hash-2",
		PluginID: domaincompanion.BuiltinPluginID, Version: domaincompanion.BuiltinVersion,
		InteractionID: "pet", Cooldown: 5 * time.Second, ClientRevision: profile.Revision, Now: now,
	})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("ApplyInteraction() error = %v, want conflict", err)
	}
	assertExpectations(t, mock)
}

func newTestRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return New(db), mock
}

func testProfile(now time.Time) domaincompanion.Profile {
	return domaincompanion.Profile{
		ID: "companion:user-1", OwnerID: "user-1", ActivePluginID: domaincompanion.BuiltinPluginID,
		ActiveVersion: domaincompanion.BuiltinVersion, Level: 1, Xp: 2, Affinity: 2,
		UnlockedIDs: []string{}, Revision: 2, LastInteractionAt: &now, CreatedAt: now, UpdatedAt: now,
	}
}

func expectEnsureProfile(mock sqlmock.Sqlmock, now time.Time) {
	mock.ExpectExec(`(?s)INSERT INTO companion_profiles`).
		WithArgs("user-1", "companion:user-1", domaincompanion.BuiltinPluginID, domaincompanion.BuiltinVersion, now, now).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectProfile(mock sqlmock.Sqlmock, profile domaincompanion.Profile) {
	mock.ExpectQuery(`(?s)SELECT id, owner_id, active_plugin_id, active_version.*FROM companion_profiles WHERE owner_id = .*FOR UPDATE`).
		WithArgs(profile.OwnerID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_id", "active_plugin_id", "active_version", "level", "xp", "affinity",
			"unlocked_ids", "revision", "last_interaction_at", "created_at", "updated_at",
		}).AddRow(profile.ID, profile.OwnerID, profile.ActivePluginID, profile.ActiveVersion, profile.Level,
			profile.Xp, profile.Affinity, []byte(`[]`), profile.Revision, profile.LastInteractionAt, profile.CreatedAt, profile.UpdatedAt))
}

func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
