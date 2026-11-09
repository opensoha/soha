package delivery

import (
	"context"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
)

type radiusDeploymentClaimKey struct{}

func WithRadiusDeploymentClaim(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, radiusDeploymentClaimKey{}, id)
}
func RadiusDeploymentClaimExecutor(ctx context.Context) string {
	id, _ := ctx.Value(radiusDeploymentClaimKey{}).(string)
	return id
}
func IsRadiusDeploymentTask(task ExecutionTask) bool {
	return strings.HasPrefix(task.ProviderKind, "radius_deployment.") || task.TaskKind == "radius_deployment_control"
}
func AuthorizeRadiusDeploymentTask(ctx context.Context, task ExecutionTask) error {
	id := RadiusDeploymentClaimExecutor(ctx)
	if id == "" || OutpostRuntimeClaimExecutor(ctx) != "" || ProxyDeploymentClaimExecutor(ctx) != "" || !IsRadiusDeploymentTask(task) || task.ProviderKind != "radius_deployment."+id || task.ClaimedByAgentID != id {
		return apperrors.ErrAccessDenied
	}
	return nil
}
