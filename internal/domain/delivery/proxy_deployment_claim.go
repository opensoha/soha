package delivery

import (
	"context"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
)

type proxyDeploymentClaimKey struct{}

// WithProxyDeploymentClaim is an internal authorization marker. HTTP request
// fields never populate it; only the dedicated authenticated executor service
// may authorize a claim for this private provider namespace.
func WithProxyDeploymentClaim(ctx context.Context, executorID string) context.Context {
	return context.WithValue(ctx, proxyDeploymentClaimKey{}, executorID)
}

func ProxyDeploymentClaimExecutor(ctx context.Context) string {
	id, _ := ctx.Value(proxyDeploymentClaimKey{}).(string)
	return id
}

func IsProxyDeploymentTask(task ExecutionTask) bool {
	return strings.HasPrefix(task.ProviderKind, "proxy_deployment.") || task.TaskKind == "proxy_deployment_control"
}

func AuthorizeProxyDeploymentTask(ctx context.Context, task ExecutionTask) error {
	id := ProxyDeploymentClaimExecutor(ctx)
	if !IsProxyDeploymentTask(task) {
		if id != "" {
			return apperrors.ErrAccessDenied
		}
		return nil
	}
	if id == "" || task.ProviderKind != "proxy_deployment."+id || task.ClaimedByAgentID != id {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func IsPrivateRuntimeTask(task ExecutionTask) bool {
	return IsOutpostRuntimeTask(task) || IsProxyDeploymentTask(task) || IsRadiusDeploymentTask(task)
}
func AuthorizePrivateRuntimeTask(ctx context.Context, task ExecutionTask) error {
	if IsRadiusDeploymentTask(task) {
		return AuthorizeRadiusDeploymentTask(ctx, task)
	}
	if RadiusDeploymentClaimExecutor(ctx) != "" {
		return apperrors.ErrAccessDenied
	}
	if IsOutpostRuntimeTask(task) && ProxyDeploymentClaimExecutor(ctx) != "" {
		return apperrors.ErrAccessDenied
	}
	if IsProxyDeploymentTask(task) && OutpostRuntimeClaimExecutor(ctx) != "" {
		return apperrors.ErrAccessDenied
	}
	if IsOutpostRuntimeTask(task) {
		return AuthorizeOutpostRuntimeTask(ctx, task)
	}
	if IsProxyDeploymentTask(task) {
		return AuthorizeProxyDeploymentTask(ctx, task)
	}
	if OutpostRuntimeClaimExecutor(ctx) != "" || ProxyDeploymentClaimExecutor(ctx) != "" {
		return apperrors.ErrAccessDenied
	}
	return nil
}
