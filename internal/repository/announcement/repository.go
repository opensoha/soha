package announcement

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domainannouncement "github.com/kubecrux/kubecrux/internal/domain/announcement"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, limit int) ([]domainannouncement.Record, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, title, summary, content, level, status, audience, sticky,
		       starts_at, ends_at, published_at, created_by, updated_by, created_at, updated_at
		FROM announcements
		ORDER BY sticky DESC, updated_at DESC
		LIMIT ?
	`, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query announcements: %w", err)
	}
	defer rows.Close()

	items := make([]domainannouncement.Record, 0, limit)
	for rows.Next() {
		item, err := scanAnnouncementRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (domainannouncement.Record, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, title, summary, content, level, status, audience, sticky,
		       starts_at, ends_at, published_at, created_by, updated_by, created_at, updated_at
		FROM announcements
		WHERE id = ?
		LIMIT 1
	`, id).Row()

	var startsAt sql.NullTime
	var endsAt sql.NullTime
	var publishedAt sql.NullTime
	var createdAt time.Time
	var updatedAt time.Time
	var item domainannouncement.Record
	if err := row.Scan(
		&item.ID,
		&item.Title,
		&item.Summary,
		&item.Content,
		&item.Level,
		&item.Status,
		&item.Audience,
		&item.Sticky,
		&startsAt,
		&endsAt,
		&publishedAt,
		&item.CreatedBy,
		&item.UpdatedBy,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domainannouncement.Record{}, err
	}
	if startsAt.Valid {
		value := startsAt.Time.Format(time.RFC3339)
		item.StartsAt = &value
	}
	if endsAt.Valid {
		value := endsAt.Time.Format(time.RFC3339)
		item.EndsAt = &value
	}
	if publishedAt.Valid {
		value := publishedAt.Time.Format(time.RFC3339)
		item.PublishedAt = &value
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	return item, nil
}

func (r *Repository) Create(ctx context.Context, item domainannouncement.Record) (domainannouncement.Record, error) {
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO announcements (
			id, title, summary, content, level, status, audience, sticky,
			starts_at, ends_at, published_at, created_by, updated_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Title, item.Summary, item.Content, item.Level, item.Status, item.Audience, item.Sticky,
		nullTime(item.StartsAt), nullTime(item.EndsAt), nullTime(item.PublishedAt), item.CreatedBy, item.UpdatedBy,
		parseRFC3339(item.CreatedAt), parseRFC3339(item.UpdatedAt)).Error; err != nil {
		return domainannouncement.Record{}, err
	}
	return item, nil
}

func (r *Repository) Update(ctx context.Context, id string, item domainannouncement.Record) (domainannouncement.Record, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE announcements
		SET title = ?, summary = ?, content = ?, level = ?, status = ?, audience = ?, sticky = ?,
		    starts_at = ?, ends_at = ?, published_at = ?, updated_by = ?, updated_at = ?
		WHERE id = ?
	`, item.Title, item.Summary, item.Content, item.Level, item.Status, item.Audience, item.Sticky,
		nullTime(item.StartsAt), nullTime(item.EndsAt), nullTime(item.PublishedAt), item.UpdatedBy,
		parseRFC3339(item.UpdatedAt), id)
	if result.Error != nil {
		return domainannouncement.Record{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainannouncement.Record{}, gorm.ErrRecordNotFound
	}
	return item, nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM announcements WHERE id = ?`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func scanAnnouncementRows(rows *sql.Rows) (domainannouncement.Record, error) {
	var item domainannouncement.Record
	var startsAt sql.NullTime
	var endsAt sql.NullTime
	var publishedAt sql.NullTime
	var createdAt time.Time
	var updatedAt time.Time
	if err := rows.Scan(
		&item.ID,
		&item.Title,
		&item.Summary,
		&item.Content,
		&item.Level,
		&item.Status,
		&item.Audience,
		&item.Sticky,
		&startsAt,
		&endsAt,
		&publishedAt,
		&item.CreatedBy,
		&item.UpdatedBy,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domainannouncement.Record{}, err
	}
	if startsAt.Valid {
		value := startsAt.Time.Format(time.RFC3339)
		item.StartsAt = &value
	}
	if endsAt.Valid {
		value := endsAt.Time.Format(time.RFC3339)
		item.EndsAt = &value
	}
	if publishedAt.Valid {
		value := publishedAt.Time.Format(time.RFC3339)
		item.PublishedAt = &value
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	return item, nil
}

func parseRFC3339(value string) time.Time {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed
	}
	return time.Now().UTC()
}

func nullTime(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, *value); err == nil {
		return parsed
	}
	return nil
}
