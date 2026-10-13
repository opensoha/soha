package copilot

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
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
	envelope := requireExplicitChatMemoryFact(t, service, owner, metadata, "original")
	derived := domaincopilot.Message{ID: "derived", Role: "assistant", Content: "Remembered original fact", Metadata: map[string]any{"contextEnvelope": contextSnapshot(*envelope)}}
	if _, err := service.validateMessageKnowledge(ctx, owner, derived); err != nil {
		t.Fatalf("current memory lineage rejected: %v", err)
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
	if _, err := service.validateMessageKnowledge(ctx, owner, derived); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("corrected fact remained in history: %v", err)
	}
	envelope = requireExplicitChatMemoryFact(t, service, owner, metadata, "corrected")
	corrected := domaincopilot.Message{Role: "assistant", Content: "Remembered corrected fact", Metadata: map[string]any{"contextSnapshot": contextSnapshot(*envelope)}}
	record.SourceRefs = []string{"kb:old-cached-source"}
	if _, err := memory.PutRecord(ctx, record, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := service.buildChatContext(ctx, owner, "session", metadata, "recall"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("opaque source was treated as current authorization: %v", err)
	}
	record.SourceRefs = nil
	if _, err := memory.PutRecord(ctx, record, policy); err != nil {
		t.Fatal(err)
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
	visible, withheld := service.groundedHistory(ctx, owner, []domaincopilot.Message{corrected, {Role: "user", Content: "new question"}})
	if !withheld || len(visible) != 1 || visible[0].Role != "user" {
		t.Fatal("deleted memory returned through derived history")
	}
	if _, err := service.buildChatContext(ctx, owner, "session", metadata, "recall"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted memory was reused: %v", err)
	}
	if envelope, err := service.buildChatContext(ctx, owner, "session", domaincopilot.SessionMetadata{}, "recall"); err != nil || envelope != nil {
		t.Fatal("memory was automatically injected")
	}
}

func requireExplicitChatMemoryFact(t *testing.T, service *Service, owner domainidentity.Principal, metadata domaincopilot.SessionMetadata, want string) *domaincopilot.ContextEnvelope {
	t.Helper()
	envelope, err := service.buildChatContext(t.Context(), owner, "session", metadata, "recall")
	if err != nil || envelope == nil || len(envelope.Evidence) != 1 || envelope.Evidence[0].Content != want {
		t.Fatalf("memory fact want %q: %+v %v", want, envelope, err)
	}
	return envelope
}

type branchingMemoryReceiptStore struct {
	*branchReferenceStore
	citations map[string]domainknowledge.Citation
	reads     int
}

func (r *branchingMemoryReceiptStore) GetMessage(_ context.Context, sessionID, id string) (domaincopilot.Message, error) {
	r.reads++
	return domaincopilot.Message{ID: id, SessionID: sessionID, Role: "assistant", Content: "source", Metadata: map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{r.citations[strings.SplitN(id, "-", 2)[0]]}}}}, nil
}

func TestMemorySourceChainBoundsTotalReceiptReads(t *testing.T) {
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermAIMemoryView}})
	memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
	service.SetChatMemoryReader(memory)
	policy := appmemory.Policy{ID: "personal", Version: "v1", OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: time.Hour, ExplicitWriteOnly: true, Enabled: true}
	if err := memory.PutPolicy(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	records := map[string]appmemory.Record{}
	for _, id := range []string{"root", "branch", "leaf"} {
		refs := []string{}
		if id != "leaf" {
			next := "branch"
			if id == "branch" {
				next = "leaf"
			}
			for index := range 32 {
				refs = append(refs, "soha://session/source/message/"+next+"-"+strconv.Itoa(index))
			}
		}
		record, err := memory.PutRecord(t.Context(), appmemory.Record{ID: id, OwnerType: "user", OwnerID: "owner", ScopeHash: "sha256:fixture", Fact: id, SourceRefs: refs, SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}, policy)
		if err != nil {
			t.Fatal(err)
		}
		records[id] = record
	}
	store := &branchingMemoryReceiptStore{branchReferenceStore: &branchReferenceStore{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}}, citations: map[string]domainknowledge.Citation{}}
	for id, record := range records {
		store.citations[id] = domainknowledge.Citation{URI: "memory:" + id, ContentHash: chatMemoryContentHash(record)}
	}
	service.sessions, service.messages = store, store
	_, err := service.authorizeChatMemoryRecord(t.Context(), domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}, records["root"])
	if !errors.Is(err, apperrors.ErrAccessDenied) || !strings.Contains(err.Error(), "200 receipts") || store.reads > 200 {
		t.Fatalf("source chain was not bounded: reads=%d err=%v", store.reads, err)
	}
}

func TestSourceBackedMemoryRechecksOriginalMessageAndRejectsCycles(t *testing.T) {
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermAIMemoryView, appaccess.PermAIKnowledgeView}})
	citation := domainknowledge.Citation{ID: "source", KnowledgeBaseID: "base", DocumentID: "doc", ChunkID: "chunk", ContentHash: "hash", DocumentVersionID: "version"}
	store := &branchReferenceStore{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}, messageMetadata: map[string]any{"contextEnvelope": map[string]any{"citations": []domainknowledge.Citation{citation}}}}
	validator := &nativeCitationFixture{}
	service.sessions, service.messages, service.contextBuilder = store, store, NewContextBuilder(validator, service.permissions)
	memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
	service.SetChatMemoryReader(memory)
	policy := appmemory.Policy{ID: "personal", Version: "v1", OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: time.Hour, ExplicitWriteOnly: true, Enabled: true}
	if err := memory.PutPolicy(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	record, err := memory.PutRecord(t.Context(), appmemory.Record{ID: "fact", OwnerType: "user", OwnerID: "owner", ScopeHash: "sha256:fixture", Fact: "remembered source fact", SourceRefs: []string{"soha://session/source/message/old"}, SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	owner := domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}
	metadata := domaincopilot.SessionMetadata{RequestContext: &domaincopilot.ContextSelection{MemoryIDs: []string{record.ID}}}
	envelope, err := service.buildChatContext(t.Context(), owner, "current", metadata, "recall")
	if err != nil || len(envelope.Citations) != 2 || envelope.Citations[1].DocumentVersionID != "version" {
		t.Fatalf("memory source lineage lost: %+v %v", envelope, err)
	}
	validator.denied = true
	if _, err := service.buildChatContext(t.Context(), owner, "current", metadata, "recall"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked original source entered memory context: %v", err)
	}
	validator.denied = false
	store.messageMetadata = map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{{URI: "memory:fact", ContentHash: chatMemoryContentHash(record)}}}}
	if _, err := service.buildChatContext(t.Context(), owner, "current", metadata, "recall"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("cyclic memory source accepted: %v", err)
	}
	for _, bad := range []string{"https://session/source/message/old", "soha://session/source/message/old?expand=true", "soha://session/source/message/old#fragment", "soha://other/source/message/old", "source-label"} {
		if _, err := chatMemorySourceReference(bad); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("unverifiable source accepted: %v", err)
		}
	}
}
