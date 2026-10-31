package conditionalmutation

import (
	"context"
	"errors"
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

type mutationRow struct {
	ID, ActorID, KeyHash, Status     string
	CreatedAt, DeadlineAt, ExpiresAt time.Time
	FinishedAt                       *time.Time
	EncryptedPayload                 string
}

func (mutationRow) TableName() string { return "platform_conditional_mutation_receipts" }

func (r *Repository) Claim(ctx context.Context, record domainresource.ConditionalMutationRecord) (domainresource.ConditionalMutationRecord, bool, error) {
	row, err := r.encode(record)
	if err != nil {
		return domainresource.ConditionalMutationRecord{}, false, err
	}
	var result domainresource.ConditionalMutationRecord
	created := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Only admission is serialized; provider I/O never holds this lock.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(1150134)).Error; err != nil {
			return err
		}
		var old mutationRow
		err := tx.Where("actor_id = ? AND key_hash = ?", row.ActorID, row.KeyHash).Take(&old).Error
		if err == nil {
			result, err = r.decode(old)
			if err == nil && (result.InputHash != record.InputHash || result.CredentialID != record.CredentialID) {
				return apperrors.ErrConflict
			}
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := mutationCapacity(tx, row.ActorID); err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		result, created = record, true
		return nil
	})
	return result, created, err
}

func mutationCapacity(tx *gorm.DB, actorID string) error {
	var counts struct{ Total, Actor int64 }
	if err := tx.Raw(`SELECT count(*) AS total, count(*) FILTER (WHERE actor_id = ?) AS actor
		FROM platform_conditional_mutation_receipts WHERE status = 'running' AND deadline_at > now()`, actorID).Scan(&counts).Error; err != nil {
		return err
	}
	if counts.Total >= 64 || counts.Actor >= 4 {
		return apperrors.ErrServiceUnavailable
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, actorID, keyHash string) (domainresource.ConditionalMutationRecord, error) {
	var row mutationRow
	if err := r.db.WithContext(ctx).Where("actor_id = ? AND key_hash = ?", actorID, keyHash).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainresource.ConditionalMutationRecord{}, apperrors.ErrNotFound
		}
		return domainresource.ConditionalMutationRecord{}, err
	}
	return r.decode(row)
}

func (r *Repository) Finish(ctx context.Context, record domainresource.ConditionalMutationRecord) error {
	if (record.Status != "succeeded" && record.Status != "unknown") || record.FinishedAt == nil {
		return apperrors.ErrInvalidArgument
	}
	row, err := r.encode(record)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&mutationRow{}).
		Where("id = ? AND actor_id = ? AND key_hash = ? AND status = 'running' AND deadline_at > now()", row.ID, row.ActorID, row.KeyHash).
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
		if err := tx.Exec(`UPDATE platform_conditional_mutation_receipts SET status = 'unknown', finished_at = deadline_at
			WHERE id IN (SELECT id FROM platform_conditional_mutation_receipts WHERE status = 'running' AND deadline_at <= now()
			ORDER BY deadline_at LIMIT 2000 FOR UPDATE SKIP LOCKED)`).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE platform_conditional_mutation_receipts SET encrypted_payload = '', status = 'unknown'
			WHERE id IN (SELECT id FROM platform_conditional_mutation_receipts WHERE expires_at <= now() AND encrypted_payload <> ''
			ORDER BY expires_at LIMIT 2000 FOR UPDATE SKIP LOCKED)`).Error
	})
}
