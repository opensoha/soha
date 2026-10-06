package networkproxy

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

type instanceRow struct {
	ID                      string `gorm:"primaryKey"`
	TenantID                string
	WorkspaceID             string
	Name                    string
	Engine                  string
	Host                    string
	Enabled                 bool
	DesiredRevision         int64
	DesiredContentEncrypted string
	DesiredHash             string
	ObservedRevision        int64
	EngineVersion           string
	CapabilitiesJSON        string `gorm:"column:capabilities;type:jsonb"`
	Health                  string
	ReasonCode              string
	LastSeenAt              *time.Time
	LastSampleAt            *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
	Registered              bool `gorm:"->;-:migration"`
}

func (instanceRow) TableName() string { return "network_proxy_instances" }

func (row instanceRow) domain() domain.Instance {
	var capabilities []string
	_ = json.Unmarshal([]byte(row.CapabilitiesJSON), &capabilities)
	return domain.Instance{
		ID: row.ID, Name: row.Name, Engine: row.Engine, Host: row.Host, EngineVersion: row.EngineVersion,
		Capabilities:    capabilities,
		DesiredRevision: row.DesiredRevision, ObservedRevision: row.ObservedRevision,
		LastSeenAt: row.LastSeenAt, LastSampleAt: row.LastSampleAt, ReasonCode: row.ReasonCode,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Enabled: row.Enabled, Health: row.Health,
		DesiredContentEncrypted: row.DesiredContentEncrypted, DesiredHash: row.DesiredHash, Registered: row.Registered,
	}
}

const instanceColumns = `network_proxy_instances.*, EXISTS (
    SELECT 1 FROM network_runtime_credentials c
    WHERE c.runtime_id = network_proxy_instances.id AND c.runtime_kind = 'proxy'
      AND c.status = 'active' AND c.not_before <= now() AND c.expires_at > now()
      AND c.tenant_id = 'default' AND c.workspace_id = 'default'
  ) AS registered`

func (r *Repository) Create(ctx context.Context, input domain.InstanceInput, now time.Time) (domain.Instance, error) {
	row := instanceRow{ID: input.ID, TenantID: "default", WorkspaceID: "default", Name: input.Name,
		Engine: input.Engine, Host: input.Host, Enabled: true, CapabilitiesJSON: "[]", CreatedAt: now, UpdatedAt: now}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &pg) && pg.Code == "23505") {
			return domain.Instance{}, apperrors.ErrConflict
		}
		return domain.Instance{}, err
	}
	return r.Get(ctx, input.ID)
}

func (r *Repository) Get(ctx context.Context, id string) (domain.Instance, error) {
	var row instanceRow
	err := r.db.WithContext(ctx).Model(&instanceRow{}).Select(instanceColumns).
		Where("network_proxy_instances.tenant_id = 'default' AND network_proxy_instances.workspace_id = 'default' AND network_proxy_instances.id = ?", id).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Instance{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domain.Instance{}, err
	}
	return row.domain(), nil
}

func (r *Repository) List(ctx context.Context, search, engine string, limit int) ([]domain.Instance, error) {
	query := r.db.WithContext(ctx).Model(&instanceRow{}).Select(instanceColumns).
		Where("network_proxy_instances.tenant_id = 'default' AND network_proxy_instances.workspace_id = 'default'")
	if search != "" {
		query = query.Where("(network_proxy_instances.id ILIKE ? OR network_proxy_instances.name ILIKE ? OR network_proxy_instances.host ILIKE ?)", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if engine != "" {
		query = query.Where("network_proxy_instances.engine = ?", engine)
	}
	var rows []instanceRow
	if err := query.Order("network_proxy_instances.created_at DESC, network_proxy_instances.id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]domain.Instance, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.domain())
	}
	return items, nil
}

func (r *Repository) UpdateConfiguration(ctx context.Context, id string, expectedRevision int64, enabled bool, encrypted, hash string, now time.Time) (domain.Instance, error) {
	result := r.db.WithContext(ctx).Model(&instanceRow{}).
		Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND desired_revision = ?", id, expectedRevision).
		Updates(map[string]any{"desired_revision": gorm.Expr("desired_revision + 1"), "enabled": enabled,
			"desired_content_encrypted": encrypted, "desired_hash": hash, "updated_at": now})
	if result.Error != nil {
		return domain.Instance{}, result.Error
	}
	if result.RowsAffected == 0 {
		if _, err := r.Get(ctx, id); err != nil {
			return domain.Instance{}, err
		}
		return domain.Instance{}, apperrors.ErrConflict
	}
	return r.Get(ctx, id)
}

func (r *Repository) Touch(ctx context.Context, id string, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&instanceRow{}).
		Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ?", id).
		Update("last_seen_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

type sampleRow struct {
	InstanceID        string    `gorm:"primaryKey"`
	ObservedAt        time.Time `gorm:"primaryKey"`
	UptimeSeconds     int64
	UploadTotal       int64
	DownloadTotal     int64
	ActiveConnections *int
}

func (sampleRow) TableName() string { return "network_proxy_samples" }

func (r *Repository) Observe(ctx context.Context, id string, observedAt time.Time, observation domain.Observation) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row instanceRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = 'default' AND workspace_id = 'default'", id).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.ErrNotFound
			}
			return err
		}
		if row.Engine != observation.Engine || (row.LastSampleAt != nil && !observedAt.After(*row.LastSampleAt)) {
			return apperrors.ErrConflict
		}
		capabilities, err := json.Marshal(observation.Capabilities)
		if err != nil {
			return err
		}
		return tx.Model(&row).Updates(map[string]any{"engine_version": observation.EngineVersion, "health": observation.Health,
			"capabilities": gorm.Expr("?::jsonb", string(capabilities)),
			"reason_code":  observation.ReasonCode, "last_seen_at": observedAt, "last_sample_at": observedAt, "updated_at": observedAt}).Error
	})
}

func (r *Repository) Apply(ctx context.Context, id string, applied domain.Applied, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row instanceRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = 'default' AND workspace_id = 'default'", id).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.ErrNotFound
			}
			return err
		}
		if applied.Revision != row.DesiredRevision || applied.Revision < row.ObservedRevision {
			return apperrors.ErrConflict
		}
		changes := map[string]any{"last_seen_at": now, "reason_code": applied.ReasonCode, "updated_at": now}
		if applied.Status == "applied" && applied.ReadbackHash == row.DesiredHash {
			changes["observed_revision"] = applied.Revision
		} else if applied.Status == "applied" {
			return apperrors.ErrConflict
		} else {
			changes["health"] = "degraded"
		}
		return tx.Model(&row).Updates(changes).Error
	})
}

type connectionsRow struct {
	InstanceID  string `gorm:"primaryKey"`
	ObservedAt  time.Time
	Connections []byte `gorm:"type:jsonb"`
}

func (connectionsRow) TableName() string { return "network_proxy_connections" }

func (r *Repository) PutConnections(ctx context.Context, id string, observedAt time.Time, connections []domain.Connection) error {
	raw, err := json.Marshal(connections)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(`INSERT INTO network_proxy_connections (instance_id, observed_at, connections)
		VALUES (?, ?, ?::jsonb) ON CONFLICT (instance_id) DO UPDATE SET
		observed_at = EXCLUDED.observed_at, connections = EXCLUDED.connections
		WHERE network_proxy_connections.observed_at < EXCLUDED.observed_at`, id, observedAt, string(raw)).Error
}

func (r *Repository) Connections(ctx context.Context, id string) (domain.ConnectionsSnapshot, error) {
	var row connectionsRow
	err := r.db.WithContext(ctx).Where("instance_id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ConnectionsSnapshot{InstanceID: id, State: "unavailable", Connections: []domain.Connection{}}, nil
	}
	if err != nil {
		return domain.ConnectionsSnapshot{}, err
	}
	var connections []domain.Connection
	if err := json.Unmarshal(row.Connections, &connections); err != nil {
		return domain.ConnectionsSnapshot{}, err
	}
	return domain.ConnectionsSnapshot{InstanceID: id, State: "available", ObservedAt: &row.ObservedAt, Connections: connections}, nil
}

type closeRow struct {
	ID           string `gorm:"primaryKey"`
	InstanceID   string
	ConnectionID string
	Status       string
	ReasonCode   string
	ExpiresAt    time.Time
	CreatedBy    string
	CreatedAt    time.Time
	CompletedAt  *time.Time
}

func (closeRow) TableName() string { return "network_proxy_close_commands" }

func (row closeRow) domain() domain.CloseCommand {
	return domain.CloseCommand{ID: row.ID, InstanceID: row.InstanceID, ConnectionID: row.ConnectionID,
		Status: row.Status, ExpiresAt: row.ExpiresAt, ReasonCode: row.ReasonCode}
}

func (r *Repository) QueueClose(ctx context.Context, id, connectionID, actorID string, now time.Time) (domain.CloseCommand, error) {
	row := closeRow{ID: uuid.NewString(), InstanceID: id, ConnectionID: connectionID, Status: "pending",
		ExpiresAt: now.Add(30 * time.Second), CreatedBy: actorID, CreatedAt: now}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return domain.CloseCommand{}, err
	}
	return row.domain(), nil
}

func (r *Repository) NextClose(ctx context.Context, id string, now time.Time) (*domain.CloseCommand, error) {
	var row closeRow
	err := r.db.WithContext(ctx).Where("instance_id = ? AND status = 'pending' AND expires_at > ?", id, now).
		Order("created_at ASC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	command := row.domain()
	return &command, nil
}

func (r *Repository) CompleteClose(ctx context.Context, id, commandID, status, reason string, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&closeRow{}).
		Where("id = ? AND instance_id = ? AND status = 'pending' AND expires_at > ?", commandID, id, now).
		Updates(map[string]any{"status": status, "reason_code": reason, "completed_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrConflict
	}
	return nil
}

func (r *Repository) Cleanup(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("observed_at < ?", now.Add(-24*time.Hour)).Delete(&sampleRow{}).Error; err != nil {
			return err
		}
		if err := tx.Where("observed_at < ?", now.Add(-5*time.Minute)).Delete(&connectionsRow{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&closeRow{}).Where("status = 'pending' AND expires_at <= ?", now).
			Updates(map[string]any{"status": "expired", "completed_at": now}).Error; err != nil {
			return err
		}
		return tx.Where("created_at < ?", now.Add(-24*time.Hour)).Delete(&closeRow{}).Error
	})
}
