package aigateway

import (
	"context"
	"reflect"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NativeTaskGuard interface {
	WithNativeTaskParent(context.Context, domaincopilot.NativeTaskParent, bool, func(context.Context) error) error
	ReserveNativeTaskToolCall(context.Context, domaincopilot.NativeTaskParent, bool) error
}

type nativeCapabilityParentKey struct{}
type nativeCapabilityParentScope struct {
	parent       domaincopilot.NativeTaskParent
	cancellation bool
}

func (s *Service) SetNativeTaskGuard(guard NativeTaskGuard) error {
	if guard == nil || (reflect.ValueOf(guard).Kind() == reflect.Pointer && reflect.ValueOf(guard).IsNil()) {
		return apperrors.ErrInvalidArgument
	}
	s.nativeTaskGuard = guard
	return nil
}

func (s *Service) withNativeCapabilityParent(ctx context.Context, run domainworkflow.Run, cancellation bool, execute func(context.Context) error) error {
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil {
		return err
	}
	if intent.NativeParent == nil {
		return execute(ctx)
	}
	if s.nativeTaskGuard == nil {
		return apperrors.ErrAccessDenied
	}
	return s.nativeTaskGuard.WithNativeTaskParent(ctx, *intent.NativeParent, cancellation, func(boundedCtx context.Context) error {
		return execute(context.WithValue(boundedCtx, nativeCapabilityParentKey{}, nativeCapabilityParentScope{parent: *intent.NativeParent, cancellation: cancellation}))
	})
}

func (s *Service) reserveNativeCapabilityToolCall(ctx context.Context) error {
	scope, ok := ctx.Value(nativeCapabilityParentKey{}).(nativeCapabilityParentScope)
	if !ok {
		return nil
	}
	if s.nativeTaskGuard == nil {
		return apperrors.ErrAccessDenied
	}
	return s.nativeTaskGuard.ReserveNativeTaskToolCall(ctx, scope.parent, scope.cancellation)
}
