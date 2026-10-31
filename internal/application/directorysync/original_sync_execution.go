package directorysync

import (
	"context"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) runOriginalSync(ctx context.Context, repo OriginalSyncStore, connection domain.Connection, policy domain.Policy, run domain.Run, factory ConnectorFactory) (domain.Run, error, bool) {
	requested, err := repo.TouchSyncRequest(ctx, run.ID)
	if err != nil || requested {
		return domain.Run{}, apperrors.ErrConflict, requested
	}
	execution, cancel := context.WithCancel(ctx)
	watch, stop := context.WithCancel(ctx)
	defer cancel()
	defer stop()
	finished := make(chan bool, 1)
	go watchOriginalSync(watch, repo, run.ID, cancel, finished)
	result, err := s.executeOriginalSync(execution, connection, policy, run, factory)
	stop()
	// Never mark canceled or release the active slot before execution returns.
	cancellation := <-finished
	return result, err, cancellation
}

func watchOriginalSync(ctx context.Context, repo OriginalSyncStore, id string, cancel context.CancelFunc, finished chan<- bool) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			finished <- false
			return
		case <-ticker.C:
			check, stop := context.WithTimeout(ctx, 3*time.Second)
			requested, err := repo.TouchSyncRequest(check, id)
			stop()
			if err != nil || requested {
				cancel()
				finished <- requested
				return
			}
		}
	}
}
