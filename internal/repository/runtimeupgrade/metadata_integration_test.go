package runtimeupgrade

import (
	"strings"
	"testing"
)

func TestRuntimeUpgradeMetadataPostgres(t *testing.T) {
	db := upgradeFixture(t)
	r := New(db)
	ctx := t.Context()
	original := `{"agentProviderId":"internal","scope":{"cluster":"keep"},"toolset":{"tools":["keep"]},"modelPreferences":{"model":"keep"}}`
	for id, metadata := range map[string]string{"null": "null", "object": original, "edited": original, "array": "[]"} {
		execTest(t, db, `INSERT INTO ai_sessions(id,title,created_by,metadata) VALUES(?,?,'actor',?::json)`, id, id, metadata)
	}
	t.Run("array_requires_manual_review_without_partial_writes", func(t *testing.T) {
		p, err := r.DryRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, i := range p.Items {
			if i.Entity == "session" && i.ID == "array" {
				found = i.Action == "manual_review" && i.Reason == "invalid_session_metadata"
			}
		}
		if !found {
			t.Fatal("invalid metadata not identified")
		}
		if err := r.Apply(ctx, ApplyOptions{Digest: p.Digest, BackupSHA256: strings.Repeat("a", 64)}); err == nil {
			t.Fatal("array accepted")
		}
		var unchanged bool
		if err := db.Raw(`SELECT metadata::jsonb='null'::jsonb FROM ai_sessions WHERE id='null'`).Row().Scan(&unchanged); err != nil || !unchanged {
			t.Fatalf("partial migration: %v", err)
		}
		var count int64
		if err := db.Raw(`SELECT count(*) FROM ai_runtime_upgrade_checkpoints`).Row().Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial checkpoint: %d %v", count, err)
		}
	})
	// An operator must repair invalid data explicitly. Removing only this test
	// fixture lets the independently valid null/object cases continue.
	execTest(t, db, `DELETE FROM ai_sessions WHERE id='array'`)
	p, err := r.DryRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("null_and_object_apply_preserve_preferences", func(t *testing.T) {
		if err := r.Apply(ctx, ApplyOptions{Digest: p.Digest, BackupSHA256: strings.Repeat("a", 64)}); err != nil {
			t.Fatal(err)
		}
		var canonical, kept bool
		if err := db.Raw(`SELECT metadata::jsonb='{"agentProviderId":"dsh"}'::jsonb FROM ai_sessions WHERE id='null'`).Row().Scan(&canonical); err != nil || !canonical {
			t.Fatalf("null not canonicalized: %v", err)
		}
		if err := db.Raw(`SELECT metadata::jsonb=jsonb_set(?::jsonb,'{agentProviderId}','"dsh"') FROM ai_sessions WHERE id='object'`, original).Row().Scan(&kept); err != nil || !kept {
			t.Fatalf("preferences lost: %v", err)
		}
	})
	t.Run("rollback_restores_exact_null_and_keeps_user_edits", func(t *testing.T) {
		execTest(t, db, `UPDATE ai_sessions SET metadata=jsonb_set(metadata::jsonb,'{userNote}','"keep-edit"')::json WHERE id='edited'`)
		if err := r.Rollback(ctx, p.Digest); err != nil {
			t.Fatal(err)
		}
		var text string
		if err := db.Raw(`SELECT metadata::text FROM ai_sessions WHERE id='null'`).Row().Scan(&text); err != nil || text != "null" {
			t.Fatalf("null not restored exactly: %s %v", text, err)
		}
		var kept bool
		if err := db.Raw(`SELECT metadata::jsonb=?::jsonb FROM ai_sessions WHERE id='object'`, original).Row().Scan(&kept); err != nil || !kept {
			t.Fatalf("original preferences not restored: %v", err)
		}
		if err := db.Raw(`SELECT metadata->>'userNote'='keep-edit' AND metadata->>'agentProviderId'='dsh' FROM ai_sessions WHERE id='edited'`).Row().Scan(&kept); err != nil || !kept {
			t.Fatalf("user edit overwritten: %v", err)
		}
	})
}
