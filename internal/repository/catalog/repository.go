package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("catalog record not found")

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListBusinessLines(ctx context.Context) ([]domaincatalog.BusinessLine, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, business_key, name, description, owners, sort_order, enabled, created_at, updated_at
		FROM business_lines
		ORDER BY sort_order ASC, name ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query business lines: %w", err)
	}
	defer rows.Close()

	items := make([]domaincatalog.BusinessLine, 0)
	for rows.Next() {
		item, err := scanBusinessLine(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetBusinessLine(ctx context.Context, id string) (domaincatalog.BusinessLine, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, business_key, name, description, owners, sort_order, enabled, created_at, updated_at
		FROM business_lines
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanBusinessLineRow(row)
}

func (r *Repository) CreateBusinessLine(ctx context.Context, input domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error) {
	item := normalizeBusinessLineInput(input)
	owners, err := json.Marshal(item.Owners)
	if err != nil {
		return domaincatalog.BusinessLine{}, fmt.Errorf("marshal business line owners: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO business_lines (id, business_key, name, description, owners, sort_order, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Key, item.Name, nullableString(item.Description), string(owners), item.SortOrder, item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaincatalog.BusinessLine{}, fmt.Errorf("create business line: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateBusinessLine(ctx context.Context, id string, input domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error) {
	item := normalizeBusinessLineInput(input)
	item.ID = strings.TrimSpace(id)
	owners, err := json.Marshal(item.Owners)
	if err != nil {
		return domaincatalog.BusinessLine{}, fmt.Errorf("marshal business line owners: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE business_lines
		SET business_key = ?, name = ?, description = ?, owners = ?, sort_order = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Key, item.Name, nullableString(item.Description), string(owners), item.SortOrder, item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaincatalog.BusinessLine{}, fmt.Errorf("update business line: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincatalog.BusinessLine{}, ErrNotFound
	}
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "business_lines", item.ID)
	return item, nil
}

func (r *Repository) DeleteBusinessLine(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM business_lines WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete business line: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListEnvironments(ctx context.Context) ([]domaincatalog.Environment, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, environment_key, name, tier, stage_level, sort_order, is_production, requires_approval, enabled, created_at, updated_at
		FROM delivery_environments
		ORDER BY sort_order ASC, name ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query delivery environments: %w", err)
	}
	defer rows.Close()

	items := make([]domaincatalog.Environment, 0)
	for rows.Next() {
		item, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetEnvironment(ctx context.Context, id string) (domaincatalog.Environment, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, environment_key, name, tier, stage_level, sort_order, is_production, requires_approval, enabled, created_at, updated_at
		FROM delivery_environments
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanEnvironmentRow(row)
}

func (r *Repository) CreateEnvironment(ctx context.Context, input domaincatalog.EnvironmentInput) (domaincatalog.Environment, error) {
	item := normalizeEnvironmentInput(input)
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO delivery_environments (id, environment_key, name, tier, stage_level, sort_order, is_production, requires_approval, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Key, item.Name, nullableString(item.Tier), item.StageLevel, item.SortOrder, item.IsProduction, item.RequiresApproval, item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaincatalog.Environment{}, fmt.Errorf("create delivery environment: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateEnvironment(ctx context.Context, id string, input domaincatalog.EnvironmentInput) (domaincatalog.Environment, error) {
	item := normalizeEnvironmentInput(input)
	item.ID = strings.TrimSpace(id)
	result := r.db.WithContext(ctx).Exec(`
		UPDATE delivery_environments
		SET environment_key = ?, name = ?, tier = ?, stage_level = ?, sort_order = ?, is_production = ?, requires_approval = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Key, item.Name, nullableString(item.Tier), item.StageLevel, item.SortOrder, item.IsProduction, item.RequiresApproval, item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaincatalog.Environment{}, fmt.Errorf("update delivery environment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincatalog.Environment{}, ErrNotFound
	}
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "delivery_environments", item.ID)
	return item, nil
}

func (r *Repository) DeleteEnvironment(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM delivery_environments WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete delivery environment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListApplicationEnvironments(ctx context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT ae.id, ae.application_id, a.business_line_id, ae.environment_id, e.environment_key, ae.workflow_template_id, ae.build_policy, ae.release_policy, ae.created_at, ae.updated_at
		FROM application_environments ae
		JOIN applications a ON a.id = ae.application_id
		JOIN delivery_environments e ON e.id = ae.environment_id
		ORDER BY ae.created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query application environments: %w", err)
	}
	defer rows.Close()

	items := make([]domaincatalog.ApplicationEnvironment, 0)
	for rows.Next() {
		item, err := scanApplicationEnvironment(rows)
		if err != nil {
			return nil, err
		}
		targets, err := r.listReleaseTargets(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.Targets = targets
		if item.WorkflowTemplateID != "" {
			template, templateErr := r.GetWorkflowTemplate(ctx, item.WorkflowTemplateID)
			if templateErr == nil {
				item.WorkflowTemplate = &template
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetApplicationEnvironment(ctx context.Context, id string) (domaincatalog.ApplicationEnvironment, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT ae.id, ae.application_id, a.business_line_id, ae.environment_id, e.environment_key, ae.workflow_template_id, ae.build_policy, ae.release_policy, ae.created_at, ae.updated_at
		FROM application_environments ae
		JOIN applications a ON a.id = ae.application_id
		JOIN delivery_environments e ON e.id = ae.environment_id
		WHERE ae.id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	item, err := scanApplicationEnvironmentRow(row)
	if err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	item.Targets, err = r.listReleaseTargets(ctx, item.ID)
	if err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	if item.WorkflowTemplateID != "" {
		template, templateErr := r.GetWorkflowTemplate(ctx, item.WorkflowTemplateID)
		if templateErr == nil {
			item.WorkflowTemplate = &template
		}
	}
	return item, nil
}

func (r *Repository) CreateApplicationEnvironment(ctx context.Context, input domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	item := normalizeApplicationEnvironmentInput(input)
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		buildPolicy, err := json.Marshal(item.BuildPolicy)
		if err != nil {
			return fmt.Errorf("marshal build policy: %w", err)
		}
		releasePolicy, err := json.Marshal(item.ReleasePolicy)
		if err != nil {
			return fmt.Errorf("marshal release policy: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO application_environments (id, application_id, environment_id, workflow_template_id, build_policy, release_policy, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.ApplicationID, item.EnvironmentID, nullableString(item.WorkflowTemplateID), string(buildPolicy), string(releasePolicy), item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return fmt.Errorf("create application environment: %w", err)
		}
		return replaceReleaseTargetsTx(tx, item.ID, input.Targets, item.CreatedAt)
	}); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	return r.GetApplicationEnvironment(ctx, item.ID)
}

func (r *Repository) UpdateApplicationEnvironment(ctx context.Context, id string, input domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	item := normalizeApplicationEnvironmentInput(input)
	item.ID = strings.TrimSpace(id)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		buildPolicy, err := json.Marshal(item.BuildPolicy)
		if err != nil {
			return fmt.Errorf("marshal build policy: %w", err)
		}
		releasePolicy, err := json.Marshal(item.ReleasePolicy)
		if err != nil {
			return fmt.Errorf("marshal release policy: %w", err)
		}
		result := tx.Exec(`
			UPDATE application_environments
			SET application_id = ?, environment_id = ?, workflow_template_id = ?, build_policy = ?, release_policy = ?, updated_at = ?
			WHERE id = ?
		`, item.ApplicationID, item.EnvironmentID, nullableString(item.WorkflowTemplateID), string(buildPolicy), string(releasePolicy), item.UpdatedAt, item.ID)
		if result.Error != nil {
			return fmt.Errorf("update application environment: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return replaceReleaseTargetsTx(tx, item.ID, input.Targets, item.UpdatedAt)
	})
	if err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	return r.GetApplicationEnvironment(ctx, item.ID)
}

func (r *Repository) DeleteApplicationEnvironment(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM application_environments WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete application environment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListWorkflowTemplates(ctx context.Context) ([]domaincatalog.WorkflowTemplate, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, template_key, name, description, category, definition, enabled, created_at, updated_at
		FROM workflow_templates
		ORDER BY name ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query workflow templates: %w", err)
	}
	defer rows.Close()

	items := make([]domaincatalog.WorkflowTemplate, 0)
	for rows.Next() {
		item, err := scanWorkflowTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetWorkflowTemplate(ctx context.Context, id string) (domaincatalog.WorkflowTemplate, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, template_key, name, description, category, definition, enabled, created_at, updated_at
		FROM workflow_templates
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanWorkflowTemplateRow(row)
}

func (r *Repository) CreateWorkflowTemplate(ctx context.Context, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	item := normalizeWorkflowTemplateInput(input)
	definition, err := json.Marshal(item.Definition)
	if err != nil {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("marshal workflow template definition: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO workflow_templates (id, template_key, name, description, category, definition, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Key, item.Name, nullableString(item.Description), nullableString(item.Category), string(definition), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("create workflow template: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateWorkflowTemplate(ctx context.Context, id string, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	item := normalizeWorkflowTemplateInput(input)
	item.ID = strings.TrimSpace(id)
	definition, err := json.Marshal(item.Definition)
	if err != nil {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("marshal workflow template definition: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE workflow_templates
		SET template_key = ?, name = ?, description = ?, category = ?, definition = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Key, item.Name, nullableString(item.Description), nullableString(item.Category), string(definition), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("update workflow template: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincatalog.WorkflowTemplate{}, ErrNotFound
	}
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "workflow_templates", item.ID)
	return item, nil
}

func (r *Repository) DeleteWorkflowTemplate(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM workflow_templates WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete workflow template: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) listReleaseTargets(ctx context.Context, applicationEnvironmentID string) ([]domaincatalog.ReleaseTarget, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, application_environment_id, cluster_id, namespace, workload_kind, workload_name, container_name, enabled, created_at, updated_at
		FROM release_targets
		WHERE application_environment_id = ?
		ORDER BY created_at ASC
	`, applicationEnvironmentID).Rows()
	if err != nil {
		return nil, fmt.Errorf("query release targets: %w", err)
	}
	defer rows.Close()

	items := make([]domaincatalog.ReleaseTarget, 0)
	for rows.Next() {
		item, err := scanReleaseTarget(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func replaceReleaseTargetsTx(tx *gorm.DB, applicationEnvironmentID string, inputs []domaincatalog.ReleaseTargetInput, now time.Time) error {
	if err := tx.Exec(`DELETE FROM release_targets WHERE application_environment_id = ?`, applicationEnvironmentID).Error; err != nil {
		return fmt.Errorf("delete release targets: %w", err)
	}
	for _, input := range inputs {
		item := normalizeReleaseTargetInput(applicationEnvironmentID, input, now)
		if err := tx.Exec(`
			INSERT INTO release_targets (id, application_environment_id, cluster_id, namespace, workload_kind, workload_name, container_name, enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.ApplicationEnvironmentID, item.ClusterID, item.Namespace, item.WorkloadKind, item.WorkloadName, nullableString(item.ContainerName), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return fmt.Errorf("create release target: %w", err)
		}
	}
	return nil
}

func scanBusinessLine(rows *sql.Rows) (domaincatalog.BusinessLine, error) {
	var item domaincatalog.BusinessLine
	var description sql.NullString
	var owners []byte
	if err := rows.Scan(&item.ID, &item.Key, &item.Name, &description, &owners, &item.SortOrder, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincatalog.BusinessLine{}, fmt.Errorf("scan business line: %w", err)
	}
	item.Description = description.String
	_ = json.Unmarshal(owners, &item.Owners)
	return item, nil
}

func scanBusinessLineRow(row *sql.Row) (domaincatalog.BusinessLine, error) {
	var item domaincatalog.BusinessLine
	var description sql.NullString
	var owners []byte
	if err := row.Scan(&item.ID, &item.Key, &item.Name, &description, &owners, &item.SortOrder, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincatalog.BusinessLine{}, ErrNotFound
		}
		return domaincatalog.BusinessLine{}, fmt.Errorf("scan business line row: %w", err)
	}
	item.Description = description.String
	_ = json.Unmarshal(owners, &item.Owners)
	return item, nil
}

func scanEnvironment(rows *sql.Rows) (domaincatalog.Environment, error) {
	var item domaincatalog.Environment
	var tier sql.NullString
	if err := rows.Scan(&item.ID, &item.Key, &item.Name, &tier, &item.StageLevel, &item.SortOrder, &item.IsProduction, &item.RequiresApproval, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincatalog.Environment{}, fmt.Errorf("scan environment: %w", err)
	}
	item.Tier = tier.String
	return item, nil
}

func scanEnvironmentRow(row *sql.Row) (domaincatalog.Environment, error) {
	var item domaincatalog.Environment
	var tier sql.NullString
	if err := row.Scan(&item.ID, &item.Key, &item.Name, &tier, &item.StageLevel, &item.SortOrder, &item.IsProduction, &item.RequiresApproval, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincatalog.Environment{}, ErrNotFound
		}
		return domaincatalog.Environment{}, fmt.Errorf("scan environment row: %w", err)
	}
	item.Tier = tier.String
	return item, nil
}

func scanApplicationEnvironment(rows *sql.Rows) (domaincatalog.ApplicationEnvironment, error) {
	var item domaincatalog.ApplicationEnvironment
	var businessLineID sql.NullString
	var environmentKey sql.NullString
	var workflowTemplateID sql.NullString
	var buildPolicy []byte
	var releasePolicy []byte
	if err := rows.Scan(&item.ID, &item.ApplicationID, &businessLineID, &item.EnvironmentID, &environmentKey, &workflowTemplateID, &buildPolicy, &releasePolicy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("scan application environment: %w", err)
	}
	item.BusinessLineID = businessLineID.String
	item.EnvironmentKey = environmentKey.String
	item.WorkflowTemplateID = workflowTemplateID.String
	_ = json.Unmarshal(buildPolicy, &item.BuildPolicy)
	_ = json.Unmarshal(releasePolicy, &item.ReleasePolicy)
	if item.BuildPolicy == nil {
		item.BuildPolicy = map[string]any{}
	}
	if item.ReleasePolicy == nil {
		item.ReleasePolicy = map[string]any{}
	}
	return item, nil
}

func scanApplicationEnvironmentRow(row *sql.Row) (domaincatalog.ApplicationEnvironment, error) {
	var item domaincatalog.ApplicationEnvironment
	var businessLineID sql.NullString
	var environmentKey sql.NullString
	var workflowTemplateID sql.NullString
	var buildPolicy []byte
	var releasePolicy []byte
	if err := row.Scan(&item.ID, &item.ApplicationID, &businessLineID, &item.EnvironmentID, &environmentKey, &workflowTemplateID, &buildPolicy, &releasePolicy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincatalog.ApplicationEnvironment{}, ErrNotFound
		}
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("scan application environment row: %w", err)
	}
	item.BusinessLineID = businessLineID.String
	item.EnvironmentKey = environmentKey.String
	item.WorkflowTemplateID = workflowTemplateID.String
	_ = json.Unmarshal(buildPolicy, &item.BuildPolicy)
	_ = json.Unmarshal(releasePolicy, &item.ReleasePolicy)
	if item.BuildPolicy == nil {
		item.BuildPolicy = map[string]any{}
	}
	if item.ReleasePolicy == nil {
		item.ReleasePolicy = map[string]any{}
	}
	return item, nil
}

func scanReleaseTarget(rows *sql.Rows) (domaincatalog.ReleaseTarget, error) {
	var item domaincatalog.ReleaseTarget
	var containerName sql.NullString
	if err := rows.Scan(&item.ID, &item.ApplicationEnvironmentID, &item.ClusterID, &item.Namespace, &item.WorkloadKind, &item.WorkloadName, &containerName, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincatalog.ReleaseTarget{}, fmt.Errorf("scan release target: %w", err)
	}
	item.ContainerName = containerName.String
	return item, nil
}

func scanWorkflowTemplate(rows *sql.Rows) (domaincatalog.WorkflowTemplate, error) {
	var item domaincatalog.WorkflowTemplate
	var description sql.NullString
	var category sql.NullString
	var definition []byte
	if err := rows.Scan(&item.ID, &item.Key, &item.Name, &description, &category, &definition, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("scan workflow template: %w", err)
	}
	item.Description = description.String
	item.Category = category.String
	_ = json.Unmarshal(definition, &item.Definition)
	if item.Definition == nil {
		item.Definition = map[string]any{}
	}
	return item, nil
}

func scanWorkflowTemplateRow(row *sql.Row) (domaincatalog.WorkflowTemplate, error) {
	var item domaincatalog.WorkflowTemplate
	var description sql.NullString
	var category sql.NullString
	var definition []byte
	if err := row.Scan(&item.ID, &item.Key, &item.Name, &description, &category, &definition, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincatalog.WorkflowTemplate{}, ErrNotFound
		}
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("scan workflow template row: %w", err)
	}
	item.Description = description.String
	item.Category = category.String
	_ = json.Unmarshal(definition, &item.Definition)
	if item.Definition == nil {
		item.Definition = map[string]any{}
	}
	return item, nil
}

func normalizeBusinessLineInput(input domaincatalog.BusinessLineInput) domaincatalog.BusinessLine {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	return domaincatalog.BusinessLine{
		ID:          id,
		Key:         strings.TrimSpace(input.Key),
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Owners:      input.Owners,
		SortOrder:   input.SortOrder,
		Enabled:     input.Enabled,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func normalizeEnvironmentInput(input domaincatalog.EnvironmentInput) domaincatalog.Environment {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	return domaincatalog.Environment{
		ID:               id,
		Key:              strings.TrimSpace(input.Key),
		Name:             strings.TrimSpace(input.Name),
		Tier:             strings.TrimSpace(input.Tier),
		StageLevel:       input.StageLevel,
		SortOrder:        input.SortOrder,
		IsProduction:     input.IsProduction,
		RequiresApproval: input.RequiresApproval,
		Enabled:          input.Enabled,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func normalizeApplicationEnvironmentInput(input domaincatalog.ApplicationEnvironmentInput) domaincatalog.ApplicationEnvironment {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	if input.BuildPolicy == nil {
		input.BuildPolicy = map[string]any{}
	}
	if input.ReleasePolicy == nil {
		input.ReleasePolicy = map[string]any{}
	}
	return domaincatalog.ApplicationEnvironment{
		ID:                 id,
		ApplicationID:      strings.TrimSpace(input.ApplicationID),
		EnvironmentID:      strings.TrimSpace(input.EnvironmentID),
		WorkflowTemplateID: strings.TrimSpace(input.WorkflowTemplateID),
		BuildPolicy:        input.BuildPolicy,
		ReleasePolicy:      input.ReleasePolicy,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func normalizeWorkflowTemplateInput(input domaincatalog.WorkflowTemplateInput) domaincatalog.WorkflowTemplate {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	if input.Definition == nil {
		input.Definition = map[string]any{}
	}
	return domaincatalog.WorkflowTemplate{
		ID:          id,
		Key:         strings.TrimSpace(input.Key),
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Category:    strings.TrimSpace(input.Category),
		Definition:  input.Definition,
		Enabled:     input.Enabled,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func normalizeReleaseTargetInput(applicationEnvironmentID string, input domaincatalog.ReleaseTargetInput, now time.Time) domaincatalog.ReleaseTarget {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	return domaincatalog.ReleaseTarget{
		ID:                       id,
		ApplicationEnvironmentID: applicationEnvironmentID,
		ClusterID:                strings.TrimSpace(input.ClusterID),
		Namespace:                strings.TrimSpace(input.Namespace),
		WorkloadKind:             strings.TrimSpace(input.WorkloadKind),
		WorkloadName:             strings.TrimSpace(input.WorkloadName),
		ContainerName:            strings.TrimSpace(input.ContainerName),
		Enabled:                  input.Enabled,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func fetchCreatedAt(ctx context.Context, db *gorm.DB, tableName, id string) time.Time {
	var createdAt time.Time
	query := fmt.Sprintf(`SELECT created_at FROM %s WHERE id = ?`, tableName)
	if err := db.WithContext(ctx).Raw(query, id).Row().Scan(&createdAt); err != nil {
		return time.Time{}
	}
	return createdAt
}
