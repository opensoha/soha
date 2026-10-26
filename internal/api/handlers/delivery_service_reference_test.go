package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type referencePreviewHandlerStub struct {
	DeliveryApplicationService
	input     domaindelivery.ServiceReferencePreviewInput
	bindingID string
	calls     int
	err       error
}

func (s *referencePreviewHandlerStub) PreviewEnvironmentServiceReference(_ context.Context, _ domainidentity.Principal, id string, input domaindelivery.ServiceReferencePreviewInput) (domaindelivery.ServiceReferencePreview, error) {
	s.calls++
	s.bindingID = id
	s.input = input
	return domaindelivery.ServiceReferencePreview{ServiceID: input.ServiceID}, s.err
}

func TestServiceReferencePreviewHandlerPreservesScopeRevisionsAndNoStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, err := range []error{nil, apperrors.ErrAccessDenied, apperrors.ErrConflict, apperrors.ErrNotFound, apperrors.ErrUnsupportedOperation} {
		t.Run(http.StatusText(referencePreviewErrorStatus(err)), func(t *testing.T) {
			service := &referencePreviewHandlerStub{err: err}
			handler := &DeliveryHandler{applications: service}
			router := gin.New()
			router.POST("/bindings/:applicationEnvironmentID/preview", handler.PreviewEnvironmentServiceReference)
			body := `{"serviceId":"svc","sourceApplicationEnvironmentId":"source","expectedConsumerUpdatedAt":"2026-10-26T08:00:00.123Z","expectedSourceUpdatedAt":"2026-10-26T08:01:00Z","sourceTargetIds":["target-b"],"expectedSourceRevision":"` + strings.Repeat("a", 64) + `"}`
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/bindings/view/preview", strings.NewReader(body)))
			if recorder.Code != referencePreviewErrorStatus(err) || recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Pragma") != "no-cache" || service.bindingID != "view" || service.input.ServiceID != "svc" || service.input.SourceApplicationEnvironmentID != "source" || len(service.input.SourceTargetIDs) != 1 || service.input.SourceTargetIDs[0] != "target-b" || service.input.ExpectedConsumerUpdatedAt.Nanosecond() != 123000000 || service.input.ExpectedSourceUpdatedAt.IsZero() || service.input.ExpectedSourceRevision != strings.Repeat("a", 64) {
				t.Fatalf("status=%d input=%+v response=%s", recorder.Code, service.input, recorder.Body.String())
			}
		})
	}
}

func referencePreviewErrorStatus(err error) int {
	switch {
	case err == nil:
		return 200
	case errors.Is(err, apperrors.ErrAccessDenied):
		return 403
	case errors.Is(err, apperrors.ErrNotFound):
		return 404
	case errors.Is(err, apperrors.ErrUnsupportedOperation):
		return 501
	default:
		return 409
	}
}

func TestServiceReferencePreviewHandlerRejectsMalformedOrWritePayloads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{"sourceTargetIds":null}`,
		`{"sourceTargetIds":[]}`,
		`{"sourceTargetIds":"target"}`,
		`{"apply":true}`,
		`{"ownershipMode":"managed"}`,
		`{} {}`,
		`{"expectedSourceUpdatedAt":"bad-date"}`,
		`{"padding":"` + strings.Repeat("x", 33*1024) + `"}`,
	} {
		t.Run(body[:min(len(body), 64)], func(t *testing.T) {
			service := &referencePreviewHandlerStub{}
			handler := &DeliveryHandler{applications: service}
			router := gin.New()
			router.POST("/preview", handler.PreviewEnvironmentServiceReference)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(body)))
			if recorder.Code != 400 || service.calls != 0 || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d calls=%d", recorder.Code, service.calls)
			}
		})
	}
}

func TestServiceReferencePreviewHandlerKeepsOmittedTargetSelectionDistinct(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &referencePreviewHandlerStub{}
	handler := &DeliveryHandler{applications: service}
	router := gin.New()
	router.POST("/preview", handler.PreviewEnvironmentServiceReference)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(`{"serviceId":"svc"}`)))
	if recorder.Code != 200 || service.input.SourceTargetIDs != nil || !service.input.ExpectedSourceUpdatedAt.Equal(time.Time{}) {
		t.Fatalf("input=%+v status=%d", service.input, recorder.Code)
	}
}
