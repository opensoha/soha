package copilot

import (
	"context"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativeTaskStore interface {
	WithNativeTaskCreation(context.Context, domaincopilot.NativeLease, func(context.Context, domaincopilot.NativeTaskParent) error) error
	NativeTaskStopReason(context.Context, domaincopilot.NativeTaskParent) (string, error)
	WithNativeTaskParent(context.Context, domaincopilot.NativeTaskParent, bool, func(context.Context) error) error
	ReserveNativeChildToolCall(context.Context, domaincopilot.NativeTaskParent, bool) error
}

func (s *Service) ReserveNativeTaskToolCall(ctx context.Context, parent domaincopilot.NativeTaskParent, cancellation bool) error {
	store, _, err := s.nativeTaskStore()
	if err != nil {
		return err
	}
	return store.ReserveNativeChildToolCall(ctx, parent, cancellation)
}

func (s *Service) nativeTaskStore() (nativeTaskStore, NativeRunStore, error) {
	tasks, ok := s.agentRuns.(nativeTaskStore)
	runs, runOK := s.agentRuns.(NativeRunStore)
	if !ok || !runOK {
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	return tasks, runs, nil
}

func (s *Service) WithNativeTaskCreation(ctx context.Context, expected domaincopilot.NativeLease, register func(context.Context, domaincopilot.NativeTaskParent, domainidentity.Principal) error) error {
	store, runs, err := s.nativeTaskStore()
	if err != nil {
		return err
	}
	return store.WithNativeTaskCreation(ctx, expected, func(txCtx context.Context, parent domaincopilot.NativeTaskParent) error {
		lease, err := runs.ReadNativeLease(txCtx, parent.OwnerID, parent.RunID)
		if err != nil {
			return err
		}
		principal, err := s.nativePrincipal(txCtx, lease.Run)
		if err != nil {
			return err
		}
		txCtx, err = s.nativeAuthorizationContext(txCtx, principal, lease.Run, true)
		if err != nil {
			return err
		}
		if _, err = s.currentNativeTaskCatalog(txCtx, principal, lease.Run); err != nil {
			return err
		}
		if err = s.validateNativeEvidence(txCtx, principal, lease.Run); err != nil {
			return err
		}
		return register(txCtx, parent, principal)
	})
}

func (s *Service) NativeTaskStopReason(ctx context.Context, parent domaincopilot.NativeTaskParent) (string, error) {
	store, runs, err := s.nativeTaskStore()
	if err != nil {
		return "", err
	}
	reason, err := store.NativeTaskStopReason(ctx, parent)
	if err != nil || reason != "" {
		return reason, err
	}
	lease, err := runs.ReadNativeLease(ctx, parent.OwnerID, parent.RunID)
	if err != nil {
		return "", err
	}
	principal, err := s.nativePrincipal(ctx, lease.Run)
	if err == nil {
		ctx, err = s.nativeAuthorizationContext(ctx, principal, lease.Run, true)
	}
	if err == nil {
		_, err = s.currentNativeTaskCatalog(ctx, principal, lease.Run)
	}
	if err == nil {
		err = s.validateNativeEvidence(ctx, principal, lease.Run)
	}
	if err != nil {
		return "failure", nil
	}
	return "", nil
}

func (s *Service) WithNativeTaskParent(ctx context.Context, parent domaincopilot.NativeTaskParent, cancellation bool, execute func(context.Context) error) error {
	store, runs, err := s.nativeTaskStore()
	if err != nil {
		return err
	}
	return store.WithNativeTaskParent(ctx, parent, cancellation, func(boundedCtx context.Context) error {
		lease, err := runs.ReadNativeLease(boundedCtx, parent.OwnerID, parent.RunID)
		if err != nil {
			return err
		}
		principal, err := s.nativePrincipal(boundedCtx, lease.Run)
		if err != nil {
			return err
		}
		if !cancellation {
			boundedCtx, err = s.nativeAuthorizationContext(boundedCtx, principal, lease.Run, true)
			if err != nil {
				return err
			}
			if _, err = s.currentNativeTaskCatalog(boundedCtx, principal, lease.Run); err != nil {
				return err
			}
			if err = s.validateNativeEvidence(boundedCtx, principal, lease.Run); err != nil {
				return err
			}
		}
		return execute(boundedCtx)
	})
}
