package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type runtimeProfilePublishTestService struct {
	CopilotRuntimeProfileService
	called   bool
	expected int64
	profile  domain.RuntimeProfile
}

func (s *runtimeProfilePublishTestService) PublishRuntimeProfile(_ context.Context, _ identity.Principal, expected int64, profile domain.RuntimeProfile) (domain.RuntimeProfile, error) {
	s.called, s.expected, s.profile = true, expected, profile
	return domain.RuntimeProfile{}, apperrors.ErrConflict
}

func TestPublishRuntimeProfileRequiresExplicitCASRevision(t *testing.T) {
	const path = "/settings/ai/workbench-runtime-profiles/chat-default"
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"absent", `{"name":"Default","scope":"chat","enabled":false,"assets":[]}`, http.StatusBadRequest},
		{"create conflict", `{"expectedRevision":0,"name":"Default","scope":"chat","enabled":false,"assets":[]}`, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &runtimeProfilePublishTestService{}
			handler := &copilotRuntimeProfileHandler{service: svc}
			router := gin.New()
			router.PUT("/settings/ai/workbench-runtime-profiles/:profileId", handler.PublishRuntimeProfile)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodPut, path, strings.NewReader(tc.body)))
			if res.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", res.Code, tc.status, res.Body.String())
			}
			if svc.called != (tc.status == http.StatusConflict) || svc.called && (svc.expected != 0 || svc.profile.ID != "chat-default" || svc.profile.Enabled) {
				t.Fatalf("publish binding: %+v", svc)
			}
		})
	}
}
