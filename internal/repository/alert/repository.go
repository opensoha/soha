package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainalert "github.com/soha/soha/internal/domain/alert"
	"gorm.io/gorm"
)

type Repository struct {
	db              *gorm.DB
	upsertBatchSize int
}

const alertUpsertBatchSize = 100

func New(db *gorm.DB) *Repository {
	return &Repository{db: db, upsertBatchSize: alertUpsertBatchSize}
}

func (r *Repository) SetUpsertBatchSize(size int) {
	if size > 0 {
		r.upsertBatchSize = size
	}
}

func (r *Repository) Upsert(ctx context.Context, source string, alerts []domainalert.IngestAlert) ([]domainalert.Instance, error) {
	if len(alerts) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	instances := make([]domainalert.Instance, 0, len(alerts))
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		batchSize := r.upsertBatchSize
		if batchSize <= 0 {
			batchSize = alertUpsertBatchSize
		}
		for start := 0; start < len(alerts); start += batchSize {
			end := start + batchSize
			if end > len(alerts) {
				end = len(alerts)
			}
			batch := make([]domainalert.Instance, 0, end-start)
			for _, alert := range alerts[start:end] {
				batch = append(batch, normalizeAlert(source, alert, now))
			}
			if err := upsertAlertBatch(tx, batch); err != nil {
				return err
			}
			instances = append(instances, batch...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return instances, nil
}

func upsertAlertBatch(tx *gorm.DB, batch []domainalert.Instance) error {
	if len(batch) == 0 {
		return nil
	}

	var builder strings.Builder
	args := make([]any, 0, len(batch)*18)
	builder.WriteString(`
		INSERT INTO alert_instances (
			id, source, fingerprint, title, summary, severity, status, cluster_id, namespace,
			labels, annotations, receiver, generator_url, starts_at, ends_at, last_seen_at, created_at, updated_at
		) VALUES
	`)
	for index, instance := range batch {
		if index > 0 {
			builder.WriteString(",")
		}
		labels, err := json.Marshal(instance.Labels)
		if err != nil {
			return fmt.Errorf("marshal alert labels: %w", err)
		}
		annotations, err := json.Marshal(instance.Annotations)
		if err != nil {
			return fmt.Errorf("marshal alert annotations: %w", err)
		}
		builder.WriteString(`
			(?, ?, ?, ?, ?, ?, ?, ?, ?,
			 ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		args = append(args,
			instance.ID,
			instance.Source,
			instance.Fingerprint,
			instance.Title,
			instance.Summary,
			instance.Severity,
			instance.Status,
			instance.ClusterID,
			instance.Namespace,
			string(labels),
			string(annotations),
			instance.Receiver,
			instance.GeneratorURL,
			nullableTime(instance.StartsAt),
			nullableTime(instance.EndsAt),
			instance.LastSeenAt,
			instance.CreatedAt,
			instance.UpdatedAt,
		)
	}
	builder.WriteString(`
		ON CONFLICT (source, fingerprint) DO UPDATE SET
			title = EXCLUDED.title,
			summary = EXCLUDED.summary,
			severity = EXCLUDED.severity,
			status = EXCLUDED.status,
			cluster_id = EXCLUDED.cluster_id,
			namespace = EXCLUDED.namespace,
			labels = EXCLUDED.labels,
			annotations = EXCLUDED.annotations,
			receiver = EXCLUDED.receiver,
			generator_url = EXCLUDED.generator_url,
			starts_at = EXCLUDED.starts_at,
			ends_at = EXCLUDED.ends_at,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = EXCLUDED.updated_at
	`)
	if err := tx.Exec(builder.String(), args...).Error; err != nil {
		return fmt.Errorf("upsert alert batch: %w", err)
	}
	return nil
}

func (r *Repository) List(ctx context.Context, filter domainalert.Filter) ([]domainalert.Instance, error) {
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
	if strings.TrimSpace(filter.ClusterID) != "" {
		conditions = append(conditions, "cluster_id = ?")
		args = append(args, strings.TrimSpace(filter.ClusterID))
	}
	query := `
		SELECT id, source, fingerprint, title, summary, severity, status, cluster_id, namespace,
			labels, annotations, receiver, generator_url, starts_at, ends_at, last_seen_at, owner_team, assignee, acknowledged_at, acknowledged_by, acknowledged_by_name, created_at, updated_at
		FROM alert_instances
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY last_seen_at DESC, updated_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query alerts: %w", err)
	}
	defer rows.Close()

	items := make([]domainalert.Instance, 0, limit)
	for rows.Next() {
		item, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, alertID string) (domainalert.Instance, error) {
	alertID = strings.TrimSpace(alertID)
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, source, fingerprint, title, summary, severity, status, cluster_id, namespace,
			labels, annotations, receiver, generator_url, starts_at, ends_at, last_seen_at, owner_team, assignee, acknowledged_at, acknowledged_by, acknowledged_by_name, created_at, updated_at
		FROM alert_instances
		WHERE id = ?
		LIMIT 1
	`, alertID).Row()
	return scanInstanceRowWithID(row, alertID)
}

func (r *Repository) UpdateOwnership(ctx context.Context, alertID string, input domainalert.OwnershipInput) (domainalert.Instance, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE alert_instances
		SET owner_team = ?, assignee = ?, updated_at = ?
		WHERE id = ?
	`, nullableString(strings.TrimSpace(input.OwnerTeam)), nullableString(strings.TrimSpace(input.Assignee)), time.Now().UTC(), strings.TrimSpace(alertID))
	if result.Error != nil {
		return domainalert.Instance{}, fmt.Errorf("update alert ownership: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.Instance{}, fmt.Errorf("alert not found: %s", strings.TrimSpace(alertID))
	}
	return r.Get(ctx, alertID)
}

func (r *Repository) Acknowledge(ctx context.Context, alertID, acknowledgedBy, acknowledgedByName string) (domainalert.Instance, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE alert_instances
		SET acknowledged_at = ?, acknowledged_by = ?, acknowledged_by_name = ?, updated_at = ?
		WHERE id = ?
	`, now, nullableString(strings.TrimSpace(acknowledgedBy)), nullableString(strings.TrimSpace(acknowledgedByName)), now, strings.TrimSpace(alertID))
	if result.Error != nil {
		return domainalert.Instance{}, fmt.Errorf("acknowledge alert: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.Instance{}, fmt.Errorf("alert not found: %s", strings.TrimSpace(alertID))
	}
	return r.Get(ctx, alertID)
}

func (r *Repository) Summary(ctx context.Context) (domainalert.Summary, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) AS total_count,
			COALESCE(SUM(CASE WHEN status = 'firing' THEN 1 ELSE 0 END), 0) AS firing_count,
			COALESCE(SUM(CASE WHEN status = 'resolved' THEN 1 ELSE 0 END), 0) AS resolved_count,
			COALESCE(SUM(CASE WHEN status = 'firing' AND severity = 'critical' THEN 1 ELSE 0 END), 0) AS critical_count,
			COALESCE(SUM(CASE WHEN status = 'firing' AND severity = 'warning' THEN 1 ELSE 0 END), 0) AS warning_count,
			COALESCE(SUM(CASE WHEN status = 'firing' AND severity = 'info' THEN 1 ELSE 0 END), 0) AS info_count,
			MAX(last_seen_at) AS last_received_at
		FROM alert_instances
	`).Row()

	var summary domainalert.Summary
	var lastReceivedAt sql.NullTime
	if err := row.Scan(
		&summary.TotalCount,
		&summary.FiringCount,
		&summary.ResolvedCount,
		&summary.CriticalCount,
		&summary.WarningCount,
		&summary.InfoCount,
		&lastReceivedAt,
	); err != nil {
		return domainalert.Summary{}, fmt.Errorf("query alert summary: %w", err)
	}
	if lastReceivedAt.Valid {
		summary.LastReceivedAt = lastReceivedAt.Time
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM notification_channels WHERE enabled = TRUE`).Row().Scan(&summary.ChannelCount); err != nil {
		return domainalert.Summary{}, fmt.Errorf("query notification channel count: %w", err)
	}
	return summary, nil
}

func (r *Repository) ListChannels(ctx context.Context) ([]domainalert.NotificationChannel, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, channel_type, enabled, config, created_at, updated_at
		FROM notification_channels
		ORDER BY name ASC, id ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query notification channels: %w", err)
	}
	defer rows.Close()

	items := make([]domainalert.NotificationChannel, 0)
	for rows.Next() {
		var item domainalert.NotificationChannel
		var config []byte
		if err := rows.Scan(&item.ID, &item.Name, &item.ChannelType, &item.Enabled, &config, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan notification channel: %w", err)
		}
		if len(config) > 0 {
			_ = json.Unmarshal(config, &item.Config)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateChannel(ctx context.Context, input domainalert.ChannelInput) (domainalert.NotificationChannel, error) {
	channel := normalizeChannelInput(input, time.Now().UTC())
	config, err := json.Marshal(channel.Config)
	if err != nil {
		return domainalert.NotificationChannel{}, fmt.Errorf("marshal notification channel config: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO notification_channels (id, name, channel_type, config, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, channel.ID, channel.Name, channel.ChannelType, string(config), channel.Enabled, channel.CreatedAt, channel.UpdatedAt).Error; err != nil {
		return domainalert.NotificationChannel{}, fmt.Errorf("create notification channel: %w", err)
	}
	return channel, nil
}

func (r *Repository) UpdateChannel(ctx context.Context, channelID string, input domainalert.ChannelInput) (domainalert.NotificationChannel, error) {
	now := time.Now().UTC()
	channel := normalizeChannelInput(input, now)
	channel.ID = strings.TrimSpace(channelID)
	config, err := json.Marshal(channel.Config)
	if err != nil {
		return domainalert.NotificationChannel{}, fmt.Errorf("marshal notification channel config: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE notification_channels
		SET name = ?, channel_type = ?, config = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, channel.Name, channel.ChannelType, string(config), channel.Enabled, channel.UpdatedAt, channel.ID)
	if result.Error != nil {
		return domainalert.NotificationChannel{}, fmt.Errorf("update notification channel: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.NotificationChannel{}, fmt.Errorf("notification channel not found: %s", channel.ID)
	}
	channel.CreatedAt = fetchCreatedAt(ctx, r.db, channel.ID)
	return channel, nil
}

func (r *Repository) ListRoutes(ctx context.Context) ([]domainalert.AlertRoute, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, matchers, channel_ids, enabled, created_at, updated_at
		FROM alert_routes
		ORDER BY name ASC, id ASC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query alert routes: %w", err)
	}
	defer rows.Close()

	items := make([]domainalert.AlertRoute, 0)
	for rows.Next() {
		var item domainalert.AlertRoute
		var matchers []byte
		var channelIDs []byte
		if err := rows.Scan(&item.ID, &item.Name, &matchers, &channelIDs, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan alert route: %w", err)
		}
		if len(matchers) > 0 {
			_ = json.Unmarshal(matchers, &item.Matchers)
		}
		if len(channelIDs) > 0 {
			_ = json.Unmarshal(channelIDs, &item.ChannelIDs)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateRoute(ctx context.Context, input domainalert.RouteInput) (domainalert.AlertRoute, error) {
	route := normalizeRouteInput(input, time.Now().UTC())
	matchers, err := json.Marshal(route.Matchers)
	if err != nil {
		return domainalert.AlertRoute{}, fmt.Errorf("marshal route matchers: %w", err)
	}
	channelIDs, err := json.Marshal(route.ChannelIDs)
	if err != nil {
		return domainalert.AlertRoute{}, fmt.Errorf("marshal route channel ids: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO alert_routes (id, name, matchers, channel_ids, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, route.ID, route.Name, string(matchers), string(channelIDs), route.Enabled, route.CreatedAt, route.UpdatedAt).Error; err != nil {
		return domainalert.AlertRoute{}, fmt.Errorf("create alert route: %w", err)
	}
	return route, nil
}

func (r *Repository) UpdateRoute(ctx context.Context, routeID string, input domainalert.RouteInput) (domainalert.AlertRoute, error) {
	now := time.Now().UTC()
	route := normalizeRouteInput(input, now)
	route.ID = strings.TrimSpace(routeID)
	matchers, err := json.Marshal(route.Matchers)
	if err != nil {
		return domainalert.AlertRoute{}, fmt.Errorf("marshal route matchers: %w", err)
	}
	channelIDs, err := json.Marshal(route.ChannelIDs)
	if err != nil {
		return domainalert.AlertRoute{}, fmt.Errorf("marshal route channel ids: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE alert_routes
		SET name = ?, matchers = ?, channel_ids = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, route.Name, string(matchers), string(channelIDs), route.Enabled, route.UpdatedAt, route.ID)
	if result.Error != nil {
		return domainalert.AlertRoute{}, fmt.Errorf("update alert route: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.AlertRoute{}, fmt.Errorf("alert route not found: %s", route.ID)
	}
	route.CreatedAt = fetchRouteCreatedAt(ctx, r.db, route.ID)
	return route, nil
}

func (r *Repository) CreateDeliveryLog(ctx context.Context, item domainalert.DeliveryLog) error {
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return fmt.Errorf("marshal alert delivery metadata: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO alert_delivery_logs (id, alert_id, channel_id, status, summary, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.AlertID, nullableString(item.ChannelID), item.Status, nullableString(item.Summary), string(metadata), item.CreatedAt).Error
}

func (r *Repository) ListDeliveryLogs(ctx context.Context, filter domainalert.DeliveryFilter) ([]domainalert.DeliveryLog, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	args := []any{}
	conditions := []string{}
	if strings.TrimSpace(filter.AlertID) != "" {
		conditions = append(conditions, "alert_id = ?")
		args = append(args, strings.TrimSpace(filter.AlertID))
	}
	if strings.TrimSpace(filter.Status) != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Status)))
	}
	query := `
		SELECT id, alert_id, channel_id, status, summary, metadata, created_at
		FROM alert_delivery_logs
	`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query alert delivery logs: %w", err)
	}
	defer rows.Close()

	items := make([]domainalert.DeliveryLog, 0, limit)
	for rows.Next() {
		var item domainalert.DeliveryLog
		var channelID sql.NullString
		var summary sql.NullString
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.AlertID, &channelID, &item.Status, &summary, &metadata, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alert delivery log: %w", err)
		}
		if channelID.Valid {
			item.ChannelID = channelID.String
		}
		if summary.Valid {
			item.Summary = summary.String
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &item.Metadata)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListSilences(ctx context.Context) ([]domainalert.AlertSilence, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, matchers, reason, starts_at, ends_at, enabled, created_at, updated_at
		FROM alert_silences
		ORDER BY starts_at DESC, created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query alert silences: %w", err)
	}
	defer rows.Close()

	items := make([]domainalert.AlertSilence, 0)
	for rows.Next() {
		var item domainalert.AlertSilence
		var matchers []byte
		var reason sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &matchers, &reason, &item.StartsAt, &item.EndsAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan alert silence: %w", err)
		}
		if len(matchers) > 0 {
			_ = json.Unmarshal(matchers, &item.Matchers)
		}
		if reason.Valid {
			item.Reason = reason.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateSilence(ctx context.Context, input domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	item := normalizeSilenceInput(input, time.Now().UTC())
	matchers, err := json.Marshal(item.Matchers)
	if err != nil {
		return domainalert.AlertSilence{}, fmt.Errorf("marshal silence matchers: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO alert_silences (id, name, matchers, reason, starts_at, ends_at, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, string(matchers), nullableString(item.Reason), item.StartsAt, item.EndsAt, item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainalert.AlertSilence{}, fmt.Errorf("create alert silence: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateSilence(ctx context.Context, silenceID string, input domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	item := normalizeSilenceInput(input, time.Now().UTC())
	item.ID = strings.TrimSpace(silenceID)
	matchers, err := json.Marshal(item.Matchers)
	if err != nil {
		return domainalert.AlertSilence{}, fmt.Errorf("marshal silence matchers: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE alert_silences
		SET name = ?, matchers = ?, reason = ?, starts_at = ?, ends_at = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, string(matchers), nullableString(item.Reason), item.StartsAt, item.EndsAt, item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainalert.AlertSilence{}, fmt.Errorf("update alert silence: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainalert.AlertSilence{}, fmt.Errorf("alert silence not found: %s", item.ID)
	}
	item.CreatedAt = fetchSilenceCreatedAt(ctx, r.db, item.ID)
	return item, nil
}

func normalizeAlert(source string, alert domainalert.IngestAlert, now time.Time) domainalert.Instance {
	normalizedSource := strings.ToLower(strings.TrimSpace(source))
	if normalizedSource == "" {
		normalizedSource = "platform"
	}
	fingerprint := strings.TrimSpace(alert.Fingerprint)
	if fingerprint == "" {
		fingerprint = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(alert.Title)+":"+strings.TrimSpace(alert.ClusterID)+":"+strings.TrimSpace(alert.Namespace), " ", "-"))
	}
	severity := strings.ToLower(strings.TrimSpace(alert.Severity))
	if severity == "" {
		severity = "warning"
	}
	status := strings.ToLower(strings.TrimSpace(alert.Status))
	if status == "" {
		status = "firing"
	}
	labels := alert.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	annotations := alert.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	return domainalert.Instance{
		ID:           normalizedSource + ":" + fingerprint,
		Source:       normalizedSource,
		Fingerprint:  fingerprint,
		Title:        strings.TrimSpace(alert.Title),
		Summary:      strings.TrimSpace(alert.Summary),
		Severity:     severity,
		Status:       status,
		ClusterID:    strings.TrimSpace(alert.ClusterID),
		Namespace:    strings.TrimSpace(alert.Namespace),
		Labels:       labels,
		Annotations:  annotations,
		Receiver:     strings.TrimSpace(alert.Receiver),
		GeneratorURL: strings.TrimSpace(alert.GeneratorURL),
		StartsAt:     alert.StartsAt,
		EndsAt:       alert.EndsAt,
		LastSeenAt:   now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func scanInstance(rows *sql.Rows) (domainalert.Instance, error) {
	var item domainalert.Instance
	var labels []byte
	var annotations []byte
	var startsAt sql.NullTime
	var endsAt sql.NullTime
	var ownerTeam sql.NullString
	var assignee sql.NullString
	var acknowledgedAt sql.NullTime
	var acknowledgedBy sql.NullString
	var acknowledgedByName sql.NullString
	if err := rows.Scan(
		&item.ID,
		&item.Source,
		&item.Fingerprint,
		&item.Title,
		&item.Summary,
		&item.Severity,
		&item.Status,
		&item.ClusterID,
		&item.Namespace,
		&labels,
		&annotations,
		&item.Receiver,
		&item.GeneratorURL,
		&startsAt,
		&endsAt,
		&item.LastSeenAt,
		&ownerTeam,
		&assignee,
		&acknowledgedAt,
		&acknowledgedBy,
		&acknowledgedByName,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainalert.Instance{}, fmt.Errorf("scan alert instance: %w", err)
	}
	if len(labels) > 0 {
		_ = json.Unmarshal(labels, &item.Labels)
	}
	if len(annotations) > 0 {
		_ = json.Unmarshal(annotations, &item.Annotations)
	}
	if startsAt.Valid {
		item.StartsAt = startsAt.Time
	}
	if endsAt.Valid {
		item.EndsAt = endsAt.Time
	}
	if ownerTeam.Valid {
		item.OwnerTeam = ownerTeam.String
	}
	if assignee.Valid {
		item.Assignee = assignee.String
	}
	if acknowledgedAt.Valid {
		item.AcknowledgedAt = acknowledgedAt.Time
	}
	if acknowledgedBy.Valid {
		item.AcknowledgedBy = acknowledgedBy.String
	}
	if acknowledgedByName.Valid {
		item.AcknowledgedByName = acknowledgedByName.String
	}
	return item, nil
}

func scanInstanceRowWithID(row *sql.Row, alertID string) (domainalert.Instance, error) {
	var item domainalert.Instance
	var labels []byte
	var annotations []byte
	var startsAt sql.NullTime
	var endsAt sql.NullTime
	var ownerTeam sql.NullString
	var assignee sql.NullString
	var acknowledgedAt sql.NullTime
	var acknowledgedBy sql.NullString
	var acknowledgedByName sql.NullString
	if err := row.Scan(
		&item.ID,
		&item.Source,
		&item.Fingerprint,
		&item.Title,
		&item.Summary,
		&item.Severity,
		&item.Status,
		&item.ClusterID,
		&item.Namespace,
		&labels,
		&annotations,
		&item.Receiver,
		&item.GeneratorURL,
		&startsAt,
		&endsAt,
		&item.LastSeenAt,
		&ownerTeam,
		&assignee,
		&acknowledgedAt,
		&acknowledgedBy,
		&acknowledgedByName,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return domainalert.Instance{}, fmt.Errorf("alert not found: %s", alertID)
		}
		return domainalert.Instance{}, fmt.Errorf("scan alert instance row: %w", err)
	}
	if len(labels) > 0 {
		_ = json.Unmarshal(labels, &item.Labels)
	}
	if len(annotations) > 0 {
		_ = json.Unmarshal(annotations, &item.Annotations)
	}
	if startsAt.Valid {
		item.StartsAt = startsAt.Time
	}
	if endsAt.Valid {
		item.EndsAt = endsAt.Time
	}
	if ownerTeam.Valid {
		item.OwnerTeam = ownerTeam.String
	}
	if assignee.Valid {
		item.Assignee = assignee.String
	}
	if acknowledgedAt.Valid {
		item.AcknowledgedAt = acknowledgedAt.Time
	}
	if acknowledgedBy.Valid {
		item.AcknowledgedBy = acknowledgedBy.String
	}
	if acknowledgedByName.Valid {
		item.AcknowledgedByName = acknowledgedByName.String
	}
	return item, nil
}

func normalizeChannelInput(input domainalert.ChannelInput, now time.Time) domainalert.NotificationChannel {
	config := input.Config
	if config == nil {
		config = map[string]any{}
	}
	channelType := strings.ToLower(strings.TrimSpace(input.ChannelType))
	if channelType == "" {
		channelType = "webhook"
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(input.Name), " ", "-"))
	}
	return domainalert.NotificationChannel{
		ID:          id,
		Name:        strings.TrimSpace(input.Name),
		ChannelType: channelType,
		Enabled:     input.Enabled,
		Config:      config,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func normalizeRouteInput(input domainalert.RouteInput, now time.Time) domainalert.AlertRoute {
	matchers := input.Matchers
	if matchers == nil {
		matchers = map[string]any{}
	}
	channelIDs := input.ChannelIDs
	if channelIDs == nil {
		channelIDs = []string{}
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(input.Name), " ", "-"))
	}
	return domainalert.AlertRoute{
		ID:         id,
		Name:       strings.TrimSpace(input.Name),
		Matchers:   matchers,
		ChannelIDs: channelIDs,
		Enabled:    input.Enabled,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func normalizeSilenceInput(input domainalert.SilenceInput, now time.Time) domainalert.AlertSilence {
	matchers := input.Matchers
	if matchers == nil {
		matchers = map[string]any{}
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(input.Name), " ", "-"))
	}
	return domainalert.AlertSilence{
		ID:        id,
		Name:      strings.TrimSpace(input.Name),
		Matchers:  matchers,
		Reason:    strings.TrimSpace(input.Reason),
		StartsAt:  input.StartsAt.UTC(),
		EndsAt:    input.EndsAt.UTC(),
		Enabled:   input.Enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func fetchCreatedAt(ctx context.Context, db *gorm.DB, channelID string) time.Time {
	var createdAt time.Time
	if err := db.WithContext(ctx).Raw(`SELECT created_at FROM notification_channels WHERE id = ?`, channelID).Row().Scan(&createdAt); err != nil {
		return time.Time{}
	}
	return createdAt
}

func fetchRouteCreatedAt(ctx context.Context, db *gorm.DB, routeID string) time.Time {
	var createdAt time.Time
	if err := db.WithContext(ctx).Raw(`SELECT created_at FROM alert_routes WHERE id = ?`, routeID).Row().Scan(&createdAt); err != nil {
		return time.Time{}
	}
	return createdAt
}

func fetchSilenceCreatedAt(ctx context.Context, db *gorm.DB, silenceID string) time.Time {
	var createdAt time.Time
	if err := db.WithContext(ctx).Raw(`SELECT created_at FROM alert_silences WHERE id = ?`, silenceID).Row().Scan(&createdAt); err != nil {
		return time.Time{}
	}
	return createdAt
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}
