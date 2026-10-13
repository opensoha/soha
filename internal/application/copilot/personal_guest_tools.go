package copilot

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	vm "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const personalVMImage = "docker.io/library/debian@sha256:918311b7b6c4c6f68b232ba516584925f6c78ad82b6fd534b98979df6438e483"

type personalGuestToolsStore interface {
	PersonalGuestPreparationStore
	CurrentPersonalGuest(context.Context, string, string) (domain.PersonalGuestRecord, error)
	ReservePersonalGuestTools(context.Context, domain.PersonalEnvironmentRecord, string) error
}

type personalGuestToolsQueue interface {
	QueuePersonalGuestTools(context.Context, identity.Principal, guest.ToolsPlan, int64) (vm.Task, error)
	FindPersonalGuestTools(context.Context, identity.Principal, string, int64) (vm.Task, error)
}

func (s *PersonalGuestPreparationService) Tools(ctx context.Context, principal identity.Principal, input sohaapi.PersonalGuestToolsInput) (vm.Task, error) {
	store, ok := s.store.(personalGuestToolsStore)
	queue, available := s.provisioner.(personalGuestToolsQueue)
	if !ok || !available || s.artifactSHA == "" {
		return vm.Task{}, apperrors.ErrServiceUnavailable
	}
	if !validPersonalRuntimeRequest(principal.UserID, input.EnvironmentVersion, input.IdempotencyKey) {
		return vm.Task{}, apperrors.ErrInvalidArgument
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("personal-guest-tools/"+principal.UserID+"/"+input.IdempotencyKey)).String()
	if task, err := queue.FindPersonalGuestTools(ctx, principal, id, input.EnvironmentVersion); err == nil {
		return task, nil
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return task, err
	}
	var task vm.Task
	err := store.WithinPersonalGuestTransaction(ctx, func(txCtx context.Context) error {
		env, _, _, err := store.LockPersonalGuestPreparation(txCtx, principal.UserID)
		if err != nil {
			return err
		}
		if !availablePersonalRuntimeRecord(env, input.EnvironmentVersion) {
			return apperrors.ErrConflict
		}
		registered, err := store.CurrentPersonalGuest(txCtx, principal.UserID, env.AssistantID)
		if err != nil {
			return err
		}
		if !freshPersonalRuntimeGuest(registered) {
			return apperrors.ErrConflict
		}
		if err := s.guest.store.CheckPersonalGuestBinding(txCtx, registered); err != nil {
			return err
		}
		quota := env.Lease.Quota
		plan := guest.ToolsPlan{SchemaVersion: guest.ToolsPlanVersion, OperationID: id, Binding: registered.Binding, ArtifactSHA256: s.artifactSHA, DiskQuotaBytes: quota.DiskBytes, CPUQuotaMilli: quota.CPUUnits, MemoryBytes: quota.MemoryBytes, PidsLimit: int64(quota.MaxProcesses), DockerImage: personalVMImage, BubblewrapVersion: "0.12.0-1~deb13u1", DockerVersion: "26.1.5+dfsg1-9+deb13u1", ChromiumVersion: "154.0.8037.92-1~deb13u1"}
		if plan.Validate(time.Now()) != nil {
			return apperrors.ErrConflict
		}
		if err := store.ReservePersonalGuestTools(txCtx, env, id); err != nil {
			return err
		}
		task, err = queue.QueuePersonalGuestTools(txCtx, principal, plan, input.EnvironmentVersion)
		return err
	})
	return task, err
}
