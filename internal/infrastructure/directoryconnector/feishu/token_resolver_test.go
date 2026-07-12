package feishu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

type loginProviderResolverStub struct {
	provider domainsettings.LoginProviderSettings
}

func (s loginProviderResolverStub) ResolveLoginProvider(context.Context, string) (domainsettings.LoginProviderSettings, error) {
	return s.provider, nil
}

func TestTenantTokenResolverExchangesAndCachesToken(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"tenant-token","expire":7200}`))
	}))
	defer server.Close()
	resolver := NewTenantTokenResolver(loginProviderResolverStub{provider: domainsettings.LoginProviderSettings{Enabled: true, ClientID: "app", ClientSecret: "secret"}}, server.Client(), server.URL)
	connection := domain.Connection{LoginProviderID: "login-1"}
	for range 2 {
		token, err := resolver(context.Background(), connection)
		if err != nil || token != "tenant-token" {
			t.Fatalf("resolve token = %q, %v", token, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", calls.Load())
	}
}
