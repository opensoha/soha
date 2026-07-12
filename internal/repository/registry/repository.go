package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainregistry "github.com/opensoha/soha/internal/domain/registry"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, limit int) ([]domainregistry.Connection, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, registry_type, endpoint, namespace, username, secret, insecure, metadata, created_at, updated_at
		FROM registry_connections
		ORDER BY name ASC, id ASC
		LIMIT ?
	`, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query registry connections: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainregistry.Connection, 0, limit)
	for rows.Next() {
		item, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Create(ctx context.Context, item domainregistry.Connection) (domainregistry.Connection, error) {
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return domainregistry.Connection{}, fmt.Errorf("marshal registry metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO registry_connections (id, name, registry_type, endpoint, namespace, username, secret, insecure, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.RegistryType, item.Endpoint, nullable(item.Namespace), nullable(item.Username), nullable(item.Secret), item.Insecure, string(metadata), parseTime(item.CreatedAt), parseTime(item.UpdatedAt)).Error; err != nil {
		return domainregistry.Connection{}, fmt.Errorf("create registry connection: %w", err)
	}
	return item, nil
}

func (r *Repository) Update(ctx context.Context, id string, item domainregistry.Connection) (domainregistry.Connection, error) {
	if item.Secret == "" {
		var existingSecret sql.NullString
		err := r.db.WithContext(ctx).Raw(`SELECT secret FROM registry_connections WHERE id = ?`, id).Row().Scan(&existingSecret)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domainregistry.Connection{}, registryNotFound(id)
			}
			return domainregistry.Connection{}, fmt.Errorf("query registry connection secret: %w", err)
		}
		if existingSecret.Valid {
			item.Secret = existingSecret.String
		}
	}
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return domainregistry.Connection{}, fmt.Errorf("marshal registry metadata: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE registry_connections
		SET name = ?, registry_type = ?, endpoint = ?, namespace = ?, username = ?, secret = ?, insecure = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, item.RegistryType, item.Endpoint, nullable(item.Namespace), nullable(item.Username), nullable(item.Secret), item.Insecure, string(metadata), parseTime(item.UpdatedAt), id)
	if result.Error != nil {
		return domainregistry.Connection{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainregistry.Connection{}, registryNotFound(id)
	}
	item.ID = id
	return item, nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM registry_connections WHERE id = ?`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return registryNotFound(id)
	}
	return nil
}

func scanConnection(rows *sql.Rows) (domainregistry.Connection, error) {
	var item domainregistry.Connection
	var namespace sql.NullString
	var username sql.NullString
	var secret sql.NullString
	var metadata []byte
	var createdAt time.Time
	var updatedAt time.Time
	if err := rows.Scan(&item.ID, &item.Name, &item.RegistryType, &item.Endpoint, &namespace, &username, &secret, &item.Insecure, &metadata, &createdAt, &updatedAt); err != nil {
		return domainregistry.Connection{}, err
	}
	if namespace.Valid {
		item.Namespace = namespace.String
	}
	if username.Valid {
		item.Username = username.String
	}
	if secret.Valid {
		item.Secret = secret.String
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	return item, nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func parseTime(value string) time.Time {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed
	}
	return time.Now().UTC()
}
