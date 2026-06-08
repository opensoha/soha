package eventstream

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	domainevent "github.com/opensoha/soha/internal/domain/event"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, event domainevent.Envelope) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO event_stream (
			id, source, category, severity, cluster_id, namespace, resource_ref, summary, payload, correlation_id, occurred_at, created_at
		) VALUES (
			?, ?, ?, ?, ?, ?, '{}', ?, ?, ?, ?, ?
		)
	`, event.ID, event.Source, event.Category, event.Severity, event.ClusterID, event.Namespace, event.Summary, string(payload), event.ID, time.Now().UTC(), time.Now().UTC()).Error
}

func (r *Repository) List(ctx context.Context, limit int) ([]domainevent.Envelope, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, source, category, severity, cluster_id, namespace, summary, payload
		FROM event_stream
		ORDER BY occurred_at DESC, created_at DESC
		LIMIT ?
	`, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query event stream: %w", err)
	}
	defer rows.Close()

	items := make([]domainevent.Envelope, 0, limit)
	for rows.Next() {
		var item domainevent.Envelope
		var payload []byte
		if err := rows.Scan(&item.ID, &item.Source, &item.Category, &item.Severity, &item.ClusterID, &item.Namespace, &item.Summary, &payload); err != nil {
			return nil, fmt.Errorf("scan event envelope: %w", err)
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &item.Payload)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, eventID string) (domainevent.Envelope, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, source, category, severity, cluster_id, namespace, summary, payload
		FROM event_stream
		WHERE id = ?
		LIMIT 1
	`, eventID).Row()

	var item domainevent.Envelope
	var payload []byte
	if err := row.Scan(&item.ID, &item.Source, &item.Category, &item.Severity, &item.ClusterID, &item.Namespace, &item.Summary, &payload); err != nil {
		return domainevent.Envelope{}, err
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &item.Payload)
	}
	return item, nil
}
