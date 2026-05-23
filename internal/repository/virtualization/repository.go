package virtualization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainvirtualization "github.com/kubecrux/kubecrux/internal/domain/virtualization"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("virtualization record not found")

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateConnection(ctx context.Context, input domainvirtualization.ConnectionInput) (domainvirtualization.Connection, error) {
	item := connectionFromInput(input)
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	if err := r.saveConnection(ctx, item, true); err != nil {
		return domainvirtualization.Connection{}, err
	}
	return item, nil
}

func (r *Repository) UpdateConnection(ctx context.Context, id string, input domainvirtualization.ConnectionInput) (domainvirtualization.Connection, error) {
	item := connectionFromInput(input)
	item.ID = strings.TrimSpace(id)
	item.UpdatedAt = time.Now().UTC()
	if err := r.saveConnection(ctx, item, false); err != nil {
		return domainvirtualization.Connection{}, err
	}
	return r.GetConnection(ctx, item.ID)
}

func (r *Repository) DeleteConnection(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM virtualization_connections WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete virtualization connection: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) GetConnection(ctx context.Context, id string) (domainvirtualization.Connection, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, provider, name, endpoint, kubernetes_cluster_id, default_namespace, enabled, verify_tls,
		       encrypted_credential, config, health, last_synced_at, created_at, updated_at
		FROM virtualization_connections
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanConnectionRow(row)
}

func (r *Repository) ListConnections(ctx context.Context, filter domainvirtualization.ConnectionFilter) ([]domainvirtualization.Connection, error) {
	limit, offset := limitOffset(filter.Limit, filter.Page, filter.PageSize)
	query := `
		SELECT id, provider, name, endpoint, kubernetes_cluster_id, default_namespace, enabled, verify_tls,
		       encrypted_credential, config, health, last_synced_at, created_at, updated_at
		FROM virtualization_connections
	`
	clauses, args := connectionClauses(filter)
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query virtualization connections: %w", err)
	}
	defer rows.Close()

	items := make([]domainvirtualization.Connection, 0, limit)
	for rows.Next() {
		item, scanErr := scanConnection(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountConnections(ctx context.Context, filter domainvirtualization.ConnectionFilter) (int, error) {
	clauses, args := connectionClauses(filter)
	return r.count(ctx, "virtualization_connections", clauses, args)
}

func (r *Repository) UpsertVM(ctx context.Context, item domainvirtualization.VM) (domainvirtualization.VM, error) {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.Status == "" {
		item.Status = "active"
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.LastSeenAt == nil {
		item.LastSeenAt = &now
	}
	ipAddresses, err := marshalJSONDefault(item.IPAddresses, []string{})
	if err != nil {
		return domainvirtualization.VM{}, fmt.Errorf("marshal virtualization vm ip addresses: %w", err)
	}
	labels, err := marshalJSON(item.Labels)
	if err != nil {
		return domainvirtualization.VM{}, fmt.Errorf("marshal virtualization vm labels: %w", err)
	}
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domainvirtualization.VM{}, fmt.Errorf("marshal virtualization vm config: %w", err)
	}
	raw, err := marshalJSON(item.Raw)
	if err != nil {
		return domainvirtualization.VM{}, fmt.Errorf("marshal virtualization vm raw: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO virtualization_vms (
			id, provider, connection_id, external_id, name, namespace, status, power_state, node_name,
			image_id, flavor_id, ip_addresses, labels, config, raw, last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (provider, connection_id, external_id) DO UPDATE SET
			name = EXCLUDED.name,
			namespace = EXCLUDED.namespace,
			status = EXCLUDED.status,
			power_state = EXCLUDED.power_state,
			node_name = EXCLUDED.node_name,
			image_id = EXCLUDED.image_id,
			flavor_id = EXCLUDED.flavor_id,
			ip_addresses = EXCLUDED.ip_addresses,
			labels = EXCLUDED.labels,
			config = EXCLUDED.config,
			raw = EXCLUDED.raw,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = EXCLUDED.updated_at
	`, item.ID, item.Provider, item.ConnectionID, item.ExternalID, item.Name, nullableString(item.Namespace), item.Status, nullableString(item.PowerState), nullableString(item.NodeName), nullableString(item.ImageID), nullableString(item.FlavorID), string(ipAddresses), string(labels), string(config), string(raw), item.LastSeenAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainvirtualization.VM{}, fmt.Errorf("upsert virtualization vm: %w", err)
	}
	return r.getVMByExternalKey(ctx, item.Provider, item.ConnectionID, item.ExternalID)
}

func (r *Repository) GetVM(ctx context.Context, id string) (domainvirtualization.VM, error) {
	row := r.db.WithContext(ctx).Raw(vmSelect()+` WHERE id = ? LIMIT 1`, strings.TrimSpace(id)).Row()
	return scanVMRow(row)
}

func (r *Repository) ListVMs(ctx context.Context, filter domainvirtualization.VMFilter) ([]domainvirtualization.VM, error) {
	query, args, limit := buildAssetListQuery(vmSelect(), "virtualization_vms", filter.Provider, filter.ConnectionID, filter.Status, filter.Search, []string{"name", "external_id", "namespace", "node_name"}, filter.Limit, filter.Page, filter.PageSize)
	clauses, extraArgs := vmExtraClauses(filter)
	query, args = injectExtraClauses(query, args, clauses, extraArgs)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	return scanVMList(rows, err, limit)
}

func (r *Repository) CountVMs(ctx context.Context, filter domainvirtualization.VMFilter) (int, error) {
	clauses, args := assetClauses(filter.Provider, filter.ConnectionID, filter.Status, filter.Search, []string{"name", "external_id", "namespace", "node_name"})
	extraClauses, extraArgs := vmExtraClauses(filter)
	clauses = append(clauses, extraClauses...)
	args = append(args, extraArgs...)
	return r.count(ctx, "virtualization_vms", clauses, args)
}

func (r *Repository) UpsertImage(ctx context.Context, item domainvirtualization.Image) (domainvirtualization.Image, error) {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.Status == "" {
		item.Status = "active"
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.LastSeenAt == nil {
		item.LastSeenAt = &now
	}
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domainvirtualization.Image{}, fmt.Errorf("marshal virtualization image config: %w", err)
	}
	raw, err := marshalJSON(item.Raw)
	if err != nil {
		return domainvirtualization.Image{}, fmt.Errorf("marshal virtualization image raw: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO virtualization_images (id, provider, connection_id, external_id, name, status, os_type, architecture, size_bytes, config, raw, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (provider, connection_id, external_id) DO UPDATE SET
			name = EXCLUDED.name,
			status = EXCLUDED.status,
			os_type = EXCLUDED.os_type,
			architecture = EXCLUDED.architecture,
			size_bytes = EXCLUDED.size_bytes,
			config = EXCLUDED.config,
			raw = EXCLUDED.raw,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = EXCLUDED.updated_at
	`, item.ID, item.Provider, item.ConnectionID, item.ExternalID, item.Name, item.Status, nullableString(item.OSType), nullableString(item.Architecture), item.SizeBytes, string(config), string(raw), item.LastSeenAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainvirtualization.Image{}, fmt.Errorf("upsert virtualization image: %w", err)
	}
	return r.getImageByExternalKey(ctx, item.Provider, item.ConnectionID, item.ExternalID)
}

func (r *Repository) ListImages(ctx context.Context, filter domainvirtualization.ImageFilter) ([]domainvirtualization.Image, error) {
	query, args, limit := buildAssetListQuery(imageSelect(), "virtualization_images", filter.Provider, filter.ConnectionID, filter.Status, filter.Search, []string{"name", "external_id", "os_type", "architecture"}, filter.Limit, filter.Page, filter.PageSize)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query virtualization images: %w", err)
	}
	defer rows.Close()
	items := make([]domainvirtualization.Image, 0, limit)
	for rows.Next() {
		item, scanErr := scanImage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetImage(ctx context.Context, id string) (domainvirtualization.Image, error) {
	row := r.db.WithContext(ctx).Raw(imageSelect()+` WHERE id = ? LIMIT 1`, strings.TrimSpace(id)).Row()
	return scanImageRow(row)
}

func (r *Repository) CountImages(ctx context.Context, filter domainvirtualization.ImageFilter) (int, error) {
	clauses, args := assetClauses(filter.Provider, filter.ConnectionID, filter.Status, filter.Search, []string{"name", "external_id", "os_type", "architecture"})
	return r.count(ctx, "virtualization_images", clauses, args)
}

func (r *Repository) UpsertFlavor(ctx context.Context, item domainvirtualization.Flavor) (domainvirtualization.Flavor, error) {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.Status == "" {
		item.Status = "active"
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.LastSeenAt == nil {
		item.LastSeenAt = &now
	}
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domainvirtualization.Flavor{}, fmt.Errorf("marshal virtualization flavor config: %w", err)
	}
	raw, err := marshalJSON(item.Raw)
	if err != nil {
		return domainvirtualization.Flavor{}, fmt.Errorf("marshal virtualization flavor raw: %w", err)
	}
	connectionID := strings.TrimSpace(item.ConnectionID)
	if connectionID == "" {
		if err := r.db.WithContext(ctx).Exec(`
			INSERT INTO virtualization_flavors (id, provider, connection_id, external_id, name, status, cpu_cores, memory_mb, disk_gb, config, raw, last_seen_at, created_at, updated_at)
			VALUES (?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (provider, external_id) WHERE connection_id IS NULL DO UPDATE SET
				name = EXCLUDED.name,
				status = EXCLUDED.status,
				cpu_cores = EXCLUDED.cpu_cores,
				memory_mb = EXCLUDED.memory_mb,
				disk_gb = EXCLUDED.disk_gb,
				config = EXCLUDED.config,
				raw = EXCLUDED.raw,
				last_seen_at = EXCLUDED.last_seen_at,
				updated_at = EXCLUDED.updated_at
		`, item.ID, item.Provider, item.ExternalID, item.Name, item.Status, item.CPUCores, item.MemoryMB, item.DiskGB, string(config), string(raw), item.LastSeenAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return domainvirtualization.Flavor{}, fmt.Errorf("upsert global virtualization flavor: %w", err)
		}
		return r.getFlavorByExternalKey(ctx, item.Provider, "", item.ExternalID)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO virtualization_flavors (id, provider, connection_id, external_id, name, status, cpu_cores, memory_mb, disk_gb, config, raw, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (provider, connection_id, external_id) WHERE connection_id IS NOT NULL DO UPDATE SET
			name = EXCLUDED.name,
			status = EXCLUDED.status,
			cpu_cores = EXCLUDED.cpu_cores,
			memory_mb = EXCLUDED.memory_mb,
			disk_gb = EXCLUDED.disk_gb,
			config = EXCLUDED.config,
			raw = EXCLUDED.raw,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = EXCLUDED.updated_at
	`, item.ID, item.Provider, connectionID, item.ExternalID, item.Name, item.Status, item.CPUCores, item.MemoryMB, item.DiskGB, string(config), string(raw), item.LastSeenAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainvirtualization.Flavor{}, fmt.Errorf("upsert virtualization flavor: %w", err)
	}
	return r.getFlavorByExternalKey(ctx, item.Provider, connectionID, item.ExternalID)
}

func (r *Repository) GetFlavor(ctx context.Context, id string) (domainvirtualization.Flavor, error) {
	row := r.db.WithContext(ctx).Raw(flavorSelect()+` WHERE id = ? LIMIT 1`, strings.TrimSpace(id)).Row()
	return scanFlavorRow(row)
}

func (r *Repository) ListFlavors(ctx context.Context, filter domainvirtualization.FlavorFilter) ([]domainvirtualization.Flavor, error) {
	query, args, limit := buildAssetListQuery(flavorSelect(), "virtualization_flavors", filter.Provider, filter.ConnectionID, filter.Status, filter.Search, []string{"name", "external_id"}, filter.Limit, filter.Page, filter.PageSize)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query virtualization flavors: %w", err)
	}
	defer rows.Close()
	items := make([]domainvirtualization.Flavor, 0, limit)
	for rows.Next() {
		item, scanErr := scanFlavor(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountFlavors(ctx context.Context, filter domainvirtualization.FlavorFilter) (int, error) {
	clauses, args := assetClauses(filter.Provider, filter.ConnectionID, filter.Status, filter.Search, []string{"name", "external_id"})
	return r.count(ctx, "virtualization_flavors", clauses, args)
}

func (r *Repository) CreateTask(ctx context.Context, item domainvirtualization.Task) (domainvirtualization.Task, error) {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.MaxRetries == 0 {
		item.MaxRetries = 1
	}
	if item.TimeoutSeconds == 0 {
		item.TimeoutSeconds = 1800
	}
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	payload, err := marshalJSON(item.Payload)
	if err != nil {
		return domainvirtualization.Task{}, fmt.Errorf("marshal virtualization task payload: %w", err)
	}
	result, err := marshalJSON(item.Result)
	if err != nil {
		return domainvirtualization.Task{}, fmt.Errorf("marshal virtualization task result: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO virtualization_tasks (
			id, provider, connection_id, vm_id, task_kind, status, requested_by, claimed_by_worker_id,
			attempt_count, max_retries, timeout_seconds, payload, result, started_at, last_heartbeat_at,
			finished_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Provider, nullableString(item.ConnectionID), nullableString(item.VMID), item.TaskKind, item.Status, nullableString(item.RequestedBy), nullableString(item.ClaimedByWorkerID), item.AttemptCount, item.MaxRetries, item.TimeoutSeconds, string(payload), string(result), item.StartedAt, item.LastHeartbeatAt, item.FinishedAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainvirtualization.Task{}, fmt.Errorf("create virtualization task: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateTask(ctx context.Context, item domainvirtualization.Task) (domainvirtualization.Task, error) {
	if item.MaxRetries == 0 {
		item.MaxRetries = 1
	}
	if item.TimeoutSeconds == 0 {
		item.TimeoutSeconds = 1800
	}
	item.UpdatedAt = time.Now().UTC()
	payload, err := marshalJSON(item.Payload)
	if err != nil {
		return domainvirtualization.Task{}, fmt.Errorf("marshal virtualization task payload: %w", err)
	}
	resultPayload, err := marshalJSON(item.Result)
	if err != nil {
		return domainvirtualization.Task{}, fmt.Errorf("marshal virtualization task result: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE virtualization_tasks
		SET status = ?, claimed_by_worker_id = ?, attempt_count = ?, max_retries = ?, timeout_seconds = ?,
		    result = ?, payload = ?, started_at = ?, last_heartbeat_at = ?, finished_at = ?, updated_at = ?
		WHERE id = ?
	`, item.Status, nullableString(item.ClaimedByWorkerID), item.AttemptCount, item.MaxRetries, item.TimeoutSeconds, string(resultPayload), string(payload), item.StartedAt, item.LastHeartbeatAt, item.FinishedAt, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainvirtualization.Task{}, fmt.Errorf("update virtualization task: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainvirtualization.Task{}, ErrNotFound
	}
	return r.GetTask(ctx, item.ID)
}

func (r *Repository) ClaimTask(ctx context.Context, workerID string, now time.Time) (domainvirtualization.Task, error) {
	task := domainvirtualization.Task{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := tx.Raw(taskSelect() + `
			WHERE status = 'queued'
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		`).Row()
		item, scanErr := scanTaskRow(row)
		if scanErr != nil {
			return scanErr
		}
		item.Status = "running"
		item.ClaimedByWorkerID = strings.TrimSpace(workerID)
		item.AttemptCount++
		if item.MaxRetries == 0 {
			item.MaxRetries = 1
		}
		if item.TimeoutSeconds == 0 {
			item.TimeoutSeconds = 1800
		}
		item.StartedAt = &now
		item.LastHeartbeatAt = &now
		item.FinishedAt = nil
		item.UpdatedAt = now
		payload, marshalErr := marshalJSON(item.Payload)
		if marshalErr != nil {
			return fmt.Errorf("marshal claimed virtualization task payload: %w", marshalErr)
		}
		resultPayload, marshalErr := marshalJSON(item.Result)
		if marshalErr != nil {
			return fmt.Errorf("marshal claimed virtualization task result: %w", marshalErr)
		}
		update := tx.Exec(`
			UPDATE virtualization_tasks
			SET status = ?, claimed_by_worker_id = ?, attempt_count = ?, max_retries = ?, timeout_seconds = ?,
			    payload = ?, result = ?, started_at = ?, last_heartbeat_at = ?, finished_at = ?, updated_at = ?
			WHERE id = ? AND status = 'queued'
		`, item.Status, nullableString(item.ClaimedByWorkerID), item.AttemptCount, item.MaxRetries, item.TimeoutSeconds, string(payload), string(resultPayload), item.StartedAt, item.LastHeartbeatAt, item.FinishedAt, item.UpdatedAt, item.ID)
		if update.Error != nil {
			return fmt.Errorf("claim virtualization task update: %w", update.Error)
		}
		if update.RowsAffected == 0 {
			return ErrNotFound
		}
		task = item
		return nil
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return domainvirtualization.Task{}, ErrNotFound
	}
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	return task, nil
}

func (r *Repository) HeartbeatTask(ctx context.Context, taskID string, workerID string, now time.Time) error {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE virtualization_tasks
		SET last_heartbeat_at = ?, updated_at = ?
		WHERE id = ? AND claimed_by_worker_id = ? AND status = 'running'
	`, now, now, strings.TrimSpace(taskID), strings.TrimSpace(workerID))
	if result.Error != nil {
		return fmt.Errorf("heartbeat virtualization task: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) GetTask(ctx context.Context, id string) (domainvirtualization.Task, error) {
	row := r.db.WithContext(ctx).Raw(taskSelect()+` WHERE id = ? LIMIT 1`, strings.TrimSpace(id)).Row()
	return scanTaskRow(row)
}

func (r *Repository) ListTasks(ctx context.Context, filter domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error) {
	limit, offset := limitOffset(filter.Limit, filter.Page, filter.PageSize)
	query := taskSelect()
	clauses, args := taskClauses(filter)
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query virtualization tasks: %w", err)
	}
	defer rows.Close()
	items := make([]domainvirtualization.Task, 0, limit)
	for rows.Next() {
		item, scanErr := scanTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountTasks(ctx context.Context, filter domainvirtualization.TaskFilter) (int, error) {
	clauses, args := taskClauses(filter)
	return r.count(ctx, "virtualization_tasks", clauses, args)
}

func (r *Repository) ListTimedOutTasks(ctx context.Context, now time.Time, limit int) ([]domainvirtualization.Task, error) {
	limit = normalizedLimit(limit)
	rows, err := r.db.WithContext(ctx).Raw(taskSelect()+`
		WHERE status = 'running'
		  AND COALESCE(last_heartbeat_at, started_at, created_at) + (timeout_seconds || ' seconds')::interval < ?
		ORDER BY COALESCE(last_heartbeat_at, started_at, created_at) ASC
		LIMIT ?
	`, now, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query timed out virtualization tasks: %w", err)
	}
	defer rows.Close()
	items := make([]domainvirtualization.Task, 0, limit)
	for rows.Next() {
		item, scanErr := scanTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateTaskLog(ctx context.Context, item domainvirtualization.TaskLog) error {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	payload, err := marshalJSON(item.Payload)
	if err != nil {
		return fmt.Errorf("marshal virtualization task log payload: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO virtualization_task_logs (id, task_id, log_level, message, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, item.ID, item.TaskID, item.LogLevel, item.Message, string(payload), item.CreatedAt).Error
}

func (r *Repository) ListTaskLogs(ctx context.Context, taskID string, limit int) ([]domainvirtualization.TaskLog, error) {
	limit = normalizedLimit(limit)
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, task_id, log_level, message, payload, created_at
		FROM virtualization_task_logs
		WHERE task_id = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, strings.TrimSpace(taskID), limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query virtualization task logs: %w", err)
	}
	defer rows.Close()
	items := make([]domainvirtualization.TaskLog, 0, limit)
	for rows.Next() {
		item, scanErr := scanTaskLog(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) saveConnection(ctx context.Context, item domainvirtualization.Connection, create bool) error {
	credential, err := marshalJSON(item.EncryptedCredential)
	if err != nil {
		return fmt.Errorf("marshal virtualization connection credential: %w", err)
	}
	config, err := marshalJSON(item.Config)
	if err != nil {
		return fmt.Errorf("marshal virtualization connection config: %w", err)
	}
	health, err := marshalJSON(item.Health)
	if err != nil {
		return fmt.Errorf("marshal virtualization connection health: %w", err)
	}
	if create {
		return r.db.WithContext(ctx).Exec(`
			INSERT INTO virtualization_connections (
				id, provider, name, endpoint, kubernetes_cluster_id, default_namespace, enabled, verify_tls,
				encrypted_credential, config, health, last_synced_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.Provider, item.Name, nullableString(item.Endpoint), nullableString(item.KubernetesClusterID), nullableString(item.DefaultNamespace), item.Enabled, item.VerifyTLS, string(credential), string(config), string(health), item.LastSyncedAt, item.CreatedAt, item.UpdatedAt).Error
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE virtualization_connections
		SET provider = ?, name = ?, endpoint = ?, kubernetes_cluster_id = ?, default_namespace = ?, enabled = ?,
		    verify_tls = ?, encrypted_credential = ?, config = ?, health = ?, updated_at = ?
		WHERE id = ?
	`, item.Provider, item.Name, nullableString(item.Endpoint), nullableString(item.KubernetesClusterID), nullableString(item.DefaultNamespace), item.Enabled, item.VerifyTLS, string(credential), string(config), string(health), item.UpdatedAt, item.ID)
	if result.Error != nil {
		return fmt.Errorf("update virtualization connection: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) UpdateConnectionHealth(ctx context.Context, id string, health map[string]any, lastSyncedAt *time.Time) (domainvirtualization.Connection, error) {
	payload, err := marshalJSON(health)
	if err != nil {
		return domainvirtualization.Connection{}, fmt.Errorf("marshal virtualization connection health: %w", err)
	}
	now := time.Now().UTC()
	var result *gorm.DB
	if lastSyncedAt != nil {
		result = r.db.WithContext(ctx).Exec(`
			UPDATE virtualization_connections
			SET health = ?, last_synced_at = ?, updated_at = ?
			WHERE id = ?
		`, string(payload), *lastSyncedAt, now, strings.TrimSpace(id))
	} else {
		result = r.db.WithContext(ctx).Exec(`
			UPDATE virtualization_connections
			SET health = ?, updated_at = ?
			WHERE id = ?
		`, string(payload), now, strings.TrimSpace(id))
	}
	if result.Error != nil {
		return domainvirtualization.Connection{}, fmt.Errorf("update virtualization connection health: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainvirtualization.Connection{}, ErrNotFound
	}
	return r.GetConnection(ctx, id)
}

func (r *Repository) MarkVMsStale(ctx context.Context, provider, connectionID string, seenBefore time.Time) error {
	return r.markAssetsStale(ctx, "virtualization_vms", provider, connectionID, seenBefore)
}

func (r *Repository) MarkImagesStale(ctx context.Context, provider, connectionID string, seenBefore time.Time) error {
	return r.markAssetsStale(ctx, "virtualization_images", provider, connectionID, seenBefore)
}

func (r *Repository) MarkFlavorsStale(ctx context.Context, provider, connectionID string, seenBefore time.Time) error {
	return r.markAssetsStale(ctx, "virtualization_flavors", provider, connectionID, seenBefore)
}

func (r *Repository) markAssetsStale(ctx context.Context, tableName, provider, connectionID string, seenBefore time.Time) error {
	query := fmt.Sprintf(`
		UPDATE %s
		SET status = 'stale', updated_at = ?
		WHERE provider = ? AND connection_id = ? AND last_seen_at < ? AND status <> 'deleted'
	`, tableName)
	if err := r.db.WithContext(ctx).Exec(query, time.Now().UTC(), strings.TrimSpace(provider), strings.TrimSpace(connectionID), seenBefore).Error; err != nil {
		return fmt.Errorf("mark %s stale: %w", tableName, err)
	}
	return nil
}

func connectionFromInput(input domainvirtualization.ConnectionInput) domainvirtualization.Connection {
	return domainvirtualization.Connection{
		ID:                   strings.TrimSpace(input.ID),
		Provider:             strings.TrimSpace(input.Provider),
		Name:                 strings.TrimSpace(input.Name),
		Endpoint:             strings.TrimSpace(input.Endpoint),
		KubernetesClusterID:  strings.TrimSpace(input.KubernetesClusterID),
		DefaultNamespace:     strings.TrimSpace(input.DefaultNamespace),
		Enabled:              input.Enabled,
		VerifyTLS:            input.VerifyTLS,
		EncryptedCredential:  input.EncryptedCredential,
		CredentialConfigured: len(input.EncryptedCredential) > 0,
		Config:               input.Config,
		Health:               input.Health,
	}
}

func scanConnection(rows *sql.Rows) (domainvirtualization.Connection, error) {
	var item domainvirtualization.Connection
	var endpoint, clusterID, namespace sql.NullString
	var credential, config, health []byte
	var lastSyncedAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.Provider, &item.Name, &endpoint, &clusterID, &namespace, &item.Enabled, &item.VerifyTLS, &credential, &config, &health, &lastSyncedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainvirtualization.Connection{}, fmt.Errorf("scan virtualization connection: %w", err)
	}
	item.Endpoint = endpoint.String
	item.KubernetesClusterID = clusterID.String
	item.DefaultNamespace = namespace.String
	if lastSyncedAt.Valid {
		value := lastSyncedAt.Time
		item.LastSyncedAt = &value
	}
	unmarshalMap(credential, &item.EncryptedCredential)
	unmarshalMap(config, &item.Config)
	unmarshalMap(health, &item.Health)
	item.CredentialConfigured = len(item.EncryptedCredential) > 0
	return item, nil
}

func scanConnectionRow(row *sql.Row) (domainvirtualization.Connection, error) {
	var item domainvirtualization.Connection
	var endpoint, clusterID, namespace sql.NullString
	var credential, config, health []byte
	var lastSyncedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Provider, &item.Name, &endpoint, &clusterID, &namespace, &item.Enabled, &item.VerifyTLS, &credential, &config, &health, &lastSyncedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainvirtualization.Connection{}, ErrNotFound
		}
		return domainvirtualization.Connection{}, fmt.Errorf("scan virtualization connection row: %w", err)
	}
	item.Endpoint = endpoint.String
	item.KubernetesClusterID = clusterID.String
	item.DefaultNamespace = namespace.String
	if lastSyncedAt.Valid {
		value := lastSyncedAt.Time
		item.LastSyncedAt = &value
	}
	unmarshalMap(credential, &item.EncryptedCredential)
	unmarshalMap(config, &item.Config)
	unmarshalMap(health, &item.Health)
	item.CredentialConfigured = len(item.EncryptedCredential) > 0
	return item, nil
}

func vmSelect() string {
	return `SELECT id, provider, connection_id, external_id, name, namespace, status, power_state, node_name, image_id, flavor_id, ip_addresses, labels, config, raw, last_seen_at, created_at, updated_at FROM virtualization_vms`
}

func imageSelect() string {
	return `SELECT id, provider, connection_id, external_id, name, status, os_type, architecture, size_bytes, config, raw, last_seen_at, created_at, updated_at FROM virtualization_images`
}

func flavorSelect() string {
	return `SELECT id, provider, connection_id, external_id, name, status, cpu_cores, memory_mb, disk_gb, config, raw, last_seen_at, created_at, updated_at FROM virtualization_flavors`
}

func taskSelect() string {
	return `SELECT id, provider, connection_id, vm_id, task_kind, status, requested_by, claimed_by_worker_id, attempt_count, max_retries, timeout_seconds, payload, result, started_at, last_heartbeat_at, finished_at, created_at, updated_at FROM virtualization_tasks`
}

func (r *Repository) getVMByExternalKey(ctx context.Context, provider, connectionID, externalID string) (domainvirtualization.VM, error) {
	row := r.db.WithContext(ctx).Raw(vmSelect()+` WHERE provider = ? AND connection_id = ? AND external_id = ? LIMIT 1`, provider, connectionID, externalID).Row()
	return scanVMRow(row)
}

func (r *Repository) getImageByExternalKey(ctx context.Context, provider, connectionID, externalID string) (domainvirtualization.Image, error) {
	row := r.db.WithContext(ctx).Raw(imageSelect()+` WHERE provider = ? AND connection_id = ? AND external_id = ? LIMIT 1`, provider, connectionID, externalID).Row()
	return scanImageRow(row)
}

func (r *Repository) getFlavorByExternalKey(ctx context.Context, provider, connectionID, externalID string) (domainvirtualization.Flavor, error) {
	if strings.TrimSpace(connectionID) == "" {
		row := r.db.WithContext(ctx).Raw(flavorSelect()+` WHERE provider = ? AND connection_id IS NULL AND external_id = ? LIMIT 1`, provider, externalID).Row()
		return scanFlavorRow(row)
	}
	row := r.db.WithContext(ctx).Raw(flavorSelect()+` WHERE provider = ? AND connection_id = ? AND external_id = ? LIMIT 1`, provider, connectionID, externalID).Row()
	return scanFlavorRow(row)
}

func scanVMList(rows *sql.Rows, err error, limit int) ([]domainvirtualization.VM, error) {
	if err != nil {
		return nil, fmt.Errorf("query virtualization vms: %w", err)
	}
	defer rows.Close()
	items := make([]domainvirtualization.VM, 0, limit)
	for rows.Next() {
		item, scanErr := scanVM(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanVM(rows *sql.Rows) (domainvirtualization.VM, error) {
	var item domainvirtualization.VM
	var namespace, powerState, nodeName, imageID, flavorID sql.NullString
	var ipAddresses, labels, config, raw []byte
	var lastSeenAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.Provider, &item.ConnectionID, &item.ExternalID, &item.Name, &namespace, &item.Status, &powerState, &nodeName, &imageID, &flavorID, &ipAddresses, &labels, &config, &raw, &lastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainvirtualization.VM{}, fmt.Errorf("scan virtualization vm: %w", err)
	}
	item.Namespace = namespace.String
	item.PowerState = powerState.String
	item.NodeName = nodeName.String
	item.ImageID = imageID.String
	item.FlavorID = flavorID.String
	if lastSeenAt.Valid {
		value := lastSeenAt.Time
		item.LastSeenAt = &value
	}
	_ = json.Unmarshal(ipAddresses, &item.IPAddresses)
	unmarshalMap(labels, &item.Labels)
	unmarshalMap(config, &item.Config)
	unmarshalMap(raw, &item.Raw)
	return item, nil
}

func scanVMRow(row *sql.Row) (domainvirtualization.VM, error) {
	var item domainvirtualization.VM
	var namespace, powerState, nodeName, imageID, flavorID sql.NullString
	var ipAddresses, labels, config, raw []byte
	var lastSeenAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Provider, &item.ConnectionID, &item.ExternalID, &item.Name, &namespace, &item.Status, &powerState, &nodeName, &imageID, &flavorID, &ipAddresses, &labels, &config, &raw, &lastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainvirtualization.VM{}, ErrNotFound
		}
		return domainvirtualization.VM{}, fmt.Errorf("scan virtualization vm row: %w", err)
	}
	item.Namespace = namespace.String
	item.PowerState = powerState.String
	item.NodeName = nodeName.String
	item.ImageID = imageID.String
	item.FlavorID = flavorID.String
	if lastSeenAt.Valid {
		value := lastSeenAt.Time
		item.LastSeenAt = &value
	}
	_ = json.Unmarshal(ipAddresses, &item.IPAddresses)
	unmarshalMap(labels, &item.Labels)
	unmarshalMap(config, &item.Config)
	unmarshalMap(raw, &item.Raw)
	return item, nil
}

func scanImage(rows *sql.Rows) (domainvirtualization.Image, error) {
	var item domainvirtualization.Image
	var osType, architecture sql.NullString
	var config, raw []byte
	var lastSeenAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.Provider, &item.ConnectionID, &item.ExternalID, &item.Name, &item.Status, &osType, &architecture, &item.SizeBytes, &config, &raw, &lastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainvirtualization.Image{}, fmt.Errorf("scan virtualization image: %w", err)
	}
	item.OSType = osType.String
	item.Architecture = architecture.String
	if lastSeenAt.Valid {
		value := lastSeenAt.Time
		item.LastSeenAt = &value
	}
	unmarshalMap(config, &item.Config)
	unmarshalMap(raw, &item.Raw)
	return item, nil
}

func scanImageRow(row *sql.Row) (domainvirtualization.Image, error) {
	var item domainvirtualization.Image
	var osType, architecture sql.NullString
	var config, raw []byte
	var lastSeenAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Provider, &item.ConnectionID, &item.ExternalID, &item.Name, &item.Status, &osType, &architecture, &item.SizeBytes, &config, &raw, &lastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainvirtualization.Image{}, ErrNotFound
		}
		return domainvirtualization.Image{}, fmt.Errorf("scan virtualization image row: %w", err)
	}
	item.OSType = osType.String
	item.Architecture = architecture.String
	if lastSeenAt.Valid {
		value := lastSeenAt.Time
		item.LastSeenAt = &value
	}
	unmarshalMap(config, &item.Config)
	unmarshalMap(raw, &item.Raw)
	return item, nil
}

func scanFlavor(rows *sql.Rows) (domainvirtualization.Flavor, error) {
	var item domainvirtualization.Flavor
	var connectionID sql.NullString
	var config, raw []byte
	var lastSeenAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.Provider, &connectionID, &item.ExternalID, &item.Name, &item.Status, &item.CPUCores, &item.MemoryMB, &item.DiskGB, &config, &raw, &lastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainvirtualization.Flavor{}, fmt.Errorf("scan virtualization flavor: %w", err)
	}
	item.ConnectionID = connectionID.String
	if lastSeenAt.Valid {
		value := lastSeenAt.Time
		item.LastSeenAt = &value
	}
	unmarshalMap(config, &item.Config)
	unmarshalMap(raw, &item.Raw)
	return item, nil
}

func scanFlavorRow(row *sql.Row) (domainvirtualization.Flavor, error) {
	var item domainvirtualization.Flavor
	var connectionID sql.NullString
	var config, raw []byte
	var lastSeenAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Provider, &connectionID, &item.ExternalID, &item.Name, &item.Status, &item.CPUCores, &item.MemoryMB, &item.DiskGB, &config, &raw, &lastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainvirtualization.Flavor{}, ErrNotFound
		}
		return domainvirtualization.Flavor{}, fmt.Errorf("scan virtualization flavor row: %w", err)
	}
	item.ConnectionID = connectionID.String
	if lastSeenAt.Valid {
		value := lastSeenAt.Time
		item.LastSeenAt = &value
	}
	unmarshalMap(config, &item.Config)
	unmarshalMap(raw, &item.Raw)
	return item, nil
}

func scanTask(rows *sql.Rows) (domainvirtualization.Task, error) {
	var item domainvirtualization.Task
	var connectionID, vmID, requestedBy, claimedByWorkerID sql.NullString
	var payload, result []byte
	var startedAt, lastHeartbeatAt, finishedAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.Provider, &connectionID, &vmID, &item.TaskKind, &item.Status, &requestedBy, &claimedByWorkerID, &item.AttemptCount, &item.MaxRetries, &item.TimeoutSeconds, &payload, &result, &startedAt, &lastHeartbeatAt, &finishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainvirtualization.Task{}, fmt.Errorf("scan virtualization task: %w", err)
	}
	item.ConnectionID = connectionID.String
	item.VMID = vmID.String
	item.RequestedBy = requestedBy.String
	item.ClaimedByWorkerID = claimedByWorkerID.String
	unmarshalMap(payload, &item.Payload)
	unmarshalMap(result, &item.Result)
	if startedAt.Valid {
		value := startedAt.Time
		item.StartedAt = &value
	}
	if lastHeartbeatAt.Valid {
		value := lastHeartbeatAt.Time
		item.LastHeartbeatAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		item.FinishedAt = &value
	}
	return item, nil
}

func scanTaskRow(row *sql.Row) (domainvirtualization.Task, error) {
	var item domainvirtualization.Task
	var connectionID, vmID, requestedBy, claimedByWorkerID sql.NullString
	var payload, result []byte
	var startedAt, lastHeartbeatAt, finishedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Provider, &connectionID, &vmID, &item.TaskKind, &item.Status, &requestedBy, &claimedByWorkerID, &item.AttemptCount, &item.MaxRetries, &item.TimeoutSeconds, &payload, &result, &startedAt, &lastHeartbeatAt, &finishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainvirtualization.Task{}, ErrNotFound
		}
		return domainvirtualization.Task{}, fmt.Errorf("scan virtualization task row: %w", err)
	}
	item.ConnectionID = connectionID.String
	item.VMID = vmID.String
	item.RequestedBy = requestedBy.String
	item.ClaimedByWorkerID = claimedByWorkerID.String
	unmarshalMap(payload, &item.Payload)
	unmarshalMap(result, &item.Result)
	if startedAt.Valid {
		value := startedAt.Time
		item.StartedAt = &value
	}
	if lastHeartbeatAt.Valid {
		value := lastHeartbeatAt.Time
		item.LastHeartbeatAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		item.FinishedAt = &value
	}
	return item, nil
}

func scanTaskLog(rows *sql.Rows) (domainvirtualization.TaskLog, error) {
	var item domainvirtualization.TaskLog
	var payload []byte
	if err := rows.Scan(&item.ID, &item.TaskID, &item.LogLevel, &item.Message, &payload, &item.CreatedAt); err != nil {
		return domainvirtualization.TaskLog{}, fmt.Errorf("scan virtualization task log: %w", err)
	}
	unmarshalMap(payload, &item.Payload)
	return item, nil
}

func buildAssetListQuery(selectSQL, tableName, provider, connectionID, status, search string, searchColumns []string, rawLimit, page, pageSize int) (string, []any, int) {
	limit, offset := limitOffset(rawLimit, page, pageSize)
	query := selectSQL
	clauses, args := assetClauses(provider, connectionID, status, search, searchColumns)
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY %s.updated_at DESC LIMIT ? OFFSET ?", tableName)
	args = append(args, limit, offset)
	return query, args, limit
}

func assetClauses(provider, connectionID, status, search string, searchColumns []string) ([]string, []any) {
	args := []any{}
	clauses := []string{}
	if value := strings.TrimSpace(provider); value != "" {
		clauses = append(clauses, "provider = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(connectionID); value != "" {
		clauses = append(clauses, "connection_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(search); value != "" && len(searchColumns) > 0 {
		searchClause := make([]string, 0, len(searchColumns))
		searchArg := "%" + strings.ToLower(value) + "%"
		for _, column := range searchColumns {
			searchClause = append(searchClause, fmt.Sprintf("LOWER(%s) LIKE ?", column))
			args = append(args, searchArg)
		}
		clauses = append(clauses, "("+strings.Join(searchClause, " OR ")+")")
	}
	return clauses, args
}

func connectionClauses(filter domainvirtualization.ConnectionFilter) ([]string, []any) {
	args := []any{}
	clauses := []string{}
	if value := strings.TrimSpace(filter.Provider); value != "" {
		clauses = append(clauses, "provider = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.KubernetesClusterID); value != "" {
		clauses = append(clauses, "kubernetes_cluster_id = ?")
		args = append(args, value)
	}
	if filter.Enabled != nil {
		clauses = append(clauses, "enabled = ?")
		args = append(args, *filter.Enabled)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		search := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, "(LOWER(name) LIKE ? OR LOWER(endpoint) LIKE ? OR LOWER(kubernetes_cluster_id) LIKE ?)")
		args = append(args, search, search, search)
	}
	return clauses, args
}

func vmExtraClauses(filter domainvirtualization.VMFilter) ([]string, []any) {
	if value := strings.TrimSpace(filter.Namespace); value != "" {
		return []string{"namespace = ?"}, []any{value}
	}
	return nil, nil
}

func taskClauses(filter domainvirtualization.TaskFilter) ([]string, []any) {
	args := []any{}
	clauses := []string{}
	if value := strings.TrimSpace(filter.Provider); value != "" {
		clauses = append(clauses, "provider = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ConnectionID); value != "" {
		clauses = append(clauses, "connection_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.VMID); value != "" {
		clauses = append(clauses, "vm_id = ?")
		args = append(args, value)
	}
	if len(filter.Statuses) > 0 {
		statuses := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			if trimmed := strings.TrimSpace(status); trimmed != "" {
				statuses = append(statuses, trimmed)
			}
		}
		if len(statuses) > 0 {
			clauses = append(clauses, fmt.Sprintf("status IN (%s)", placeholders(len(statuses))))
			for _, status := range statuses {
				args = append(args, status)
			}
		}
	} else if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if filter.Abnormal {
		clauses = append(clauses, "status IN (?, ?)")
		args = append(args, "failed", "callback_timeout")
	}
	if filter.Pending {
		clauses = append(clauses, "status IN (?, ?)")
		args = append(args, "queued", "running")
	}
	if value := strings.TrimSpace(filter.TaskKind); value != "" {
		clauses = append(clauses, "task_kind = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		search := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, "(LOWER(task_kind) LIKE ? OR LOWER(status) LIKE ? OR LOWER(vm_id) LIKE ? OR LOWER(connection_id) LIKE ?)")
		args = append(args, search, search, search, search)
	}
	return clauses, args
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	items := make([]string, count)
	for i := range items {
		items[i] = "?"
	}
	return strings.Join(items, ", ")
}

func injectExtraClauses(query string, args []any, clauses []string, extraArgs []any) (string, []any) {
	if len(clauses) == 0 {
		return query, args
	}
	clauseSQL := strings.Join(clauses, " AND ")
	if strings.Contains(query, " WHERE ") {
		query = strings.Replace(query, " ORDER BY", " AND "+clauseSQL+" ORDER BY", 1)
	} else {
		query = strings.Replace(query, " ORDER BY", " WHERE "+clauseSQL+" ORDER BY", 1)
	}
	if len(args) >= 2 {
		args = append(args[:len(args)-2], append(extraArgs, args[len(args)-2:]...)...)
	} else {
		args = append(args, extraArgs...)
	}
	return query, args
}

func (r *Repository) count(ctx context.Context, tableName string, clauses []string, args []any) (int, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int
	if err := r.db.WithContext(ctx).Raw(query, args...).Row().Scan(&total); err != nil {
		return 0, fmt.Errorf("count %s: %w", tableName, err)
	}
	return total, nil
}

func marshalJSON(value any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(value)
}

func marshalJSONDefault(value any, fallback any) ([]byte, error) {
	if value == nil {
		return json.Marshal(fallback)
	}
	return json.Marshal(value)
}

func unmarshalMap(raw []byte, target *map[string]any) {
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, target)
	}
	if *target == nil {
		*target = map[string]any{}
	}
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func normalizedLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 50
	}
	return limit
}

func normalizedPage(page int) int {
	if page <= 0 {
		return 1
	}
	return page
}

func limitOffset(limit, page, pageSize int) (int, int) {
	size := normalizedLimit(limit)
	if pageSize > 0 {
		size = normalizedLimit(pageSize)
	}
	currentPage := normalizedPage(page)
	return size, (currentPage - 1) * size
}
