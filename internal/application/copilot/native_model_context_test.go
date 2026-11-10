package copilot

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func TestNativeModelContextRecordsExactGatewayInputWithoutBodies(t *testing.T) {
	_, loop, _, _ := nativeLoopFixture(t)
	cp := initialNativeCheckpoint(loop.lease.Run)
	request := appaigateway.WorkbenchRelayRequest{AgentRunID: "run", PublicModel: "model-one", RouteID: "route-one", Messages: []appaigateway.WorkbenchRelayMessage{{Role: "user", Content: "private input text"}}, Tools: []appaigateway.WorkbenchTool{{Name: "lookup", Parameters: map[string]any{"type": "object"}}}}
	if err := reserveNativeModelBudget(&cp, &request); err != nil {
		t.Fatal(err)
	}
	cp.Usage.Steps++
	if err := recordNativeModelContext(&cp, request, map[string]any{"contentHash": "source-one"}); err != nil {
		t.Fatal(err)
	}
	first, err := json.Marshal(cp.ModelContext)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(first), "private input text") || cp.ModelContext.RequestHashBasis != "gateway_input" || cp.ModelContext.ReservedInputTokens+int64(request.MaxOutputTokens) != cp.ModelReservation {
		t.Fatal("context revision exposed body or lost reservation provenance")
	}
	if len(cp.ModelContext.Messages) != 1 || cp.ModelContext.Messages[0].Role != "user" || len(cp.ModelContext.Messages[0].Hash) != 64 {
		t.Fatal("actual message fingerprints are missing")
	}
	firstHash, firstID := cp.ModelContext.RequestHash, cp.ModelContext.ID
	request.PublicModel, request.Messages[0].Content = "model-two", "changed input"
	if err := reserveNativeModelBudget(&cp, &request); err != nil {
		t.Fatal(err)
	}
	cp.Usage.Steps++
	if err := recordNativeModelContext(&cp, request, map[string]any{"contentHash": "source-two"}); err != nil {
		t.Fatal(err)
	}
	if cp.ModelContext.RequestHash == firstHash || cp.ModelContext.ID == firstID || !strings.Contains(string(first), "source-one") || strings.Contains(string(first), "source-two") {
		t.Fatal("later request overwrote its predecessor or reused identity")
	}
}

func TestContextLineageRechecksSummarySources(t *testing.T) {
	citation := domainknowledge.Citation{ID: "citation", KnowledgeBaseID: "base", DocumentID: "document", ChunkID: "chunk", ContentHash: "hash", DocumentVersionID: "version"}
	store := &branchReferenceStore{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}, messageMetadata: map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{citation}}}}
	validator := &nativeCitationFixture{}
	service := &Service{sessions: store, messages: store, contextBuilder: NewContextBuilder(validator, nil)}
	metadata := domaincopilot.SessionMetadata{Summary: "protected summary", PinnedContext: map[string]any{"summarySourceMessageIds": []string{"old"}}}
	principal := domainidentity.Principal{UserID: "owner"}
	for _, denied := range []bool{false, true} {
		validator.denied = denied
		messages, _, lineage, err := service.groundedProviderContext(t.Context(), principal, domaincopilot.Session{ID: "source"}, metadata, nil, domaincopilot.Message{ID: "current", Content: "continue"}, "en-US")
		if err != nil || lineage.Summary == nil || lineage.Summary.Included == denied || !reflect.DeepEqual(lineage.Summary.SourceMessageIDs, []string{"old"}) {
			t.Fatalf("summary selection after denied=%t: %+v %v", denied, lineage.Summary, err)
		}
		if denied && (lineage.Summary.Reason != "session.summary:source_unavailable" || strings.Contains(messages[0].Content, "protected summary")) {
			t.Fatal("revoked summary was used or exclusion reason was lost")
		}
		encoded, err := json.Marshal(lineage)
		if err != nil || strings.Contains(string(encoded), "protected summary") {
			t.Fatal("summary lineage copied its body")
		}
	}
}

func TestContextKeepsHistoricalCitationForDerivedAnswer(t *testing.T) {
	citation := domainknowledge.Citation{ID: "citation", KnowledgeBaseID: "base", DocumentID: "document", ChunkID: "chunk", ContentHash: "hash", DocumentVersionID: "version"}
	validator := &nativeCitationFixture{}
	service := &Service{contextBuilder: NewContextBuilder(validator, nil)}
	principal := domainidentity.Principal{UserID: "owner"}
	original := domaincopilot.Message{ID: "original", Role: "assistant", Content: "protected historical answer", Metadata: map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{citation}}}}
	messages, snapshot, _, err := service.groundedProviderContext(t.Context(), principal, domaincopilot.Session{ID: "source"}, domaincopilot.SessionMetadata{Mode: "general"}, []domaincopilot.Message{original}, domaincopilot.Message{ID: "current", Role: "user", Content: "explain further"}, "en-US")
	if err != nil || snapshot == nil || len(messages) != 3 {
		t.Fatalf("historical evidence lost its durable lineage: %v snapshot=%v", err, snapshot)
	}
	derived := domaincopilot.Message{ID: "derived", Role: "assistant", Content: "derived protected answer", Metadata: map[string]any{"contextSnapshot": snapshot}}
	validator.denied = true
	visible, withheld := service.groundedHistory(t.Context(), principal, []domaincopilot.Message{original, derived, {ID: "ordinary", Role: "user", Content: "fresh question"}})
	if !withheld || len(visible) != 1 || visible[0].ID != "ordinary" {
		t.Fatal("derived answer survived withdrawal of the historical source")
	}
}

func TestContextHistoricalMemoryDeletionRemovesDerivedAnswer(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermAIMemoryView}})
	memory, err := appmemory.NewService(appmemory.NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	service.SetChatMemoryReader(memory)
	policy := appmemory.Policy{ID: "personal", Version: "v1", OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: time.Hour, ExplicitWriteOnly: true, Enabled: true}
	if err := memory.PutPolicy(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	record, err := memory.PutRecord(t.Context(), appmemory.Record{ID: "fact", OwnerType: "user", OwnerID: "owner", ScopeHash: "sha256:test", Fact: "private fact", SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	citation := domainknowledge.Citation{URI: "memory:fact", ContentHash: chatMemoryContentHash(record)}
	original := domaincopilot.Message{ID: "original", Role: "assistant", Content: "remembered private fact", Metadata: map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{citation}}}}
	principal := domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}
	_, snapshot, _, err := service.groundedProviderContext(t.Context(), principal, domaincopilot.Session{ID: "session"}, domaincopilot.SessionMetadata{}, []domaincopilot.Message{original}, domaincopilot.Message{ID: "current", Content: "continue"}, "en-US")
	if err != nil || snapshot == nil {
		t.Fatal("historical memory source was not preserved")
	}
	derived := domaincopilot.Message{ID: "derived", Role: "assistant", Content: "further private fact", Metadata: map[string]any{"contextSnapshot": snapshot}}
	if err := memory.DeleteRecord(t.Context(), "fact"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.validateMessageKnowledge(t.Context(), principal, derived); err == nil {
		t.Fatal("deleted memory survived through a newly derived answer")
	}
}

func TestContextHistoryCitationBudgetDeduplicatesAndRejectsOverflow(t *testing.T) {
	principal := domainidentity.Principal{UserID: "owner"}
	one := domainknowledge.Citation{KnowledgeBaseID: "base", DocumentID: "document", ChunkID: "chunk", ContentHash: "hash"}
	envelope, err := inheritChatHistoryCitations(t.Context(), principal, "session", "question", nil, []domainknowledge.Citation{one, one})
	if err != nil || envelope == nil || len(envelope.Citations) != 1 {
		t.Fatal("duplicate history citations consumed the source budget")
	}
	inherited := make([]domainknowledge.Citation, 201)
	for index := range inherited {
		inherited[index] = domainknowledge.Citation{KnowledgeBaseID: "base", DocumentID: string(rune(index + 1))}
	}
	if envelope, err := inheritChatHistoryCitations(t.Context(), principal, "session", "question", nil, inherited); err == nil || envelope != nil {
		t.Fatal("oversized provenance was silently dropped or accepted")
	}
}
