package deliverytrigger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"gorm.io/gorm"
)

type Repository struct {
	db   *gorm.DB
	keys keyring.Ring
}

func New(db *gorm.DB, keys keyring.Ring) *Repository { return &Repository{db: db, keys: keys} }

const triggerColumns = `t.definition, t.execution_token_id, t.updated_by_token_id, t.target_digest, t.signing_secret_ciphertext,
 COALESCE((SELECT e.definition FROM delivery_trigger_events e WHERE e.trigger_id=t.id ORDER BY e.created_at DESC,e.id DESC LIMIT 1),'null'::jsonb),
 COALESCE((SELECT e.definition->>'batchId' FROM delivery_trigger_events e WHERE e.trigger_id=t.id AND e.definition->>'batchId' <> '' ORDER BY e.created_at DESC,e.id DESC LIMIT 1),'')`

func (r *Repository) scanTrigger(row interface{ Scan(...any) error }) (domain.StoredTrigger, error) {
	var item domain.StoredTrigger
	var definition, lastEvent []byte
	var ciphertext string
	err := row.Scan(&definition, &item.ExecutionTokenID, &item.UpdatedByTokenID, &item.TargetDigest, &ciphertext, &lastEvent, &item.LastBatchID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal(definition, &item.Trigger); err != nil {
		return item, err
	}
	if err := json.Unmarshal(lastEvent, &item.LastEvent); err != nil {
		return item, err
	}
	if ciphertext != "" {
		item.SigningSecret, err = secretcrypto.DecryptStringWithKeyring(r.keys, ciphertext)
	}
	return item, err
}

func (r *Repository) Get(ctx context.Context, id string) (domain.StoredTrigger, error) {
	return r.scanTrigger(r.db.WithContext(ctx).Raw(`SELECT `+triggerColumns+` FROM delivery_triggers t WHERE t.id=?`, id).Row())
}

func (r *Repository) List(ctx context.Context, kind, target string, offset, limit int) ([]domain.StoredTrigger, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT `+triggerColumns+` FROM delivery_triggers t WHERE (?='' OR t.target_kind=?) AND (?='' OR t.target_id=?) ORDER BY t.created_at,t.id OFFSET ? LIMIT ?`, kind, kind, target, target, offset, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domain.StoredTrigger{}
	for rows.Next() {
		item, err := r.scanTrigger(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Save(ctx context.Context, item domain.StoredTrigger, expected int) (domain.StoredTrigger, error) {
	item.LastEvent = nil
	data, err := json.Marshal(item.Trigger)
	if err != nil {
		return item, err
	}
	ciphertext := ""
	if item.SigningSecret != "" {
		ciphertext, err = secretcrypto.EncryptStringWithKeyring(r.keys, item.SigningSecret)
		if err != nil {
			return item, err
		}
	}
	if expected == 0 {
		err = r.db.WithContext(ctx).Exec(`INSERT INTO delivery_triggers(id,revision,target_kind,target_id,trigger_type,enabled,definition,execution_token_id,updated_by_token_id,target_digest,signing_secret_ciphertext,created_at,updated_at) VALUES (?,?,?,?,?,?,?::jsonb,?,?,?,?,?,?)`, item.ID, item.Revision, item.TargetKind, item.TargetID, item.Type, item.Enabled, string(data), item.ExecutionTokenID, item.UpdatedByTokenID, item.TargetDigest, ciphertext, item.CreatedAt, item.UpdatedAt).Error
		return item, err
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE delivery_triggers SET revision=?, target_kind=?, target_id=?, trigger_type=?, enabled=?, definition=?::jsonb, execution_token_id=?, updated_by_token_id=?, target_digest=?, signing_secret_ciphertext=?, updated_at=? WHERE id=? AND revision=?`, item.Revision, item.TargetKind, item.TargetID, item.Type, item.Enabled, string(data), item.ExecutionTokenID, item.UpdatedByTokenID, item.TargetDigest, ciphertext, item.UpdatedAt, item.ID, expected)
	if result.Error != nil {
		return item, result.Error
	}
	if result.RowsAffected != 1 {
		return item, fmt.Errorf("%w: trigger changed", apperrors.ErrConflict)
	}
	return item, nil
}

func scanEvent(row interface{ Scan(...any) error }) (domain.StoredEvent, error) {
	var item domain.StoredEvent
	var data []byte
	var claimed sql.NullTime
	err := row.Scan(&data, &item.PayloadDigest, &item.SourceGeneration, &item.Lease, &claimed, &item.Attempts, &item.PreparedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	attempts := item.Attempts
	err = json.Unmarshal(data, &item.Event)
	item.Attempts, item.ClaimedAt = attempts, claimed.Time
	return item, err
}

const eventColumns = `definition,payload_digest,source_generation,lease,claimed_at,attempts,prepared_at`

func readEvent(db *gorm.DB, id string) (domain.StoredEvent, error) {
	return scanEvent(db.Raw(`SELECT `+eventColumns+` FROM delivery_trigger_events WHERE id=?`, id).Row())
}

func (r *Repository) Enqueue(ctx context.Context, event domain.StoredEvent) (domain.Event, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var revision int
		var enabled bool
		if err := tx.Raw(`SELECT revision,enabled FROM delivery_triggers WHERE id=? FOR UPDATE`, event.TriggerID).Row().Scan(&revision, &enabled); err != nil {
			return err
		}
		if !enabled || revision != event.TriggerRevision {
			return fmt.Errorf("%w: trigger disabled or changed", apperrors.ErrConflict)
		}
		var existingID string
		err := tx.Raw(`SELECT id FROM delivery_trigger_events WHERE trigger_id=? AND event_id=?`, event.TriggerID, event.EventID).Row().Scan(&existingID)
		if err == nil {
			previous, err := readEvent(tx, existingID)
			if err != nil {
				return err
			}
			if previous.PayloadDigest != event.PayloadDigest {
				return fmt.Errorf("%w: event ID reused with different content", apperrors.ErrConflict)
			}
			event = previous
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		data, err := json.Marshal(event.Event)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO delivery_trigger_events(id,trigger_id,trigger_revision,event_id,payload_digest,status,definition,created_at,updated_at) VALUES (?,?,?,?,?,?,?::jsonb,?,?)`, event.ID, event.TriggerID, event.TriggerRevision, event.EventID, event.PayloadDigest, event.Status, string(data), event.CreatedAt, event.UpdatedAt).Error
	})
	return event.Event, err
}

func (r *Repository) Events(ctx context.Context, id string, offset, limit int) ([]domain.Event, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT `+eventColumns+` FROM delivery_trigger_events WHERE trigger_id=? ORDER BY created_at DESC,id DESC OFFSET ? LIMIT ?`, id, offset, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domain.Event{}
	for rows.Next() {
		item, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item.Event)
	}
	return items, rows.Err()
}

func (r *Repository) Claim(ctx context.Context, lease string, now time.Time) (domain.StoredEvent, error) {
	var event domain.StoredEvent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A bounded dispatcher has two minutes; a three-minute lease also covers final persistence.
		cutoff := now.Add(-3 * time.Minute)
		if err := tx.Exec(`UPDATE delivery_trigger_events SET status='failed',definition=definition || jsonb_build_object('status','failed','reason','recovery_exhausted','updatedAt',?::timestamptz),updated_at=? WHERE status='processing' AND claimed_at<? AND attempts>=3`, now, now, cutoff).Error; err != nil {
			return err
		}
		var id string
		err := tx.Raw(`SELECT e.id FROM delivery_trigger_events e JOIN delivery_triggers t ON t.id=e.trigger_id
 WHERE (e.status='queued' OR (e.status='processing' AND e.claimed_at<? AND e.attempts<3))
 AND NOT EXISTS(SELECT 1 FROM delivery_trigger_events active WHERE active.trigger_id=e.trigger_id AND active.id<>e.id AND active.status='processing' AND active.claimed_at>=?)
 ORDER BY e.created_at,e.id LIMIT 1 FOR UPDATE OF t,e SKIP LOCKED`, cutoff, cutoff).Row().Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			// Commit exhausted-lease cleanup even when no next event is available.
			return nil
		}
		if err != nil {
			return err
		}
		event, err = readEvent(tx, id)
		if err != nil {
			return err
		}
		event.Status, event.Lease, event.ClaimedAt, event.UpdatedAt = "processing", lease, now, now
		event.Attempts++
		return writeEvent(tx, event, false)
	})
	if err == nil && event.ID == "" {
		return event, apperrors.ErrNotFound
	}
	return event, err
}

func writeEvent(tx *gorm.DB, event domain.StoredEvent, checkLease bool) error {
	data, err := json.Marshal(event.Event)
	if err != nil {
		return err
	}
	query := `UPDATE delivery_trigger_events SET status=?,definition=?::jsonb,source_generation=?,attempts=?,lease=?,claimed_at=?,prepared_at=?,updated_at=? WHERE id=?`
	args := []any{event.Status, string(data), event.SourceGeneration, event.Attempts, event.Lease, event.ClaimedAt, event.PreparedAt, event.UpdatedAt, event.ID}
	if checkLease {
		query += ` AND status='processing' AND lease=?`
		args = append(args, event.Lease)
	}
	result := tx.Exec(query, args...)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}

func (r *Repository) Checkpoint(ctx context.Context, event domain.StoredEvent) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var valid bool
		err := tx.Raw(`SELECT enabled AND revision=? FROM delivery_triggers WHERE id=? FOR UPDATE`, event.TriggerRevision, event.TriggerID).Row().Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return apperrors.ErrConflict
		}
		return writeEvent(tx, event, true)
	})
}

func (r *Repository) Finish(ctx context.Context, event domain.StoredEvent) error {
	return writeEvent(r.db.WithContext(ctx), event, true)
}
