package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type capabilityTaskHandlerStub struct {
	AIGatewayTaskService
	limit    int
	revision domainaigateway.CapabilityTaskRevisionInput
}

func (s *capabilityTaskHandlerStub) ListCapabilityTasks(_ context.Context, _ domainidentity.Principal, limit int) ([]domainaigateway.CapabilityTask, error) {
	s.limit = limit
	return []domainaigateway.CapabilityTask{{ID: "task-1", Status: "blocked"}}, nil
}
func (s *capabilityTaskHandlerStub) ResumeCapabilityTask(_ context.Context, _ domainidentity.Principal, id string, input domainaigateway.CapabilityTaskRevisionInput) (domainaigateway.CapabilityTask, error) {
	s.revision = input
	return domainaigateway.CapabilityTask{ID: id, Version: input.ExpectedVersion + 1, PlanVersion: 2, Plan: input.Plan}, nil
}

func TestCapabilityTaskHandlerMatchesGeneratedListContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &capabilityTaskHandlerStub{}
	handler := aiGatewayTaskHandler{tasks: service}
	router := gin.New()
	router.GET("/tasks", handler.ListCapabilityTasks)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tasks?limit=12", nil))
	var envelope sohaapi.CapabilityTaskListEnvelope
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || len(envelope.Items) != 1 || envelope.Items[0].ID != "task-1" || service.limit != 12 {
		t.Fatalf("list contract mismatch: %d %s", response.Code, response.Body.String())
	}
	for _, limit := range []string{"0", "101", "invalid"} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tasks?limit="+limit, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid limit accepted: %s", limit)
		}
	}
}

func TestCapabilityTaskHandlerPreservesRevisionFence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &capabilityTaskHandlerStub{}
	handler := aiGatewayTaskHandler{tasks: service}
	router := gin.New()
	router.POST("/tasks/:taskId/resume", handler.ResumeCapabilityTask)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/tasks/task-1/resume", strings.NewReader(`{"expectedVersion":9,"plan":{"goal":"same goal","steps":[],"verificationSteps":[]}}`)))
	var envelope sohaapi.CapabilityTaskEnvelope
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Data.Version != 10 || service.revision.ExpectedVersion != 9 {
		t.Fatalf("revision contract: %s", response.Body.String())
	}
}
