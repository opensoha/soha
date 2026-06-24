package scopegrant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

var ErrNotFound = fmt.Errorf("%w: scope grant not found", apperrors.ErrNotFound)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context) ([]domainscopegrant.Record, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, subject_type, subject_id, business_line_id, environment_ids, application_ids, role, effect, enabled, created_at, updated_at
		FROM scope_grants
		ORDER BY created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query scope grants: %w", err)
	}
	defer rows.Close()

	items := make([]domainscopegrant.Record, 0)
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (domainscopegrant.Record, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, subject_type, subject_id, business_line_id, environment_ids, application_ids, role, effect, enabled, created_at, updated_at
		FROM scope_grants
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanRow(row)
}

func (r *Repository) Create(ctx context.Context, input domainscopegrant.Input) (domainscopegrant.Record, error) {
	item := normalizeInput(input)
	environmentIDs, err := json.Marshal(item.EnvironmentIDs)
	if err != nil {
		return domainscopegrant.Record{}, fmt.Errorf("marshal scope grant environment ids: %w", err)
	}
	applicationIDs, err := json.Marshal(item.ApplicationIDs)
	if err != nil {
		return domainscopegrant.Record{}, fmt.Errorf("marshal scope grant application ids: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO scope_grants (id, subject_type, subject_id, business_line_id, environment_ids, application_ids, role, effect, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.SubjectType, item.SubjectID, item.BusinessLineID, string(environmentIDs), string(applicationIDs), item.Role, item.Effect, item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainscopegrant.Record{}, fmt.Errorf("create scope grant: %w", err)
	}
	return item, nil
}

func (r *Repository) Update(ctx context.Context, id string, input domainscopegrant.Input) (domainscopegrant.Record, error) {
	item := normalizeInput(input)
	item.ID = strings.TrimSpace(id)
	environmentIDs, err := json.Marshal(item.EnvironmentIDs)
	if err != nil {
		return domainscopegrant.Record{}, fmt.Errorf("marshal scope grant environment ids: %w", err)
	}
	applicationIDs, err := json.Marshal(item.ApplicationIDs)
	if err != nil {
		return domainscopegrant.Record{}, fmt.Errorf("marshal scope grant application ids: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE scope_grants
		SET subject_type = ?, subject_id = ?, business_line_id = ?, environment_ids = ?, application_ids = ?, role = ?, effect = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.SubjectType, item.SubjectID, item.BusinessLineID, string(environmentIDs), string(applicationIDs), item.Role, item.Effect, item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainscopegrant.Record{}, fmt.Errorf("update scope grant: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainscopegrant.Record{}, ErrNotFound
	}
	item.CreatedAt = fetchCreatedAt(ctx, r.db, item.ID)
	return item, nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM scope_grants WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete scope grant: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func scanRecord(rows *sql.Rows) (domainscopegrant.Record, error) {
	var item domainscopegrant.Record
	var environmentIDs []byte
	var applicationIDs []byte
	if err := rows.Scan(&item.ID, &item.SubjectType, &item.SubjectID, &item.BusinessLineID, &environmentIDs, &applicationIDs, &item.Role, &item.Effect, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainscopegrant.Record{}, fmt.Errorf("scan scope grant: %w", err)
	}
	_ = json.Unmarshal(environmentIDs, &item.EnvironmentIDs)
	_ = json.Unmarshal(applicationIDs, &item.ApplicationIDs)
	return item, nil
}

func scanRow(row *sql.Row) (domainscopegrant.Record, error) {
	var item domainscopegrant.Record
	var environmentIDs []byte
	var applicationIDs []byte
	if err := row.Scan(&item.ID, &item.SubjectType, &item.SubjectID, &item.BusinessLineID, &environmentIDs, &applicationIDs, &item.Role, &item.Effect, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainscopegrant.Record{}, ErrNotFound
		}
		return domainscopegrant.Record{}, fmt.Errorf("scan scope grant row: %w", err)
	}
	_ = json.Unmarshal(environmentIDs, &item.EnvironmentIDs)
	_ = json.Unmarshal(applicationIDs, &item.ApplicationIDs)
	return item, nil
}

func normalizeInput(input domainscopegrant.Input) domainscopegrant.Record {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	effect := strings.TrimSpace(input.Effect)
	if effect == "" {
		effect = "allow"
	}
	return domainscopegrant.Record{
		ID:             id,
		SubjectType:    strings.TrimSpace(input.SubjectType),
		SubjectID:      strings.TrimSpace(input.SubjectID),
		BusinessLineID: strings.TrimSpace(input.BusinessLineID),
		EnvironmentIDs: input.EnvironmentIDs,
		ApplicationIDs: input.ApplicationIDs,
		Role:           strings.TrimSpace(input.Role),
		Effect:         effect,
		Enabled:        input.Enabled,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func fetchCreatedAt(ctx context.Context, db *gorm.DB, id string) time.Time {
	var createdAt time.Time
	if err := db.WithContext(ctx).Raw(`SELECT created_at FROM scope_grants WHERE id = ?`, id).Row().Scan(&createdAt); err != nil {
		return time.Time{}
	}
	return createdAt
}
