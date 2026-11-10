package runtimeupgrade

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Rollback compensates only this checkpoint. It intentionally keeps the legacy
// writer gate closed and never restores runs, approvals, memory bodies or TTLs.
func (r *Repository) Rollback(ctx context.Context, digest string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTables(tx); err != nil {
			return err
		}
		var state string
		if err := tx.Raw(`SELECT state FROM ai_runtime_upgrade_checkpoints WHERE digest=? FOR UPDATE`, digest).Row().Scan(&state); err != nil {
			return err
		}
		if state == "rolled_back" {
			return nil
		}
		var live int64
		if err := tx.Raw(`SELECT count(*) FROM ai_memory_sync_outbox o JOIN ai_runtime_upgrade_items i ON i.id=o.id AND i.entity='created_outbox' WHERE i.checkpoint=? AND o.lease_until>now()`, digest).Row().Scan(&live); err != nil {
			return err
		}
		if live > 0 {
			return fmt.Errorf("migration sync jobs have live leases; stop worker and wait")
		}
		if err := restoreSessions(tx, digest); err != nil {
			return err
		}
		if err := rollbackOutbox(tx, digest); err != nil {
			return err
		}
		return tx.Exec(`UPDATE ai_runtime_upgrade_checkpoints SET state='rolled_back' WHERE digest=?`, digest).Error
	})
}
func restoreSessions(tx *gorm.DB, digest string) error {
	// Compare all preferences to the original plus the one migrated key. A user
	// edit after cutover wins; rollback must never overwrite it.
	return tx.Exec(`UPDATE ai_sessions s SET metadata=(i.original->'metadata')::json
 FROM ai_runtime_upgrade_items i WHERE i.checkpoint=? AND i.entity='session' AND s.id=i.id
 AND s.metadata::jsonb=jsonb_set(`+canonicalMetadata("i.original->'metadata'")+`,'{agentProviderId}','"dsh"'::jsonb)`, digest).Error
}

// The expressions passed here are package-owned SQL, never request input. JSON
// null is a historical default; arrays/scalars are rejected before any writes.
func canonicalMetadata(expression string) string {
	return "CASE WHEN " + expression + "='null'::jsonb THEN '{}'::jsonb ELSE " + expression + " END"
}
func rollbackOutbox(tx *gorm.DB, digest string) error {
	if err := tx.Exec(`INSERT INTO ai_runtime_upgrade_quarantine(entity,id,checkpoint,reason)
 SELECT 'memory',o.record_id,?,'rollback_sync_disabled' FROM ai_memory_sync_outbox o
 JOIN ai_runtime_upgrade_items i ON i.id=o.id AND i.entity='created_outbox' WHERE i.checkpoint=?
 ON CONFLICT(entity,id) DO NOTHING`, digest, digest).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE ai_memory_sync_outbox o SET state='done',lease_token='',lease_until=NULL
 FROM ai_runtime_upgrade_items i WHERE i.id=o.id AND i.entity='created_outbox' AND i.checkpoint=? AND NOT o.attempted`, digest).Error; err != nil {
		return err
	}
	// Submitted/unknown retains must resolve before dependent cleanup. Never mark
	// an ambiguous remote submission failed or replay it under another identity.
	return tx.Exec(`INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload,dependency_id)
 SELECT gen_random_uuid()::text,o.record_id,o.revision,o.document_id,'delete',o.payload,o.id
 FROM ai_memory_sync_outbox o JOIN ai_runtime_upgrade_items i ON i.id=o.id AND i.entity='created_outbox'
 WHERE i.checkpoint=? AND o.attempted ON CONFLICT(record_id,revision,kind) DO NOTHING`, digest).Error
}
