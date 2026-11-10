package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	app "github.com/opensoha/soha/internal/application/memory"
)

type partialRecallBackend struct{ candidates []app.Candidate }

func (*partialRecallBackend) Retain(context.Context, string, app.SyncJob) error { return nil }
func (*partialRecallBackend) Operation(context.Context, string, string) (string, error) {
	return "completed", nil
}
func (*partialRecallBackend) DeleteDocument(context.Context, string, string, string) error {
	return nil
}
func (b *partialRecallBackend) Recall(context.Context, string, string) ([]app.Candidate, error) {
	return b.candidates, nil
}

type durableRecallSources struct{ calls int }

func (s *durableRecallSources) ValidateSource(context.Context, app.Record) error {
	s.calls++
	return nil
}

func TestResolveDocumentDurabilityPostgres(t *testing.T) {
	db := newMemoryPostgresFixture(t)
	r := New(db)
	ctx := t.Context()
	policy := app.Policy{ID: "p", Version: "v1", OwnerID: "alice", Mode: "chat", Enabled: true, OwnerTypes: []string{"user"}}
	if err := r.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, state string
		attempted   bool
		changedRev  bool
		quarantined bool
		visible     bool
	}{
		{name: "pending_partial_facts_denied", state: "pending", attempted: true},
		{name: "retry_partial_facts_denied", state: "retry", attempted: true},
		{name: "unknown_partial_facts_denied", state: "unknown", attempted: true},
		{name: "failed_partial_facts_denied", state: "failed", attempted: true},
		{name: "unattempted_done_denied", state: "done"},
		{name: "confirmed_done_resolves_and_recalls", state: "done", attempted: true, visible: true},
		{name: "confirmed_old_revision_denied", state: "done", attempted: true, changedRev: true},
		{name: "confirmed_quarantined_denied", state: "done", attempted: true, quarantined: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			expiry := now.Add(time.Hour)
			record := app.Record{ID: tc.name, OwnerType: "user", OwnerID: "alice", ScopeHash: app.Scope("alice", "chat"), Status: "active", Fact: "source fact", SourceType: "explicit_user", SourceRefs: []string{"source"}, PolicyID: "p", PolicyVer: "v1", ExpiresAt: &expiry, CreatedAt: now, ValidFrom: now}
			if err := r.PutRecord(ctx, record); err != nil {
				t.Fatal(err)
			}
			var document string
			if err := db.Raw(`UPDATE ai_memory_sync_outbox SET state=?,attempted=?,next_attempt_at=now()+interval '1 hour' WHERE record_id=? RETURNING document_id`, tc.state, tc.attempted, record.ID).Row().Scan(&document); err != nil {
				t.Fatal(err)
			}
			if tc.changedRev {
				if err := db.Exec(`UPDATE ai_memory_records SET revision=revision+1 WHERE id=?`, record.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			if tc.quarantined {
				quarantineRecord(t, r, record.ID)
			}
			resolved, err := r.ResolveDocument(ctx, document)
			if tc.visible {
				if err != nil || resolved.ID != record.ID || resolved.Revision != 1 {
					t.Fatalf("confirmed receipt not resolved: %+v %v", resolved, err)
				}
			} else if !errors.Is(err, app.ErrNotFound) {
				t.Fatalf("unconfirmed/stale document resolved: %+v %v", resolved, err)
			}
			// The vendor supplies partial facts even for nonterminal/failed jobs.
			// Exercise the real application recall with this PostgreSQL resolver.
			sources := &durableRecallSources{}
			backend := &partialRecallBackend{candidates: []app.Candidate{{DocumentID: document, Revision: 1}}}
			service, err := app.NewBackendService(r, r, backend, sources, "isolated-durability")
			if err != nil {
				t.Fatal(err)
			}
			items, err := service.Recall(ctx, "user", "alice", record.ScopeHash, "query")
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if tc.visible {
				want = 1
			}
			if len(items) != want || sources.calls != want {
				t.Fatalf("partial fact crossed durable boundary: items=%d sourceChecks=%d want=%d", len(items), sources.calls, want)
			}
		})
	}
	t.Run("unknown_reconciliation_and_delete_remain_available", func(t *testing.T) {
		id := "unknown_partial_facts_denied"
		if err := db.Exec(`UPDATE ai_memory_sync_outbox SET next_attempt_at=now() WHERE record_id=?`, id).Error; err != nil {
			t.Fatal(err)
		}
		job, err := r.ClaimSync(ctx)
		if err != nil || !job.Attempted || job.Record.ID != id {
			t.Fatalf("read filter blocked operation reconciliation: %+v %v", job, err)
		}
		if err := r.FinishSync(ctx, job, "done"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.ResolveDocument(ctx, job.DocumentID); err != nil {
			t.Fatalf("successful reconciliation not visible: %v", err)
		}
		if err := r.DeleteRecord(ctx, id, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := r.ResolveDocument(ctx, job.DocumentID); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("deleted revision resolved: %v", err)
		}
		cleanup, err := r.ClaimSync(ctx)
		if err != nil || cleanup.Kind != "delete" || cleanup.Record.ID != id {
			t.Fatalf("delete cleanup blocked: %+v %v", cleanup, err)
		}
	})
}
