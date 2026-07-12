package portforward

import (
	"context"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"gorm.io/gorm"
)

// Record is kept as a compatibility alias for repository callers. The port
// record contract is owned by the application package that consumes it.
type Record = domainresource.PortForwardRecord

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context) ([]Record, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT session_id, cluster_id, namespace, target_kind, target_name,
			local_port, remote_port, status, connection_mode,
			COALESCE(last_error, ''), COALESCE(created_by, ''), created_at, updated_at
		FROM port_forward_sessions
		ORDER BY created_at DESC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []Record{}
	for rows.Next() {
		var rec Record
		if err := rows.Scan(&rec.SessionID, &rec.ClusterID, &rec.Namespace, &rec.TargetKind, &rec.TargetName,
			&rec.LocalPort, &rec.RemotePort, &rec.Status, &rec.ConnectionMode,
			&rec.LastError, &rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, rec)
	}
	return items, rows.Err()
}

func (r *Repository) ListByCluster(ctx context.Context, clusterID string) ([]Record, error) {
	all, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(all))
	for _, item := range all {
		if item.ClusterID == clusterID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *Repository) Upsert(ctx context.Context, rec Record) error {
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO port_forward_sessions
			(session_id, cluster_id, namespace, target_kind, target_name,
			 local_port, remote_port, status, connection_mode, last_error,
			 created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (session_id) DO UPDATE SET
			status = EXCLUDED.status,
			connection_mode = EXCLUDED.connection_mode,
			last_error = EXCLUDED.last_error,
			updated_at = EXCLUDED.updated_at
	`, rec.SessionID, rec.ClusterID, rec.Namespace, rec.TargetKind, rec.TargetName,
		rec.LocalPort, rec.RemotePort, rec.Status, rec.ConnectionMode, rec.LastError,
		rec.CreatedBy, rec.CreatedAt, rec.UpdatedAt).Error
}

func (r *Repository) Delete(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Exec(`DELETE FROM port_forward_sessions WHERE session_id = ?`, sessionID).Error
}

func (r *Repository) MarkStatus(ctx context.Context, sessionID, status, lastErr string) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE port_forward_sessions SET status = ?, last_error = ?, updated_at = ? WHERE session_id = ?
	`, status, lastErr, time.Now().UTC(), sessionID).Error
}
