package handlers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type personalEnvironmentHandlerService struct {
	called           bool
	actor            domainidentity.Principal
	action, template string
	deletionInput    sohaapi.PersonalEnvironmentDataDeletionRequest
}

func (s *personalEnvironmentHandlerService) ExportPersonalEnvironmentData(_ context.Context, actor domainidentity.Principal, input sohaapi.PersonalEnvironmentDataExportRequest, _ string, target io.Writer) (contractenv.ExportResult, error) {
	s.called, s.actor = true, actor
	data := []byte("test archive bytes")
	_, err := target.Write(data)
	return contractenv.ExportResult{OwnerID: input.AssistantID, SizeBytes: int64(len(data)), SHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(data)), ObservedAt: time.Now().UTC()}, err
}

func TestPersonalDataExportHandlerStagesOnlyAuthenticatedArchive(t *testing.T) {
	for _, body := range []string{`{"assistantId":"mine","environmentVersion":2}`, `null`, `{"assistantId":"mine","environmentVersion":0}`, `{"assistantId":"mine","environmentVersion":2,"ownerId":"other"}`, `{"assistantId":"mine","environmentVersion":2}{}`} {
		service := &personalEnvironmentHandlerService{}
		h := copilotPersonalEnvironmentHandler{service: service}
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/copilot/assistant/environment/export", strings.NewReader(body))
		c.Set("principal", domainidentity.Principal{UserID: "authenticated"})
		h.ExportPersonalEnvironmentData(c)
		if body != `{"assistantId":"mine","environmentVersion":2}` {
			if response.Code != http.StatusBadRequest || service.called {
				t.Fatal("invalid export reached the owner")
			}
			continue
		}
		if response.Code != 200 || response.Body.String() != "test archive bytes" || service.actor.UserID != "authenticated" || response.Header().Get("X-Assistant-ID") != "mine" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Length") != "18" {
			t.Fatal("export changed user binding, bytes or privacy headers")
		}
	}
}

func (s *personalEnvironmentHandlerService) ListPersonalEnvironmentTemplates(_ context.Context, actor domainidentity.Principal) ([]domain.PersonalEnvironmentTemplate, error) {
	s.called, s.actor = true, actor
	return []domain.PersonalEnvironmentTemplate{}, nil
}

func (s *personalEnvironmentHandlerService) GetPersonalEnvironment(_ context.Context, actor domainidentity.Principal, _ string) (domain.PersonalEnvironment, error) {
	s.called, s.actor = true, actor
	return domain.PersonalEnvironment{AssistantID: "mine", State: "unknown", Version: 1}, nil
}

func (s *personalEnvironmentHandlerService) ChangePersonalEnvironment(_ context.Context, actor domainidentity.Principal, action, template, _ string) (domain.PersonalEnvironment, error) {
	s.called, s.actor, s.action, s.template = true, actor, action, template
	return domain.PersonalEnvironment{AssistantID: "mine", State: "sleeping", Version: 2, DataInitialized: true}, nil
}

func (s *personalEnvironmentHandlerService) DeletePersonalEnvironmentData(_ context.Context, actor domainidentity.Principal, input sohaapi.PersonalEnvironmentDataDeletionRequest, _ string) (domain.PersonalEnvironment, error) {
	s.called, s.actor, s.deletionInput = true, actor, input
	return domain.PersonalEnvironment{AssistantID: "mine", State: "deleted", Version: 3, DataInitialized: true}, nil
}

func TestPersonalDataDeletionHandlerRejectsUnconfirmedBodies(t *testing.T) {
	valid := `{"assistantId":"mine","environmentVersion":2,"confirmationName":"Soha"}`
	for _, test := range []struct {
		name, body string
		valid      bool
	}{
		{"confirmed", valid, true},
		{"missing_name", `{"assistantId":"mine","environmentVersion":2}`, false},
		{"null", `null`, false},
		{"null_name", `{"assistantId":"mine","environmentVersion":2,"confirmationName":null}`, false},
		{"zero_version", `{"assistantId":"mine","environmentVersion":0,"confirmationName":"Soha"}`, false},
		{"caller_owner", strings.TrimSuffix(valid, "}") + `,"ownerId":"other"}`, false},
		{"trailing", valid + `{}`, false},
		{"oversized", strings.Repeat(" ", 4097) + valid, false},
		{"invalid_utf8", strings.Replace(valid, "Soha", string([]byte{0xff}), 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &personalEnvironmentHandlerService{}
			h := copilotPersonalEnvironmentHandler{service: service}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodDelete, "/copilot/assistant/environment", strings.NewReader(test.body))
			c.Set("principal", domainidentity.Principal{UserID: "authenticated"})
			h.DeletePersonalEnvironmentData(c)
			status := http.StatusBadRequest
			if test.valid {
				status = http.StatusOK
			}
			if response.Code != status || service.called != test.valid {
				t.Fatalf("status=%d called=%t", response.Code, service.called)
			}
			if test.valid && (service.actor.UserID != "authenticated" || service.deletionInput.AssistantID != "mine" || service.deletionInput.EnvironmentVersion != 2 || service.deletionInput.ConfirmationName != "Soha") {
				t.Fatal("deletion selection or server owner changed")
			}
		})
	}
}

func TestPersonalEnvironmentHandlerUsesAuthenticatedOwnerAndStrictAction(t *testing.T) {
	for _, test := range []struct {
		name, body string
		valid      bool
	}{
		{"wake", `{"action":"wake","templateId":"personal"}`, true},
		{"reconcile", `{"action":"reconcile"}`, true},
		{"sleep", `{"action":"sleep"}`, true},
		{"owner", `{"action":"wake","ownerId":"other"}`, false},
		{"node", `{"action":"wake","agentClusterId":"node"}`, false},
		{"quota", `{"action":"wake","quota":{"diskBytes":1}}`, false},
		{"command", `{"action":"execute"}`, false},
		{"missing", `{}`, false},
		{"sleep template", `{"action":"sleep","templateId":"personal"}`, false},
		{"empty template", `{"action":"wake","templateId":""}`, false},
		{"null template", `{"action":"wake","templateId":null}`, false},
		{"trailing", `{"action":"sleep"}{}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &personalEnvironmentHandlerService{}
			h := copilotPersonalEnvironmentHandler{service: service}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/copilot/assistant/environment", strings.NewReader(test.body))
			c.Set("principal", domainidentity.Principal{UserID: "authenticated"})
			h.ChangePersonalEnvironment(c)
			status := http.StatusBadRequest
			if test.valid {
				status = http.StatusOK
			}
			if response.Code != status || service.called != test.valid {
				t.Fatalf("status=%d called=%t", response.Code, service.called)
			}
			if test.valid && service.actor.UserID != "authenticated" {
				t.Fatal("caller chose owner")
			}
		})
	}
}
