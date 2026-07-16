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

type computeHandlerFake struct{ filter appcompute.TaskFilter }

func (*computeHandlerFake) Overview(context.Context, domainidentity.Principal) (sohaapi.ComputeOverview, error) {
	return sohaapi.ComputeOverview{}, nil
}
func (*computeHandlerFake) ListAccessSources(context.Context, domainidentity.Principal, appcompute.AccessSourceFilter) (sohaapi.ComputeAccessSourceListEnvelope, error) {
	return sohaapi.ComputeAccessSourceListEnvelope{}, nil
}
func (f *computeHandlerFake) ListTasks(_ context.Context, _ domainidentity.Principal, filter appcompute.TaskFilter) (sohaapi.ComputeTaskListEnvelope, error) {
	f.filter = filter
	if filter.Cursor != "" {
		return sohaapi.ComputeTaskListEnvelope{}, fmt.Errorf("%w: invalid cursor", apperrors.ErrInvalidArgument)
	}
	return sohaapi.ComputeTaskListEnvelope{}, nil
}
func (*computeHandlerFake) GetTask(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskView, error) {
	return sohaapi.ComputeTaskView{ID: "task-1"}, nil
}
func (*computeHandlerFake) ListTaskLogs(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskLogListEnvelope, error) {
	return sohaapi.ComputeTaskLogListEnvelope{Items: []sohaapi.ComputeTaskLog{}}, nil
}
func (*computeHandlerFake) CancelTask(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskView, error) {
	return sohaapi.ComputeTaskView{ID: "task-1"}, nil
}
func (*computeHandlerFake) RetryTask(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskView, error) {
	return sohaapi.ComputeTaskView{ID: "task-1"}, nil
}
func TestComputeHandlerRejectsInvalidTaskFiltersAndCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewComputeHandler(&computeHandlerFake{})
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

func TestComputeTaskHandlersExposeCanonicalFacade(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &computeHandlerFake{}
	handler := NewComputeHandler(service)
	router := gin.New()
	router.GET("/compute/tasks", handler.ListTasks)
	router.GET("/compute/tasks/:domain/:id", handler.GetTask)
	router.GET("/compute/tasks/:domain/:id/logs", handler.ListTaskLogs)
	router.POST("/compute/tasks/:domain/:id/cancel", handler.CancelTask)
	router.POST("/compute/tasks/:domain/:id/retry", handler.RetryTask)

	requests := []struct {
		method string
		target string
		status int
	}{
		{method: http.MethodGet, target: "/compute/tasks?resourceKind=project&resourceId=project-1", status: http.StatusOK},
		{method: http.MethodGet, target: "/compute/tasks/virtualization/task-1", status: http.StatusOK},
		{method: http.MethodGet, target: "/compute/tasks/virtualization/task-1/logs", status: http.StatusOK},
		{method: http.MethodPost, target: "/compute/tasks/virtualization/task-1/cancel", status: http.StatusAccepted},
		{method: http.MethodPost, target: "/compute/tasks/container_runtime/task-1/retry", status: http.StatusAccepted},
		{method: http.MethodGet, target: "/compute/tasks/bogus/task-1", status: http.StatusBadRequest},
	}
	for _, item := range requests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(item.method, item.target, nil)
		router.ServeHTTP(recorder, request)
		if recorder.Code != item.status {
			t.Fatalf("%s %s status = %d, body = %s", item.method, item.target, recorder.Code, recorder.Body.String())
		}
	}
	if service.filter.ResourceKind != "project" || service.filter.ResourceID != "project-1" {
		t.Fatalf("task filter = %#v", service.filter)
	}
}
