package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type captureWorkflowTemplateService struct {
	WorkflowTemplateService
	input domaincatalog.WorkflowTemplateInput
}

func (s *captureWorkflowTemplateService) CreateWorkflowTemplate(_ context.Context, _ domainidentity.Principal, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	s.input = input
	return domaincatalog.WorkflowTemplate{ID: "workflow-1"}, nil
}

func (s *captureWorkflowTemplateService) UpdateWorkflowTemplate(ctx context.Context, principal domainidentity.Principal, _ string, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	return s.CreateWorkflowTemplate(ctx, principal, input)
}

func TestWorkflowTemplateHTTPPreservesDraftAndRevision(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			service := &captureWorkflowTemplateService{}
			handler := NewCatalogHandlerWithServices(nil, nil, service)
			router := gin.New()
			router.POST("/workflow-templates", handler.CreateWorkflowTemplate)
			router.PUT("/workflow-templates", handler.UpdateWorkflowTemplate)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(method, "/workflow-templates", strings.NewReader(`{"key":"release","name":"Release","definition":{},"enabled":true,"expectedRevision":7,"publish":false}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			if response.Code >= 300 || service.input.ExpectedRevision == nil || *service.input.ExpectedRevision != 7 || service.input.Publish == nil || *service.input.Publish {
				t.Fatalf("status=%d input=%+v body=%s", response.Code, service.input, response.Body.String())
			}
		})
	}
}
