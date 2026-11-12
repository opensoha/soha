package execution

import (
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
)

func TestRadiusDeploymentPrivateStatusAndCallbackRequireExecutorIdentity(t *testing.T) {
	assertPrivateStatusAndCallbacks(t, domain.TaskKind, "radius_deployment.edge", (*Service).GetRadiusDeploymentExecutionTask, (*Service).RecordRadiusDeploymentCallback)
}
func TestRadiusDeploymentPrivateMaintenanceExpiresQueuedAndClaimedWithoutDispatchingDelivery(t *testing.T) {
	now := time.Now().UTC()
	assertPrivateMaintenance(t, domain.TaskKind, "radius_deployment.", map[string]any{"radiusDeployment": domain.Command{Deadline: now.Add(-time.Second)}}, now, (*Service).maintainRadiusDeployment)
}
func TestRadiusDeploymentPrivateMaintenanceDeadlineHasNoEarlyExpiry(t *testing.T) {
	deadline := time.Now().UTC()
	assertPrivateCommandDeadline(t, "radiusDeployment", domain.Command{Deadline: deadline}, radiusDeploymentExpired, deadline)
}
func TestRadiusDeploymentGenericActionsCannotReplayOrCancel(t *testing.T) {
	assertPrivateLifecycle(t, domain.TaskKind, "radius_deployment.edge")
}
