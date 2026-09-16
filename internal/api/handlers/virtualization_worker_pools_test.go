package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

type workerPoolHandlerStub struct {
	VirtualizationWorkerPoolService
	calls    int
	revision int
}

func (s *workerPoolHandlerStub) SaveWorkerPool(_ context.Context, _ identity.Principal, id string, input sohaapi.VirtualizationWorkerPoolInput) (sohaapi.VirtualizationWorkerPool, error) {
	s.calls++
	s.revision = input.ExpectedRevision
	return sohaapi.VirtualizationWorkerPool{ID: uuid.MustParse(id), Revision: input.ExpectedRevision + 1, Spec: input.Spec}, nil
}
func (s *workerPoolHandlerStub) DeleteWorkerPool(_ context.Context, _ identity.Principal, _ string, revision int) error {
	s.calls++
	s.revision = revision
	return nil
}
func (s *workerPoolHandlerStub) CreateWorker(_ context.Context, _ identity.Principal, _ string, input sohaapi.VirtualizationWorkerCreateInput) (domain.Task, error) {
	s.calls++
	s.revision = input.PoolRevision
	return domain.Task{ID: "original-operation", TaskKind: "vm_create", Status: "queued", Payload: map[string]any{"gatewayAuthorizationCredential": "sealed-private", "workerCloudInitCredential": "sealed-bootstrap"}}, nil
}
func TestWorkerPoolRequestBoundaries(t *testing.T) {
	id := uuid.NewString()
	for _, tc := range []struct {
		name, method, path, body string
		status, calls            int
	}{
		{"save", "PUT", "/pools/" + id, `{"expectedRevision":2,"spec":{}}`, 200, 1},
		{"missing revision", "PUT", "/pools/" + id, `{"spec":{}}`, 400, 0},
		{"missing spec", "PUT", "/pools/" + id, `{"expectedRevision":0}`, 400, 0},
		{"unknown nested field", "PUT", "/pools/" + id, `{"expectedRevision":0,"spec":{"token":"forbidden"}}`, 400, 0},
		{"two bodies", "PUT", "/pools/" + id, `{"expectedRevision":0,"spec":{}} {}`, 400, 0},
		{"create", "POST", "/pools/" + id + "/nodes", `{"poolRevision":2,"idempotencyKey":"original-key"}`, 202, 1},
		{"injected bootstrap", "POST", "/pools/" + id + "/nodes", `{"poolRevision":2,"idempotencyKey":"original-key","token":"forbidden"}`, 400, 0},
		{"delete", "DELETE", "/pools/" + id + "?expectedRevision=2", "", 204, 1},
		{"delete without revision", "DELETE", "/pools/" + id, "", 400, 0},
		{"delete invalid revision", "DELETE", "/pools/" + id + "?expectedRevision=0", "", 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &workerPoolHandlerStub{}
			handler := &VirtualizationHandler{workerPools: service}
			router := gin.New()
			router.PUT("/pools/:id", handler.SaveWorkerPool)
			router.POST("/pools/:id/nodes", handler.CreateWorker)
			router.DELETE("/pools/:id", handler.DeleteWorkerPool)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			if response.Code != tc.status || service.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
			}
			if service.calls > 0 && service.revision != 2 {
				t.Fatalf("revision lost: %d", service.revision)
			}
			if strings.Contains(response.Body.String(), "sealed-private") || strings.Contains(response.Body.String(), "sealed-bootstrap") {
				t.Fatal("protected task payload leaked")
			}
		})
	}
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/pools", nil)
	(&VirtualizationHandler{}).ListWorkerPools(ctx)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired pool status=%d", response.Code)
	}
}
