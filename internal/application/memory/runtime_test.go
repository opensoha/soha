package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPrivateScopeAndIndependentConsent(t *testing.T) {
	if Scope("a", "chat") == Scope("a", "heya") || Scope("a", "chat") == Scope("b", "chat") {
		t.Fatal("private banks collide")
	}
	no, yes := false, true
	p := Policy{Enabled: true, ReadEnabled: &no, WriteEnabled: &yes}
	if ReadAllowed(p) || !WriteAllowed(p) {
		t.Fatal("read/write consent coupled")
	}
	s, _ := NewService(NewMemoryStore())
	if _, err := s.Recall(t.Context(), "a", "chat", "hello"); err == nil {
		t.Fatal("disabled engine silently succeeded")
	}
}
func TestWriteDisabledRejectsAndDeletionRemainsLocal(t *testing.T) {
	s, _ := NewService(NewMemoryStore())
	no := false
	p := Policy{ID: "p", Version: "1", OwnerID: "a", Mode: "heya", Enabled: true, OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: time.Hour, ExplicitWriteOnly: true}
	s.PutPolicy(t.Context(), p)
	r := Record{ID: "r", OwnerType: "user", OwnerID: "a", ScopeHash: Scope("a", "heya"), Fact: "fact", SourceType: "explicit_user", PolicyVer: "1", Confidence: 1}
	p.WriteEnabled = &no
	if _, err := s.PutRecord(t.Context(), r, p); err == nil {
		t.Fatal("disabled writes accepted")
	}
	p.WriteEnabled = nil
	r.OwnerID = "b"
	if _, err := s.PutRecord(t.Context(), r, p); err == nil {
		t.Fatal("foreign policy accepted")
	}
	r.OwnerID = "a"
	got, err := s.PutRecord(t.Context(), r, p)
	if err != nil || got.SyncStatus != "disabled" {
		t.Fatalf("%+v %v", got, err)
	}
	if err = s.DeleteRecord(t.Context(), r.ID); err != nil {
		t.Fatal(err)
	}
	active, _ := s.ListRecords(t.Context(), "user", "a")
	all, _ := s.ListRecordStates(t.Context(), "user", "a", true)
	if len(active) != 0 || len(all) != 1 || all[0].Status != "deleted" {
		t.Fatal("tombstone not visible")
	}
}

type idleSync struct {
	testSync
	called chan struct{}
}

func (s *idleSync) ClaimSync(context.Context) (SyncJob, error) {
	select {
	case s.called <- struct{}{}:
	default:
	}
	return SyncJob{}, ErrNotFound
}
func TestSyncWorkerStopsAndReportsFailure(t *testing.T) {
	ss := &idleSync{called: make(chan struct{}, 1)}
	b, _ := NewBackendService(NewMemoryStore(), ss, &testBackend{}, &testSources{}, "i")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); b.RunSync(ctx, func(err error) { t.Errorf("unexpected %v", err) }) }()
	select {
	case <-ss.called:
	case <-time.After(time.Second):
		t.Fatal("worker never polled")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not join")
	}
	ss2 := &testSync{j: SyncJob{Kind: "delete"}}
	b, _ = NewBackendService(NewMemoryStore(), ss2, &testBackend{err: errors.New("offline")}, &testSources{}, "i")
	worked, err := b.SyncOne(t.Context())
	if !worked || err == nil || ss2.state != "retry" {
		t.Fatal("failed delete reported as complete")
	}
}

func TestPersonalPolicyBindingAndAutomaticConsent(t *testing.T) {
	s, _ := NewService(NewMemoryStore())
	p := Policy{ID: "client-hint", Enabled: true, ExplicitWriteOnly: true, DefaultTTL: time.Hour, MaximumTTL: time.Hour}
	got, err := s.SavePersonalPolicy(t.Context(), "a", p)
	if err != nil || got.ID != PrivatePolicyID("a", "chat") || got.OwnerID != "a" || got.Mode != "chat" || got.Version != "v1" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = s.SavePersonalPolicy(t.Context(), "b", got); err == nil {
		t.Fatal("foreign policy overwritten")
	}
	p.ExplicitWriteOnly = false
	if _, err = s.SavePersonalPolicy(t.Context(), "a", p); err == nil {
		t.Fatal("Chat auto consent accepted")
	}
	p.Mode = "heya"
	if _, err = s.SavePersonalPolicy(t.Context(), "a", p); err != nil {
		t.Fatal(err)
	}
	global := Policy{ID: "legacy", Version: "v1", Enabled: true, ExplicitWriteOnly: true, OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: time.Hour}
	s.PutPolicy(t.Context(), global)
	saved, err := s.SavePersonalPolicy(t.Context(), "a", global)
	if err != nil || saved.OwnerID != "" || saved.ID != "legacy" {
		t.Fatalf("%+v %v", saved, err)
	}
	policies, err := s.ListPersonalPolicies(t.Context(), "b")
	if err != nil || len(policies) != 1 || policies[0].ID != "legacy" {
		t.Fatalf("foreign policy disclosed: %+v %v", policies, err)
	}
}

func TestLegacyWriteCannotBypassExplicitPrivateConsent(t *testing.T) {
	s, _ := NewService(NewMemoryStore())
	legacy := Policy{ID: "legacy", Version: "v1", Enabled: true, OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: time.Hour, ExplicitWriteOnly: true}
	if err := s.PutPolicy(t.Context(), legacy); err != nil {
		t.Fatal(err)
	}
	r := Record{ID: "legacy", OwnerType: "user", OwnerID: "alice", ScopeHash: Scope("alice", "chat"), Fact: "fact", SourceType: "explicit_user", PolicyVer: legacy.Version}
	if _, err := s.PutRecord(t.Context(), r, legacy); err != nil {
		t.Fatalf("legacy fallback unavailable before an explicit preference: %v", err)
	}
	no := false
	consent := legacy
	consent.ID, consent.Mode, consent.WriteEnabled = "", "chat", &no
	if _, err := s.SavePersonalPolicy(t.Context(), r.OwnerID, consent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutRecord(t.Context(), r, legacy); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("legacy global policy bypassed explicit private write opt-out: %v", err)
	}
}
