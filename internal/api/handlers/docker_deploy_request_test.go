package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type originalDeployHandlerService struct {
	actor, id, key, action string
	err                    error
}

func (s *originalDeployHandlerService) GetProjectDeploymentRequest(_ context.Context, actor domainidentity.Principal, id, key, action string) (domain.Operation, error) {
	s.actor, s.id, s.key, s.action = actor.UserID, id, key, action
	return domain.Operation{ID: "original-operation"}, s.err
}

func TestOriginalDockerDeploymentHandlerBoundary(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{nil, 200}, {apperrors.ErrAccessDenied, 403}, {apperrors.ErrConflict, 409}, {apperrors.ErrNotFound, 404}} {
		service := &originalDeployHandlerService{err: test.err}
		handler := NewDockerHandlerWithServices(DockerServices{DeploymentRequests: service}, keyring.Ring{})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/original?action=restart", nil)
		c.Params = gin.Params{{Key: "id", Value: "project-owned"}, {Key: "requestKey", Value: "original-key"}}
		c.Set("principal", domainidentity.Principal{UserID: "actor"})
		handler.GetProjectDeploymentRequest(c)
		if w.Code != test.status || service.actor != "actor" || service.id != "project-owned" || service.key != "original-key" || service.action != "restart" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status=%d invocation=%+v", w.Code, service)
		}
	}
	var typedNil *originalDeployHandlerService
	for _, service := range []DockerDeploymentRequests{nil, typedNil} {
		handler := NewDockerHandlerWithServices(DockerServices{DeploymentRequests: service}, keyring.Ring{})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/original", nil)
		handler.GetProjectDeploymentRequest(c)
		if w.Code != 501 {
			t.Fatalf("missing dependency accepted: %d", w.Code)
		}
	}
}

func TestBoundDockerDeployInputPreservesExactPreconditions(t *testing.T) {
	condition := sohaapi.DockerConfigurationPreconditions{ExpectedUID: uuid.New(), ExpectedConfigurationRevision: uuid.New()}
	bound := sohaapi.DockerProjectDeployPreconditions{HostID: "host-owned", Host: condition, Project: condition}
	body, err := json.Marshal(sohaapi.DockerProjectDeployInput{Action: "restart", Preconditions: &bound})
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range []bool{false, true} {
		service := &dockerDeployInputService{}
		handler := &DockerHandler{projects: service, planning: service}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/deploy", strings.NewReader(string(body)))
		c.Request.Header.Set("Idempotency-Key", "original-key")
		if plan {
			handler.PlanProjectDeploy(c)
		} else {
			handler.DeployProject(c)
		}
		if service.calls != 1 || service.input.Preconditions == nil || *service.input.Preconditions != bound {
			t.Fatalf("binding lost at HTTP boundary: %s", w.Body.String())
		}
	}
	for _, invalid := range []string{
		`{"preconditions":null}`, `{"preconditions":{}}`,
		strings.Replace(string(body), `"hostId"`, `"HostID"`, 1),
		strings.Replace(string(body), `"expectedUid"`, `"ExpectedUID"`, 1),
		strings.Replace(string(body), `"hostId":"host-owned"`, `"hostId":"host-owned","hostId":"other-host"`, 1),
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/deploy", strings.NewReader(invalid))
		if _, accepted := decodeDockerProjectDeploy(c); accepted || w.Code != 400 {
			t.Fatalf("ambiguous binding accepted: %s", invalid)
		}
	}
}
