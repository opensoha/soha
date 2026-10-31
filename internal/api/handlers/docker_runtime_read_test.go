package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type fakeDockerVersionedReader struct {
	calls   int
	input   appdocker.RuntimeReadInput
	project string
}

func (f *fakeDockerVersionedReader) ReadProjectRuntime(_ context.Context, _ domainidentity.Principal, project string, input appdocker.RuntimeReadInput) (protocol.ReadResult, error) {
	f.calls++
	f.input = input
	f.project = project
	return protocol.ReadResult{}, nil
}

func TestDockerVersionedReadHandlerRejectsAmbiguousOrForeignQueryControls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, route, query, body string
		status                   int
	}{
		{"logs", "logs", "serviceName=web&tailLines=20&sinceSeconds=3", "", 200},
		{"files", "files", "serviceName=web&target=%2Fdata&path=notes&limit=10", "", 200},
		{"file", "file", "serviceName=web&target=%2Fdata&path=note.txt&limitBytes=100", "", 200},
		{"volumes", "volumes", "serviceName=web", "", 200},
		{"missing service", "logs", "", "", 400},
		{"duplicate", "logs", "serviceName=web&serviceName=other", "", 400},
		{"caller CID", "logs", "serviceName=web&containerId=other", "", 400},
		{"caller UID", "volumes", "serviceName=web&hostUid=other", "", 400},
		{"other action flag", "volumes", "serviceName=web&limitBytes=2", "", 400},
		{"invalid integer", "files", "serviceName=web&target=%2Fdata&limit=wrong", "", 400},
		{"excess limit", "file", "serviceName=web&target=%2Fdata&limitBytes=1048577", "", 400},
		{"bad escape", "logs", "serviceName=web%ff&tailLines=%ZZ", "", 400},
		{"GET body", "file", "serviceName=web&target=%2Fdata", "{}", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakeDockerVersionedReader{}
			handler := NewDockerHandlerWithServices(DockerServices{VersionedRuntime: service}, keyring.Ring{})
			router := gin.New()
			router.GET("/projects/:id/logs", handler.GetRuntimeLogsV1)
			router.GET("/projects/:id/volumes", handler.GetRuntimeVolumesV1)
			router.GET("/projects/:id/files", handler.ListRuntimeFilesV1)
			router.GET("/projects/:id/file", handler.ReadRuntimeFileV1)
			request := httptest.NewRequest(http.MethodGet, "/projects/project-1/"+tc.route+"?"+tc.query, strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			wantCalls := 0
			if tc.status == 200 {
				wantCalls = 1
			}
			if recorder.Code != tc.status || service.calls != wantCalls {
				t.Fatalf("status=%d calls=%d want=%d/%d", recorder.Code, service.calls, tc.status, wantCalls)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("identity-bound runtime responses must not be cached")
			}
			if wantCalls == 1 && (service.project != "project-1" || service.input.ServiceName != "web") {
				t.Fatal("wrong resolved request", service.project, service.input)
			}
		})
	}
}

func TestDockerVersionedReadMissingAndTypedNilServiceRemainUnsupported(t *testing.T) {
	for _, service := range []DockerVersionedRuntimeService{nil, (*fakeDockerVersionedReader)(nil)} {
		handler := NewDockerHandlerWithServices(DockerServices{VersionedRuntime: service}, keyring.Ring{})
		router := gin.New()
		router.GET("/projects/:id/volumes", handler.GetRuntimeVolumesV1)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/projects/project-1/volumes?serviceName=web", nil))
		if recorder.Code != http.StatusNotImplemented {
			t.Fatal("missing versioned capability was not explicit", recorder.Code)
		}
	}
}
