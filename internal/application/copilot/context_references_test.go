package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type contextResourceReader struct{ agentToolResourceReader }

func (r contextResourceReader) ListPods(ctx context.Context, p domainidentity.Principal, cluster, namespace string) ([]domainresource.PodView, error) {
	if p.UserID != "reader" || cluster != "allowed" || namespace != "apps" {
		return nil, apperrors.ErrAccessDenied
	}
	return r.agentToolResourceReader.ListPods(ctx, p, cluster, namespace)
}

func TestChatReferencesReadAsUserAndClipAttachments(t *testing.T) {
	service := &Service{resources: contextResourceReader{agentToolResourceReader{pods: []domainresource.PodView{{Name: "api", Namespace: "apps", Phase: "Running"}}}}}
	selection := &domaincopilot.ContextSelection{
		References:  []domaincopilot.ContextReference{{Kind: "pod", ClusterID: "allowed", Namespace: "apps", Name: "api"}},
		Attachments: []domaincopilot.TextAttachment{{ID: "file-1", Name: "incident.log", Content: "password=do-not-send\n" + strings.Repeat("x", 30000)}},
	}
	metadata := domaincopilot.SessionMetadata{RequestContext: selection}
	ctx := context.Background()
	if _, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "other"}, "session", metadata, "inspect"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("resource ACL not enforced: %v", err)
	}
	envelope, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "reader"}, "session", metadata, "inspect")
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope.Evidence) != 2 || len(envelope.Truncations) != 1 || envelope.BudgetUsage.EvidenceTokens != 6000 || envelope.ContentHash == "" {
		t.Fatalf("missing budget/provenance: %+v", envelope)
	}
	if strings.Contains(envelope.Evidence[1].Content, "do-not-send") {
		t.Fatal("attachment secret was not redacted")
	}
	if envelope.Citations[0].URI != "resource:allowed/apps/pod/api" {
		t.Fatal("resource cannot be located")
	}
	metadata.RequestContext = nil
	if removed, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "reader"}, "session", metadata, "next"); err != nil || removed != nil {
		t.Fatal("removed references inherited")
	}
	for _, file := range []domaincopilot.TextAttachment{{ID: "x", Name: "test.pdf"}, {ID: "x", Name: "test.log", Content: "binary\x00data"}, {ID: "x", Name: "test.log", Content: strings.Repeat("x", 65537)}} {
		if validateChatAttachment(file) == nil {
			t.Fatalf("invalid attachment accepted: %s", file.Name)
		}
	}
}

type branchReferenceStore struct {
	*inspectionAuthzTestRepository
	archived        bool
	messageMetadata map[string]any
}

func (r *branchReferenceStore) GetSession(_ context.Context, owner, id string) (domaincopilot.Session, error) {
	if owner != "owner" || id != "source" {
		return domaincopilot.Session{}, apperrors.ErrAccessDenied
	}
	metadata := map[string]any{}
	if r.archived {
		metadata["archivedAt"] = "2026-09-10T00:00:00Z"
	}
	return domaincopilot.Session{ID: "source", CreatedBy: "owner", Title: "Original", Metadata: metadata}, nil
}
func (r *branchReferenceStore) GetMessage(_ context.Context, session, id string) (domaincopilot.Message, error) {
	if session != "source" || id != "old" {
		return domaincopilot.Message{}, apperrors.ErrNotFound
	}
	return domaincopilot.Message{ID: "old", SessionID: session, Role: "assistant", Content: "frozen selected background", Metadata: r.messageMetadata}, nil
}

func TestChatKnowledgeLineageRevocationBlocksQuotesAndHistory(t *testing.T) {
	citation := domainknowledge.Citation{ID: "citation", KnowledgeBaseID: "base", DocumentID: "document", ChunkID: "chunk", ContentHash: "hash", DocumentVersionID: "version"}
	store := &branchReferenceStore{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}, messageMetadata: map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{citation}}}}
	validator := &nativeCitationFixture{}
	service := &Service{sessions: store, messages: store, contextBuilder: NewContextBuilder(validator, nil)}
	principal := domainidentity.Principal{UserID: "owner"}
	metadata := domaincopilot.SessionMetadata{RequestContext: &domaincopilot.ContextSelection{References: []domaincopilot.ContextReference{{Kind: "session", SessionID: "source", MessageIDs: []string{"old"}}}}}
	envelope, err := service.buildChatContext(context.Background(), principal, "branch", metadata, "quote")
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope.Citations) != 2 || envelope.Citations[0].DocumentVersionID != "version" {
		t.Fatalf("source lineage lost when quoting: %+v", envelope.Citations)
	}
	derived := domaincopilot.Message{ID: "derived", Role: "assistant", Content: "Derived answer from the quote", Metadata: map[string]any{"contextSnapshot": contextSnapshot(*envelope)}}
	validator.denied = true
	if _, err := service.buildChatContext(context.Background(), principal, "branch", metadata, "quote"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked quote accepted: %v", err)
	}
	visible, withheld := service.groundedHistory(context.Background(), principal, []domaincopilot.Message{derived, {ID: "ordinary", Role: "user", Content: "Fresh question"}})
	if !withheld || len(visible) != 1 || visible[0].ID != "ordinary" {
		t.Fatalf("revoked answer entered history: %+v", visible)
	}
	public := service.visibleKnowledgeMessage(context.Background(), principal, derived)
	if strings.Contains(public.Content, "Derived answer") || public.Metadata["knowledgeUnavailable"] != true {
		t.Fatalf("revoked content still public: %+v", public)
	}
	toolDerived := domaincopilot.Message{Metadata: map[string]any{"toolExecutions": []map[string]any{{"toolName": "knowledge.search", "status": "success", "startedAt": "", "output": map[string]any{"knowledgeCitations": []domainknowledge.Citation{citation}}}}}}
	if _, err := service.validateMessageKnowledge(context.Background(), principal, toolDerived); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("tool lineage not validated: %v", err)
	}
}

func TestRevokedKnowledgeIsWithheldFromTaskEventsAndPersonalArtifacts(t *testing.T) {
	service, loop, _, _ := nativeLoopFixture(t)
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	citation := domainknowledge.Citation{ID: "source", KnowledgeBaseID: "base", DocumentID: "doc", ChunkID: "chunk", ContentHash: "hash", DocumentVersionID: "version"}
	metadata := map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{citation}}}
	run := loop.lease.Run
	run.Output["contextSnapshot"] = metadata["contextSnapshot"]
	run.Output["answer"] = "protected source content"
	run.Output["workbenchEvents"] = []domaincopilot.WorkbenchStreamEvent{{Type: "message.delta", ContentDelta: "protected source content"}}
	run.AnalysisArtifacts = []domaincopilot.AnalysisArtifact{{Kind: "report", Summary: "protected source content"}}
	repo, ok := service.sessions.(*inspectionAuthzTestRepository)
	if !ok {
		t.Fatal("personal artifact repository fixture is unavailable")
	}
	repo.session = domaincopilot.Session{ID: run.SessionID, CreatedBy: principal.UserID, Metadata: map[string]any{"conversationKind": "personal_main"}}
	repo.agentRuns = []domaincopilot.AgentRun{run}
	repo.personalArtifacts = []domaincopilot.PersonalArtifact{{ID: "message:derived", Artifact: run.AnalysisArtifacts[0], SourceMetadata: metadata}}
	visible, err := service.ListPersonalAssistantArtifacts(t.Context(), principal)
	if err != nil || len(visible) != 2 {
		t.Fatalf("authorized artifacts missing: %+v %v", visible, err)
	}
	builder, ok := service.contextBuilder.(*ContextBuilder)
	if !ok {
		t.Fatal("context builder fixture is unavailable")
	}
	validator, ok := builder.knowledge.(*nativeCitationFixture)
	if !ok {
		t.Fatal("citation fixture is unavailable")
	}
	validator.denied = true
	assertRevokedTaskAndArtifactProjection(t, service, principal, run, repo)
}

func assertRevokedTaskAndArtifactProjection(t *testing.T, service *Service, principal domainidentity.Principal, run domaincopilot.AgentRun, repo *inspectionAuthzTestRepository) {
	t.Helper()
	var visible []domaincopilot.PersonalArtifact
	var err error
	for _, list := range []func() ([]domaincopilot.AgentRun, error){
		func() ([]domaincopilot.AgentRun, error) {
			return service.ListAgentRuns(t.Context(), principal, run.SessionID)
		},
		func() ([]domaincopilot.AgentRun, error) {
			return service.ListPersonalAssistantTasks(t.Context(), principal)
		},
	} {
		runs, err := list()
		if err != nil || len(runs) != 1 {
			t.Fatalf("task lifecycle hidden: %+v %v", runs, err)
		}
		data, _ := json.Marshal(runs)
		if strings.Contains(string(data), "protected source content") || runs[0].ID != run.ID || runs[0].Status != run.Status || runs[0].Output["knowledgeUnavailable"] != true {
			t.Fatalf("task projection leaked: %s", data)
		}
	}
	visible, err = service.ListPersonalAssistantArtifacts(t.Context(), principal)
	if err != nil || len(visible) != 0 {
		t.Fatalf("revoked artifact remained visible: %+v %v", visible, err)
	}
	page := domaincopilot.NativeEventPage{Items: []domaincopilot.WorkbenchStreamEvent{{ID: "event", Sequence: 42, Type: "message.delta", ContentDelta: "protected source content", RunID: run.ID}}, NextSequence: 42, HasMore: true}
	redacted := service.visibleKnowledgeEvents(t.Context(), principal, run, page)
	data, _ := json.Marshal(redacted)
	if strings.Contains(string(data), "protected source content") || redacted.NextSequence != 42 || !redacted.HasMore || len(redacted.Items) != 1 || redacted.Items[0].Sequence != 42 || redacted.Items[0].Code != "knowledge_unavailable" {
		t.Fatalf("event projection leaked or lost cursor: %s", data)
	}
	if len(repo.agentRuns[0].AnalysisArtifacts) != 1 || repo.agentRuns[0].Output["answer"] != "protected source content" {
		t.Fatal("visibility check modified durable audit data")
	}
}

func TestSummaryChecksSourcesOutsideRecentHistoryWindow(t *testing.T) {
	citation := domainknowledge.Citation{ID: "citation", KnowledgeBaseID: "base", DocumentID: "document", ChunkID: "chunk", ContentHash: "hash", DocumentVersionID: "version"}
	store := &branchReferenceStore{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}, messageMetadata: map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{citation}}}}
	validator := &nativeCitationFixture{}
	service := &Service{sessions: store, messages: store, contextBuilder: NewContextBuilder(validator, nil)}
	principal := domainidentity.Principal{UserID: "owner"}
	metadata := domaincopilot.SessionMetadata{Summary: "A sourced summary", PinnedContext: map[string]any{"summarySourceMessageIds": []string{"old"}}}
	session := domaincopilot.Session{ID: "source", Metadata: sessionMetadataMap(metadata)}
	if visible := service.visibleSessionSummary(t.Context(), principal, session); stringValue(visible.Metadata["summary"]) != metadata.Summary {
		t.Fatal("current summary was hidden from session API")
	}
	messages, snapshot, err := service.groundedProviderMessages(t.Context(), principal, domaincopilot.Session{ID: "source"}, metadata, nil, domaincopilot.Message{Role: "user", Content: "continue"}, "en-US")
	if err != nil || len(messages) != 3 || !strings.Contains(messages[0].Content, "A sourced summary") || snapshot == nil {
		t.Fatalf("current sourced summary missing: %+v %v", messages, err)
	}
	validator.denied = true
	if visible := service.visibleSessionSummary(t.Context(), principal, session); stringValue(visible.Metadata["summary"]) != "" || stringValue(session.Metadata["summary"]) != metadata.Summary {
		t.Fatal("revoked summary leaked through session API or mutated stored metadata")
	}
	envelope, err := service.buildChatContext(t.Context(), principal, "source", metadata, "continue")
	if err != nil || envelope == nil || envelope.Session.Summary != "" || len(envelope.Citations) != 0 || len(envelope.Truncations) != 1 {
		t.Fatalf("revoked summary reintroduced old source: %+v %v", envelope, err)
	}
	metadata.PinnedContext = nil
	envelope, err = service.buildChatContext(t.Context(), principal, "source", metadata, "continue")
	if err != nil || envelope == nil || envelope.Session.Summary != "" || envelope.Truncations[0] != "session.summary:source_lineage_unavailable" {
		t.Fatalf("untracked legacy summary bypassed lineage: %+v %v", envelope, err)
	}
	patch := map[string]any{"summary": "fresh summary"}
	bound := bindChatSummarySource(domaincopilot.SessionMetadata{PinnedContext: map[string]any{"preserved": true}}, patch, "new-message")
	if bound.PinnedContext["preserved"] != true || agentPrincipalStringList(bound.PinnedContext["summarySourceMessageIds"])[0] != "new-message" || patch["pinnedContext"] == nil {
		t.Fatal("summary origin or existing pinned context lost")
	}
}

type summaryProjectionStore struct {
	*branchReferenceStore
	session domaincopilot.Session
}

func (r *summaryProjectionStore) GetSession(_ context.Context, owner, id string) (domaincopilot.Session, error) {
	if owner != r.session.CreatedBy || id != r.session.ID {
		return domaincopilot.Session{}, apperrors.ErrAccessDenied
	}
	return r.session, nil
}

func (r *summaryProjectionStore) ListSessions(context.Context, string, int) ([]domaincopilot.Session, error) {
	return []domaincopilot.Session{r.session}, nil
}

func (r *summaryProjectionStore) GetOrCreateMainSession(context.Context, domaincopilot.Session) (domaincopilot.Session, bool, error) {
	return r.session, false, nil
}

func (r *summaryProjectionStore) UpdateSession(_ context.Context, _, _ string, session domaincopilot.Session) (domaincopilot.Session, error) {
	r.session = session
	return session, nil
}

func TestPublicSessionAPIsHideRevokedSummaryWithoutChangingAuditData(t *testing.T) {
	service, _ := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse, appaccess.PermAIKnowledgeView}})
	citation := domainknowledge.Citation{ID: "citation", KnowledgeBaseID: "base", DocumentID: "document", ChunkID: "chunk", ContentHash: "hash", DocumentVersionID: "version"}
	store := &summaryProjectionStore{branchReferenceStore: &branchReferenceStore{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}, messageMetadata: map[string]any{"contextSnapshot": map[string]any{"citations": []domainknowledge.Citation{citation}}}}, session: domaincopilot.Session{ID: "source", CreatedBy: "owner", Metadata: sessionMetadataMap(domaincopilot.SessionMetadata{ConversationKind: "main", Summary: "protected summary", PinnedContext: map[string]any{"summarySourceMessageIds": []string{"old"}}})}}
	validator := &nativeCitationFixture{}
	service.sessions, service.messages, service.contextBuilder = store, store, NewContextBuilder(validator, service.permissions)
	principal := domainidentity.Principal{UserID: "owner", Roles: []string{"chat"}}
	readers := map[string]func() (domaincopilot.Session, error){
		"get": func() (domaincopilot.Session, error) { return service.GetSession(t.Context(), principal, "source") },
		"main": func() (domaincopilot.Session, error) {
			return service.EnsureMainSession(t.Context(), principal, "internal", "en-US")
		},
		"update": func() (domaincopilot.Session, error) {
			return service.UpdateSession(t.Context(), principal, "source", "title", "", "", "", "", nil, nil, "", nil, nil, false)
		},
		"list": func() (domaincopilot.Session, error) {
			items, err := service.ListSessions(t.Context(), principal)
			if err != nil || len(items) != 1 {
				return domaincopilot.Session{}, apperrors.ErrInvalidArgument
			}
			return items[0], nil
		},
	}
	for _, denied := range []bool{false, true} {
		validator.denied = denied
		want := "protected summary"
		if denied {
			want = ""
		}
		for name, read := range readers {
			session, err := read()
			if err != nil || stringValue(session.Metadata["summary"]) != want {
				t.Fatalf("%s summary visibility: denied=%t %+v %v", name, denied, session, err)
			}
		}
		patch := service.visibleSessionPatch(t.Context(), principal, "source", map[string]any{"summary": "protected summary", "mode": "general"})
		if stringValue(patch["summary"]) != want || patch["mode"] != "general" {
			t.Fatalf("session patch visibility: %+v", patch)
		}
	}
	if stringValue(store.session.Metadata["summary"]) != "protected summary" {
		t.Fatal("visibility filtering changed durable summary")
	}
}

func TestBranchReferenceReauthorizesOriginalMessagesAndCanBeRemoved(t *testing.T) {
	store := &branchReferenceStore{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}}
	service := &Service{sessions: store, messages: store}
	metadata := domaincopilot.SessionMetadata{PinnedContext: map[string]any{"branchReference": map[string]any{"kind": "session", "name": "Branch", "sessionId": "source", "messageIds": []string{"old"}}}}
	ctx := context.Background()
	envelope, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "owner"}, "branch", metadata, "continue")
	if err != nil || len(envelope.Evidence) != 1 || !strings.Contains(envelope.Evidence[0].Content, "frozen selected background") {
		t.Fatalf("branch evidence: %+v %v", envelope, err)
	}
	if _, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "other"}, "branch", metadata, "continue"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("cross-user branch: %v", err)
	}
	store.archived = true
	if _, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "owner"}, "branch", metadata, "continue"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("archived source reused: %v", err)
	}
	metadata.PinnedContext = nil
	if envelope, err := service.buildChatContext(ctx, domainidentity.Principal{UserID: "owner"}, "branch", metadata, "continue"); err != nil || envelope != nil {
		t.Fatalf("removed branch reused: %+v %v", envelope, err)
	}
}
