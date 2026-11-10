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
	policy := appmemory.Policy{ID: appmemory.PrivatePolicyID("owner", "chat"), Version: "v1", OwnerID: "owner", Mode: "chat", OwnerTypes: []string{"user"}, DefaultTTL: 24 * time.Hour, MaximumTTL: 24 * time.Hour, ExplicitWriteOnly: true, Enabled: true}
	if err := memory.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	record, err := memory.PutRecord(ctx, appmemory.Record{ID: "fact", OwnerType: "user", OwnerID: "owner", ScopeHash: appmemory.Scope("owner", "chat"), Fact: "original", SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}, policy)
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
	other.ScopeHash = appmemory.Scope(other.OwnerID, "chat")
	if _, err := memory.PutRecord(ctx, other, policy); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("foreign policy accepted for a memory write: %v", err)
	}
	otherPolicy := policy
	otherPolicy.ID = appmemory.PrivatePolicyID(other.OwnerID, "chat")
	otherPolicy.OwnerID = other.OwnerID
	if err := memory.PutPolicy(ctx, otherPolicy); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.PutRecord(ctx, other, otherPolicy); !errors.Is(err, apperrors.ErrAccessDenied) {
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
	policy := appmemory.Policy{ID: appmemory.PrivatePolicyID("owner", "chat"), Version: "v1", OwnerID: "owner", Mode: "chat", OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: time.Hour, ExplicitWriteOnly: true, Enabled: true}
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
		hashes := map[string]string{}
		for _, ref := range refs {
			hashes[ref] = memoryMessageHash(domaincopilot.Message{Role: "assistant", Content: "source"})
		}
		record, err := memory.PutRecord(t.Context(), appmemory.Record{SourceHashes: hashes, ID: id, OwnerType: "user", OwnerID: "owner", ScopeHash: appmemory.Scope("owner", "chat"), Fact: id, SourceRefs: refs, SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}, policy)
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
	policy := appmemory.Policy{ID: appmemory.PrivatePolicyID("owner", "chat"), Version: "v1", OwnerID: "owner", Mode: "chat", OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: time.Hour, ExplicitWriteOnly: true, Enabled: true}
	if err := memory.PutPolicy(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	record, err := memory.PutRecord(t.Context(), appmemory.Record{SourceHashes: map[string]string{"soha://session/source/message/old": memoryMessageHash(domaincopilot.Message{Role: "assistant", Content: "frozen selected background"})}, ID: "fact", OwnerType: "user", OwnerID: "owner", ScopeHash: appmemory.Scope("owner", "chat"), Fact: "remembered source fact", SourceRefs: []string{"soha://session/source/message/old"}, SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}, policy)
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

type liveMemorySource struct {
	*inspectionAuthzTestRepository
	content  string
	heya     bool
	metadata map[string]any
}

func (f *liveMemorySource) GetSession(_ context.Context, owner, id string) (domaincopilot.Session, error) {
	if owner != "owner" || id != "source" {
		return domaincopilot.Session{}, apperrors.ErrAccessDenied
	}
	kind := "main"
	if f.heya {
		kind = "personal_side"
	}
	return domaincopilot.Session{ID: id, CreatedBy: owner, Metadata: map[string]any{"conversationKind": kind}}, nil
}
func (f *liveMemorySource) GetMessage(_ context.Context, session, id string) (domaincopilot.Message, error) {
	if session != "source" || id != "message" {
		return domaincopilot.Message{}, apperrors.ErrNotFound
	}
	return domaincopilot.Message{ID: id, SessionID: session, Role: "user", Content: f.content, Metadata: f.metadata}, nil
}
func TestLiveMemorySourceRevalidatesRevisionOwnerAndMode(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermAIMemoryView, appaccess.PermObserveAIChatUse}})
	owner := domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}
	resolver := &chatToolPrincipalResolver{principal: owner}
	service.agentPrincipals = resolver
	source := &liveMemorySource{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}, content: "I prefer concise answers"}
	service.sessions, service.messages = source, source
	memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
	service.SetChatMemoryReader(memory)
	p := appmemory.Policy{ID: "p", Version: "1", OwnerID: "owner", Mode: "chat", OwnerTypes: []string{"user"}, Enabled: true, ExplicitWriteOnly: true, DefaultTTL: time.Hour, MaximumTTL: time.Hour}
	if err := memory.PutPolicy(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	r, err := service.PrepareMemoryRecord(t.Context(), owner, appmemory.Record{ID: "r", Fact: "concise", PolicyVer: "1", SourceType: "explicit_user", Confidence: 1, SourceRefs: []string{"soha://session/source/message/message"}}, p)
	if err != nil {
		t.Fatal(err)
	}
	r, err = memory.PutRecord(t.Context(), r, p)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ValidateSource(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	source.content = "modified source"
	if err = service.ValidateSource(t.Context(), r); err == nil {
		t.Fatal("changed source accepted")
	}
	source.content = "I prefer concise answers"
	source.heya = true
	if err = service.ValidateSource(t.Context(), r); err == nil {
		t.Fatal("cross-surface receipt accepted")
	}
	source.heya = false
	resolver.principal = domainidentity.Principal{UserID: "other", Roles: []string{"reader"}}
	if err = service.ValidateSource(t.Context(), r); err == nil {
		t.Fatal("identity mismatch accepted")
	}
}
func TestAutomaticMemoryRequiresPrivateHeyaConsentAndPureUserPreference(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermAIMemoryView, appaccess.PermObserveAIChatUse, appaccess.ManagedActionPermission(appaccess.PermAIMemoryManage, "update")}})
	owner := domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}
	source := &liveMemorySource{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}, content: "我偏好简短回答", heya: true}
	service.sessions, service.messages = source, source
	service.agentPrincipals = &chatToolPrincipalResolver{principal: owner}
	memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
	service.SetChatMemoryReader(memory)
	p := appmemory.Policy{ID: "p", Version: "1", OwnerID: "owner", Mode: "heya", OwnerTypes: []string{"user"}, Enabled: true, ExplicitWriteOnly: false, DefaultTTL: time.Hour, MaximumTTL: time.Hour}
	memory.PutPolicy(t.Context(), p)
	if err := service.RememberChatUserMessage(t.Context(), owner, "source", "message", "chat"); err != nil {
		t.Fatal(err)
	}
	records, _ := memory.ListRecords(t.Context(), "user", "owner")
	if len(records) != 0 {
		t.Fatal("Chat auto-wrote")
	}
	if err := service.RememberChatUserMessage(t.Context(), owner, "source", "message", "heya"); err != nil {
		t.Fatal(err)
	}
	records, _ = memory.ListRecords(t.Context(), "user", "owner")
	if len(records) != 1 || records[0].SourceType != "curated_extractor" {
		t.Fatalf("%+v", records)
	}
	p.Enabled = false
	memory.PutPolicy(t.Context(), p)
	if err := service.ValidateSource(t.Context(), records[0]); err == nil {
		t.Fatal("revoked consent accepted")
	}
	p.Enabled = true
	memory.PutPolicy(t.Context(), p)
	source.metadata = map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{{URI: "memory:missing"}}}}
	if err := service.RememberChatUserMessage(t.Context(), owner, "source", "message", "heya"); err == nil {
		t.Fatal("derived knowledge auto-retained")
	}
}

type sharingMemorySource struct {
	*liveMemorySource
	messages map[string]domaincopilot.Message
}

func (f *sharingMemorySource) GetMessage(_ context.Context, session, id string) (domaincopilot.Message, error) {
	if session != "source" {
		return domaincopilot.Message{}, apperrors.ErrAccessDenied
	}
	m, ok := f.messages[id]
	if !ok {
		return domaincopilot.Message{}, apperrors.ErrNotFound
	}
	return m, nil
}
func TestExplicitMemorySharingRequiresPersistedSelectionAndCarriesVerifiedReceipts(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermAIMemoryView}})
	owner := domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}
	source := &sharingMemorySource{liveMemorySource: &liveMemorySource{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}}, messages: map[string]domaincopilot.Message{}}
	service.sessions, service.messages = source, source
	memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
	service.SetChatMemoryReader(memory)
	p := appmemory.Policy{ID: "heya", Version: "1", OwnerID: "owner", Mode: "heya", OwnerTypes: []string{"user"}, Enabled: true, ExplicitWriteOnly: true, DefaultTTL: time.Hour, MaximumTTL: time.Hour}
	memory.PutPolicy(t.Context(), p)
	r, err := memory.PutRecord(t.Context(), appmemory.Record{ID: "r", OwnerType: "user", OwnerID: "owner", ScopeHash: appmemory.Scope("owner", "heya"), Fact: "my personal preference", SourceType: "explicit_user", Confidence: 1, PolicyVer: "1"}, p)
	if err != nil {
		t.Fatal(err)
	}
	env := &domaincopilot.ContextEnvelope{SessionID: "source", Citations: []domainknowledge.Citation{{URI: "memory:r", ContentHash: chatMemoryContentHash(r)}}}
	source.messages["selected"] = domaincopilot.Message{ID: "selected", SessionID: "source", Role: "user", Metadata: map[string]any{"_sohaExplicitMemoryIds": []string{"r"}}}
	source.messages["next"] = domaincopilot.Message{ID: "next", SessionID: "source", Role: "user"}
	if err = service.ValidateChatMemoryScopeForTurn(t.Context(), owner, env, "chat", "next", nil); err == nil {
		t.Fatal("citation granted sharing without selection")
	}
	receipts, err := service.MemoryShareReceiptsForTurn(t.Context(), owner, env, "chat", "selected", nil)
	if err != nil || receipts["r"].SelectionMessageID != "selected" {
		t.Fatalf("%+v %v", receipts, err)
	}
	source.messages["answer"] = domaincopilot.Message{ID: "answer", SessionID: "source", Role: "assistant", Metadata: map[string]any{"requestMessageId": "selected", "contextSnapshot": map[string]any{"citations": env.Citations, "_sohaMemoryShareReceipts": receipts}}}
	inherited, err := service.MemoryShareReceiptsForTurn(t.Context(), owner, env, "chat", "next", []string{"answer"})
	if err != nil || inherited["r"] != receipts["r"] {
		t.Fatalf("inherited %+v %v", inherited, err)
	}
	source.messages["derived"] = domaincopilot.Message{ID: "derived", SessionID: "source", Role: "assistant", Metadata: map[string]any{"requestMessageId": "next", "contextSnapshot": map[string]any{"citations": env.Citations, "_sohaMemoryShareReceipts": inherited}}}
	if err = service.ValidateChatMemoryScopeForTurn(t.Context(), owner, env, "chat", "", []string{"derived"}); err != nil {
		t.Fatal("multi-turn receipt lost", err)
	}
	// A real assistant citation and request ID without its server share receipt do not grant access.
	source.messages["legacy"] = domaincopilot.Message{ID: "legacy", SessionID: "source", Role: "assistant", Metadata: map[string]any{"requestMessageId": "selected", "contextSnapshot": map[string]any{"citations": env.Citations}}}
	if err = service.ValidateChatMemoryScopeForTurn(t.Context(), owner, env, "chat", "", []string{"legacy"}); err == nil {
		t.Fatal("legacy assistant inferred sharing")
	}
	no := false
	target := p
	target.ID = "chat"
	target.Mode = "chat"
	target.ReadEnabled = &no
	memory.PutPolicy(t.Context(), target)
	if err = service.ValidateChatMemoryScopeForTurn(t.Context(), owner, env, "chat", "selected", nil); err == nil {
		t.Fatal("recipient read opt-out bypassed")
	}
	yes := true
	target.ReadEnabled = &yes
	memory.PutPolicy(t.Context(), target)
	p.Enabled = false
	memory.PutPolicy(t.Context(), p)
	if err = service.ValidateChatMemoryScopeForTurn(t.Context(), owner, env, "chat", "", []string{"derived"}); err == nil {
		t.Fatal("donor revoked consent bypassed")
	}
	p.Enabled = true
	memory.PutPolicy(t.Context(), p)
	// Raising the current policy threshold revokes a previously shared lower-confidence fact.
	p.MinimumConfidence = 1
	r.Confidence = 0.5
	r, err = memory.PutRecord(t.Context(), r, appmemory.Policy{ID: p.ID, Version: p.Version, OwnerID: p.OwnerID, Mode: p.Mode, OwnerTypes: p.OwnerTypes, Enabled: true, ExplicitWriteOnly: true, DefaultTTL: time.Hour, MaximumTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	memory.PutPolicy(t.Context(), p)
	if err = service.ValidateChatMemoryScopeForTurn(t.Context(), owner, env, "chat", "selected", nil); err == nil {
		t.Fatal("raised confidence threshold ignored")
	}
	p.MinimumConfidence = 0
	memory.PutPolicy(t.Context(), p)
	// Unchanged fact with changed source provenance still invalidates historical sharing.
	r.SourceRefs = []string{"soha://session/source/message/selected"}
	r.SourceHashes = map[string]string{r.SourceRefs[0]: memoryMessageHash(source.messages["selected"])}
	r, err = memory.PutRecord(t.Context(), r, p)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ValidateChatMemoryScopeForTurn(t.Context(), owner, env, "chat", "", []string{"derived"}); err == nil {
		t.Fatal("changed source reused a sharing receipt")
	}
	memory.DeleteRecord(t.Context(), "r")
	if err = service.ValidateChatMemoryScopeForTurn(t.Context(), owner, env, "chat", "selected", nil); err == nil {
		t.Fatal("deleted memory shared")
	}
}

func TestClosedChatCannotSelectLegacyGlobalMemory(t *testing.T) {
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermAIMemoryView}})
	memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
	service.SetChatMemoryReader(memory)
	owner := domainidentity.Principal{UserID: "owner", Roles: []string{"reader"}}
	legacy := appmemory.Policy{ID: "legacy", Version: "v1", OwnerTypes: []string{"user"}, Enabled: true,
		DefaultTTL: time.Hour, MaximumTTL: time.Hour, ExplicitWriteOnly: true}
	if err := memory.PutPolicy(t.Context(), legacy); err != nil {
		t.Fatal(err)
	}
	record, err := memory.PutRecord(t.Context(), appmemory.Record{ID: "legacy-memory", OwnerType: "user", OwnerID: owner.UserID,
		ScopeHash: appmemory.Scope(owner.UserID, "chat"), SourceType: "explicit_user", PolicyVer: legacy.Version, Fact: "legacy"}, legacy)
	if err != nil {
		t.Fatal(err)
	}
	readOnly := false
	for _, name := range []string{"legacy_fallback", "read_only", "read_denied", "disabled"} {
		t.Run(name, func(t *testing.T) {
			consent := legacy
			consent.ID, consent.Mode = "", "chat"
			switch name {
			case "read_only":
				consent.WriteEnabled = &readOnly
			case "read_denied":
				consent.ReadEnabled = &readOnly
			case "disabled":
				consent.Enabled = false
			}
			if name != "legacy_fallback" {
				if _, err := memory.SavePersonalPolicy(t.Context(), owner.UserID, consent); err != nil {
					t.Fatal(err)
				}
			}
			envelope := &domaincopilot.ContextEnvelope{}
			envelope.Budgets.MaxEvidenceTokens = 16
			err := service.appendChatMemory(t.Context(), owner, envelope, []string{record.ID})
			if name == "legacy_fallback" || name == "read_only" {
				if err != nil || len(envelope.Citations) != 1 || envelope.Citations[0].URI != "memory:"+record.ID {
					t.Fatalf("readable legacy memory unavailable: %+v %v", envelope.Citations, err)
				}
				return
			}
			if !errors.Is(err, apperrors.ErrAccessDenied) || len(envelope.Citations) != 0 {
				t.Fatalf("closed Chat accepted legacy global memory: %+v %v", envelope.Citations, err)
			}
		})
	}
}
