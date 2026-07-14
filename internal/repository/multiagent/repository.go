package multiagent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	appmultiagent "github.com/opensoha/soha/internal/application/multiagent"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Put(ctx context.Context, plan appmultiagent.Plan) error {
	payload, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode multi-agent plan: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_multi_agent_plans(id,status,payload,created_at,completed_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,payload=EXCLUDED.payload,completed_at=EXCLUDED.completed_at`, plan.ID, plan.Status, payload, plan.CreatedAt, plan.CompletedAt).Error; err != nil {
		return fmt.Errorf("put multi-agent plan: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id string) (appmultiagent.Plan, error) {
	var payload []byte
	if err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_multi_agent_plans WHERE id=?`, id).Row().Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmultiagent.Plan{}, appmultiagent.ErrNotFound
		}
		return appmultiagent.Plan{}, fmt.Errorf("get multi-agent plan: %w", err)
	}
	var item appmultiagent.Plan
	if err := json.Unmarshal(payload, &item); err != nil {
		return appmultiagent.Plan{}, fmt.Errorf("decode multi-agent plan: %w", err)
	}
	return item, nil
}

func (r *Repository) List(ctx context.Context) ([]appmultiagent.Plan, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_multi_agent_plans ORDER BY created_at DESC`).Rows()
	if err != nil {
		return nil, fmt.Errorf("list multi-agent plans: %w", err)
	}
	defer rows.Close()
	items := make([]appmultiagent.Plan, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan multi-agent plan: %w", err)
		}
		var item appmultiagent.Plan
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, fmt.Errorf("decode multi-agent plan: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
