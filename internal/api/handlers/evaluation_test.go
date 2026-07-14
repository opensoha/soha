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
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type evaluationAuthorizerStub struct {
	err                 error
	keys                *[]string
	resolvedPermissions []string
}

func (s evaluationAuthorizerStub) PermissionKeys(_ context.Context, _ domainidentity.Principal) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]string(nil), s.resolvedPermissions...), nil
}

func (s evaluationAuthorizerStub) Authorize(_ context.Context, _ domainidentity.Principal, key string) error {
	if s.keys != nil {
		*s.keys = append(*s.keys, key)
	}
	return s.err
}

func (s evaluationAuthorizerStub) AuthorizeAny(_ context.Context, _ domainidentity.Principal, keys ...string) error {
	if s.keys != nil {
		*s.keys = append(*s.keys, keys...)
	}
	return s.err
}

func TestEvaluationHandlerCreatesContractDatasetAndStartsRun(t *testing.T) {
	service := appaieval.MustNewService(appaieval.NewMemoryStore())
	handler := NewEvaluationHandler(service, evaluationAuthorizerStub{})
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return now }

	datasetRecorder := httptest.NewRecorder()
	datasetContext, _ := gin.CreateTestContext(datasetRecorder)
	datasetContext.Request = httptest.NewRequest(http.MethodPost, "/ai/evaluations/datasets", strings.NewReader(`{"schemaVersion":"opensoha.dev/evaluation-dataset/v1","id":"rag-regression","name":"RAG regression","version":"v1","samples":[{"id":"s1","input":"What failed?"}],"createdAt":"2026-07-14T08:00:00Z"}`))
	datasetContext.Request.Header.Set("Content-Type", "application/json")
	datasetContext.Set("principal", domainidentity.Principal{UserID: "u-1"})
	handler.createDataset(datasetContext)
	if datasetRecorder.Code != http.StatusCreated {
		t.Fatalf("create dataset status = %d, body=%s", datasetRecorder.Code, datasetRecorder.Body.String())
	}

	runRecorder := httptest.NewRecorder()
	runContext, _ := gin.CreateTestContext(runRecorder)
	runContext.Request = httptest.NewRequest(http.MethodPost, "/ai/evaluations/runs", strings.NewReader(`{"schemaVersion":"opensoha.dev/evaluation-run/v1","id":"eval-1","datasetId":"rag-regression","datasetVersion":"v1","candidateRefs":{"prompt":"prompt:v2"},"status":"running","startedAt":"2026-07-14T08:00:00Z"}`))
	runContext.Request.Header.Set("Content-Type", "application/json")
	runContext.Set("principal", domainidentity.Principal{UserID: "u-1"})
	handler.startRun(runContext)
	if runRecorder.Code != http.StatusAccepted {
		t.Fatalf("start run status = %d, body=%s", runRecorder.Code, runRecorder.Body.String())
	}
}

func TestEvaluationHandlerFailsClosedWhenAuthorizationDenied(t *testing.T) {
	handler := NewEvaluationHandler(appaieval.MustNewService(appaieval.NewMemoryStore()), evaluationAuthorizerStub{err: apperrors.ErrAccessDenied})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/ai/evaluations/datasets", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.listDatasets(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestEvaluationHandlerUsesDedicatedPermissions(t *testing.T) {
	keys := []string{}
	handler := NewEvaluationHandler(
		appaieval.MustNewService(appaieval.NewMemoryStore()),
		evaluationAuthorizerStub{keys: &keys},
	)

	readRecorder := httptest.NewRecorder()
	readContext, _ := gin.CreateTestContext(readRecorder)
	readContext.Request = httptest.NewRequest(http.MethodGet, "/ai/evaluations/datasets", nil)
	handler.listDatasets(readContext)

	writeRecorder := httptest.NewRecorder()
	writeContext, _ := gin.CreateTestContext(writeRecorder)
	writeContext.Request = httptest.NewRequest(http.MethodPost, "/ai/evaluations/datasets", strings.NewReader(`{}`))
	writeContext.Request.Header.Set("Content-Type", "application/json")
	handler.createDataset(writeContext)

	if len(keys) != 3 || keys[0] != "ai.evaluations.view" || keys[1] != "ai.evaluations.manage" || keys[2] != "ai.evaluations.manage" {
		t.Fatalf("authorization keys = %v", keys)
	}
}
