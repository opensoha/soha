package providerportal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
)

type stubOIDCClientSecretService struct {
	OIDCClientService
	create func(context.Context, domainidentity.Principal, string, domainprovider.OIDCClientInput) (domainprovider.OIDCClientCreated, error)
	reveal func(context.Context, domainidentity.Principal, string) (domainprovider.OIDCClientSecretReveal, error)
}

func (s stubOIDCClientSecretService) CreateOIDCClient(ctx context.Context, principal domainidentity.Principal, providerID string, input domainprovider.OIDCClientInput) (domainprovider.OIDCClientCreated, error) {
	return s.create(ctx, principal, providerID, input)
}

func (s stubOIDCClientSecretService) RevealOIDCClientSecret(ctx context.Context, principal domainidentity.Principal, clientID string) (domainprovider.OIDCClientSecretReveal, error) {
	return s.reveal(ctx, principal, clientID)
}

func TestRevealOIDCClientSecretDisablesCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := stubOIDCClientSecretService{
		reveal: func(_ context.Context, _ domainidentity.Principal, clientID string) (domainprovider.OIDCClientSecretReveal, error) {
			if clientID != "oidc-client-1" {
				t.Fatalf("Client ID path = %q", clientID)
			}
			return domainprovider.OIDCClientSecretReveal{
				ClientID:     "generated-client-id",
				ClientSecret: "revealable-client-secret",
				RevealedAt:   time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	handler := New(Services{OIDCClients: service})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "clientID", Value: "oidc-client-1"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/identity/oidc-clients/oidc-client-1/secret/reveal", nil)

	handler.RevealOIDCClientSecret(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"clientSecret":"revealable-client-secret"`) {
		t.Fatalf("response body = %s", body)
	}
}

func TestCreateOIDCClientSecretDisablesCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := stubOIDCClientSecretService{
		create: func(_ context.Context, _ domainidentity.Principal, _ string, _ domainprovider.OIDCClientInput) (domainprovider.OIDCClientCreated, error) {
			return domainprovider.OIDCClientCreated{
				Client:       domainprovider.OIDCClient{ID: "client-1", ClientID: "generated-client-id"},
				ClientSecret: "generated-client-secret",
			}, nil
		},
	}
	handler := New(Services{OIDCClients: service})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "providerID", Value: "provider-1"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/identity/providers/provider-1/oidc-clients", strings.NewReader(`{"redirectUris":["https://app.example.com/oauth/callback"]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.CreateOIDCClient(c)

	if recorder.Code != http.StatusCreated || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = %d Cache-Control=%q", recorder.Code, recorder.Header().Get("Cache-Control"))
	}
}
