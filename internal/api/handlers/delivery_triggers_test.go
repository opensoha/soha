package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
)

type triggerHandlerStub struct {
	DeliveryTriggerService
	body  string
	calls int
}

func (s *triggerHandlerStub) ReceiveWebhook(_ context.Context, id, eventID, timestamp, signature string, body []byte) (domain.Event, error) {
	s.body, s.calls = string(body), s.calls+1
	return domain.Event{ID: id, EventID: eventID}, nil
}

func TestDeliveryWebhookPreservesSignedBytesAndRejectsAmbiguousHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body         string
		duplicate, missing bool
		status             int
	}{
		{name: "raw bytes", body: "{ \"project\": {\"id\": 42} }\n", status: 202},
		{name: "duplicate credential", body: "{}", duplicate: true, status: 401},
		{name: "missing signature", body: "{}", missing: true, status: 401},
		{name: "body limit", body: strings.Repeat("x", (1<<20)+1), status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &triggerHandlerStub{}
			router := gin.New()
			router.POST("/triggers/:triggerID/webhook", NewDeliveryTriggerHandler(service).Webhook)
			request := httptest.NewRequest(http.MethodPost, "/triggers/trigger/webhook", strings.NewReader(tc.body))
			for key, value := range map[string]string{"webhook-id": "event", "webhook-timestamp": "123", "webhook-signature": "v1,signed"} {
				request.Header.Set(key, value)
			}
			if tc.duplicate {
				request.Header.Add("webhook-id", "other")
			}
			if tc.missing {
				request.Header.Del("webhook-signature")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status %d, want %d", response.Code, tc.status)
			}
			if tc.status == 202 {
				if service.calls != 1 || service.body != tc.body {
					t.Fatal("signed bytes changed")
				}
			} else if service.calls != 0 {
				t.Fatal("invalid request reached application")
			}
		})
	}
}

func TestDeliveryTriggerRejectsExecutionInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{"executionTokenId":"other"}`, `{"serviceAccountId":"admin"}`, `{"revision":10}`, `{"schedule":{"timeZone":"UTC","executeAs":"admin"}}`, `{} {}`, `{"name":"` + strings.Repeat("x", 64<<10) + `"}`} {
		router := gin.New()
		router.POST("/triggers", NewDeliveryTriggerHandler(&triggerHandlerStub{}).Save)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/triggers", strings.NewReader(body)))
		if response.Code != 400 {
			t.Fatalf("unexpected status %d", response.Code)
		}
	}
}
