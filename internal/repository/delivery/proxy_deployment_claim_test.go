package delivery

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/delivery"
	"testing"
)

func TestProxyDeploymentClaimsRejectGenericAndMismatchedIdentity(t *testing.T) {
	// A nil database deliberately proves rejection happens before any SQL access.
	assertPrivateClaimRejections(t, []privateClaimCase{
		{"generic private provider", context.Background(), []string{"proxy_deployment.edge"}, "edge"},
		{"mixed providers", context.Background(), []string{"ci_agent_runner", "proxy_deployment.edge"}, "edge"},
		{"wrong agent", domain.WithProxyDeploymentClaim(context.Background(), "edge"), []string{"proxy_deployment.edge"}, "other"},
		{"wrong provider", domain.WithProxyDeploymentClaim(context.Background(), "edge"), []string{"proxy_deployment.other"}, "edge"},
	})
}
func TestProxyDeploymentTimeoutLostHeartbeatRaceDoesNotOverwriteTask(t *testing.T) {
	assertPrivateTimeoutCAS(t, "proxy_deployment_control", "proxy_deployment.edge")
}
