package assistantmesh

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

type Record = domain.Record

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, item Record) error {
	if !canonicalUUID(item.ID) {
		return fmt.Errorf("%w: assistant mesh asset id must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	if !canonicalUUID(item.OwnerID) {
		return fmt.Errorf("%w: assistant mesh asset owner must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	if !validKind(item.Kind) {
		return fmt.Errorf("%w: invalid assistant mesh asset kind", apperrors.ErrInvalidArgument)
	}
	if err := validatePayload(item.Payload); err != nil {
		return err
	}
	if err := dbtx.DB(ctx, r.db).Exec(
		`INSERT INTO assistant_mesh_assets (id, owner_id, kind, payload) VALUES (?, ?, ?, ?::jsonb)`,
		item.ID, item.OwnerID, item.Kind, string(item.Payload),
	).Error; err != nil {
		return fmt.Errorf("create assistant mesh asset: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (Record, error) {
	if !canonicalUUID(id) {
		return Record{}, fmt.Errorf("%w: assistant mesh asset id must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	var item Record
	var payload []byte
	err := r.db.WithContext(ctx).Raw(
		`SELECT id, owner_id, kind, payload FROM assistant_mesh_assets WHERE id = ?`, id,
	).Row().Scan(&item.ID, &item.OwnerID, &item.Kind, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, apperrors.ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("get assistant mesh asset: %w", err)
	}
	item.Payload = payload
	return item, nil
}

func (r *Repository) List(ctx context.Context, kind string, ownerID string, limit, offset int) ([]Record, error) {
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 200", apperrors.ErrInvalidArgument)
	}
	if offset < 0 {
		return nil, fmt.Errorf("%w: offset must not be negative", apperrors.ErrInvalidArgument)
	}
	if kind != "" && !validKind(kind) {
		return nil, fmt.Errorf("%w: invalid assistant mesh asset kind", apperrors.ErrInvalidArgument)
	}
	if ownerID != "" && !canonicalUUID(ownerID) {
		return nil, fmt.Errorf("%w: assistant mesh asset owner must be a canonical UUID", apperrors.ErrInvalidArgument)
	}

	query := `SELECT id, owner_id, kind, payload FROM assistant_mesh_assets`
	args := []any{}
	if kind != "" {
		query += ` WHERE kind = ?`
		args = append(args, kind)
	}
	if ownerID != "" {
		if kind == "" {
			query += ` WHERE`
		} else {
			query += ` AND`
		}
		query += ` owner_id = ?`
		args = append(args, ownerID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("list assistant mesh assets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]Record, 0)
	for rows.Next() {
		var item Record
		var payload []byte
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Kind, &payload); err != nil {
			return nil, fmt.Errorf("scan assistant mesh asset: %w", err)
		}
		item.Payload = payload
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assistant mesh assets: %w", err)
	}
	return items, nil
}

func (r *Repository) Update(ctx context.Context, id string, mutate func(context.Context, Record) (json.RawMessage, error)) error {
	if !canonicalUUID(id) {
		return fmt.Errorf("%w: assistant mesh asset id must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	return dbtx.Within(ctx, r.db, func(txCtx context.Context) error {
		db := dbtx.DB(txCtx, r.db)
		var item Record
		var payload []byte
		err := db.Raw(
			`SELECT id, owner_id, kind, payload FROM assistant_mesh_assets WHERE id = ? FOR UPDATE`, id,
		).Row().Scan(&item.ID, &item.OwnerID, &item.Kind, &payload)
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock assistant mesh asset: %w", err)
		}
		item.Payload = payload
		updated, err := mutate(txCtx, item)
		if err != nil {
			return err
		}
		if err := validatePayload(updated); err != nil {
			return err
		}
		result := db.Exec(
			`UPDATE assistant_mesh_assets SET payload = ?::jsonb, updated_at = now() WHERE id = ?`,
			string(updated), id,
		)
		if result.Error != nil {
			return fmt.Errorf("update assistant mesh asset: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrNotFound
		}
		return nil
	})
}

func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func validKind(kind string) bool {
	return kind == domain.KindSkill || kind == domain.KindExperienceCard || kind == domain.KindWorkPulse || kind == "mcp"
}

func validatePayload(payload json.RawMessage) error {
	var object map[string]json.RawMessage
	if !json.Valid(payload) || json.Unmarshal(payload, &object) != nil || object == nil {
		return fmt.Errorf("%w: payload must be a JSON object", apperrors.ErrInvalidArgument)
	}
	return nil
}
