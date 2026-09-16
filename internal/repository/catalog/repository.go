package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"strings"
	"time"

	"github.com/google/uuid"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

var ErrNotFound = fmt.Errorf("%w: catalog record not found", apperrors.ErrNotFound)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListEnvironments(ctx context.Context) ([]domaincatalog.Environment, error) {
	rows, err := dbtx.DB(ctx, r.db).Raw(`
		SELECT id, environment_key, name, tier, stage_level, sort_order, is_production, requires_approval, enabled, created_at, updated_at
		FROM delivery_environments
		ORDER BY sort_order ASC, name ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query delivery environments: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
	rows, err := dbtx.DB(ctx, r.db).Raw(`
		SELECT ae.id, ae.application_id, a.business_line_id, a.app_group, ae.environment_id, COALESCE(e.environment_key, ae.environment_id), ae.alias, ae.cluster_id, ae.namespace, ae.registry_id, ae.strategy_profile_id, ae.promotion_policy_id, ae.artifact_policy_id, ae.workflow_template_id, ae.workflow_template_version, ae.build_policy, ae.release_policy, ae.resource_selector, ae.created_at, ae.updated_at
		FROM application_environments ae
		JOIN applications a ON a.id = ae.application_id
		LEFT JOIN delivery_environments e ON e.id = ae.environment_id
		ORDER BY ae.created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query application environments: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
			template, templateErr := r.GetWorkflowTemplateVersion(ctx, item.WorkflowTemplateID, item.WorkflowTemplateVersion)
			if templateErr != nil {
				return nil, fmt.Errorf("read bound workflow version: %w", templateErr)
			}
			item.WorkflowTemplate = &template
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetApplicationEnvironment(ctx context.Context, id string) (domaincatalog.ApplicationEnvironment, error) {
	row := dbtx.DB(ctx, r.db).Raw(`
		SELECT ae.id, ae.application_id, a.business_line_id, a.app_group, ae.environment_id, COALESCE(e.environment_key, ae.environment_id), ae.alias, ae.cluster_id, ae.namespace, ae.registry_id, ae.strategy_profile_id, ae.promotion_policy_id, ae.artifact_policy_id, ae.workflow_template_id, ae.workflow_template_version, ae.build_policy, ae.release_policy, ae.resource_selector, ae.created_at, ae.updated_at
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
		template, templateErr := r.GetWorkflowTemplateVersion(ctx, item.WorkflowTemplateID, item.WorkflowTemplateVersion)
		if templateErr != nil {
			return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("read bound workflow version: %w", templateErr)
		}
		item.WorkflowTemplate = &template
	}
	return item, nil
}

func (r *Repository) CreateApplicationEnvironment(ctx context.Context, input domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	item := normalizeApplicationEnvironmentInput(input)
	if err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := resolveWorkflowBindingVersion(tx, &item, true); err != nil {
			return err
		}
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
			INSERT INTO application_environments (id, application_id, environment_id, alias, cluster_id, namespace, registry_id, strategy_profile_id, promotion_policy_id, artifact_policy_id, workflow_template_id, workflow_template_version, build_policy, release_policy, resource_selector, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.ApplicationID, item.EnvironmentID, nullableString(item.Alias), nullableString(item.ClusterID), nullableString(item.Namespace), nullableString(item.RegistryID), nullableString(item.StrategyProfileID), nullableString(item.PromotionPolicyID), nullableString(item.ArtifactPolicyID), nullableString(item.WorkflowTemplateID), item.WorkflowTemplateVersion, string(buildPolicy), string(releasePolicy), string(resourceSelector), item.CreatedAt, item.UpdatedAt).Error; err != nil {
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
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := resolveWorkflowBindingVersion(tx, &item, false); err != nil {
			return err
		}
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
		query := `
			UPDATE application_environments
			SET application_id = ?, environment_id = ?, alias = ?, cluster_id = ?, namespace = ?, registry_id = ?, strategy_profile_id = ?, promotion_policy_id = ?, artifact_policy_id = ?, workflow_template_id = ?, workflow_template_version = ?, build_policy = ?, release_policy = ?, resource_selector = ?, updated_at = ?
			WHERE id = ?
		`
		args := []any{item.ApplicationID, item.EnvironmentID, nullableString(item.Alias), nullableString(item.ClusterID), nullableString(item.Namespace), nullableString(item.RegistryID), nullableString(item.StrategyProfileID), nullableString(item.PromotionPolicyID), nullableString(item.ArtifactPolicyID), nullableString(item.WorkflowTemplateID), item.WorkflowTemplateVersion, string(buildPolicy), string(releasePolicy), string(resourceSelector), item.UpdatedAt, item.ID}
		if input.ExpectedUpdatedAt != nil {
			query += " AND updated_at = ?"
			args = append(args, *input.ExpectedUpdatedAt)
		}
		result := tx.Exec(query, args...)
		if result.Error != nil {
			return fmt.Errorf("update application environment: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			if input.ExpectedUpdatedAt != nil {
				return fmt.Errorf("%w: application environment changed; reload before saving", apperrors.ErrConflict)
			}
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
	result := dbtx.DB(ctx, r.db).Exec(`DELETE FROM application_environments WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete application environment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListBuildTemplates(ctx context.Context) ([]domaincatalog.BuildTemplate, error) {
	rows, err := dbtx.DB(ctx, r.db).Raw(`
		SELECT id, template_key, name, description, builder_kind, dockerfile_template, build_commands, variable_schema, default_variables, enabled, created_at, updated_at, revision, published_version, publication_state, content_digest
		FROM build_templates
		ORDER BY name ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query build templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
	row := dbtx.DB(ctx, r.db).Raw(`
		SELECT id, template_key, name, description, builder_kind, dockerfile_template, build_commands, variable_schema, default_variables, enabled, created_at, updated_at, revision, published_version, publication_state, content_digest
		FROM build_templates
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanBuildTemplateRow(row)
}

func (r *Repository) CreateBuildTemplate(ctx context.Context, input domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error) {
	return r.saveBuildTemplate(ctx, "", input)
}

func (r *Repository) UpdateBuildTemplate(ctx context.Context, id string, input domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error) {
	return r.saveBuildTemplate(ctx, strings.TrimSpace(id), input)
}

func (r *Repository) DeleteBuildTemplate(ctx context.Context, id string) error {
	return r.deprecateTemplate(ctx, "build_templates", id)
}

func (r *Repository) ListWorkflowTemplates(ctx context.Context) ([]domaincatalog.WorkflowTemplate, error) {
	rows, err := dbtx.DB(ctx, r.db).Raw(`
		SELECT id, template_key, name, description, category, definition, enabled, created_at, updated_at, revision, published_version, publication_state, content_digest
		FROM workflow_templates
		ORDER BY name ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query workflow templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
	row := dbtx.DB(ctx, r.db).Raw(`
		SELECT id, template_key, name, description, category, definition, enabled, created_at, updated_at, revision, published_version, publication_state, content_digest
		FROM workflow_templates
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanWorkflowTemplateRow(row)
}

func (r *Repository) CreateWorkflowTemplate(ctx context.Context, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	return r.saveWorkflowTemplate(ctx, "", input)
}

func (r *Repository) UpdateWorkflowTemplate(ctx context.Context, id string, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	return r.saveWorkflowTemplate(ctx, strings.TrimSpace(id), input)
}

func (r *Repository) DeleteWorkflowTemplate(ctx context.Context, id string) error {
	return r.deprecateTemplate(ctx, "workflow_templates", id)
}

func (r *Repository) SaveApplicationWorkflow(ctx context.Context, applicationID, bindingID string, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	applicationID, bindingID = strings.TrimSpace(applicationID), strings.TrimSpace(bindingID)
	input.Category = "application:" + applicationID
	input.Publish = nil
	var item domaincatalog.WorkflowTemplate
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var currentID sql.NullString
		var currentVersion int64
		if err := tx.Raw(`SELECT workflow_template_id, workflow_template_version FROM application_environments WHERE id = ? AND application_id = ? FOR UPDATE`, bindingID, applicationID).Row().Scan(&currentID, &currentVersion); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		repo := New(tx)
		id := ""
		if currentID.Valid {
			current, err := repo.GetWorkflowTemplateVersion(ctx, currentID.String, currentVersion)
			if err != nil {
				return err
			}
			if input.ExpectedRevision != nil && current.Revision != *input.ExpectedRevision {
				return fmt.Errorf("%w: workflow changed; reload before saving", apperrors.ErrConflict)
			}
			if current.Category == input.Category {
				id, input.Key = current.ID, current.Key
			} else {
				input.ExpectedRevision = nil
			}
		}
		var err error
		item, err = repo.saveWorkflowTemplate(ctx, id, input)
		if err != nil {
			return err
		}
		return tx.Exec(`UPDATE application_environments SET workflow_template_id = ?, workflow_template_version = ?, updated_at = ? WHERE id = ? AND application_id = ?`,
			item.ID, item.PublishedVersion, item.UpdatedAt, bindingID, applicationID).Error
	})
	return item, err
}

func (r *Repository) listReleaseTargets(ctx context.Context, applicationEnvironmentID string) ([]domaincatalog.ReleaseTarget, error) {
	rows, err := dbtx.DB(ctx, r.db).Raw(`
		SELECT id, application_environment_id, COALESCE(cluster_id, ''), namespace, target_kind, executor_kind, group_key, wave_key, region_key, config_ref, workload_kind, workload_name, container_name, metadata, enabled, created_at, updated_at, helm_configuration, docker_configuration
		FROM release_targets
		WHERE application_environment_id = ?
		ORDER BY created_at ASC
	`, applicationEnvironmentID).Rows()
	if err != nil {
		return nil, fmt.Errorf("query release targets: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
		docker, err := json.Marshal(item.Docker)
		if err != nil {
			return err
		}
		helm, err := json.Marshal(item.Helm)
		if err != nil {
			return fmt.Errorf("marshal Helm target configuration: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO release_targets (id, application_environment_id, cluster_id, namespace, target_kind, executor_kind, group_key, wave_key, region_key, config_ref, workload_kind, workload_name, container_name, metadata, enabled, created_at, updated_at, helm_configuration, docker_configuration)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb)
		`, item.ID, item.ApplicationEnvironmentID, nullableString(item.ClusterID), item.Namespace, item.TargetKind, item.ExecutorKind, nullableString(item.GroupKey), nullableString(item.WaveKey), nullableString(item.RegionKey), nullableString(item.ConfigRef), item.WorkloadKind, item.WorkloadName, nullableString(item.ContainerName), string(metadata), item.Enabled, item.CreatedAt, item.UpdatedAt, string(helm), string(docker)).Error; err != nil {
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
	var alias sql.NullString
	var clusterID sql.NullString
	var namespace sql.NullString
	var registryID sql.NullString
	var strategyProfileID sql.NullString
	var promotionPolicyID sql.NullString
	var artifactPolicyID sql.NullString
	var workflowTemplateID sql.NullString
	var buildPolicy []byte
	var releasePolicy []byte
	var resourceSelector []byte
	if err := rows.Scan(&item.ID, &item.ApplicationID, &businessLineID, &applicationGroup, &item.EnvironmentID, &environmentKey, &alias, &clusterID, &namespace, &registryID, &strategyProfileID, &promotionPolicyID, &artifactPolicyID, &workflowTemplateID, &item.WorkflowTemplateVersion, &buildPolicy, &releasePolicy, &resourceSelector, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("scan application environment: %w", err)
	}
	item.BusinessLineID = businessLineID.String
	item.ApplicationGroup = applicationGroup.String
	item.EnvironmentKey = environmentKey.String
	item.Alias = alias.String
	item.ClusterID = clusterID.String
	item.Namespace = namespace.String
	item.RegistryID = registryID.String
	item.StrategyProfileID = strategyProfileID.String
	item.PromotionPolicyID = promotionPolicyID.String
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
	var alias sql.NullString
	var clusterID sql.NullString
	var namespace sql.NullString
	var registryID sql.NullString
	var strategyProfileID sql.NullString
	var promotionPolicyID sql.NullString
	var artifactPolicyID sql.NullString
	var workflowTemplateID sql.NullString
	var buildPolicy []byte
	var releasePolicy []byte
	var resourceSelector []byte
	if err := row.Scan(&item.ID, &item.ApplicationID, &businessLineID, &applicationGroup, &item.EnvironmentID, &environmentKey, &alias, &clusterID, &namespace, &registryID, &strategyProfileID, &promotionPolicyID, &artifactPolicyID, &workflowTemplateID, &item.WorkflowTemplateVersion, &buildPolicy, &releasePolicy, &resourceSelector, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincatalog.ApplicationEnvironment{}, ErrNotFound
		}
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("scan application environment row: %w", err)
	}
	item.BusinessLineID = businessLineID.String
	item.ApplicationGroup = applicationGroup.String
	item.EnvironmentKey = environmentKey.String
	item.Alias = alias.String
	item.ClusterID = clusterID.String
	item.Namespace = namespace.String
	item.RegistryID = registryID.String
	item.StrategyProfileID = strategyProfileID.String
	item.PromotionPolicyID = promotionPolicyID.String
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
	var docker []byte
	var helm []byte
	if err := rows.Scan(&item.ID, &item.ApplicationEnvironmentID, &item.ClusterID, &item.Namespace, &targetKind, &executorKind, &groupKey, &waveKey, &regionKey, &configRef, &item.WorkloadKind, &item.WorkloadName, &containerName, &metadata, &item.Enabled, &item.CreatedAt, &item.UpdatedAt, &helm, &docker); err != nil {
		return domaincatalog.ReleaseTarget{}, fmt.Errorf("scan release target: %w", err)
	}
	item.TargetKind = targetKind.String
	item.ExecutorKind = executorKind.String
	item.GroupKey = groupKey.String
	item.WaveKey = waveKey.String
	item.RegionKey = regionKey.String
	item.ConfigRef = configRef.String
	item.ContainerName = containerName.String
	if len(docker) > 0 {
		if err := json.Unmarshal(docker, &item.Docker); err != nil {
			return item, err
		}
	}
	if len(helm) > 0 {
		if err := json.Unmarshal(helm, &item.Helm); err != nil {
			return domaincatalog.ReleaseTarget{}, fmt.Errorf("decode Helm target configuration: %w", err)
		}
	}
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
	if err := rows.Scan(&item.ID, &item.Key, &item.Name, &description, &item.BuilderKind, &dockerfileTemplate, &buildCommands, &variableSchema, &defaultVariables, &item.Enabled, &item.CreatedAt, &item.UpdatedAt, &item.Revision, &item.PublishedVersion, &item.PublicationState, &item.ContentDigest); err != nil {
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
	if err := row.Scan(&item.ID, &item.Key, &item.Name, &description, &item.BuilderKind, &dockerfileTemplate, &buildCommands, &variableSchema, &defaultVariables, &item.Enabled, &item.CreatedAt, &item.UpdatedAt, &item.Revision, &item.PublishedVersion, &item.PublicationState, &item.ContentDigest); err != nil {
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
	if err := rows.Scan(&item.ID, &item.Key, &item.Name, &description, &category, &definition, &item.Enabled, &item.CreatedAt, &item.UpdatedAt, &item.Revision, &item.PublishedVersion, &item.PublicationState, &item.ContentDigest); err != nil {
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
	if err := row.Scan(&item.ID, &item.Key, &item.Name, &description, &category, &definition, &item.Enabled, &item.CreatedAt, &item.UpdatedAt, &item.Revision, &item.PublishedVersion, &item.PublicationState, &item.ContentDigest); err != nil {
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
		ID:                      id,
		ApplicationID:           strings.TrimSpace(input.ApplicationID),
		EnvironmentID:           strings.TrimSpace(input.EnvironmentID),
		Alias:                   strings.TrimSpace(input.Alias),
		ClusterID:               strings.TrimSpace(input.ClusterID),
		Namespace:               strings.TrimSpace(input.Namespace),
		RegistryID:              strings.TrimSpace(input.RegistryID),
		StrategyProfileID:       strings.TrimSpace(input.StrategyProfileID),
		PromotionPolicyID:       strings.TrimSpace(input.PromotionPolicyID),
		ArtifactPolicyID:        strings.TrimSpace(input.ArtifactPolicyID),
		WorkflowTemplateID:      strings.TrimSpace(input.WorkflowTemplateID),
		WorkflowTemplateVersion: input.WorkflowTemplateVersion,
		BuildPolicy:             input.BuildPolicy,
		ReleasePolicy:           input.ReleasePolicy,
		ResourceSelector:        input.ResourceSelector,
		CreatedAt:               now,
		UpdatedAt:               now,
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
		Helm:                     input.Helm,
		Docker:                   input.Docker,
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
	if err := dbtx.DB(ctx, db).Raw(query, id).Row().Scan(&createdAt); err != nil {
		return time.Time{}
	}
	return createdAt
}
