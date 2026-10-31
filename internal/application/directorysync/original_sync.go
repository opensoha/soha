package directorysync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type OriginalSyncStore interface {
	ClaimSyncRequest(context.Context, domain.Run) (domain.Run, bool, error)
	GetSyncRequest(context.Context, string, string) (domain.Run, error)
	TouchSyncRequest(context.Context, string) (bool, error)
	RequestSyncCancellation(context.Context, string) (domain.Run, error)
	FinishCanceledSyncRequest(context.Context, string, string) error
}

type ConnectorFactory interface {
	Connector(string) (Connector, error)
}

func originalSyncReference(actor, credential, key string) (string, string, error) {
	id, err := uuid.Parse(key)
	if err != nil || len(key) != 36 || id == uuid.Nil || !strings.EqualFold(id.String(), key) || actor == "" || credential == "" || len(actor) > 256 || len(credential) > 256 {
		return "", "", apperrors.ErrInvalidArgument
	}
	return fmt.Sprintf("manual-v1:%x", sha256.Sum256([]byte(actor+"\x00"+id.String()))), fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(credential))), nil
}

func (s *Service) GetOriginalSync(ctx context.Context, connectionID, actor, credential, key string) (domain.Run, error) {
	stored, binding, err := originalSyncReference(actor, credential, key)
	if err != nil {
		return domain.Run{}, err
	}
	repo, ok := s.repository.(OriginalSyncStore)
	if !ok {
		return domain.Run{}, apperrors.ErrUnsupportedOperation
	}
	run, err := repo.GetSyncRequest(ctx, connectionID, stored)
	if err != nil {
		return domain.Run{}, err
	}
	if run.RequestedBy != actor || run.RequestCredentialID != binding {
		return domain.Run{}, apperrors.ErrAccessDenied
	}
	return run, nil
}

// SyncOriginal claims before any connector access. A duplicate can only observe
// its original row, including queued/running/failed work; it never retries it.
func (s *Service) SyncOriginal(ctx context.Context, connectionID, actor, credential, key string, factory ConnectorFactory) (domain.Run, error) {
	stored, binding, err := originalSyncReference(actor, credential, key)
	if err != nil {
		return domain.Run{}, err
	}
	repo, ok := s.repository.(OriginalSyncStore)
	if !ok {
		return domain.Run{}, apperrors.ErrUnsupportedOperation
	}
	connection, policy, err := s.repository.GetConnection(ctx, connectionID)
	if err != nil {
		return domain.Run{}, err
	}
	run := domain.Run{ID: uuid.NewString(), ConnectionID: connectionID, Trigger: "manual", Mode: "full", Status: domain.RunQueued,
		IncludePeople: policy.SyncPeople, RequestedBy: actor, IdempotencyKey: stored, RequestCredentialID: binding, CreatedAt: s.now().UTC()}
	run, created, err := repo.ClaimSyncRequest(ctx, run)
	if err != nil {
		return domain.Run{}, err
	}
	if run.RequestedBy != actor || run.RequestCredentialID != binding {
		return domain.Run{}, apperrors.ErrAccessDenied
	}
	if !created {
		return run, nil
	}
	if err := s.repository.TransitionRun(ctx, run.ID, domain.RunRunning, domain.RunStats{}, "", ""); err != nil {
		return domain.Run{}, err
	}
	result, err, cancellation := s.runOriginalSync(ctx, repo, connection, policy, run, factory)
	if err == nil {
		return result, nil
	}
	// Cancellation cannot make the occupied key eligible for another pull.
	// Failure persistence may itself be unavailable; preserve the original row.
	persist, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if cancellation {
		_ = repo.FinishCanceledSyncRequest(persist, run.ID, "Synchronization stopped after cancellation; earlier effects may have committed. Query the original run; do not replay this request")
	} else {
		_ = s.repository.TransitionRun(persist, run.ID, domain.RunFailed, domain.RunStats{}, "original_outcome_unconfirmed", "Original synchronization failed or was interrupted; effects may be incomplete. Query the original run; do not replay this request")
	}
	return s.repository.GetRun(persist, run.ID)
}

func (s *Service) RequestOriginalCancellation(ctx context.Context, id, actor, credential string) (domain.Run, error) {
	run, err := s.repository.GetRun(ctx, id)
	if err != nil {
		return domain.Run{}, err
	}
	if actor == "" || credential == "" || run.RequestedBy != actor || run.RequestCredentialID != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(credential))) {
		return domain.Run{}, apperrors.ErrAccessDenied
	}
	repo, ok := s.repository.(OriginalSyncStore)
	if !ok {
		return domain.Run{}, apperrors.ErrUnsupportedOperation
	}
	return repo.RequestSyncCancellation(ctx, id)
}

func (s *Service) executeOriginalSync(ctx context.Context, connection domain.Connection, policy domain.Policy, run domain.Run, factory ConnectorFactory) (domain.Run, error) {
	if nilSyncDependency(factory) {
		return domain.Run{}, apperrors.ErrServiceUnavailable
	}
	connector, err := factory.Connector(connection.ProviderType)
	if err != nil {
		return domain.Run{}, err
	}
	if nilSyncDependency(connector) {
		return domain.Run{}, apperrors.ErrServiceUnavailable
	}
	// Connector selection and its input use the same captured configuration.
	// PullSnapshot's public entry still loads the current connection for callers.
	snapshot, _, err := pullConnectionSnapshot(ctx, connection, policy, connector)
	if err != nil {
		return domain.Run{}, err
	}
	current, currentPolicy, organizations, people, err := s.load(ctx, connection.ID)
	if err != nil {
		return domain.Run{}, err
	}
	if !current.UpdatedAt.Equal(connection.UpdatedAt) || current.ProviderType != connection.ProviderType || current.CredentialRef != connection.CredentialRef || !reflect.DeepEqual(policy, currentPolicy) {
		return domain.Run{}, apperrors.ErrConflict
	}
	plan, err := BuildPlan(snapshot, organizations, people, policy.SyncPeople)
	if err != nil {
		return domain.Run{}, err
	}
	result, _, err := s.applyFullPlan(ctx, current, policy, plan, run)
	return result, err
}

func nilSyncDependency(value any) bool {
	if value == nil {
		return true
	}
	switch reflect.ValueOf(value).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(value).IsNil()
	default:
		return false
	}
}
