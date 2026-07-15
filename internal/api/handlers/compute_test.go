package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appcompute "github.com/opensoha/soha/internal/application/compute"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type computeHandlerFake struct{}

func (computeHandlerFake) Overview(context.Context, domainidentity.Principal) (sohaapi.ComputeOverview, error) {
	return sohaapi.ComputeOverview{}, nil
}
func (computeHandlerFake) ListAccessSources(context.Context, domainidentity.Principal, appcompute.AccessSourceFilter) (sohaapi.ComputeAccessSourceListEnvelope, error) {
	return sohaapi.ComputeAccessSourceListEnvelope{}, nil
}
func (computeHandlerFake) ListProviders(context.Context, domainidentity.Principal, appcompute.ProviderFilter) (sohaapi.ComputeProviderListEnvelope, error) {
	return sohaapi.ComputeProviderListEnvelope{}, nil
}
func (computeHandlerFake) ListRelations(context.Context, domainidentity.Principal, string, string, string, string, int) (sohaapi.ComputeResourceRelations, error) {
	return sohaapi.ComputeResourceRelations{}, nil
}
func (computeHandlerFake) ListTasks(_ context.Context, _ domainidentity.Principal, filter appcompute.TaskFilter) (sohaapi.ComputeTaskListEnvelope, error) {
	if filter.Cursor != "" {
		return sohaapi.ComputeTaskListEnvelope{}, fmt.Errorf("%w: invalid cursor", apperrors.ErrInvalidArgument)
	}
	return sohaapi.ComputeTaskListEnvelope{}, nil
}
func (computeHandlerFake) GetTask(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskView, error) {
	return sohaapi.ComputeTaskView{}, nil
}

func TestComputeHandlerRejectsInvalidTaskFiltersAndCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewComputeHandler(computeHandlerFake{})
	router.GET("/compute/tasks", handler.ListTasks)
	for _, target := range []string{"/compute/tasks?status=bogus", "/compute/tasks?cursor=bogus"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, body = %s", target, recorder.Code, recorder.Body.String())
		}
	}
}
