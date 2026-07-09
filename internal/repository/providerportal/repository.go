package providerportal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListApplications(ctx context.Context, filter domainportal.ApplicationFilter) ([]domainportal.Application, error) {
	var builder strings.Builder
	args := make([]any, 0)
	builder.WriteString(`
		SELECT id, slug, name, description, icon_url, category, tags, launch_url, provider_id,
		       provider_type, portal_visible, featured, sort_order, status, metadata,
		       created_by, updated_by, created_at, updated_at
		FROM identity_applications
		WHERE 1 = 1
	`)
	query := strings.TrimSpace(filter.Query)
	if query != "" {
		builder.WriteString(` AND (name ILIKE ? OR description ILIKE ? OR category ILIKE ? OR slug ILIKE ?)`)
		like := "%" + query + "%"
		args = append(args, like, like, like, like)
	}
	status := strings.TrimSpace(filter.Status)
	if status != "" {
		builder.WriteString(` AND status = ?`)
		args = append(args, status)
	}
	builder.WriteString(` ORDER BY featured DESC, sort_order ASC, name ASC, id ASC`)
	if filter.Limit > 0 {
		builder.WriteString(` LIMIT ?`)
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		builder.WriteString(` OFFSET ?`)
		args = append(args, filter.Offset)
	}
	items, err := r.scanApplications(ctx, builder.String(), args...)
	if err != nil {
		return nil, err
	}
	return r.withAssignments(ctx, items)
}

func (r *Repository) ListPortalApplications(ctx context.Context) ([]domainportal.Application, error) {
	items, err := r.scanApplications(ctx, `
		SELECT id, slug, name, description, icon_url, category, tags, launch_url, provider_id,
		       provider_type, portal_visible, featured, sort_order, status, metadata,
		       created_by, updated_by, created_at, updated_at
		FROM identity_applications
		WHERE portal_visible = TRUE AND status = ?
		ORDER BY featured DESC, sort_order ASC, name ASC, id ASC
	`, domainportal.ApplicationStatusEnabled)
	if err != nil {
		return nil, err
	}
	return r.withAssignments(ctx, items)
}

func (r *Repository) GetApplication(ctx context.Context, applicationID string) (domainportal.Application, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, slug, name, description, icon_url, category, tags, launch_url, provider_id,
		       provider_type, portal_visible, featured, sort_order, status, metadata,
		       created_by, updated_by, created_at, updated_at
		FROM identity_applications
		WHERE id = ? OR slug = ?
		LIMIT 1
	`, strings.TrimSpace(applicationID), strings.TrimSpace(applicationID)).Row()
	item, err := scanApplication(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainportal.Application{}, fmt.Errorf("%w: identity application not found", apperrors.ErrNotFound)
		}
		return domainportal.Application{}, err
	}
	assignments, err := r.ListAssignments(ctx, []string{item.ID})
	if err != nil {
		return domainportal.Application{}, err
	}
	item.Assignments = assignments[item.ID]
	return item, nil
}

func (r *Repository) CreateApplication(ctx context.Context, item domainportal.Application) (domainportal.Application, error) {
	if err := r.insertApplication(r.db.WithContext(ctx), item); err != nil {
		return domainportal.Application{}, err
	}
	return r.GetApplication(ctx, item.ID)
}

func (r *Repository) CreateApplicationWithAssignments(ctx context.Context, item domainportal.Application, assignments []domainportal.ApplicationAssignment) (domainportal.Application, error) {
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.insertApplication(tx, item); err != nil {
			return err
		}
		return replaceAssignmentsTx(tx, item.ID, assignments)
	}); err != nil {
		return domainportal.Application{}, err
	}
	return r.GetApplication(ctx, item.ID)
}

func (r *Repository) insertApplication(tx *gorm.DB, item domainportal.Application) error {
	tags, err := marshalJSON(item.Tags)
	if err != nil {
		return fmt.Errorf("marshal application tags: %w", err)
	}
	metadata, err := marshalJSON(item.Metadata)
	if err != nil {
		return fmt.Errorf("marshal application metadata: %w", err)
	}
	return tx.Exec(`
			INSERT INTO identity_applications (
				id, slug, name, description, icon_url, category, tags, launch_url, provider_id,
				provider_type, portal_visible, featured, sort_order, status, metadata,
			created_by, updated_by, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?)
		`, item.ID, item.Slug, item.Name, item.Description, nullableString(item.IconURL), item.Category, tags,
		item.LaunchURL, nullableString(item.ProviderID), item.ProviderType, item.PortalVisible, item.Featured,
		item.SortOrder, item.Status, metadata, item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt).Error
}

func (r *Repository) UpdateApplication(ctx context.Context, item domainportal.Application) (domainportal.Application, error) {
	if err := r.updateApplication(r.db.WithContext(ctx), item); err != nil {
		return domainportal.Application{}, err
	}
	return r.GetApplication(ctx, item.ID)
}

func (r *Repository) UpdateApplicationWithAssignments(ctx context.Context, item domainportal.Application, assignments []domainportal.ApplicationAssignment) (domainportal.Application, error) {
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.updateApplication(tx, item); err != nil {
			return err
		}
		return replaceAssignmentsTx(tx, item.ID, assignments)
	}); err != nil {
		return domainportal.Application{}, err
	}
	return r.GetApplication(ctx, item.ID)
}

func (r *Repository) updateApplication(tx *gorm.DB, item domainportal.Application) error {
	tags, err := marshalJSON(item.Tags)
	if err != nil {
		return fmt.Errorf("marshal application tags: %w", err)
	}
	metadata, err := marshalJSON(item.Metadata)
	if err != nil {
		return fmt.Errorf("marshal application metadata: %w", err)
	}
	result := tx.Exec(`
			UPDATE identity_applications
			SET slug = ?, name = ?, description = ?, icon_url = ?, category = ?, tags = ?::jsonb,
			    launch_url = ?, provider_id = ?, provider_type = ?, portal_visible = ?, featured = ?,
		    sort_order = ?, status = ?, metadata = ?::jsonb, updated_by = ?, updated_at = ?
		WHERE id = ?
	`, item.Slug, item.Name, item.Description, nullableString(item.IconURL), item.Category, tags,
		item.LaunchURL, nullableString(item.ProviderID), item.ProviderType, item.PortalVisible, item.Featured,
		item.SortOrder, item.Status, metadata, item.UpdatedBy, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: identity application not found", apperrors.ErrNotFound)
	}
	return nil
}

func (r *Repository) DeleteApplication(ctx context.Context, applicationID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM identity_applications WHERE id = ?`, strings.TrimSpace(applicationID))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: identity application not found", apperrors.ErrNotFound)
	}
	return nil
}

func (r *Repository) ValidateProviderBinding(ctx context.Context, providerID, applicationID, providerType string) error {
	var value int
	err := r.db.WithContext(ctx).Raw(`
		SELECT 1
		FROM identity_providers
		WHERE id = ? AND application_id = ? AND type = ?
		LIMIT 1
	`, strings.TrimSpace(providerID), strings.TrimSpace(applicationID), strings.TrimSpace(providerType)).Row().Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: identity provider is not bound to application", apperrors.ErrInvalidArgument)
		}
		return err
	}
	return nil
}

func (r *Repository) ReplaceAssignments(ctx context.Context, applicationID string, assignments []domainportal.ApplicationAssignment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return replaceAssignmentsTx(tx, applicationID, assignments)
	})
}

func replaceAssignmentsTx(tx *gorm.DB, applicationID string, assignments []domainportal.ApplicationAssignment) error {
	if err := tx.Exec(`DELETE FROM identity_application_assignments WHERE application_id = ?`, applicationID).Error; err != nil {
		return err
	}
	if len(assignments) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(assignments)*7)
	builder.WriteString(`
		INSERT INTO identity_application_assignments (
			id, application_id, subject_type, subject_id, effect, created_by, created_at
		)
		VALUES
	`)
	for index, item := range assignments {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, ?, ?, ?, ?)")
		args = append(args, item.ID, applicationID, item.SubjectType, item.SubjectID, item.Effect, item.CreatedBy, item.CreatedAt)
	}
	builder.WriteString(`
		ON CONFLICT (application_id, subject_type, subject_id, effect) DO UPDATE SET
			created_by = EXCLUDED.created_by,
			created_at = EXCLUDED.created_at
	`)
	return tx.Exec(builder.String(), args...).Error
}

func (r *Repository) ListAssignments(ctx context.Context, applicationIDs []string) (map[string][]domainportal.ApplicationAssignment, error) {
	out := map[string][]domainportal.ApplicationAssignment{}
	if len(applicationIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, application_id, subject_type, subject_id, effect, created_by, created_at
		FROM identity_application_assignments
		WHERE application_id IN ?
		ORDER BY subject_type ASC, subject_id ASC, effect ASC, id ASC
	`, compactStrings(applicationIDs)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domainportal.ApplicationAssignment
		if err := rows.Scan(&item.ID, &item.ApplicationID, &item.SubjectType, &item.SubjectID, &item.Effect, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		out[item.ApplicationID] = append(out[item.ApplicationID], item)
	}
	return out, rows.Err()
}

func (r *Repository) ListFavoriteApplicationIDs(ctx context.Context, userID string) (map[string]bool, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT application_id
		FROM identity_application_favorites
		WHERE user_id = ?
	`, strings.TrimSpace(userID)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var applicationID string
		if err := rows.Scan(&applicationID); err != nil {
			return nil, err
		}
		out[applicationID] = true
	}
	return out, rows.Err()
}

func (r *Repository) SetFavorite(ctx context.Context, userID, applicationID string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_application_favorites (user_id, application_id, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT (user_id, application_id) DO NOTHING
	`, strings.TrimSpace(userID), strings.TrimSpace(applicationID), now).Error
}

func (r *Repository) DeleteFavorite(ctx context.Context, userID, applicationID string) error {
	return r.db.WithContext(ctx).Exec(`
		DELETE FROM identity_application_favorites
		WHERE user_id = ? AND application_id = ?
	`, strings.TrimSpace(userID), strings.TrimSpace(applicationID)).Error
}

func (r *Repository) ListRecentLaunches(ctx context.Context, userID string, limit int) ([]domainportal.ApplicationLaunch, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT l.id, l.application_id, COALESCE(a.name, ''), l.user_id, l.provider_id, l.provider_type,
		       l.result, l.reason, l.launch_url, l.source_ip, l.user_agent, l.created_at
		FROM identity_application_launches l
		LEFT JOIN identity_applications a ON a.id = l.application_id
		WHERE l.user_id = ?
		ORDER BY l.created_at DESC
		LIMIT ?
	`, strings.TrimSpace(userID), limit).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domainportal.ApplicationLaunch, 0)
	for rows.Next() {
		var item domainportal.ApplicationLaunch
		var providerID sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.ApplicationID,
			&item.ApplicationName,
			&item.UserID,
			&providerID,
			&item.ProviderType,
			&item.Result,
			&item.Reason,
			&item.LaunchURL,
			&item.SourceIP,
			&item.UserAgent,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if providerID.Valid {
			item.ProviderID = providerID.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetLastLaunches(ctx context.Context, userID string) (map[string]time.Time, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT application_id, MAX(created_at)
		FROM identity_application_launches
		WHERE user_id = ?
		GROUP BY application_id
	`, strings.TrimSpace(userID)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var applicationID string
		var launchedAt time.Time
		if err := rows.Scan(&applicationID, &launchedAt); err != nil {
			return nil, err
		}
		out[applicationID] = launchedAt
	}
	return out, rows.Err()
}

func (r *Repository) RecordLaunch(ctx context.Context, item domainportal.ApplicationLaunch) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_application_launches (
			id, application_id, user_id, provider_id, provider_type, result,
			reason, launch_url, source_ip, user_agent, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ApplicationID, item.UserID, nullableString(item.ProviderID), item.ProviderType, item.Result,
		item.Reason, item.LaunchURL, item.SourceIP, item.UserAgent, item.CreatedAt).Error
}

func (r *Repository) scanApplications(ctx context.Context, query string, args ...any) ([]domainportal.Application, error) {
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domainportal.Application, 0)
	for rows.Next() {
		item, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) withAssignments(ctx context.Context, items []domainportal.Application) ([]domainportal.Application, error) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	assignments, err := r.ListAssignments(ctx, ids)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Assignments = assignments[items[index].ID]
	}
	return items, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanApplication(row scanner) (domainportal.Application, error) {
	var item domainportal.Application
	var tagsRaw, metadataRaw []byte
	var iconURL, providerID sql.NullString
	if err := row.Scan(
		&item.ID,
		&item.Slug,
		&item.Name,
		&item.Description,
		&iconURL,
		&item.Category,
		&tagsRaw,
		&item.LaunchURL,
		&providerID,
		&item.ProviderType,
		&item.PortalVisible,
		&item.Featured,
		&item.SortOrder,
		&item.Status,
		&metadataRaw,
		&item.CreatedBy,
		&item.UpdatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainportal.Application{}, err
	}
	if iconURL.Valid {
		item.IconURL = iconURL.String
	}
	if providerID.Valid {
		item.ProviderID = providerID.String
	}
	if err := unmarshalJSON(tagsRaw, &item.Tags); err != nil {
		return domainportal.Application{}, fmt.Errorf("decode application tags: %w", err)
	}
	if err := unmarshalJSON(metadataRaw, &item.Metadata); err != nil {
		return domainportal.Application{}, fmt.Errorf("decode application metadata: %w", err)
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func marshalJSON(value any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalJSON(raw []byte, out any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	return json.Unmarshal(raw, out)
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
