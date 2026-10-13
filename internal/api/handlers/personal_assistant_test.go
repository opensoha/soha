package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type avatarHandlerService struct {
	CopilotPersonalAssistantService
	called bool
	actor  identity.Principal
	input  domain.PersonalAssistantAvatar
}

func (s *avatarHandlerService) UpdatePersonalAssistantAvatar(_ context.Context, actor identity.Principal, input domain.PersonalAssistantAvatar, _ string) (domain.PersonalAssistant, error) {
	s.called, s.actor, s.input = true, actor, input
	return domain.PersonalAssistant{ID: "own-assistant", Name: "Soha"}, nil
}

func TestPersonalAvatarHandlerStrictOwnerInput(t *testing.T) {
	for _, item := range []struct {
		name, body string
		valid      bool
	}{
		{"reset", `{"avatarUrl":"","avatarMotion":"off"}`, true},
		{"missing", `{}`, false}, {"null", `{"avatarUrl":null}`, false},
		{"owner", `{"avatarUrl":"","ownerId":"other"}`, false},
		{"trailing", `{"avatarUrl":""}{}`, false},
		{"oversize", `{"avatarUrl":"` + strings.Repeat("a", 900000) + `"}`, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			service := &avatarHandlerService{}
			h := copilotPersonalAssistantHandler{service: service}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPatch, "/copilot/assistant/avatar", strings.NewReader(item.body))
			c.Set("principal", identity.Principal{UserID: "authenticated"})
			h.UpdatePersonalAssistantAvatar(c)
			status := http.StatusBadRequest
			if item.valid {
				status = http.StatusOK
			}
			if response.Code != status || service.called != item.valid {
				t.Fatalf("status=%d called=%t", response.Code, service.called)
			}
			if item.valid && (service.actor.UserID != "authenticated" || service.input.AvatarMotion != "off") {
				t.Fatal("lost owner or preference")
			}
		})
	}
}
