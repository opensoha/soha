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
)

type draftCreationStub struct {
	DeliveryDraftPlanService
	input *domaindelivery.DeliveryDraftInput
}

func (s *draftCreationStub) CreateDeliveryDraft(_ context.Context, _ domainidentity.Principal, input domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraft, error) {
	s.input = &input
	return domaindelivery.DeliveryDraft{ID: "draft-1"}, nil
}

func TestCreateDeliveryDraftPreservesIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, key string
		status    int
	}{
		{"legacy", "", http.StatusCreated},
		{"retry", "draft-retry-1", http.StatusCreated},
		{"maximum", strings.Repeat("k", 128), http.StatusCreated},
		{"short", "short", http.StatusBadRequest},
		{"long", strings.Repeat("k", 129), http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &draftCreationStub{}
			handler := &DeliveryHandler{drafts: service}
			router := gin.New()
			router.POST("/delivery/drafts", handler.CreateDeliveryDraft)
			body, err := json.Marshal(map[string]any{"idempotencyKey": tc.key, "applicationDraft": map[string]any{"name": "Demo", "key": "demo"}})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/delivery/drafts", strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if tc.status == http.StatusCreated {
				if service.input == nil || service.input.IdempotencyKey != tc.key {
					t.Fatalf("idempotency key lost: %+v", service.input)
				}
			} else if service.input != nil {
				t.Fatal("invalid key reached creation service")
			}
		})
	}
}
