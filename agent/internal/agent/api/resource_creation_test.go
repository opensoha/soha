package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

func TestValidateResourceCreateRequestBoundsDocuments(t *testing.T) {
	valid := domainresource.KubernetesResourceAgentCreateDocument{
		Document: domainresource.KubernetesResourceDocument{
			Index: 0, ContentHash: strings.Repeat("a", 64), APIVersion: "v1", Kind: "ConfigMap", Name: "app", ScopeMode: domainresource.KubernetesResourceScopeModeNamespace,
		},
		ResourceRef: domainresource.KubernetesResourceRef{ClusterID: "cluster-a", APIVersion: "v1", Kind: "ConfigMap", Name: "app", ScopeMode: domainresource.KubernetesResourceScopeModeNamespace},
		Content:     "apiVersion: v1",
	}
	cases := []struct {
		name    string
		request domainresource.KubernetesResourceAgentCreateRequest
		want    string
	}{
		{name: "missing operation", request: domainresource.KubernetesResourceAgentCreateRequest{}, want: "operationId"},
		{name: "empty", request: domainresource.KubernetesResourceAgentCreateRequest{OperationID: "op"}, want: "at least one"},
		{name: "missing target", request: domainresource.KubernetesResourceAgentCreateRequest{OperationID: "op", Documents: []domainresource.KubernetesResourceAgentCreateDocument{{}}}, want: "requires"},
		{name: "oversized", request: domainresource.KubernetesResourceAgentCreateRequest{OperationID: "op", Documents: []domainresource.KubernetesResourceAgentCreateDocument{{Document: valid.Document, ResourceRef: valid.ResourceRef, Content: strings.Repeat("x", resourceCreateMaxDocumentBytes+1)}}}, want: "size"},
		{name: "valid", request: domainresource.KubernetesResourceAgentCreateRequest{OperationID: "op", Documents: []domainresource.KubernetesResourceAgentCreateDocument{valid}}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateResourceCreateRequest(tc.request); !strings.Contains(got, tc.want) {
				t.Fatalf("validateResourceCreateRequest() = %q, want substring %q", got, tc.want)
			}
		})
	}
}

func TestResourceCreateDeniedWhenActionNotAllowlisted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, zap.NewNop(), &k8sagent.Client{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/platform/resources", bytes.NewBufferString(`{"documents":[{"index":0,"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"app","content":"apiVersion: v1"}]}`))
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "action_not_allowed") {
		t.Fatalf("status = %d body=%s, want action denial", recorder.Code, recorder.Body.String())
	}
}

func TestResourceCreateReturnsCreatedContractResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	platform := router.Group("/api/v1/platform")
	client := &fakeResourceCreationClient{}
	actions := newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: []string{actionPlatformResourcesCreate}}, zap.NewNop(), nil)
	registerResourceCreationRoutes(platform, client, actions)

	input := validAgentCreateRequest()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/platform/resources", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated || !strings.Contains(recorder.Body.String(), `"operationId":"operation-1"`) {
		t.Fatalf("status = %d body=%s, want created contract result", recorder.Code, recorder.Body.String())
	}
	if !client.created {
		t.Fatal("CreateResources() was not called")
	}
}

type fakeResourceCreationClient struct{ created bool }

func (f *fakeResourceCreationClient) PreflightResourceCreate(context.Context, domainresource.KubernetesResourceAgentCreateRequest) domainresource.KubernetesResourceAgentPreflightResult {
	return domainresource.KubernetesResourceAgentPreflightResult{Ready: true, Items: []domainresource.KubernetesResourceAgentPreflightItem{}}
}

func (f *fakeResourceCreationClient) CreateResources(_ context.Context, request domainresource.KubernetesResourceAgentCreateRequest) domainresource.KubernetesResourceAgentCreateResult {
	f.created = true
	return domainresource.KubernetesResourceAgentCreateResult{
		OperationID: request.OperationID, Status: domainresource.KubernetesResourceCreateBatchStatusSucceeded,
		Items: []domainresource.KubernetesResourceCreateResultItem{},
	}
}

func validAgentCreateRequest() domainresource.KubernetesResourceAgentCreateRequest {
	return domainresource.KubernetesResourceAgentCreateRequest{
		OperationID: "operation-1",
		Documents: []domainresource.KubernetesResourceAgentCreateDocument{{
			Document: domainresource.KubernetesResourceDocument{
				Index: 0, ContentHash: strings.Repeat("a", 64), APIVersion: "v1", Kind: "ConfigMap", Name: "app", ScopeMode: domainresource.KubernetesResourceScopeModeNamespace,
			},
			ResourceRef: domainresource.KubernetesResourceRef{
				ClusterID: "cluster-a", APIVersion: "v1", Kind: "ConfigMap", Name: "app", Namespace: "default", ScopeMode: domainresource.KubernetesResourceScopeModeNamespace,
			},
			Content: "apiVersion: v1",
		}},
	}
}
