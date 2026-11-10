package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	di "github.com/opensoha/soha/internal/domain/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type targetIdentityEnvironmentStub struct {
	ApplicationEnvironmentService
	input dc.ApplicationEnvironmentInput
	calls int
}

func (s *targetIdentityEnvironmentStub) UpdateApplicationEnvironment(_ context.Context, _ di.Principal, _ string, input dc.ApplicationEnvironmentInput) (dc.ApplicationEnvironment, error) {
	s.input = input
	s.calls++
	return dc.ApplicationEnvironment{}, nil
}
func TestEnvironmentHTTPPreservesTargetOmissionAndTypedServiceIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name, path, body string
		wantStatus       int
		targetsNil       bool
		targetCount      int
		service          string
	}{
		{"configuration", "/env/configuration", `{"applicationId":"app","environmentId":"test","expectedUpdatedAt":"2026-10-26T00:00:00Z","namespace":"new-default"}`, 200, true, 0, ""},
		{"legacy omission", "/env", `{"applicationId":"app","environmentId":"test"}`, 200, true, 0, ""},
		{"explicit empty", "/env", `{"applicationId":"app","environmentId":"test","targets":[]}`, 200, false, 0, ""},
		{"explicit identity", "/env", `{"applicationId":"app","environmentId":"test","targets":[{"serviceId":"logical-api","namespace":"actual-ns"}]}`, 200, false, 1, "logical-api"},
		{"configuration rejects targets", "/env/configuration", `{"expectedUpdatedAt":"2026-10-26T00:00:00Z","targets":[]}`, 400, false, 0, ""},
		{"configuration rejects explicit null", "/env/configuration", `{"expectedUpdatedAt":"2026-10-26T00:00:00Z","targets":null}`, 400, false, 0, ""},
		{"configuration requires CAS", "/env/configuration", `{"applicationId":"app","environmentId":"test"}`, 400, false, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &targetIdentityEnvironmentStub{}
			handler := &CatalogHandler{environments: service}
			router := gin.New()
			router.PUT("/env", handler.UpdateApplicationEnvironment)
			router.PUT("/env/configuration", handler.UpdateApplicationEnvironmentConfiguration)
			request := httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tt.wantStatus != 200 {
				if service.calls != 0 {
					t.Fatal("rejected configuration reached update")
				}
				return
			}
			if service.calls != 1 || (service.input.Targets == nil) != tt.targetsNil || len(service.input.Targets) != tt.targetCount {
				t.Fatalf("target intent lost: %+v", service.input)
			}
			if tt.name == "configuration" && (service.input.Namespace != "new-default" || service.input.ApplicationID != "app" || service.input.EnvironmentID != "test" || service.input.ExpectedUpdatedAt == nil) {
				t.Fatalf("configuration fields lost: %+v", service.input)
			}
			if tt.service != "" && (service.input.Targets[0].ServiceID != tt.service || service.input.Targets[0].Namespace != "actual-ns") {
				t.Fatalf("identity lost: %+v", service.input.Targets)
			}
		})
	}
}
