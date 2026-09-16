package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type buildTriggerCapture struct {
	BuildService
	input domainbuild.TriggerInput
}

func (s *buildTriggerCapture) Trigger(_ context.Context, _ domainidentity.Principal, input domainbuild.TriggerInput) (domainbuild.Record, error) {
	s.input = input
	return domainbuild.Record{ID: "build-1", ApplicationID: input.ApplicationID, Status: "queued"}, nil
}

func TestTriggerBuildPreservesRepositoryRefsWithoutEnvironment(t *testing.T) {
	service := &buildTriggerCapture{}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/builds/trigger", strings.NewReader(`{"applicationId":"app-1","buildSourceId":"build-1","refName":"main","repositoryRefs":[{"repositoryId":"repo-1","refType":"tag","refName":"v1"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	NewBuildHandler(service).TriggerBuild(c)
	if response.Code != http.StatusAccepted || service.input.ApplicationEnvironmentID != "" || len(service.input.RepositoryRefs) != 1 || service.input.RepositoryRefs[0].RefName != "v1" {
		t.Fatalf("status = %d, input = %#v", response.Code, service.input)
	}
}
