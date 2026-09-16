package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestDockerRunnerClaimReturnsTokenWithoutExposingItOnOperationJSON(t *testing.T) {
	const token = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	item := domaindocker.Operation{ID: "operation-1", CallbackToken: token}
	handler := NewDockerHandlerWithServices(
		DockerServices{RunnerOperations: &stubDockerRunnerOperationService{item: item}}, legacyRunnerKeyring("runner-token"),
	).ClaimOperation

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/claim", strings.NewReader(`{"workerId":"worker-1","operationKinds":["host_sync"],"callbackTokenSupported":true}`))
	ctx.Request.Header.Set("Authorization", "Bearer runner-token")
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler(ctx)

	if recorder.Code != http.StatusAccepted || !strings.Contains(recorder.Body.String(), `"callbackToken":"`+token+`"`) {
		t.Fatalf("claim response = %d %s, want callback token", recorder.Code, recorder.Body.String())
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal operation: %v", err)
	}
	if strings.Contains(string(raw), "callbackToken") || strings.Contains(string(raw), token) {
		t.Fatalf("operation JSON exposed callback token: %s", raw)
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

func (s *stubDockerRunnerOperationService) GetOperationForRunner(context.Context, string, domaindocker.RunnerAuthorization) (domaindocker.Operation, error) {
	return s.item, s.err
}

func (s *stubDockerRunnerOperationService) RecordOperationCallback(context.Context, domaindocker.OperationCallbackInput) (domaindocker.Operation, error) {
	return s.item, s.err
}

type helmClaimAuthenticator struct{}

func (helmClaimAuthenticator) AuthenticateAgentExecution(_ context.Context, clusterID, token string) error {
	if clusterID == "one" && token == "cluster-token" {
		return nil
	}
	return apperrors.ErrUnauthorized
}

type capturedHelmClaim struct {
	stubDeliveryRunnerService
	providers []string
}

func (s *capturedHelmClaim) ClaimExecutionTask(_ context.Context, providers []string, _, _ string) (domaindelivery.ExecutionTask, error) {
	s.providers = providers
	return domaindelivery.ExecutionTask{ID: "claimed"}, nil
}

func TestHelmClaimConfidentialInputsRequireClusterCredentials(t *testing.T) {
	for _, test := range []struct {
		name, token     string
		providers, want []string
		duplicate       bool
	}{
		{name: "general credential", token: "runner-token", providers: []string{"helm_direct", "helm_agent.one", "ci_agent_runner"}, want: []string{"ci_agent_runner"}},
		{name: "cluster credential", token: "cluster-token", providers: []string{"helm_direct", "helm_agent.one", "helm_agent.two", "manifest_agent.one", "manifest_agent_v3.one", "manifest_agent_v3.two", "ci_agent_runner"}, want: []string{"helm_agent.one", "manifest_agent.one", "manifest_agent_v3.one"}},
		{name: "wrong credential", token: "wrong", providers: []string{"helm_agent.one"}},
		{name: "direct forbidden", token: "runner-token", providers: []string{"helm_direct"}},
		{name: "duplicate authorization", token: "cluster-token", providers: []string{"helm_agent.one"}, duplicate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &capturedHelmClaim{}
			h := NewDeliveryHandlerWithServices(DeliveryServices{Runner: service, Agents: helmClaimAuthenticator{}}, legacyRunnerKeyring("runner-token"))
			body, _ := json.Marshal(map[string]any{"agentId": "arbitrary-untrusted-name", "providerKinds": test.providers})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/claim", strings.NewReader(string(body)))
			c.Request.Header.Set("Authorization", "Bearer "+test.token)
			c.Request.Header.Set("Content-Type", "application/json")
			if test.duplicate {
				c.Request.Header.Add("Authorization", "Bearer cluster-token")
			}
			h.ClaimExecutionTask(c)
			wantStatus := http.StatusAccepted
			if test.want == nil {
				wantStatus = http.StatusUnauthorized
			}
			if w.Code != wantStatus || !reflect.DeepEqual(service.providers, test.want) {
				t.Fatalf("status=%d providers=%v want=%v", w.Code, service.providers, test.want)
			}
		})
	}
}
