package knowledgegraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	appknowledgegraph "github.com/opensoha/soha/internal/application/knowledgegraph"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Put(ctx context.Context, revision appknowledgegraph.Revision) error {
	payload, err := json.Marshal(revision)
	if err != nil {
		return fmt.Errorf("encode graph revision: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_knowledge_graph_revisions(id,knowledge_base_id,source_index_ref,status,payload,created_at,published_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,payload=EXCLUDED.payload,published_at=EXCLUDED.published_at`, revision.ID, revision.KnowledgeBaseID, revision.SourceIndexRef, revision.Status, payload, revision.CreatedAt, revision.PublishedAt).Error; err != nil {
		return fmt.Errorf("put graph revision: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (appknowledgegraph.Revision, error) {
	var payload []byte
	if err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_knowledge_graph_revisions WHERE id=?`, id).Row().Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appknowledgegraph.Revision{}, appknowledgegraph.ErrNotFound
		}
		return appknowledgegraph.Revision{}, fmt.Errorf("get graph revision: %w", err)
	}
	var item appknowledgegraph.Revision
	if err := json.Unmarshal(payload, &item); err != nil {
		return appknowledgegraph.Revision{}, fmt.Errorf("decode graph revision: %w", err)
	}
	return item, nil
}

func (r *Repository) List(ctx context.Context, knowledgeBaseID string) ([]appknowledgegraph.Revision, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_knowledge_graph_revisions WHERE knowledge_base_id=? ORDER BY created_at DESC`, knowledgeBaseID).Rows()
	if err != nil {
		return nil, fmt.Errorf("list graph revisions: %w", err)
	}
	defer rows.Close()
	items := make([]appknowledgegraph.Revision, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan graph revision: %w", err)
		}
		var item appknowledgegraph.Revision
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, fmt.Errorf("decode graph revision: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Publish(ctx context.Context, id string, now time.Time) (appknowledgegraph.Revision, error) {
	var published appknowledgegraph.Revision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var payload []byte
		if err := tx.Raw(`SELECT payload FROM ai_knowledge_graph_revisions WHERE id=? FOR UPDATE`, id).Row().Scan(&payload); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return appknowledgegraph.ErrNotFound
			}
			return err
		}
		if err := json.Unmarshal(payload, &published); err != nil {
			return err
		}
		if published.Status != "verified" {
			return fmt.Errorf("graph revision is not verified")
		}
		if err := tx.Exec(`UPDATE ai_knowledge_graph_revisions SET status='superseded',payload=jsonb_set(payload,'{status}','"superseded"'::jsonb) WHERE knowledge_base_id=? AND status='active'`, published.KnowledgeBaseID).Error; err != nil {
			return err
		}
		published.Status = "active"
		published.PublishedAt = &now
		encoded, err := json.Marshal(published)
		if err != nil {
			return err
		}
		return tx.Exec(`UPDATE ai_knowledge_graph_revisions SET status='active',published_at=?,payload=? WHERE id=? AND status='verified'`, now, encoded, id).Error
	})
	if err != nil {
		return appknowledgegraph.Revision{}, fmt.Errorf("publish graph revision: %w", err)
	}
	return published, nil
}
