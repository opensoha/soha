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
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("catalog record not found")

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
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

func (r *Repository) ListApplicationEnvironments(ctx context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT ae.id, ae.application_id, a.business_line_id, a.app_group, ae.environment_id, COALESCE(e.environment_key, ae.environment_id), ae.strategy_profile_id, ae.promotion_policy_id, ae.approval_policy_id, ae.artifact_policy_id, ae.workflow_template_id, ae.build_policy, ae.release_policy, ae.resource_selector, ae.created_at, ae.updated_at
		FROM application_environments ae
		JOIN applications a ON a.id = ae.application_id
		LEFT JOIN delivery_environments e ON e.id = ae.environment_id
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
		SELECT ae.id, ae.application_id, a.business_line_id, a.app_group, ae.environment_id, COALESCE(e.environment_key, ae.environment_id), ae.strategy_profile_id, ae.promotion_policy_id, ae.approval_policy_id, ae.artifact_policy_id, ae.workflow_template_id, ae.build_policy, ae.release_policy, ae.resource_selector, ae.created_at, ae.updated_at
		FROM application_environments ae
		JOIN applications a ON a.id = ae.application_id
		LEFT JOIN delivery_environments e ON e.id = ae.environment_id
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
		resourceSelector, err := json.Marshal(item.ResourceSelector)
		if err != nil {
			return fmt.Errorf("marshal resource selector: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO application_environments (id, application_id, environment_id, strategy_profile_id, promotion_policy_id, approval_policy_id, artifact_policy_id, workflow_template_id, build_policy, release_policy, resource_selector, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.ApplicationID, item.EnvironmentID, nullableString(item.StrategyProfileID), nullableString(item.PromotionPolicyID), nullableString(item.ApprovalPolicyID), nullableString(item.ArtifactPolicyID), nullableString(item.WorkflowTemplateID), string(buildPolicy), string(releasePolicy), string(resourceSelector), item.CreatedAt, item.UpdatedAt).Error; err != nil {
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
		resourceSelector, err := json.Marshal(item.ResourceSelector)
		if err != nil {
			return fmt.Errorf("marshal resource selector: %w", err)
		}
		result := tx.Exec(`
			UPDATE application_environments
			SET application_id = ?, environment_id = ?, strategy_profile_id = ?, promotion_policy_id = ?, approval_policy_id = ?, artifact_policy_id = ?, workflow_template_id = ?, build_policy = ?, release_policy = ?, resource_selector = ?, updated_at = ?
			WHERE id = ?
		`, item.ApplicationID, item.EnvironmentID, nullableString(item.StrategyProfileID), nullableString(item.PromotionPolicyID), nullableString(item.ApprovalPolicyID), nullableString(item.ArtifactPolicyID), nullableString(item.WorkflowTemplateID), string(buildPolicy), string(releasePolicy), string(resourceSelector), item.UpdatedAt, item.ID)
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

func (r *Repository) ListBuildTemplates(ctx context.Context) ([]domaincatalog.BuildTemplate, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, template_key, name, description, builder_kind, dockerfile_template, build_commands, variable_schema, default_variables, enabled, created_at, updated_at
		FROM build_templates
		ORDER BY name ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query build templates: %w", err)
	}
	defer rows.Close()

	items := make([]domaincatalog.BuildTemplate, 0)
	for rows.Next() {
		item, scanErr := scanBuildTemplate(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetBuildTemplate(ctx context.Context, id string) (domaincatalog.BuildTemplate, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, template_key, name, description, builder_kind, dockerfile_template, build_commands, variable_schema, default_variables, enabled, created_at, updated_at
		FROM build_templates
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanBuildTemplateRow(row)
}

func (r *Repository) CreateBuildTemplate(ctx context.Context, input domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error) {
	item := normalizeBuildTemplateInput(input)
	buildCommands, err := json.Marshal(item.BuildCommands)
	if err != nil {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("marshal build template commands: %w", err)
	}
	variableSchema, err := json.Marshal(item.VariableSchema)
	if err != nil {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("marshal build template variable schema: %w", err)
	}
	defaultVariables, err := json.Marshal(item.DefaultVariables)
	if err != nil {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("marshal build template default variables: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO build_templates (id, template_key, name, description, builder_kind, dockerfile_template, build_commands, variable_schema, default_variables, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Key, item.Name, nullableString(item.Description), item.BuilderKind, nullableString(item.DockerfileTemplate), string(buildCommands), string(variableSchema), string(defaultVariables), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("create build template: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateBuildTemplate(ctx context.Context, id string, input domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error) {
	item := normalizeBuildTemplateInput(input)
	item.ID = strings.TrimSpace(id)
	buildCommands, err := json.Marshal(item.BuildCommands)
	if err != nil {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("marshal build template commands: %w", err)
	}
	variableSchema, err := json.Marshal(item.VariableSchema)
	if err != nil {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("marshal build template variable schema: %w", err)
	}
	defaultVariables, err := json.Marshal(item.DefaultVariables)
	if err != nil {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("marshal build template default variables: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE build_templates
		SET template_key = ?, name = ?, description = ?, builder_kind = ?, dockerfile_template = ?, build_commands = ?, variable_schema = ?, default_variables = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Key, item.Name, nullableString(item.Description), item.BuilderKind, nullableString(item.DockerfileTemplate), string(buildCommands), string(variableSchema), string(defaultVariables), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("update build template: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincatalog.BuildTemplate{}, ErrNotFound
	}
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "build_templates", item.ID)
	return item, nil
}

func (r *Repository) DeleteBuildTemplate(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM build_templates WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete build template: %w", result.Error)
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
		SELECT id, application_environment_id, cluster_id, namespace, target_kind, executor_kind, group_key, wave_key, region_key, config_ref, workload_kind, workload_name, container_name, metadata, enabled, created_at, updated_at
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
		metadata, err := json.Marshal(item.Metadata)
		if err != nil {
			return fmt.Errorf("marshal release target metadata: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO release_targets (id, application_environment_id, cluster_id, namespace, target_kind, executor_kind, group_key, wave_key, region_key, config_ref, workload_kind, workload_name, container_name, metadata, enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.ApplicationEnvironmentID, item.ClusterID, item.Namespace, item.TargetKind, item.ExecutorKind, nullableString(item.GroupKey), nullableString(item.WaveKey), nullableString(item.RegionKey), nullableString(item.ConfigRef), item.WorkloadKind, item.WorkloadName, nullableString(item.ContainerName), string(metadata), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return fmt.Errorf("create release target: %w", err)
		}
	}
	return nil
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

func scanApplicationEnvironment(rows *sql.Rows) (domaincatalog.ApplicationEnvironment, error) {
	var item domaincatalog.ApplicationEnvironment
	var businessLineID sql.NullString
	var applicationGroup sql.NullString
	var environmentKey sql.NullString
	var strategyProfileID sql.NullString
	var promotionPolicyID sql.NullString
	var approvalPolicyID sql.NullString
	var artifactPolicyID sql.NullString
	var workflowTemplateID sql.NullString
	var buildPolicy []byte
	var releasePolicy []byte
	var resourceSelector []byte
	if err := rows.Scan(&item.ID, &item.ApplicationID, &businessLineID, &applicationGroup, &item.EnvironmentID, &environmentKey, &strategyProfileID, &promotionPolicyID, &approvalPolicyID, &artifactPolicyID, &workflowTemplateID, &buildPolicy, &releasePolicy, &resourceSelector, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("scan application environment: %w", err)
	}
	item.BusinessLineID = businessLineID.String
	item.ApplicationGroup = applicationGroup.String
	item.EnvironmentKey = environmentKey.String
	item.StrategyProfileID = strategyProfileID.String
	item.PromotionPolicyID = promotionPolicyID.String
	item.ApprovalPolicyID = approvalPolicyID.String
	item.ArtifactPolicyID = artifactPolicyID.String
	item.WorkflowTemplateID = workflowTemplateID.String
	_ = json.Unmarshal(buildPolicy, &item.BuildPolicy)
	_ = json.Unmarshal(releasePolicy, &item.ReleasePolicy)
	_ = json.Unmarshal(resourceSelector, &item.ResourceSelector)
	return item, nil
}

func scanApplicationEnvironmentRow(row *sql.Row) (domaincatalog.ApplicationEnvironment, error) {
	var item domaincatalog.ApplicationEnvironment
	var businessLineID sql.NullString
	var applicationGroup sql.NullString
	var environmentKey sql.NullString
	var strategyProfileID sql.NullString
	var promotionPolicyID sql.NullString
	var approvalPolicyID sql.NullString
	var artifactPolicyID sql.NullString
	var workflowTemplateID sql.NullString
	var buildPolicy []byte
	var releasePolicy []byte
	var resourceSelector []byte
	if err := row.Scan(&item.ID, &item.ApplicationID, &businessLineID, &applicationGroup, &item.EnvironmentID, &environmentKey, &strategyProfileID, &promotionPolicyID, &approvalPolicyID, &artifactPolicyID, &workflowTemplateID, &buildPolicy, &releasePolicy, &resourceSelector, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincatalog.ApplicationEnvironment{}, ErrNotFound
		}
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("scan application environment row: %w", err)
	}
	item.BusinessLineID = businessLineID.String
	item.ApplicationGroup = applicationGroup.String
	item.EnvironmentKey = environmentKey.String
	item.StrategyProfileID = strategyProfileID.String
	item.PromotionPolicyID = promotionPolicyID.String
	item.ApprovalPolicyID = approvalPolicyID.String
	item.ArtifactPolicyID = artifactPolicyID.String
	item.WorkflowTemplateID = workflowTemplateID.String
	_ = json.Unmarshal(buildPolicy, &item.BuildPolicy)
	_ = json.Unmarshal(releasePolicy, &item.ReleasePolicy)
	_ = json.Unmarshal(resourceSelector, &item.ResourceSelector)
	return item, nil
}

func scanReleaseTarget(rows *sql.Rows) (domaincatalog.ReleaseTarget, error) {
	var item domaincatalog.ReleaseTarget
	var targetKind sql.NullString
	var executorKind sql.NullString
	var groupKey sql.NullString
	var waveKey sql.NullString
	var regionKey sql.NullString
	var configRef sql.NullString
	var containerName sql.NullString
	var metadata []byte
	if err := rows.Scan(&item.ID, &item.ApplicationEnvironmentID, &item.ClusterID, &item.Namespace, &targetKind, &executorKind, &groupKey, &waveKey, &regionKey, &configRef, &item.WorkloadKind, &item.WorkloadName, &containerName, &metadata, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincatalog.ReleaseTarget{}, fmt.Errorf("scan release target: %w", err)
	}
	item.TargetKind = targetKind.String
	item.ExecutorKind = executorKind.String
	item.GroupKey = groupKey.String
	item.WaveKey = waveKey.String
	item.RegionKey = regionKey.String
	item.ConfigRef = configRef.String
	item.ContainerName = containerName.String
	_ = json.Unmarshal(metadata, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanBuildTemplate(rows *sql.Rows) (domaincatalog.BuildTemplate, error) {
	var item domaincatalog.BuildTemplate
	var description sql.NullString
	var dockerfileTemplate sql.NullString
	var buildCommands []byte
	var variableSchema []byte
	var defaultVariables []byte
	if err := rows.Scan(&item.ID, &item.Key, &item.Name, &description, &item.BuilderKind, &dockerfileTemplate, &buildCommands, &variableSchema, &defaultVariables, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("scan build template: %w", err)
	}
	item.Description = description.String
	item.DockerfileTemplate = dockerfileTemplate.String
	_ = json.Unmarshal(buildCommands, &item.BuildCommands)
	_ = json.Unmarshal(variableSchema, &item.VariableSchema)
	_ = json.Unmarshal(defaultVariables, &item.DefaultVariables)
	if item.VariableSchema == nil {
		item.VariableSchema = map[string]any{}
	}
	if item.DefaultVariables == nil {
		item.DefaultVariables = map[string]any{}
	}
	return item, nil
}

func scanBuildTemplateRow(row *sql.Row) (domaincatalog.BuildTemplate, error) {
	var item domaincatalog.BuildTemplate
	var description sql.NullString
	var dockerfileTemplate sql.NullString
	var buildCommands []byte
	var variableSchema []byte
	var defaultVariables []byte
	if err := row.Scan(&item.ID, &item.Key, &item.Name, &description, &item.BuilderKind, &dockerfileTemplate, &buildCommands, &variableSchema, &defaultVariables, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincatalog.BuildTemplate{}, ErrNotFound
		}
		return domaincatalog.BuildTemplate{}, fmt.Errorf("scan build template row: %w", err)
	}
	item.Description = description.String
	item.DockerfileTemplate = dockerfileTemplate.String
	_ = json.Unmarshal(buildCommands, &item.BuildCommands)
	_ = json.Unmarshal(variableSchema, &item.VariableSchema)
	_ = json.Unmarshal(defaultVariables, &item.DefaultVariables)
	if item.VariableSchema == nil {
		item.VariableSchema = map[string]any{}
	}
	if item.DefaultVariables == nil {
		item.DefaultVariables = map[string]any{}
	}
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

func normalizeApplicationEnvironmentInput(input domaincatalog.ApplicationEnvironmentInput) domaincatalog.ApplicationEnvironment {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	return domaincatalog.ApplicationEnvironment{
		ID:                 id,
		ApplicationID:      strings.TrimSpace(input.ApplicationID),
		EnvironmentID:      strings.TrimSpace(input.EnvironmentID),
		StrategyProfileID:  strings.TrimSpace(input.StrategyProfileID),
		PromotionPolicyID:  strings.TrimSpace(input.PromotionPolicyID),
		ApprovalPolicyID:   strings.TrimSpace(input.ApprovalPolicyID),
		ArtifactPolicyID:   strings.TrimSpace(input.ArtifactPolicyID),
		WorkflowTemplateID: strings.TrimSpace(input.WorkflowTemplateID),
		BuildPolicy:        input.BuildPolicy,
		ReleasePolicy:      input.ReleasePolicy,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func normalizeBuildTemplateInput(input domaincatalog.BuildTemplateInput) domaincatalog.BuildTemplate {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	if input.VariableSchema == nil {
		input.VariableSchema = map[string]any{}
	}
	if input.DefaultVariables == nil {
		input.DefaultVariables = map[string]any{}
	}
	return domaincatalog.BuildTemplate{
		ID:                 id,
		Key:                strings.TrimSpace(input.Key),
		Name:               strings.TrimSpace(input.Name),
		Description:        strings.TrimSpace(input.Description),
		BuilderKind:        firstNonEmpty(strings.TrimSpace(input.BuilderKind), "custom"),
		DockerfileTemplate: input.DockerfileTemplate,
		BuildCommands:      input.BuildCommands,
		VariableSchema:     input.VariableSchema,
		DefaultVariables:   input.DefaultVariables,
		Enabled:            input.Enabled,
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
		TargetKind:               firstNonEmpty(strings.TrimSpace(input.TargetKind), "k8s_workload"),
		ExecutorKind:             firstNonEmpty(strings.TrimSpace(input.ExecutorKind), "k8s_job_runner"),
		GroupKey:                 strings.TrimSpace(input.GroupKey),
		WaveKey:                  strings.TrimSpace(input.WaveKey),
		RegionKey:                strings.TrimSpace(input.RegionKey),
		ConfigRef:                strings.TrimSpace(input.ConfigRef),
		WorkloadKind:             strings.TrimSpace(input.WorkloadKind),
		WorkloadName:             strings.TrimSpace(input.WorkloadName),
		ContainerName:            strings.TrimSpace(input.ContainerName),
		Metadata:                 input.Metadata,
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
