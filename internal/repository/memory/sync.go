package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	app "github.com/opensoha/soha/internal/application/memory"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func normalizeRecordTimes(r app.Record) app.Record {
	r.CreatedAt = r.CreatedAt.UTC().Truncate(time.Microsecond)
	r.ValidFrom = r.ValidFrom.UTC().Truncate(time.Microsecond)
	if r.ExpiresAt != nil {
		expiry := r.ExpiresAt.UTC().Truncate(time.Microsecond)
		r.ExpiresAt = &expiry
	}
	if r.DeletedAt != nil {
		deleted := r.DeletedAt.UTC().Truncate(time.Microsecond)
		r.DeletedAt = &deleted
	}
	return r
}

// PutRecord fences ownership and tombstones while committing the outbox in the same transaction.
func (r *Repository) PutRecord(ctx context.Context, record app.Record) error {
	record = normalizeRecordTimes(record)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 123))`, record.ID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT id FROM ai_memory_records WHERE id=? FOR UPDATE`, record.ID).Error; err != nil {
			return err
		}
		old, err := New(tx).GetRecord(ctx, record.ID)
		if err != nil && !errors.Is(err, app.ErrNotFound) {
			return err
		}
		record.SyncStatus = ""
		record.Quarantined = false
		record.QuarantineReason = ""
		record.Revision = 1
		if err == nil {
			if old.Quarantined {
				return apperrors.ErrAccessDenied
			}
			if old.OwnerType != record.OwnerType || old.OwnerID != record.OwnerID || old.ScopeHash != record.ScopeHash || old.Status == "deleted" {
				return apperrors.ErrAccessDenied
			}
			old = normalizeRecordTimes(old)
			old.SyncStatus = ""
			record.CreatedAt = old.CreatedAt
			record.Revision = old.Revision
			if reflect.DeepEqual(old, record) {
				return nil
			}
			record.Revision++
		}
		payload, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO ai_memory_records(id,owner_type,owner_id,scope_hash,status,expires_at,payload,created_at,deleted_at,revision) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,expires_at=EXCLUDED.expires_at,payload=EXCLUDED.payload,deleted_at=EXCLUDED.deleted_at,revision=EXCLUDED.revision`, record.ID, record.OwnerType, record.OwnerID, record.ScopeHash, record.Status, record.ExpiresAt, payload, record.CreatedAt, record.DeletedAt, record.Revision).Error; err != nil {
			return err
		}
		return enqueueRetain(tx, record, payload)
	})
}
func enqueueRetain(tx *gorm.DB, r app.Record, payload []byte) error {
	sum := sha256.Sum256([]byte(r.ID))
	document := fmt.Sprintf("soha-%s-r%d", hex.EncodeToString(sum[:]), r.Revision)
	return tx.Exec(`INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload) VALUES(?,?,?,?,'retain',?)`, uuid.NewString(), r.ID, r.Revision, document, payload).Error
}
func queueDeletes(tx *gorm.DB, id string) error {
	// The original payload keeps the old owner/scope mapping even if migration later permits moves.
	return tx.Exec(`INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload,dependency_id)
 SELECT gen_random_uuid()::text,record_id,revision,document_id,'delete',payload,id FROM ai_memory_sync_outbox WHERE record_id=? AND kind='retain'
 ON CONFLICT(record_id,revision,kind) DO NOTHING`, id).Error
}
func (r *Repository) DeleteRecord(ctx context.Context, id string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,123))`, id).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT id FROM ai_memory_records WHERE id=? FOR UPDATE`, id).Error; err != nil {
			return err
		}
		old, err := New(tx).GetRecord(ctx, id)
		if err != nil {
			return err
		}
		if old.Status == "deleted" {
			return queueDeletes(tx, id)
		}
		now = now.UTC().Truncate(time.Microsecond)
		return tx.Exec(`UPDATE ai_memory_records SET status='deleted',deleted_at=?,revision=revision+1,
		 payload=CASE WHEN ? THEN payload ELSE payload || jsonb_build_object('status','deleted','deletedAt',?::timestamptz) END
		 WHERE id=?`, now, old.Quarantined, now, id).Error
	})
}

// InvalidateSync fences the evaluated revision, so a stale validation cannot revoke a correction.
func (r *Repository) InvalidateSync(ctx context.Context, record app.Record, sourceRevoked bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var id string
		err := tx.Raw(`SELECT id FROM ai_memory_records WHERE id=? AND revision=? AND status='active' FOR UPDATE`, record.ID, record.Revision).Row().Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if record.Quarantined {
			return queueDeletes(tx, id)
		}
		query := `UPDATE ai_memory_records SET revision=revision+1 WHERE id=?`
		if sourceRevoked {
			query = `UPDATE ai_memory_records SET revision=revision+1,status='deleted',deleted_at=now(),
				 payload=payload || jsonb_build_object('status','deleted','deletedAt',now()) WHERE id=?`
		}
		return tx.Exec(query, id).Error
	})
}

func (r *Repository) ClaimSync(ctx context.Context) (app.SyncJob, error) {
	var j app.SyncJob
	if err := r.queueExpired(ctx); err != nil {
		return j, err
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var payload []byte
		// An attempted quarantined retain can only reconcile its original
		// operation: BackendService never resubmits Attempted=true. Unsubmitted
		// quarantined facts stay blocked; confirmed terminal receipts unblock deletes.
		row := tx.Raw(`SELECT o.id,o.revision,o.document_id,o.kind,o.payload,o.attempted,
   COALESCE(CASE WHEN d.attempted THEN d.id END,''), o.state IN ('done','failed')
   FROM ai_memory_sync_outbox o JOIN ai_memory_records m ON m.id=o.record_id
   LEFT JOIN ai_memory_sync_outbox d ON d.id=o.dependency_id
   WHERE o.next_attempt_at<=NOW() AND (o.lease_until IS NULL OR o.lease_until<NOW())
   AND (o.state IN ('pending','retry','unknown') OR
      (o.kind='retain' AND o.attempted AND o.state IN ('done','failed') AND m.revision=o.revision AND m.status='active'
       AND NOT EXISTS(SELECT 1 FROM ai_memory_sync_outbox x WHERE x.record_id=o.record_id AND x.revision=o.revision AND x.kind='delete')))
   AND (o.kind='delete' OR o.attempted
      OR EXISTS(SELECT 1 FROM ai_memory_sync_outbox x WHERE x.record_id=o.record_id AND x.revision=o.revision AND x.kind='delete')
      OR NOT EXISTS(SELECT 1 FROM ai_runtime_upgrade_quarantine q WHERE q.entity='memory' AND q.id=o.record_id))
   AND (d.id IS NULL OR d.state IN ('done','failed'))
   ORDER BY o.next_attempt_at,o.created_at,o.id FOR UPDATE OF o SKIP LOCKED LIMIT 1`).Row()
		if err := row.Scan(&j.ID, &j.Revision, &j.DocumentID, &j.Kind, &payload, &j.Attempted, &j.RetainOperationID, &j.Revalidate); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return app.ErrNotFound
			}
			return err
		}
		if err := json.Unmarshal(payload, &j.Record); err != nil {
			return err
		}
		j.Record.Revision = j.Revision
		j.LeaseToken = uuid.NewString()
		return tx.Exec(`UPDATE ai_memory_sync_outbox SET lease_token=?,lease_until=NOW()+INTERVAL '2 minutes' WHERE id=?`, j.LeaseToken, j.ID).Error
	})
	return j, err
}
func (r *Repository) FinishSync(ctx context.Context, j app.SyncJob, state string) error {
	if state != "pending" && state != "done" && state != "retry" && state != "failed" && state != "unknown" {
		return fmt.Errorf("invalid sync state")
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_memory_sync_outbox
	 SET state=CASE WHEN ? THEN state ELSE ? END,operation_purged=operation_purged OR ?,lease_token='',lease_until=NULL,
	     next_attempt_at=NOW()+CASE WHEN ? THEN INTERVAL '1 minute' ELSE INTERVAL '10 seconds' END
	 WHERE id=? AND lease_token=?`, j.Revalidate, state, state == "done" && j.Kind == "delete" && j.RetainOperationID != "", j.Revalidate || state == "done" || state == "failed", j.ID, j.LeaseToken)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("memory sync lease lost")
	}
	return nil
}
func (r *Repository) ResolveDocument(ctx context.Context, document string) (app.Record, error) {
	var id string
	var revision int64
	// Vendor recall can expose partial facts before a retain finishes. Only a
	// submitted retain with a durably confirmed successful receipt is resolvable.
	err := r.db.WithContext(ctx).Raw(`SELECT record_id,revision FROM ai_memory_sync_outbox o WHERE document_id=? AND kind='retain' AND attempted=true AND state='done' AND NOT EXISTS(SELECT 1 FROM ai_memory_sync_outbox x WHERE x.record_id=o.record_id AND x.revision=o.revision AND x.kind='delete') AND NOT EXISTS(SELECT 1 FROM ai_runtime_upgrade_quarantine q WHERE q.entity='memory' AND q.id=o.record_id)`, document).Row().Scan(&id, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return app.Record{}, app.ErrNotFound
	}
	if err != nil {
		return app.Record{}, err
	}
	record, err := r.GetRecord(ctx, id)
	if err != nil {
		return app.Record{}, err
	}
	if record.Revision != revision {
		return app.Record{}, app.ErrNotFound
	}
	return record, nil
}

func (r *Repository) MarkSubmitting(ctx context.Context, j app.SyncJob) error {
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_memory_sync_outbox o SET attempted=true WHERE id=? AND lease_token=? AND lease_until>now() AND NOT attempted
 AND NOT EXISTS(SELECT 1 FROM ai_runtime_upgrade_quarantine q WHERE q.entity='memory' AND q.id=o.record_id)
 AND NOT EXISTS(SELECT 1 FROM ai_memory_sync_outbox d WHERE d.record_id=o.record_id AND d.revision=o.revision AND d.kind='delete')
 AND EXISTS(SELECT 1 FROM ai_memory_records m
   JOIN ai_memory_policies p ON p.id=m.payload->>'policyId' AND p.version=m.payload->>'policyVersion'
   LEFT JOIN ai_memory_policies c ON COALESCE(p.payload->>'ownerId','')=''
    AND COALESCE(p.payload->>'mode','') IN ('','chat')
    AND c.id=ai_memory_chat_policy_id(m.owner_id) AND c.version='v1'
   WHERE m.id=o.record_id AND m.revision=o.revision AND m.status='active' AND m.deleted_at IS NULL AND m.expires_at>now()
   AND p.payload->'enabled'='true'::jsonb AND p.payload->'writeEnabled' IS DISTINCT FROM 'false'::jsonb
   AND (c.id IS NULL OR (c.payload->>'ownerId'=m.owner_id AND c.payload->>'mode'='chat'
        AND c.payload->'enabled'='true'::jsonb AND c.payload->'writeEnabled' IS DISTINCT FROM 'false'::jsonb)))`, j.ID, j.LeaseToken)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("memory sync lease lost")
	}
	return nil
}

func (r *Repository) EnsureBank(ctx context.Context, record app.Record, bank string) error {
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_memory_backend_banks(owner_type,owner_id,scope_hash,bank_id) VALUES(?,?,?,?) ON CONFLICT(owner_type,owner_id,scope_hash) DO NOTHING`, record.OwnerType, record.OwnerID, record.ScopeHash, bank).Error; err != nil {
		return err
	}
	var stored string
	if err := r.db.WithContext(ctx).Raw(`SELECT bank_id FROM ai_memory_backend_banks WHERE owner_type=? AND owner_id=? AND scope_hash=?`, record.OwnerType, record.OwnerID, record.ScopeHash).Row().Scan(&stored); err != nil {
		return err
	}
	if stored != bank {
		return fmt.Errorf("memory bank namespace changed; explicit migration required")
	}
	return nil
}

func (r *Repository) queueExpired(ctx context.Context) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []string
		if err := tx.Raw(`SELECT id FROM ai_memory_records WHERE status='active' AND expires_at<=NOW() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 100`).Scan(&ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := tx.Exec(`UPDATE ai_memory_records m SET status='expired',revision=revision+1,
			 payload=CASE WHEN jsonb_typeof(payload)='object' AND
			  NOT EXISTS(SELECT 1 FROM ai_runtime_upgrade_quarantine q WHERE q.entity='memory' AND q.id=m.id)
			 THEN payload || '{"status":"expired"}'::jsonb ELSE payload END WHERE id=?`, id).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
