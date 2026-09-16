package aigateway

import "context"

// ExecutionAuthorization is server-only provenance for a domain queue. Domains
// must seal it at rest: Call.Input may contain sensitive bootstrap material.
// It records the accepted call, never a permission snapshot or resolved secret.
type ExecutionAuthorization struct {
	ActorID        string                `json:"actorId"`
	Call           ToolInvocationRequest `json:"call"`
	ResourceScope  map[string]any        `json:"resourceScope"`
	ApprovalID     string                `json:"approvalId,omitempty"`
	ApprovalPolicy string                `json:"approvalPolicy,omitempty"`
}

type executionAuthorizationKey struct{}

func WithExecutionAuthorization(ctx context.Context, authorization ExecutionAuthorization) context.Context {
	return context.WithValue(ctx, executionAuthorizationKey{}, authorization)
}

func ExecutionAuthorizationFrom(ctx context.Context) (ExecutionAuthorization, bool) {
	value, ok := ctx.Value(executionAuthorizationKey{}).(ExecutionAuthorization)
	return value, ok
}
