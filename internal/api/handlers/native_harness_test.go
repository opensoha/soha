package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativeHandlerStub struct {
	CopilotAgentRunService
	calls     int
	principal domainidentity.Principal
	runID     string
	cursor    int64
	input     string
	err       error
}

func (s *nativeHandlerStub) ListAgentRunEvents(_ context.Context, p domainidentity.Principal, id string, cursor int64) (domaincopilot.NativeEventPage, error) {
	s.calls++
	s.principal, s.runID, s.cursor = p, id, cursor
	return domaincopilot.NativeEventPage{Items: []domaincopilot.WorkbenchStreamEvent{}, NextSequence: 7, ResyncRequired: cursor > 7}, s.err
}

func (s *nativeHandlerStub) ResumeAgentRun(_ context.Context, p domainidentity.Principal, id, input string) (domaincopilot.AgentRun, error) {
	s.calls++
	s.principal, s.runID, s.input = p, id, input
	return domaincopilot.AgentRun{ID: id, Status: "running"}, s.err
}

func (s *nativeHandlerStub) FindNativeRequest(_ context.Context, p domainidentity.Principal, id, key string) (domaincopilot.AgentRun, error) {
	s.calls++
	s.principal, s.runID, s.input = p, id, key
	return domaincopilot.AgentRun{ID: "original-run", SessionID: id, Status: "running"}, s.err
}

func nativeHandlerRequest(stub *nativeHandlerStub, method, path, body string) *httptest.ResponseRecorder {
	router := gin.New()
	handler := &copilotAgentRunHandler{service: stub}
	router.Use(func(c *gin.Context) { c.Set("principal", domainidentity.Principal{UserID: "owner"}) })
	router.GET("/runs/:runID/events", handler.ListAgentRunEvents)
	router.POST("/runs/:runID/resume", handler.ResumeAgentRun)
	router.GET("/sessions/:sessionID/native-request", handler.FindNativeRequest)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestNativeRequestRecoveryIsReadOnlyAndPreservesOpaqueKey(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{nil, 200}, {apperrors.ErrNotFound, 404}, {apperrors.ErrAccessDenied, 403}} {
		stub := &nativeHandlerStub{err: test.err}
		response := nativeHandlerRequest(stub, http.MethodGet, "/sessions/owned/native-request?requestId=opaque%2Bkey%2Fone", "")
		if response.Code != test.status || stub.calls != 1 || stub.principal.UserID != "owner" || stub.runID != "owned" || stub.input != "opaque+key/one" {
			t.Fatalf("request lookup lost ownership/key: status=%d stub=%+v", response.Code, stub)
		}
	}
}

func TestNativeEventHandlerEnvelopeAndCursor(t *testing.T) {
	for _, test := range []struct {
		query  string
		cursor int64
		status int
	}{
		{"", 0, 200}, {"?afterSequence=6", 6, 200}, {"?afterSequence=8", 8, 200},
		{"?afterSequence=-1", 0, 400}, {"?afterSequence=bad", 0, 400}, {"?afterSequence=9223372036854775808", 0, 400},
	} {
		t.Run(test.query, func(t *testing.T) {
			stub := &nativeHandlerStub{}
			response := nativeHandlerRequest(stub, http.MethodGet, "/runs/run-1/events"+test.query, "")
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.status != 200 {
				if stub.calls != 0 {
					t.Fatal("invalid cursor reached service")
				}
				return
			}
			var envelope struct {
				Data domaincopilot.NativeEventPage `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Data.Items == nil || envelope.Data.NextSequence != 7 || envelope.Data.HasMore || envelope.Data.ResyncRequired != (test.cursor > 7) {
				t.Fatalf("page=%+v", envelope.Data)
			}
			if stub.principal.UserID != "owner" || stub.runID != "run-1" || stub.cursor != test.cursor {
				t.Fatalf("service parameters=%+v", stub)
			}
		})
	}
}

func TestNativeResumeHandlerBoundsAndOwnership(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		err        error
	}{
		{"bounded_unicode", `{"input":"` + strings.Repeat("中", 16384) + `"}`, 200, nil},
		{"too_long", `{"input":"` + strings.Repeat("中", 16385) + `"}`, 400, nil},
		{"unknown_field", `{"input":"ok","unexpected":true}`, 400, nil},
		{"null", `null`, 400, nil},
		{"trailing", `{} {}`, 400, nil},
		{"wrong_type", `{"input":1}`, 400, nil},
		{"unowned", `{"input":"ok"}`, 404, apperrors.ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &nativeHandlerStub{err: test.err}
			response := nativeHandlerRequest(stub, http.MethodPost, "/runs/run-1/resume", test.body)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.status == 400 && stub.calls != 0 {
				t.Fatal("invalid body reached service")
			}
			if stub.calls > 0 && (stub.principal.UserID != "owner" || stub.runID != "run-1") {
				t.Fatal("ownership context lost")
			}
			if test.status == 200 && (!strings.Contains(response.Body.String(), `"data":`) || len([]rune(stub.input)) != 16384) {
				t.Fatal("resume envelope or input lost")
			}
		})
	}
}
