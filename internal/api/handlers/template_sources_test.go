package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type templateSourceHandlerStub struct {
	TemplateSourceService
	calls int
}

func (s *templateSourceHandlerStub) List(context.Context, domainidentity.Principal, int, int) ([]domaindocument.Source, error) {
	s.calls++
	return []domaindocument.Source{}, nil
}

func TestTemplateSourceRequestCannotInjectContentOrExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ path, body string }{
		{"/sources", `{"repositoryId":"repo","autoPublish":true}`},
		{"/sync", `{"expectedGeneration":1,"idempotencyKey":"key","files":[]}`},
		{"/apply", `{"expectedGeneration":1,"idempotencyKey":"key","candidateDigest":"sha256:a","content":"changed"}`},
		{"/remove", `{"expectedGeneration":1,"disposition":"keep","deleteObjects":true}`},
		{"/sync", `{} {}`},
		{"/sync", `{"idempotencyKey":"` + strings.Repeat("x", 4096) + `"}`},
	} {
		service := &templateSourceHandlerStub{}
		handler := NewTemplateSourceHandler(service)
		router := gin.New()
		router.POST("/sources", handler.Save)
		router.POST("/sync", handler.Sync)
		router.POST("/apply", handler.Apply)
		router.POST("/remove", handler.Remove)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe %s input accepted: %d", tc.path, response.Code)
		}
	}
}

func TestTemplateSourceListPaginationAndEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &templateSourceHandlerStub{}
	router := gin.New()
	router.GET("/sources", NewTemplateSourceHandler(service).List)
	for _, query := range []string{"?limit=0", "?limit=201", "?offset=-1", "?offset=invalid", "?limit=1.5"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources"+query, nil))
		if response.Code != http.StatusBadRequest || service.calls != 0 {
			t.Fatal("invalid pagination reached storage")
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sources?offset=0&limit=200", nil))
	var payload map[string]json.RawMessage
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || string(payload["data"]) != "[]" || len(payload) != 1 {
		t.Fatalf("source list does not match contract envelope: %s", response.Body.String())
	}
}
