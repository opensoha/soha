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
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/ai/evaluations/runs/run/execute", strings.NewReader(`{"executorProfileId":"profile"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "runID", Value: "run"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})
	handler.executeRun(ctx)
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
	policy, err := memory.GetPolicy(t.Context(), "personal", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if policy.DefaultTTL != 30*24*time.Hour || !policy.ExplicitWriteOnly || !policy.Enabled {
		t.Fatalf("policy=%#v", policy)
	}
}
