package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Get(ctx context.Context, key string) (map[string]any, bool, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT value
		FROM app_settings
		WHERE setting_key = ?
		LIMIT 1
	`, key).Row()

	var raw []byte
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	value := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, false, fmt.Errorf("decode app setting %s: %w", key, err)
		}
	}
	return value, true, nil
}

func (r *Repository) Upsert(ctx context.Context, key, category string, value map[string]any, updatedBy string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal app setting %s: %w", key, err)
	}
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO app_settings (setting_key, category, value, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (setting_key) DO UPDATE SET
			category = EXCLUDED.category,
			value = EXCLUDED.value,
			updated_by = EXCLUDED.updated_by,
			updated_at = EXCLUDED.updated_at
	`, key, category, string(encoded), updatedBy, now, now).Error
}
