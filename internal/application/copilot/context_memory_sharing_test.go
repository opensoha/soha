package copilot

import (
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestExplicitMemorySelectionMetadataIsServerOwned(t *testing.T) {
	selected := []string{"selected"}
	metadata := chatUserMetadata(map[string]any{"_sohaExplicitMemoryIds": []string{"forged"}}, &domaincopilot.ContextSelection{MemoryIDs: selected})
	selected[0] = "changed"
	if got := agentPrincipalStringList(metadata["_sohaExplicitMemoryIds"]); len(got) != 1 || got[0] != "selected" {
		t.Fatalf("selection receipt aliased: %v", got)
	}
	metadata = chatUserMetadata(metadata, nil)
	if _, exists := metadata["_sohaExplicitMemoryIds"]; exists {
		t.Fatal("caller receipt survived absent selection")
	}
}
func TestCrossModeMemoryHistoryWithoutSharingReceiptDoesNotBlockNewQuestion(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermAIMemoryView, appaccess.PermObserveAIChatUse}})
	principal := domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}
	repo.session = domaincopilot.Session{ID: "session", CreatedBy: "owner"}
	memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
	service.SetChatMemoryReader(memory)
	policy := appmemory.Policy{ID: "heya", Version: "1", OwnerID: "owner", Mode: "heya", OwnerTypes: []string{"user"}, Enabled: true, ExplicitWriteOnly: true, DefaultTTL: time.Hour, MaximumTTL: time.Hour}
	if err := memory.PutPolicy(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.PutRecord(t.Context(), appmemory.Record{ID: "private", OwnerType: "user", OwnerID: "owner", ScopeHash: appmemory.Scope("owner", "heya"), Fact: "PRIVATE_MEMORY_CANARY", SourceType: "explicit_user", Confidence: 1, PolicyVer: "1"}, policy); err != nil {
		t.Fatal(err)
	}
	envelope, err := service.buildChatContext(t.Context(), principal, "session", domaincopilot.SessionMetadata{RequestContext: &domaincopilot.ContextSelection{MemoryIDs: []string{"private"}}}, "source")
	if err != nil {
		t.Fatal(err)
	}
	history := []domaincopilot.Message{{ID: "old-derived", SessionID: "session", Role: "assistant", Content: "PRIVATE_MEMORY_CANARY", Metadata: map[string]any{"contextSnapshot": contextSnapshot(*envelope)}}}
	messages, _, lineage, err := service.groundedProviderContext(t.Context(), principal, repo.session, domaincopilot.SessionMetadata{}, history, domaincopilot.Message{ID: "new-question", SessionID: "session", Role: "user", Content: "hello"}, "en-US")
	if err != nil {
		t.Fatalf("ordinary chat blocked: %v", err)
	}
	for _, message := range messages {
		if strings.Contains(message.Content, "PRIVATE_MEMORY_CANARY") {
			t.Fatal("cross-mode memory history leaked")
		}
	}
	if len(lineage.ExcludedMessages) != 1 || lineage.ExcludedMessages[0].MessageID != "old-derived" {
		t.Fatalf("missing withholding receipt: %+v", lineage)
	}
}
