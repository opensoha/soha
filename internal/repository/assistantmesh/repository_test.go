package assistantmesh

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestValidationRejectsBeforeDatabase(t *testing.T) {
	repository, mock, _ := newTestRepository(t)
	id, owner := uuid.NewString(), uuid.NewString()
	cases := []struct {
		name string
		call func() error
	}{
		{"non-canonical id", func() error {
			return repository.Create(context.Background(), Record{ID: "asset-1", OwnerID: owner, Kind: "skill", Payload: json.RawMessage(`{}`)})
		}},
		{"non-uuid owner", func() error {
			return repository.Create(context.Background(), Record{ID: id, OwnerID: "owner-1", Kind: "skill", Payload: json.RawMessage(`{}`)})
		}},
		{"unknown kind", func() error {
			return repository.Create(context.Background(), Record{ID: id, OwnerID: owner, Kind: "other", Payload: json.RawMessage(`{}`)})
		}},
		{"non-object payload", func() error {
			return repository.Create(context.Background(), Record{ID: id, OwnerID: owner, Kind: "skill", Payload: json.RawMessage(`[]`)})
		}},
		{"invalid get id", func() error { _, err := repository.Get(context.Background(), "asset-1"); return err }},
		{"limit below minimum", func() error { _, err := repository.List(context.Background(), "", "", 0, 0); return err }},
		{"limit above maximum", func() error { _, err := repository.List(context.Background(), "", "", 201, 0); return err }},
		{"negative offset", func() error { _, err := repository.List(context.Background(), "", "", 1, -1); return err }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("error = %v, want invalid argument", err)
			}
		})
	}
	assertExpectations(t, mock)
}

func TestCreateGetAndList(t *testing.T) {
	repository, mock, _ := newTestRepository(t)
	ctx := context.Background()
	id, owner := uuid.NewString(), uuid.NewString()
	payload := json.RawMessage(`{"title":"triage"}`)

	mock.ExpectExec(`INSERT INTO assistant_mesh_assets`).WithArgs(id, owner, "skill", string(payload)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.Create(ctx, Record{ID: id, OwnerID: owner, Kind: "skill", Payload: payload}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	mock.ExpectQuery(`SELECT id, owner_id, kind, payload FROM assistant_mesh_assets WHERE id = \$1`).WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_id", "kind", "payload"}).AddRow(id, owner, "skill", []byte(payload)))
	got, err := repository.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != id || got.OwnerID != owner || got.Kind != "skill" || string(got.Payload) != string(payload) {
		t.Fatalf("Get() = %#v", got)
	}

	mock.ExpectQuery(`SELECT id, owner_id, kind, payload FROM assistant_mesh_assets WHERE kind = \$1 AND owner_id = \$2 ORDER BY created_at DESC, id DESC LIMIT \$3 OFFSET \$4`).
		WithArgs("skill", owner, 25, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_id", "kind", "payload"}).AddRow(id, owner, "skill", []byte(payload)))
	items, err := repository.List(ctx, "skill", owner, 25, 5)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != id {
		t.Fatalf("List() = %#v", items)
	}

	mock.ExpectQuery(`SELECT id, owner_id, kind, payload FROM assistant_mesh_assets ORDER BY created_at DESC, id DESC LIMIT \$1 OFFSET \$2`).WithArgs(200, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_id", "kind", "payload"}))
	items, err = repository.List(ctx, "", "", 200, 0)
	if err != nil || len(items) != 0 {
		t.Fatalf("List(all) = %#v, %v", items, err)
	}

	missingID := uuid.NewString()
	mock.ExpectQuery(`SELECT id, owner_id, kind, payload FROM assistant_mesh_assets WHERE id = \$1`).WithArgs(missingID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_id", "kind", "payload"}))
	if _, err := repository.Get(ctx, missingID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Get(missing) error = %v, want not found", err)
	}
	assertExpectations(t, mock)
}

func TestUpdateLocksAndRollsBackOnCallbackError(t *testing.T) {
	repository, mock, db := newTestRepository(t)
	ctx := context.Background()
	id, owner := uuid.NewString(), uuid.NewString()
	original := json.RawMessage(`{"revision":1}`)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, owner_id, kind, payload FROM assistant_mesh_assets WHERE id = \$1 FOR UPDATE`).WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_id", "kind", "payload"}).AddRow(id, owner, "experience_card", []byte(original)))
	mock.ExpectExec(`INSERT INTO assistant_mesh_audit`).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE assistant_mesh_assets SET payload = \$1::jsonb, updated_at = now\(\) WHERE id = \$2`).
		WithArgs(`{"revision":2}`, id).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	updated := json.RawMessage(`{"revision":2}`)
	err := repository.Update(ctx, id, func(txCtx context.Context, current Record) (json.RawMessage, error) {
		if current.ID != id || current.OwnerID != owner || current.Kind != "experience_card" || string(current.Payload) != string(original) {
			t.Fatalf("mutate record = %#v", current)
		}
		return updated, dbtx.DB(txCtx, db).Exec(`INSERT INTO assistant_mesh_audit (asset_id) VALUES (?)`, id).Error
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, owner_id, kind, payload FROM assistant_mesh_assets WHERE id = \$1 FOR UPDATE`).WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_id", "kind", "payload"}).AddRow(id, owner, "experience_card", []byte(original)))
	mock.ExpectRollback()
	callbackErr := errors.New("audit failed")
	err = repository.Update(ctx, id, func(context.Context, Record) (json.RawMessage, error) { return nil, callbackErr })
	if !errors.Is(err, callbackErr) {
		t.Fatalf("Update() error = %v, want callback error", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, owner_id, kind, payload FROM assistant_mesh_assets WHERE id = \$1 FOR UPDATE`).WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_id", "kind", "payload"}))
	mock.ExpectRollback()
	if err := repository.Update(ctx, id, func(context.Context, Record) (json.RawMessage, error) { return updated, nil }); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Update(missing) error = %v, want not found", err)
	}
	assertExpectations(t, mock)
}

func newTestRepository(t *testing.T) (*Repository, sqlmock.Sqlmock, *gorm.DB) {
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
	return New(db), mock, db
}

func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
