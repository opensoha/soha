package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	appcluster "github.com/opensoha/soha/internal/application/cluster"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestVirtualizationHandlerWiresPolicyAndGuestAccess(t *testing.T) {
	handler := newVirtualizationHandler(&appvirtualization.Service{})
	for name, action := range map[string]func(*gin.Context){
		"capacity policy":   handler.GetCapacityPolicy,
		"VM access binding": handler.GetVMAccessBinding,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			ctx.Params = gin.Params{{Key: "id", Value: "test-id"}}
			action(ctx)
			// The real service must reach its fail-closed permission check.
			if response.Code != http.StatusForbidden {
				t.Fatalf("business authorization was not reached: status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

type sshClusterFixture struct {
	appcluster.RuntimeRegistry
	appcluster.RuntimeReader
	appcluster.Repository
	connection domaincluster.Connection
}

func (fixture *sshClusterFixture) GetConnection(_ context.Context, id string) (domaincluster.Connection, error) {
	if id != fixture.connection.Summary.ID {
		return domaincluster.Connection{}, apperrors.ErrNotFound
	}
	return fixture.connection, nil
}

func TestVMSSHAuthenticatorAcceptsBothAgentTransportsWithExactCredential(t *testing.T) {
	for _, transport := range []string{"http", "reverse_session"} {
		t.Run(transport, func(t *testing.T) {
			fixture := &sshClusterFixture{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "ssh-agent", ConnectionMode: domaincluster.ConnectionModeAgent}, Metadata: map[string]any{"transport": transport, "token": "fixture-agent-token"}}}
			cluster, err := appcluster.New(fixture, fixture, nil, nil, fixture, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			options := newVMSSHOptions(cfgpkg.Config{}, &infrastructure{}, &repositories{}, &coreServices{clusterService: cluster})
			if err = options.AuthenticateExecutionAgent(t.Context(), "ssh-agent", "fixture-agent-token"); err != nil {
				t.Fatal("registered SSH Agent was rejected", err)
			}
			if err = options.AuthenticateExecutionAgent(t.Context(), "ssh-agent", "other-agent-token"); !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Fatal("wrong Agent credential accepted", err)
			}
			if err = options.AuthenticateExecutionAgent(t.Context(), "other-cluster", "fixture-agent-token"); err == nil {
				t.Fatal("cross-cluster credential accepted")
			}
		})
	}
}
