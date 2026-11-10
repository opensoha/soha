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

type referenceCandidateHandlerStub struct {
	DeliveryApplicationService
	err                  error
	calls                int
	bindingID, serviceID string
	input                domaindelivery.ServiceReferenceCandidateInput
	remove               domaindelivery.ServiceReferenceCandidateDeleteInput
}

func (s *referenceCandidateHandlerStub) ListEnvironmentServiceReferenceCandidates(_ context.Context, _ domainidentity.Principal, b, cursor string, limit int) (domaindelivery.ServiceReferenceCandidatePage, error) {
	s.calls++
	s.bindingID = b
	return domaindelivery.ServiceReferenceCandidatePage{Items: []domaindelivery.ServiceReferenceCandidateSummary{{ID: "candidate", ServiceID: cursor, Revision: int64(limit), State: "candidate"}}}, s.err
}

func (s *referenceCandidateHandlerStub) GetEnvironmentServiceReferenceCandidate(_ context.Context, _ domainidentity.Principal, b, v string) (domaindelivery.ServiceReferenceCandidate, error) {
	s.calls++
	s.bindingID, s.serviceID = b, v
	return domaindelivery.ServiceReferenceCandidate{State: "candidate"}, s.err
}
func (s *referenceCandidateHandlerStub) PutEnvironmentServiceReferenceCandidate(_ context.Context, _ domainidentity.Principal, b, v string, input domaindelivery.ServiceReferenceCandidateInput) (domaindelivery.ServiceReferenceCandidate, error) {
	s.calls++
	s.bindingID, s.serviceID, s.input = b, v, input
	return domaindelivery.ServiceReferenceCandidate{State: "candidate"}, s.err
}
func (s *referenceCandidateHandlerStub) DeleteEnvironmentServiceReferenceCandidate(_ context.Context, _ domainidentity.Principal, b, v string, input domaindelivery.ServiceReferenceCandidateDeleteInput) error {
	s.calls++
	s.bindingID, s.serviceID, s.remove = b, v, input
	return s.err
}

func referenceCandidateRouter(service DeliveryApplicationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &DeliveryHandler{applications: service}
	r := gin.New()
	path := "/bindings/:applicationEnvironmentID/candidates/:serviceID"
	r.GET("/bindings/:applicationEnvironmentID/candidates", h.ListEnvironmentServiceReferenceCandidates)
	r.GET(path, h.GetEnvironmentServiceReferenceCandidate)
	r.PUT(path, h.PutEnvironmentServiceReferenceCandidate)
	r.DELETE(path, h.DeleteEnvironmentServiceReferenceCandidate)
	return r
}

func TestServiceReferenceCandidateHandlersPreserveScopeAndNoStore(t *testing.T) {
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		for _, err := range []error{nil, apperrors.ErrAccessDenied, apperrors.ErrNotFound, apperrors.ErrConflict, apperrors.ErrUnsupportedOperation} {
			t.Run(method+"/"+http.StatusText(referencePreviewErrorStatus(err)), func(t *testing.T) {
				s := &referenceCandidateHandlerStub{err: err}
				body := `{"sourceApplicationEnvironmentId":"source","expectedConsumerUpdatedAt":"2026-10-26T08:00:00.123456Z","expectedSourceUpdatedAt":"2026-10-26T08:01:00Z","sourceTargetIds":["target"],"expectedSourceRevision":"` + strings.Repeat("a", 64) + `","expectedRevision":0}`
				if method == "DELETE" {
					body = `{"expectedCandidateId":"candidate","expectedRevision":3,"expectedConsumerUpdatedAt":"2026-10-26T08:00:00.123456Z"}`
				}
				w := httptest.NewRecorder()
				referenceCandidateRouter(s).ServeHTTP(w, httptest.NewRequest(method, "/bindings/consumer/candidates/service", strings.NewReader(body)))
				status := referencePreviewErrorStatus(err)
				if method == "DELETE" && err == nil {
					status = 204
				}
				if w.Code != status || s.calls != 1 || s.bindingID != "consumer" || s.serviceID != "service" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" {
					t.Fatalf("status=%d calls=%d scope=%s/%s", w.Code, s.calls, s.bindingID, s.serviceID)
				}
				if method == "PUT" && (s.input.ExpectedRevision == nil || *s.input.ExpectedRevision != 0 || s.input.ExpectedConsumerUpdatedAt.Nanosecond() != 123456000) {
					t.Fatalf("input=%+v", s.input)
				}
				if method == "DELETE" && (s.remove.ExpectedCandidateID != "candidate" || s.remove.ExpectedRevision != 3 || s.remove.ExpectedConsumerUpdatedAt.Nanosecond() != 123456000) {
					t.Fatalf("delete=%+v", s.remove)
				}
			})
		}
	}
}

func TestServiceReferenceCandidateHandlersRejectActivationAndMalformedPayloads(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		for _, body := range []string{`{"activate":true}`, `{"force":true}`, `{} {}`, `{"expectedRevision":"3"}`, `{"expectedConsumerUpdatedAt":"bad-time"}`, `{"padding":"` + strings.Repeat("x", 33*1024) + `"}`} {
			t.Run(method+"/"+body[:min(len(body), 45)], func(t *testing.T) {
				s := &referenceCandidateHandlerStub{}
				w := httptest.NewRecorder()
				referenceCandidateRouter(s).ServeHTTP(w, httptest.NewRequest(method, "/bindings/view/candidates/service", strings.NewReader(body)))
				if w.Code != 400 || s.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status=%d calls=%d", w.Code, s.calls)
				}
			})
		}
	}
}

func TestServiceReferenceCandidateHandlersRejectUnavailableCapability(t *testing.T) {
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		w := httptest.NewRecorder()
		referenceCandidateRouter(&referencePreviewHandlerStub{}).ServeHTTP(w, httptest.NewRequest(method, "/bindings/view/candidates/service", strings.NewReader(`{}`)))
		if w.Code != 501 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("method=%s status=%d", method, w.Code)
		}
	}
}

func TestServiceReferenceCandidateMetadataHandlerBoundsAndPreservesCursor(t *testing.T) {
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=wrong", "?limit=1&limit=2", "?cursor=a&cursor=b", "?includeSource=true"} {
		s := &referenceCandidateHandlerStub{}
		w := httptest.NewRecorder()
		referenceCandidateRouter(s).ServeHTTP(w, httptest.NewRequest("GET", "/bindings/view/candidates"+query, nil))
		if w.Code != 400 || s.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("query=%s status=%d calls=%d", query, w.Code, s.calls)
		}
	}
	s := &referenceCandidateHandlerStub{}
	w := httptest.NewRecorder()
	referenceCandidateRouter(s).ServeHTTP(w, httptest.NewRequest("GET", "/bindings/view/candidates?limit=3&cursor=svc", nil))
	if w.Code != 200 || s.calls != 1 || s.bindingID != "view" || !strings.Contains(w.Body.String(), `"serviceId":"svc"`) || !strings.Contains(w.Body.String(), `"revision":3`) {
		t.Fatalf("metadata response=%s", w.Body.String())
	}
}
