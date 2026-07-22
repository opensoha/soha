package systemintegration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context, filter domain.Filter) ([]domain.Integration, error) {
	query := `SELECT id FROM system_integrations WHERE 1=1`
	args := make([]any, 0, 3)
	if filter.Category != "" {
		query += ` AND category = ?`
		args = append(args, filter.Category)
	}
	if filter.ProviderType != "" {
		query += ` AND provider_type = ?`
		args = append(args, filter.ProviderType)
	}
	if filter.Enabled != nil {
		query += ` AND enabled = ?`
		args = append(args, *filter.Enabled)
	}
	query += ` ORDER BY category, name, id`
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("list system integrations: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Integration, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		item, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (domain.Integration, error) {
	var item domain.Integration
	var configuration []byte
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, category, provider_type, name, description, enabled, configuration,
		       health_status, last_checked_at, last_error, version, created_by, updated_by, created_at, updated_at
		FROM system_integrations WHERE id = ?
	`, strings.TrimSpace(id)).Row().Scan(&item.ID, &item.Category, &item.ProviderType, &item.Name, &item.Description,
		&item.Enabled, &configuration, &item.HealthStatus, &item.LastCheckedAt, &item.LastError, &item.Version,
		&item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Integration{}, fmt.Errorf("%w: system integration not found", apperrors.ErrNotFound)
	}
	if err != nil {
		return domain.Integration{}, fmt.Errorf("get system integration: %w", err)
	}
	if err := decodeConfiguration(configuration, &item.Configuration); err != nil {
		return domain.Integration{}, fmt.Errorf("decode system integration configuration: %w", err)
	}
	credentials, err := r.Credentials(ctx, item.ID)
	if err != nil {
		return domain.Integration{}, err
	}
	item.CredentialKeys = sortedKeys(credentials)
	return item, nil
}

func (r *Repository) Create(ctx context.Context, item domain.Integration, credentials map[string]string) (domain.Integration, error) {
	configuration, err := encodeConfiguration(item.Configuration)
	if err != nil {
		return domain.Integration{}, fmt.Errorf("encode system integration configuration: %w", err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO system_integrations (id, category, provider_type, name, description, enabled, configuration,
				health_status, version, created_by, updated_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?, 1, ?, ?, ?, ?)
		`, item.ID, item.Category, item.ProviderType, item.Name, item.Description, item.Enabled, string(configuration),
			item.HealthStatus, item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return err
		}
		return upsertCredentials(tx, item.ID, credentials, item.UpdatedAt)
	})
	if err != nil {
		return domain.Integration{}, fmt.Errorf("create system integration: %w", err)
	}
	return r.Get(ctx, item.ID)
}

func (r *Repository) Update(ctx context.Context, item domain.Integration, expectedVersion int64, credentials map[string]string, clearKeys []string) (domain.Integration, error) {
	configuration, err := encodeConfiguration(item.Configuration)
	if err != nil {
		return domain.Integration{}, fmt.Errorf("encode system integration configuration: %w", err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			UPDATE system_integrations SET name=?, description=?, enabled=?, configuration=?::jsonb,
				health_status='unknown', last_error='', version=version+1, updated_by=?, updated_at=?
			WHERE id=? AND version=?
		`, item.Name, item.Description, item.Enabled, string(configuration), item.UpdatedBy, item.UpdatedAt, item.ID, expectedVersion)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Raw(`SELECT COUNT(1) FROM system_integrations WHERE id=?`, item.ID).Scan(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("%w: system integration not found", apperrors.ErrNotFound)
			}
			return fmt.Errorf("%w: system integration version changed", apperrors.ErrConflict)
		}
		if len(clearKeys) > 0 {
			if err := tx.Exec(`DELETE FROM system_integration_credentials WHERE integration_id=? AND credential_key IN ?`, item.ID, clearKeys).Error; err != nil {
				return err
			}
		}
		return upsertCredentials(tx, item.ID, credentials, item.UpdatedAt)
	})
	if err != nil {
		return domain.Integration{}, err
	}
	return r.Get(ctx, item.ID)
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM system_integrations WHERE id=?`, strings.TrimSpace(id))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: system integration not found", apperrors.ErrNotFound)
	}
	return nil
}

func (r *Repository) Credentials(ctx context.Context, id string) (map[string]string, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT credential_key, value_encrypted FROM system_integration_credentials WHERE integration_id=? ORDER BY credential_key`, id).Rows()
	if err != nil {
		return nil, fmt.Errorf("list system integration credentials: %w", err)
	}
	defer rows.Close()
	items := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		items[key] = value
	}
	return items, rows.Err()
}

func (r *Repository) UpdateHealth(ctx context.Context, id, status, lastError string, checkedAt time.Time) error {
	result := r.db.WithContext(ctx).Exec(`UPDATE system_integrations SET health_status=?, last_checked_at=?, last_error=?, updated_at=? WHERE id=?`, status, checkedAt, lastError, checkedAt, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: system integration not found", apperrors.ErrNotFound)
	}
	return nil
}

func upsertCredentials(tx *gorm.DB, id string, credentials map[string]string, updatedAt time.Time) error {
	for key, value := range credentials {
		if err := tx.Exec(`
			INSERT INTO system_integration_credentials (integration_id, credential_key, value_encrypted, updated_at)
			VALUES (?, ?, ?, ?) ON CONFLICT (integration_id, credential_key) DO UPDATE SET
				value_encrypted=EXCLUDED.value_encrypted, updated_at=EXCLUDED.updated_at
		`, id, key, value, updatedAt).Error; err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func encodeConfiguration(fields []sohaapi.SystemIntegrationConfigurationField) ([]byte, error) {
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		values[field.Key] = field.Value
	}
	return json.Marshal(values)
}

func decodeConfiguration(raw []byte, target *[]sohaapi.SystemIntegrationConfigurationField) error {
	values := map[string]string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
	}
	keys := sortedKeys(values)
	fields := make([]sohaapi.SystemIntegrationConfigurationField, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, sohaapi.SystemIntegrationConfigurationField{Key: key, Value: values[key]})
	}
	*target = fields
	return nil
}
