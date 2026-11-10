package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	app "github.com/opensoha/soha/internal/application/memory"
)

func quarantineRecord(t *testing.T, r *Repository, id string) {
	t.Helper()
	if err := r.db.Exec(`INSERT INTO ai_runtime_upgrade_checkpoints(digest,backup_sha256,plan,state) VALUES('test-checkpoint','test-backup','{}','applied') ON CONFLICT DO NOTHING`).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.db.Exec(`INSERT INTO ai_runtime_upgrade_quarantine(entity,id,checkpoint,reason) VALUES('memory',?,'test-checkpoint','missing_provenance')`, id).Error; err != nil {
		t.Fatal(err)
	}
}

func quarantineFixtureRecord(t *testing.T, r *Repository, id, owner string) app.Record {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiry := now.Add(time.Hour)
	v := app.Record{ID: id, OwnerType: "user", OwnerID: owner, ScopeHash: app.Scope(owner, "chat"), Status: "active", Fact: "private", ExpiresAt: &expiry, CreatedAt: now, ValidFrom: now}
	if err := r.PutRecord(t.Context(), v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMemoryAuthoritativeIdentityPostgres(t *testing.T) {
	db := newMemoryPostgresFixture(t)
	r := New(db)
	ctx := t.Context()
	quarantineFixtureRecord(t, r, "r", "alice")
	quarantineFixtureRecord(t, r, "target", "carol")
	forged := quarantineFixtureRecord(t, r, "bob-record", "bob")
	forged.ID = "target"
	forged.OwnerType = "session"
	forged.ScopeHash = "forged-scope"
	payload, _ := json.Marshal(forged)
	if err := db.Exec(`UPDATE ai_memory_records SET payload=? WHERE id='r'`, payload).Error; err != nil {
		t.Fatal(err)
	}
	t.Run("preapply_get_and_list_keep_relational_identity", func(t *testing.T) {
		v, err := r.GetRecord(ctx, "r")
		if err != nil || v.ID != "r" || v.OwnerType != "user" || v.OwnerID != "alice" || v.ScopeHash != app.Scope("alice", "chat") || !v.Quarantined || v.QuarantineReason != "legacy_state_mismatch" {
			t.Fatalf("unsafe identity %+v %v", v, err)
		}
		items, err := r.ListRecords(ctx, "user", "alice", time.Now())
		if err != nil || len(items) != 1 || items[0].ID != "r" || items[0].OwnerID != "alice" || !items[0].Quarantined {
			t.Fatalf("unsafe list %+v %v", items, err)
		}
		var originalID string
		var gate bool
		if err := db.Raw(`SELECT payload->>'id' FROM ai_memory_records WHERE id='r'`).Row().Scan(&originalID); err != nil {
			t.Fatal(err)
		}
		if err := db.Raw(`SELECT enabled FROM ai_runtime_upgrade_gate WHERE id`).Row().Scan(&gate); err != nil {
			t.Fatal(err)
		}
		if originalID != "target" || gate {
			t.Fatal("projection changed source or enabled cutover")
		}
	})
	t.Run("delete_quarantined_row_preserves_other_target", func(t *testing.T) {
		if err := r.DeleteRecord(ctx, "r", time.Now()); err != nil {
			t.Fatal(err)
		}
		deleted, err := r.GetRecord(ctx, "r")
		if err != nil || deleted.Status != "deleted" || deleted.ID != "r" || deleted.OwnerID != "alice" {
			t.Fatalf("wrong delete %+v %v", deleted, err)
		}
		other, err := r.GetRecord(ctx, "target")
		if err != nil || other.Status != "active" || other.OwnerID != "carol" {
			t.Fatalf("target changed %+v %v", other, err)
		}
	})
}

type quarantineBackend struct {
	status                       string
	err                          error
	retains, deletes, operations int
}

func (b *quarantineBackend) Retain(context.Context, string, app.SyncJob) error {
	b.retains++
	return fmt.Errorf("retain must never be called")
}
func (b *quarantineBackend) Operation(context.Context, string, string) (string, error) {
	b.operations++
	return b.status, b.err
}
func (b *quarantineBackend) DeleteDocument(context.Context, string, string, string) error {
	b.deletes++
	return nil
}
func (b *quarantineBackend) Recall(context.Context, string, string) ([]app.Candidate, error) {
	return nil, nil
}

type quarantineSources struct{ calls int }

func (s *quarantineSources) ValidateSource(context.Context, app.Record) error {
	s.calls++
	return errors.New("quarantined source denied")
}

func TestQuarantinedOperationReconciliationPostgres(t *testing.T) {
	db := newMemoryPostgresFixture(t)
	r := New(db)
	ctx := t.Context()
	quarantineFixtureRecord(t, r, "unattempted", "alice")
	quarantineRecord(t, r, "unattempted")
	t.Run("unattempted_is_not_claimable", func(t *testing.T) {
		if _, err := r.ClaimSync(ctx); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("unattempted retain claimed: %v", err)
		}
	})
	for _, state := range []string{"completed", "failed", "not_found"} {
		t.Run(state, func(t *testing.T) {
			id := "attempted-" + state
			quarantineFixtureRecord(t, r, id, "alice")
			if err := db.Exec(`UPDATE ai_memory_sync_outbox SET attempted=true,state='unknown' WHERE record_id=?`, id).Error; err != nil {
				t.Fatal(err)
			}
			quarantineRecord(t, r, id)
			if err := r.DeleteRecord(ctx, id, time.Now()); err != nil {
				t.Fatal(err)
			}
			b := &quarantineBackend{status: state}
			if state == "not_found" {
				b.err = app.ErrBackendNotFound
			}
			sources := &quarantineSources{}
			service, err := app.NewBackendService(r, r, b, sources, "isolated-test")
			if err != nil {
				t.Fatal(err)
			}
			worked, err := service.SyncOne(ctx)
			if !worked {
				t.Fatalf("quarantined operation not reconciled: %v", err)
			}
			if b.operations != 1 || b.retains != 0 || sources.calls != 0 {
				t.Fatalf("not lookup only: %+v sourceCalls=%d", b, sources.calls)
			}
			var retainState string
			if e := db.Raw(`SELECT state FROM ai_memory_sync_outbox WHERE record_id=? AND kind='retain'`, id).Row().Scan(&retainState); e != nil {
				t.Fatal(e)
			}
			if state == "not_found" {
				if !errors.Is(err, app.ErrSyncUnknown) || retainState != "unknown" {
					t.Fatalf("unknown lost: %s %v", retainState, err)
				}
				if _, err := r.ClaimSync(ctx); !errors.Is(err, app.ErrNotFound) {
					t.Fatalf("unresolved dependency released: %v", err)
				}
			} else {
				if state == "completed" && err != nil {
					t.Fatal(err)
				}
				if state == "failed" && err == nil {
					t.Fatal("failed operation claimed success")
				}
				worked, err = service.SyncOne(ctx)
				if err != nil || !worked || b.deletes != 1 || b.retains != 0 {
					t.Fatalf("dependent cleanup blocked: %+v %v", b, err)
				}
			}
			v, err := r.GetRecord(ctx, id)
			if err != nil || !v.Quarantined || v.Status != "deleted" {
				t.Fatalf("ledger/tombstone cleared: %+v %v", v, err)
			}
		})
	}
}

func TestMalformedLegacyMemoryRemainsListableAndDeletablePostgres(t *testing.T) {
	db := newMemoryPostgresFixture(t)
	r := New(db)
	ctx := t.Context()
	quarantineFixtureRecord(t, r, "malformed", "alice")
	executeMemoryFixture(t, db, `UPDATE ai_memory_records SET payload='["unreadable",{"sourceRefs":7}]'::jsonb WHERE id='malformed'`)
	items, err := r.ListRecords(ctx, "user", "alice", time.Now())
	if err != nil || len(items) != 1 || items[0].ID != "malformed" || items[0].OwnerID != "alice" ||
		items[0].Status != "active" || !items[0].Quarantined || items[0].Fact != "" {
		t.Fatalf("malformed row lost authoritative management identity: %+v %v", items, err)
	}
	if err := r.DeleteRecord(ctx, "malformed", time.Now()); err != nil {
		t.Fatal(err)
	}
	record, err := r.GetRecord(ctx, "malformed")
	if err != nil || record.Status != "deleted" || record.DeletedAt == nil ||
		!record.Quarantined || record.SyncStatus != "delete_pending" {
		t.Fatalf("malformed row did not produce a durable tombstone: %+v %v", record, err)
	}
	var preserved bool
	if err := db.Raw(`SELECT payload='["unreadable",{"sourceRefs":7}]'::jsonb FROM ai_memory_records WHERE id='malformed'`).Row().Scan(&preserved); err != nil || !preserved {
		t.Fatalf("original quarantined payload changed: %t %v", preserved, err)
	}
}
