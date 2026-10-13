package copilot

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"time"
)

type feedRow struct {
	Owner         string
	Instructions  string
	SearchTool    string
	AutoUpdate    bool
	State         string
	Error         string
	JobID         string
	LastUpdatedAt *time.Time
}

func (r feedRow) record() domain.FeedRecord {
	return domain.FeedRecord{Owner: r.Owner, Settings: domain.FeedSettings{Instructions: r.Instructions, SearchTool: r.SearchTool, AutoUpdate: r.AutoUpdate}, State: r.State, Error: r.Error, JobID: r.JobID, LastUpdatedAt: r.LastUpdatedAt}
}

func (r *Repository) ensureFeed(ctx context.Context, owner string) error {
	return r.db.WithContext(ctx).Exec(`INSERT INTO ai_personal_feed_settings(owner,instructions) VALUES (?,?) ON CONFLICT(owner) DO NOTHING`, owner, domain.DefaultFeedInstructions).Error
}

func (r *Repository) ReadFeed(ctx context.Context, owner, before string, limit int) (domain.FeedRecord, []domain.FeedItem, string, error) {
	if err := r.ensureFeed(ctx, owner); err != nil {
		return domain.FeedRecord{}, nil, "", err
	}
	var row feedRow
	if err := r.db.WithContext(ctx).Raw(`SELECT * FROM ai_personal_feed_settings WHERE owner=?`, owner).Scan(&row).Error; err != nil {
		return domain.FeedRecord{}, nil, "", err
	}
	query := r.db.WithContext(ctx).Table("ai_personal_feed_items").Where("owner=? AND NOT hidden", owner)
	if before != "" {
		var anchor struct {
			ID        string
			CreatedAt time.Time
		}
		if err := r.db.WithContext(ctx).Table("ai_personal_feed_items").Select("id,created_at").Where("owner=? AND id=?", owner, before).Scan(&anchor).Error; err != nil {
			return row.record(), nil, "", err
		}
		if anchor.ID == "" {
			return row.record(), nil, "", apperrors.ErrNotFound
		}
		query = query.Where("(created_at,id)<(?,?)", anchor.CreatedAt, anchor.ID)
	}
	rows, err := query.Select("id,title,body,reason,sources,liked,created_at").Order("created_at DESC,id DESC").Limit(limit + 1).Rows()
	if err != nil {
		return row.record(), nil, "", err
	}
	defer func() { _ = rows.Close() }()
	items := []domain.FeedItem{}
	for rows.Next() {
		var item domain.FeedItem
		var sources []byte
		if err := rows.Scan(&item.ID, &item.Title, &item.Body, &item.Reason, &sources, &item.Liked, &item.CreatedAt); err != nil {
			return row.record(), nil, "", err
		}
		if err := json.Unmarshal(sources, &item.Sources); err != nil {
			return row.record(), nil, "", err
		}
		items = append(items, item)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	return row.record(), items, next, rows.Err()
}

func (r *Repository) SaveFeedSettings(ctx context.Context, owner string, settings domain.FeedSettings, now time.Time) error {
	if err := r.ensureFeed(ctx, owner); err != nil {
		return err
	}
	// A new fence invalidates any generation already using the old preferences.
	return r.db.WithContext(ctx).Exec(`UPDATE ai_personal_feed_settings SET instructions=?,search_tool=?,auto_update=?,state='idle',error='',job_id=?,lease_until=NULL,next_update_at=? WHERE owner=?`, settings.Instructions, settings.SearchTool, settings.AutoUpdate, uuid.NewString(), now.Add(24*time.Hour), owner).Error
}

func (r *Repository) GetFeedItem(ctx context.Context, owner, id string) (domain.FeedItem, error) {
	var item domain.FeedItem
	var sources []byte
	err := r.db.WithContext(ctx).Raw(`SELECT id,title,body,reason,sources,liked,created_at FROM ai_personal_feed_items WHERE owner=? AND id=?`, owner, id).Row().Scan(&item.ID, &item.Title, &item.Body, &item.Reason, &sources, &item.Liked, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, copilotNotFound("feed item", id)
	}
	if err != nil {
		return item, err
	}
	return item, json.Unmarshal(sources, &item.Sources)
}

func (r *Repository) SetFeedFeedback(ctx context.Context, owner, id string, liked, hidden *bool) (domain.FeedItem, error) {
	updates := map[string]any{}
	if liked != nil {
		updates["liked"] = *liked
	}
	if hidden != nil {
		updates["hidden"] = *hidden
	}
	result := r.db.WithContext(ctx).Table("ai_personal_feed_items").Where("owner=? AND id=?", owner, id).Updates(updates)
	if result.Error != nil {
		return domain.FeedItem{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domain.FeedItem{}, apperrors.ErrNotFound
	}
	return r.GetFeedItem(ctx, owner, id)
}

func (r *Repository) QueueFeedRefresh(ctx context.Context, owner string, now time.Time) error {
	if err := r.ensureFeed(ctx, owner); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct {
			State         string
			LastAttemptAt *time.Time
		}
		if err := tx.Raw(`SELECT state,last_attempt_at FROM ai_personal_feed_settings WHERE owner=? FOR UPDATE`, owner).Scan(&row).Error; err != nil {
			return err
		}
		if row.State == "queued" || row.State == "running" {
			return nil
		}
		if row.LastAttemptAt != nil && now.Sub(*row.LastAttemptAt) < time.Minute {
			return fmt.Errorf("%w: please wait one minute before refreshing again", apperrors.ErrConflict)
		}
		return tx.Exec(`UPDATE ai_personal_feed_settings SET state='queued',error='',job_id=?,last_attempt_at=? WHERE owner=?`, uuid.NewString(), now, owner).Error
	})
}

func (r *Repository) ClaimFeedRefresh(ctx context.Context, now time.Time) (domain.FeedRecord, bool, error) {
	var result domain.FeedRecord
	claimed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE ai_personal_feed_settings SET state='failed',error='刷新中断，请重新更新',job_id='',lease_until=NULL,next_update_at=? WHERE state='running' AND lease_until<?`, now.Add(24*time.Hour), now).Error; err != nil {
			return err
		}
		var row feedRow
		if err := tx.Raw(`SELECT * FROM ai_personal_feed_settings WHERE state='queued' OR (auto_update AND state IN ('idle','failed') AND next_update_at<=?) ORDER BY last_attempt_at NULLS FIRST LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Scan(&row).Error; err != nil {
			return err
		}
		if row.Owner == "" {
			return nil
		}
		row.State = "running"
		row.JobID = uuid.NewString()
		if err := tx.Exec(`UPDATE ai_personal_feed_settings SET state='running',error='',job_id=?,lease_until=?,last_attempt_at=? WHERE owner=?`, row.JobID, now.Add(3*time.Minute), now, row.Owner).Error; err != nil {
			return err
		}
		result = row.record()
		claimed = true
		return nil
	})
	return result, claimed, err
}

func (r *Repository) FinishFeedRefresh(ctx context.Context, record domain.FeedRecord, items []domain.FeedItem, failure string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row feedRow
		if err := tx.Raw(`SELECT * FROM ai_personal_feed_settings WHERE owner=? FOR UPDATE`, record.Owner).Scan(&row).Error; err != nil {
			return err
		}
		if row.State != "running" || row.JobID != record.JobID {
			return nil
		}
		for _, item := range items {
			if len(item.Sources) == 0 {
				return apperrors.ErrInvalidArgument
			}
			sources, err := json.Marshal(item.Sources)
			if err != nil {
				return err
			}
			key := fmt.Sprintf("%x", sha256.Sum256([]byte(item.Sources[0].URL)))
			if err := tx.Exec(`INSERT INTO ai_personal_feed_items(id,owner,title,body,reason,sources,source_key,created_at) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(owner,source_key) DO NOTHING`, item.ID, record.Owner, item.Title, item.Body, item.Reason, string(sources), key, now).Error; err != nil {
				return err
			}
		}
		state := "idle"
		if failure != "" {
			state = "failed"
		}
		return tx.Exec(`UPDATE ai_personal_feed_settings SET state=?,error=?,job_id='',lease_until=NULL,next_update_at=?,last_updated_at=CASE WHEN ?='' THEN ? ELSE last_updated_at END WHERE owner=?`, state, failure, now.Add(24*time.Hour), failure, now, record.Owner).Error
	})
}
