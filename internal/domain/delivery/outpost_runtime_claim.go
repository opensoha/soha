package delivery

import (
	"context"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
)

type outpostRuntimeClaimKey struct{}

// WithOutpostRuntimeClaim is an internal authorization marker. HTTP request
// fields never populate it; only the dedicated authenticated executor service
// may authorize a claim for this private provider namespace.
func WithOutpostRuntimeClaim(ctx context.Context, executorID string) context.Context {
	return context.WithValue(ctx, outpostRuntimeClaimKey{}, executorID)
}

func OutpostRuntimeClaimExecutor(ctx context.Context) string {
	id, _ := ctx.Value(outpostRuntimeClaimKey{}).(string)
	return id
}

func IsOutpostRuntimeTask(task ExecutionTask) bool {
	return strings.HasPrefix(task.ProviderKind, "outpost_runtime.") || task.TaskKind == "outpost_runtime_control"
}

func AuthorizeOutpostRuntimeTask(ctx context.Context, task ExecutionTask) error {
	id := OutpostRuntimeClaimExecutor(ctx)
	if !IsOutpostRuntimeTask(task) {
		if id != "" {
			return apperrors.ErrAccessDenied
		}
		return nil
	}
	if id == "" || task.ProviderKind != "outpost_runtime."+id || task.ClaimedByAgentID != id {
		return apperrors.ErrAccessDenied
	}
	return nil
}
