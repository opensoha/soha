package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appaieval "github.com/opensoha/soha/internal/application/aieval"
	appknowledgegraph "github.com/opensoha/soha/internal/application/knowledgegraph"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	appmultiagent "github.com/opensoha/soha/internal/application/multiagent"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type advancedExecutorStub struct{}

type advancedKnowledgeAccessStub struct{ err error }

func (s advancedKnowledgeAccessStub) Search(context.Context, domainidentity.Principal, domainknowledge.SearchRequest) (domainknowledge.SearchResult, error) {
	return domainknowledge.SearchResult{}, s.err
}

func (s advancedKnowledgeAccessStub) GetBase(_ context.Context, _ domainidentity.Principal, id string) (domainknowledge.KnowledgeBase, error) {
	if s.err != nil {
		return domainknowledge.KnowledgeBase{}, s.err
	}
	return domainknowledge.KnowledgeBase{ID: id}, nil
}

func (advancedExecutorStub) Execute(_ context.Context, request appaieval.ExecutionRequest) (appaieval.ExecutionResult, error) {
	return appaieval.ExecutionResult{Output: appaieval.SampleOutput{SampleID: request.Sample.ID}, TraceRef: "trace:1"}, nil
}

func TestAdvancedHandlerChecksGraphBaseBeforePublishing(t *testing.T) {
	runs := appaieval.MustNewService(appaieval.NewMemoryStore())
	advanced, _ := appaieval.NewAdvancedService(runs, appaieval.NewAdvancedMemoryStore(), advancedExecutorStub{})
	memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
	graph, _ := appknowledgegraph.NewService(appknowledgegraph.NewMemoryStore())
	multi, _ := appmultiagent.NewService(appmultiagent.NewMemoryStore())
	if err := graph.PutRevision(t.Context(), appknowledgegraph.Revision{ID: "revision-1", KnowledgeBaseID: "base-1", SourceIndexRef: "index:1", ExtractorVer: "v1"}); err != nil {
		t.Fatal(err)
	}
	handler := NewAIAdvancedHandler(advanced, runs, memory, graph, multi, advancedKnowledgeAccessStub{}, evaluationAuthorizerStub{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/ai/knowledge-bases/base-2/graph-revisions/revision-1/publish", nil)
	ctx.Params = gin.Params{{Key: "baseID", Value: "base-2"}, {Key: "revisionID", Value: "revision-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})
	handler.publishGraphRevision(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	revision, err := graph.GetRevision(t.Context(), "revision-1")
	if err != nil || revision.Status != "verified" {
		t.Fatalf("revision=%#v error=%v", revision, err)
	}
}

func TestAdvancedHandlerFailsClosedOnGraphBaseACL(t *testing.T) {
	runs := appaieval.MustNewService(appaieval.NewMemoryStore())
	advanced, _ := appaieval.NewAdvancedService(runs, appaieval.NewAdvancedMemoryStore(), advancedExecutorStub{})
	memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
	graph, _ := appknowledgegraph.NewService(appknowledgegraph.NewMemoryStore())
	multi, _ := appmultiagent.NewService(appmultiagent.NewMemoryStore())
	handler := NewAIAdvancedHandler(advanced, runs, memory, graph, multi, advancedKnowledgeAccessStub{err: apperrors.ErrAccessDenied}, evaluationAuthorizerStub{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/ai/knowledge-bases/base-1/graph-revisions", nil)
	ctx.Params = gin.Params{{Key: "baseID", Value: "base-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})
	handler.listGraphRevisions(ctx)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func newAdvancedHandlerForTest(t *testing.T) (*AIAdvancedHandler, *appaieval.Service, *appaieval.AdvancedService, *appmemory.Service) {
	t.Helper()
	runs := appaieval.MustNewService(appaieval.NewMemoryStore())
	advanced, err := appaieval.NewAdvancedService(runs, appaieval.NewAdvancedMemoryStore(), advancedExecutorStub{})
	if err != nil {
		t.Fatal(err)
	}
	memory, err := appmemory.NewService(appmemory.NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	graph, _ := appknowledgegraph.NewService(appknowledgegraph.NewMemoryStore())
	multi, _ := appmultiagent.NewService(appmultiagent.NewMemoryStore())
	return NewAIAdvancedHandler(advanced, runs, memory, graph, multi, nil, evaluationAuthorizerStub{}), runs, advanced, memory
}

func TestAdvancedHandlerResolvesExecutorProfileID(t *testing.T) {
	handler, runs, advanced, _ := newAdvancedHandlerForTest(t)
	now := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	dataset := appaieval.Dataset{SchemaVersion: "opensoha.dev/evaluation-dataset/v1", ID: "dataset", Name: "Dataset", Version: "v1", Samples: []appaieval.DatasetSample{{ID: "sample", Input: "check"}}, CreatedAt: now}
	if err := runs.PutDataset(t.Context(), dataset); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.StartRun(t.Context(), appaieval.Run{ID: "run", DatasetID: "dataset", DatasetVersion: "v1", CandidateRefs: map[string]string{"routeId": "route"}}, now); err != nil {
		t.Fatal(err)
	}
	profile := appaieval.ExecutorProfile{ID: "profile", EnvironmentPolicy: "readonly", IsolationMode: "read-only", Timeout: time.Second}
	if err := advanced.PutExecutorProfile(t.Context(), profile); err != nil {
		t.Fatal(err)
	}
	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder(), deadline: time.Now()}
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/ai/evaluations/runs/run/execute", strings.NewReader(`{"executorProfileId":"profile"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "runID", Value: "run"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})
	handler.executeRun(ctx)
	if !rec.deadline.IsZero() {
		t.Fatal("long evaluation kept the ordinary HTTP write deadline")
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdvancedHandlerTranslatesMemoryPolicyForm(t *testing.T) {
	handler, _, _, memory := newAdvancedHandlerForTest(t)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/ai/memory/policies", strings.NewReader(`{"id":"personal","name":"Personal","consentMode":"explicit","ttlDays":30}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})
	handler.putMemoryPolicy(ctx)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	policy, err := memory.GetPolicy(t.Context(), appmemory.PrivatePolicyID("u-1", "chat"), "v1")
	if err != nil {
		t.Fatal(err)
	}
	if policy.DefaultTTL != 30*24*time.Hour || !policy.ExplicitWriteOnly || !policy.Enabled {
		t.Fatalf("policy=%#v", policy)
	}
}

type privateChatFeedbackStub struct{}

func (privateChatFeedbackStub) ChatFeedbackReference(_ context.Context, principal domainidentity.Principal, session, message string) (string, error) {
	if principal.UserID != "owner" || session != "session" || message != "message" {
		return "", apperrors.ErrAccessDenied
	}
	return "agent:owned", nil
}
func TestChatFeedbackKeepsOnlyVerifiedOwnerScopedReference(t *testing.T) {
	handler, _, advanced, _ := newAdvancedHandlerForTest(t)
	handler.WithChatFeedback(privateChatFeedbackStub{})
	body := `{"id":"ignored","traceRef":"forged","sessionId":"session","messageId":"message","disposition":"accepted","redactedInput":"private input","redactedOutput":"private reply"}`
	for _, owner := range []string{"other", "owner"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/ai/evaluations/feedback", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Set("principal", domainidentity.Principal{UserID: owner})
		handler.putFeedback(ctx)
		if owner == "other" && recorder.Code != http.StatusForbidden {
			t.Fatalf("foreign reference status: %d", recorder.Code)
		}
		if owner == "owner" && recorder.Code != http.StatusCreated {
			t.Fatalf("owned feedback: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	records, err := advanced.ListFeedback(t.Context())
	if err != nil || len(records) != 1 || records[0].TraceRef != "agent:owned" || records[0].RedactedInput != "" || records[0].RedactedOutput != "" || !strings.HasPrefix(records[0].ID, "chat-feedback:") {
		t.Fatalf("private feedback: %+v %v", records, err)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/ai/evaluations/feedback", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: "other"})
	handler.listFeedback(ctx)
	if strings.Contains(recorder.Body.String(), "agent:owned") {
		t.Fatal("another user saw private feedback")
	}
}

func TestMemoryHandlerPrivateConsentAndServerOwnedProvenance(t *testing.T) {
	h, _, _, memory := newAdvancedHandlerForTest(t)
	invoke := func(body string, handler func(*gin.Context)) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/ai/memory", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})
		handler(ctx)
		return rec
	}
	for _, body := range []string{`{"id":"hint","name":"Memory","consentMode":"automatic","ttlDays":30,"mode":"chat"}`, `{"id":"hint","name":"Memory","consentMode":"automatic","ttlDays":30}`} {
		if rec := invoke(body, h.putMemoryPolicy); rec.Code != http.StatusBadRequest {
			t.Fatalf("Chat/global auto accepted: %d %s", rec.Code, rec.Body.String())
		}
	}
	rec := invoke(`{"id":"hint","name":"Memory","consentMode":"automatic","ttlDays":30,"mode":"heya","readEnabled":false,"writeEnabled":true}`, h.putMemoryPolicy)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	p, err := memory.GetPolicy(t.Context(), appmemory.PrivatePolicyID("u-1", "heya"), "v1")
	if err != nil || p.OwnerID != "u-1" || p.ExplicitWriteOnly || p.ReadEnabled == nil || *p.ReadEnabled || !strings.Contains(rec.Body.String(), `"readEnabled":false`) {
		t.Fatalf("%+v %v", p, err)
	}
	rec = invoke(`{"id":"hint","name":"Memory","consentMode":"explicit","ttlDays":30,"mode":"chat"}`, h.putMemoryPolicy)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	policyID := appmemory.PrivatePolicyID("u-1", "chat")
	rec = invoke(`{"policyId":"`+policyID+`","policyVersion":"v1","record":{"id":"r","ownerType":"user","ownerId":"u-1","scopeHash":"sha256:forged","fact":"my preference","sourceType":"explicit_user","sourceHashes":{"forged":"bad"},"confidence":1,"policyVersion":"v1"}}`, h.putMemory)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	r, err := memory.GetRecord(t.Context(), "r")
	if err != nil || r.ScopeHash != appmemory.Scope("u-1", "chat") || len(r.SourceHashes) != 0 {
		t.Fatalf("forged provenance retained: %+v %v", r, err)
	}
	rec = invoke(`{"policyId":"`+policyID+`","policyVersion":"v1","record":{"id":"bad","ownerType":"user","ownerId":"u-1","fact":"RAG","sourceType":"curated_extractor","confidence":1,"policyVersion":"v1"}}`, h.putMemory)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("API impersonated extractor: %d %s", rec.Code, rec.Body.String())
	}
}
func TestMemoryHandlerOwnTombstonesAndPolicyPrivacy(t *testing.T) {
	h, _, _, memory := newAdvancedHandlerForTest(t)
	p := appmemory.Policy{ID: "hint", Version: "v1", Enabled: true, ExplicitWriteOnly: true, DefaultTTL: time.Hour, MaximumTTL: time.Hour}
	own, err := memory.SavePersonalPolicy(t.Context(), "u-1", p)
	if err != nil {
		t.Fatal(err)
	}
	memory.SavePersonalPolicy(t.Context(), "other", p)
	for _, owner := range []string{"u-1", "other"} {
		policy := own
		policy.OwnerID = owner
		r := appmemory.Record{ID: owner, OwnerType: "user", OwnerID: owner, ScopeHash: appmemory.Scope(owner, "chat"), Fact: "private", SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}
		if _, err := memory.PutRecord(t.Context(), r, policy); err != nil {
			t.Fatal(err)
		}
		memory.DeleteRecord(t.Context(), owner)
	}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/ai/memory?includeDeleted=true", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})
	h.listMemory(ctx)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"u-1"`) || strings.Contains(rec.Body.String(), `"ownerId":"other"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/ai/memory/policies", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})
	h.listMemoryPolicies(ctx)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), appmemory.PrivatePolicyID("other", "chat")) {
		t.Fatalf("foreign policy leak %s", rec.Body.String())
	}
}
