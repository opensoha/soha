package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type deploymentManagementStub struct {
	calls int
	input api.NetworkProxyDeploymentControlInput
}

func (s *deploymentManagementStub) View(context.Context, identity.Principal, string) (api.NetworkProxyDeployment, error) {
	s.calls++
	return api.NetworkProxyDeployment{Members: []api.NetworkProxyDeploymentMember{}, State: "unbound"}, nil
}
func (s *deploymentManagementStub) BindRequest(context.Context, identity.Principal, string, api.NetworkProxyDeploymentInput) (api.NetworkProxyDeployment, error) {
	s.calls++
	return api.NetworkProxyDeployment{}, nil
}
func (s *deploymentManagementStub) ControlRequest(_ context.Context, _ identity.Principal, _ string, input api.NetworkProxyDeploymentControlInput) (api.NetworkProxyDeploymentOperation, error) {
	s.calls++
	s.input = input
	return api.NetworkProxyDeploymentOperation{OperationID: "durable-task", RequestID: input.RequestID, DesiredRevision: 2, Status: "pending"}, nil
}

type deploymentExecutorStub struct{ calls int }

func (s *deploymentExecutorStub) Claim(context.Context, string, string) (delivery.ExecutionTask, error) {
	s.calls++
	return delivery.ExecutionTask{ID: "task"}, nil
}
func (s *deploymentExecutorStub) Status(context.Context, string, string) (delivery.ExecutionTask, error) {
	s.calls++
	return delivery.ExecutionTask{}, nil
}
func (s *deploymentExecutorStub) Callback(context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	s.calls++
	return delivery.ExecutionTask{}, nil
}
func TestProxyDeploymentHTTPBoundsMutationWireAndExecutorIdentity(t *testing.T) {
	management, executor := &deploymentManagementStub{}, &deploymentExecutorStub{}
	handler := NewProxyDeploymentHandler(management, executor)
	router := gin.New()
	router.POST("/proxy/:instanceID/control", handler.Control)
	router.POST("/executor/claim", handler.Claim)
	for _, tc := range []struct {
		name, body string
		status     int
	}{{"valid", `{"action":"stop","expectedRevision":1,"requestId":"req"}`, 202}, {"arbitrary kind", `{"action":"stop","expectedRevision":1,"requestId":"req","workloadKind":"StatefulSet"}`, 400}, {"trailing", `{"action":"stop","expectedRevision":1,"requestId":"req"} {}`, 400}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/proxy/p1/control", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			if tc.status == 202 && !strings.Contains(rec.Body.String(), "durable-task") {
				t.Fatal("missing durable operation")
			}
		})
	}
	if management.calls != 1 || management.input.DrainSeconds != 0 {
		t.Fatal("invalid mutation delegated or defaults rewritten by transport")
	}
	req := httptest.NewRequest(http.MethodPost, "/executor/claim", strings.NewReader(`{"executorId":"exec"}`))
	req.Header.Add("Authorization", "Bearer first")
	req.Header.Add("Authorization", "Bearer second")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 401 || executor.calls != 0 {
		t.Fatal("ambiguous executor credential accepted")
	}
}
