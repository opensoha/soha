package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type originalHealingStub struct {
	HealingRunService
	event, policy, key string
}

func (s *originalHealingStub) CreateOriginalHealingRequest(_ context.Context, _ domainidentity.Principal, _ domainidentity.AccessContext, event, policy, key string) (domainalert.HealingOriginalReceipt, error) {
	s.event, s.policy, s.key = event, policy, key
	return healingHandlerReceipt(event, key), nil
}
func (s *originalHealingStub) GetOriginalHealingRequest(_ context.Context, _ domainidentity.Principal, _ domainidentity.AccessContext, event, key string) (domainalert.HealingOriginalReceipt, error) {
	s.event, s.key = event, key
	return healingHandlerReceipt(event, key), nil
}
func healingHandlerReceipt(event, key string) domainalert.HealingOriginalReceipt {
	now := time.Now().UTC()
	return domainalert.HealingOriginalReceipt{Protocol: "healing-request-v1", IdempotencyKey: key, Run: domainalert.HealingOriginalRun{ID: "healing-run:owned", EventID: event, PolicyID: "policy:owned", Status: "pending_approval", ApprovalStatus: "pending", SubmissionStatus: "not_submitted", ExecutionGeneration: 1, CreatedAt: now, UpdatedAt: now}}
}
func TestHealingOriginalHandlerUsesClosedProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key := "51a2f86e-d1c0-472a-a843-b7b13c0043c9"
	for _, create := range []bool{false, true} {
		stub := &originalHealingStub{}
		handler := healingRunHandler{service: stub}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Params = gin.Params{{Key: "eventID", Value: "event:owned"}, {Key: "requestID", Value: key}}
		c.Request = httptest.NewRequest(http.MethodPost, "/original", strings.NewReader(`{"idempotencyKey":"`+key+`","policyId":"policy:owned"}`))
		status := 200
		if create {
			handler.CreateOriginalHealingRequest(c)
			status = 201
		} else {
			handler.GetOriginalHealingRequest(c)
		}
		if recorder.Code != status || stub.event != "event:owned" || stub.key != key || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status=%d body=%s coordinates=%+v", recorder.Code, recorder.Body, stub)
		}
		var result map[string]any
		if json.Unmarshal(recorder.Body.Bytes(), &result) != nil {
			t.Fatal("invalid JSON")
		}
		if strings.Contains(recorder.Body.String(), "credential") || strings.Contains(recorder.Body.String(), "result") {
			t.Fatal("private fields projected")
		}
	}
}
func TestHealingOriginalInputRejectsAmbiguousOrExtraFields(t *testing.T) {
	valid := `{"idempotencyKey":"51a2f86e-d1c0-472a-a843-b7b13c0043c9","policyId":"policy:owned"}`
	for _, body := range []string{`null`, `{}`, `[]`, valid + `{}`, strings.Replace(valid, `"policyId":`, `"PolicyId":`, 1), strings.Replace(valid, `"policyId":`, `"extra":true,"policyId":`, 1), strings.Replace(valid, `"policyId":`, `"policyId":"other","policyId":`, 1), strings.Replace(valid, `"policy:owned"`, `null`, 1), strings.Repeat(" ", 4097) + valid} {
		if _, err := decodeOriginalHealingInput(strings.NewReader(body)); err == nil {
			t.Fatalf("unsafe body accepted=%s", body[:min(len(body), 100)])
		}
	}
	if _, err := decodeOriginalHealingInput(strings.NewReader(valid)); err != nil {
		t.Fatal(err)
	}
}
