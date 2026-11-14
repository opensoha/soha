package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type artifactsContractService struct {
	CopilotPersonalAssistantService
	items []domain.PersonalArtifact
	err   error
	actor string
}

func (service *artifactsContractService) ListPersonalAssistantArtifacts(_ context.Context, actor identity.Principal) ([]domain.PersonalArtifact, error) {
	service.actor = actor.UserID
	return service.items, service.err
}

func TestPersonalArtifactsPreserveLegacyAndPublishedEnvelopes(t *testing.T) {
	for _, name := range []string{"nil", "empty", "saved", "forbidden"} {
		t.Run(name, func(t *testing.T) {
			service := &artifactsContractService{}
			if name == "empty" {
				service.items = []domain.PersonalArtifact{}
			}
			if name == "saved" {
				service.items = []domain.PersonalArtifact{{ID: "artifact-1", SessionID: "session-1", CreatedAt: time.Date(2026, 10, 25, 4, 0, 0, 0, time.UTC),
					Artifact: domain.AnalysisArtifact{Kind: "report", RunID: "run-1", Summary: "Saved report"}, SourceMetadata: map[string]any{"apiToken": "PRIVATE_LINEAGE"}}}
			}
			if name == "forbidden" {
				service.err = apperrors.ErrAccessDenied
			}
			handler := copilotPersonalAssistantHandler{service: service}
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/copilot/assistant/artifacts", nil)
			ctx.Set("principal", identity.Principal{UserID: "authenticated-owner"})
			handler.ListPersonalAssistantArtifacts(ctx)
			if service.actor != "authenticated-owner" {
				t.Fatal("handler lost the authenticated actor")
			}
			if name == "forbidden" {
				if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), `"data"`) || strings.Contains(response.Body.String(), `"items"`) {
					t.Fatal("denial returned artifact data")
				}
				return
			}
			if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "PRIVATE_LINEAGE") {
				t.Fatal("invalid or unredacted artifact response")
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(raw["data"], raw["items"]) || string(raw["data"]) == "null" {
				t.Fatal("canonical and legacy arrays differ or became null")
			}
			var published sohaapi.PersonalAssistantArtifactListEnvelope
			var legacy struct {
				Items []sohaapi.PersonalAssistantArtifact `json:"items"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &published); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(response.Body.Bytes(), &legacy); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(published.Data, legacy.Items) || len(published.Data) != len(service.items) {
				t.Fatal("published and legacy clients read different artifacts")
			}
		})
	}
}
