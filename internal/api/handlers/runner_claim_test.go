package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestRunnerClaimResponses(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		handler func(error) gin.HandlerFunc
	}{
		{
			name: "delivery",
			body: `{"agentId":"agent-1","providerKinds":["ci_agent_runner"]}`,
			handler: func(err error) gin.HandlerFunc {
				service := &stubDeliveryRunnerService{item: domaindelivery.ExecutionTask{ID: "task-1"}, err: err}
				return NewDeliveryHandlerWithServices(
					DeliveryServices{Runner: service}, legacyRunnerKeyring("runner-token"),
				).ClaimExecutionTask
			},
		},
		{
			name: "docker",
			body: `{"workerId":"worker-1","agentId":"agent-1","operationKinds":["host_sync"]}`,
			handler: func(err error) gin.HandlerFunc {
				service := &stubDockerRunnerOperationService{item: domaindocker.Operation{ID: "operation-1"}, err: err}
				return NewDockerHandlerWithServices(
					DockerServices{RunnerOperations: service}, legacyRunnerKeyring("runner-token"),
				).ClaimOperation
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("empty queue", func(t *testing.T) {
				status, errorsCount := invokeRunnerClaim(t, tt.handler(apperrors.ErrNotFound), tt.body)
				if status != http.StatusNoContent {
					t.Fatalf("status = %d, want %d", status, http.StatusNoContent)
				}
				if errorsCount != 0 {
					t.Fatalf("Gin errors = %d, want 0", errorsCount)
				}
			})

			t.Run("claimed", func(t *testing.T) {
				status, errorsCount := invokeRunnerClaim(t, tt.handler(nil), tt.body)
				if status != http.StatusAccepted {
					t.Fatalf("status = %d, want %d", status, http.StatusAccepted)
				}
				if errorsCount != 0 {
					t.Fatalf("Gin errors = %d, want 0", errorsCount)
				}
			})

			t.Run("unexpected error", func(t *testing.T) {
				status, errorsCount := invokeRunnerClaim(t, tt.handler(errors.New("storage unavailable")), tt.body)
				if status != http.StatusInternalServerError {
					t.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
				}
				if errorsCount != 1 {
					t.Fatalf("Gin errors = %d, want 1", errorsCount)
				}
			})
		})
	}
}

func invokeRunnerClaim(t *testing.T, handler gin.HandlerFunc, body string) (int, int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/claim", strings.NewReader(body))
	ctx.Request.Header.Set("Authorization", "Bearer runner-token")
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler(ctx)
	ctx.Writer.WriteHeaderNow()
	return recorder.Code, len(ctx.Errors)
}

type stubDeliveryRunnerService struct {
	item domaindelivery.ExecutionTask
	err  error
}

func (s *stubDeliveryRunnerService) GetExecutionTaskForRunner(context.Context, string) (domaindelivery.ExecutionTask, error) {
	return s.item, s.err
}

func (s *stubDeliveryRunnerService) RecordCallback(context.Context, domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionTask, error) {
	return s.item, s.err
}

func (s *stubDeliveryRunnerService) ClaimExecutionTask(context.Context, []string, string, string) (domaindelivery.ExecutionTask, error) {
	return s.item, s.err
}

type stubDockerRunnerOperationService struct {
	item domaindocker.Operation
	err  error
}

func (s *stubDockerRunnerOperationService) ClaimOperation(context.Context, domaindocker.OperationClaimInput) (domaindocker.Operation, error) {
	return s.item, s.err
}

func (s *stubDockerRunnerOperationService) GetOperationForRunner(context.Context, string) (domaindocker.Operation, error) {
	return s.item, s.err
}

func (s *stubDockerRunnerOperationService) RecordOperationCallback(context.Context, domaindocker.OperationCallbackInput) (domaindocker.Operation, error) {
	return s.item, s.err
}
