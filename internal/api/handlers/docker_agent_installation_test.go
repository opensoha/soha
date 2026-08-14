package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestDockerHostAgentEnrollmentResponseIsNotCacheable(t *testing.T) {
	service := &stubDockerHostInstallationService{credentials: sohaapi.DockerHostAgentCredentials{
		HostID: "host-1", OperationID: "operation-1", AgentID: "host-1",
		AgentBearerToken: "agent-token-32-characters-minimum", RuntimeBearerToken: "runtime-token-32-characters-minimum", IssuedAt: time.Now().UTC(),
	}}
	handler := NewDockerHandlerWithServices(DockerServices{HostInstallation: service}, legacyRunnerKeyring(""))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = []gin.Param{{Key: "operationID", Value: "operation-1"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/docker/agent-installations/operation-1/enroll", strings.NewReader(`{"agentId":"host-1","enrollmentToken":"enrollment-token-32-characters-minimum"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.ExchangeHostAgentEnrollment(ctx)

	if recorder.Code != http.StatusCreated || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("enrollment response = %d, Cache-Control %q", recorder.Code, recorder.Header().Get("Cache-Control"))
	}
	if !strings.Contains(recorder.Body.String(), `"runtimeBearerToken":"runtime-token-32-characters-minimum"`) {
		t.Fatalf("enrollment response body = %s", recorder.Body.String())
	}
}

func TestDockerHostAgentEnrollmentReplayReturnsConflict(t *testing.T) {
	handler := NewDockerHandlerWithServices(DockerServices{
		HostInstallation: &stubDockerHostInstallationService{err: appdocker.ErrHostAgentEnrollmentConsumed},
	}, legacyRunnerKeyring(""))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = []gin.Param{{Key: "operationID", Value: "operation-1"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/docker/agent-installations/operation-1/enroll", strings.NewReader(`{"agentId":"host-1","enrollmentToken":"enrollment-token-32-characters-minimum"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.ExchangeHostAgentEnrollment(ctx)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("replayed enrollment status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

type stubDockerHostInstallationService struct {
	credentials sohaapi.DockerHostAgentCredentials
	err         error
}

func (s *stubDockerHostInstallationService) CreateHostAgentInstallation(context.Context, domainidentity.Principal, string) (domaindocker.HostAgentInstallation, error) {
	return domaindocker.HostAgentInstallation{}, s.err
}

func (s *stubDockerHostInstallationService) RenderHostAgentInstallation(context.Context, string) ([]byte, error) {
	return nil, s.err
}

func (s *stubDockerHostInstallationService) ExchangeHostAgentEnrollment(context.Context, string, sohaapi.DockerHostAgentEnrollmentRequest) (sohaapi.DockerHostAgentCredentials, error) {
	return s.credentials, s.err
}

func (s *stubDockerHostInstallationService) AuthenticateHostAgent(context.Context, string) (domaindocker.RunnerAuthorization, error) {
	return domaindocker.RunnerAuthorization{}, s.err
}
