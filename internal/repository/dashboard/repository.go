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
	SourceFormat        string
	SourceSchemaVersion int
	DataSourceID        string
	Tags                []byte
	Panels              []byte
	Variables           []byte
	DataSourceBindings  []byte
	ImportWarnings      []byte
	RawJSON             []byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (r *Repository) ListDashboards(ctx context.Context) ([]domainobservability.Dashboard, error) {
	rows := make([]dashboardRow, 0)
	if err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, source, source_format, source_schema_version, data_source_id, tags, panels,
		       variables, data_source_bindings, import_warnings, NULL AS raw_json, created_at, updated_at
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
		SELECT id, name, source, source_format, source_schema_version, data_source_id, tags, panels,
		       variables, data_source_bindings, import_warnings, raw_json, created_at, updated_at
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
	variables, err := json.Marshal(item.Variables)
	if err != nil {
		return domainobservability.Dashboard{}, fmt.Errorf("marshal dashboard variables: %w", err)
	}
	bindings, err := json.Marshal(item.DataSourceBindings)
	if err != nil {
		return domainobservability.Dashboard{}, fmt.Errorf("marshal dashboard data source bindings: %w", err)
	}
	warnings, err := json.Marshal(item.ImportWarnings)
	if err != nil {
		return domainobservability.Dashboard{}, fmt.Errorf("marshal dashboard import warnings: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO observability_dashboards
			(id, name, source, source_format, source_schema_version, data_source_id, tags, panels,
			 variables, data_source_bindings, import_warnings, raw_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, NULLIF(?, '')::jsonb, ?, ?)
	`, item.ID, item.Name, item.Source, item.SourceFormat, item.SourceSchemaVersion, item.DataSourceID,
		string(tags), string(panels), string(variables), string(bindings), string(warnings), item.RawJSON, item.CreatedAt, item.UpdatedAt).Error; err != nil {
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
		ID: row.ID, Name: row.Name, Source: row.Source, SourceFormat: row.SourceFormat, SourceSchemaVersion: row.SourceSchemaVersion,
		DataSourceID: row.DataSourceID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if err := json.Unmarshal(row.Tags, &item.Tags); err != nil {
		return domainobservability.Dashboard{}, fmt.Errorf("decode dashboard tags: %w", err)
	}
	if err := json.Unmarshal(row.Panels, &item.Panels); err != nil {
		return domainobservability.Dashboard{}, fmt.Errorf("decode dashboard panels: %w", err)
	}
	if len(row.Variables) > 0 {
		if err := json.Unmarshal(row.Variables, &item.Variables); err != nil {
			return domainobservability.Dashboard{}, fmt.Errorf("decode dashboard variables: %w", err)
		}
	}
	if len(row.DataSourceBindings) > 0 {
		if err := json.Unmarshal(row.DataSourceBindings, &item.DataSourceBindings); err != nil {
			return domainobservability.Dashboard{}, fmt.Errorf("decode dashboard data source bindings: %w", err)
		}
	}
	if len(row.ImportWarnings) > 0 {
		if err := json.Unmarshal(row.ImportWarnings, &item.ImportWarnings); err != nil {
			return domainobservability.Dashboard{}, fmt.Errorf("decode dashboard import warnings: %w", err)
		}
	}
	item.RawJSON = string(row.RawJSON)
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if item.Panels == nil {
		item.Panels = []domainobservability.DashboardPanel{}
	}
	if item.Variables == nil {
		item.Variables = []domainobservability.DashboardVariable{}
	}
	if item.DataSourceBindings == nil {
		item.DataSourceBindings = []domainobservability.DashboardDataSourceBinding{}
	}
	if item.ImportWarnings == nil {
		item.ImportWarnings = []domainobservability.DashboardImportWarning{}
	}
	return item, nil
}
