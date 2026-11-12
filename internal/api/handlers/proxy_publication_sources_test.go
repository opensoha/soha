package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type publicationSourcesStub struct{ called bool }

func (s *publicationSourcesStub) List(context.Context, identity.Principal, string) (api.NetworkProxySourceList, error) {
	return api.NetworkProxySourceList{Items: []api.NetworkProxySource{}}, nil
}
func (s *publicationSourcesStub) Get(context.Context, identity.Principal, string, string) (api.NetworkProxySource, error) {
	return api.NetworkProxySource{Configured: true}, nil
}
func (s *publicationSourcesStub) Put(context.Context, identity.Principal, string, string, api.NetworkProxySourceInput) (api.NetworkProxySource, error) {
	s.called = true
	return api.NetworkProxySource{SourceID: "source", Configured: true, Revision: 1}, nil
}
func (s *publicationSourcesStub) Delete(context.Context, identity.Principal, string, string, int64) error {
	s.called = true
	return nil
}
func (s *publicationSourcesStub) Preview(context.Context, identity.Principal, string, api.NetworkProxySourceInput) (api.NetworkProxyConversionPreview, error) {
	s.called = true
	return api.NetworkProxyConversionPreview{}, nil
}
func TestPublicationSourceTransportEnvelopeAndStrictInput(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		called     bool
	}{
		{"create", `{"name":"Source","format":"manual","nodes":[],"content":"synthetic-secret"}`, http.StatusAccepted, true},
		{"unknown_controller", `{"name":"Source","format":"manual","controller":"synthetic-secret"}`, http.StatusBadRequest, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &publicationSourcesStub{}
			h := NewProxyPublicationSourcesHandler(s)
			router := gin.New()
			router.POST("/instances/:instanceID/sources", h.Create)
			request := httptest.NewRequest(http.MethodPost, "/instances/instance/sources", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || s.called != test.called {
				t.Fatal("unexpected transport result", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "synthetic-secret") {
				t.Fatal("source secret echoed")
			}
			if test.called && !strings.Contains(response.Body.String(), `"data":`) {
				t.Fatal("missing console data envelope")
			}
		})
	}
}
