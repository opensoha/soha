package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainalert "github.com/soha/soha/internal/domain/alert"
)

func (r *Repository) ListAlertIntegrations(ctx context.Context) ([]domainalert.AlertIntegration, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, integration_type, description, token, label_mapping, dedupe_config, enabled, status, last_error, last_received_at, created_at, updated_at
		FROM alert_integrations
		ORDER BY updated_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query alert integrations: %w", err)
	}
	defer rows.Close()

	items := make([]domainalert.AlertIntegration, 0)
	for rows.Next() {
		item, err := scanAlertIntegration(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetAlertIntegration(ctx context.Context, integrationID string) (domainalert.AlertIntegration, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, integration_type, description, token, label_mapping, dedupe_config, enabled, status, last_error, last_received_at, created_at, updated_at
		FROM alert_integrations
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(integrationID)).Row()
	return scanAlertIntegrationRow(row, integrationID)
}

func (r *Repository) CreateAlertIntegration(ctx context.Context, input domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error) {
	item := normalizeAlertIntegrationInput(input, time.Now().UTC())
	labelMapping, err := json.Marshal(item.LabelMapping)
	if err != nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("marshal alert integration label mapping: %w", err)
	}
	dedupeConfig, err := json.Marshal(item.DedupeConfig)
	if err != nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("marshal alert integration dedupe config: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO alert_integrations (
			id, name, integration_type, description, token, label_mapping, dedupe_config, enabled, status, last_error, last_received_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.IntegrationType, nullableString(item.Description), item.Token, string(labelMapping), string(dedupeConfig), item.Enabled, item.Status, nullableString(item.LastError), nullableTime(item.LastReceivedAt), item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("create alert integration: %w", err)
	}
	return withAlertIntegrationRuntimeFields(item), nil
}

func (r *Repository) UpdateAlertIntegration(ctx context.Context, integrationID string, input domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error) {
	integrationID = strings.TrimSpace(integrationID)
	current, err := r.GetAlertIntegration(ctx, integrationID)
	if err != nil {
		return domainalert.AlertIntegration{}, err
	}
	item := normalizeAlertIntegrationInput(input, time.Now().UTC())
	item.ID = integrationID
	item.CreatedAt = current.CreatedAt
	item.Status = strings.TrimSpace(current.Status)
	if item.Status == "" {
		item.Status = "pending"
	}
	item.LastError = current.LastError
	item.LastReceivedAt = current.LastReceivedAt
	if strings.TrimSpace(input.Token) == "" {
		item.Token = current.Token
	}
	labelMapping, err := json.Marshal(item.LabelMapping)
	if err != nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("marshal alert integration label mapping: %w", err)
	}
	dedupeConfig, err := json.Marshal(item.DedupeConfig)
	if err != nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("marshal alert integration dedupe config: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE alert_integrations
		SET name = ?, integration_type = ?, description = ?, token = ?, label_mapping = ?, dedupe_config = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, item.IntegrationType, nullableString(item.Description), item.Token, string(labelMapping), string(dedupeConfig), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("update alert integration: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.AlertIntegration{}, fmt.Errorf("alert integration not found: %s", item.ID)
	}
	return withAlertIntegrationRuntimeFields(item), nil
}

func (r *Repository) UpdateAlertIntegrationStatus(ctx context.Context, integrationID string, input domainalert.AlertIntegrationStatusInput) (domainalert.AlertIntegration, error) {
	integrationID = strings.TrimSpace(integrationID)
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = "active"
	}
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE alert_integrations
		SET status = ?, last_error = ?, last_received_at = COALESCE(?, last_received_at), updated_at = ?
		WHERE id = ?
	`, status, nullableString(input.LastError), nullableTime(input.LastReceivedAt), now, integrationID)
	if result.Error != nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("update alert integration status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.AlertIntegration{}, fmt.Errorf("alert integration not found: %s", integrationID)
	}
	return r.GetAlertIntegration(ctx, integrationID)
}

func normalizeAlertIntegrationInput(input domainalert.AlertIntegrationInput, now time.Time) domainalert.AlertIntegration {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "integration:" + uuid.NewString()
	}
	integrationType := strings.ToLower(strings.TrimSpace(input.IntegrationType))
	if integrationType == "" {
		integrationType = "generic_json"
	}
	token := strings.TrimSpace(input.Token)
	if token == "" {
		token = uuid.NewString()
	}
	if input.LabelMapping == nil {
		input.LabelMapping = map[string]any{}
	}
	if input.DedupeConfig == nil {
		input.DedupeConfig = map[string]any{}
	}
	return withAlertIntegrationRuntimeFields(domainalert.AlertIntegration{
		ID:              id,
		Name:            strings.TrimSpace(input.Name),
		IntegrationType: integrationType,
		Description:     strings.TrimSpace(input.Description),
		Token:           token,
		LabelMapping:    input.LabelMapping,
		DedupeConfig:    input.DedupeConfig,
		Enabled:         input.Enabled,
		Status:          "pending",
		CreatedAt:       now,
		UpdatedAt:       now,
	})
}

func scanAlertIntegration(rows *sql.Rows) (domainalert.AlertIntegration, error) {
	var item domainalert.AlertIntegration
	var description sql.NullString
	var labelMapping []byte
	var dedupeConfig []byte
	var lastError sql.NullString
	var lastReceivedAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.Name, &item.IntegrationType, &description, &item.Token, &labelMapping, &dedupeConfig, &item.Enabled, &item.Status, &lastError, &lastReceivedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainalert.AlertIntegration{}, fmt.Errorf("scan alert integration: %w", err)
	}
	if description.Valid {
		item.Description = description.String
	}
	if lastError.Valid {
		item.LastError = lastError.String
	}
	if lastReceivedAt.Valid {
		item.LastReceivedAt = lastReceivedAt.Time
	}
	_ = json.Unmarshal(labelMapping, &item.LabelMapping)
	_ = json.Unmarshal(dedupeConfig, &item.DedupeConfig)
	if item.LabelMapping == nil {
		item.LabelMapping = map[string]any{}
	}
	if item.DedupeConfig == nil {
		item.DedupeConfig = map[string]any{}
	}
	return withAlertIntegrationRuntimeFields(item), nil
}

func scanAlertIntegrationRow(row *sql.Row, integrationID string) (domainalert.AlertIntegration, error) {
	var item domainalert.AlertIntegration
	var description sql.NullString
	var labelMapping []byte
	var dedupeConfig []byte
	var lastError sql.NullString
	var lastReceivedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Name, &item.IntegrationType, &description, &item.Token, &labelMapping, &dedupeConfig, &item.Enabled, &item.Status, &lastError, &lastReceivedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return domainalert.AlertIntegration{}, fmt.Errorf("alert integration not found: %s", strings.TrimSpace(integrationID))
		}
		return domainalert.AlertIntegration{}, fmt.Errorf("scan alert integration row: %w", err)
	}
	if description.Valid {
		item.Description = description.String
	}
	if lastError.Valid {
		item.LastError = lastError.String
	}
	if lastReceivedAt.Valid {
		item.LastReceivedAt = lastReceivedAt.Time
	}
	_ = json.Unmarshal(labelMapping, &item.LabelMapping)
	_ = json.Unmarshal(dedupeConfig, &item.DedupeConfig)
	if item.LabelMapping == nil {
		item.LabelMapping = map[string]any{}
	}
	if item.DedupeConfig == nil {
		item.DedupeConfig = map[string]any{}
	}
	return withAlertIntegrationRuntimeFields(item), nil
}

func withAlertIntegrationRuntimeFields(item domainalert.AlertIntegration) domainalert.AlertIntegration {
	item.TokenPreview = previewSecret(item.Token)
	item.WebhookPath = "/api/v1/integrations/alerts/" + item.ID + "/webhook"
	return item
}

func previewSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 10 {
		return "****"
	}
	return value[:6] + "..." + value[len(value)-4:]
}
