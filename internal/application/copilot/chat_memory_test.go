package copilot

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestExplicitChatMemoryRechecksOwnerPolicyCorrectionsAndDeletion(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermAIMemoryView}})
	store := appmemory.NewMemoryStore()
	memory, _ := appmemory.NewService(store)
	service.SetChatMemoryReader(memory)
	ctx := context.Background()
	policy := appmemory.Policy{ID: "personal", Version: "v1", OwnerTypes: []string{"user"}, DefaultTTL: 24 * time.Hour, MaximumTTL: 24 * time.Hour, ExplicitWriteOnly: true, Enabled: true}
	if err := memory.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	record, err := memory.PutRecord(ctx, appmemory.Record{ID: "fact", OwnerType: "user", OwnerID: "owner", ScopeHash: "sha256:test", Fact: "original", SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	owner := domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}
	metadata := domaincopilot.SessionMetadata{RequestContext: &domaincopilot.ContextSelection{MemoryIDs: []string{"fact"}}}
	envelope, err := service.buildChatContext(ctx, owner, "session", metadata, "recall")
	if err != nil || len(envelope.Evidence) != 1 || envelope.Evidence[0].Content != "original" {
		t.Fatalf("memory missing: %+v %v", envelope, err)
	}
	if _, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "other", Roles: []string{"reader"}}, "session", metadata, "recall"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("other user's memory read: %v", err)
	}
	other := record
	other.OwnerID = "other"
	if _, err := memory.PutRecord(ctx, other, policy); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("memory owner was mutable: %v", err)
	}
	record.Fact = "corrected"
	if _, err := memory.PutRecord(ctx, record, policy); err != nil {
		t.Fatal(err)
	}
	envelope, err = service.buildChatContext(ctx, owner, "session", metadata, "recall")
	if err != nil || envelope.Evidence[0].Content != "corrected" {
		t.Fatal("correction was not used")
	}
	policy.Enabled = false
	if err := memory.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := service.buildChatContext(ctx, owner, "session", metadata, "recall"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("disabled policy was used: %v", err)
	}
	if err := memory.DeleteRecord(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.buildChatContext(ctx, owner, "session", metadata, "recall"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted memory was reused: %v", err)
	}
	if envelope, err := service.buildChatContext(ctx, owner, "session", domaincopilot.SessionMetadata{}, "recall"); err != nil || envelope != nil {
		t.Fatal("memory was automatically injected")
	}
}
