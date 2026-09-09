package announcement

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domainannouncement "github.com/opensoha/soha/internal/domain/announcement"
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
		SELECT id, title, content, level, status, audience, sticky,
		       starts_at, ends_at, published_at, created_by, updated_by, created_at, updated_at
		FROM announcements
		ORDER BY sticky DESC, updated_at DESC
		LIMIT ?
	`, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query announcements: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
		SELECT id, title, content, level, status, audience, sticky,
		       starts_at, ends_at, published_at, created_by, updated_by, created_at, updated_at
		FROM announcements
		WHERE id = ?
		LIMIT 1
	`, id).Row()
	item, err := scanAnnouncementRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainannouncement.Record{}, announcementNotFound(id)
		}
		return domainannouncement.Record{}, err
	}
	return item, nil
}

func (r *Repository) Create(ctx context.Context, item domainannouncement.Record) (domainannouncement.Record, error) {
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO announcements (
			id, title, content, level, status, audience, sticky,
			starts_at, ends_at, published_at, created_by, updated_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Title, item.Content, item.Level, item.Status, item.Audience, item.Sticky,
		nullTime(item.StartsAt), nullTime(item.EndsAt), nullTime(item.PublishedAt), item.CreatedBy, item.UpdatedBy,
		parseRFC3339(item.CreatedAt), parseRFC3339(item.UpdatedAt)).Error; err != nil {
		return domainannouncement.Record{}, err
	}
	return r.Get(ctx, item.ID)
}

func (r *Repository) Update(ctx context.Context, id string, item domainannouncement.Record) (domainannouncement.Record, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE announcements
		SET title = ?, content = ?, level = ?, status = ?, audience = ?, sticky = ?,
		    starts_at = ?, ends_at = ?, published_at = ?, updated_by = ?, updated_at = ?
		WHERE id = ?
	`, item.Title, item.Content, item.Level, item.Status, item.Audience, item.Sticky,
		nullTime(item.StartsAt), nullTime(item.EndsAt), nullTime(item.PublishedAt), item.UpdatedBy,
		parseRFC3339(item.UpdatedAt), id)
	if result.Error != nil {
		return domainannouncement.Record{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainannouncement.Record{}, announcementNotFound(id)
	}
	return r.Get(ctx, id)
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM announcements WHERE id = ?`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return announcementNotFound(id)
	}
	return nil
}

func (r *Repository) Publish(ctx context.Context, id string, publishedAt time.Time, updatedBy string) (domainannouncement.Record, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			UPDATE announcements
			SET status = 'published', published_at = ?, updated_by = ?, updated_at = ?
			WHERE id = ?
		`, publishedAt, updatedBy, publishedAt, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return announcementNotFound(id)
		}
		if err := tx.Exec(`DELETE FROM announcement_receipts WHERE announcement_id = ?`, id).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return domainannouncement.Record{}, err
	}
	return r.Get(ctx, id)
}

func (r *Repository) Withdraw(ctx context.Context, id string, updatedAt time.Time, updatedBy string) (domainannouncement.Record, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE announcements
		SET status = 'draft', updated_by = ?, updated_at = ?
		WHERE id = ?
	`, updatedBy, updatedAt, id)
	if result.Error != nil {
		return domainannouncement.Record{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainannouncement.Record{}, announcementNotFound(id)
	}
	return r.Get(ctx, id)
}

func (r *Repository) ListInbox(ctx context.Context, userID string, limit int, now time.Time) (domainannouncement.Inbox, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT a.id, a.title, a.content, a.level, a.status, a.audience, a.sticky,
		       a.starts_at, a.ends_at, a.published_at, a.created_by, a.updated_by, a.created_at, a.updated_at,
		       ar.read_at
		FROM announcements a
		LEFT JOIN announcement_receipts ar
		  ON ar.announcement_id = a.id AND ar.user_id = ?
		WHERE a.status = 'published'
		  AND a.published_at IS NOT NULL
		  AND (a.starts_at IS NULL OR a.starts_at <= ?)
		  AND (a.ends_at IS NULL OR a.ends_at >= ?)
		ORDER BY a.sticky DESC, a.published_at DESC, a.updated_at DESC
		LIMIT ?
	`, userID, now, now, limit).Rows()
	if err != nil {
		return domainannouncement.Inbox{}, fmt.Errorf("query announcement inbox: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainannouncement.InboxItem, 0, limit)
	for rows.Next() {
		item, err := scanInboxItem(rows)
		if err != nil {
			return domainannouncement.Inbox{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domainannouncement.Inbox{}, err
	}

	var unreadCount int
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(1)
		FROM announcements a
		LEFT JOIN announcement_receipts ar
		  ON ar.announcement_id = a.id AND ar.user_id = ?
		WHERE a.status = 'published'
		  AND a.published_at IS NOT NULL
		  AND (a.starts_at IS NULL OR a.starts_at <= ?)
		  AND (a.ends_at IS NULL OR a.ends_at >= ?)
		  AND ar.read_at IS NULL
	`, userID, now, now).Row().Scan(&unreadCount); err != nil {
		return domainannouncement.Inbox{}, fmt.Errorf("count unread announcements: %w", err)
	}

	return domainannouncement.Inbox{
		Items:       items,
		UnreadCount: unreadCount,
	}, nil
}

func (r *Repository) MarkRead(ctx context.Context, announcementID, userID string, readAt time.Time) error {
	now := readAt.UTC()
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO announcement_receipts (
			id, announcement_id, user_id, read_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (announcement_id, user_id)
		DO UPDATE SET read_at = EXCLUDED.read_at, updated_at = EXCLUDED.updated_at
	`, uuid.NewString(), announcementID, userID, now, now, now).Error
}

func (r *Repository) ListReceipts(ctx context.Context, announcementID string, query domainannouncement.ReceiptQuery) (domainannouncement.ReceiptPage, error) {
	pattern := "%" + query.Keyword + "%"
	args := []any{announcementID, query.Keyword, pattern, pattern, pattern}
	var readCount, unreadCount int
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(ar.read_at) AS read_count,
		       COUNT(*) - COUNT(ar.read_at) AS unread_count
		FROM users u
		LEFT JOIN announcement_receipts ar
		  ON ar.announcement_id = ? AND ar.user_id = u.id
		WHERE u.status = 'active'
		  AND (? = '' OR u.username ILIKE ? OR COALESCE(u.display_name, '') ILIKE ? OR u.email ILIKE ?)
	`, args...).Row().Scan(&readCount, &unreadCount); err != nil {
		return domainannouncement.ReceiptPage{}, fmt.Errorf("count announcement receipts: %w", err)
	}

	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT u.id, u.username, COALESCE(u.display_name, ''), u.email,
		       COALESCE((
		         SELECT json_agg(t.name ORDER BY t.name)
		         FROM user_team_bindings utb
		         JOIN teams t ON t.id = utb.team_id
		         WHERE utb.user_id = u.id
		       ), '[]'::json),
		       ar.read_at
		FROM users u
		LEFT JOIN announcement_receipts ar
		  ON ar.announcement_id = ? AND ar.user_id = u.id
		WHERE u.status = 'active'
		  AND (? = '' OR u.username ILIKE ? OR COALESCE(u.display_name, '') ILIKE ? OR u.email ILIKE ?)
		  AND (? = 'all' OR (? = 'read' AND ar.read_at IS NOT NULL) OR (? = 'unread' AND ar.read_at IS NULL))
		ORDER BY (ar.read_at IS NULL) DESC, ar.read_at DESC NULLS LAST, u.username ASC, u.id ASC
		LIMIT ? OFFSET ?
	`, append(args, query.State, query.State, query.State, query.PageSize, (query.Page-1)*query.PageSize)...).Rows()
	if err != nil {
		return domainannouncement.ReceiptPage{}, fmt.Errorf("query announcement receipts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainannouncement.Receipt, 0, query.PageSize)
	for rows.Next() {
		var item domainannouncement.Receipt
		var teamNames []byte
		var readAt sql.NullTime
		if err := rows.Scan(&item.UserID, &item.Username, &item.DisplayName, &item.Email, &teamNames, &readAt); err != nil {
			return domainannouncement.ReceiptPage{}, fmt.Errorf("scan announcement receipt: %w", err)
		}
		if err := json.Unmarshal(teamNames, &item.TeamNames); err != nil {
			return domainannouncement.ReceiptPage{}, fmt.Errorf("decode announcement receipt teams: %w", err)
		}
		if readAt.Valid {
			value := readAt.Time.UTC().Format(time.RFC3339)
			item.IsRead = true
			item.ReadAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domainannouncement.ReceiptPage{}, fmt.Errorf("iterate announcement receipts: %w", err)
	}

	total := readCount + unreadCount
	switch query.State {
	case "read":
		total = readCount
	case "unread":
		total = unreadCount
	}
	return domainannouncement.ReceiptPage{
		Items:       items,
		Total:       total,
		Page:        query.Page,
		PageSize:    query.PageSize,
		ReadCount:   readCount,
		UnreadCount: unreadCount,
	}, nil
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
	return hydrateAnnouncementRecord(item, startsAt, endsAt, publishedAt, createdAt, updatedAt), nil
}

func scanAnnouncementRow(row *sql.Row) (domainannouncement.Record, error) {
	var item domainannouncement.Record
	var startsAt sql.NullTime
	var endsAt sql.NullTime
	var publishedAt sql.NullTime
	var createdAt time.Time
	var updatedAt time.Time
	if err := row.Scan(
		&item.ID,
		&item.Title,
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
	return hydrateAnnouncementRecord(item, startsAt, endsAt, publishedAt, createdAt, updatedAt), nil
}

func scanInboxItem(rows *sql.Rows) (domainannouncement.InboxItem, error) {
	var item domainannouncement.InboxItem
	var startsAt sql.NullTime
	var endsAt sql.NullTime
	var publishedAt sql.NullTime
	var createdAt time.Time
	var updatedAt time.Time
	var readAt sql.NullTime
	if err := rows.Scan(
		&item.ID,
		&item.Title,
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
		&readAt,
	); err != nil {
		return domainannouncement.InboxItem{}, err
	}
	item.Record = hydrateAnnouncementRecord(item.Record, startsAt, endsAt, publishedAt, createdAt, updatedAt)
	if readAt.Valid {
		value := readAt.Time.Format(time.RFC3339)
		item.IsRead = true
		item.ReadAt = &value
	}
	return item, nil
}

func hydrateAnnouncementRecord(
	item domainannouncement.Record,
	startsAt sql.NullTime,
	endsAt sql.NullTime,
	publishedAt sql.NullTime,
	createdAt time.Time,
	updatedAt time.Time,
) domainannouncement.Record {
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
	return item
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
