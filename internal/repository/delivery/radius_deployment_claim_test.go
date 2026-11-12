package delivery

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/delivery"
	"testing"
)

func TestRadiusDeploymentClaimsRejectGenericAndMismatchedIdentity(t *testing.T) {
	// A nil database deliberately proves rejection happens before any SQL access.
	assertPrivateClaimRejections(t, []privateClaimCase{
		{"generic private provider", context.Background(), []string{"radius_deployment.edge"}, "edge"},
		{"mixed providers", context.Background(), []string{"ci_agent_runner", "radius_deployment.edge"}, "edge"},
		{"wrong agent", domain.WithRadiusDeploymentClaim(context.Background(), "edge"), []string{"radius_deployment.edge"}, "other"},
		{"wrong provider", domain.WithRadiusDeploymentClaim(context.Background(), "edge"), []string{"radius_deployment.other"}, "edge"},
	})
}
func TestRadiusDeploymentTimeoutLostHeartbeatRaceDoesNotOverwriteTask(t *testing.T) {
	assertPrivateTimeoutCAS(t, "radius_deployment_control", "radius_deployment.edge")
}
