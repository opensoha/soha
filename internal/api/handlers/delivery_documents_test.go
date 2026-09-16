package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type documentHandlerStub struct {
	DeliveryDocumentService
	calls int
}

func (s *documentHandlerStub) Preview(context.Context, domainidentity.Principal, domaindocument.PreviewInput) (domaindocument.Preview, error) {
	s.calls++
	return domaindocument.Preview{Valid: true}, nil
}

func TestDeliveryDocumentRequestIsBoundedAndStrict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{"files":[],"publish":true}`, `{"files":[{"path":"a.yaml","content":"x","execute":true}]}`, `{"files":[]} {}`, `{"files":[{"content":"` + strings.Repeat("x", 16<<20) + `"}]}`} {
		service := &documentHandlerStub{}
		router := gin.New()
		router.POST("/preview", NewDeliveryDocumentHandler(service).Preview)
		request := httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(body))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || service.calls != 0 {
			t.Fatalf("invalid document request reached service: %d calls=%d", response.Code, service.calls)
		}
	}
}
