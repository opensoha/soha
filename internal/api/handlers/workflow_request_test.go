package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type workflowRequestServiceStub struct {
	WorkflowService
	calls      int
	actor, key string
	input      domainworkflow.Input
}

func (s *workflowRequestServiceStub) SubmitWorkflowRequest(_ context.Context, p domainidentity.Principal, key string, input domainworkflow.Input) (domainworkflow.TriggerReceipt, error) {
	s.calls++
	s.actor, s.key, s.input = p.UserID, key, input
	return domainworkflow.TriggerReceipt{ID: "request-1", RequestKey: key, RunID: "workflow:original", SubmissionStatus: "unknown"}, nil
}
func (s *workflowRequestServiceStub) GetWorkflowRequest(_ context.Context, p domainidentity.Principal, key string) (domainworkflow.TriggerReceipt, error) {
	s.calls++
	s.actor, s.key = p.UserID, key
	return domainworkflow.TriggerReceipt{ID: "request-1", RequestKey: key, RunID: "workflow:original", SubmissionStatus: "unknown"}, nil
}

func workflowRequestTestRouter(service *workflowRequestServiceStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("principal", domainidentity.Principal{UserID: "actor-from-middleware"}) })
	h := NewWorkflowHandler(service)
	router.POST("/workflow-requests", h.SubmitRequest)
	router.GET("/workflow-requests/:requestKey", h.GetRequest)
	return router
}

func TestWorkflowRequestHTTPBindsOriginalActorAndExactParameterValues(t *testing.T) {
	service := &workflowRequestServiceStub{}
	router := workflowRequestTestRouter(service)
	body := `{"idempotencyKey":"original-request-1","workflow":{"applicationId":"app-1","triggerBuild":true,"variables":{"counter":9007199254740993},"buildArgs":{"OPT":"safe"}}}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/workflow-requests", strings.NewReader(body)))
	if w.Code != 202 || service.calls != 1 || service.actor != "actor-from-middleware" || service.key != "original-request-1" || service.input.RequestedRunID != "" || service.input.Variables["counter"] != json.Number("9007199254740993") || service.input.BuildArgs["OPT"] != "safe" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("bad original request mapping/status: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/workflow-requests/original-request-1", nil))
	if w.Code != 200 || service.calls != 2 || service.key != "original-request-1" {
		t.Fatal("read did not use original key")
	}
}

func TestWorkflowRequestHTTPRejectsInvalidInputBeforeService(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `[]`, `{"idempotencyKey":"original-request-1","workflow":null}`, `{"idempotencyKey":"original-request-1","workflow":{"applicationId":"app-1","requestedRunId":"forged"}}`, `{"idempotencyKey":"original-request-1","workflow":{"applicationId":"app-1"},"actorId":"forged"}`, `{"idempotencyKey":"original-request-1","workflow":{"applicationId":"app-1"}} {}`, "{\"x\":\"" + string([]byte{255}) + "\"}", strings.Repeat(" ", 2<<20) + "{}"} {
		t.Run(body[:min(len(body), 90)], func(t *testing.T) {
			service := &workflowRequestServiceStub{}
			w := httptest.NewRecorder()
			workflowRequestTestRouter(service).ServeHTTP(w, httptest.NewRequest("POST", "/workflow-requests", strings.NewReader(body)))
			if w.Code != 400 || service.calls != 0 {
				t.Fatalf("invalid body reached service: %d", w.Code)
			}
		})
	}
}
