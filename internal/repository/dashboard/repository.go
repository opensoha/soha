package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	domainobservability "github.com/opensoha/soha/internal/domain/observability"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

type dashboardRow struct {
	ID                  string
	Name                string
	Source              string
	SourceSchemaVersion int
	DataSourceID        string
	Tags                []byte
	Panels              []byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (r *Repository) ListDashboards(ctx context.Context) ([]domainobservability.Dashboard, error) {
	rows := make([]dashboardRow, 0)
	if err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, source, source_schema_version, data_source_id, tags, panels, created_at, updated_at
		FROM observability_dashboards
		ORDER BY updated_at DESC, id ASC
	`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]domainobservability.Dashboard, 0, len(rows))
	for _, row := range rows {
		item, err := decodeDashboard(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) GetDashboard(ctx context.Context, id string) (domainobservability.Dashboard, error) {
	var row dashboardRow
	result := r.db.WithContext(ctx).Raw(`
		SELECT id, name, source, source_schema_version, data_source_id, tags, panels, created_at, updated_at
		FROM observability_dashboards
		WHERE id = ?
	`, id).Scan(&row)
	if result.Error != nil {
		return domainobservability.Dashboard{}, result.Error
	}
	if row.ID == "" {
		return domainobservability.Dashboard{}, fmt.Errorf("%w: dashboard not found", apperrors.ErrNotFound)
	}
	return decodeDashboard(row)
}

func (r *Repository) CreateDashboard(ctx context.Context, item domainobservability.Dashboard) (domainobservability.Dashboard, error) {
	tags, err := json.Marshal(item.Tags)
	if err != nil {
		return domainobservability.Dashboard{}, fmt.Errorf("marshal dashboard tags: %w", err)
	}
	panels, err := json.Marshal(item.Panels)
	if err != nil {
		return domainobservability.Dashboard{}, fmt.Errorf("marshal dashboard panels: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO observability_dashboards
			(id, name, source, source_schema_version, data_source_id, tags, panels, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?)
	`, item.ID, item.Name, item.Source, item.SourceSchemaVersion, item.DataSourceID, string(tags), string(panels), item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainobservability.Dashboard{}, err
	}
	return item, nil
}

func (r *Repository) DeleteDashboard(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM observability_dashboards WHERE id = ?`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: dashboard not found", apperrors.ErrNotFound)
	}
	return nil
}

func decodeDashboard(row dashboardRow) (domainobservability.Dashboard, error) {
	item := domainobservability.Dashboard{
		ID: row.ID, Name: row.Name, Source: row.Source, SourceSchemaVersion: row.SourceSchemaVersion,
		DataSourceID: row.DataSourceID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if err := json.Unmarshal(row.Tags, &item.Tags); err != nil {
		return domainobservability.Dashboard{}, fmt.Errorf("decode dashboard tags: %w", err)
	}
	if err := json.Unmarshal(row.Panels, &item.Panels); err != nil {
		return domainobservability.Dashboard{}, fmt.Errorf("decode dashboard panels: %w", err)
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if item.Panels == nil {
		item.Panels = []domainobservability.DashboardPanel{}
	}
	return item, nil
}
