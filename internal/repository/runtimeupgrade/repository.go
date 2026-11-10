package runtimeupgrade

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

var inventories = []struct{ entity, table, key string }{
	{"session", "ai_sessions", "id"}, {"run", "ai_agent_runs", "id"},
	{"memory", "ai_memory_records", "id"}, {"policy", "ai_memory_policies", "id || ':' || version"},
	{"outbox", "ai_memory_sync_outbox", "id"}, {"approval", "ai_gateway_approval_requests", "id"},
	{"task", "workflow_runs", "id"}, {"workflow_approval", "workflow_approvals", "id"},
	{"settings", "app_settings", "setting_key"}, {"ai_client", "ai_clients", "id"},
	{"provider_catalog", "ai_agent_provider_catalog_state", "id"},
	{"provider_ack", "ai_agent_provider_registry_acks", "runner_id"},
	{"cutover_gate", "ai_runtime_upgrade_gate", "id::text"},
	{"quarantine_ledger", "ai_runtime_upgrade_quarantine", "entity || ':' || id"},
}

func inventory(tx *gorm.DB) ([]row, time.Time, error) {
	var now time.Time
	if err := tx.Raw(`SELECT now()`).Row().Scan(&now); err != nil {
		return nil, now, err
	}
	out := []row{}
	for _, spec := range inventories {
		// Names and expressions are a closed, package-owned list, never user input.
		projection := "to_jsonb(t)"
		if spec.entity == "memory" {
			projection += ` || jsonb_build_object('_ownerExists',EXISTS(SELECT 1 FROM users u WHERE u.id::text=t.owner_id))`
		}
		if spec.entity == "run" {
			projection += ` || jsonb_build_object('_quarantined',EXISTS(SELECT 1 FROM ai_runtime_upgrade_quarantine q WHERE q.entity='run' AND q.id=t.id))`
		}
		rows, err := tx.Raw(`SELECT ` + spec.key + `,` + projection + ` FROM ` + spec.table + ` t ORDER BY 1`).Rows()
		if err != nil {
			return nil, now, err
		}
		for rows.Next() {
			r := row{Entity: spec.entity}
			if err := rows.Scan(&r.ID, &r.Data); err != nil {
				rows.Close()
				return nil, now, err
			}
			out = append(out, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, now, err
		}
	}
	return out, now, nil
}

// DryRun uses a read-only repeatable-read snapshot, including approval/task and
// provider configuration hashes. It creates no checkpoint or business rows.
func (r *Repository) DryRun(ctx context.Context) (Plan, error) {
	var p Plan
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, now, err := inventory(tx)
		if err == nil {
			p = build(rows, now)
		}
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return p, err
}

type ApplyOptions struct {
	Digest, BackupSHA256 string
	QuarantineRuns       []string
}

func lockTables(tx *gorm.DB) error {
	// Serialize maintenance and exclude all old writers, including inserts, while
	// the reviewed snapshot is compared and the permanent cutover gate is enabled.
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(123,4)`).Error; err != nil {
		return err
	}
	for _, spec := range inventories {
		if err := tx.Exec(`LOCK TABLE ` + spec.table + ` IN SHARE ROW EXCLUSIVE MODE`).Error; err != nil {
			return err
		}
	}
	if err := tx.Exec(`LOCK TABLE users IN SHARE MODE`).Error; err != nil {
		return err
	}
	return tx.Exec(`LOCK TABLE ai_runtime_upgrade_gate IN EXCLUSIVE MODE`).Error
}
func (r *Repository) Apply(ctx context.Context, o ApplyOptions) error {
	ids, err := boundedIDs(o.QuarantineRuns)
	if err != nil {
		return err
	}
	if len(o.Digest) != 64 || len(o.BackupSHA256) != 64 {
		return fmt.Errorf("reviewed digest and backup SHA256 required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTables(tx); err != nil {
			return err
		}
		var state string
		err := tx.Raw(`SELECT state FROM ai_runtime_upgrade_checkpoints WHERE digest=?`, o.Digest).Row().Scan(&state)
		if err == nil {
			if state == "applied" {
				return nil
			}
			return fmt.Errorf("checkpoint already rolled back; create a fresh plan")
		}
		if err != sql.ErrNoRows {
			return err
		}
		rows, now, err := inventory(tx)
		if err != nil {
			return err
		}
		p := build(rows, now)
		if p.Digest != o.Digest {
			return fmt.Errorf("inventory changed; repeat dry-run")
		}
		if err := checkDrain(p, ids); err != nil {
			return err
		}
		b, _ := json.Marshal(p)
		if err := tx.Exec(`INSERT INTO ai_runtime_upgrade_checkpoints(digest,backup_sha256,plan,state) VALUES(?,?,?,'applied')`, p.Digest, o.BackupSHA256, b).Error; err != nil {
			return err
		}
		source := map[string]row{}
		for _, row := range rows {
			source[row.Entity+":"+row.ID] = row
		}
		for _, item := range p.Items {
			if err := applyItem(tx, p.Digest, item, source[item.Entity+":"+item.ID]); err != nil {
				return err
			}
		}
		return tx.Exec(`UPDATE ai_runtime_upgrade_gate SET enabled=true,checkpoint=? WHERE id`, p.Digest).Error
	})
}
func checkDrain(p Plan, ids map[string]bool) error {
	used := map[string]bool{}
	for _, i := range p.Items {
		if i.Action == "manual_review" {
			return fmt.Errorf("session %s has invalid metadata; explicit repair required", i.ID)
		}
		if i.Action == "live_lease" {
			return fmt.Errorf("run %s has a live lease; wait for expiry before quarantine", i.ID)
		}
		if i.Action == "drain" {
			if !ids[i.ID] {
				return fmt.Errorf("run %s requires drain or explicit quarantine", i.ID)
			}
			used[i.ID] = true
		}
	}
	for id := range ids {
		if !used[id] {
			return fmt.Errorf("quarantine ID %s is not a reviewed nonterminal legacy run", id)
		}
	}
	return nil
}
func saveOriginal(tx *gorm.DB, digest string, item Item, r row) error {
	return tx.Exec(`INSERT INTO ai_runtime_upgrade_items(checkpoint,entity,id,original) VALUES(?,?,?,?)`, digest, item.Entity, item.ID, []byte(r.Data)).Error
}
func quarantine(tx *gorm.DB, digest string, i Item) error {
	return tx.Exec(`INSERT INTO ai_runtime_upgrade_quarantine(entity,id,checkpoint,reason) VALUES(?,?,?,?) ON CONFLICT(entity,id) DO NOTHING`, i.Entity, i.ID, digest, i.Reason).Error
}
func applyItem(tx *gorm.DB, digest string, i Item, r row) error {
	switch i.Action {
	case "fence_provider":
		return tx.Exec(`UPDATE ai_runtime_upgrade_gate SET legacy_provider_ids=array_append(legacy_provider_ids,?) WHERE id AND NOT (?=ANY(legacy_provider_ids))`, i.ID, i.ID).Error
	case "map_dsh":
		if err := saveOriginal(tx, digest, i, r); err != nil {
			return err
		}
		return tx.Exec(`UPDATE ai_sessions SET metadata=jsonb_set(`+canonicalMetadata("metadata::jsonb")+`,'{agentProviderId}','"dsh"'::jsonb)::json WHERE id=?`, i.ID).Error
	case "drain":
		if err := saveOriginal(tx, digest, i, r); err != nil {
			return err
		}
		if err := quarantine(tx, digest, i); err != nil {
			return err
		}
		return tx.Exec(`UPDATE ai_agent_runs SET status='unknown',native_phase='legacy_reconciliation_required',native_fence=native_fence+1,native_version=native_version+1,native_lease_until=NULL,native_cancel_at=now(),callback_token='',output=(output::jsonb || '{"stopReason":"legacy_reconciliation_required","executionReadOnly":true}'::jsonb)::json,error_message='Legacy execution quarantined; side effects require reconciliation',updated_at=now() WHERE id=?`, i.ID).Error
	case "quarantine":
		if err := saveOriginal(tx, digest, i, r); err != nil {
			return err
		}
		if err := quarantine(tx, digest, i); err != nil {
			return err
		}
		// Unsubmitted facts have no remote effects. Submitted jobs retain their
		// operation receipts until the dependent cleanup can prove settlement.
		if err := tx.Exec(`UPDATE ai_memory_sync_outbox SET state='done',lease_token='',lease_until=NULL WHERE record_id=? AND kind='retain' AND NOT attempted`, i.ID).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload,dependency_id)
		 SELECT gen_random_uuid()::text,record_id,revision,document_id,'delete',payload,id
		 FROM ai_memory_sync_outbox WHERE record_id=? AND kind='retain' AND attempted
		 ON CONFLICT(record_id,revision,kind) DO NOTHING`, i.ID).Error
	case "enqueue":
		return enqueue(tx, digest, i, r)
	}
	return nil
}
func enqueue(tx *gorm.DB, digest string, i Item, r row) error {
	v := object(r.Data)
	var identity struct {
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal(r.Data, &identity); err != nil {
		return err
	}
	revision := identity.Revision
	if revision < 1 {
		return fmt.Errorf("invalid memory revision")
	}
	job, doc := stableJob(i.ID, revision)
	payload, _ := json.Marshal(v["payload"])
	result := tx.Exec(`INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload) VALUES(?,?,?,?,'retain',?) ON CONFLICT(record_id,revision,kind) DO NOTHING`, job, i.ID, revision, doc, payload)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	return saveOriginal(tx, digest, Item{Entity: "created_outbox", ID: job}, row{Data: json.RawMessage(`{}`)})
}
