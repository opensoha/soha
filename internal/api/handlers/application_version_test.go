package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type versionedApplicationStub struct {
	ApplicationCatalogService
	input  domainapp.UpsertInput
	called bool
}

func (s *versionedApplicationStub) Update(_ context.Context, _ domainidentity.Principal, _ string, input domainapp.UpsertInput) (domainapp.App, error) {
	s.called, s.input = true, input
	return domainapp.App{}, apperrors.NewBusiness(apperrors.ErrConflict, "application_version_conflict", "Application changed.", "应用配置已修改。")
}

func TestUpdateApplicationVersionBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, version string
		status        int
	}{
		{"stale", `,"expectedVersion":2`, http.StatusConflict},
		{"legacy", "", http.StatusConflict},
		{"zero", `,"expectedVersion":0`, http.StatusBadRequest},
		{"negative", `,"expectedVersion":-1`, http.StatusBadRequest},
		{"fraction", `,"expectedVersion":1.5`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &versionedApplicationStub{}
			handler := &ApplicationHandler{applications: service}
			router := gin.New()
			router.PUT("/applications/:applicationID", handler.UpdateApplication)
			request := httptest.NewRequest(http.MethodPut, "/applications/app-1", strings.NewReader(`{"name":"API","key":"api","enabled":true`+tc.version+`}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if tc.status == http.StatusBadRequest && service.called {
				t.Fatal("invalid version reached application service")
			}
			if tc.name == "stale" && (service.input.ExpectedVersion == nil || *service.input.ExpectedVersion != 2 || !strings.Contains(response.Body.String(), "application_version_conflict")) {
				t.Fatalf("version or conflict mapping lost: %#v, %s", service.input, response.Body.String())
			}
		})
	}
}
