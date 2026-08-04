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
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
)

type captureSecretService struct{ created domainsecret.CreateInput }

func (s *captureSecretService) List(context.Context, domainidentity.Principal, domainsecret.Filter) ([]sohaapi.SecretMetadata, error) {
	return nil, nil
}
func (s *captureSecretService) Get(context.Context, domainidentity.Principal, string) (sohaapi.SecretMetadata, error) {
	return sohaapi.SecretMetadata{}, nil
}
func (s *captureSecretService) Create(_ context.Context, _ domainidentity.Principal, input domainsecret.CreateInput) (sohaapi.SecretMetadata, error) {
	s.created = input
	return sohaapi.SecretMetadata{ID: "secret-1", Bindings: []sohaapi.SecretBinding{}, CreatedAt: time.Unix(0, 0), UpdatedAt: time.Unix(0, 0)}, nil
}
func (s *captureSecretService) Update(context.Context, domainidentity.Principal, string, domainsecret.UpdateInput) (sohaapi.SecretMetadata, error) {
	return sohaapi.SecretMetadata{}, nil
}
func (s *captureSecretService) Disable(context.Context, domainidentity.Principal, string) (sohaapi.SecretMetadata, error) {
	return sohaapi.SecretMetadata{}, nil
}
func (s *captureSecretService) ListVersions(context.Context, domainidentity.Principal, string) ([]sohaapi.SecretVersionMetadata, error) {
	return nil, nil
}
func (s *captureSecretService) Rotate(context.Context, domainidentity.Principal, string, domainsecret.RotateInput) (sohaapi.SecretVersionMetadata, error) {
	return sohaapi.SecretVersionMetadata{}, nil
}
func (s *captureSecretService) RevokeVersion(context.Context, domainidentity.Principal, string, int) (sohaapi.SecretVersionMetadata, error) {
	return sohaapi.SecretVersionMetadata{}, nil
}
func (s *captureSecretService) RedeemLease(context.Context, string, string, string) (sohaapi.SecretLeaseRedemption, error) {
	return sohaapi.SecretLeaseRedemption{}, nil
}

func TestSecretHandlerAcceptsVaultKV2CreateRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &captureSecretService{}
	router := gin.New()
	router.POST("/secrets", NewSecretHandler(service).Create)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/secrets", bytes.NewBufferString(`{
		"name":"vault-token","scopeType":"project","scopeId":"demo",
		"bindings":[{"targetType":"capability","targetRef":"docker.project.deploy"}],
		"vaultKv2":{"mount":"team/kv","path":"demo/app","key":"token","version":7}
	}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || service.created.VaultKV2 == nil {
		t.Fatalf("status=%d input=%#v body=%s", recorder.Code, service.created, recorder.Body.String())
	}
	if got := *service.created.VaultKV2; got.Mount != "team/kv" || got.Path != "demo/app" || got.Key != "token" || got.Version != 7 {
		t.Fatalf("Vault reference = %#v", got)
	}
}
