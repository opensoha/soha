package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type resourceCreationScopeStub struct {
	clusterID string
	input     domainresource.ResourceCreateScopeDecisionRequest
	result    domainresource.ResourceCreateScopeDecision
}

func (s *resourceCreationScopeStub) DecideCreateScope(_ context.Context, _ domainidentity.Principal, clusterID string, input domainresource.ResourceCreateScopeDecisionRequest) (domainresource.ResourceCreateScopeDecision, error) {
	s.clusterID = clusterID
	s.input = input
	return s.result, nil
}

func (*resourceCreationScopeStub) PreflightCreate(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateRequest) (domainresource.ResourceCreatePreflight, error) {
	return domainresource.ResourceCreatePreflight{}, nil
}

func (*resourceCreationScopeStub) ExecuteCreate(context.Context, domainidentity.Principal, string, domainresource.ResourceCreateRequest) (domainresource.ResourceCreateExecution, error) {
	return domainresource.ResourceCreateExecution{}, nil
}

func TestDecideResourceCreationScopeMapsContractResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &resourceCreationScopeStub{result: domainresource.ResourceCreateScopeDecision{
		Allowed: true, Reason: "allowed", AllowedActions: []string{"create"},
		ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"minio"},
		ResourceGroups: []string{"configuration"}, ResourceKinds: []string{"ConfigMap"},
		Capability: domainresource.ResourceCreateCapability{Key: "resource.create", Status: "available", Mode: "direct"},
	}}
	handler := &resourceCreationHandler{service: service}
	router := gin.New()
	router.POST("/clusters/:clusterID/resource-creation/scope-decision", func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{UserID: "user-1"})
		handler.DecideResourceCreationScope(c)
	})
	body := bytes.NewBufferString(`{"action":"create","namespace":"minio","resourceGroup":"configuration","apiVersion":"v1","kind":"ConfigMap"}`)
	request := httptest.NewRequest(http.MethodPost, "/clusters/cluster-a/resource-creation/scope-decision", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if service.clusterID != "cluster-a" || service.input.Namespace != "minio" || service.input.Kind != "ConfigMap" {
		t.Fatalf("service request = cluster %q input %#v", service.clusterID, service.input)
	}
	var envelope struct {
		Data struct {
			Allowed    bool `json:"allowed"`
			Capability struct {
				Status string `json:"status"`
			} `json:"capability"`
			ResourceScope struct {
				Namespaces []string `json:"namespaces"`
			} `json:"resourceScope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Allowed || envelope.Data.Capability.Status != "available" || len(envelope.Data.ResourceScope.Namespaces) != 1 {
		t.Fatalf("response = %#v", envelope.Data)
	}
}

func TestDecideResourceCreationScopeRejectsNonCreateAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &resourceCreationHandler{service: &resourceCreationScopeStub{}}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"action":"delete","resourceGroup":"configuration","kind":"ConfigMap"}`))
	context.Request.Header.Set("Content-Type", "application/json")
	handler.DecideResourceCreationScope(context)
	if context.Writer.Status() != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", context.Writer.Status())
	}
}
