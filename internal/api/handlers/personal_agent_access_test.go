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

type personalAccessHandlerFixture struct {
	CopilotPersonalAgentService
	called        bool
	actor         identity.Principal
	owner, status string
	version       int64
}

func (s *personalAccessHandlerFixture) DecidePersonalAgentAccess(_ context.Context, actor identity.Principal, owner, status string, version int64) (domain.PersonalAgentAccess, error) {
	s.called, s.actor, s.owner, s.status, s.version = true, actor, owner, status, version
	return domain.PersonalAgentAccess{UserID: owner, Status: status, Version: version + 1}, nil
}
func TestPersonalAgentDecisionUsesAuthenticatedActorAndStrictVersionInput(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"status":"approved","version":2}`, true},
		{`{"status":"approved","version":2,"userId":"foreign"}`, false},
		{`{"status":"approved","version":2}{}`, false},
	} {
		fixture := &personalAccessHandlerFixture{}
		h := copilotPersonalAgentHandler{service: fixture}
		out := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(out)
		c.Request = httptest.NewRequest(http.MethodPatch, "/copilot/assistant/access/requests/target", strings.NewReader(tc.body))
		c.Set("principal", identity.Principal{UserID: "authenticated-admin"})
		c.Params = gin.Params{{Key: "userID", Value: "target"}}
		h.DecidePersonalAgentAccess(c)
		if fixture.called != tc.valid {
			t.Fatalf("strict input accepted: %s %d", tc.body, out.Code)
		}
		if tc.valid && (out.Code != http.StatusOK || fixture.actor.UserID != "authenticated-admin" || fixture.owner != "target" || fixture.version != 2) {
			t.Fatalf("lost actor, path owner or version: %+v", fixture)
		}
		if !tc.valid && out.Code != http.StatusBadRequest {
			t.Fatalf("malformed body: %d", out.Code)
		}
	}
}
