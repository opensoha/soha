package copilot

import (
	"context"
	"encoding/json"
	"errors"
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

func TestNativeModelContextUnknownResultDoesNotInventReceipt(t *testing.T) {
	service, loop, gateway, _ := nativeLoopFixture(t)
	gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		return appaigateway.WorkbenchRelayResponse{}, errors.New("fixture upstream outcome unknown")
	}
	if err := service.processNativeRun(t.Context(), loop, loop.lease); err != nil {
		t.Fatal(err)
	}
	var cp nativeCheckpoint
	if err := json.Unmarshal(loop.lease.Checkpoint, &cp); err != nil {
		t.Fatal(err)
	}
	if cp.ModelContext == nil || cp.ModelContext.Status != "outcome_unknown" || cp.ModelContext.GatewayRequestID != "" || cp.ModelContext.UsageKnown || cp.ModelContext.ReservedTokens <= 0 || cp.BudgetTokens <= 0 {
		t.Fatal("unknown attempt lost its reservation or invented a receipt")
	}
}

func assertNativeModelRevisionHistory(t *testing.T, checkpoints []json.RawMessage) {
	t.Helper()
	if len(checkpoints) != 3 {
		t.Fatalf("model revisions=%d, want 3", len(checkpoints))
	}
	ids, hashes := map[string]bool{}, map[string]bool{}
	previousMessages := 0
	for _, raw := range checkpoints {
		var cp nativeCheckpoint
		if err := json.Unmarshal(raw, &cp); err != nil {
			t.Fatal(err)
		}
		current := cp.ModelContext
		if current == nil || current.Status != "prepared" || ids[current.ID] || hashes[current.RequestHash] || len(current.Messages) <= previousMessages {
			t.Fatal("separate model attempts lost identity or accumulated observations")
		}
		ids[current.ID], hashes[current.RequestHash], previousMessages = true, true, len(current.Messages)
	}
}

func assertNativeLatestContextUsage(t *testing.T, store *nativeLoopStore) {
	t.Helper()
	usage, ok := store.final[0].Metadata["contextUsage"].(*domaincopilot.ModelContextUsage)
	if !ok || usage == nil {
		t.Fatal("latest model context usage is missing")
	}
	total, ok := store.lease.Run.Output["usage"].(map[string]any)
	if !ok || usage.InputTokens == nil || *usage.InputTokens != 8 || usage.ContextWindowTokens != 128000 || total["totalTokens"] != int64(30) {
		t.Fatal("latest model window was conflated with cumulative task consumption")
	}
}

func TestNativeModelContextIsSavedBeforeInvocation(t *testing.T) {
	service, loop, gateway, _ := nativeLoopFixture(t)
	loop.lease.Run.Input["modelPolicy"] = map[string]any{"publicModel": "fixture"}
	gateway.model = func(_ context.Context, request appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		var cp nativeCheckpoint
		if err := json.Unmarshal(loop.lease.Checkpoint, &cp); err != nil {
			t.Fatal(err)
		}
		if cp.ModelContext == nil || cp.ModelContext.Status != "prepared" || cp.ModelContext.RequestedPublicModel != request.PublicModel || len(cp.ModelContext.RequestHash) != 64 || len(loop.modelCheckpoints) != 1 {
			t.Fatal("model dispatch preceded durable context revision")
		}
		return appaigateway.WorkbenchRelayResponse{Content: "Grounded answer", PublicModel: "resolved-fixture", RouteID: "resolved-route", RequestID: "gateway-request", Usage: map[string]float64{"totalTokens": 100}}, nil
	}
	if err := service.processNativeRun(t.Context(), loop, loop.lease); err != nil {
		t.Fatal(err)
	}
	var final nativeCheckpoint
	if err := json.Unmarshal(loop.lease.Checkpoint, &final); err != nil {
		t.Fatal(err)
	}
	if final.ModelContext == nil || final.ModelContext.Status != "response_received" || final.ModelContext.GatewayRequestID != "gateway-request" || !final.ModelContext.UsageKnown || final.ModelContext.CostKnown {
		t.Fatal("resolved context lacks receipt/usage or invents known cost")
	}
	var prepared nativeCheckpoint
	if err := json.Unmarshal(loop.modelCheckpoints[0], &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.ModelContext.Status != "prepared" || prepared.ModelContext.ResolvedPublicModel != "" {
		t.Fatal("completed response mutated the prepared journal entry")
	}
}

func TestNativeModelContextRecordsHistorySelectionBeforeInvocation(t *testing.T) {
	service, loop, gateway, _ := nativeLoopFixture(t)
	repo, ok := service.messages.(*inspectionAuthzTestRepository)
	if !ok {
		t.Fatal("missing history fixture")
	}
	loop.lease.Run.Input["requestMessageId"] = "request"
	old := loop.lease.Run.QueuedAt.Add(-time.Minute)
	citation := domainknowledge.Citation{ID: "citation", KnowledgeBaseID: "base", DocumentID: "document", ChunkID: "chunk", ContentHash: "hash"}
	service.contextBuilder = NewContextBuilder(&nativeCitationFixture{denied: true}, service.permissions)
	repo.messages = []domaincopilot.Message{
		{ID: "visible", Role: "user", Content: "visible historical message", CreatedAt: old},
		{ID: "revoked", Role: "assistant", Content: "excluded protected body", CreatedAt: old, Metadata: map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{citation}}}},
		{ID: "request", Role: "user", Content: "duplicate historical body", CreatedAt: old},
		{ID: "later", Role: "user", Content: "later private body", CreatedAt: loop.lease.Run.QueuedAt},
		{ID: "system", Role: "system", Content: "untrusted system body", CreatedAt: old},
		{ID: "empty", Role: "user", Content: "  ", CreatedAt: old},
		{ID: "legacy", Role: "assistant", Content: "legacy body", CreatedAt: old, Metadata: map[string]any{"hiddenInGeneralChat": true}},
	}
	gateway.model = func(_ context.Context, request appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		var cp nativeCheckpoint
		if err := json.Unmarshal(loop.lease.Checkpoint, &cp); err != nil {
			t.Fatal(err)
		}
		if cp.ModelContext == nil {
			t.Fatal("missing persisted model context")
		}
		assertNativeHistorySelection(t, cp.ModelContext.Lineage, loop.lease.Run.QueuedAt)
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		for _, body := range []string{"excluded protected body", "duplicate historical body", "later private body", "untrusted system body", "legacy body"} {
			if strings.Contains(string(encoded), body) {
				t.Fatalf("excluded body reached Gateway: %s", body)
			}
		}
		return appaigateway.WorkbenchRelayResponse{Content: "Answer with authorized history"}, nil
	}
	if err := service.processNativeRun(t.Context(), loop, loop.lease); err != nil {
		t.Fatal(err)
	}
}

func assertNativeHistorySelection(t *testing.T, lineage chatContextLineage, queuedAt time.Time) {
	t.Helper()
	if lineage.RequestMessageID != "request" || !reflect.DeepEqual(lineage.HistoryMessageIDs, []string{"visible"}) {
		t.Fatal("current request and historical source identities were lost")
	}
	if lineage.HistoryWindow == nil || lineage.HistoryWindow.Scope != "recent_window" || lineage.HistoryWindow.Limit != 21 || lineage.HistoryWindow.Loaded != 7 || !lineage.HistoryWindow.Before.Equal(queuedAt) {
		t.Fatal("history window claimed full history or lost its time boundary")
	}
	want := map[string]string{"revoked": "source_unavailable", "request": "duplicate_current_request", "later": "at_or_after_request", "system": "unsupported_role", "empty": "empty_content", "legacy": "legacy_platform_context"}
	got := map[string]string{}
	for _, excluded := range lineage.ExcludedMessages {
		got[excluded.MessageID] = excluded.Reason
	}
	if !reflect.DeepEqual(got, want) || len(lineage.ExcludedMessages) != len(want) {
		t.Fatalf("history exclusions=%v, want %v", got, want)
	}
	encoded, err := json.Marshal(lineage)
	if err != nil || strings.Contains(string(encoded), "body") || strings.Contains(string(encoded), "visible historical message") {
		t.Fatal("history lineage copied message bodies")
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
