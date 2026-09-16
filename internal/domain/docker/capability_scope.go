package docker

import "context"

type scopeCheckKey struct{}

// WithScopeCheck constrains domain facts to the Gateway's already authorized targets.
// It grants no permissions and is never decoded from client input.
func WithScopeCheck(ctx context.Context, check func(map[string]string) error) context.Context {
	return context.WithValue(ctx, scopeCheckKey{}, check)
}

func CheckScope(ctx context.Context, scope map[string]string) error {
	if check, ok := ctx.Value(scopeCheckKey{}).(func(map[string]string) error); ok {
		return check(scope)
	}
	return nil
}
