package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	appmemory "github.com/opensoha/soha/internal/application/memory"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

// Quarantine is an operator-owned ledger, never accepted from client payloads.
const stateMismatch = `(m.payload->>'status' IS DISTINCT FROM m.status
 OR m.payload->>'id' IS DISTINCT FROM m.id
 OR m.payload->>'ownerType' IS DISTINCT FROM m.owner_type
 OR m.payload->>'ownerId' IS DISTINCT FROM m.owner_id
 OR m.payload->>'scopeHash' IS DISTINCT FROM m.scope_hash
 OR (m.status='active' AND m.expires_at IS NULL)
 OR NOT ai_memory_timestamp_matches(m.expires_at,m.payload->>'expiresAt')
 OR NOT ai_memory_timestamp_matches(m.deleted_at,m.payload->>'deletedAt'))`
const recordProjection = `m.payload,
 jsonb_build_object('id',m.id,'ownerType',m.owner_type,'ownerId',m.owner_id,'scopeHash',m.scope_hash,'status',m.status,'expiresAt',m.expires_at,'deletedAt',m.deleted_at,'createdAt',m.created_at),
 COALESCE((SELECT q.reason FROM ai_runtime_upgrade_quarantine q WHERE q.entity='memory' AND q.id=m.id),''),` + stateMismatch

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

// syncProjection distinguishes local disablement from completed remote cleanup.
const syncProjection = `CASE
 WHEN EXISTS(SELECT 1 FROM ai_memory_sync_outbox d JOIN ai_memory_sync_outbox o ON o.id=d.dependency_id
   WHERE d.record_id=m.id AND d.kind='delete' AND d.state!='done'
   AND (d.state IN ('failed','retry','unknown') OR o.state='unknown')) THEN 'error'
 WHEN EXISTS(SELECT 1 FROM ai_memory_sync_outbox d WHERE d.record_id=m.id AND d.kind='delete' AND d.state!='done') THEN 'delete_pending'
 WHEN m.status IN ('deleted','expired') THEN 'deleted'
 WHEN EXISTS(SELECT 1 FROM ai_memory_sync_outbox o WHERE o.record_id=m.id AND o.revision=m.revision AND o.kind='retain' AND o.state IN ('unknown','failed','retry')) THEN 'error'
 WHEN EXISTS(SELECT 1 FROM ai_memory_sync_outbox o WHERE o.record_id=m.id AND o.revision=m.revision AND o.kind='retain' AND o.state='done' AND o.attempted=true) THEN 'synced'
 WHEN EXISTS(SELECT 1 FROM ai_memory_sync_outbox o WHERE o.record_id=m.id AND o.revision=m.revision AND o.kind='retain' AND o.state='done' AND o.attempted=false) THEN 'disabled'
 WHEN EXISTS(SELECT 1 FROM ai_memory_sync_outbox o WHERE o.record_id=m.id AND o.revision=m.revision AND o.kind='retain') THEN 'pending'
 ELSE 'disabled' END`

func (r *Repository) ListRecords(ctx context.Context, ownerType, ownerID string, now time.Time) ([]appmemory.Record, error) {
	return r.ListRecordStates(ctx, ownerType, ownerID, now, false)
}
func (r *Repository) ListRecordStates(ctx context.Context, ownerType, ownerID string, now time.Time, includeDeleted bool) ([]appmemory.Record, error) {
	query := `SELECT ` + recordProjection + `,m.revision,` + syncProjection + ` FROM ai_memory_records m WHERE m.owner_type=? AND m.owner_id=?`
	args := []any{ownerType, ownerID}
	if !includeDeleted {
		query += ` AND m.status='active' AND m.expires_at>?`
		args = append(args, now)
	}
	rows, err := r.db.WithContext(ctx).Raw(query+` ORDER BY m.created_at DESC`, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("list memory records: %w", err)
	}
	defer rows.Close()
	items := []appmemory.Record{}
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetRecord(ctx context.Context, id string) (appmemory.Record, error) {
	row := r.db.WithContext(ctx).Raw(`SELECT `+recordProjection+`,m.revision,`+syncProjection+` FROM ai_memory_records m WHERE m.id=?`, id).Row()
	item, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return appmemory.Record{}, appmemory.ErrNotFound
	}
	if err != nil {
		return appmemory.Record{}, fmt.Errorf("get memory record: %w", err)
	}
	return item, nil
}

func scanRecord(row interface{ Scan(...any) error }) (appmemory.Record, error) {
	var payload, identity []byte
	var reason, syncStatus string
	var mismatch bool
	var revision int64
	if err := row.Scan(&payload, &identity, &reason, &mismatch, &revision, &syncStatus); err != nil {
		return appmemory.Record{}, err
	}
	var item appmemory.Record
	if err := json.Unmarshal(payload, &item); err != nil {
		item = appmemory.Record{}
		if reason == "" {
			reason = "invalid_payload"
		}
	}
	if err := json.Unmarshal(identity, &item); err != nil {
		return appmemory.Record{}, fmt.Errorf("decode authoritative memory identity: %w", err)
	}
	if mismatch && reason == "" {
		reason = "legacy_state_mismatch"
	}
	item.Quarantined = reason != ""
	item.QuarantineReason = reason
	item.Revision = revision
	item.SyncStatus = syncStatus
	return item, nil
}

func (r *Repository) PutPolicy(ctx context.Context, policy appmemory.Policy) error {
	payload, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("encode memory policy: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_memory_policies(id,version,payload,updated_at) VALUES(?,?,?,NOW()) ON CONFLICT(id,version) DO UPDATE SET payload=EXCLUDED.payload,updated_at=NOW()`, policy.ID, policy.Version, payload).Error; err != nil {
		return fmt.Errorf("put memory policy: %w", err)
	}
	return nil
}

func (r *Repository) GetPolicy(ctx context.Context, id, version string) (appmemory.Policy, error) {
	var payload []byte
	if err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_memory_policies WHERE id=? AND version=?`, id, version).Row().Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmemory.Policy{}, appmemory.ErrNotFound
		}
		return appmemory.Policy{}, fmt.Errorf("get memory policy: %w", err)
	}
	var policy appmemory.Policy
	if err := json.Unmarshal(payload, &policy); err != nil {
		return appmemory.Policy{}, fmt.Errorf("decode memory policy: %w", err)
	}
	return policy, nil
}

func (r *Repository) ListPolicies(ctx context.Context) ([]appmemory.Policy, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_memory_policies ORDER BY id,version`).Rows()
	if err != nil {
		return nil, fmt.Errorf("list memory policies: %w", err)
	}
	defer rows.Close()
	items := make([]appmemory.Policy, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan memory policy: %w", err)
		}
		var item appmemory.Policy
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, fmt.Errorf("decode memory policy: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
