package delivery

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/delivery"
	"testing"
)

func TestOutpostRuntimeClaimsRejectGenericAndMismatchedIdentity(t *testing.T) {
	// A nil database deliberately proves rejection happens before any SQL access.
	assertPrivateClaimRejections(t, []privateClaimCase{
		{"generic private provider", context.Background(), []string{"outpost_runtime.edge"}, "edge"},
		{"mixed providers", context.Background(), []string{"ci_agent_runner", "outpost_runtime.edge"}, "edge"},
		{"wrong agent", domain.WithOutpostRuntimeClaim(context.Background(), "edge"), []string{"outpost_runtime.edge"}, "other"},
		{"wrong provider", domain.WithOutpostRuntimeClaim(context.Background(), "edge"), []string{"outpost_runtime.other"}, "edge"},
	})
}
func TestPrivateTimeoutLostHeartbeatRaceDoesNotOverwriteTask(t *testing.T) {
	assertPrivateTimeoutCAS(t, "outpost_runtime_control", "outpost_runtime.edge")
}
