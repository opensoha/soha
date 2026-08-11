package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appcompute "github.com/opensoha/soha/internal/application/compute"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type computeHandlerFake struct {
	filter         appcompute.TaskFilter
	idempotencyKey string
}

func (*computeHandlerFake) Capabilities(context.Context, domainidentity.Principal) (sohaapi.ComputeCapabilityManifest, error) {
	return sohaapi.ComputeCapabilityManifest{}, nil
}
func (*computeHandlerFake) Overview(context.Context, domainidentity.Principal) (sohaapi.ComputeOverview, error) {
	return sohaapi.ComputeOverview{}, nil
}
func (*computeHandlerFake) ListAccessSources(context.Context, domainidentity.Principal, appcompute.AccessSourceFilter) (sohaapi.ComputeAccessSourceListEnvelope, error) {
	return sohaapi.ComputeAccessSourceListEnvelope{}, nil
}
func (*computeHandlerFake) ListProviders(context.Context, domainidentity.Principal, appcompute.ProviderFilter) (sohaapi.ComputeProviderListEnvelope, error) {
	return sohaapi.ComputeProviderListEnvelope{}, nil
}
func (*computeHandlerFake) ListProviderInstances(context.Context, domainidentity.Principal, appcompute.ProviderInstanceFilter) (sohaapi.ComputeProviderInstanceListEnvelope, error) {
	return sohaapi.ComputeProviderInstanceListEnvelope{}, nil
}
func (*computeHandlerFake) GetProviderInstance(context.Context, domainidentity.Principal, string, string, string) (sohaapi.ComputeProviderInstance, error) {
	return sohaapi.ComputeProviderInstance{}, nil
}
func (f *computeHandlerFake) CheckProviderInstanceHealth(_ context.Context, _ domainidentity.Principal, _, _, _, key string, _ sohaapi.ComputeProviderReadRequest) (sohaapi.ComputeTaskView, error) {
	f.idempotencyKey = key
	return sohaapi.ComputeTaskView{ID: "task-1"}, nil
}
func (*computeHandlerFake) DiscoverProviderInstance(context.Context, domainidentity.Principal, string, string, string, string, sohaapi.ComputeProviderDiscoverRequest) (sohaapi.ComputeTaskView, error) {
	return sohaapi.ComputeTaskView{ID: "task-1"}, nil
}
func (*computeHandlerFake) GetResource(context.Context, domainidentity.Principal, string, string, string) (map[string]any, error) {
	return map[string]any{}, nil
}
func (*computeHandlerFake) ListResourceRelations(context.Context, domainidentity.Principal, string, string, string, string, int) (sohaapi.ComputeResourceRelations, error) {
	return sohaapi.ComputeResourceRelations{}, nil
}
func (*computeHandlerFake) ExecuteResourceAction(context.Context, domainidentity.Principal, string, string, string, string, string, sohaapi.ComputeResourceActionRequest) (sohaapi.ComputeTaskView, error) {
	return sohaapi.ComputeTaskView{ID: "task-1"}, nil
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

func TestComputeProviderMutationRequiresAndForwardsIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &computeHandlerFake{}
	handler := NewComputeHandler(service)
	router := gin.New()
	router.POST("/compute/provider-instances/:domain/:providerKey/:instanceRef/health-checks", handler.CheckProviderInstanceHealth)

	for _, test := range []struct {
		key    string
		status int
	}{
		{status: http.StatusBadRequest},
		{key: "compute-health-1", status: http.StatusAccepted},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/compute/provider-instances/virtualization/pve/connection-1/health-checks", strings.NewReader(`{"expectedGeneration":1}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", test.key)
		router.ServeHTTP(recorder, request)
		if recorder.Code != test.status {
			t.Fatalf("key %q status = %d, body = %s", test.key, recorder.Code, recorder.Body.String())
		}
	}
	if service.idempotencyKey != "compute-health-1" {
		t.Fatalf("idempotency key = %q", service.idempotencyKey)
	}
}
