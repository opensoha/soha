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
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestExecuteResourceCreationRequiresIdempotencyKey(t *testing.T) {
	t.Parallel()
	service := &resourceCreationHandlerStub{}
	ctx, recorder := resourceCreationTestContext(http.MethodPost, `{"source":"global_yaml","content":"apiVersion: v1"}`)
	(&resourceCreationHandler{service: service}).ExecuteResourceCreation(ctx)
	if recorder.Code != http.StatusBadRequest || service.executeCalled {
		t.Fatalf("status=%d executeCalled=%v", recorder.Code, service.executeCalled)
	}
}

func TestPreflightResourceCreationUsesContractWireShape(t *testing.T) {
	t.Parallel()
	service := &resourceCreationHandlerStub{preflight: domainresource.ResourceCreatePreflight{
		Ready: true, ContentHash: "batch-hash", Documents: []domainresource.ResourceCreateDocument{{
			Index: 0, Resource: domainresource.ResourceCreateRef{APIVersion: "v1", Kind: "ConfigMap", Name: "app", Namespace: "minio", Namespaced: true},
			OriginalNamespace: "", DocumentHash: "document-hash", Status: "ready",
			Authorization: domainresource.ResourceCreateCheck{Allowed: true, AllowedActions: []string{"create"}, ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"minio"}},
			Capability:    domainresource.ResourceCreateCapability{Key: "resource.create", Status: "available", Mode: "agent"}, DryRun: domainresource.ResourceDryRunCheck{Valid: true},
		}},
	}}
	ctx, recorder := resourceCreationTestContext(http.MethodPost, `{"source":"global_yaml","defaultNamespace":"minio","content":"apiVersion: v1"}`)
	(&resourceCreationHandler{service: service}).PreflightResourceCreation(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Ready bool `json:"ready"`
			Items []struct {
				ResolvedNamespace string `json:"resolvedNamespace"`
				Capability        struct {
					Mode string `json:"mode"`
				} `json:"capability"`
				Authorization struct {
					AllowedActions []string `json:"allowedActions"`
					ResourceScope  struct {
						Namespaces []string `json:"namespaces"`
					} `json:"resourceScope"`
				} `json:"authorization"`
				Document struct {
					Index int `json:"index"`
				} `json:"document"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Data.Ready || len(payload.Data.Items) != 1 || payload.Data.Items[0].Document.Index != 0 || payload.Data.Items[0].ResolvedNamespace != "minio" ||
		payload.Data.Items[0].Capability.Mode != "agent" || len(payload.Data.Items[0].Authorization.AllowedActions) != 1 || len(payload.Data.Items[0].Authorization.ResourceScope.Namespaces) != 1 {
		t.Fatalf("response = %#v", payload)
	}
}

func TestPreflightResourceCreationBoundsRequestBody(t *testing.T) {
	t.Parallel()
	service := &resourceCreationHandlerStub{}
	body := `{"source":"global_yaml","content":"` + strings.Repeat("x", domainresource.ResourceCreateMaxBodyBytes+(64<<10)) + `"}`
	ctx, recorder := resourceCreationTestContext(http.MethodPost, body)
	(&resourceCreationHandler{service: service}).PreflightResourceCreation(ctx)
	if recorder.Code != http.StatusBadRequest || service.preflightCalled {
		t.Fatalf("status=%d preflightCalled=%v", recorder.Code, service.preflightCalled)
	}
}

func TestMapResourceCreateExecutionKeepsReplayHashAndRuntimeCodes(t *testing.T) {
	t.Parallel()
	contentHash := strings.Repeat("a", 64)
	result := mapResourceCreateExecution("cluster-a", domainresource.ResourceCreateExecution{
		OperationID: "operation-a", ContentHash: contentHash, Status: "failed",
		Documents: []domainresource.ResourceCreateExecutionDocument{{
			Index: 0, Resource: domainresource.ResourceCreateRef{APIVersion: "v1", Kind: "ConfigMap", Name: "app", Namespace: "minio", Namespaced: true},
			Status: "failed", ErrorCode: "resource_already_exists", Error: "already exists",
		}},
	})
	if len(result.Items) != 1 || result.Items[0].Document.ContentHash != contentHash || result.Items[0].Error == nil ||
		result.Items[0].Error.Code != sohaapi.KubernetesResourceCreateErrorCodeResourceAlreadyExists {
		t.Fatalf("mapped execution = %#v", result)
	}
	if got := contractResourceCreateErrorCode("resource_create_failed"); got != sohaapi.KubernetesResourceCreateErrorCodeResourceCreateFailed {
		t.Fatalf("runtime failure code = %q", got)
	}
}

func resourceCreationTestContext(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, "/api/v1/clusters/cluster-a/resource-creation/preflight", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "clusterID", Value: "cluster-a"}}
	return ctx, recorder
}

type resourceCreationHandlerStub struct {
	preflight       domainresource.ResourceCreatePreflight
	preflightCalled bool
	executeCalled   bool
}

func (s *resourceCreationHandlerStub) PreflightCreate(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateRequest) (domainresource.ResourceCreatePreflight, error) {
	s.preflightCalled = true
	return s.preflight, nil
}

func (s *resourceCreationHandlerStub) ExecuteCreate(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateRequest) (domainresource.ResourceCreateExecution, error) {
	s.executeCalled = true
	return domainresource.ResourceCreateExecution{}, nil
}

func (s *resourceCreationHandlerStub) DecideCreateScope(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateScopeDecisionRequest) (domainresource.ResourceCreateScopeDecision, error) {
	return domainresource.ResourceCreateScopeDecision{}, nil
}
