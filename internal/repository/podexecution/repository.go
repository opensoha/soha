package podexecution

import (
	"context"
	"errors"
	"fmt"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"gorm.io/gorm"
)

type Repository struct {
	db   *gorm.DB
	keys keyring.Ring
}

func New(db *gorm.DB, keys keyring.Ring) *Repository { return &Repository{db: db, keys: keys} }

type executionRow struct {
	ID, ActorID, KeyHash, ClusterID, Namespace, PodName, ExpectedUID, Container, Status string
	CreatedAt, DeadlineAt, ExpiresAt                                                    time.Time
	FinishedAt                                                                          *time.Time
	EncryptedPayload                                                                    string
}

func (executionRow) TableName() string { return "platform_pod_executions" }

func (r *Repository) Claim(ctx context.Context, record domainresource.PodExecutionRecord) (domainresource.PodExecutionRecord, bool, error) {
	row, err := r.encode(record)
	if err != nil {
		return domainresource.PodExecutionRecord{}, false, err
	}
	var claimed domainresource.PodExecutionRecord
	created := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize only the short admission transaction, never provider I/O.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(1150128)).Error; err != nil {
			return err
		}
		var old executionRow
		err := tx.Where("actor_id = ? AND key_hash = ?", row.ActorID, row.KeyHash).Take(&old).Error
		if err == nil {
			claimed, err = r.decode(old)
			if err != nil {
				return err
			}
			if claimed.InputHash != record.InputHash {
				return fmt.Errorf("%w: execution key is bound to another original request", apperrors.ErrConflict)
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := executionCapacity(tx, row.ActorID); err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		claimed, created = record, true
		return nil
	})
	return claimed, created, err
}

func executionCapacity(tx *gorm.DB, actorID string) error {
	var counts struct{ Total, Actor int64 }
	if err := tx.Raw(`SELECT count(*) AS total, count(*) FILTER (WHERE actor_id = ?) AS actor
		FROM platform_pod_executions WHERE status = 'running' AND deadline_at > now()`, actorID).Scan(&counts).Error; err != nil {
		return err
	}
	if counts.Total >= 64 || counts.Actor >= 4 {
		return fmt.Errorf("%w: Pod execution capacity reached", apperrors.ErrServiceUnavailable)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, actorID, id, keyHash string) (domainresource.PodExecutionRecord, error) {
	query := r.db.WithContext(ctx).Where("actor_id = ?", actorID)
	if id != "" {
		query = query.Where("id = ?", id)
	} else {
		query = query.Where("key_hash = ?", keyHash)
	}
	var row executionRow
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainresource.PodExecutionRecord{}, apperrors.ErrNotFound
		}
		return domainresource.PodExecutionRecord{}, err
	}
	return r.decode(row)
}

func (r *Repository) Finish(ctx context.Context, record domainresource.PodExecutionRecord) error {
	if record.Receipt.Status == "running" || record.Receipt.FinishedAt == nil {
		return apperrors.ErrInvalidArgument
	}
	row, err := r.encode(record)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&executionRow{}).
		Where("id = ? AND actor_id = ? AND status = 'running' AND deadline_at > now()", row.ID, row.ActorID).
		Updates(map[string]any{"status": row.Status, "finished_at": row.FinishedAt, "encrypted_payload": row.EncryptedPayload})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}

func (r *Repository) Cleanup(ctx context.Context) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE platform_pod_executions SET status = 'unknown', finished_at = deadline_at
			WHERE id IN (SELECT id FROM platform_pod_executions WHERE status = 'running' AND deadline_at <= now()
			ORDER BY deadline_at LIMIT 2000 FOR UPDATE SKIP LOCKED)`).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE platform_pod_executions SET encrypted_payload = '', cluster_id = '', namespace = '',
			pod_name = '', expected_uid = '', container = '', status = 'unknown'
			WHERE id IN (SELECT id FROM platform_pod_executions WHERE expires_at <= now() AND encrypted_payload <> ''
			ORDER BY expires_at LIMIT 2000 FOR UPDATE SKIP LOCKED)`).Error
	})
}
