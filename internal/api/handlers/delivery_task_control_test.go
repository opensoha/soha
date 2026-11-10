package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type taskControlActions struct {
	action, id, reason string
	err                error
}

func (s *taskControlActions) CancelExecutionTask(_ context.Context, _ domainidentity.Principal, id string, input domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error) {
	s.action, s.id, s.reason = "cancel", id, input.Reason
	return s.result()
}

func (s *taskControlActions) RetryExecutionTask(_ context.Context, _ domainidentity.Principal, id string, input domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error) {
	s.action, s.id, s.reason = "retry", id, input.Reason
	return s.result()
}

func (s *taskControlActions) result() (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{ID: s.id, Status: "queued", CallbackToken: "synthetic-callback", SecretLease: &domainsecret.LeaseGrant{Token: "synthetic-lease"}}, s.err
}

func callTaskControl(body string, actions *taskControlActions) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Params = gin.Params{{Key: "taskID", Value: "task-original"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	(&DeliveryHandler{actions: actions}).ControlExecutionTask(ctx)
	return response
}

func TestExecutionTaskControlStrictBoundary(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"empty", "", 400}, {"null", "null", 400}, {"array", "[]", 400},
		{"missing action", `{}`, 400}, {"unknown action", `{"action":"delete"}`, 400},
		{"unknown field", `{"action":"cancel","force":true}`, 400},
		{"null reason", `{"action":"cancel","reason":null}`, 400},
		{"trailing JSON", `{"action":"retry"}{}`, 400},
		{"oversized", `{"action":"retry","reason":"` + strings.Repeat("x", 32<<10) + `"}`, 413},
		{"invalid UTF-8", "{\"action\":\"cancel\",\"reason\":\"\xff\"}", 400},
		{"Unicode over limit", `{"action":"cancel","reason":"` + strings.Repeat("验", 2049) + `"}`, 400},
		{"cancel", `{"action":"cancel"}`, 202},
		{"Unicode limit", `{"action":"retry","reason":"` + strings.Repeat("验", 2048) + `"}`, 202},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := &taskControlActions{}
			response := callTaskControl(test.body, actions)
			if response.Code != test.status {
				t.Fatalf("status=%d, want %d", response.Code, test.status)
			}
			if test.status != 202 && actions.action != "" {
				t.Fatal("invalid request reached application action")
			}
			if test.status != 202 {
				return
			}
			if actions.id != "task-original" || strings.Contains(response.Body.String(), "synthetic-") {
				t.Fatal("task identity changed or runner credentials exposed")
			}
			var envelope struct {
				Data domaindelivery.ExecutionTask `json:"data"`
			}
			if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Data.Status != "queued" || envelope.Data.CallbackToken != "[redacted]" || envelope.Data.Payload == nil {
				t.Fatal("actual state or safe required task fields lost")
			}
			if actions.action == "retry" && actions.reason != strings.Repeat("验", 2048) {
				t.Fatal("Unicode reason changed")
			}
		})
	}
}

func TestExecutionTaskControlPreservesServiceDenial(t *testing.T) {
	actions := &taskControlActions{err: apperrors.ErrAccessDenied}
	response := callTaskControl(`{"action":"retry"}`, actions)
	if response.Code != http.StatusForbidden || actions.action != "retry" || strings.Contains(response.Body.String(), "synthetic-") {
		t.Fatal("service authorization error was not preserved")
	}
}
