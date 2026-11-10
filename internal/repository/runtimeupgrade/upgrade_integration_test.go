package runtimeupgrade

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	app "github.com/opensoha/soha/internal/application/memory"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	memories "github.com/opensoha/soha/internal/repository/memory"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func upgradeFixture(t *testing.T) *gorm.DB {
	t.Helper()
	s := os.Getenv("TASK123_UPGRADE_TEST_PORT")
	if s == "" {
		t.Skip("requires exclusively owned Task123 PostgreSQL")
	}
	port, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 4, MaxIdleConns: 2}
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "task123_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.DB().Exec(`CREATE DATABASE "` + cfg.Name + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.DB().Exec(`DROP DATABASE "` + cfg.Name + `" WITH (FORCE)`).Error; err != nil {
			t.Error(err)
		}
	})
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.MigrateFromFile(context.Background(), "../../../migrations/postgres"); err != nil {
		t.Fatal(err)
	}
	return store.DB()
}
func execTest(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatal(err)
	}
}
func TestRuntimeUpgradePostgres(t *testing.T) {
	db := upgradeFixture(t)
	ctx := context.Background()
	r := New(db)
	m := memories.New(db)
	execTest(t, db, `INSERT INTO users(id,username,email,status,created_at,updated_at) VALUES('11111111-1111-4111-8111-111111111111','owner','owner@example.invalid','active',now(),now())`)
	execTest(t, db, `INSERT INTO ai_sessions(id,title,created_by,metadata) VALUES('session','history','11111111-1111-4111-8111-111111111111','{"agentProviderId":"internal","scope":{"cluster":"keep"},"modelPreferences":{"model":"preserve"}}')`)
	execTest(t, db, `INSERT INTO ai_agent_runs(id,provider_id,provider_kind,capability_id,session_id,created_by,status,callback_token,queued_at,created_at,updated_at,native_checkpoint) VALUES('legacy','internal','native','general','session','11111111-1111-4111-8111-111111111111','running','old-token',now(),now(),now(),'{"approved":"preserve"}')`)
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiry := now.Add(time.Hour)
	policy := app.Policy{ID: "policy", Version: "v1", Enabled: true, OwnerTypes: []string{"user"}, MaximumTTL: 2 * time.Hour, DefaultTTL: time.Hour, ExplicitWriteOnly: true}
	if err := m.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	valid := app.Record{ID: "eligible", OwnerType: "user", OwnerID: "11111111-1111-4111-8111-111111111111", ScopeHash: app.Scope("11111111-1111-4111-8111-111111111111", "chat"), Status: "active", Fact: "preserve fact", SourceType: "explicit_user", SourceRefs: []string{"source"}, SourceHashes: map[string]string{"source": strings.Repeat("a", 64)}, PolicyID: "policy", PolicyVer: "v1", CreatedAt: now, ValidFrom: now, ExpiresAt: &expiry}
	for _, id := range []string{"eligible", "ambiguous", "deleted"} {
		v := valid
		v.ID = id
		if id == "ambiguous" {
			v.SourceHashes = nil
		}
		payload, _ := json.Marshal(v)
		execTest(t, db, `INSERT INTO ai_memory_records(id,owner_type,owner_id,scope_hash,status,expires_at,payload,created_at) VALUES(?,'user','11111111-1111-4111-8111-111111111111',?,'active',?,?,?)`, id, valid.ScopeHash, expiry, payload, now)
	}
	if err := m.DeleteRecord(ctx, "deleted", now); err != nil {
		t.Fatal(err)
	}
	conflict := valid
	conflict.ID = "conflicting"
	conflict.Status = "deleted"
	earlier := now.Add(-time.Hour)
	conflict.ExpiresAt = &earlier
	conflictPayload, _ := json.Marshal(conflict)
	execTest(t, db, `INSERT INTO ai_memory_records(id,owner_type,owner_id,scope_hash,status,expires_at,payload,created_at) VALUES('conflicting','user','11111111-1111-4111-8111-111111111111',?,'active',?,?,?)`, valid.ScopeHash, expiry, conflictPayload, now)
	t.Run("preapply_conflicting_state_is_quarantined", func(t *testing.T) {
		v, err := m.GetRecord(ctx, "conflicting")
		if err != nil || !v.Quarantined || v.QuarantineReason != "legacy_state_mismatch" {
			t.Fatalf("unsafe preapply projection %+v %v", v, err)
		}
		list, err := m.ListRecords(ctx, "user", "11111111-1111-4111-8111-111111111111", now)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range list {
			if v.ID == "conflicting" {
				found = true
				if !v.Quarantined {
					t.Fatal("list enabled conflicting row")
				}
			}
		}
		if !found {
			t.Fatal("governance record hidden")
		}
	})
	// A pre-cutover retain is preserved, then disabled by the ledger. Its old
	// lease identity must not authorize submission after cutover.
	execTest(t, db, `INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload,lease_token) SELECT 'old-retain',id,revision,'old-document','retain',payload,'old-lease' FROM ai_memory_records WHERE id='ambiguous'`)
	for _, kind := range []string{"session", "orphan"} {
		v := valid
		v.ID = "owner-check-" + kind
		if kind == "session" {
			v.OwnerType = "session"
			v.OwnerID = "session"
		} else {
			v.OwnerID = "22222222-2222-4222-8222-222222222222"
		}
		payload, _ := json.Marshal(v)
		execTest(t, db, `INSERT INTO ai_memory_records(id,owner_type,owner_id,scope_hash,status,expires_at,payload,created_at) VALUES(?,?,?,?,'active',?,?,?)`, v.ID, v.OwnerType, v.OwnerID, v.ScopeHash, expiry, payload, now)
	}
	t.Run("unsupported_and_orphan_owner_quarantined", func(t *testing.T) {
		q, err := r.DryRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
		reasons := map[string]string{}
		for _, i := range q.Items {
			if i.Entity == "memory" {
				reasons[i.ID] = i.Action + ":" + i.Reason
			}
		}
		if reasons["owner-check-session"] != "quarantine:unsupported_owner_type" || reasons["owner-check-orphan"] != "quarantine:orphan_owner" {
			t.Fatal(reasons)
		}
	})
	var p Plan
	t.Run("dryrun_no_write_and_stale_digest", func(t *testing.T) {
		var err error
		p, err = r.DryRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var count int64
		db.Raw(`SELECT count(*) FROM ai_runtime_upgrade_checkpoints`).Scan(&count)
		if count != 0 {
			t.Fatal("dryrun wrote ledger")
		}
		execTest(t, db, `UPDATE ai_sessions SET title='changed' WHERE id='session'`)
		if err := r.Apply(ctx, ApplyOptions{Digest: p.Digest, BackupSHA256: strings.Repeat("a", 64), QuarantineRuns: []string{"legacy"}}); err == nil {
			t.Fatal("stale digest accepted")
		}
		p, err = r.DryRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("live_lease_and_unaccounted_rejected", func(t *testing.T) {
		if err := r.Apply(ctx, ApplyOptions{Digest: p.Digest, BackupSHA256: strings.Repeat("a", 64)}); err == nil {
			t.Fatal("unaccounted run accepted")
		}
		execTest(t, db, `UPDATE ai_agent_runs SET native_lease_until=now()+interval '1 hour' WHERE id='legacy'`)
		q, err := r.DryRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if r.Apply(ctx, ApplyOptions{Digest: q.Digest, BackupSHA256: strings.Repeat("a", 64), QuarantineRuns: []string{"legacy"}}) == nil {
			t.Fatal("live lease accepted")
		}
		execTest(t, db, `UPDATE ai_agent_runs SET native_lease_until=NULL WHERE id='legacy'`)
		p, err = r.DryRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("live_memory_lease_rejected", func(t *testing.T) {
		execTest(t, db, `UPDATE ai_memory_sync_outbox SET lease_until=now()+interval '1 hour' WHERE id='old-retain'`)
		q, err := r.DryRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if r.Apply(ctx, ApplyOptions{Digest: q.Digest, BackupSHA256: strings.Repeat("a", 64), QuarantineRuns: []string{"legacy"}}) == nil {
			t.Fatal("live sync lease accepted")
		}
		execTest(t, db, `UPDATE ai_memory_sync_outbox SET lease_until=NULL WHERE id='old-retain'`)
		p, err = r.DryRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("atomic_failure_and_restartable_apply", func(t *testing.T) {
		execTest(t, db, `CREATE FUNCTION fail_cutover() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected interruption';END$$`)
		execTest(t, db, `CREATE TRIGGER fail_cutover BEFORE UPDATE ON ai_runtime_upgrade_gate FOR EACH ROW EXECUTE FUNCTION fail_cutover()`)
		o := ApplyOptions{Digest: p.Digest, BackupSHA256: strings.Repeat("a", 64), QuarantineRuns: []string{"legacy"}}
		if r.Apply(ctx, o) == nil {
			t.Fatal("injected failure accepted")
		}
		var count int64
		db.Raw(`SELECT count(*) FROM ai_runtime_upgrade_checkpoints`).Scan(&count)
		if count != 0 {
			t.Fatal("partial checkpoint escaped transaction")
		}
		execTest(t, db, `DROP TRIGGER fail_cutover ON ai_runtime_upgrade_gate`)
		if err := r.Apply(ctx, o); err != nil {
			t.Fatal(err)
		}
		if err := New(db).Apply(ctx, o); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("mixed_writer_fence_and_history", func(t *testing.T) {
		insertErr := db.Exec(`INSERT INTO ai_agent_runs(id,provider_id,provider_kind,capability_id,created_by,status,callback_token,queued_at,created_at,updated_at) VALUES('old-writer-new','internal','native','general','11111111-1111-4111-8111-111111111111','queued','legacy-token',now(),now(),now())`).Error
		if insertErr == nil || !strings.Contains(insertErr.Error(), "legacy runtime is fenced") {
			t.Fatalf("mixed-version admission fence: %v", insertErr)
		}
		if db.Exec(`UPDATE ai_agent_runs SET status='running' WHERE id='legacy'`).Error == nil {
			t.Fatal("old writer accepted")
		}
		var v struct {
			Status, ProviderKind, NativePhase string
			NativeFence                       int64
			NativeCheckpoint                  []byte
			Output                            []byte
		}
		if err := db.Raw(`SELECT status,provider_kind,native_phase,native_fence,native_checkpoint,output FROM ai_agent_runs WHERE id='legacy'`).Scan(&v).Error; err != nil {
			t.Fatal(err)
		}
		if v.Status != "unknown" || v.ProviderKind != "native" || v.NativeFence != 1 || v.NativePhase != "legacy_reconciliation_required" || !strings.Contains(string(v.NativeCheckpoint), "preserve") {
			t.Fatalf("lost legacy facts: %+v", v)
		}
		var metadata string
		db.Raw(`SELECT metadata::text FROM ai_sessions WHERE id='session'`).Scan(&metadata)
		if !strings.Contains(metadata, "dsh") || !strings.Contains(metadata, "preserve") {
			t.Fatal(metadata)
		}
	})
	t.Run("quarantine_readonly_retain_and_delete", func(t *testing.T) {
		v, err := m.GetRecord(ctx, "ambiguous")
		if err != nil || !v.Quarantined || v.Revision != 1 || v.Status != "active" {
			t.Fatalf("%+v %v", v, err)
		}
		if _, err := m.ResolveDocument(ctx, "old-document"); err == nil {
			t.Fatal("quarantine recalled")
		}
		if m.MarkSubmitting(ctx, app.SyncJob{ID: "old-retain", LeaseToken: "old-lease"}) == nil {
			t.Fatal("stale worker submitted quarantine")
		}
		if db.Exec(`UPDATE ai_memory_sync_outbox SET attempted=true WHERE id='old-retain'`).Error == nil {
			t.Fatal("mixed-version worker submitted quarantine")
		}
		v.Quarantined = false
		v.QuarantineReason = ""
		if m.PutRecord(ctx, v) == nil {
			t.Fatal("client removed quarantine")
		}
		var count int64
		db.Raw(`SELECT count(*) FROM ai_memory_sync_outbox WHERE record_id='eligible' AND revision=1`).Scan(&count)
		if count != 1 {
			t.Fatal("outbox not idempotent", count)
		}
		if err := m.DeleteRecord(ctx, "ambiguous", now); err != nil {
			t.Fatal(err)
		}
		v, err = m.GetRecord(ctx, "ambiguous")
		if err != nil || v.Status != "deleted" || !v.Quarantined {
			t.Fatalf("%+v %v", v, err)
		}
		job, err := m.ClaimSync(ctx)
		if err != nil || job.Record.ID != "eligible" {
			t.Fatalf("unexpected eligible claim %+v %v", job, err)
		}
		if err := m.FinishSync(ctx, job, "done"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("rollback_keeps_tombstones_and_gate", func(t *testing.T) {
		if err := m.DeleteRecord(ctx, "eligible", now); err != nil {
			t.Fatal(err)
		}
		if err := r.Rollback(ctx, p.Digest); err != nil {
			t.Fatal(err)
		}
		next, err := r.DryRun(ctx)
		if err != nil || next.Digest == p.Digest {
			t.Fatalf("cutover ledger missing from restart fingerprint: %v", err)
		}
		if err := checkDrain(next, nil); err != nil {
			t.Fatalf("already quarantined unknown became unaccounted: %v", err)
		}
		if err := r.Rollback(ctx, p.Digest); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"eligible", "deleted", "ambiguous"} {
			v, err := m.GetRecord(ctx, id)
			if err != nil || v.Status != "deleted" {
				t.Fatalf("resurrected %s %+v %v", id, v, err)
			}
		}
		if db.Exec(`UPDATE ai_memory_records SET status='active',deleted_at=NULL WHERE id='deleted'`).Error == nil {
			t.Fatal("mixed-version writer revived tombstone")
		}
		if db.Exec(`UPDATE ai_agent_runs SET status='queued' WHERE id='legacy'`).Error == nil {
			t.Fatal("rollback reopened old engine")
		}
		var metadata string
		db.Raw(`SELECT metadata::text FROM ai_sessions WHERE id='session'`).Scan(&metadata)
		if !strings.Contains(metadata, "internal") || !strings.Contains(metadata, "preserve") {
			t.Fatal(metadata)
		}
	})
}
