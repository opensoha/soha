package aigateway

import (
	"context"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func withDockerScopeCheck(ctx context.Context, toolName string) context.Context {
	resolved, _ := ctx.Value(capabilityScopeContextKey{}).(capabilityScopeContext)
	if resolved.tool != toolName {
		return ctx
	}
	return domaindocker.WithScopeCheck(ctx, func(actual map[string]string) error {
		for _, scope := range resolved.scopes {
			matched, checked := true, false
			for key, value := range actual {
				// A creation authorizes its parent scope before an operation ID exists.
				if expected, exists := scope[key]; exists {
					checked = true
					if value != expected {
						matched = false
					}
				}
			}
			if matched && checked {
				return nil
			}
		}
		return apperrors.ErrConflict
	})
}
