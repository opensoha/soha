package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	"github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type sshAgentUserParser struct{}

func (sshAgentUserParser) ParseAccessToken(context.Context, string) (identity.Principal, identity.AccessContext, error) {
	return identity.Principal{}, identity.AccessContext{}, apperrors.ErrUnauthorized
}

type sshAgentRoutesService struct {
	apiHandlers.VirtualizationGuestAccessService
}

func (sshAgentRoutesService) CheckVMSSHCommandAuthorization(_ context.Context, id, token string, input sohaapi.VMSSHCredentialRedemptionInput) (sohaapi.VirtualMachineSSHCommand, error) {
	if id != "session-one" || token != "fixture-agent-token" || input.AgentID != "agent-one" || input.ClusterID != "cluster-one" || input.LeaseID != "lease-one" || input.Token != "fixture-single-use-token" {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrAccessDenied
	}
	return sohaapi.VirtualMachineSSHCommand{ID: id}, nil
}

func (service sshAgentRoutesService) RedeemVMSSHCredentials(ctx context.Context, id, token string, input sohaapi.VMSSHCredentialRedemptionInput) (sohaapi.SecretLeaseRedemption, error) {
	_, err := service.CheckVMSSHCommandAuthorization(ctx, id, token, input)
	return sohaapi.SecretLeaseRedemption{}, err
}

func TestVMSSHAgentCallbacksUseAgentAuthenticationAndKeepBrowserRoutesProtected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(apiMiddleware.BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, sshAgentUserParser{}))
	deps := Dependencies{System: &apiHandlers.SystemHandler{}, Auth: &apiHandlers.AuthHandler{}, Virtualization: apiHandlers.NewVirtualizationHandlerWithServices(apiHandlers.VirtualizationServices{GuestAccess: sshAgentRoutesService{}})}
	registerPublicRoutes(router.Group("/api/v1"), cfgpkg.Config{}, deps)
	protected := router.Group("/api/v1", apiMiddleware.RequireAuth())
	registerVirtualizationRoutes(protected, deps)
	for _, suffix := range []string{"credentials", "execution-status"} {
		for _, token := range []string{"fixture-agent-token", "wrong-agent-token", ""} {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/virtualization/ssh-sessions/session-one/"+suffix, strings.NewReader(`{"clusterId":"cluster-one","agentId":"agent-one","leaseId":"lease-one","token":"fixture-single-use-token"}`))
			request.Header.Set("Content-Type", "application/json")
			if token != "" {
				request.Header.Set("Authorization", "Bearer "+token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := http.StatusForbidden
			if token == "fixture-agent-token" {
				want = http.StatusOK
			}
			if response.Code != want {
				t.Fatalf("%s callback status=%d want=%d", suffix, response.Code, want)
			}
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/virtualization/ssh-sessions/session-one", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("browser session route lost user authentication", response.Code)
	}
}
