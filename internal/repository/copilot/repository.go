package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListSessions(ctx context.Context, createdBy string, limit int) ([]domaincopilot.Session, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, title, created_by, metadata, created_at, updated_at
		FROM ai_sessions
		WHERE created_by = ? AND deleted_at IS NULL
		ORDER BY updated_at DESC, created_at DESC
		LIMIT ?
	`, createdBy, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query ai sessions: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.Session, 0, limit)
	for rows.Next() {
		item, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetSession(ctx context.Context, createdBy, sessionID string) (domaincopilot.Session, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, title, created_by, metadata, created_at, updated_at
		FROM ai_sessions
		WHERE created_by = ? AND id = ? AND deleted_at IS NULL
		LIMIT 1
	`, createdBy, sessionID).Row()
	return scanSessionRow(row)
}

func (r *Repository) CreateSession(ctx context.Context, session domaincopilot.Session) (domaincopilot.Session, error) {
	metadata, err := json.Marshal(session.Metadata)
	if err != nil {
		return domaincopilot.Session{}, fmt.Errorf("marshal ai session metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_sessions (id, title, created_by, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, session.ID, session.Title, session.CreatedBy, string(metadata), session.CreatedAt, session.UpdatedAt).Error; err != nil {
		return domaincopilot.Session{}, fmt.Errorf("create ai session: %w", err)
	}
	return session, nil
}

func (r *Repository) UpdateSession(ctx context.Context, createdBy, sessionID string, session domaincopilot.Session) (domaincopilot.Session, error) {
	metadata, err := json.Marshal(session.Metadata)
	if err != nil {
		return domaincopilot.Session{}, fmt.Errorf("marshal ai session metadata: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_sessions
		SET title = ?, metadata = ?, updated_at = ?
		WHERE created_by = ? AND id = ? AND deleted_at IS NULL
	`, session.Title, string(metadata), session.UpdatedAt, createdBy, sessionID)
	if result.Error != nil {
		return domaincopilot.Session{}, fmt.Errorf("update ai session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincopilot.Session{}, fmt.Errorf("ai session not found: %s", sessionID)
	}
	return r.GetSession(ctx, createdBy, sessionID)
}

func (r *Repository) DeleteSession(ctx context.Context, createdBy, sessionID string) error {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_sessions
		SET deleted_at = ?, updated_at = ?
		WHERE created_by = ? AND id = ? AND deleted_at IS NULL
	`, time.Now().UTC(), time.Now().UTC(), createdBy, sessionID)
	if result.Error != nil {
		return fmt.Errorf("delete ai session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("ai session not found: %s", sessionID)
	}
	return nil
}

func (r *Repository) ListMessages(ctx context.Context, sessionID string, limit int) ([]domaincopilot.Message, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, session_id, role, content, metadata, created_at
		FROM ai_messages
		WHERE session_id = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, sessionID, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query ai messages: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.Message, 0, limit)
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListRecentMessages(ctx context.Context, sessionID string, limit int) ([]domaincopilot.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, session_id, role, content, metadata, created_at
		FROM (
			SELECT id, session_id, role, content, metadata, created_at
			FROM ai_messages
			WHERE session_id = ?
			ORDER BY created_at DESC
			LIMIT ?
		) recent_messages
		ORDER BY created_at ASC
	`, sessionID, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query recent ai messages: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.Message, 0, limit)
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateMessage(ctx context.Context, message domaincopilot.Message) (domaincopilot.Message, error) {
	metadata, err := json.Marshal(message.Metadata)
	if err != nil {
		return domaincopilot.Message{}, fmt.Errorf("marshal ai message metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_messages (id, session_id, role, content, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, message.ID, message.SessionID, message.Role, message.Content, string(metadata), message.CreatedAt).Error; err != nil {
		return domaincopilot.Message{}, fmt.Errorf("create ai message: %w", err)
	}
	if message.Role == "assistant" || message.Role == "user" {
		_ = r.db.WithContext(ctx).Exec(`UPDATE ai_sessions SET updated_at = ? WHERE id = ?`, time.Now().UTC(), message.SessionID).Error
	}
	return message, nil
}

func (r *Repository) ListDataSources(ctx context.Context) ([]domaincopilot.DataSource, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, source_kind, backend_type, enabled, credential_ref, scope, query_budget, redaction_policy, mcp_adapter, config,
			validation_status, validation_message, last_validated_at, created_at, updated_at
		FROM ai_data_sources
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query data sources: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.DataSource, 0)
	for rows.Next() {
		item, err := scanDataSource(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetDataSource(ctx context.Context, dataSourceID string) (domaincopilot.DataSource, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, source_kind, backend_type, enabled, credential_ref, scope, query_budget, redaction_policy, mcp_adapter, config,
			validation_status, validation_message, last_validated_at, created_at, updated_at
		FROM ai_data_sources
		WHERE id = ?
		LIMIT 1
	`, dataSourceID).Row()
	return scanDataSourceRow(row)
}

func (r *Repository) CreateDataSource(ctx context.Context, item domaincopilot.DataSource) (domaincopilot.DataSource, error) {
	scope, err := json.Marshal(item.Scope)
	if err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("marshal data source scope: %w", err)
	}
	queryBudget, err := json.Marshal(item.QueryBudget)
	if err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("marshal data source query budget: %w", err)
	}
	redactionPolicy, err := json.Marshal(item.RedactionPolicy)
	if err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("marshal data source redaction policy: %w", err)
	}
	config, err := json.Marshal(item.Config)
	if err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("marshal data source config: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_data_sources (
			id, name, source_kind, backend_type, enabled, credential_ref, scope, query_budget, redaction_policy, mcp_adapter, config,
			validation_status, validation_message, last_validated_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.SourceKind, item.BackendType, item.Enabled, nullableString(item.CredentialRef), string(scope), string(queryBudget), string(redactionPolicy), item.MCPAdapter, string(config), nil, nil, nil, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("create data source: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateDataSource(ctx context.Context, dataSourceID string, input domaincopilot.DataSourceInput) (domaincopilot.DataSource, error) {
	scope, err := json.Marshal(input.Scope)
	if err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("marshal data source scope: %w", err)
	}
	queryBudget, err := json.Marshal(input.QueryBudget)
	if err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("marshal data source query budget: %w", err)
	}
	redactionPolicy, err := json.Marshal(input.RedactionPolicy)
	if err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("marshal data source redaction policy: %w", err)
	}
	config, err := json.Marshal(input.Config)
	if err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("marshal data source config: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_data_sources
		SET name = ?, source_kind = ?, backend_type = ?, enabled = ?, credential_ref = ?, scope = ?, query_budget = ?, redaction_policy = ?, mcp_adapter = ?, config = ?,
			validation_status = NULL, validation_message = NULL, last_validated_at = NULL, updated_at = ?
		WHERE id = ?
	`, input.Name, input.SourceKind, input.BackendType, input.Enabled, nullableString(input.CredentialRef), string(scope), string(queryBudget), string(redactionPolicy), input.MCPAdapter, string(config), time.Now().UTC(), dataSourceID)
	if result.Error != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("update data source: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincopilot.DataSource{}, fmt.Errorf("data source not found: %s", dataSourceID)
	}
	return r.GetDataSource(ctx, dataSourceID)
}

func (r *Repository) UpdateDataSourceValidation(ctx context.Context, dataSourceID, status, message string, validatedAt time.Time) (domaincopilot.DataSource, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_data_sources
		SET validation_status = ?, validation_message = ?, last_validated_at = ?, updated_at = ?
		WHERE id = ?
	`, nullableString(status), nullableString(message), validatedAt, time.Now().UTC(), dataSourceID)
	if result.Error != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("update data source validation: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincopilot.DataSource{}, fmt.Errorf("data source not found: %s", dataSourceID)
	}
	return r.GetDataSource(ctx, dataSourceID)
}

func (r *Repository) ListAnalysisProfiles(ctx context.Context) ([]domaincopilot.AnalysisProfile, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, mode, enabled_sources, enabled_playbooks, query_budgets, output_style, remediation_policy, default_time_range_minutes, timeout_seconds, enabled, created_at, updated_at
		FROM ai_analysis_profiles
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query analysis profiles: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.AnalysisProfile, 0)
	for rows.Next() {
		item, err := scanAnalysisProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateAnalysisProfile(ctx context.Context, item domaincopilot.AnalysisProfile) (domaincopilot.AnalysisProfile, error) {
	enabledSources, err := json.Marshal(item.EnabledSources)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("marshal enabled sources: %w", err)
	}
	enabledPlaybooks, err := json.Marshal(item.EnabledPlaybooks)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("marshal enabled playbooks: %w", err)
	}
	queryBudgets, err := json.Marshal(item.QueryBudgets)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("marshal query budgets: %w", err)
	}
	outputStyle, err := json.Marshal(item.OutputStyle)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("marshal output style: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_analysis_profiles (id, name, mode, enabled_sources, enabled_playbooks, query_budgets, output_style, remediation_policy, default_time_range_minutes, timeout_seconds, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.Mode, string(enabledSources), string(enabledPlaybooks), string(queryBudgets), string(outputStyle), item.RemediationPolicy, item.DefaultTimeRangeMinutes, item.TimeoutSeconds, item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("create analysis profile: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateAnalysisProfile(ctx context.Context, profileID string, input domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfile, error) {
	enabledSources, err := json.Marshal(input.EnabledSources)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("marshal enabled sources: %w", err)
	}
	enabledPlaybooks, err := json.Marshal(input.EnabledPlaybooks)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("marshal enabled playbooks: %w", err)
	}
	queryBudgets, err := json.Marshal(input.QueryBudgets)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("marshal query budgets: %w", err)
	}
	outputStyle, err := json.Marshal(input.OutputStyle)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("marshal output style: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_analysis_profiles
		SET name = ?, mode = ?, enabled_sources = ?, enabled_playbooks = ?, query_budgets = ?, output_style = ?, remediation_policy = ?, default_time_range_minutes = ?, timeout_seconds = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, input.Name, input.Mode, string(enabledSources), string(enabledPlaybooks), string(queryBudgets), string(outputStyle), input.RemediationPolicy, input.DefaultTimeRangeMinutes, input.TimeoutSeconds, input.Enabled, time.Now().UTC(), profileID)
	if result.Error != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("update analysis profile: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("analysis profile not found: %s", profileID)
	}
	return r.getAnalysisProfile(ctx, profileID)
}

func (r *Repository) ListAutomationPolicies(ctx context.Context) ([]domaincopilot.AutomationPolicy, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, enabled, trigger_type, analysis_kinds, agent_provider_id, trigger_conditions, dedup_window_seconds, analysis_profile_id, remediation_policy, approval_policy, cooldown_seconds, created_at, updated_at
		FROM ai_automation_policies
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query automation policies: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.AutomationPolicy, 0)
	for rows.Next() {
		item, err := scanAutomationPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateAutomationPolicy(ctx context.Context, item domaincopilot.AutomationPolicy) (domaincopilot.AutomationPolicy, error) {
	analysisKinds, err := json.Marshal(item.AnalysisKinds)
	if err != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("marshal analysis kinds: %w", err)
	}
	triggerConditions, err := json.Marshal(item.TriggerConditions)
	if err != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("marshal trigger conditions: %w", err)
	}
	approvalPolicy, err := json.Marshal(item.ApprovalPolicy)
	if err != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("marshal approval policy: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_automation_policies (id, name, enabled, trigger_type, analysis_kinds, agent_provider_id, trigger_conditions, dedup_window_seconds, analysis_profile_id, remediation_policy, approval_policy, cooldown_seconds, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.Enabled, item.TriggerType, string(analysisKinds), item.AgentProviderID, string(triggerConditions), item.DedupWindowSeconds, item.AnalysisProfileID, item.RemediationPolicy, string(approvalPolicy), item.CooldownSeconds, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("create automation policy: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateAutomationPolicy(ctx context.Context, policyID string, input domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicy, error) {
	analysisKinds, err := json.Marshal(input.AnalysisKinds)
	if err != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("marshal analysis kinds: %w", err)
	}
	triggerConditions, err := json.Marshal(input.TriggerConditions)
	if err != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("marshal trigger conditions: %w", err)
	}
	approvalPolicy, err := json.Marshal(input.ApprovalPolicy)
	if err != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("marshal approval policy: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_automation_policies
		SET name = ?, enabled = ?, trigger_type = ?, analysis_kinds = ?, agent_provider_id = ?, trigger_conditions = ?, dedup_window_seconds = ?, analysis_profile_id = ?, remediation_policy = ?, approval_policy = ?, cooldown_seconds = ?, updated_at = ?
		WHERE id = ?
	`, input.Name, input.Enabled, input.TriggerType, string(analysisKinds), input.AgentProviderID, string(triggerConditions), input.DedupWindowSeconds, input.AnalysisProfileID, input.RemediationPolicy, string(approvalPolicy), input.CooldownSeconds, time.Now().UTC(), policyID)
	if result.Error != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("update automation policy: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("automation policy not found: %s", policyID)
	}
	return r.getAutomationPolicy(ctx, policyID)
}

func (r *Repository) DeleteAutomationPolicy(ctx context.Context, policyID string) error {
	result := r.db.WithContext(ctx).Exec(`
		DELETE FROM ai_automation_policies
		WHERE id = ?
	`, policyID)
	if result.Error != nil {
		return fmt.Errorf("delete automation policy: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: automation policy %s", apperrors.ErrNotFound, policyID)
	}
	return nil
}

func (r *Repository) ListRootCauseRuns(ctx context.Context, createdBy string, filter domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	query := `
		SELECT id, kind, session_id, title, created_by, analysis_profile_id, trigger_type, status, severity, summary, cluster_id, namespace, workload_kind, workload_name, alert_id,
			time_range_minutes, question, evidence, hypotheses, recommendations, tool_executions, data_source_snapshot, playbook_results, remediation_plan, dedup_key, created_at, updated_at
		FROM ai_root_cause_runs
		WHERE created_by = ?
	`
	args := []any{createdBy}
	if filter.ClusterID != "" {
		query += ` AND cluster_id = ?`
		args = append(args, filter.ClusterID)
	}
	if filter.AlertID != "" {
		query += ` AND alert_id = ?`
		args = append(args, filter.AlertID)
	}
	if filter.TriggerType != "" {
		query += ` AND trigger_type = ?`
		args = append(args, filter.TriggerType)
	}
	if filter.DedupKey != "" {
		query += ` AND dedup_key = ?`
		args = append(args, filter.DedupKey)
	}
	if filter.DedupKeyPrefix != "" {
		query += ` AND dedup_key LIKE ? ESCAPE '!'`
		args = append(args, escapeSQLLikePattern(filter.DedupKeyPrefix)+"%")
	}
	query += ` ORDER BY updated_at DESC, created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query root cause runs: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.RootCauseRun, 0, limit)
	for rows.Next() {
		item, err := scanRootCauseRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetRootCauseRun(ctx context.Context, createdBy, runID string) (domaincopilot.RootCauseRun, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, kind, session_id, title, created_by, analysis_profile_id, trigger_type, status, severity, summary, cluster_id, namespace, workload_kind, workload_name, alert_id,
			time_range_minutes, question, evidence, hypotheses, recommendations, tool_executions, data_source_snapshot, playbook_results, remediation_plan, dedup_key, created_at, updated_at
		FROM ai_root_cause_runs
		WHERE created_by = ? AND id = ?
		LIMIT 1
	`, createdBy, runID).Row()
	return scanRootCauseRunRow(row)
}

func (r *Repository) CreateRootCauseRun(ctx context.Context, run domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error) {
	evidence, err := json.Marshal(run.Evidence)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause evidence: %w", err)
	}
	hypotheses, err := json.Marshal(run.Hypotheses)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause hypotheses: %w", err)
	}
	recommendations, err := json.Marshal(run.Recommendations)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause recommendations: %w", err)
	}
	toolExecutions, err := json.Marshal(run.ToolExecutions)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause tool executions: %w", err)
	}
	dataSourceSnapshot, err := json.Marshal(run.DataSourceSnapshot)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause data source snapshot: %w", err)
	}
	playbookResults, err := json.Marshal(run.PlaybookResults)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause playbook results: %w", err)
	}
	remediationPlan, err := json.Marshal(run.RemediationPlan)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause remediation plan: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_root_cause_runs (
			id, kind, session_id, title, created_by, analysis_profile_id, trigger_type, status, severity, summary, cluster_id, namespace, workload_kind, workload_name, alert_id,
			time_range_minutes, question, evidence, hypotheses, recommendations, tool_executions, data_source_snapshot, playbook_results, remediation_plan, dedup_key, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		run.ID,
		run.Kind,
		nullableString(run.SessionID),
		run.Title,
		run.CreatedBy,
		nullableString(run.AnalysisProfileID),
		nullableString(run.TriggerType),
		run.Status,
		run.Severity,
		run.Summary,
		nullableString(run.ClusterID),
		nullableString(run.Namespace),
		nullableString(run.WorkloadKind),
		nullableString(run.WorkloadName),
		nullableString(run.AlertID),
		run.TimeRangeMinutes,
		nullableString(run.Question),
		string(evidence),
		string(hypotheses),
		string(recommendations),
		string(toolExecutions),
		string(dataSourceSnapshot),
		string(playbookResults),
		string(remediationPlan),
		nullableString(run.DedupKey),
		run.CreatedAt,
		run.UpdatedAt,
	).Error; err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("create root cause run: %w", err)
	}
	return run, nil
}

func (r *Repository) UpdateRootCauseRun(ctx context.Context, run domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error) {
	evidence, err := json.Marshal(run.Evidence)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause evidence: %w", err)
	}
	hypotheses, err := json.Marshal(run.Hypotheses)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause hypotheses: %w", err)
	}
	recommendations, err := json.Marshal(run.Recommendations)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause recommendations: %w", err)
	}
	toolExecutions, err := json.Marshal(run.ToolExecutions)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause tool executions: %w", err)
	}
	dataSourceSnapshot, err := json.Marshal(run.DataSourceSnapshot)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause data source snapshot: %w", err)
	}
	playbookResults, err := json.Marshal(run.PlaybookResults)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause playbook results: %w", err)
	}
	remediationPlan, err := json.Marshal(run.RemediationPlan)
	if err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("marshal root cause remediation plan: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_root_cause_runs
		SET kind = ?, session_id = ?, title = ?, analysis_profile_id = ?, trigger_type = ?, status = ?, severity = ?, summary = ?,
			cluster_id = ?, namespace = ?, workload_kind = ?, workload_name = ?, alert_id = ?, time_range_minutes = ?, question = ?,
			evidence = ?, hypotheses = ?, recommendations = ?, tool_executions = ?, data_source_snapshot = ?, playbook_results = ?,
			remediation_plan = ?, dedup_key = ?, updated_at = ?
		WHERE id = ? AND created_by = ?
	`, run.Kind, nullableString(run.SessionID), run.Title, nullableString(run.AnalysisProfileID), nullableString(run.TriggerType), run.Status, run.Severity, run.Summary,
		nullableString(run.ClusterID), nullableString(run.Namespace), nullableString(run.WorkloadKind), nullableString(run.WorkloadName), nullableString(run.AlertID), run.TimeRangeMinutes, nullableString(run.Question),
		string(evidence), string(hypotheses), string(recommendations), string(toolExecutions), string(dataSourceSnapshot), string(playbookResults), string(remediationPlan), nullableString(run.DedupKey), run.UpdatedAt, run.ID, run.CreatedBy)
	if result.Error != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("update root cause run: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("%w: root cause run %s", apperrors.ErrNotFound, run.ID)
	}
	return r.GetRootCauseRun(ctx, run.CreatedBy, run.ID)
}

func (r *Repository) ListAgentRuns(ctx context.Context, filter domaincopilot.AgentRunFilter) ([]domaincopilot.AgentRun, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query := agentRunSelect()
	clauses := make([]string, 0, 8)
	args := make([]any, 0, 8)
	if value := strings.TrimSpace(filter.CreatedBy); value != "" {
		clauses = append(clauses, "created_by = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ProviderID); value != "" {
		clauses = append(clauses, "provider_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.CapabilityID); value != "" {
		clauses = append(clauses, "capability_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.TriggerType); value != "" {
		clauses = append(clauses, "input ->> 'triggerType' = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.DedupKey); value != "" {
		clauses = append(clauses, "input ->> 'dedupKey' = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.DedupKeyPrefix); value != "" {
		clauses = append(clauses, "input ->> 'dedupKey' LIKE ? ESCAPE '!'")
		args = append(args, escapeSQLLikePattern(value)+"%")
	}
	if len(clauses) > 0 {
		query += ` WHERE ` + strings.Join(clauses, " AND ")
	}
	query += `
		ORDER BY updated_at DESC, created_at DESC
		LIMIT ?
	`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query ai agent runs: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.AgentRun, 0, limit)
	for rows.Next() {
		item, err := scanAgentRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetAgentRun(ctx context.Context, createdBy, runID string) (domaincopilot.AgentRun, error) {
	query := agentRunSelect() + ` WHERE id = ?`
	args := []any{strings.TrimSpace(runID)}
	if strings.TrimSpace(createdBy) != "" {
		query += ` AND created_by = ?`
		args = append(args, strings.TrimSpace(createdBy))
	}
	query += ` LIMIT 1`
	row := r.db.WithContext(ctx).Raw(query, args...).Row()
	return scanAgentRunRow(row)
}

func (r *Repository) CreateAgentRun(ctx context.Context, run domaincopilot.AgentRun) (domaincopilot.AgentRun, error) {
	skillIDs, err := json.Marshal(run.SkillIDs)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run skills: %w", err)
	}
	scope, err := json.Marshal(run.Scope)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run scope: %w", err)
	}
	toolset, err := json.Marshal(run.Toolset)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run toolset: %w", err)
	}
	toolBindings, err := json.Marshal(run.ToolBindings)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run tool bindings: %w", err)
	}
	skillBindings, err := json.Marshal(run.SkillBindings)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run skill bindings: %w", err)
	}
	input, err := json.Marshal(run.Input)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run input: %w", err)
	}
	output, err := json.Marshal(run.Output)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run output: %w", err)
	}
	toolExecutions, err := json.Marshal(run.ToolExecutions)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run tool executions: %w", err)
	}
	analysisArtifacts, err := json.Marshal(run.AnalysisArtifacts)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run artifacts: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_agent_runs (
			id, provider_id, provider_kind, capability_id, skill_ids, session_id, root_cause_run_id, created_by, status, scope, toolset, tool_bindings, skill_bindings, input, output,
			tool_executions, analysis_artifacts, callback_token, claimed_by_agent_id, external_run_id, error_message, timeout_seconds,
			queued_at, started_at, last_heartbeat_at, completed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		run.ID,
		run.ProviderID,
		run.ProviderKind,
		run.CapabilityID,
		string(skillIDs),
		nullableString(run.SessionID),
		nullableString(run.RootCauseRunID),
		run.CreatedBy,
		run.Status,
		string(scope),
		string(toolset),
		string(toolBindings),
		string(skillBindings),
		string(input),
		string(output),
		string(toolExecutions),
		string(analysisArtifacts),
		run.CallbackToken,
		nullableString(run.ClaimedByAgentID),
		nullableString(run.ExternalRunID),
		nullableString(run.ErrorMessage),
		run.TimeoutSeconds,
		run.QueuedAt,
		run.StartedAt,
		run.LastHeartbeatAt,
		run.CompletedAt,
		run.CreatedAt,
		run.UpdatedAt,
	).Error; err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("create ai agent run: %w", err)
	}
	return run, nil
}

func (r *Repository) ClaimAgentRun(ctx context.Context, input domaincopilot.AgentRunClaimInput) (domaincopilot.AgentRun, error) {
	agentID := strings.TrimSpace(input.AgentID)
	if agentID == "" {
		return domaincopilot.AgentRun{}, apperrors.ErrNotFound
	}
	now := time.Now().UTC()
	var claimed domaincopilot.AgentRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		clauses := []string{"status = ?"}
		args := []any{domaincopilot.AgentRunStatusQueued}
		if values := compactStringValues(input.ProviderIDs); len(values) > 0 {
			clauses = append(clauses, fmt.Sprintf("provider_id IN (%s)", placeholders(len(values))))
			for _, value := range values {
				args = append(args, value)
			}
		}
		if values := compactStringValues(input.Kinds); len(values) > 0 {
			clauses = append(clauses, fmt.Sprintf("provider_kind IN (%s)", placeholders(len(values))))
			for _, value := range values {
				args = append(args, value)
			}
		}
		rows, queryErr := tx.Raw(agentRunSelect()+" WHERE "+strings.Join(clauses, " AND ")+`
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		`, args...).Rows()
		if queryErr != nil {
			return fmt.Errorf("claim ai agent run query: %w", queryErr)
		}
		defer rows.Close()
		if !rows.Next() {
			return apperrors.ErrNotFound
		}
		item, scanErr := scanAgentRun(rows)
		if scanErr != nil {
			return scanErr
		}
		result := tx.Exec(`
			UPDATE ai_agent_runs
			SET status = ?, claimed_by_agent_id = ?, started_at = COALESCE(started_at, ?), last_heartbeat_at = ?, updated_at = ?
			WHERE id = ? AND status = ?
		`, domaincopilot.AgentRunStatusRunning, agentID, now, now, now, item.ID, domaincopilot.AgentRunStatusQueued)
		if result.Error != nil {
			return fmt.Errorf("claim ai agent run update: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return apperrors.ErrNotFound
		}
		item.Status = domaincopilot.AgentRunStatusRunning
		item.ClaimedByAgentID = agentID
		item.StartedAt = &now
		item.LastHeartbeatAt = &now
		item.UpdatedAt = now
		claimed = item
		return nil
	})
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	return claimed, nil
}

func (r *Repository) UpdateAgentRunCallback(ctx context.Context, input domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error) {
	current, err := r.GetAgentRun(ctx, "", input.RunID)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	if strings.TrimSpace(current.CallbackToken) == "" || strings.TrimSpace(current.CallbackToken) != strings.TrimSpace(input.CallbackToken) {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: invalid ai agent callback token", apperrors.ErrAccessDenied)
	}
	if agentRunTerminal(current.Status) {
		return current, nil
	}
	status := normalizeAgentRunStatus(input.Status)
	if status == "" {
		status = domaincopilot.AgentRunStatusRunning
	}
	now := time.Now().UTC()
	output := mergeAgentRunOutput(current.Output, input.Payload)
	toolExecutions := mergeAgentRunToolExecutions(current.ToolExecutions, input.ToolExecutions)
	analysisArtifacts := mergeAgentRunAnalysisArtifacts(current.AnalysisArtifacts, input.AnalysisArtifacts)
	outputBytes, err := json.Marshal(output)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run callback output: %w", err)
	}
	toolBytes, err := json.Marshal(toolExecutions)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run callback tools: %w", err)
	}
	artifactBytes, err := json.Marshal(analysisArtifacts)
	if err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("marshal agent run callback artifacts: %w", err)
	}
	completedAt := current.CompletedAt
	if agentRunTerminal(status) {
		completedAt = &now
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_agent_runs
		SET status = ?, output = ?, tool_executions = ?, analysis_artifacts = ?, claimed_by_agent_id = COALESCE(NULLIF(?, ''), claimed_by_agent_id),
			external_run_id = COALESCE(NULLIF(?, ''), external_run_id), error_message = COALESCE(NULLIF(?, ''), error_message),
			last_heartbeat_at = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND callback_token = ?
	`, status, string(outputBytes), string(toolBytes), string(artifactBytes), strings.TrimSpace(input.AgentID), strings.TrimSpace(input.ExternalRunID), strings.TrimSpace(input.ErrorMessage), now, completedAt, now, strings.TrimSpace(input.RunID), strings.TrimSpace(input.CallbackToken))
	if result.Error != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("update ai agent run callback: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: ai agent run %s", apperrors.ErrNotFound, input.RunID)
	}
	return r.GetAgentRun(ctx, "", input.RunID)
}

func (r *Repository) GetAnalysisProfile(ctx context.Context, profileID string) (domaincopilot.AnalysisProfile, error) {
	return r.getAnalysisProfile(ctx, profileID)
}

func (r *Repository) ListInspectionTasks(ctx context.Context, createdBy string, limit int) ([]domaincopilot.InspectionTask, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, title, scope_type, cluster_id, namespace, checks, enabled, interval_minutes, metadata, created_by, last_run_at, created_at, updated_at
		FROM ai_inspection_tasks
		WHERE created_by = ?
		ORDER BY updated_at DESC, created_at DESC
		LIMIT ?
	`, createdBy, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query inspection tasks: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.InspectionTask, 0, limit)
	for rows.Next() {
		item, err := scanInspectionTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetInspectionTask(ctx context.Context, createdBy, taskID string) (domaincopilot.InspectionTask, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, title, scope_type, cluster_id, namespace, checks, enabled, interval_minutes, metadata, created_by, last_run_at, created_at, updated_at
		FROM ai_inspection_tasks
		WHERE created_by = ? AND id = ?
		LIMIT 1
	`, createdBy, taskID).Row()
	return scanInspectionTaskRow(row)
}

func (r *Repository) ListDueInspectionTasks(ctx context.Context, now time.Time, limit int) ([]domaincopilot.InspectionTask, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, title, scope_type, cluster_id, namespace, checks, enabled, interval_minutes, metadata, created_by, last_run_at, created_at, updated_at
		FROM ai_inspection_tasks
		WHERE enabled = TRUE
		  AND interval_minutes > 0
		ORDER BY updated_at ASC, created_at ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query due inspection tasks: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.InspectionTask, 0, limit)
	for rows.Next() {
		item, err := scanInspectionTask(rows)
		if err != nil {
			return nil, err
		}
		if item.LastRunAt == nil || item.LastRunAt.Add(time.Duration(item.IntervalMinutes)*time.Minute).Before(now) || item.LastRunAt.Add(time.Duration(item.IntervalMinutes)*time.Minute).Equal(now) {
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i].LastRunAt
		right := items[j].LastRunAt
		switch {
		case left == nil && right == nil:
			return items[i].UpdatedAt.Before(items[j].UpdatedAt)
		case left == nil:
			return true
		case right == nil:
			return false
		case !left.Equal(*right):
			return left.Before(*right)
		default:
			return items[i].UpdatedAt.Before(items[j].UpdatedAt)
		}
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *Repository) CreateInspectionTask(ctx context.Context, task domaincopilot.InspectionTask) (domaincopilot.InspectionTask, error) {
	checks, err := json.Marshal(task.Checks)
	if err != nil {
		return domaincopilot.InspectionTask{}, fmt.Errorf("marshal inspection checks: %w", err)
	}
	metadata, err := json.Marshal(task.Metadata)
	if err != nil {
		return domaincopilot.InspectionTask{}, fmt.Errorf("marshal inspection task metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_inspection_tasks (
			id, title, scope_type, cluster_id, namespace, checks, enabled, interval_minutes, metadata, created_by, last_run_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, task.ID, task.Title, task.ScopeType, nullableString(task.ClusterID), nullableString(task.Namespace), string(checks), task.Enabled, task.IntervalMinutes, string(metadata), task.CreatedBy, task.LastRunAt, task.CreatedAt, task.UpdatedAt).Error; err != nil {
		return domaincopilot.InspectionTask{}, fmt.Errorf("create inspection task: %w", err)
	}
	return task, nil
}

func (r *Repository) UpdateInspectionTask(ctx context.Context, createdBy, taskID string, input domaincopilot.InspectionTaskInput) (domaincopilot.InspectionTask, error) {
	checks, err := json.Marshal(input.Checks)
	if err != nil {
		return domaincopilot.InspectionTask{}, fmt.Errorf("marshal inspection checks: %w", err)
	}
	metadata, err := json.Marshal(input.Metadata)
	if err != nil {
		return domaincopilot.InspectionTask{}, fmt.Errorf("marshal inspection task metadata: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_inspection_tasks
		SET title = ?, scope_type = ?, cluster_id = ?, namespace = ?, checks = ?, enabled = ?, interval_minutes = ?, metadata = ?, updated_at = ?
		WHERE created_by = ? AND id = ?
	`, input.Title, input.ScopeType, nullableString(input.ClusterID), nullableString(input.Namespace), string(checks), input.Enabled, input.IntervalMinutes, string(metadata), time.Now().UTC(), createdBy, taskID)
	if result.Error != nil {
		return domaincopilot.InspectionTask{}, fmt.Errorf("update inspection task: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaincopilot.InspectionTask{}, fmt.Errorf("inspection task not found: %s", taskID)
	}
	return r.GetInspectionTask(ctx, createdBy, taskID)
}

func (r *Repository) DeleteInspectionTask(ctx context.Context, createdBy, taskID string) error {
	result := r.db.WithContext(ctx).Exec(`
		DELETE FROM ai_inspection_tasks
		WHERE created_by = ? AND id = ?
	`, createdBy, taskID)
	if result.Error != nil {
		return fmt.Errorf("delete inspection task: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: inspection task %s", apperrors.ErrNotFound, taskID)
	}
	return nil
}

func (r *Repository) TouchInspectionTaskRun(ctx context.Context, taskID string, runAt time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE ai_inspection_tasks
		SET last_run_at = ?, updated_at = ?
		WHERE id = ?
	`, runAt, time.Now().UTC(), taskID).Error
}

func (r *Repository) ListInspectionRuns(ctx context.Context, createdBy string, filter domaincopilot.InspectionRunFilter) ([]domaincopilot.InspectionRun, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT
			r.id, r.task_id, r.triggered_by, r.status, r.severity, r.summary, r.findings, r.report, r.started_at, r.completed_at, r.created_at,
			t.cluster_id, t.namespace, t.checks
		FROM ai_inspection_runs r
		INNER JOIN ai_inspection_tasks t ON t.id = r.task_id
		WHERE t.created_by = ?
	`
	args := []any{createdBy}
	if filter.TaskID != "" {
		query += ` AND r.task_id = ?`
		args = append(args, filter.TaskID)
	}
	query += ` ORDER BY r.created_at DESC`
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query inspection runs: %w", err)
	}
	defer rows.Close()
	items := make([]domaincopilot.InspectionRun, 0, limit)
	for rows.Next() {
		item, runClusterID, runNamespace, runChecks, err := scanInspectionRunWithTask(rows)
		if err != nil {
			return nil, err
		}
		if filter.ClusterID != "" && runClusterID != filter.ClusterID {
			continue
		}
		if filter.Namespace != "" && runNamespace != filter.Namespace {
			continue
		}
		if filter.Check != "" && !containsString(runChecks, filter.Check) {
			continue
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	return items, rows.Err()
}

func (r *Repository) CreateInspectionRun(ctx context.Context, run domaincopilot.InspectionRun) (domaincopilot.InspectionRun, error) {
	findings, err := json.Marshal(run.Findings)
	if err != nil {
		return domaincopilot.InspectionRun{}, fmt.Errorf("marshal inspection findings: %w", err)
	}
	report, err := json.Marshal(run.Report)
	if err != nil {
		return domaincopilot.InspectionRun{}, fmt.Errorf("marshal inspection report: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_inspection_runs (
			id, task_id, triggered_by, status, severity, summary, findings, report, started_at, completed_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.TaskID, run.TriggeredBy, run.Status, run.Severity, run.Summary, string(findings), string(report), run.StartedAt, run.CompletedAt, run.CreatedAt).Error; err != nil {
		return domaincopilot.InspectionRun{}, fmt.Errorf("create inspection run: %w", err)
	}
	return run, nil
}

func scanSession(rows *sql.Rows) (domaincopilot.Session, error) {
	var item domaincopilot.Session
	var metadata []byte
	if err := rows.Scan(&item.ID, &item.Title, &item.CreatedBy, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.Session{}, fmt.Errorf("scan ai session: %w", err)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	return item, nil
}

func scanSessionRow(row *sql.Row) (domaincopilot.Session, error) {
	var item domaincopilot.Session
	var metadata []byte
	if err := row.Scan(&item.ID, &item.Title, &item.CreatedBy, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.Session{}, fmt.Errorf("scan ai session row: %w", err)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	return item, nil
}

func scanMessage(rows *sql.Rows) (domaincopilot.Message, error) {
	var item domaincopilot.Message
	var metadata []byte
	if err := rows.Scan(&item.ID, &item.SessionID, &item.Role, &item.Content, &metadata, &item.CreatedAt); err != nil {
		return domaincopilot.Message{}, fmt.Errorf("scan ai message: %w", err)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	return item, nil
}

func scanDataSource(rows *sql.Rows) (domaincopilot.DataSource, error) {
	var item domaincopilot.DataSource
	var backendType string
	var credentialRef sql.NullString
	var scope []byte
	var queryBudget []byte
	var redactionPolicy []byte
	var config []byte
	var validationStatus sql.NullString
	var validationMessage sql.NullString
	var lastValidatedAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.Name, &item.SourceKind, &backendType, &item.Enabled, &credentialRef, &scope, &queryBudget, &redactionPolicy, &item.MCPAdapter, &config, &validationStatus, &validationMessage, &lastValidatedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("scan data source: %w", err)
	}
	item.BackendType = backendType
	if credentialRef.Valid {
		item.CredentialRef = credentialRef.String
	}
	if len(scope) > 0 {
		_ = json.Unmarshal(scope, &item.Scope)
	}
	if len(queryBudget) > 0 {
		_ = json.Unmarshal(queryBudget, &item.QueryBudget)
	}
	if len(redactionPolicy) > 0 {
		_ = json.Unmarshal(redactionPolicy, &item.RedactionPolicy)
	}
	if len(config) > 0 {
		_ = json.Unmarshal(config, &item.Config)
	}
	if validationStatus.Valid {
		item.ValidationStatus = validationStatus.String
	}
	if validationMessage.Valid {
		item.ValidationMessage = validationMessage.String
	}
	if lastValidatedAt.Valid {
		value := lastValidatedAt.Time
		item.LastValidatedAt = &value
	}
	return item, nil
}

func scanDataSourceRow(row *sql.Row) (domaincopilot.DataSource, error) {
	var item domaincopilot.DataSource
	var backendType string
	var credentialRef sql.NullString
	var scope []byte
	var queryBudget []byte
	var redactionPolicy []byte
	var config []byte
	var validationStatus sql.NullString
	var validationMessage sql.NullString
	var lastValidatedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Name, &item.SourceKind, &backendType, &item.Enabled, &credentialRef, &scope, &queryBudget, &redactionPolicy, &item.MCPAdapter, &config, &validationStatus, &validationMessage, &lastValidatedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.DataSource{}, fmt.Errorf("scan data source: %w", err)
	}
	item.BackendType = backendType
	if credentialRef.Valid {
		item.CredentialRef = credentialRef.String
	}
	if len(scope) > 0 {
		_ = json.Unmarshal(scope, &item.Scope)
	}
	if len(queryBudget) > 0 {
		_ = json.Unmarshal(queryBudget, &item.QueryBudget)
	}
	if len(redactionPolicy) > 0 {
		_ = json.Unmarshal(redactionPolicy, &item.RedactionPolicy)
	}
	if len(config) > 0 {
		_ = json.Unmarshal(config, &item.Config)
	}
	if validationStatus.Valid {
		item.ValidationStatus = validationStatus.String
	}
	if validationMessage.Valid {
		item.ValidationMessage = validationMessage.String
	}
	if lastValidatedAt.Valid {
		value := lastValidatedAt.Time
		item.LastValidatedAt = &value
	}
	return item, nil
}

func scanAnalysisProfile(rows *sql.Rows) (domaincopilot.AnalysisProfile, error) {
	var item domaincopilot.AnalysisProfile
	var enabledSources []byte
	var enabledPlaybooks []byte
	var queryBudgets []byte
	var outputStyle []byte
	if err := rows.Scan(&item.ID, &item.Name, &item.Mode, &enabledSources, &enabledPlaybooks, &queryBudgets, &outputStyle, &item.RemediationPolicy, &item.DefaultTimeRangeMinutes, &item.TimeoutSeconds, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("scan analysis profile: %w", err)
	}
	if len(enabledSources) > 0 {
		_ = json.Unmarshal(enabledSources, &item.EnabledSources)
	}
	if len(enabledPlaybooks) > 0 {
		_ = json.Unmarshal(enabledPlaybooks, &item.EnabledPlaybooks)
	}
	if len(queryBudgets) > 0 {
		_ = json.Unmarshal(queryBudgets, &item.QueryBudgets)
	}
	if len(outputStyle) > 0 {
		_ = json.Unmarshal(outputStyle, &item.OutputStyle)
	}
	return item, nil
}

func scanAnalysisProfileRow(row *sql.Row) (domaincopilot.AnalysisProfile, error) {
	var item domaincopilot.AnalysisProfile
	var enabledSources []byte
	var enabledPlaybooks []byte
	var queryBudgets []byte
	var outputStyle []byte
	if err := row.Scan(&item.ID, &item.Name, &item.Mode, &enabledSources, &enabledPlaybooks, &queryBudgets, &outputStyle, &item.RemediationPolicy, &item.DefaultTimeRangeMinutes, &item.TimeoutSeconds, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.AnalysisProfile{}, fmt.Errorf("scan analysis profile: %w", err)
	}
	if len(enabledSources) > 0 {
		_ = json.Unmarshal(enabledSources, &item.EnabledSources)
	}
	if len(enabledPlaybooks) > 0 {
		_ = json.Unmarshal(enabledPlaybooks, &item.EnabledPlaybooks)
	}
	if len(queryBudgets) > 0 {
		_ = json.Unmarshal(queryBudgets, &item.QueryBudgets)
	}
	if len(outputStyle) > 0 {
		_ = json.Unmarshal(outputStyle, &item.OutputStyle)
	}
	return item, nil
}

func scanAutomationPolicy(rows *sql.Rows) (domaincopilot.AutomationPolicy, error) {
	var item domaincopilot.AutomationPolicy
	var triggerConditions []byte
	var approvalPolicy []byte
	var analysisKinds []byte
	var agentProviderID sql.NullString
	if err := rows.Scan(&item.ID, &item.Name, &item.Enabled, &item.TriggerType, &analysisKinds, &agentProviderID, &triggerConditions, &item.DedupWindowSeconds, &item.AnalysisProfileID, &item.RemediationPolicy, &approvalPolicy, &item.CooldownSeconds, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("scan automation policy: %w", err)
	}
	item.AgentProviderID = "internal"
	if agentProviderID.Valid && strings.TrimSpace(agentProviderID.String) != "" {
		item.AgentProviderID = agentProviderID.String
	}
	if len(analysisKinds) > 0 {
		_ = json.Unmarshal(analysisKinds, &item.AnalysisKinds)
	}
	if len(triggerConditions) > 0 {
		_ = json.Unmarshal(triggerConditions, &item.TriggerConditions)
	}
	if len(approvalPolicy) > 0 {
		_ = json.Unmarshal(approvalPolicy, &item.ApprovalPolicy)
	}
	return item, nil
}

func scanAutomationPolicyRow(row *sql.Row) (domaincopilot.AutomationPolicy, error) {
	var item domaincopilot.AutomationPolicy
	var triggerConditions []byte
	var approvalPolicy []byte
	var analysisKinds []byte
	var agentProviderID sql.NullString
	if err := row.Scan(&item.ID, &item.Name, &item.Enabled, &item.TriggerType, &analysisKinds, &agentProviderID, &triggerConditions, &item.DedupWindowSeconds, &item.AnalysisProfileID, &item.RemediationPolicy, &approvalPolicy, &item.CooldownSeconds, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.AutomationPolicy{}, fmt.Errorf("scan automation policy: %w", err)
	}
	item.AgentProviderID = "internal"
	if agentProviderID.Valid && strings.TrimSpace(agentProviderID.String) != "" {
		item.AgentProviderID = agentProviderID.String
	}
	if len(analysisKinds) > 0 {
		_ = json.Unmarshal(analysisKinds, &item.AnalysisKinds)
	}
	if len(triggerConditions) > 0 {
		_ = json.Unmarshal(triggerConditions, &item.TriggerConditions)
	}
	if len(approvalPolicy) > 0 {
		_ = json.Unmarshal(approvalPolicy, &item.ApprovalPolicy)
	}
	return item, nil
}

func (r *Repository) getAnalysisProfile(ctx context.Context, profileID string) (domaincopilot.AnalysisProfile, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, mode, enabled_sources, enabled_playbooks, query_budgets, output_style, remediation_policy, default_time_range_minutes, timeout_seconds, enabled, created_at, updated_at
		FROM ai_analysis_profiles
		WHERE id = ?
		LIMIT 1
	`, profileID).Row()
	return scanAnalysisProfileRow(row)
}

func (r *Repository) getAutomationPolicy(ctx context.Context, policyID string) (domaincopilot.AutomationPolicy, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, enabled, trigger_type, analysis_kinds, agent_provider_id, trigger_conditions, dedup_window_seconds, analysis_profile_id, remediation_policy, approval_policy, cooldown_seconds, created_at, updated_at
		FROM ai_automation_policies
		WHERE id = ?
		LIMIT 1
	`, policyID).Row()
	return scanAutomationPolicyRow(row)
}

func scanRootCauseRun(rows *sql.Rows) (domaincopilot.RootCauseRun, error) {
	var item domaincopilot.RootCauseRun
	var kind string
	var sessionID sql.NullString
	var analysisProfileID sql.NullString
	var triggerType sql.NullString
	var clusterID sql.NullString
	var namespace sql.NullString
	var workloadKind sql.NullString
	var workloadName sql.NullString
	var alertID sql.NullString
	var question sql.NullString
	var evidence []byte
	var hypotheses []byte
	var recommendations []byte
	var toolExecutions []byte
	var dataSourceSnapshot []byte
	var playbookResults []byte
	var remediationPlan []byte
	var dedupKey sql.NullString
	if err := rows.Scan(
		&item.ID,
		&kind,
		&sessionID,
		&item.Title,
		&item.CreatedBy,
		&analysisProfileID,
		&triggerType,
		&item.Status,
		&item.Severity,
		&item.Summary,
		&clusterID,
		&namespace,
		&workloadKind,
		&workloadName,
		&alertID,
		&item.TimeRangeMinutes,
		&question,
		&evidence,
		&hypotheses,
		&recommendations,
		&toolExecutions,
		&dataSourceSnapshot,
		&playbookResults,
		&remediationPlan,
		&dedupKey,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("scan root cause run: %w", err)
	}
	item.Kind = kind
	if sessionID.Valid {
		item.SessionID = sessionID.String
	}
	if analysisProfileID.Valid {
		item.AnalysisProfileID = analysisProfileID.String
	}
	if triggerType.Valid {
		item.TriggerType = triggerType.String
	}
	if clusterID.Valid {
		item.ClusterID = clusterID.String
	}
	if namespace.Valid {
		item.Namespace = namespace.String
	}
	if workloadKind.Valid {
		item.WorkloadKind = workloadKind.String
	}
	if workloadName.Valid {
		item.WorkloadName = workloadName.String
	}
	if alertID.Valid {
		item.AlertID = alertID.String
	}
	if question.Valid {
		item.Question = question.String
	}
	if dedupKey.Valid {
		item.DedupKey = dedupKey.String
	}
	if len(evidence) > 0 {
		_ = json.Unmarshal(evidence, &item.Evidence)
	}
	if len(hypotheses) > 0 {
		_ = json.Unmarshal(hypotheses, &item.Hypotheses)
	}
	if len(recommendations) > 0 {
		_ = json.Unmarshal(recommendations, &item.Recommendations)
	}
	if len(toolExecutions) > 0 {
		_ = json.Unmarshal(toolExecutions, &item.ToolExecutions)
	}
	if len(dataSourceSnapshot) > 0 {
		_ = json.Unmarshal(dataSourceSnapshot, &item.DataSourceSnapshot)
	}
	if len(playbookResults) > 0 {
		_ = json.Unmarshal(playbookResults, &item.PlaybookResults)
	}
	if len(remediationPlan) > 0 {
		_ = json.Unmarshal(remediationPlan, &item.RemediationPlan)
	}
	return item, nil
}

func scanRootCauseRunRow(row *sql.Row) (domaincopilot.RootCauseRun, error) {
	var item domaincopilot.RootCauseRun
	var kind string
	var sessionID sql.NullString
	var analysisProfileID sql.NullString
	var triggerType sql.NullString
	var clusterID sql.NullString
	var namespace sql.NullString
	var workloadKind sql.NullString
	var workloadName sql.NullString
	var alertID sql.NullString
	var question sql.NullString
	var evidence []byte
	var hypotheses []byte
	var recommendations []byte
	var toolExecutions []byte
	var dataSourceSnapshot []byte
	var playbookResults []byte
	var remediationPlan []byte
	var dedupKey sql.NullString
	if err := row.Scan(
		&item.ID,
		&kind,
		&sessionID,
		&item.Title,
		&item.CreatedBy,
		&analysisProfileID,
		&triggerType,
		&item.Status,
		&item.Severity,
		&item.Summary,
		&clusterID,
		&namespace,
		&workloadKind,
		&workloadName,
		&alertID,
		&item.TimeRangeMinutes,
		&question,
		&evidence,
		&hypotheses,
		&recommendations,
		&toolExecutions,
		&dataSourceSnapshot,
		&playbookResults,
		&remediationPlan,
		&dedupKey,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("scan root cause run: %w", err)
	}
	item.Kind = kind
	if sessionID.Valid {
		item.SessionID = sessionID.String
	}
	if analysisProfileID.Valid {
		item.AnalysisProfileID = analysisProfileID.String
	}
	if triggerType.Valid {
		item.TriggerType = triggerType.String
	}
	if clusterID.Valid {
		item.ClusterID = clusterID.String
	}
	if namespace.Valid {
		item.Namespace = namespace.String
	}
	if workloadKind.Valid {
		item.WorkloadKind = workloadKind.String
	}
	if workloadName.Valid {
		item.WorkloadName = workloadName.String
	}
	if alertID.Valid {
		item.AlertID = alertID.String
	}
	if question.Valid {
		item.Question = question.String
	}
	if dedupKey.Valid {
		item.DedupKey = dedupKey.String
	}
	if len(evidence) > 0 {
		_ = json.Unmarshal(evidence, &item.Evidence)
	}
	if len(hypotheses) > 0 {
		_ = json.Unmarshal(hypotheses, &item.Hypotheses)
	}
	if len(recommendations) > 0 {
		_ = json.Unmarshal(recommendations, &item.Recommendations)
	}
	if len(toolExecutions) > 0 {
		_ = json.Unmarshal(toolExecutions, &item.ToolExecutions)
	}
	if len(dataSourceSnapshot) > 0 {
		_ = json.Unmarshal(dataSourceSnapshot, &item.DataSourceSnapshot)
	}
	if len(playbookResults) > 0 {
		_ = json.Unmarshal(playbookResults, &item.PlaybookResults)
	}
	if len(remediationPlan) > 0 {
		_ = json.Unmarshal(remediationPlan, &item.RemediationPlan)
	}
	return item, nil
}

func agentRunSelect() string {
	return `
		SELECT id, provider_id, provider_kind, capability_id, skill_ids, session_id, root_cause_run_id, created_by, status, scope, toolset, tool_bindings, skill_bindings, input, output,
			tool_executions, analysis_artifacts, callback_token, claimed_by_agent_id, external_run_id, error_message, timeout_seconds,
			queued_at, started_at, last_heartbeat_at, completed_at, created_at, updated_at
		FROM ai_agent_runs
	`
}

func scanAgentRun(rows *sql.Rows) (domaincopilot.AgentRun, error) {
	var item domaincopilot.AgentRun
	var skillIDs []byte
	var sessionID sql.NullString
	var rootCauseRunID sql.NullString
	var scope []byte
	var toolset []byte
	var toolBindings []byte
	var skillBindings []byte
	var input []byte
	var output []byte
	var toolExecutions []byte
	var analysisArtifacts []byte
	var claimedByAgentID sql.NullString
	var externalRunID sql.NullString
	var errorMessage sql.NullString
	var startedAt sql.NullTime
	var lastHeartbeatAt sql.NullTime
	var completedAt sql.NullTime
	if err := rows.Scan(
		&item.ID,
		&item.ProviderID,
		&item.ProviderKind,
		&item.CapabilityID,
		&skillIDs,
		&sessionID,
		&rootCauseRunID,
		&item.CreatedBy,
		&item.Status,
		&scope,
		&toolset,
		&toolBindings,
		&skillBindings,
		&input,
		&output,
		&toolExecutions,
		&analysisArtifacts,
		&item.CallbackToken,
		&claimedByAgentID,
		&externalRunID,
		&errorMessage,
		&item.TimeoutSeconds,
		&item.QueuedAt,
		&startedAt,
		&lastHeartbeatAt,
		&completedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("scan ai agent run: %w", err)
	}
	decodeAgentRunFields(&item, skillIDs, sessionID, rootCauseRunID, scope, toolset, toolBindings, skillBindings, input, output, toolExecutions, analysisArtifacts, claimedByAgentID, externalRunID, errorMessage, startedAt, lastHeartbeatAt, completedAt)
	return item, nil
}

func scanAgentRunRow(row *sql.Row) (domaincopilot.AgentRun, error) {
	var item domaincopilot.AgentRun
	var skillIDs []byte
	var sessionID sql.NullString
	var rootCauseRunID sql.NullString
	var scope []byte
	var toolset []byte
	var toolBindings []byte
	var skillBindings []byte
	var input []byte
	var output []byte
	var toolExecutions []byte
	var analysisArtifacts []byte
	var claimedByAgentID sql.NullString
	var externalRunID sql.NullString
	var errorMessage sql.NullString
	var startedAt sql.NullTime
	var lastHeartbeatAt sql.NullTime
	var completedAt sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.ProviderID,
		&item.ProviderKind,
		&item.CapabilityID,
		&skillIDs,
		&sessionID,
		&rootCauseRunID,
		&item.CreatedBy,
		&item.Status,
		&scope,
		&toolset,
		&toolBindings,
		&skillBindings,
		&input,
		&output,
		&toolExecutions,
		&analysisArtifacts,
		&item.CallbackToken,
		&claimedByAgentID,
		&externalRunID,
		&errorMessage,
		&item.TimeoutSeconds,
		&item.QueuedAt,
		&startedAt,
		&lastHeartbeatAt,
		&completedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("scan ai agent run row: %w", err)
	}
	decodeAgentRunFields(&item, skillIDs, sessionID, rootCauseRunID, scope, toolset, toolBindings, skillBindings, input, output, toolExecutions, analysisArtifacts, claimedByAgentID, externalRunID, errorMessage, startedAt, lastHeartbeatAt, completedAt)
	return item, nil
}

func decodeAgentRunFields(item *domaincopilot.AgentRun, skillIDs []byte, sessionID sql.NullString, rootCauseRunID sql.NullString, scope []byte, toolset []byte, toolBindings []byte, skillBindings []byte, input []byte, output []byte, toolExecutions []byte, analysisArtifacts []byte, claimedByAgentID sql.NullString, externalRunID sql.NullString, errorMessage sql.NullString, startedAt sql.NullTime, lastHeartbeatAt sql.NullTime, completedAt sql.NullTime) {
	if len(skillIDs) > 0 {
		_ = json.Unmarshal(skillIDs, &item.SkillIDs)
	}
	if sessionID.Valid {
		item.SessionID = sessionID.String
	}
	if rootCauseRunID.Valid {
		item.RootCauseRunID = rootCauseRunID.String
	}
	if len(scope) > 0 {
		_ = json.Unmarshal(scope, &item.Scope)
	}
	if len(toolset) > 0 {
		_ = json.Unmarshal(toolset, &item.Toolset)
	}
	if len(toolBindings) > 0 {
		_ = json.Unmarshal(toolBindings, &item.ToolBindings)
	}
	if len(skillBindings) > 0 {
		_ = json.Unmarshal(skillBindings, &item.SkillBindings)
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &item.Input)
	}
	if len(output) > 0 {
		_ = json.Unmarshal(output, &item.Output)
	}
	if len(toolExecutions) > 0 {
		_ = json.Unmarshal(toolExecutions, &item.ToolExecutions)
	}
	if len(analysisArtifacts) > 0 {
		_ = json.Unmarshal(analysisArtifacts, &item.AnalysisArtifacts)
	}
	if claimedByAgentID.Valid {
		item.ClaimedByAgentID = claimedByAgentID.String
	}
	if externalRunID.Valid {
		item.ExternalRunID = externalRunID.String
	}
	if errorMessage.Valid {
		item.ErrorMessage = errorMessage.String
	}
	if startedAt.Valid {
		value := startedAt.Time
		item.StartedAt = &value
	}
	if lastHeartbeatAt.Valid {
		value := lastHeartbeatAt.Time
		item.LastHeartbeatAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time
		item.CompletedAt = &value
	}
}

func scanInspectionTask(rows *sql.Rows) (domaincopilot.InspectionTask, error) {
	var item domaincopilot.InspectionTask
	var checks []byte
	var metadata []byte
	var clusterID sql.NullString
	var namespace sql.NullString
	var lastRunAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.Title, &item.ScopeType, &clusterID, &namespace, &checks, &item.Enabled, &item.IntervalMinutes, &metadata, &item.CreatedBy, &lastRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.InspectionTask{}, fmt.Errorf("scan inspection task: %w", err)
	}
	if clusterID.Valid {
		item.ClusterID = clusterID.String
	}
	if namespace.Valid {
		item.Namespace = namespace.String
	}
	if len(checks) > 0 {
		_ = json.Unmarshal(checks, &item.Checks)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	if lastRunAt.Valid {
		value := lastRunAt.Time
		item.LastRunAt = &value
	}
	return item, nil
}

func scanInspectionTaskRow(row *sql.Row) (domaincopilot.InspectionTask, error) {
	var item domaincopilot.InspectionTask
	var checks []byte
	var metadata []byte
	var clusterID sql.NullString
	var namespace sql.NullString
	var lastRunAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Title, &item.ScopeType, &clusterID, &namespace, &checks, &item.Enabled, &item.IntervalMinutes, &metadata, &item.CreatedBy, &lastRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaincopilot.InspectionTask{}, fmt.Errorf("scan inspection task row: %w", err)
	}
	if clusterID.Valid {
		item.ClusterID = clusterID.String
	}
	if namespace.Valid {
		item.Namespace = namespace.String
	}
	if len(checks) > 0 {
		_ = json.Unmarshal(checks, &item.Checks)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	if lastRunAt.Valid {
		value := lastRunAt.Time
		item.LastRunAt = &value
	}
	return item, nil
}

func scanInspectionRun(rows *sql.Rows) (domaincopilot.InspectionRun, error) {
	var item domaincopilot.InspectionRun
	var findings []byte
	var report []byte
	var completedAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.TaskID, &item.TriggeredBy, &item.Status, &item.Severity, &item.Summary, &findings, &report, &item.StartedAt, &completedAt, &item.CreatedAt); err != nil {
		return domaincopilot.InspectionRun{}, fmt.Errorf("scan inspection run: %w", err)
	}
	if len(findings) > 0 {
		_ = json.Unmarshal(findings, &item.Findings)
	}
	if len(report) > 0 {
		_ = json.Unmarshal(report, &item.Report)
	}
	if completedAt.Valid {
		value := completedAt.Time
		item.CompletedAt = &value
	}
	return item, nil
}

func scanInspectionRunWithTask(rows *sql.Rows) (domaincopilot.InspectionRun, string, string, []string, error) {
	var item domaincopilot.InspectionRun
	var findings []byte
	var report []byte
	var completedAt sql.NullTime
	var clusterID sql.NullString
	var namespace sql.NullString
	var checks []byte
	if err := rows.Scan(
		&item.ID,
		&item.TaskID,
		&item.TriggeredBy,
		&item.Status,
		&item.Severity,
		&item.Summary,
		&findings,
		&report,
		&item.StartedAt,
		&completedAt,
		&item.CreatedAt,
		&clusterID,
		&namespace,
		&checks,
	); err != nil {
		return domaincopilot.InspectionRun{}, "", "", nil, fmt.Errorf("scan inspection run with task: %w", err)
	}
	if len(findings) > 0 {
		_ = json.Unmarshal(findings, &item.Findings)
	}
	if len(report) > 0 {
		_ = json.Unmarshal(report, &item.Report)
	}
	if completedAt.Valid {
		value := completedAt.Time
		item.CompletedAt = &value
	}
	runChecks := []string{}
	if len(checks) > 0 {
		_ = json.Unmarshal(checks, &runChecks)
	}
	return item, clusterID.String, namespace.String, runChecks, nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func compactStringValues(items []string) []string {
	values := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	items := make([]string, count)
	for i := range items {
		items[i] = "?"
	}
	return strings.Join(items, ",")
}

func normalizeAgentRunStatus(status string) string {
	switch strings.TrimSpace(status) {
	case domaincopilot.AgentRunStatusQueued:
		return domaincopilot.AgentRunStatusQueued
	case domaincopilot.AgentRunStatusRunning, "":
		return domaincopilot.AgentRunStatusRunning
	case domaincopilot.AgentRunStatusCompleted, "succeeded", "success":
		return domaincopilot.AgentRunStatusCompleted
	case domaincopilot.AgentRunStatusFailed, "error":
		return domaincopilot.AgentRunStatusFailed
	case domaincopilot.AgentRunStatusCanceled, "cancelled":
		return domaincopilot.AgentRunStatusCanceled
	case domaincopilot.AgentRunStatusCallbackTimeout:
		return domaincopilot.AgentRunStatusCallbackTimeout
	default:
		return domaincopilot.AgentRunStatusFailed
	}
}

func agentRunTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case domaincopilot.AgentRunStatusCompleted, domaincopilot.AgentRunStatusFailed, domaincopilot.AgentRunStatusCanceled, domaincopilot.AgentRunStatusCallbackTimeout:
		return true
	default:
		return false
	}
}

func mergeAgentRunOutput(current map[string]any, patch map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range current {
		merged[key] = value
	}
	for key, value := range patch {
		merged[key] = value
	}
	return merged
}

func mergeAgentRunToolExecutions(current []domaincopilot.ToolExecution, patch []domaincopilot.ToolExecution) []domaincopilot.ToolExecution {
	if len(current) == 0 {
		return append([]domaincopilot.ToolExecution(nil), patch...)
	}
	if len(patch) == 0 {
		return append([]domaincopilot.ToolExecution(nil), current...)
	}
	merged := append([]domaincopilot.ToolExecution(nil), current...)
	indexByID := map[string]int{}
	for index, item := range merged {
		if trimmed := strings.TrimSpace(item.ID); trimmed != "" {
			indexByID[trimmed] = index
		}
	}
	for _, item := range patch {
		id := strings.TrimSpace(item.ID)
		if id != "" {
			if index, ok := indexByID[id]; ok {
				merged[index] = item
				continue
			}
			indexByID[id] = len(merged)
		}
		merged = append(merged, item)
	}
	return merged
}

func mergeAgentRunAnalysisArtifacts(current []domaincopilot.AnalysisArtifact, patch []domaincopilot.AnalysisArtifact) []domaincopilot.AnalysisArtifact {
	if len(current) == 0 {
		return append([]domaincopilot.AnalysisArtifact(nil), patch...)
	}
	if len(patch) == 0 {
		return append([]domaincopilot.AnalysisArtifact(nil), current...)
	}
	merged := append([]domaincopilot.AnalysisArtifact(nil), current...)
	indexByKey := map[string]int{}
	for index, item := range merged {
		if key := agentRunAnalysisArtifactKey(item); key != "" {
			indexByKey[key] = index
		}
	}
	for _, item := range patch {
		key := agentRunAnalysisArtifactKey(item)
		if key != "" {
			if index, ok := indexByKey[key]; ok {
				merged[index] = item
				continue
			}
			indexByKey[key] = len(merged)
		}
		merged = append(merged, item)
	}
	return merged
}

func agentRunAnalysisArtifactKey(item domaincopilot.AnalysisArtifact) string {
	runID := strings.TrimSpace(item.RunID)
	kind := strings.TrimSpace(item.Kind)
	title := strings.TrimSpace(item.Title)
	if kind == "" && title == "" {
		return ""
	}
	if runID != "" {
		return strings.Join([]string{runID, kind, title}, "\x00")
	}
	return strings.Join([]string{kind, title}, "\x00")
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func escapeSQLLikePattern(value string) string {
	value = strings.ReplaceAll(value, "!", "!!")
	value = strings.ReplaceAll(value, "%", "!%")
	value = strings.ReplaceAll(value, "_", "!_")
	return value
}
