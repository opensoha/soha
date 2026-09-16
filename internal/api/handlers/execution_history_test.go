package handlers

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type historyHandlerService struct {
	WorkflowService
	filter domainworkflow.ExecutionHistoryFilter
	calls  int
}

func (s *historyHandlerService) ListExecutionHistory(_ context.Context, _ domainidentity.Principal, filter domainworkflow.ExecutionHistoryFilter) (domainworkflow.ExecutionHistoryPage, error) {
	s.filter = filter
	s.calls++
	return domainworkflow.ExecutionHistoryPage{Items: []domainworkflow.ExecutionHistoryEntry{}}, nil
}
func TestExecutionHistoryHandlerBindsFiltersAndRejectsBadLimits(t *testing.T) {
	service := &historyHandlerService{}
	router := gin.New()
	router.GET("/history", NewWorkflowHandler(service).ListExecutionHistory)
	for _, query := range []string{"limit=0", "limit=101", "limit=nope"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/history?"+query, nil))
		if response.Code != 400 || service.calls != 0 {
			t.Fatalf("invalid limit reached service: %s %d", query, response.Code)
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/history?applicationId=app&serviceId=web&workflowId=definition&applicationEnvironmentId=prod&buildSourceId=source&status=failed&search=release&cursor=older&limit=12", nil))
	if response.Code != 200 || service.calls != 1 || service.filter.ApplicationID != "app" || service.filter.ServiceID != "web" || service.filter.WorkflowID != "definition" || service.filter.ApplicationEnvironmentID != "prod" || service.filter.BuildSourceID != "source" || service.filter.Status != "failed" || service.filter.Search != "release" || service.filter.Cursor != "older" || service.filter.Limit != 12 {
		t.Fatalf("lost history filters: %d %+v", response.Code, service.filter)
	}
}
