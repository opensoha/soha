package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domaincluster "github.com/kubecrux/kubecrux/internal/domain/cluster"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("cluster not found")

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context) ([]domaincluster.Summary, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, region, environment, labels, connection_mode, version, capabilities, health_snapshot
		FROM clusters
		ORDER BY name ASC, id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domaincluster.Summary, 0)
	for rows.Next() {
		item, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, clusterID string) (domaincluster.Summary, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, region, environment, labels, connection_mode, version, capabilities, health_snapshot
		FROM clusters
		WHERE id = ?
		LIMIT 1
	`, clusterID).Row()
	return scanSummaryRow(row)
}

func (r *Repository) ListConnections(ctx context.Context) ([]domaincluster.Connection, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT c.id, c.name, c.region, c.environment, c.labels, c.connection_mode, c.version, c.capabilities, c.health_snapshot,
			ccm.credential_type, ccm.source_type, ccm.source_ref, ccm.metadata
		FROM clusters c
		LEFT JOIN LATERAL (
			SELECT credential_type, source_type, source_ref, metadata
			FROM cluster_credentials_meta
			WHERE cluster_id = c.id
			ORDER BY updated_at DESC
			LIMIT 1
		) ccm ON TRUE
		ORDER BY c.name ASC, c.id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domaincluster.Connection, 0)
	for rows.Next() {
		item, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetConnection(ctx context.Context, clusterID string) (domaincluster.Connection, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT c.id, c.name, c.region, c.environment, c.labels, c.connection_mode, c.version, c.capabilities, c.health_snapshot,
			ccm.credential_type, ccm.source_type, ccm.source_ref, ccm.metadata
		FROM clusters c
		LEFT JOIN LATERAL (
			SELECT credential_type, source_type, source_ref, metadata
			FROM cluster_credentials_meta
			WHERE cluster_id = c.id
			ORDER BY updated_at DESC
			LIMIT 1
		) ccm ON TRUE
		WHERE c.id = ?
		LIMIT 1
	`, clusterID).Row()
	return scanConnectionRow(row)
}

func (r *Repository) UpsertRegistration(ctx context.Context, connection domaincluster.Connection) error {
	return r.saveRegistration(ctx, connection)
}

func (r *Repository) UpdateRegistration(ctx context.Context, connection domaincluster.Connection) error {
	if _, err := r.GetConnection(ctx, connection.Summary.ID); err != nil {
		return err
	}
	return r.saveRegistration(ctx, connection)
}

func (r *Repository) Delete(ctx context.Context, clusterID string) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := tx.Exec(`DELETE FROM cluster_credentials_meta WHERE cluster_id = ?`, clusterID).Error; err != nil {
		tx.Rollback()
		return err
	}
	result := tx.Exec(`DELETE FROM clusters WHERE id = ?`, clusterID)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		return ErrNotFound
	}
	return tx.Commit().Error
}

func (r *Repository) saveRegistration(ctx context.Context, connection domaincluster.Connection) error {
	labels, err := json.Marshal(connection.Summary.Labels)
	if err != nil {
		return fmt.Errorf("marshal cluster labels: %w", err)
	}
	capabilities, err := json.Marshal(connection.Summary.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal cluster capabilities: %w", err)
	}
	health, err := json.Marshal(connection.Summary.Health)
	if err != nil {
		return fmt.Errorf("marshal cluster health: %w", err)
	}
	metadata, err := json.Marshal(connection.Metadata)
	if err != nil {
		return fmt.Errorf("marshal cluster metadata: %w", err)
	}
	now := time.Now().UTC()
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := tx.Exec(`
		INSERT INTO clusters (id, name, region, environment, labels, connection_mode, version, capabilities, health_snapshot, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			region = EXCLUDED.region,
			environment = EXCLUDED.environment,
			labels = EXCLUDED.labels,
			connection_mode = EXCLUDED.connection_mode,
			version = EXCLUDED.version,
			capabilities = EXCLUDED.capabilities,
			health_snapshot = EXCLUDED.health_snapshot,
			updated_at = EXCLUDED.updated_at
	`, connection.Summary.ID, connection.Summary.Name, connection.Summary.Region, connection.Summary.Environment, string(labels), connection.Summary.ConnectionMode, connection.Summary.Version, string(capabilities), string(health), now, now).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Exec(`
		INSERT INTO cluster_credentials_meta (id, cluster_id, credential_type, source_type, source_ref, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			credential_type = EXCLUDED.credential_type,
			source_type = EXCLUDED.source_type,
			source_ref = EXCLUDED.source_ref,
			metadata = EXCLUDED.metadata,
			updated_at = EXCLUDED.updated_at
	`, fmt.Sprintf("%s:primary", connection.Summary.ID), connection.Summary.ID, connection.CredentialType, connection.SourceType, connection.SourceRef, string(metadata), now, now).Error; err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

func (r *Repository) UpsertSnapshot(ctx context.Context, summary domaincluster.Summary) error {
	labels, err := json.Marshal(summary.Labels)
	if err != nil {
		return fmt.Errorf("marshal cluster labels: %w", err)
	}
	capabilities, err := json.Marshal(summary.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal cluster capabilities: %w", err)
	}
	health, err := json.Marshal(summary.Health)
	if err != nil {
		return fmt.Errorf("marshal cluster health: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO clusters (id, name, region, environment, labels, connection_mode, version, capabilities, health_snapshot, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			region = EXCLUDED.region,
			environment = EXCLUDED.environment,
			labels = EXCLUDED.labels,
			connection_mode = COALESCE(NULLIF(clusters.connection_mode, ''), EXCLUDED.connection_mode),
			version = EXCLUDED.version,
			capabilities = EXCLUDED.capabilities,
			health_snapshot = EXCLUDED.health_snapshot,
			updated_at = NOW()
	`, summary.ID, summary.Name, summary.Region, summary.Environment, string(labels), summary.ConnectionMode, summary.Version, string(capabilities), string(health)).Error
}

func scanSummary(rows *sql.Rows) (domaincluster.Summary, error) {
	var summary domaincluster.Summary
	var labels []byte
	var connectionMode sql.NullString
	var version sql.NullString
	var capabilities []byte
	var health []byte
	if err := rows.Scan(&summary.ID, &summary.Name, &summary.Region, &summary.Environment, &labels, &connectionMode, &version, &capabilities, &health); err != nil {
		return domaincluster.Summary{}, err
	}
	decodeSummaryFields(&summary, labels, connectionMode, version, capabilities, health)
	return summary, nil
}

func scanSummaryRow(row *sql.Row) (domaincluster.Summary, error) {
	var summary domaincluster.Summary
	var labels []byte
	var connectionMode sql.NullString
	var version sql.NullString
	var capabilities []byte
	var health []byte
	if err := row.Scan(&summary.ID, &summary.Name, &summary.Region, &summary.Environment, &labels, &connectionMode, &version, &capabilities, &health); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincluster.Summary{}, ErrNotFound
		}
		return domaincluster.Summary{}, err
	}
	decodeSummaryFields(&summary, labels, connectionMode, version, capabilities, health)
	return summary, nil
}

func scanConnection(rows *sql.Rows) (domaincluster.Connection, error) {
	var connection domaincluster.Connection
	var labels []byte
	var connectionMode sql.NullString
	var version sql.NullString
	var capabilities []byte
	var health []byte
	var credentialType sql.NullString
	var sourceType sql.NullString
	var sourceRef sql.NullString
	var metadata []byte
	if err := rows.Scan(&connection.Summary.ID, &connection.Summary.Name, &connection.Summary.Region, &connection.Summary.Environment, &labels, &connectionMode, &version, &capabilities, &health, &credentialType, &sourceType, &sourceRef, &metadata); err != nil {
		return domaincluster.Connection{}, err
	}
	decodeSummaryFields(&connection.Summary, labels, connectionMode, version, capabilities, health)
	decodeConnectionFields(&connection, credentialType, sourceType, sourceRef, metadata)
	return connection, nil
}

func scanConnectionRow(row *sql.Row) (domaincluster.Connection, error) {
	var connection domaincluster.Connection
	var labels []byte
	var connectionMode sql.NullString
	var version sql.NullString
	var capabilities []byte
	var health []byte
	var credentialType sql.NullString
	var sourceType sql.NullString
	var sourceRef sql.NullString
	var metadata []byte
	if err := row.Scan(&connection.Summary.ID, &connection.Summary.Name, &connection.Summary.Region, &connection.Summary.Environment, &labels, &connectionMode, &version, &capabilities, &health, &credentialType, &sourceType, &sourceRef, &metadata); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaincluster.Connection{}, ErrNotFound
		}
		return domaincluster.Connection{}, err
	}
	decodeSummaryFields(&connection.Summary, labels, connectionMode, version, capabilities, health)
	decodeConnectionFields(&connection, credentialType, sourceType, sourceRef, metadata)
	return connection, nil
}

func decodeSummaryFields(summary *domaincluster.Summary, labels []byte, connectionMode sql.NullString, version sql.NullString, capabilities []byte, health []byte) {
	if len(labels) > 0 {
		_ = json.Unmarshal(labels, &summary.Labels)
	}
	if connectionMode.Valid {
		summary.ConnectionMode = domaincluster.ConnectionMode(connectionMode.String)
	}
	if summary.ConnectionMode == "" {
		summary.ConnectionMode = domaincluster.ConnectionModeDirectKubeconfig
	}
	if version.Valid {
		summary.Version = version.String
	}
	if len(capabilities) > 0 {
		_ = json.Unmarshal(capabilities, &summary.Capabilities)
	}
	if len(health) > 0 {
		_ = json.Unmarshal(health, &summary.Health)
	}
}

func decodeConnectionFields(connection *domaincluster.Connection, credentialType sql.NullString, sourceType sql.NullString, sourceRef sql.NullString, metadata []byte) {
	if credentialType.Valid {
		connection.CredentialType = credentialType.String
	}
	if sourceType.Valid {
		connection.SourceType = sourceType.String
	}
	if sourceRef.Valid {
		connection.SourceRef = sourceRef.String
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &connection.Metadata)
	}
	if connection.Metadata == nil {
		connection.Metadata = map[string]any{}
	}
}
