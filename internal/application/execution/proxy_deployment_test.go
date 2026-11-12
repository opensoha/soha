package execution

import (
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
)

func TestProxyDeploymentPrivateStatusAndCallbackRequireExecutorIdentity(t *testing.T) {
	assertPrivateStatusAndCallbacks(t, domain.TaskKind, "proxy_deployment.edge", (*Service).GetProxyDeploymentExecutionTask, (*Service).RecordProxyDeploymentCallback)
}
func TestProxyDeploymentPrivateMaintenanceExpiresQueuedAndClaimedWithoutDispatchingDelivery(t *testing.T) {
	now := time.Now().UTC()
	assertPrivateMaintenance(t, domain.TaskKind, "proxy_deployment.", map[string]any{"proxyDeployment": domain.Command{Deadline: now.Add(-time.Second)}}, now, (*Service).maintainProxyDeployment)
}
func TestProxyDeploymentPrivateMaintenanceDeadlineHasNoEarlyExpiry(t *testing.T) {
	deadline := time.Now().UTC()
	assertPrivateCommandDeadline(t, "proxyDeployment", domain.Command{Deadline: deadline}, proxyDeploymentExpired, deadline)
}
func TestProxyDeploymentGenericActionsCannotReplayOrCancel(t *testing.T) {
	assertPrivateLifecycle(t, domain.TaskKind, "proxy_deployment.edge")
}
