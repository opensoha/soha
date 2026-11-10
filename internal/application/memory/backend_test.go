package memory

import (
	"context"
	"errors"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

type testBackend struct {
	c       []Candidate
	err     error
	status  string
	retains int
	deletes int
}

func (b *testBackend) Retain(context.Context, string, SyncJob) error { b.retains++; return nil }
func (b *testBackend) Operation(context.Context, string, string) (string, error) {
	return b.status, b.err
}
func (b *testBackend) DeleteDocument(context.Context, string, string, string) error {
	b.deletes++
	return b.err
}
func (b *testBackend) Recall(context.Context, string, string) ([]Candidate, error) { return b.c, b.err }

type testSync struct {
	r             Record
	j             SyncJob
	state         string
	marked        bool
	invalidated   bool
	sourceRevoked bool
}

func (s *testSync) EnsureBank(context.Context, Record, string) error { return nil }
func (s *testSync) ClaimSync(context.Context) (SyncJob, error)       { return s.j, nil }
func (s *testSync) MarkSubmitting(context.Context, SyncJob) error    { s.marked = true; return nil }
func (s *testSync) FinishSync(_ context.Context, _ SyncJob, state string) error {
	s.state = state
	return nil
}
func (s *testSync) ResolveDocument(context.Context, string) (Record, error) { return s.r, nil }

func (s *testSync) InvalidateSync(_ context.Context, _ Record, sourceRevoked bool) error {
	s.invalidated = true
	s.sourceRevoked = sourceRevoked
	return nil
}

type testSources struct{ err error }

func (s *testSources) ValidateSource(context.Context, Record) error { return s.err }
func TestBackendRecallRevalidates(t *testing.T) {
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)
	base := Record{ID: "r", Revision: 2, OwnerType: "user", OwnerID: "a", ScopeHash: "sha256:scope", Status: "active", SourceRefs: []string{"message/1"}, PolicyID: "p", PolicyVer: "1", Fact: "Soha confirmed", ExpiresAt: &expires}
	for _, name := range []string{"valid", "owner", "scope", "revision", "deleted", "expired", "source", "source-revoked", "policy-disabled", "write-disabled", "quarantined"} {
		t.Run(name, func(t *testing.T) {
			r := cloneRecord(base)
			p := Policy{ID: "p", Version: "1", OwnerID: "a", Enabled: true, OwnerTypes: []string{"user"}}
			v := &testSources{}
			switch name {
			case "owner":
				r.OwnerID = "b"
			case "scope":
				r.ScopeHash = "sha256:other"
			case "revision":
				r.Revision = 3
			case "deleted":
				r.Status = "deleted"
			case "expired":
				past := time.Now().Add(-time.Hour)
				r.ExpiresAt = &past
			case "source":
				r.SourceRefs = nil
			case "source-revoked":
				v.err = apperrors.ErrAccessDenied
			case "policy-disabled":
				p.Enabled = false
			case "write-disabled":
				disabled := false
				p.WriteEnabled = &disabled
			case "quarantined":
				r.Quarantined = true
			}
			store := NewMemoryStore()
			store.PutPolicy(ctx, p)
			ss := &testSync{r: r}
			b := &testBackend{c: []Candidate{{DocumentID: "doc", Revision: 2}}}
			s, err := NewBackendService(store, ss, b, v, "instance")
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.Recall(ctx, "user", "a", "sha256:scope", "q")
			if err != nil {
				t.Fatal(err)
			}
			if name == "valid" || name == "write-disabled" {
				if len(got) != 1 || got[0].Fact != "Soha confirmed" {
					t.Fatal(got)
				}
			} else if len(got) != 0 {
				t.Fatal("untrusted result", got)
			}
			if want := name == "source-revoked" || name == "policy-disabled"; ss.invalidated != want || ss.sourceRevoked != (name == "source-revoked") {
				t.Fatalf("invalidated=%t sourceRevoked=%t", ss.invalidated, ss.sourceRevoked)
			}
		})
	}
}
func TestBackendSyncSubmissionRecovery(t *testing.T) {
	for _, name := range []string{"fresh", "lost-ack", "completed", "running", "write-disabled", "upstream-failure", "delete-failure"} {
		t.Run(name, func(t *testing.T) {
			b := &testBackend{err: ErrBackendNotFound}
			ss := &testSync{j: SyncJob{Kind: "retain"}}
			switch name {
			case "lost-ack":
				ss.j.Attempted = true
			case "completed":
				b.err = nil
				b.status = "completed"
			case "running":
				b.err = nil
				b.status = "processing"
			case "upstream-failure":
				b.err = errors.New("401")
			case "delete-failure":
				ss.j.Kind = "delete"
				b.err = errors.New("503")
			}
			store := NewMemoryStore()
			expiry := time.Now().Add(time.Hour)
			ss.j.Record = Record{ID: "record", Status: "active", ExpiresAt: &expiry, SourceRefs: []string{"source"}, PolicyID: "p", PolicyVer: "1", OwnerType: "user", OwnerID: "a"}
			ss.j.Revision = 0
			store.PutRecord(context.Background(), ss.j.Record)
			policy := Policy{ID: "p", Version: "1", OwnerID: "a", Enabled: true, OwnerTypes: []string{"user"}}
			if name == "write-disabled" {
				disabled := false
				policy.WriteEnabled = &disabled
			}
			store.PutPolicy(context.Background(), policy)
			s, _ := NewBackendService(store, ss, b, &testSources{}, "instance")
			_, err := s.SyncOne(context.Background())
			if name == "fresh" {
				if b.retains != 1 || !ss.marked {
					t.Fatal("did not persist submission")
				}
			} else if b.retains != 0 {
				t.Fatal("blind retain retry")
			}
			if name == "lost-ack" && !errors.Is(err, ErrSyncUnknown) {
				t.Fatal(err)
			}
			wantState := "retry"
			switch name {
			case "completed", "write-disabled":
				wantState = "done"
			case "lost-ack":
				wantState = "unknown"
			case "fresh", "running":
				wantState = "pending"
			}
			if ss.state != wantState || (name == "write-disabled" && (ss.invalidated || ss.marked)) {
				t.Fatalf("want state %s, got %+v", wantState, ss)
			}
		})
	}
	var b *testBackend
	if _, err := NewBackendService(NewMemoryStore(), &testSync{}, b, &testSources{}, "instance"); err == nil {
		t.Fatal("typed nil backend accepted")
	}
	s, _ := NewBackendService(NewMemoryStore(), &testSync{}, &testBackend{}, &testSources{}, "one")
	if s.bank(Record{OwnerID: "a", ScopeHash: "sha256:x"}) == s.bank(Record{OwnerID: "b", ScopeHash: "sha256:x"}) {
		t.Fatal("bank collision")
	}
}

func TestSourceInfrastructureFailureRemainsRetryable(t *testing.T) {
	ctx := t.Context()
	now := time.Now()
	expiry := now.Add(time.Hour)
	r := Record{ID: "r", Revision: 1, OwnerType: "user", OwnerID: "a", Status: "active",
		PolicyID: "p", PolicyVer: "1", SourceRefs: []string{"source"}, ExpiresAt: &expiry}
	store := NewMemoryStore()
	if err := store.PutRecord(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := store.PutPolicy(ctx, Policy{ID: "p", Version: "1", OwnerID: "a", Enabled: true, OwnerTypes: []string{"user"}}); err != nil {
		t.Fatal(err)
	}
	outbox := &testSync{j: SyncJob{Record: r, Revision: 1, Kind: "retain"}}
	backend := &testBackend{}
	dbErr := errors.New("temporary source database failure")
	source := &testSources{err: dbErr}
	service, err := NewBackendService(store, outbox, backend, source, "instance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SyncOne(ctx); !errors.Is(err, dbErr) || outbox.state != "retry" || outbox.invalidated || outbox.marked {
		t.Fatalf("temporary failure lost retry: state=%s invalidated=%t marked=%t error=%v", outbox.state, outbox.invalidated, outbox.marked, err)
	}

	outbox.j.Attempted, outbox.j.Revalidate = true, true
	source.err = apperrors.ErrAccessDenied
	if _, err := service.SyncOne(ctx); err != nil || !outbox.invalidated || !outbox.sourceRevoked || backend.retains != 0 {
		t.Fatalf("synced revision was not selectively invalidated: %+v %v", outbox, err)
	}
}
