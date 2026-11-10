package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	"github.com/opensoha/soha/internal/domain/identity"
)

type dlpSessionWorkflow struct {
	ContentDLPWorkflowService
	session string
}

func (s *dlpSessionWorkflow) Authorize(_ context.Context, _ identity.Principal, _, _ string, in dlp.AuthorizationInput) (dlp.Authorization, error) {
	s.session = in.SessionID
	return dlp.Authorization{Token: strings.Repeat("a", 43)}, nil
}

type managedSessionWorkflow struct {
	ManagedContentWorkflowService
	session string
}

func (s *managedSessionWorkflow) Authorize(_ context.Context, _ identity.Principal, _, _, session string) (dlp.Authorization, error) {
	s.session = session
	return dlp.Authorization{Token: strings.Repeat("a", 43)}, nil
}

func TestDLPReleaseSessionComesFromAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{"docker", "managed"} {
		for _, body := range []string{`{"action":"acknowledge"}`, `{"action":"acknowledge","sessionId":"forged"}`} {
			t.Run(kind+body, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest("POST", "/authorize", strings.NewReader(body))
				c.Set("access_context", identity.AccessContext{SessionID: "authenticated-session"})
				var captured string
				if kind == "docker" {
					service := &dlpSessionWorkflow{}
					NewContentDLPWorkflowHandler(service).Authorize(c)
					captured = service.session
				} else {
					service := &managedSessionWorkflow{}
					NewManagedContentWorkflowHandler(service).Authorize(c)
					captured = service.session
				}
				if strings.Contains(body, "forged") {
					if recorder.Code != 400 || captured != "" {
						t.Fatal("self-reported authentication context accepted", recorder.Code, captured)
					}
				} else if recorder.Code != 200 || captured != "authenticated-session" {
					t.Fatal("authenticated session was lost", recorder.Code, captured)
				}
			})
		}
	}
}
