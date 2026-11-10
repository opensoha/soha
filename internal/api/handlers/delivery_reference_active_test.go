package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type activeReferenceHandlerStub struct {
	DeliveryApplicationService
	calls                                       int
	err                                         error
	binding, service, target, ref, cursor, plan string
	revision                                    int64
	limit                                       int
	input                                       domaindelivery.ServiceReferenceInput
}

func (s *activeReferenceHandlerStub) ListEnvironmentServiceReferences(_ context.Context, _ domainidentity.Principal, binding, cursor string, limit int) (domaindelivery.ServiceReferencePage, error) {
	s.calls++
	s.binding, s.cursor, s.limit = binding, cursor, limit
	return domaindelivery.ServiceReferencePage{Items: []domaindelivery.ServiceReferenceSummary{}}, s.err
}
func (s *activeReferenceHandlerStub) GetEnvironmentServiceReference(_ context.Context, _ domainidentity.Principal, binding, service string) (domaindelivery.ServiceReference, error) {
	s.calls++
	s.binding, s.service = binding, service
	return domaindelivery.ServiceReference{State: "active"}, s.err
}
func (s *activeReferenceHandlerStub) PutEnvironmentServiceReference(_ context.Context, _ domainidentity.Principal, binding, service string, input domaindelivery.ServiceReferenceInput) (domaindelivery.ServiceReference, error) {
	s.calls++
	s.binding, s.service, s.input = binding, service, input
	return domaindelivery.ServiceReference{State: "active"}, s.err
}
func (s *activeReferenceHandlerStub) DeleteEnvironmentServiceReference(_ context.Context, _ domainidentity.Principal, binding, service string, input domaindelivery.ServiceReferenceDeleteInput) error {
	s.calls++
	s.binding, s.service, s.ref, s.revision = binding, service, input.ExpectedReferenceID, input.ExpectedRevision
	return s.err
}
func (s *activeReferenceHandlerStub) ResolveEnvironmentServiceReferenceTarget(_ context.Context, _ domainidentity.Principal, binding, service, target, ref string, revision int64) (domaindelivery.ServiceReferenceResolvedTarget, error) {
	s.calls++
	s.binding, s.service, s.target, s.ref, s.revision = binding, service, target, ref, revision
	return domaindelivery.ServiceReferenceResolvedTarget{ReferenceID: ref, ReferenceRevision: revision}, s.err
}
func (s *activeReferenceHandlerStub) ListEnvironmentServiceReferenceConsumers(_ context.Context, _ domainidentity.Principal, binding, service, cursor string, limit int) (domaindelivery.ServiceReferenceConsumerPage, error) {
	s.calls++
	s.binding, s.service, s.cursor, s.limit = binding, service, cursor, limit
	return domaindelivery.ServiceReferenceConsumerPage{}, s.err
}
func (s *activeReferenceHandlerStub) GetDeliveryPlanServiceReferenceImpact(_ context.Context, _ domainidentity.Principal, plan, cursor string, limit int) (domaindelivery.DeliveryPlanServiceReferenceImpact, error) {
	s.calls++
	s.plan, s.cursor, s.limit = plan, cursor, limit
	return domaindelivery.DeliveryPlanServiceReferenceImpact{}, s.err
}

func activeReferenceRouter(service DeliveryApplicationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &DeliveryHandler{applications: service}
	r := gin.New()
	r.GET("/bindings/:applicationEnvironmentID/refs", h.ListEnvironmentServiceReferences)
	r.GET("/bindings/:applicationEnvironmentID/refs/:serviceID", h.GetEnvironmentServiceReference)
	r.PUT("/bindings/:applicationEnvironmentID/refs/:serviceID", h.PutEnvironmentServiceReference)
	r.DELETE("/bindings/:applicationEnvironmentID/refs/:serviceID", h.DeleteEnvironmentServiceReference)
	r.GET("/bindings/:applicationEnvironmentID/refs/:serviceID/targets/:targetID", h.ResolveEnvironmentServiceReferenceTarget)
	r.GET("/bindings/:applicationEnvironmentID/consumers", h.ListEnvironmentServiceReferenceConsumers)
	r.GET("/plans/:planID/impact", h.GetDeliveryPlanServiceReferenceImpact)
	return r
}

func TestActiveReferenceHandlersNoStoreAndScope(t *testing.T) {
	cases := []struct{ method, path, body string }{
		{"GET", "/bindings/consumer/refs?cursor=svc&limit=7", ""},
		{"GET", "/bindings/consumer/refs/svc", ""},
		{"PUT", "/bindings/consumer/refs/svc", `{"expectedCandidateId":"candidate","expectedCandidateRevision":4,"expectedRevision":2,"expectedReferenceId":"ref","expectedConsumerUpdatedAt":"2026-11-04T08:00:00.123456Z"}`},
		{"DELETE", "/bindings/consumer/refs/svc", `{"expectedReferenceId":"ref","expectedRevision":2}`},
		{"GET", "/bindings/consumer/refs/svc/targets/target?expectedReferenceId=ref&expectedRevision=2", ""},
		{"GET", "/bindings/consumer/consumers?serviceId=svc&cursor=visible&limit=7", ""},
		{"GET", "/plans/plan/impact?cursor=visible&limit=7", ""},
	}
	for _, tc := range cases {
		for _, err := range []error{nil, apperrors.ErrAccessDenied, apperrors.ErrNotFound, apperrors.ErrConflict, apperrors.ErrUnsupportedOperation} {
			t.Run(tc.method+tc.path+http.StatusText(referencePreviewErrorStatus(err)), func(t *testing.T) {
				s := &activeReferenceHandlerStub{err: err}
				w := httptest.NewRecorder()
				activeReferenceRouter(s).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
				status := referencePreviewErrorStatus(err)
				if tc.method == "DELETE" && err == nil {
					status = 204
				}
				if w.Code != status || s.calls != 1 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" {
					t.Fatalf("status=%d calls=%d response=%s", w.Code, s.calls, w.Body.String())
				}
				if strings.HasPrefix(tc.path, "/bindings/") && s.binding != "consumer" {
					t.Fatal("consumer binding lost")
				}
				if tc.method == "PUT" && (s.input.ExpectedReferenceID != "ref" || s.input.ExpectedRevision == nil || *s.input.ExpectedRevision != 2 || s.input.ExpectedConsumerUpdatedAt.Nanosecond() != 123456000) {
					t.Fatalf("CAS input lost: %+v", s.input)
				}
				if strings.Contains(tc.path, "/targets/") && (s.target != "target" || s.ref != "ref" || s.revision != 2) {
					t.Fatal("target identity lost")
				}
				if strings.HasPrefix(tc.path, "/plans/") && s.plan != "plan" {
					t.Fatal("Plan identity lost")
				}
			})
		}
	}
}

func TestActiveReferenceHandlersRejectMalformedOrUnexpectedInput(t *testing.T) {
	cases := []struct{ method, path, body string }{
		{"PUT", "/bindings/view/refs/svc", `{"expectedRevision":0,"expectedReferenceId":""}`},
		{"PUT", "/bindings/view/refs/svc", `{"expectedRevision":0,"expectedReferenceId":null}`},
		{"PUT", "/bindings/view/refs/svc", `{"expectedRevision":1,"expectedReferenceId":null}`},
		{"PUT", "/bindings/view/refs/svc", `{"sourceApplicationEnvironmentId":"arbitrary"}`},
		{"PUT", "/bindings/view/refs/svc", `{"force":true}`},
		{"PUT", "/bindings/view/refs/svc", `{} {}`},
		{"DELETE", "/bindings/view/refs/svc", `{"deleteResources":true}`},
		{"GET", "/bindings/view/refs?limit=101", ""},
		{"GET", "/bindings/view/refs?limit=1&limit=2", ""},
		{"GET", "/bindings/view/refs?cursor=a&cursor=b", ""},
		{"GET", "/bindings/view/refs/svc?includeYaml=true", ""},
		{"GET", "/bindings/view/refs/svc/targets/t?expectedRevision=0&expectedReferenceId=ref", ""},
		{"GET", "/bindings/view/refs/svc/targets/t?expectedRevision=1", ""},
		{"GET", "/bindings/view/consumers?includeHidden=true&serviceId=svc", ""},
		{"GET", "/bindings/view/consumers", ""},
		{"GET", "/plans/plan/impact?limit=-1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			s := &activeReferenceHandlerStub{}
			w := httptest.NewRecorder()
			activeReferenceRouter(s).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if w.Code != 400 || s.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, s.calls, w.Body.String())
			}
		})
	}
}

func TestActiveReferenceHandlersUnavailableFailClosed(t *testing.T) {
	for _, path := range []string{"/bindings/view/refs", "/bindings/view/refs/svc", "/bindings/view/consumers?serviceId=svc", "/plans/plan/impact"} {
		w := httptest.NewRecorder()
		activeReferenceRouter(&referencePreviewHandlerStub{}).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 501 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("path=%s status=%d", path, w.Code)
		}
	}
}
