package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type targetSourceHandlerStub struct {
	DeliveryKubernetesImportService
	filter          domaindelivery.KubernetesImportCandidateFilter
	binding, target string
	err             error
}

func (s *targetSourceHandlerStub) ListKubernetesImportCandidates(_ context.Context, _ domainidentity.Principal, filter domaindelivery.KubernetesImportCandidateFilter) (domaindelivery.KubernetesImportCandidatePage, error) {
	s.filter = filter
	return domaindelivery.KubernetesImportCandidatePage{Items: []domaindelivery.KubernetesImportCandidate{}}, nil
}
func (s *targetSourceHandlerStub) GetImportedTargetSource(_ context.Context, _ domainidentity.Principal, binding, target string) (domainresource.ResourceYAMLView, error) {
	s.binding, s.target = binding, target
	return domainresource.ResourceYAMLView{Content: "kind: Widget"}, s.err
}

func TestDeliverySourceHandlerForwardsExplicitScopeAndPreventsCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &targetSourceHandlerStub{}
	handler := &DeliveryHandler{imports: service}
	router := gin.New()
	router.GET("/candidates", handler.ListTargetCandidates)
	router.GET("/bindings/:applicationEnvironmentID/targets/:targetID/source", handler.GetImportedTargetSource)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/candidates?clusterId=c&namespace=n&workloadKind=CustomResource&crdName=widgets.example.com&limit=25", nil))
	if response.Code != 200 || service.filter.WorkloadKind != "CustomResource" || service.filter.CRDName != "widgets.example.com" || service.filter.Limit != 25 {
		t.Fatalf("filter=%+v response=%s", service.filter, response.Body.String())
	}
	for _, err := range []error{nil, apperrors.ErrConflict, apperrors.ErrAccessDenied} {
		service.err = err
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/bindings/env-1/targets/target-1/source", nil))
		want := 200
		if errors.Is(err, apperrors.ErrConflict) {
			want = 409
		}
		if errors.Is(err, apperrors.ErrAccessDenied) {
			want = 403
		}
		if response.Code != want || response.Header().Get("Cache-Control") != "no-store" || service.binding != "env-1" || service.target != "target-1" {
			t.Fatalf("source response=%d headers=%v scope=%s/%s", response.Code, response.Header(), service.binding, service.target)
		}
	}
}
