package virtualization

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/idempotency"
)

type workerCreation struct {
	pool          domain.WorkerPool
	id, inputHash string
}

func (s *Service) authorizeWorkerCreate(ctx context.Context, principal domainidentity.Principal) error {
	for _, permission := range []string{appaccess.PermVirtualizationVMsCreate, appaccess.PermVirtualizationImagesView, appaccess.PlatformActionPermission("", "Node", "create")} {
		if err := s.authorize(ctx, principal, permission); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateWorker(ctx context.Context, principal domainidentity.Principal, poolID string, input sohaapi.VirtualizationWorkerCreateInput) (domain.Task, error) {
	if err := s.authorizeWorkerCreate(ctx, principal); err != nil {
		return domain.Task{}, err
	}
	pool, err := s.readWorkerPool(ctx, principal, poolID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := s.checkWorkerClusterAccess(ctx, principal, pool.Spec.ClusterID, true); err != nil {
		return domain.Task{}, err
	}
	poolID = pool.ID.String()
	id, hash, err := idempotentTaskIdentity("virtualization.worker.create/"+poolID, principal, input.IdempotencyKey, input)
	if err != nil || input.PoolRevision < 1 {
		return domain.Task{}, fmt.Errorf("%w: worker creation requires a pool revision and valid idempotency key", apperrors.ErrInvalidArgument)
	}
	if existing, err := s.tasks.GetTask(ctx, id); err == nil {
		if !idempotency.Matches(existing.Payload, hash) {
			return domain.Task{}, apperrors.ErrConflict
		}
		return domain.WithOperationState(existing, time.Now().UTC()), nil
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return domain.Task{}, err
	}
	if input.PoolRevision != pool.Revision || !pool.Spec.Enabled || !s.workerRuntimeConfigured() {
		return domain.Task{}, fmt.Errorf("%w: worker supply is disabled, unavailable or its revision changed", apperrors.ErrConflict)
	}
	if s.credentialKeys.Active().ID() == "" && strings.TrimSpace(s.credentialKey) == "" {
		return domain.Task{}, fmt.Errorf("%w: worker bootstrap requires configured credential encryption", apperrors.ErrConflict)
	}
	if err := s.checkWorkerPoolImage(ctx, pool); err != nil {
		return domain.Task{}, err
	}
	spec := pool.Spec
	return s.createVM(ctx, principal, CreateVMInput{ConnectionID: spec.ConnectionID, Name: domain.WorkerNodeName(id), Architecture: "amd64", Node: spec.ProviderNode, CPU: spec.CPU, MemoryMiB: spec.MemoryMiB, DiskGiB: spec.DiskGiB, ImageID: spec.ImageID, TemplateID: pool.Identity.TemplateID, SourceID: pool.Identity.TemplateID, SourceMode: "template_clone", RequireCapacity: true, StartAfterCreate: true, ProviderParams: map[string]any{"storage": spec.Storage, "bridge": spec.Bridge, "snippetStorage": spec.SnippetStorage, "ipconfig0": "ip=dhcp"}}, &workerCreation{pool: pool, id: id, inputHash: hash})
}

func (s *Service) saveWorkerCreation(ctx context.Context, principal domainidentity.Principal, task domain.Task, worker workerCreation) (domain.Task, error) {
	task.ID = worker.id
	task.Payload["workerPoolId"], task.Payload["workerPoolRevision"] = worker.pool.ID.String(), worker.pool.Revision
	task.Payload["workerPool"], task.Payload["workerPoolIdentity"] = worker.pool.VirtualizationWorkerPool, worker.pool.Identity
	task.Payload[idempotency.PayloadHashKey] = worker.inputHash
	task.Payload["executionActorId"], task.Payload["executionTokenId"] = principal.UserID, principal.AccessTokenID
	if err := s.sealGatewayExecution(ctx, task.Payload); err != nil {
		return domain.Task{}, err
	}
	var saved domain.Task
	err := s.workerPools.WithWorkerPoolAdmission(ctx, worker.pool.ID.String(), worker.pool.Revision, task.ID, func(ctx context.Context) error {
		if existing, err := s.tasks.GetTask(ctx, task.ID); err == nil {
			if !idempotency.Matches(existing.Payload, worker.inputHash) {
				return apperrors.ErrConflict
			}
			saved = existing
			return nil
		} else if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		var err error
		saved, err = s.createTaskWithAdmission(ctx, task)
		return err
	})
	return saved, err
}

func (s *Service) workerRuntimeConfigured() bool {
	r := s.workerRuntime
	return r != nil && r.PrepareBootstrap != nil && r.ClaimNode != nil && r.Observe != nil && r.RevokeBootstrap != nil && s.workerPools != nil
}

func workerPoolFromTask(task domain.Task) (domain.WorkerPool, error) {
	var pool domain.WorkerPool
	decodePayloadValue(task.Payload["workerPool"], &pool.VirtualizationWorkerPool)
	decodePayloadValue(task.Payload["workerPoolIdentity"], &pool.Identity)
	if pool.ID.String() != payloadString(task.Payload, "workerPoolId") || pool.Revision != payloadInt(task.Payload, "workerPoolRevision") || pool.Spec.ConnectionID != task.ConnectionID || pool.Identity.ClusterUID == "" || pool.Identity.ConnectionIdentity == "" {
		return pool, fmt.Errorf("%w: worker pool snapshot is missing or invalid", apperrors.ErrConflict)
	}
	return pool, domain.ValidateWorkerPoolSpec(pool.Spec)
}

func (s *Service) checkWorkerPoolImage(ctx context.Context, pool domain.WorkerPool) error {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	connection, template, fingerprint, err := s.inspectWorkerImage(ctx, pool.Spec)
	if err != nil {
		return err
	}
	if pool.Identity.ConnectionIdentity != vmCreateConnectionIdentity(connection) || pool.Identity.TemplateID != template || pool.Identity.TemplateFingerprint != fingerprint {
		return fmt.Errorf("%w: worker provider or template changed after pool registration", apperrors.ErrConflict)
	}
	return nil
}

func (s *Service) checkWorkerPoolEnabled(ctx context.Context, frozen domain.WorkerPool) error {
	if !s.workerRuntimeConfigured() {
		return apperrors.ErrUnsupportedOperation
	}
	current, err := s.workerPools.GetWorkerPool(ctx, frozen.ID.String())
	if err != nil {
		return err
	}
	if !current.Spec.Enabled || current.Revision != frozen.Revision {
		return fmt.Errorf("%w: original worker pool revision %s is no longer enabled", apperrors.ErrConflict, strconv.Itoa(frozen.Revision))
	}
	return nil
}
