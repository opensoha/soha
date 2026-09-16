package workflow

import "context"

type deliveryScopeCheckKey struct{}

// WithDeliveryScopeCheck adds a caller restriction at the domain write/read
// boundary. It never replaces domain authorization.
func WithDeliveryScopeCheck(ctx context.Context, check func([]map[string]string) error) context.Context {
	return context.WithValue(ctx, deliveryScopeCheckKey{}, check)
}

func CheckDeliveryScopes(ctx context.Context, scopes []map[string]string) error {
	if check, ok := ctx.Value(deliveryScopeCheckKey{}).(func([]map[string]string) error); ok {
		return check(scopes)
	}
	return nil
}
