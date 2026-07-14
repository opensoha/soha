package memory

import (
	"testing"
	"time"
)

func TestServiceRequiresExplicitOrCuratedMemoryAndPropagatesDelete(t *testing.T) {
	service, err := NewService(NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	policy := Policy{ID: "user-memory", Version: "v1", OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: 24 * time.Hour, MinimumConfidence: .8, Enabled: true}
	if err := service.PutPolicy(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	record, err := service.PutRecord(t.Context(), Record{ID: "memory-1", OwnerType: "user", OwnerID: "user-1", ScopeHash: "sha256:abc", Fact: "prefers concise output", SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "active" || record.ExpiresAt == nil {
		t.Fatalf("record = %#v", record)
	}
	items, err := service.ListRecords(t.Context(), "user", "user-1")
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %#v, %v", items, err)
	}
	if err := service.DeleteRecord(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	items, err = service.ListRecords(t.Context(), "user", "user-1")
	if err != nil || len(items) != 0 {
		t.Fatalf("items after delete = %#v, %v", items, err)
	}
}

func TestServiceRejectsImplicitAndExpiredMemory(t *testing.T) {
	service, _ := NewService(NewMemoryStore())
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	policy := Policy{ID: "strict", Version: "v1", OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: 2 * time.Hour, MinimumConfidence: .9, ExplicitWriteOnly: true, Enabled: true}
	_, err := service.PutRecord(t.Context(), Record{ID: "memory-1", OwnerType: "user", OwnerID: "user-1", ScopeHash: "sha256:abc", Fact: "fact", SourceType: "curated_extractor", Confidence: 1, PolicyVer: "v1"}, policy)
	if err == nil {
		t.Fatal("expected explicit write policy error")
	}
	expiresAt := now.Add(3 * time.Hour)
	_, err = service.PutRecord(t.Context(), Record{ID: "memory-2", OwnerType: "user", OwnerID: "user-1", ScopeHash: "sha256:abc", Fact: "fact", SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1", ExpiresAt: &expiresAt}, policy)
	if err == nil {
		t.Fatal("expected maximum ttl error")
	}
}
