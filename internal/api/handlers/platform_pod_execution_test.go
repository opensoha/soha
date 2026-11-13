package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	contractresource "github.com/opensoha/soha/contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type podExecutionHandlerSpy struct {
	PodDiagnostics
	calls                   int
	input                   contractresource.PodArgvInput
	key, executionID, actor string
	access                  domainidentity.AccessContext
}

func (s *podExecutionHandlerSpy) ExecutePodArgv(_ context.Context, p domainidentity.Principal, a domainidentity.AccessContext, _, _, _, key string, input contractresource.PodArgvInput) (contractresource.PodExecutionReceipt, error) {
	s.calls++
	s.input = input
	s.key = key
	s.actor = p.UserID
	s.access = a
	return contractresource.PodExecutionReceipt{Status: "running"}, nil
}
func (s *podExecutionHandlerSpy) GetPodExecution(_ context.Context, p domainidentity.Principal, _, _, _, id, key string) (contractresource.PodExecutionReceipt, error) {
	s.calls++
	s.executionID = id
	s.key = key
	s.actor = p.UserID
	return contractresource.PodExecutionReceipt{Status: "unknown"}, nil
}

func TestPodExecutionHandlerStrictInputAndTrustedAuthenticationContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"valid", "missing-key", "duplicate-key", "unknown-field", "trailing", "missing-uid"} {
		t.Run(scenario, func(t *testing.T) {
			spy := &podExecutionHandlerSpy{}
			handler := &podResourceHandler{diagnostics: spy}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("principal", domainidentity.Principal{UserID: "trusted-actor"})
				c.Set("access_context", domainidentity.AccessContext{SessionID: "trusted-session", AuthzVersion: 3})
			})
			router.POST("/clusters/:clusterID/workloads/pods/:podName/executions", handler.ExecutePodArgv)
			body := `{"expectedUid":"reviewed","argv":["printf","$(literal)"]}`
			if scenario == "unknown-field" {
				body = strings.TrimSuffix(body, "}") + `,"actor":"attacker"}`
			}
			if scenario == "trailing" {
				body += " {}"
			}
			if scenario == "missing-uid" {
				body = `{"argv":["true"]}`
			}
			request := httptest.NewRequest("POST", "/clusters/c/workloads/pods/app/executions?namespace=test", strings.NewReader(body))
			if scenario != "missing-key" {
				request.Header.Add("Idempotency-Key", "original")
			}
			if scenario == "duplicate-key" {
				request.Header.Add("Idempotency-Key", "another")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if scenario == "valid" {
				if response.Code != 202 || spy.calls != 1 || spy.actor != "trusted-actor" || spy.key != "original" || spy.access.AuthzVersion != 3 || spy.input.Argv[1] != "$(literal)" || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("invalid transport: %d %+v", response.Code, spy)
				}
			} else if response.Code != 400 || spy.calls != 0 {
				t.Fatalf("invalid payload reached service: %d %d", response.Code, spy.calls)
			}
		})
	}
}
func TestPodExecutionReceiptReadsDoNotInvokeExec(t *testing.T) {
	gin.SetMode(gin.TestMode)
	spy := &podExecutionHandlerSpy{}
	handler := &podResourceHandler{diagnostics: spy}
	router := gin.New()
	router.GET("/pods/:podName/executions", handler.GetPodExecution)
	request := httptest.NewRequest(http.MethodGet, "/pods/app/executions?namespace=test", nil)
	request.Header.Set("Idempotency-Key", "original")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 || spy.calls != 1 || spy.executionID != "" || spy.key != "original" || spy.input.Argv != nil {
		t.Fatalf("receipt read executed argv: %d %+v", response.Code, spy)
	}
}
