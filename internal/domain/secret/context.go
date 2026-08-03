package secret

import "context"

type valuesContextKey struct{}
type executionContextKey struct{}

func WithValues(ctx context.Context, values map[string]string) context.Context {
	copy := make(map[string]string, len(values))
	for alias, value := range values {
		copy[alias] = value
	}
	return context.WithValue(ctx, valuesContextKey{}, copy)
}

func ValuesFromContext(ctx context.Context) map[string]string {
	values, _ := ctx.Value(valuesContextKey{}).(map[string]string)
	copy := make(map[string]string, len(values))
	for alias, value := range values {
		copy[alias] = value
	}
	return copy
}

func WithExecutionContext(ctx context.Context, execution ExecutionContext) context.Context {
	execution.References = append([]Reference(nil), execution.References...)
	execution.Principal.Roles = append([]string(nil), execution.Principal.Roles...)
	execution.Principal.Teams = append([]string(nil), execution.Principal.Teams...)
	execution.Principal.Projects = append([]string(nil), execution.Principal.Projects...)
	execution.Principal.Tags = append([]string(nil), execution.Principal.Tags...)
	execution.Principal.PermissionKeys = append([]string(nil), execution.Principal.PermissionKeys...)
	return context.WithValue(ctx, executionContextKey{}, execution)
}

func ExecutionContextFrom(ctx context.Context) (ExecutionContext, bool) {
	execution, ok := ctx.Value(executionContextKey{}).(ExecutionContext)
	if !ok || len(execution.References) == 0 {
		return ExecutionContext{}, false
	}
	execution.References = append([]Reference(nil), execution.References...)
	return execution, true
}
