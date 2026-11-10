package networkruntime_test

import (
	"testing"
	"time"
)

func TestContentReleaseWithPostgres(t *testing.T) {
	scenario := newRuntimeRepositoryScenario(t)
	scenario.seedPolicy(t)
	t.Run("dlp_linked_approvals", scenario.checkDLPHoldSources)
	t.Run("managed_linked_approvals", scenario.checkManagedDLPHoldSources)
	t.Run("content_release_atomicity", scenario.checkContentReleaseAtomicity)
	t.Run("content_release_concurrent_approval", scenario.checkContentReleaseConcurrentApproval)
}

func TestVPNGovernanceWithPostgres(t *testing.T) {
	scenario := newRuntimeRepositoryScenario(t)
	scenario.seedPolicy(t)
	scenario.now = time.Now().UTC().Add(-2 * time.Minute)
	scenario.enrollFirstEndpoint(t)
	scenario.rotateInitialEndpoint(t)
	scenario.seedNetwork(t)
	scenario.checkGatewayEnrollment(t)
	t.Run("TransactionAndRevocation", scenario.checkVPNGovernance)
}
