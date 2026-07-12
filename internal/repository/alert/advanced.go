package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	"gorm.io/gorm"
)

func (r *Repository) ListRules(ctx context.Context) ([]domainalert.AlertRule, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, rule_type, datasource_selector, query_spec, threshold_spec, for_seconds, group_by,
			labels, annotations, notification_policy_id, healing_policy_ids, enabled, created_at, updated_at
		FROM alert_rules
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query alert rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.AlertRule, 0)
	for rows.Next() {
		item, err := scanAlertRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetRule(ctx context.Context, ruleID string) (domainalert.AlertRule, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, rule_type, datasource_selector, query_spec, threshold_spec, for_seconds, group_by,
			labels, annotations, notification_policy_id, healing_policy_ids, enabled, created_at, updated_at
		FROM alert_rules
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(ruleID)).Row()
	return scanAlertRuleRow(row, ruleID)
}

func (r *Repository) CreateRule(ctx context.Context, input domainalert.AlertRuleInput) (domainalert.AlertRule, error) {
	item := normalizeAlertRuleInput(input, time.Now().UTC())
	datasourceSelector, err := json.Marshal(item.DatasourceSelector)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule datasource selector: %w", err)
	}
	querySpec, err := json.Marshal(item.QuerySpec)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule query spec: %w", err)
	}
	thresholdSpec, err := json.Marshal(item.ThresholdSpec)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule threshold spec: %w", err)
	}
	groupBy, err := json.Marshal(item.GroupBy)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule groupBy: %w", err)
	}
	labels, err := json.Marshal(item.Labels)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule labels: %w", err)
	}
	annotations, err := json.Marshal(item.Annotations)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule annotations: %w", err)
	}
	healingPolicyIDs, err := json.Marshal(item.HealingPolicyIDs)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule healing policy ids: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO alert_rules (
			id, name, rule_type, datasource_selector, query_spec, threshold_spec, for_seconds, group_by,
			labels, annotations, notification_policy_id, healing_policy_ids, enabled, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.RuleType, string(datasourceSelector), string(querySpec), string(thresholdSpec), item.ForSeconds, string(groupBy),
		string(labels), string(annotations), nullableString(item.NotificationPolicyID), string(healingPolicyIDs), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("create alert rule: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateRule(ctx context.Context, ruleID string, input domainalert.AlertRuleInput) (domainalert.AlertRule, error) {
	item := normalizeAlertRuleInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(ruleID)
	datasourceSelector, err := json.Marshal(item.DatasourceSelector)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule datasource selector: %w", err)
	}
	querySpec, err := json.Marshal(item.QuerySpec)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule query spec: %w", err)
	}
	thresholdSpec, err := json.Marshal(item.ThresholdSpec)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule threshold spec: %w", err)
	}
	groupBy, err := json.Marshal(item.GroupBy)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule groupBy: %w", err)
	}
	labels, err := json.Marshal(item.Labels)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule labels: %w", err)
	}
	annotations, err := json.Marshal(item.Annotations)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule annotations: %w", err)
	}
	healingPolicyIDs, err := json.Marshal(item.HealingPolicyIDs)
	if err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("marshal alert rule healing policy ids: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE alert_rules
		SET name = ?, rule_type = ?, datasource_selector = ?, query_spec = ?, threshold_spec = ?, for_seconds = ?, group_by = ?,
			labels = ?, annotations = ?, notification_policy_id = ?, healing_policy_ids = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, item.RuleType, string(datasourceSelector), string(querySpec), string(thresholdSpec), item.ForSeconds, string(groupBy),
		string(labels), string(annotations), nullableString(item.NotificationPolicyID), string(healingPolicyIDs), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.AlertRule{}, fmt.Errorf("update alert rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.AlertRule{}, alertNotFound("alert rule", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "alert_rules", item.ID)
	return item, nil
}

func (r *Repository) ListRuleRuns(ctx context.Context, filter domainalert.AlertRuleRunFilter) ([]domainalert.AlertRuleRun, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	args := []any{}
	conditions := []string{}
	if strings.TrimSpace(filter.RuleID) != "" {
		conditions = append(conditions, "rule_id = ?")
		args = append(args, strings.TrimSpace(filter.RuleID))
	}
	query := `
		SELECT id, rule_id, status, summary, matched, duration_ms, error, result, created_at, updated_at
		FROM alert_rule_runs
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query alert rule runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainalert.AlertRuleRun, 0, limit)
	for rows.Next() {
		item, err := scanAlertRuleRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateRuleRun(ctx context.Context, input domainalert.AlertRuleRunInput) (domainalert.AlertRuleRun, error) {
	item := normalizeAlertRuleRunInput(input, time.Now().UTC())
	result, err := json.Marshal(item.Result)
	if err != nil {
		return domainalert.AlertRuleRun{}, fmt.Errorf("marshal alert rule run result: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO alert_rule_runs (id, rule_id, status, summary, matched, duration_ms, error, result, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.RuleID, item.Status, nullableString(item.Summary), item.Matched, item.DurationMs, nullableString(item.Error), string(result), item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.AlertRuleRun{}, fmt.Errorf("create alert rule run: %w", err)
	}
	return item, nil
}

func (r *Repository) ListEvents(ctx context.Context, filter domainalert.AlertEventFilter) ([]domainalert.AlertEvent, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args := []any{}
	conditions := []string{}
	if strings.TrimSpace(filter.Status) != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Status)))
	}
	if strings.TrimSpace(filter.RuleID) != "" {
		conditions = append(conditions, "rule_id = ?")
		args = append(args, strings.TrimSpace(filter.RuleID))
	}
	if strings.TrimSpace(filter.ClusterID) != "" {
		conditions = append(conditions, "cluster_id = ?")
		args = append(args, strings.TrimSpace(filter.ClusterID))
	}
	query := `
		SELECT id, rule_id, source_type, source_system, fingerprint, title, summary, severity, status, cluster_id, namespace,
			labels, annotations, receiver, generator_url, current_state, last_notification_at, starts_at, ends_at, last_seen_at, created_at, updated_at
		FROM alert_events
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY last_seen_at DESC, updated_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query alert events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.AlertEvent, 0, limit)
	for rows.Next() {
		item, err := scanAlertEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetEvent(ctx context.Context, eventID string) (domainalert.AlertEvent, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, rule_id, source_type, source_system, fingerprint, title, summary, severity, status, cluster_id, namespace,
			labels, annotations, receiver, generator_url, current_state, last_notification_at, starts_at, ends_at, last_seen_at, created_at, updated_at
		FROM alert_events
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(eventID)).Row()
	return scanAlertEventRow(row, eventID)
}

func (r *Repository) CreateEvent(ctx context.Context, input domainalert.AlertEventInput) (domainalert.AlertEvent, error) {
	item := normalizeAlertEventInput(input, time.Now().UTC())
	labels, err := json.Marshal(item.Labels)
	if err != nil {
		return domainalert.AlertEvent{}, fmt.Errorf("marshal alert event labels: %w", err)
	}
	annotations, err := json.Marshal(item.Annotations)
	if err != nil {
		return domainalert.AlertEvent{}, fmt.Errorf("marshal alert event annotations: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO alert_events (
			id, rule_id, source_type, source_system, fingerprint, title, summary, severity, status, cluster_id, namespace,
			labels, annotations, receiver, generator_url, current_state, last_notification_at, starts_at, ends_at, last_seen_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			rule_id = EXCLUDED.rule_id,
			source_type = EXCLUDED.source_type,
			source_system = EXCLUDED.source_system,
			fingerprint = EXCLUDED.fingerprint,
			title = EXCLUDED.title,
			summary = EXCLUDED.summary,
			severity = EXCLUDED.severity,
			status = EXCLUDED.status,
			cluster_id = EXCLUDED.cluster_id,
			namespace = EXCLUDED.namespace,
			labels = EXCLUDED.labels,
			annotations = (
				EXCLUDED.annotations::jsonb ||
				jsonb_strip_nulls(jsonb_build_object(
					'ownerTeam', alert_events.annotations::jsonb->>'ownerTeam',
					'assignee', alert_events.annotations::jsonb->>'assignee',
					'acknowledgedAt', alert_events.annotations::jsonb->>'acknowledgedAt',
					'acknowledgedBy', alert_events.annotations::jsonb->>'acknowledgedBy',
					'acknowledgedByName', alert_events.annotations::jsonb->>'acknowledgedByName'
				))
			)::json,
			receiver = EXCLUDED.receiver,
			generator_url = EXCLUDED.generator_url,
			current_state = CASE
				WHEN EXCLUDED.status = 'resolved' THEN 'resolved'
				WHEN COALESCE(NULLIF(alert_events.current_state, ''), alert_events.status) = 'acknowledged' AND EXCLUDED.status = 'firing' THEN 'acknowledged'
				ELSE EXCLUDED.current_state
			END,
			last_notification_at = EXCLUDED.last_notification_at,
			starts_at = EXCLUDED.starts_at,
			ends_at = EXCLUDED.ends_at,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = EXCLUDED.updated_at
	`, item.ID, nullableString(item.RuleID), item.SourceType, nullableString(item.SourceSystem), item.Fingerprint, item.Title, item.Summary, item.Severity, item.Status,
		nullableString(item.ClusterID), nullableString(item.Namespace), string(labels), string(annotations), nullableString(item.Receiver), nullableString(item.GeneratorURL),
		nullableString(item.CurrentState), nullableTime(item.LastNotificationAt), nullableTime(item.StartsAt), nullableTime(item.EndsAt), nullableTime(item.LastSeenAt),
		item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.AlertEvent{}, fmt.Errorf("create alert event: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateEvent(ctx context.Context, eventID string, input domainalert.AlertEventInput) (domainalert.AlertEvent, error) {
	item := normalizeAlertEventInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(eventID)
	labels, err := json.Marshal(item.Labels)
	if err != nil {
		return domainalert.AlertEvent{}, fmt.Errorf("marshal alert event labels: %w", err)
	}
	annotations, err := json.Marshal(item.Annotations)
	if err != nil {
		return domainalert.AlertEvent{}, fmt.Errorf("marshal alert event annotations: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE alert_events
		SET rule_id = ?, source_type = ?, source_system = ?, fingerprint = ?, title = ?, summary = ?, severity = ?, status = ?, cluster_id = ?, namespace = ?,
			labels = ?, annotations = ?, receiver = ?, generator_url = ?, current_state = ?, last_notification_at = ?, starts_at = ?, ends_at = ?, last_seen_at = ?, updated_at = ?
		WHERE id = ?
	`, nullableString(item.RuleID), item.SourceType, nullableString(item.SourceSystem), item.Fingerprint, item.Title, item.Summary, item.Severity, item.Status,
		nullableString(item.ClusterID), nullableString(item.Namespace), string(labels), string(annotations), nullableString(item.Receiver), nullableString(item.GeneratorURL),
		nullableString(item.CurrentState), nullableTime(item.LastNotificationAt), nullableTime(item.StartsAt), nullableTime(item.EndsAt), nullableTime(item.LastSeenAt), item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.AlertEvent{}, fmt.Errorf("update alert event: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.AlertEvent{}, alertNotFound("alert event", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "alert_events", item.ID)
	return item, nil
}

func (r *Repository) ListNotificationPolicies(ctx context.Context) ([]domainalert.NotificationPolicy, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, matchers, processor_chain, channel_refs, oncall_ref, send_resolved, cooldown_seconds, enabled, created_at, updated_at
		FROM notification_policies
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query notification policies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.NotificationPolicy, 0)
	for rows.Next() {
		item, err := scanNotificationPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateNotificationPolicy(ctx context.Context, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
	item := normalizeNotificationPolicyInput(input, time.Now().UTC())
	matchers, err := json.Marshal(item.Matchers)
	if err != nil {
		return domainalert.NotificationPolicy{}, fmt.Errorf("marshal notification policy matchers: %w", err)
	}
	processorChain, err := json.Marshal(item.ProcessorChain)
	if err != nil {
		return domainalert.NotificationPolicy{}, fmt.Errorf("marshal notification policy processor chain: %w", err)
	}
	channelRefs, err := json.Marshal(item.ChannelRefs)
	if err != nil {
		return domainalert.NotificationPolicy{}, fmt.Errorf("marshal notification policy channel refs: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO notification_policies (
			id, name, matchers, processor_chain, channel_refs, oncall_ref, send_resolved, cooldown_seconds, enabled, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, string(matchers), string(processorChain), string(channelRefs), nullableString(item.OnCallRef), item.SendResolved, item.CooldownSeconds, item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.NotificationPolicy{}, fmt.Errorf("create notification policy: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateNotificationPolicy(ctx context.Context, policyID string, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
	item := normalizeNotificationPolicyInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(policyID)
	matchers, err := json.Marshal(item.Matchers)
	if err != nil {
		return domainalert.NotificationPolicy{}, fmt.Errorf("marshal notification policy matchers: %w", err)
	}
	processorChain, err := json.Marshal(item.ProcessorChain)
	if err != nil {
		return domainalert.NotificationPolicy{}, fmt.Errorf("marshal notification policy processor chain: %w", err)
	}
	channelRefs, err := json.Marshal(item.ChannelRefs)
	if err != nil {
		return domainalert.NotificationPolicy{}, fmt.Errorf("marshal notification policy channel refs: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE notification_policies
		SET name = ?, matchers = ?, processor_chain = ?, channel_refs = ?, oncall_ref = ?, send_resolved = ?, cooldown_seconds = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, string(matchers), string(processorChain), string(channelRefs), nullableString(item.OnCallRef), item.SendResolved, item.CooldownSeconds, item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.NotificationPolicy{}, fmt.Errorf("update notification policy: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.NotificationPolicy{}, alertNotFound("notification policy", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "notification_policies", item.ID)
	return item, nil
}

func (r *Repository) ListNotificationTemplates(ctx context.Context) ([]domainalert.NotificationTemplate, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, template_type, content_type, body_template, headers, query_params, sample_payload, enabled, created_at, updated_at
		FROM notification_templates
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query notification templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.NotificationTemplate, 0)
	for rows.Next() {
		item, err := scanNotificationTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateNotificationTemplate(ctx context.Context, input domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error) {
	item := normalizeNotificationTemplateInput(input, time.Now().UTC())
	headers, err := json.Marshal(item.Headers)
	if err != nil {
		return domainalert.NotificationTemplate{}, fmt.Errorf("marshal notification template headers: %w", err)
	}
	queryParams, err := json.Marshal(item.QueryParams)
	if err != nil {
		return domainalert.NotificationTemplate{}, fmt.Errorf("marshal notification template query params: %w", err)
	}
	samplePayload, err := json.Marshal(item.SamplePayload)
	if err != nil {
		return domainalert.NotificationTemplate{}, fmt.Errorf("marshal notification template sample payload: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO notification_templates (
			id, name, template_type, content_type, body_template, headers, query_params, sample_payload, enabled, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.TemplateType, item.ContentType, nullableString(item.BodyTemplate), string(headers), string(queryParams), string(samplePayload), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.NotificationTemplate{}, fmt.Errorf("create notification template: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateNotificationTemplate(ctx context.Context, templateID string, input domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error) {
	item := normalizeNotificationTemplateInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(templateID)
	headers, err := json.Marshal(item.Headers)
	if err != nil {
		return domainalert.NotificationTemplate{}, fmt.Errorf("marshal notification template headers: %w", err)
	}
	queryParams, err := json.Marshal(item.QueryParams)
	if err != nil {
		return domainalert.NotificationTemplate{}, fmt.Errorf("marshal notification template query params: %w", err)
	}
	samplePayload, err := json.Marshal(item.SamplePayload)
	if err != nil {
		return domainalert.NotificationTemplate{}, fmt.Errorf("marshal notification template sample payload: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE notification_templates
		SET name = ?, template_type = ?, content_type = ?, body_template = ?, headers = ?, query_params = ?, sample_payload = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, item.TemplateType, item.ContentType, nullableString(item.BodyTemplate), string(headers), string(queryParams), string(samplePayload), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.NotificationTemplate{}, fmt.Errorf("update notification template: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.NotificationTemplate{}, alertNotFound("notification template", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "notification_templates", item.ID)
	return item, nil
}

func (r *Repository) ListHealingPolicies(ctx context.Context) ([]domainalert.HealingPolicy, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, trigger_mode, workflow_template_id, approval_policy_ref, cooldown_seconds, concurrency_key, safety_window_seconds, definition, enabled, created_at, updated_at
		FROM healing_policies
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query healing policies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.HealingPolicy, 0)
	for rows.Next() {
		item, err := scanHealingPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetHealingPolicy(ctx context.Context, policyID string) (domainalert.HealingPolicy, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, trigger_mode, workflow_template_id, approval_policy_ref, cooldown_seconds, concurrency_key, safety_window_seconds, definition, enabled, created_at, updated_at
		FROM healing_policies
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(policyID)).Row()
	return scanHealingPolicyRow(row, policyID)
}

func (r *Repository) CreateHealingPolicy(ctx context.Context, input domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error) {
	item := normalizeHealingPolicyInput(input, time.Now().UTC())
	definition, err := json.Marshal(item.Definition)
	if err != nil {
		return domainalert.HealingPolicy{}, fmt.Errorf("marshal healing policy definition: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO healing_policies (
			id, name, trigger_mode, workflow_template_id, approval_policy_ref, cooldown_seconds, concurrency_key, safety_window_seconds, definition, enabled, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.TriggerMode, item.WorkflowTemplateID, nullableString(item.ApprovalPolicyRef), item.CooldownSeconds, nullableString(item.ConcurrencyKey), item.SafetyWindowSeconds, string(definition), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.HealingPolicy{}, fmt.Errorf("create healing policy: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateHealingPolicy(ctx context.Context, policyID string, input domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error) {
	item := normalizeHealingPolicyInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(policyID)
	definition, err := json.Marshal(item.Definition)
	if err != nil {
		return domainalert.HealingPolicy{}, fmt.Errorf("marshal healing policy definition: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE healing_policies
		SET name = ?, trigger_mode = ?, workflow_template_id = ?, approval_policy_ref = ?, cooldown_seconds = ?, concurrency_key = ?, safety_window_seconds = ?, definition = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, item.TriggerMode, item.WorkflowTemplateID, nullableString(item.ApprovalPolicyRef), item.CooldownSeconds, nullableString(item.ConcurrencyKey), item.SafetyWindowSeconds, string(definition), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.HealingPolicy{}, fmt.Errorf("update healing policy: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.HealingPolicy{}, alertNotFound("healing policy", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "healing_policies", item.ID)
	return item, nil
}

func (r *Repository) ListHealingRuns(ctx context.Context, filter domainalert.HealingRunFilter) ([]domainalert.HealingRun, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args := []any{}
	conditions := []string{}
	if strings.TrimSpace(filter.PolicyID) != "" {
		conditions = append(conditions, "policy_id = ?")
		args = append(args, strings.TrimSpace(filter.PolicyID))
	}
	if strings.TrimSpace(filter.EventID) != "" {
		conditions = append(conditions, "event_id = ?")
		args = append(args, strings.TrimSpace(filter.EventID))
	}
	if strings.TrimSpace(filter.Status) != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Status)))
	}
	query := `
		SELECT id, policy_id, event_id, status, approval_status, approval_comment, requested_by, approved_by, workflow_run_id, result, started_at, completed_at, created_at, updated_at
		FROM healing_runs
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY updated_at DESC, created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query healing runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.HealingRun, 0, limit)
	for rows.Next() {
		item, err := scanHealingRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetHealingRun(ctx context.Context, runID string) (domainalert.HealingRun, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, policy_id, event_id, status, approval_status, approval_comment, requested_by, approved_by, workflow_run_id, result, started_at, completed_at, created_at, updated_at
		FROM healing_runs
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(runID)).Row()
	return scanHealingRunRow(row, runID)
}

func (r *Repository) CreateHealingRun(ctx context.Context, input domainalert.HealingRunInput) (domainalert.HealingRun, error) {
	item := normalizeHealingRunInput(input, time.Now().UTC())
	result, err := json.Marshal(item.Result)
	if err != nil {
		return domainalert.HealingRun{}, fmt.Errorf("marshal healing run result: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO healing_runs (
			id, policy_id, event_id, status, approval_status, approval_comment, requested_by, approved_by, workflow_run_id, result, started_at, completed_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.PolicyID, nullableString(item.EventID), item.Status, nullableString(item.ApprovalStatus), nullableString(item.ApprovalComment), nullableString(item.RequestedBy), nullableString(item.ApprovedBy), nullableString(item.WorkflowRunID), string(result), nullableTime(item.StartedAt), nullableTime(item.CompletedAt), item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.HealingRun{}, fmt.Errorf("create healing run: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateHealingRun(ctx context.Context, runID string, input domainalert.HealingRunInput) (domainalert.HealingRun, error) {
	item := normalizeHealingRunInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(runID)
	result, err := json.Marshal(item.Result)
	if err != nil {
		return domainalert.HealingRun{}, fmt.Errorf("marshal healing run result: %w", err)
	}
	updated := r.db.WithContext(ctx).Exec(`
		UPDATE healing_runs
		SET policy_id = ?, event_id = ?, status = ?, approval_status = ?, approval_comment = ?, requested_by = ?, approved_by = ?, workflow_run_id = ?, result = ?, started_at = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, item.PolicyID, nullableString(item.EventID), item.Status, nullableString(item.ApprovalStatus), nullableString(item.ApprovalComment), nullableString(item.RequestedBy), nullableString(item.ApprovedBy), nullableString(item.WorkflowRunID), string(result), nullableTime(item.StartedAt), nullableTime(item.CompletedAt), item.UpdatedAt, item.ID)
	if updated.Error != nil {
		return domainalert.HealingRun{}, fmt.Errorf("update healing run: %w", updated.Error)
	}
	if updated.RowsAffected == 0 {
		return domainalert.HealingRun{}, alertNotFound("healing run", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "healing_runs", item.ID)
	return item, nil
}

func (r *Repository) ListOnCallSchedules(ctx context.Context) ([]domainalert.OnCallSchedule, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, time_zone, description, enabled, created_at, updated_at
		FROM oncall_schedules
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query oncall schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.OnCallSchedule, 0)
	for rows.Next() {
		var item domainalert.OnCallSchedule
		var timeZone, description sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &timeZone, &description, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan oncall schedule: %w", err)
		}
		if timeZone.Valid {
			item.TimeZone = timeZone.String
		}
		if description.Valid {
			item.Description = description.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateOnCallSchedule(ctx context.Context, input domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	item := normalizeOnCallScheduleInput(input, time.Now().UTC())
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO oncall_schedules (id, name, time_zone, description, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, nullableString(item.TimeZone), nullableString(item.Description), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.OnCallSchedule{}, fmt.Errorf("create oncall schedule: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateOnCallSchedule(ctx context.Context, scheduleID string, input domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	item := normalizeOnCallScheduleInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(scheduleID)
	result := r.db.WithContext(ctx).Exec(`
		UPDATE oncall_schedules
		SET name = ?, time_zone = ?, description = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, nullableString(item.TimeZone), nullableString(item.Description), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.OnCallSchedule{}, fmt.Errorf("update oncall schedule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.OnCallSchedule{}, alertNotFound("oncall schedule", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "oncall_schedules", item.ID)
	return item, nil
}

func (r *Repository) ListOnCallRotations(ctx context.Context) ([]domainalert.OnCallRotation, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, schedule_id, name, participants, rotation_config, enabled, created_at, updated_at
		FROM oncall_rotations
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query oncall rotations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.OnCallRotation, 0)
	for rows.Next() {
		var item domainalert.OnCallRotation
		var participants []byte
		var rotationConfig []byte
		if err := rows.Scan(&item.ID, &item.ScheduleID, &item.Name, &participants, &rotationConfig, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan oncall rotation: %w", err)
		}
		_ = json.Unmarshal(participants, &item.Participants)
		_ = json.Unmarshal(rotationConfig, &item.RotationConfig)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateOnCallRotation(ctx context.Context, input domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error) {
	item := normalizeOnCallRotationInput(input, time.Now().UTC())
	participants, err := json.Marshal(item.Participants)
	if err != nil {
		return domainalert.OnCallRotation{}, fmt.Errorf("marshal oncall rotation participants: %w", err)
	}
	rotationConfig, err := json.Marshal(item.RotationConfig)
	if err != nil {
		return domainalert.OnCallRotation{}, fmt.Errorf("marshal oncall rotation config: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO oncall_rotations (id, schedule_id, name, participants, rotation_config, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ScheduleID, item.Name, string(participants), string(rotationConfig), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.OnCallRotation{}, fmt.Errorf("create oncall rotation: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateOnCallRotation(ctx context.Context, rotationID string, input domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error) {
	item := normalizeOnCallRotationInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(rotationID)
	participants, err := json.Marshal(item.Participants)
	if err != nil {
		return domainalert.OnCallRotation{}, fmt.Errorf("marshal oncall rotation participants: %w", err)
	}
	rotationConfig, err := json.Marshal(item.RotationConfig)
	if err != nil {
		return domainalert.OnCallRotation{}, fmt.Errorf("marshal oncall rotation config: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE oncall_rotations
		SET schedule_id = ?, name = ?, participants = ?, rotation_config = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.ScheduleID, item.Name, string(participants), string(rotationConfig), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.OnCallRotation{}, fmt.Errorf("update oncall rotation: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.OnCallRotation{}, alertNotFound("oncall rotation", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "oncall_rotations", item.ID)
	return item, nil
}

func (r *Repository) ListOnCallEscalationPolicies(ctx context.Context) ([]domainalert.OnCallEscalationPolicy, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, steps, enabled, created_at, updated_at
		FROM oncall_escalation_policies
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query oncall escalation policies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.OnCallEscalationPolicy, 0)
	for rows.Next() {
		var item domainalert.OnCallEscalationPolicy
		var steps []byte
		if err := rows.Scan(&item.ID, &item.Name, &steps, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan oncall escalation policy: %w", err)
		}
		_ = json.Unmarshal(steps, &item.Steps)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateOnCallEscalationPolicy(ctx context.Context, input domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error) {
	item := normalizeOnCallEscalationPolicyInput(input, time.Now().UTC())
	steps, err := json.Marshal(item.Steps)
	if err != nil {
		return domainalert.OnCallEscalationPolicy{}, fmt.Errorf("marshal oncall escalation policy steps: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO oncall_escalation_policies (id, name, steps, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, string(steps), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.OnCallEscalationPolicy{}, fmt.Errorf("create oncall escalation policy: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateOnCallEscalationPolicy(ctx context.Context, policyID string, input domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error) {
	item := normalizeOnCallEscalationPolicyInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(policyID)
	steps, err := json.Marshal(item.Steps)
	if err != nil {
		return domainalert.OnCallEscalationPolicy{}, fmt.Errorf("marshal oncall escalation policy steps: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE oncall_escalation_policies
		SET name = ?, steps = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, string(steps), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.OnCallEscalationPolicy{}, fmt.Errorf("update oncall escalation policy: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.OnCallEscalationPolicy{}, alertNotFound("oncall escalation policy", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "oncall_escalation_policies", item.ID)
	return item, nil
}

func (r *Repository) ListOnCallAssignmentRules(ctx context.Context) ([]domainalert.OnCallAssignmentRule, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, integration_id, integration_type, business_line_id, alert_category, alert_name, severity, service, role, matchers, target_type, target_ref, route_order, group_by, priority, enabled, created_at, updated_at
		FROM oncall_assignment_rules
		ORDER BY CASE WHEN route_order > 0 THEN route_order ELSE 100000 - priority END ASC, priority DESC, updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query oncall assignment rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]domainalert.OnCallAssignmentRule, 0)
	for rows.Next() {
		item, err := scanOnCallAssignmentRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateOnCallAssignmentRule(ctx context.Context, input domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error) {
	item := normalizeOnCallAssignmentRuleInput(input, time.Now().UTC())
	matchers, err := json.Marshal(item.Matchers)
	if err != nil {
		return domainalert.OnCallAssignmentRule{}, fmt.Errorf("marshal oncall assignment rule matchers: %w", err)
	}
	groupBy, err := json.Marshal(item.GroupBy)
	if err != nil {
		return domainalert.OnCallAssignmentRule{}, fmt.Errorf("marshal oncall assignment rule group by: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO oncall_assignment_rules (
			id, name, integration_id, integration_type, business_line_id, alert_category, alert_name, severity, service, role, matchers, target_type, target_ref, route_order, group_by, priority, enabled, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, nullableString(item.IntegrationID), nullableString(item.IntegrationType), nullableString(item.BusinessLineID), nullableString(item.AlertCategory), nullableString(item.AlertName), nullableString(item.Severity), nullableString(item.Service), nullableString(item.Role), string(matchers), item.TargetType, item.TargetRef, item.RouteOrder, string(groupBy), item.Priority, item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.OnCallAssignmentRule{}, fmt.Errorf("create oncall assignment rule: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateOnCallAssignmentRule(ctx context.Context, ruleID string, input domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error) {
	item := normalizeOnCallAssignmentRuleInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(ruleID)
	matchers, err := json.Marshal(item.Matchers)
	if err != nil {
		return domainalert.OnCallAssignmentRule{}, fmt.Errorf("marshal oncall assignment rule matchers: %w", err)
	}
	groupBy, err := json.Marshal(item.GroupBy)
	if err != nil {
		return domainalert.OnCallAssignmentRule{}, fmt.Errorf("marshal oncall assignment rule group by: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE oncall_assignment_rules
		SET name = ?, integration_id = ?, integration_type = ?, business_line_id = ?, alert_category = ?, alert_name = ?, severity = ?, service = ?, role = ?, matchers = ?, target_type = ?, target_ref = ?, route_order = ?, group_by = ?, priority = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, nullableString(item.IntegrationID), nullableString(item.IntegrationType), nullableString(item.BusinessLineID), nullableString(item.AlertCategory), nullableString(item.AlertName), nullableString(item.Severity), nullableString(item.Service), nullableString(item.Role), string(matchers), item.TargetType, item.TargetRef, item.RouteOrder, string(groupBy), item.Priority, item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.OnCallAssignmentRule{}, fmt.Errorf("update oncall assignment rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.OnCallAssignmentRule{}, alertNotFound("oncall assignment rule", item.ID)
	}
	item.CreatedAt = fetchTableCreatedAt(ctx, r.db, "oncall_assignment_rules", item.ID)
	return item, nil
}

func fetchTableCreatedAt(ctx context.Context, db *gorm.DB, table string, id string) time.Time {
	var createdAt time.Time
	if err := db.WithContext(ctx).Raw(fmt.Sprintf(`SELECT created_at FROM %s WHERE id = ?`, table), id).Row().Scan(&createdAt); err != nil {
		return time.Time{}
	}
	return createdAt
}

func scanAlertRule(rows *sql.Rows) (domainalert.AlertRule, error) {
	var item domainalert.AlertRule
	var datasourceSelector []byte
	var querySpec []byte
	var thresholdSpec []byte
	var groupBy []byte
	var labels []byte
	var annotations []byte
	var notificationPolicyID sql.NullString
	var healingPolicyIDs []byte
	if err := rows.Scan(&item.ID, &item.Name, &item.RuleType, &datasourceSelector, &querySpec, &thresholdSpec, &item.ForSeconds, &groupBy, &labels, &annotations, &notificationPolicyID, &healingPolicyIDs, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainalert.AlertRule{}, fmt.Errorf("scan alert rule: %w", err)
	}
	_ = json.Unmarshal(datasourceSelector, &item.DatasourceSelector)
	_ = json.Unmarshal(querySpec, &item.QuerySpec)
	_ = json.Unmarshal(thresholdSpec, &item.ThresholdSpec)
	_ = json.Unmarshal(groupBy, &item.GroupBy)
	_ = json.Unmarshal(labels, &item.Labels)
	_ = json.Unmarshal(annotations, &item.Annotations)
	_ = json.Unmarshal(healingPolicyIDs, &item.HealingPolicyIDs)
	if notificationPolicyID.Valid {
		item.NotificationPolicyID = notificationPolicyID.String
	}
	return item, nil
}

func scanAlertRuleRow(row *sql.Row, ruleID string) (domainalert.AlertRule, error) {
	var item domainalert.AlertRule
	var datasourceSelector []byte
	var querySpec []byte
	var thresholdSpec []byte
	var groupBy []byte
	var labels []byte
	var annotations []byte
	var notificationPolicyID sql.NullString
	var healingPolicyIDs []byte
	if err := row.Scan(&item.ID, &item.Name, &item.RuleType, &datasourceSelector, &querySpec, &thresholdSpec, &item.ForSeconds, &groupBy, &labels, &annotations, &notificationPolicyID, &healingPolicyIDs, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainalert.AlertRule{}, alertNotFound("alert rule", ruleID)
		}
		return domainalert.AlertRule{}, fmt.Errorf("scan alert rule row: %w", err)
	}
	_ = json.Unmarshal(datasourceSelector, &item.DatasourceSelector)
	_ = json.Unmarshal(querySpec, &item.QuerySpec)
	_ = json.Unmarshal(thresholdSpec, &item.ThresholdSpec)
	_ = json.Unmarshal(groupBy, &item.GroupBy)
	_ = json.Unmarshal(labels, &item.Labels)
	_ = json.Unmarshal(annotations, &item.Annotations)
	_ = json.Unmarshal(healingPolicyIDs, &item.HealingPolicyIDs)
	if notificationPolicyID.Valid {
		item.NotificationPolicyID = notificationPolicyID.String
	}
	return item, nil
}

func scanAlertEvent(rows *sql.Rows) (domainalert.AlertEvent, error) {
	var item domainalert.AlertEvent
	var labels []byte
	var annotations []byte
	var ruleID sql.NullString
	var sourceSystem sql.NullString
	var clusterID sql.NullString
	var namespace sql.NullString
	var receiver sql.NullString
	var generatorURL sql.NullString
	var currentState sql.NullString
	var lastNotificationAt sql.NullTime
	var startsAt sql.NullTime
	var endsAt sql.NullTime
	if err := rows.Scan(&item.ID, &ruleID, &item.SourceType, &sourceSystem, &item.Fingerprint, &item.Title, &item.Summary, &item.Severity, &item.Status, &clusterID, &namespace,
		&labels, &annotations, &receiver, &generatorURL, &currentState, &lastNotificationAt, &startsAt, &endsAt, &item.LastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainalert.AlertEvent{}, fmt.Errorf("scan alert event: %w", err)
	}
	if ruleID.Valid {
		item.RuleID = ruleID.String
	}
	if sourceSystem.Valid {
		item.SourceSystem = sourceSystem.String
	}
	if clusterID.Valid {
		item.ClusterID = clusterID.String
	}
	if namespace.Valid {
		item.Namespace = namespace.String
	}
	if receiver.Valid {
		item.Receiver = receiver.String
	}
	if generatorURL.Valid {
		item.GeneratorURL = generatorURL.String
	}
	if currentState.Valid {
		item.CurrentState = currentState.String
	}
	if lastNotificationAt.Valid {
		item.LastNotificationAt = lastNotificationAt.Time
	}
	if startsAt.Valid {
		item.StartsAt = startsAt.Time
	}
	if endsAt.Valid {
		item.EndsAt = endsAt.Time
	}
	_ = json.Unmarshal(labels, &item.Labels)
	_ = json.Unmarshal(annotations, &item.Annotations)
	return item, nil
}

func scanAlertEventRow(row *sql.Row, eventID string) (domainalert.AlertEvent, error) {
	var item domainalert.AlertEvent
	var labels []byte
	var annotations []byte
	var ruleID sql.NullString
	var sourceSystem sql.NullString
	var clusterID sql.NullString
	var namespace sql.NullString
	var receiver sql.NullString
	var generatorURL sql.NullString
	var currentState sql.NullString
	var lastNotificationAt sql.NullTime
	var startsAt sql.NullTime
	var endsAt sql.NullTime
	if err := row.Scan(&item.ID, &ruleID, &item.SourceType, &sourceSystem, &item.Fingerprint, &item.Title, &item.Summary, &item.Severity, &item.Status, &clusterID, &namespace,
		&labels, &annotations, &receiver, &generatorURL, &currentState, &lastNotificationAt, &startsAt, &endsAt, &item.LastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainalert.AlertEvent{}, alertNotFound("alert event", eventID)
		}
		return domainalert.AlertEvent{}, fmt.Errorf("scan alert event row: %w", err)
	}
	if ruleID.Valid {
		item.RuleID = ruleID.String
	}
	if sourceSystem.Valid {
		item.SourceSystem = sourceSystem.String
	}
	if clusterID.Valid {
		item.ClusterID = clusterID.String
	}
	if namespace.Valid {
		item.Namespace = namespace.String
	}
	if receiver.Valid {
		item.Receiver = receiver.String
	}
	if generatorURL.Valid {
		item.GeneratorURL = generatorURL.String
	}
	if currentState.Valid {
		item.CurrentState = currentState.String
	}
	if lastNotificationAt.Valid {
		item.LastNotificationAt = lastNotificationAt.Time
	}
	if startsAt.Valid {
		item.StartsAt = startsAt.Time
	}
	if endsAt.Valid {
		item.EndsAt = endsAt.Time
	}
	_ = json.Unmarshal(labels, &item.Labels)
	_ = json.Unmarshal(annotations, &item.Annotations)
	return item, nil
}

func scanAlertRuleRun(rows *sql.Rows) (domainalert.AlertRuleRun, error) {
	var item domainalert.AlertRuleRun
	var ruleID sql.NullString
	var summary sql.NullString
	var runError sql.NullString
	var result []byte
	if err := rows.Scan(&item.ID, &ruleID, &item.Status, &summary, &item.Matched, &item.DurationMs, &runError, &result, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainalert.AlertRuleRun{}, fmt.Errorf("scan alert rule run: %w", err)
	}
	if ruleID.Valid {
		item.RuleID = ruleID.String
	}
	if summary.Valid {
		item.Summary = summary.String
	}
	if runError.Valid {
		item.Error = runError.String
	}
	_ = json.Unmarshal(result, &item.Result)
	return item, nil
}

func normalizeAlertRuleRunInput(input domainalert.AlertRuleRunInput, now time.Time) domainalert.AlertRuleRun {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "rule-run:" + uuid.NewString()
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = "completed"
	}
	if input.Result == nil {
		input.Result = map[string]any{}
	}
	return domainalert.AlertRuleRun{
		ID:         id,
		RuleID:     strings.TrimSpace(input.RuleID),
		Status:     status,
		Summary:    strings.TrimSpace(input.Summary),
		Matched:    input.Matched,
		DurationMs: input.DurationMs,
		Error:      strings.TrimSpace(input.Error),
		Result:     input.Result,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func scanNotificationPolicy(rows *sql.Rows) (domainalert.NotificationPolicy, error) {
	var item domainalert.NotificationPolicy
	var matchers []byte
	var processorChain []byte
	var channelRefs []byte
	var oncallRef sql.NullString
	if err := rows.Scan(&item.ID, &item.Name, &matchers, &processorChain, &channelRefs, &oncallRef, &item.SendResolved, &item.CooldownSeconds, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainalert.NotificationPolicy{}, fmt.Errorf("scan notification policy: %w", err)
	}
	_ = json.Unmarshal(matchers, &item.Matchers)
	_ = json.Unmarshal(processorChain, &item.ProcessorChain)
	_ = json.Unmarshal(channelRefs, &item.ChannelRefs)
	if oncallRef.Valid {
		item.OnCallRef = oncallRef.String
	}
	return item, nil
}

func scanOnCallAssignmentRule(rows *sql.Rows) (domainalert.OnCallAssignmentRule, error) {
	var item domainalert.OnCallAssignmentRule
	var integrationID, integrationType, businessLineID, alertCategory, alertName, severity, service, role sql.NullString
	var matchers []byte
	var groupBy []byte
	if err := rows.Scan(
		&item.ID,
		&item.Name,
		&integrationID,
		&integrationType,
		&businessLineID,
		&alertCategory,
		&alertName,
		&severity,
		&service,
		&role,
		&matchers,
		&item.TargetType,
		&item.TargetRef,
		&item.RouteOrder,
		&groupBy,
		&item.Priority,
		&item.Enabled,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainalert.OnCallAssignmentRule{}, fmt.Errorf("scan oncall assignment rule: %w", err)
	}
	if integrationID.Valid {
		item.IntegrationID = integrationID.String
	}
	if integrationType.Valid {
		item.IntegrationType = integrationType.String
	}
	if businessLineID.Valid {
		item.BusinessLineID = businessLineID.String
	}
	if alertCategory.Valid {
		item.AlertCategory = alertCategory.String
	}
	if alertName.Valid {
		item.AlertName = alertName.String
	}
	if severity.Valid {
		item.Severity = severity.String
	}
	if service.Valid {
		item.Service = service.String
	}
	if role.Valid {
		item.Role = role.String
	}
	_ = json.Unmarshal(matchers, &item.Matchers)
	if item.Matchers == nil {
		item.Matchers = map[string]any{}
	}
	_ = json.Unmarshal(groupBy, &item.GroupBy)
	item.GroupBy = normalizeStrings(item.GroupBy)
	return item, nil
}

func scanNotificationTemplate(rows *sql.Rows) (domainalert.NotificationTemplate, error) {
	var item domainalert.NotificationTemplate
	var bodyTemplate sql.NullString
	var headers []byte
	var queryParams []byte
	var samplePayload []byte
	if err := rows.Scan(&item.ID, &item.Name, &item.TemplateType, &item.ContentType, &bodyTemplate, &headers, &queryParams, &samplePayload, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainalert.NotificationTemplate{}, fmt.Errorf("scan notification template: %w", err)
	}
	if bodyTemplate.Valid {
		item.BodyTemplate = bodyTemplate.String
	}
	_ = json.Unmarshal(headers, &item.Headers)
	_ = json.Unmarshal(queryParams, &item.QueryParams)
	_ = json.Unmarshal(samplePayload, &item.SamplePayload)
	return item, nil
}

func scanHealingPolicyRow(row *sql.Row, policyID string) (domainalert.HealingPolicy, error) {
	var item domainalert.HealingPolicy
	var approvalPolicyRef sql.NullString
	var concurrencyKey sql.NullString
	var definition []byte
	if err := row.Scan(&item.ID, &item.Name, &item.TriggerMode, &item.WorkflowTemplateID, &approvalPolicyRef, &item.CooldownSeconds, &concurrencyKey, &item.SafetyWindowSeconds, &definition, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainalert.HealingPolicy{}, alertNotFound("healing policy", policyID)
		}
		return domainalert.HealingPolicy{}, fmt.Errorf("scan healing policy row: %w", err)
	}
	if approvalPolicyRef.Valid {
		item.ApprovalPolicyRef = approvalPolicyRef.String
	}
	if concurrencyKey.Valid {
		item.ConcurrencyKey = concurrencyKey.String
	}
	_ = json.Unmarshal(definition, &item.Definition)
	return item, nil
}

func scanHealingPolicy(rows *sql.Rows) (domainalert.HealingPolicy, error) {
	var item domainalert.HealingPolicy
	var approvalPolicyRef sql.NullString
	var concurrencyKey sql.NullString
	var definition []byte
	if err := rows.Scan(&item.ID, &item.Name, &item.TriggerMode, &item.WorkflowTemplateID, &approvalPolicyRef, &item.CooldownSeconds, &concurrencyKey, &item.SafetyWindowSeconds, &definition, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainalert.HealingPolicy{}, fmt.Errorf("scan healing policy: %w", err)
	}
	if approvalPolicyRef.Valid {
		item.ApprovalPolicyRef = approvalPolicyRef.String
	}
	if concurrencyKey.Valid {
		item.ConcurrencyKey = concurrencyKey.String
	}
	_ = json.Unmarshal(definition, &item.Definition)
	return item, nil
}

func scanHealingRun(rows *sql.Rows) (domainalert.HealingRun, error) {
	var item domainalert.HealingRun
	var eventID sql.NullString
	var approvalStatus sql.NullString
	var approvalComment sql.NullString
	var requestedBy sql.NullString
	var approvedBy sql.NullString
	var workflowRunID sql.NullString
	var result []byte
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.PolicyID, &eventID, &item.Status, &approvalStatus, &approvalComment, &requestedBy, &approvedBy, &workflowRunID, &result, &startedAt, &completedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainalert.HealingRun{}, fmt.Errorf("scan healing run: %w", err)
	}
	if eventID.Valid {
		item.EventID = eventID.String
	}
	if approvalStatus.Valid {
		item.ApprovalStatus = approvalStatus.String
	}
	if approvalComment.Valid {
		item.ApprovalComment = approvalComment.String
	}
	if requestedBy.Valid {
		item.RequestedBy = requestedBy.String
	}
	if approvedBy.Valid {
		item.ApprovedBy = approvedBy.String
	}
	if workflowRunID.Valid {
		item.WorkflowRunID = workflowRunID.String
	}
	if startedAt.Valid {
		item.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = completedAt.Time
	}
	_ = json.Unmarshal(result, &item.Result)
	return item, nil
}

func scanHealingRunRow(row *sql.Row, runID string) (domainalert.HealingRun, error) {
	var item domainalert.HealingRun
	var eventID sql.NullString
	var approvalStatus sql.NullString
	var approvalComment sql.NullString
	var requestedBy sql.NullString
	var approvedBy sql.NullString
	var workflowRunID sql.NullString
	var result []byte
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.PolicyID, &eventID, &item.Status, &approvalStatus, &approvalComment, &requestedBy, &approvedBy, &workflowRunID, &result, &startedAt, &completedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainalert.HealingRun{}, alertNotFound("healing run", runID)
		}
		return domainalert.HealingRun{}, fmt.Errorf("scan healing run row: %w", err)
	}
	if eventID.Valid {
		item.EventID = eventID.String
	}
	if approvalStatus.Valid {
		item.ApprovalStatus = approvalStatus.String
	}
	if approvalComment.Valid {
		item.ApprovalComment = approvalComment.String
	}
	if requestedBy.Valid {
		item.RequestedBy = requestedBy.String
	}
	if approvedBy.Valid {
		item.ApprovedBy = approvedBy.String
	}
	if workflowRunID.Valid {
		item.WorkflowRunID = workflowRunID.String
	}
	if startedAt.Valid {
		item.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = completedAt.Time
	}
	_ = json.Unmarshal(result, &item.Result)
	return item, nil
}

func normalizeAlertRuleInput(input domainalert.AlertRuleInput, now time.Time) domainalert.AlertRule {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "rule:" + uuid.NewString()
	}
	ruleType := strings.ToLower(strings.TrimSpace(input.RuleType))
	if ruleType == "" {
		ruleType = "metrics"
	}
	if input.DatasourceSelector == nil {
		input.DatasourceSelector = map[string]any{}
	}
	if input.QuerySpec == nil {
		input.QuerySpec = map[string]any{}
	}
	if input.ThresholdSpec == nil {
		input.ThresholdSpec = map[string]any{}
	}
	if input.GroupBy == nil {
		input.GroupBy = []string{}
	}
	if input.Labels == nil {
		input.Labels = map[string]string{}
	}
	if input.Annotations == nil {
		input.Annotations = map[string]string{}
	}
	if input.HealingPolicyIDs == nil {
		input.HealingPolicyIDs = []string{}
	}
	return domainalert.AlertRule{
		ID:                   id,
		Name:                 strings.TrimSpace(input.Name),
		RuleType:             ruleType,
		DatasourceSelector:   input.DatasourceSelector,
		QuerySpec:            input.QuerySpec,
		ThresholdSpec:        input.ThresholdSpec,
		ForSeconds:           input.ForSeconds,
		GroupBy:              normalizeStrings(input.GroupBy),
		Labels:               input.Labels,
		Annotations:          input.Annotations,
		NotificationPolicyID: strings.TrimSpace(input.NotificationPolicyID),
		HealingPolicyIDs:     normalizeStrings(input.HealingPolicyIDs),
		Enabled:              input.Enabled,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

func normalizeAlertEventInput(input domainalert.AlertEventInput, now time.Time) domainalert.AlertEvent {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "event:" + uuid.NewString()
	}
	sourceType := strings.ToLower(strings.TrimSpace(input.SourceType))
	if sourceType == "" {
		sourceType = "external_webhook"
	}
	if input.Labels == nil {
		input.Labels = map[string]string{}
	}
	if input.Annotations == nil {
		input.Annotations = map[string]string{}
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = "firing"
	}
	currentState := strings.ToLower(strings.TrimSpace(input.CurrentState))
	if currentState == "" {
		currentState = status
	}
	return domainalert.AlertEvent{
		ID:                 id,
		RuleID:             strings.TrimSpace(input.RuleID),
		SourceType:         sourceType,
		SourceSystem:       strings.TrimSpace(input.SourceSystem),
		Fingerprint:        strings.TrimSpace(input.Fingerprint),
		Title:              strings.TrimSpace(input.Title),
		Summary:            strings.TrimSpace(input.Summary),
		Severity:           strings.ToLower(strings.TrimSpace(input.Severity)),
		Status:             status,
		ClusterID:          strings.TrimSpace(input.ClusterID),
		Namespace:          strings.TrimSpace(input.Namespace),
		Labels:             input.Labels,
		Annotations:        input.Annotations,
		Receiver:           strings.TrimSpace(input.Receiver),
		GeneratorURL:       strings.TrimSpace(input.GeneratorURL),
		CurrentState:       currentState,
		LastNotificationAt: input.LastNotificationAt,
		StartsAt:           input.StartsAt,
		EndsAt:             input.EndsAt,
		LastSeenAt:         now,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func normalizeNotificationPolicyInput(input domainalert.NotificationPolicyInput, now time.Time) domainalert.NotificationPolicy {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "policy:" + uuid.NewString()
	}
	if input.Matchers == nil {
		input.Matchers = map[string]any{}
	}
	if input.ProcessorChain == nil {
		input.ProcessorChain = []string{}
	}
	if input.ChannelRefs == nil {
		input.ChannelRefs = []string{}
	}
	return domainalert.NotificationPolicy{
		ID:              id,
		Name:            strings.TrimSpace(input.Name),
		Matchers:        input.Matchers,
		ProcessorChain:  normalizeStrings(input.ProcessorChain),
		ChannelRefs:     normalizeStrings(input.ChannelRefs),
		OnCallRef:       strings.TrimSpace(input.OnCallRef),
		SendResolved:    input.SendResolved,
		CooldownSeconds: input.CooldownSeconds,
		Enabled:         input.Enabled,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func normalizeNotificationTemplateInput(input domainalert.NotificationTemplateInput, now time.Time) domainalert.NotificationTemplate {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "template:" + uuid.NewString()
	}
	if input.Headers == nil {
		input.Headers = map[string]any{}
	}
	if input.QueryParams == nil {
		input.QueryParams = map[string]any{}
	}
	if input.SamplePayload == nil {
		input.SamplePayload = map[string]any{}
	}
	templateType := strings.ToLower(strings.TrimSpace(input.TemplateType))
	if templateType == "" {
		templateType = "generic_json"
	}
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = "application/json"
	}
	return domainalert.NotificationTemplate{
		ID:            id,
		Name:          strings.TrimSpace(input.Name),
		TemplateType:  templateType,
		ContentType:   contentType,
		BodyTemplate:  strings.TrimSpace(input.BodyTemplate),
		Headers:       input.Headers,
		QueryParams:   input.QueryParams,
		SamplePayload: input.SamplePayload,
		Enabled:       input.Enabled,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func normalizeHealingPolicyInput(input domainalert.HealingPolicyInput, now time.Time) domainalert.HealingPolicy {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "healing:" + uuid.NewString()
	}
	triggerMode := strings.ToLower(strings.TrimSpace(input.TriggerMode))
	if triggerMode == "" {
		triggerMode = "approval_then_auto"
	}
	if input.Definition == nil {
		input.Definition = map[string]any{}
	}
	return domainalert.HealingPolicy{
		ID:                  id,
		Name:                strings.TrimSpace(input.Name),
		TriggerMode:         triggerMode,
		WorkflowTemplateID:  strings.TrimSpace(input.WorkflowTemplateID),
		ApprovalPolicyRef:   strings.TrimSpace(input.ApprovalPolicyRef),
		CooldownSeconds:     input.CooldownSeconds,
		ConcurrencyKey:      strings.TrimSpace(input.ConcurrencyKey),
		SafetyWindowSeconds: input.SafetyWindowSeconds,
		Definition:          input.Definition,
		Enabled:             input.Enabled,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func normalizeHealingRunInput(input domainalert.HealingRunInput, now time.Time) domainalert.HealingRun {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "healing-run:" + uuid.NewString()
	}
	if input.Result == nil {
		input.Result = map[string]any{}
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = "pending_approval"
	}
	approvalStatus := strings.ToLower(strings.TrimSpace(input.ApprovalStatus))
	if approvalStatus == "" {
		approvalStatus = "pending"
	}
	return domainalert.HealingRun{
		ID:              id,
		PolicyID:        strings.TrimSpace(input.PolicyID),
		EventID:         strings.TrimSpace(input.EventID),
		Status:          status,
		ApprovalStatus:  approvalStatus,
		ApprovalComment: strings.TrimSpace(input.ApprovalComment),
		RequestedBy:     strings.TrimSpace(input.RequestedBy),
		ApprovedBy:      strings.TrimSpace(input.ApprovedBy),
		WorkflowRunID:   strings.TrimSpace(input.WorkflowRunID),
		Result:          input.Result,
		StartedAt:       input.StartedAt,
		CompletedAt:     input.CompletedAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func normalizeOnCallScheduleInput(input domainalert.OnCallScheduleInput, now time.Time) domainalert.OnCallSchedule {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "schedule:" + uuid.NewString()
	}
	return domainalert.OnCallSchedule{
		ID:          id,
		Name:        strings.TrimSpace(input.Name),
		TimeZone:    strings.TrimSpace(input.TimeZone),
		Description: strings.TrimSpace(input.Description),
		Enabled:     input.Enabled,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func normalizeOnCallRotationInput(input domainalert.OnCallRotationInput, now time.Time) domainalert.OnCallRotation {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "rotation:" + uuid.NewString()
	}
	if input.Participants == nil {
		input.Participants = []string{}
	}
	if input.RotationConfig == nil {
		input.RotationConfig = map[string]any{}
	}
	return domainalert.OnCallRotation{
		ID:             id,
		ScheduleID:     strings.TrimSpace(input.ScheduleID),
		Name:           strings.TrimSpace(input.Name),
		Participants:   normalizeStrings(input.Participants),
		RotationConfig: input.RotationConfig,
		Enabled:        input.Enabled,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func normalizeOnCallEscalationPolicyInput(input domainalert.OnCallEscalationPolicyInput, now time.Time) domainalert.OnCallEscalationPolicy {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "escalation:" + uuid.NewString()
	}
	if input.Steps == nil {
		input.Steps = []map[string]any{}
	}
	return domainalert.OnCallEscalationPolicy{
		ID:        id,
		Name:      strings.TrimSpace(input.Name),
		Steps:     input.Steps,
		Enabled:   input.Enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func normalizeOnCallAssignmentRuleInput(input domainalert.OnCallAssignmentRuleInput, now time.Time) domainalert.OnCallAssignmentRule {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "oncall-rule:" + uuid.NewString()
	}
	if input.Matchers == nil {
		input.Matchers = map[string]any{}
	}
	targetType := strings.ToLower(strings.TrimSpace(input.TargetType))
	if targetType == "" {
		targetType = "escalation"
	}
	priority := input.Priority
	if priority == 0 {
		priority = 100
	}
	return domainalert.OnCallAssignmentRule{
		ID:              id,
		Name:            strings.TrimSpace(input.Name),
		IntegrationID:   strings.TrimSpace(input.IntegrationID),
		IntegrationType: strings.ToLower(strings.TrimSpace(input.IntegrationType)),
		BusinessLineID:  strings.TrimSpace(input.BusinessLineID),
		AlertCategory:   strings.TrimSpace(input.AlertCategory),
		AlertName:       strings.TrimSpace(input.AlertName),
		Severity:        strings.ToLower(strings.TrimSpace(input.Severity)),
		Service:         strings.TrimSpace(input.Service),
		Role:            strings.ToLower(strings.TrimSpace(input.Role)),
		Matchers:        input.Matchers,
		TargetType:      targetType,
		TargetRef:       strings.TrimSpace(input.TargetRef),
		RouteOrder:      input.RouteOrder,
		GroupBy:         normalizeStrings(input.GroupBy),
		Priority:        priority,
		Enabled:         input.Enabled,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func normalizeStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(item); value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}
