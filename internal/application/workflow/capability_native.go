package workflow

import (
	"context"
	"reflect"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Copilot owns the original Native identity, evidence and stop fence. Workflow
// keeps its existing queue and the owning domains keep their operation records.
type NativeTaskGuard interface {
	WithNativeTaskCreation(context.Context, domaincopilot.NativeLease, func(context.Context, domaincopilot.NativeTaskParent, domainidentity.Principal) error) error
	NativeTaskStopReason(context.Context, domaincopilot.NativeTaskParent) (string, error)
	WithNativeTaskParent(context.Context, domaincopilot.NativeTaskParent, bool, func(context.Context) error) error
}

func (s *Service) SetNativeTaskGuard(guard NativeTaskGuard) error {
	if guard == nil || (reflect.ValueOf(guard).Kind() == reflect.Pointer && reflect.ValueOf(guard).IsNil()) {
		return apperrors.ErrInvalidArgument
	}
	s.nativeTaskGuard = guard
	return nil
}

func (s *Service) CreateNativeCapabilityTask(ctx context.Context, lease domaincopilot.NativeLease, input domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityTask, error) {
	if s.nativeTaskGuard == nil {
		return domainaigateway.CapabilityTask{}, apperrors.ErrUnsupportedOperation
	}
	var task domainaigateway.CapabilityTask
	err := s.nativeTaskGuard.WithNativeTaskCreation(ctx, lease, func(txCtx context.Context, parent domaincopilot.NativeTaskParent, principal domainidentity.Principal) error {
		if principal.UserID != parent.OwnerID {
			return apperrors.ErrAccessDenied
		}
		var err error
		task, err = s.createCapabilityTask(txCtx, principal, input, &parent)
		return err
	})
	return task, err
}

// Recovery only looks up an existing child; it never creates another operation.
func (s *Service) FindNativeCapabilityTask(ctx context.Context, principal domainidentity.Principal, parent domaincopilot.NativeTaskParent, key, digest string) (domainaigateway.CapabilityTask, error) {
	repo, ok := s.repo.(interface {
		FindCapabilityRun(context.Context, string, string, string) (domainworkflow.Run, error)
	})
	if !ok || s.nativeTaskGuard == nil || s.capabilityRuntime == nil {
		return domainaigateway.CapabilityTask{}, apperrors.ErrUnsupportedOperation
	}
	if principal.UserID != parent.OwnerID || key == "" || digest == "" {
		return domainaigateway.CapabilityTask{}, apperrors.ErrAccessDenied
	}
	var task domainaigateway.CapabilityTask
	err := s.nativeTaskGuard.WithNativeTaskParent(ctx, parent, true, func(boundedCtx context.Context) error {
		run, err := repo.FindCapabilityRun(boundedCtx, principal.UserID, key, digest)
		if err != nil {
			return err
		}
		intent, err := domainworkflow.CapabilityIntentFrom(run)
		if err != nil || !nativeCapabilityParentMatches(intent.NativeParent, parent) {
			return apperrors.ErrConflict
		}
		task, err = s.capabilityRuntime.VisibleCapabilityTask(boundedCtx, principal, run)
		return err
	})
	return task, err
}

func nativeCapabilityParentMatches(stored *domaincopilot.NativeTaskParent, expected domaincopilot.NativeTaskParent) bool {
	return stored != nil && stored.RunID == expected.RunID && stored.OwnerID == expected.OwnerID && stored.SessionID == expected.SessionID && stored.RequestHash == expected.RequestHash && stored.Deadline.Equal(expected.Deadline)
}

func (s *Service) nativeCapabilityStopReason(ctx context.Context, intent domainworkflow.CapabilityIntent) (string, error) {
	if intent.NativeParent == nil {
		return "", nil
	}
	if s.nativeTaskGuard == nil {
		return "", apperrors.ErrAccessDenied
	}
	return s.nativeTaskGuard.NativeTaskStopReason(ctx, *intent.NativeParent)
}
