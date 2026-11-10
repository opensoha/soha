package memory

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	app "github.com/opensoha/soha/internal/application/memory"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMemoryPostgresFixture(t *testing.T) *gorm.DB {
	return newMemoryPostgresFixtureAt(t, true)
}

func newMemoryPostgresFixtureAt(t *testing.T, lifecycle bool) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TASK123_MEMORY_TEST_DSN")
	if dsn == "" {
		t.Skip("requires this run's private PostgreSQL cluster")
	}
	if !strings.Contains(dsn, "host=127.0.0.1") || !strings.Contains(dsn, "dbname=task123_memory_test") {
		t.Fatal("refuse non-test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	schema := "task123_memory_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	scoped, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	scopedSQL, _ := scoped.DB()
	t.Cleanup(func() { _ = scopedSQL.Close() })
	baseline, err := os.ReadFile("../../../migrations/postgres/0021_ai_advanced_intelligence.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(baseline)
	text = text[strings.Index(text, "CREATE TABLE IF NOT EXISTS ai_memory_policies"):strings.Index(text, "CREATE TABLE IF NOT EXISTS ai_knowledge_graph_revisions")]
	if err := scoped.Exec(text).Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../../migrations/postgres/0143_memory_backend_sync.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := scoped.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	// This fixture owns only memory tables; the full upgrade rehearsal separately
	// exercises the run triggers against the complete production migration chain.
	upgrade, err := os.ReadFile("../../../migrations/postgres/0144_runtime_upgrade.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := scoped.Exec(string(upgrade[:strings.Index(string(upgrade), "CREATE FUNCTION")])).Error; err != nil {
		t.Fatal(err)
	}
	if err := scoped.Exec(`
	 CREATE TABLE ai_sessions(id text PRIMARY KEY, deleted_at timestamp, metadata json);
	 CREATE TABLE ai_messages(id text PRIMARY KEY, session_id text, role text, content text);
	 CREATE TABLE ai_agent_runs(id text PRIMARY KEY)
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := scoped.Exec(string(upgrade[strings.Index(string(upgrade), "CREATE FUNCTION"):])).Error; err != nil {
		t.Fatal(err)
	}
	if lifecycle {
		applyMemoryLifecycleMigration(t, scoped)
	}
	return scoped
}

func applyMemoryLifecycleMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	lifecycle, err := os.ReadFile("../../../migrations/postgres/0145_memory_lifecycle.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(lifecycle)).Error; err != nil {
		t.Fatal(err)
	}
}
func TestSyncPostgresDurability(t *testing.T) {
	scoped := newMemoryPostgresFixture(t)
	var err error
	ctx := context.Background()
	r := New(scoped)
	policy := app.Policy{ID: "p", Version: "v1", OwnerID: "a", Mode: "chat", OwnerTypes: []string{"user"}, Enabled: true}
	if err := r.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	record := app.Record{ID: "record", OwnerType: "user", OwnerID: "a", ScopeHash: "sha256:scope", Status: "active", Fact: "first", ExpiresAt: &expiry, SourceRefs: []string{"source"}, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), PolicyID: policy.ID, PolicyVer: policy.Version}
	if err := r.PutRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	t.Run("duplicate_and_owner_scope_fence", func(t *testing.T) {
		current, err := r.GetRecord(ctx, record.ID)
		if err != nil || current.Revision != 1 {
			t.Fatalf("%+v %v", current, err)
		}
		if err := r.PutRecord(ctx, current); err != nil {
			t.Fatal(err)
		}
		var count int64
		scoped.Raw("SELECT count(*) FROM ai_memory_sync_outbox").Scan(&count)
		if count != 1 {
			t.Fatal(count)
		}
		for _, field := range []string{"owner", "scope"} {
			bad := current
			if field == "owner" {
				bad.OwnerID = "b"
			} else {
				bad.ScopeHash = "sha256:other"
			}
			if err := r.PutRecord(ctx, bad); err == nil {
				t.Fatal("cross scope write accepted")
			}
		}
	})
	var first app.SyncJob
	t.Run("restart_recovers_lease_and_stable_operation", func(t *testing.T) {
		first, err = r.ClaimSync(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.MarkSubmitting(ctx, first); err != nil {
			t.Fatal(err)
		}
		scoped.Exec("UPDATE ai_memory_sync_outbox SET lease_until=NOW()-INTERVAL '1 second'")
		recovered, err := New(scoped).ClaimSync(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if recovered.ID != first.ID || !recovered.Attempted || recovered.LeaseToken == first.LeaseToken {
			t.Fatal("lost submission identity")
		}
		if err := r.FinishSync(ctx, first, "done"); err == nil {
			t.Fatal("stale lease completed")
		}
		first = recovered
	})
	t.Run("delete_waits_for_late_retain", func(t *testing.T) {
		if err := r.DeleteRecord(ctx, record.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := r.PutRecord(ctx, record); err == nil {
			t.Fatal("tombstone resurrected")
		}
		if _, err := r.ClaimSync(ctx); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("delete raced pending retain: %v", err)
		}
		if err := r.FinishSync(ctx, first, "done"); err != nil {
			t.Fatal(err)
		}
		cleanup, err := New(scoped).ClaimSync(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if cleanup.Kind != "delete" || cleanup.DocumentID != first.DocumentID || cleanup.RetainOperationID != first.ID {
			t.Fatal(cleanup)
		}
		if err := r.FinishSync(ctx, cleanup, "retry"); err != nil {
			t.Fatal(err)
		}
		scoped.Exec("UPDATE ai_memory_sync_outbox SET next_attempt_at=NOW() WHERE kind='delete'")
		retry, err := New(scoped).ClaimSync(ctx)
		if err != nil || retry.DocumentID != cleanup.DocumentID {
			t.Fatalf("%+v %v", retry, err)
		}
		if err := r.FinishSync(ctx, retry, "done"); err != nil {
			t.Fatal(err)
		}
		got, err := r.ResolveDocument(ctx, retry.DocumentID)
		if !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("correction_and_expiry", func(t *testing.T) {
		other := record
		other.ID = "corrected"
		if err := r.PutRecord(ctx, other); err != nil {
			t.Fatal(err)
		}
		first, err := r.ClaimSync(ctx)
		if err != nil {
			t.Fatal(err)
		}
		other.Fact = "corrected"
		if err := r.PutRecord(ctx, other); err != nil {
			t.Fatal(err)
		}
		current, err := r.GetRecord(ctx, other.ID)
		if err != nil || current.Revision != 2 {
			t.Fatalf("%+v %v", current, err)
		}
		if _, err := r.ResolveDocument(ctx, first.DocumentID); !errors.Is(err, app.ErrNotFound) {
			t.Fatal("obsolete document mapped to new revision")
		}
		if err := r.FinishSync(ctx, first, "done"); err != nil {
			t.Fatal(err)
		}
		scoped.Exec("UPDATE ai_memory_records SET expires_at=NOW()-INTERVAL '1 second' WHERE id=?", other.ID)
		if err := r.queueExpired(ctx); err != nil {
			t.Fatal(err)
		}
		current, err = r.GetRecord(ctx, other.ID)
		if err != nil || current.Status != "expired" || current.Revision != 3 {
			t.Fatalf("%+v %v", current, err)
		}
		if err := r.EnsureBank(ctx, other, "bank-one"); err != nil {
			t.Fatal(err)
		}
		if err := r.EnsureBank(ctx, other, "bank-two"); err == nil {
			t.Fatal("namespace drift accepted")
		}
	})
	t.Run("transaction_rollback", func(t *testing.T) {
		scoped.Exec("ALTER TABLE ai_memory_sync_outbox ADD CONSTRAINT reject_test_record CHECK (record_id<>'rollback')")
		other := record
		other.ID = "rollback"
		if err := r.PutRecord(ctx, other); err == nil {
			t.Fatal("expected outbox failure")
		}
		if _, err := r.GetRecord(ctx, "rollback"); !errors.Is(err, app.ErrNotFound) {
			t.Fatal("non-atomic CRUD", err)
		}
	})
	t.Run("user_sync_state_projection", func(t *testing.T) {
		item := record
		item.ID = "projection"
		if err := r.PutRecord(ctx, item); err != nil {
			t.Fatal(err)
		}
		requireState := func(want string) {
			t.Helper()
			got, err := r.GetRecord(ctx, item.ID)
			if err != nil || got.SyncStatus != want {
				t.Fatalf("want %s got %+v %v", want, got, err)
			}
		}
		requireState("pending")
		if err := scoped.Exec("UPDATE ai_memory_sync_outbox SET attempted=true,state='done' WHERE record_id=?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
		requireState("synced")
		if err := r.DeleteRecord(ctx, item.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
		requireState("delete_pending")
		scoped.Exec("UPDATE ai_memory_sync_outbox SET state='retry' WHERE record_id=? AND kind='delete'", item.ID)
		requireState("error")
		if err := scoped.Exec("UPDATE ai_memory_sync_outbox SET state='done' WHERE record_id=? AND kind='delete'", item.ID).Error; err == nil {
			t.Fatal("old worker acknowledged deletion without operation cleanup")
		}
		if err := scoped.Exec("UPDATE ai_memory_sync_outbox SET state='done',operation_purged=true WHERE record_id=? AND kind='delete'", item.ID).Error; err != nil {
			t.Fatal(err)
		}
		requireState("deleted")
		active, err := r.ListRecordStates(ctx, "user", "a", time.Now(), false)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range active {
			if r.ID == item.ID {
				t.Fatal("tombstone still active")
			}
		}
		all, err := r.ListRecordStates(ctx, "user", "a", time.Now(), true)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, r := range all {
			if r.ID == item.ID && r.SyncStatus == "deleted" {
				found = true
			}
		}
		if !found {
			t.Fatal("missing own deleted status")
		}
	})

}
