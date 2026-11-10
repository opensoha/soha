package memory

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	app "github.com/opensoha/soha/internal/application/memory"
	"gorm.io/gorm"
)

func executeMemoryFixture(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatal(err)
	}
}

func TestSelectiveSourceAndPolicyRevocationPostgres(t *testing.T) {
	db := newMemoryPostgresFixture(t)
	r := New(db)
	ctx := t.Context()
	executeMemoryFixture(t, db, `UPDATE ai_runtime_upgrade_gate SET enabled=true WHERE id`)
	executeMemoryFixture(t, db, `INSERT INTO ai_sessions(id,metadata) VALUES('s1','{}'),('s2','{}')`)
	p := app.Policy{ID: "legacy", Version: "v1", Enabled: true, OwnerTypes: []string{"user"}}
	consent := app.Policy{ID: app.PrivatePolicyID("alice", "chat"), Version: "v1", OwnerID: "alice", Mode: "chat", Enabled: true, OwnerTypes: []string{"user"}}
	for _, policy := range []app.Policy{p, consent} {
		if err := r.PutPolicy(ctx, policy); err != nil {
			t.Fatal(err)
		}
	}
	expiry := time.Now().Add(time.Hour)
	for _, item := range []struct{ id, owner, source string }{
		{"deleted-source", "alice", "soha://session/s1/message/m1"},
		{"legacy-in-mode", "alice", "soha://session/s2/message/m2"},
		{"other-owner", "bob", "soha://session/s3/message/m3"},
	} {
		record := app.Record{ID: item.id, OwnerType: "user", OwnerID: item.owner, ScopeHash: app.Scope(item.owner, "chat"),
			Status: "active", SourceType: "explicit_user", SourceRefs: []string{item.source}, PolicyID: p.ID, PolicyVer: p.Version,
			CreatedAt: time.Now(), ExpiresAt: &expiry}
		if err := r.PutRecord(ctx, record); err != nil {
			t.Fatal(err)
		}
		executeMemoryFixture(t, db, `UPDATE ai_memory_sync_outbox SET attempted=true,state='done' WHERE record_id=?`, item.id)
	}
	requireState := func(id, status, sync string, revision int64) {
		t.Helper()
		got, err := r.GetRecord(ctx, id)
		if err != nil || got.Status != status || got.SyncStatus != sync || got.Revision != revision || got.Quarantined {
			t.Fatalf("unexpected record state: %+v %v", got, err)
		}
	}
	requireState("deleted-source", "active", "synced", 1)
	executeMemoryFixture(t, db, `UPDATE ai_sessions SET deleted_at=now() WHERE id='s1'`)
	requireState("deleted-source", "deleted", "delete_pending", 2)
	requireState("legacy-in-mode", "active", "synced", 1)
	requireState("other-owner", "active", "synced", 1)
	writeDenied := false
	consent.WriteEnabled = &writeDenied
	if err := r.PutPolicy(ctx, consent); err != nil {
		t.Fatal(err)
	}
	requireState("legacy-in-mode", "active", "synced", 1)
	queued := app.Record{ID: "queued-before-write-check", OwnerType: "user", OwnerID: "alice", ScopeHash: app.Scope("alice", "chat"),
		Status: "active", SourceType: "explicit_user", SourceRefs: []string{"soha://session/s2/message/m2"},
		PolicyID: p.ID, PolicyVer: p.Version, CreatedAt: time.Now(), ExpiresAt: &expiry}
	if err := r.PutRecord(ctx, queued); err != nil {
		t.Fatal(err)
	}
	lease := "leased-before-policy-recheck"
	var jobID string
	if err := db.Raw(`UPDATE ai_memory_sync_outbox SET lease_token=?,lease_until=now()+interval '1 minute' WHERE record_id=? RETURNING id`, lease, queued.ID).Row().Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkSubmitting(ctx, app.SyncJob{ID: jobID, LeaseToken: lease}); err == nil {
		t.Fatal("global policy bypassed the private write opt-out at the durable submission fence")
	}
	consent.ReadEnabled = &writeDenied
	if err := r.PutPolicy(ctx, consent); err != nil {
		t.Fatal(err)
	}
	requireState("legacy-in-mode", "active", "delete_pending", 2)
	requireState("other-owner", "active", "synced", 1)
}

func TestLifecycleMigrationRepairsOldTombstoneAndFencesOldWriter(t *testing.T) {
	db := newMemoryPostgresFixtureAt(t, false)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(999 * time.Nanosecond)
	expiry := now.Add(time.Hour)
	for _, id := range []string{"old-tombstone", "old-successful-delete", "old-precision"} {
		r := app.Record{ID: id, OwnerType: "user", OwnerID: "alice", ScopeHash: app.Scope("alice", "chat"),
			Status: "active", SourceType: "explicit_user", ExpiresAt: &expiry, CreatedAt: now}
		payload, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		executeMemoryFixture(t, db, `INSERT INTO ai_memory_records(id,owner_type,owner_id,scope_hash,status,expires_at,payload,created_at) VALUES(?,?,?,?,?,?,?,?)`, id, r.OwnerType, r.OwnerID, r.ScopeHash, r.Status, expiry.Truncate(time.Microsecond), payload, now)
		executeMemoryFixture(t, db, `INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload,attempted,state) VALUES(?,?,1,?,'retain',?,true,'failed')`, "retain-"+id, id, "doc-"+id, payload)
	}
	executeMemoryFixture(t, db, `UPDATE ai_memory_sync_outbox SET next_attempt_at=now()+interval '1 hour' WHERE record_id='old-precision'`)
	executeMemoryFixture(t, db, `UPDATE ai_memory_records SET status='deleted',deleted_at=now() WHERE id LIKE 'old-%' AND id<>'old-precision'`)
	executeMemoryFixture(t, db, `INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload,dependency_id,state)
	 SELECT 'old-delete-job',record_id,revision,document_id,'delete',payload,id,'done'
	 FROM ai_memory_sync_outbox WHERE id='retain-old-successful-delete'`)
	applyMemoryLifecycleMigration(t, db)
	r := New(db)
	v, err := r.GetRecord(ctx, "old-precision")
	if err != nil || v.Quarantined {
		t.Fatalf("benign precision mismatch quarantined: %+v %v", v, err)
	}
	for range 2 {
		job, err := r.ClaimSync(ctx)
		if err != nil || job.Kind != "delete" || job.RetainOperationID != "retain-"+job.Record.ID {
			t.Fatalf("legacy cleanup not recovered: %+v %v", job, err)
		}
		if err := r.FinishSync(ctx, job, "done"); err != nil {
			t.Fatal(err)
		}
		v, err := r.GetRecord(ctx, job.Record.ID)
		if err != nil || v.SyncStatus != "deleted" {
			t.Fatalf("historical retain failure masked deletion: %+v %v", v, err)
		}
	}
	executeMemoryFixture(t, db, `UPDATE ai_runtime_upgrade_gate SET enabled=true WHERE id`)
	if err := db.Exec(`UPDATE ai_memory_records SET payload=payload || '{"fact":"old write"}' WHERE id='old-precision'`).Error; err == nil {
		t.Fatal("mixed-version update bypassed revision fence")
	}
	if err := db.Exec(`INSERT INTO ai_memory_records(id,owner_type,owner_id,scope_hash,status,expires_at,payload,created_at)
	 SELECT 'old-insert',owner_type,owner_id,scope_hash,status,expires_at,payload,created_at FROM ai_memory_records WHERE id='old-precision'`).Error; err == nil {
		t.Fatal("mixed-version insert bypassed outbox fence")
	}
	if _, err := r.GetRecord(ctx, "old-insert"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("legacy insert persisted: %v", err)
	}
}
