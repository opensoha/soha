package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
)

type systemIntegrationHandlerStub struct{ update domain.UpdateInput }

func (*systemIntegrationHandlerStub) List(context.Context, domainidentity.Principal, domain.Filter) ([]sohaapi.SystemIntegration, error) {
	return []sohaapi.SystemIntegration{}, nil
}
func (*systemIntegrationHandlerStub) Get(context.Context, domainidentity.Principal, string) (sohaapi.SystemIntegration, error) {
	return sohaapi.SystemIntegration{}, nil
}
func (*systemIntegrationHandlerStub) Create(context.Context, domainidentity.Principal, sohaapi.SystemIntegrationCreateRequest) (sohaapi.SystemIntegration, error) {
	return sohaapi.SystemIntegration{}, nil
}
func (s *systemIntegrationHandlerStub) Update(_ context.Context, _ domainidentity.Principal, _ string, input domain.UpdateInput) (sohaapi.SystemIntegration, error) {
	s.update = input
	return sohaapi.SystemIntegration{ID: "gitlab-1", Category: sohaapi.SystemIntegrationCategorySourceControl, ProviderType: "gitlab", Name: "GitLab", Enabled: input.Enabled != nil && *input.Enabled, Configuration: []sohaapi.SystemIntegrationConfigurationField{}, CredentialKeys: []string{"token"}, HealthStatus: sohaapi.SystemIntegrationHealthStatusUnknown, Version: 2, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}
func (*systemIntegrationHandlerStub) Delete(context.Context, domainidentity.Principal, string) error {
	return nil
}
func (*systemIntegrationHandlerStub) Test(context.Context, domainidentity.Principal, string) (sohaapi.SystemIntegrationTestResult, error) {
	return sohaapi.SystemIntegrationTestResult{}, nil
}
func (*systemIntegrationHandlerStub) ListSourceConnections(context.Context, domainidentity.Principal) ([]sohaapi.SourceConnection, error) {
	return nil, nil
}
func (*systemIntegrationHandlerStub) GetSourceConnection(context.Context, domainidentity.Principal, string) (sohaapi.SourceConnection, error) {
	return sohaapi.SourceConnection{}, nil
}
func (*systemIntegrationHandlerStub) ListSourceRepositories(context.Context, domainidentity.Principal, string, string, string, int) ([]sohaapi.SourceRepository, string, error) {
	return nil, "", nil
}
func (*systemIntegrationHandlerStub) ListSourceBranches(context.Context, domainidentity.Principal, string, string) ([]sohaapi.SourceBranch, error) {
	return nil, nil
}
func (*systemIntegrationHandlerStub) ListSourceTags(context.Context, domainidentity.Principal, string, string) ([]sohaapi.SourceTag, error) {
	return nil, nil
}
func (*systemIntegrationHandlerStub) GetSourceFile(context.Context, domainidentity.Principal, string, string, string, string) (sohaapi.SourceFile, error) {
	return sohaapi.SourceFile{}, nil
}

func TestSystemIntegrationUpdatePreservesExplicitFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &systemIntegrationHandlerStub{}
	handler := NewSystemIntegrationHandler(service)
	router := gin.New()
	router.PATCH("/system-integrations/:integrationID", handler.Update)
	request := httptest.NewRequest(http.MethodPatch, "/system-integrations/gitlab-1", bytes.NewBufferString(`{"expectedVersion":1,"enabled":false}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.update.Enabled == nil || *service.update.Enabled {
		t.Fatalf("explicit false was not preserved: %#v", service.update.Enabled)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("credentialValue")) {
		t.Fatalf("response exposed credentials: %s", response.Body.String())
	}
}
