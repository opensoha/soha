package assistantmesh

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

const (
	receptionRetention = 7 * 24 * time.Hour
	receptionPageSize  = 100
	inquiryColumns     = `id, caller_id, owner_id, request_id, question_key, question, status, answer, citations, created_at, due_at, closed_at, purge_after`
)

type inquiryCounts struct {
	pairHour  int
	callerDay int
	ownerOpen int
}

func (r *Repository) GetProfile(ctx context.Context, ownerID string) (domain.Profile, error) {
	if !canonicalUUID(ownerID) {
		return domain.Profile{}, fmt.Errorf("%w: assistant mesh profile owner must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	var profile domain.Profile
	err := dbtx.DB(ctx, r.db).Raw(
		`SELECT open, away, intro FROM assistant_mesh_profiles WHERE owner_id = ?`, ownerID,
	).Row().Scan(&profile.Open, &profile.Away, &profile.Intro)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Profile{OwnerID: ownerID}, nil
	}
	if err != nil {
		return domain.Profile{}, fmt.Errorf("get assistant mesh profile: %w", err)
	}
	profile.OwnerID = ownerID
	return profile, nil
}

func (r *Repository) PutProfile(ctx context.Context, profile domain.Profile, now time.Time, retention time.Duration) error {
	if !canonicalUUID(profile.OwnerID) {
		return fmt.Errorf("%w: assistant mesh profile owner must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	now, retention = receptionTimes(now, retention)
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		if profile.Open {
			if err := lockInquiryParticipants(ctx, db, profile.OwnerID, profile.OwnerID); err != nil {
				return err
			}
		}
		if err := db.Exec(`
			INSERT INTO assistant_mesh_profiles (owner_id, open, away, intro, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (owner_id) DO UPDATE
			SET open = EXCLUDED.open, away = EXCLUDED.away, intro = EXCLUDED.intro, updated_at = EXCLUDED.updated_at
		`, profile.OwnerID, profile.Open, profile.Away, profile.Intro, now, now).Error; err != nil {
			return fmt.Errorf("put assistant mesh profile: %w", err)
		}
		if profile.Open {
			return nil
		}
		if err := db.Exec(`
			UPDATE assistant_mesh_inquiries
			SET status = ?, closed_at = ?, purge_after = COALESCE(purge_after, ?)
			WHERE owner_id = ? AND status IN (?, ?) AND due_at > ?
		`, domain.InquiryInvalidated, now, now.Add(retention), profile.OwnerID, domain.InquiryPending, domain.InquiryProcessing, now).Error; err != nil {
			return fmt.Errorf("close assistant mesh profile: %w", err)
		}
		return nil
	})
}

func (r *Repository) ListProfiles(ctx context.Context, afterID string, limit int) ([]domain.Profile, error) {
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 200", apperrors.ErrInvalidArgument)
	}
	if afterID != "" && !canonicalUUID(afterID) {
		return nil, fmt.Errorf("%w: profile cursor must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	query := `SELECT owner_id, open, away, intro FROM assistant_mesh_profiles WHERE open`
	args := []any{}
	if afterID != "" {
		query += ` AND owner_id > ?`
		args = append(args, afterID)
	}
	query += ` ORDER BY owner_id LIMIT ?`
	args = append(args, limit)
	rows, err := dbtx.DB(ctx, r.db).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("list assistant mesh profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []domain.Profile{}
	for rows.Next() {
		var profile domain.Profile
		if err := rows.Scan(&profile.OwnerID, &profile.Open, &profile.Away, &profile.Intro); err != nil {
			return nil, fmt.Errorf("scan assistant mesh profile: %w", err)
		}
		items = append(items, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assistant mesh profiles: %w", err)
	}
	return items, nil
}

func (r *Repository) GetInquiry(ctx context.Context, callerID, requestID string) (domain.Inquiry, error) {
	if !canonicalUUID(callerID) {
		return domain.Inquiry{}, fmt.Errorf("%w: inquiry caller must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(requestID) == "" {
		return domain.Inquiry{}, fmt.Errorf("%w: inquiry request is required", apperrors.ErrInvalidArgument)
	}
	item, found, err := inquiryByRequest(ctx, dbtx.DB(ctx, r.db), callerID, requestID)
	if err != nil {
		return domain.Inquiry{}, err
	}
	if !found {
		return domain.Inquiry{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *Repository) CreateInquiry(ctx context.Context, item domain.Inquiry, limits domain.InquiryLimits) (domain.Inquiry, error) {
	now := item.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
		item.CreatedAt = now
	}
	if err := validateInquiry(item, now, limits); err != nil {
		return domain.Inquiry{}, err
	}
	citations := item.Citations
	if citations == nil {
		citations = []domain.Citation{}
	}
	citationsJSON, err := json.Marshal(citations)
	if err != nil {
		return domain.Inquiry{}, fmt.Errorf("encode inquiry citations: %w", err)
	}

	var result domain.Inquiry
	err = dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		if existing, found, err := inquiryByRequest(ctx, db, item.CallerID, item.RequestID); err != nil {
			return err
		} else if found {
			result, err = inquiryReceipt(existing, item)
			return err
		}
		if err := lockInquiryParticipants(ctx, db, item.CallerID, item.OwnerID); err != nil {
			return err
		}
		if existing, found, err := inquiryByRequest(ctx, db, item.CallerID, item.RequestID); err != nil {
			return err
		} else if found {
			result, err = inquiryReceipt(existing, item)
			return err
		}
		open, err := lockInquiryOwner(ctx, db, item.OwnerID)
		if err != nil {
			return err
		}
		if !open {
			return fmt.Errorf("%w: recipient is not accepting inquiries", apperrors.ErrConflict)
		}
		if (item.Status == domain.InquiryPending || item.Status == domain.InquiryProcessing) && limits.MergeWindow > 0 {
			if merged, found, err := mergeableInquiry(ctx, db, item, now, limits.MergeWindow); err != nil {
				return err
			} else if found {
				if err := aliasInquiry(ctx, db, item.CallerID, item.RequestID, merged.ID); err != nil {
					return err
				}
				result = merged
				return nil
			}
		}
		if err := checkInquiryCapacity(ctx, db, item, limits, now); err != nil {
			return err
		}
		result, err = insertInquiry(ctx, db, item, citationsJSON)
		return err
	})
	return result, err
}

func checkInquiryCapacity(ctx context.Context, db *gorm.DB, item domain.Inquiry, limits domain.InquiryLimits, now time.Time) error {
	counts, err := inquiryAdmissionCounts(ctx, db, item.CallerID, item.OwnerID, now)
	if err != nil {
		return err
	}
	if item.Status == domain.InquiryPending && limits.OwnerPending > 0 && counts.ownerOpen >= limits.OwnerPending {
		return errors.Join(apperrors.ErrConflict, domain.ErrInquiryInboxFull)
	}
	if limits.PairPerHour > 0 && counts.pairHour >= limits.PairPerHour {
		return fmt.Errorf("%w: inquiry pair hourly limit reached", domain.ErrInquiryRateLimited)
	}
	if limits.CallerPerDay > 0 && counts.callerDay >= limits.CallerPerDay {
		return fmt.Errorf("%w: inquiry caller daily limit reached", domain.ErrInquiryRateLimited)
	}
	return nil
}

func (r *Repository) ListInquiries(ctx context.Context, userID string, inbox bool, now time.Time, retention time.Duration) ([]domain.Inquiry, error) {
	if !canonicalUUID(userID) {
		return nil, fmt.Errorf("%w: inquiry participant must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	now, retention = receptionTimes(now, retention)
	items := []domain.Inquiry{}
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		if err := refreshInquiries(ctx, db, userID, now, retention); err != nil {
			return err
		}
		query := `SELECT ` + inquiryColumns + ` FROM assistant_mesh_inquiries WHERE (owner_id = ? OR caller_id = ?)`
		args := []any{userID, userID}
		if inbox {
			query += ` AND owner_id = ? AND escalated`
		} else {
			query += ` AND caller_id = ?`
		}
		args = append(args, userID)
		query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
		args = append(args, receptionPageSize)
		rows, err := db.Raw(query, args...).Rows()
		if err != nil {
			return fmt.Errorf("list assistant mesh inquiries: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			item, err := scanInquiry(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("list assistant mesh inquiries: %w", err)
		}
		return nil
	})
	return items, err
}

func (r *Repository) ReplyInquiry(ctx context.Context, ownerID, inquiryID, answer string, now time.Time, retention time.Duration) (domain.Inquiry, error) {
	if !canonicalUUID(ownerID) {
		return domain.Inquiry{}, fmt.Errorf("%w: inquiry owner must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	if !canonicalUUID(inquiryID) {
		return domain.Inquiry{}, fmt.Errorf("%w: inquiry id must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	now, retention = receptionTimes(now, retention)
	var item domain.Inquiry
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		var open bool
		err := db.Raw(`SELECT open FROM assistant_mesh_profiles WHERE owner_id = ? FOR UPDATE`, ownerID).Row().Scan(&open)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !open) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock assistant mesh profile: %w", err)
		}
		item, err = scanInquiry(db.Raw(`
			UPDATE assistant_mesh_inquiries AS i
			SET status = ?, answer = ?, closed_at = ?, purge_after = ?
			WHERE i.id = ? AND i.owner_id = ? AND i.status = ? AND i.due_at > ?
			  AND EXISTS (SELECT 1 FROM assistant_mesh_profiles p WHERE p.owner_id = i.owner_id AND p.open)
			  AND EXISTS (SELECT 1 FROM users u WHERE u.id = i.owner_id AND u.status = 'active')
			  AND EXISTS (SELECT 1 FROM users u WHERE u.id = i.caller_id AND u.status = 'active')
			RETURNING `+inquiryColumns,
			domain.InquiryReplied, answer, now, now.Add(retention), inquiryID, ownerID,
			domain.InquiryPending, now,
		).Row())
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrNotFound
		}
		return err
	})
	return item, err
}

func receptionTimes(now time.Time, retention time.Duration) (time.Time, time.Duration) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if retention <= 0 {
		retention = receptionRetention
	}
	return now, retention
}

func validateInquiry(item domain.Inquiry, now time.Time, limits domain.InquiryLimits) error {
	if !canonicalUUID(item.CallerID) || !canonicalUUID(item.OwnerID) {
		return fmt.Errorf("%w: inquiry participants must be canonical UUIDs", apperrors.ErrInvalidArgument)
	}
	if item.ID == "" {
		return fmt.Errorf("%w: inquiry id is required", apperrors.ErrInvalidArgument)
	}
	if !canonicalUUID(item.ID) {
		return fmt.Errorf("%w: inquiry id must be a canonical UUID", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(item.RequestID) == "" || strings.TrimSpace(item.QuestionKey) == "" {
		return fmt.Errorf("%w: inquiry request and question key are required", apperrors.ErrInvalidArgument)
	}
	if item.DueAt.IsZero() {
		return fmt.Errorf("%w: inquiry due time is required", apperrors.ErrInvalidArgument)
	}
	if !validInquiryStatus(item.Status) {
		return fmt.Errorf("%w: invalid inquiry status", apperrors.ErrInvalidArgument)
	}
	if now.After(item.DueAt) && (item.Status == domain.InquiryPending || item.Status == domain.InquiryProcessing) {
		return fmt.Errorf("%w: pending inquiry must not already be due", apperrors.ErrInvalidArgument)
	}
	if limits.PairPerHour < 0 || limits.CallerPerDay < 0 || limits.OwnerPending < 0 || limits.MergeWindow < 0 {
		return fmt.Errorf("%w: inquiry limits must not be negative", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validInquiryStatus(status string) bool {
	switch status {
	case domain.InquiryAnswered, domain.InquiryProcessing, domain.InquiryPending, domain.InquiryReplied, domain.InquiryExpired, domain.InquiryInvalidated:
		return true
	default:
		return false
	}
}

func inquiryReceipt(existing, item domain.Inquiry) (domain.Inquiry, error) {
	if existing.OwnerID != item.OwnerID || existing.QuestionKey != item.QuestionKey {
		return domain.Inquiry{}, fmt.Errorf("%w: inquiry request is bound to a different recipient or question", apperrors.ErrConflict)
	}
	return existing, nil
}

func lockInquiryParticipants(ctx context.Context, db *gorm.DB, callerID, ownerID string) error {
	ids := []string{callerID}
	if ownerID != callerID {
		ids = append(ids, ownerID)
	}
	sort.Strings(ids)
	rows, err := db.Raw(`SELECT id, status FROM users WHERE id IN (?, ?) ORDER BY id FOR UPDATE`, ids[0], ids[len(ids)-1]).Rows()
	if err != nil {
		return fmt.Errorf("lock inquiry participants: %w", err)
	}
	defer func() { _ = rows.Close() }()
	found := 0
	active := true
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return fmt.Errorf("scan inquiry participant: %w", err)
		}
		found++
		active = active && status == "active"
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("lock inquiry participants: %w", err)
	}
	if found != len(ids) || !active {
		return fmt.Errorf("%w: inquiry participant is not active", apperrors.ErrConflict)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var approved int
	if err := db.WithContext(ctx).Raw(`
		SELECT count(*) FROM (
			SELECT user_id FROM ai_personal_agent_access
			WHERE user_id IN (?, ?) AND status = 'approved' ORDER BY user_id FOR UPDATE
		) AS authorized
	`, ids[0], ids[len(ids)-1]).Row().Scan(&approved); err != nil {
		return fmt.Errorf("lock Heya access for assistant mesh: %w", err)
	}
	if approved != len(ids) {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func lockInquiryOwner(ctx context.Context, db *gorm.DB, ownerID string) (bool, error) {
	var open bool
	err := db.Raw(`SELECT open FROM assistant_mesh_profiles WHERE owner_id = ? FOR UPDATE`, ownerID).Row().Scan(&open)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock inquiry recipient profile: %w", err)
	}
	return open, nil
}

func mergeableInquiry(ctx context.Context, db *gorm.DB, item domain.Inquiry, now time.Time, window time.Duration) (domain.Inquiry, bool, error) {
	row := db.Raw(`SELECT `+inquiryColumns+` FROM assistant_mesh_inquiries
		WHERE caller_id = ? AND owner_id = ? AND question_key = ? AND status IN (?, ?) AND due_at > ? AND created_at >= ?
		ORDER BY created_at DESC, id DESC LIMIT 1`,
		item.CallerID, item.OwnerID, item.QuestionKey, domain.InquiryPending, domain.InquiryProcessing, now, now.Add(-window)).Row()
	merged, err := scanInquiry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Inquiry{}, false, nil
	}
	if err != nil {
		return domain.Inquiry{}, false, err
	}
	return merged, true, nil
}

func inquiryAdmissionCounts(ctx context.Context, db *gorm.DB, callerID, ownerID string, now time.Time) (inquiryCounts, error) {
	var counts inquiryCounts
	err := db.Raw(`
		SELECT
			(SELECT count(*) FROM assistant_mesh_inquiries WHERE caller_id = ? AND owner_id = ? AND created_at >= ?),
			(SELECT count(*) FROM assistant_mesh_inquiries WHERE caller_id = ? AND created_at >= ?),
			(SELECT count(*) FROM assistant_mesh_inquiries WHERE owner_id = ? AND status = ? AND due_at > ?)
	`, callerID, ownerID, now.Add(-time.Hour), callerID, now.Add(-24*time.Hour), ownerID, domain.InquiryPending, now).
		Row().Scan(&counts.pairHour, &counts.callerDay, &counts.ownerOpen)
	if err != nil {
		return inquiryCounts{}, fmt.Errorf("count inquiry admission: %w", err)
	}
	return counts, nil
}

func insertInquiry(ctx context.Context, db *gorm.DB, item domain.Inquiry, citationsJSON []byte) (domain.Inquiry, error) {
	stored, err := scanInquiry(db.Raw(`
		INSERT INTO assistant_mesh_inquiries
			(id, caller_id, owner_id, request_id, question_key, question, status, answer, citations, created_at, due_at, closed_at, purge_after, escalated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?)
		ON CONFLICT (caller_id, request_id) DO NOTHING
		RETURNING `+inquiryColumns,
		item.ID, item.CallerID, item.OwnerID, item.RequestID, item.QuestionKey, item.Question, item.Status, item.Answer,
		string(citationsJSON), item.CreatedAt, item.DueAt, item.ClosedAt, item.PurgeAfter, item.Status == domain.InquiryPending,
	).Row())
	if !errors.Is(err, sql.ErrNoRows) {
		return stored, err
	}
	existing, found, err := inquiryByRequest(ctx, db, item.CallerID, item.RequestID)
	if err != nil {
		return domain.Inquiry{}, err
	}
	if !found {
		return domain.Inquiry{}, fmt.Errorf("%w: inquiry request was not stored", apperrors.ErrConflict)
	}
	return inquiryReceipt(existing, item)
}

func aliasInquiry(ctx context.Context, db *gorm.DB, callerID, requestID, inquiryID string) error {
	if err := db.Exec(`
		INSERT INTO assistant_mesh_inquiry_aliases (caller_id, request_id, inquiry_id)
		VALUES (?, ?, ?)
		ON CONFLICT (caller_id, request_id) DO NOTHING
	`, callerID, requestID, inquiryID).Error; err != nil {
		return fmt.Errorf("alias assistant mesh inquiry: %w", err)
	}
	return nil
}

func inquiryByRequest(ctx context.Context, db *gorm.DB, callerID, requestID string) (domain.Inquiry, bool, error) {
	item, err := scanInquiry(db.Raw(`
		SELECT `+inquiryColumns+` FROM assistant_mesh_inquiries WHERE caller_id = ? AND request_id = ?
		UNION ALL
		SELECT `+inquiryColumns+` FROM assistant_mesh_inquiries
		WHERE id = (SELECT inquiry_id FROM assistant_mesh_inquiry_aliases WHERE caller_id = ? AND request_id = ?)
		LIMIT 1`,
		callerID, requestID, callerID, requestID,
	).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Inquiry{}, false, nil
	}
	if err != nil {
		return domain.Inquiry{}, false, fmt.Errorf("get assistant mesh inquiry: %w", err)
	}
	return item, true, nil
}

func refreshInquiries(ctx context.Context, db *gorm.DB, userID string, now time.Time, retention time.Duration) error {
	if err := db.Exec(`
		UPDATE assistant_mesh_inquiries
		SET status = ?, closed_at = due_at, purge_after = COALESCE(purge_after, due_at + make_interval(secs => assistant_mesh_effective_inquiry_retention_seconds(?)))
		WHERE status IN (?, ?) AND due_at <= ? AND (caller_id = ? OR owner_id = ?)
	`, domain.InquiryExpired, int(retention.Seconds()), domain.InquiryPending, domain.InquiryProcessing, now, userID, userID).Error; err != nil {
		return fmt.Errorf("expire assistant mesh inquiries: %w", err)
	}
	if err := db.Exec(`
		UPDATE assistant_mesh_inquiries AS i
		SET status = ?, closed_at = ?, purge_after = COALESCE(i.purge_after, ?)
		WHERE i.status IN (?, ?) AND (i.caller_id = ? OR i.owner_id = ?)
		  AND (
		    NOT EXISTS (SELECT 1 FROM assistant_mesh_profiles p WHERE p.owner_id = i.owner_id AND p.open)
		    OR NOT EXISTS (SELECT 1 FROM users u WHERE u.id = i.owner_id AND u.status = 'active')
		    OR NOT EXISTS (SELECT 1 FROM users u WHERE u.id = i.caller_id AND u.status = 'active')
		  )
	`, domain.InquiryInvalidated, now, now.Add(retention), domain.InquiryPending, domain.InquiryProcessing, userID, userID).Error; err != nil {
		return fmt.Errorf("invalidate assistant mesh inquiries: %w", err)
	}
	if err := db.Exec(`
		UPDATE assistant_mesh_inquiries
		SET question = '', answer = '',
			    citation_versions = CASE WHEN citations <> '[]'::jsonb
			        THEN assistant_mesh_retained_citation_versions(citations) ELSE citation_versions END,
			    citations = '[]'::jsonb
		WHERE (caller_id = ? OR owner_id = ?) AND purge_after IS NOT NULL AND purge_after <= ?
		  AND (question <> '' OR answer <> '' OR citations <> '[]'::jsonb)
	`, userID, userID, now).Error; err != nil {
		return fmt.Errorf("purge assistant mesh inquiry content: %w", err)
	}
	return nil
}

type inquiryScanner interface {
	Scan(dest ...any) error
}

func scanInquiry(scanner inquiryScanner) (domain.Inquiry, error) {
	var item domain.Inquiry
	var citations []byte
	var closedAt, purgeAfter sql.NullTime
	if err := scanner.Scan(&item.ID, &item.CallerID, &item.OwnerID, &item.RequestID, &item.QuestionKey,
		&item.Question, &item.Status, &item.Answer, &citations, &item.CreatedAt, &item.DueAt,
		&closedAt, &purgeAfter); err != nil {
		return domain.Inquiry{}, err
	}
	if err := json.Unmarshal(citations, &item.Citations); err != nil {
		return domain.Inquiry{}, fmt.Errorf("decode inquiry citations: %w", err)
	}
	if closedAt.Valid {
		item.ClosedAt = &closedAt.Time
	}
	if purgeAfter.Valid {
		item.PurgeAfter = &purgeAfter.Time
	}
	return item, nil
}
