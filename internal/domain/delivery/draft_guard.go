package delivery

import "context"

type draftScopeCheckKey struct{}

// WithDraftScopeCheck binds an additional caller restriction to the actual
// prepared intent. It cannot bypass the domain's own authorization or CAS.
func WithDraftScopeCheck(ctx context.Context, check func(context.Context, DeliveryDraft) error) context.Context {
	return context.WithValue(ctx, draftScopeCheckKey{}, check)
}

func CheckDraftScope(ctx context.Context, draft DeliveryDraft) error {
	if check, ok := ctx.Value(draftScopeCheckKey{}).(func(context.Context, DeliveryDraft) error); ok {
		return check(ctx, draft)
	}
	return nil
}
