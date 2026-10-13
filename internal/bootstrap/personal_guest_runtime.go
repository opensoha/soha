package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	vm "github.com/opensoha/soha/internal/application/virtualization"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	store "github.com/opensoha/soha/internal/repository/copilot"
)

func recoverPersonalVM(ctx context.Context, repo *store.Repository, service *vm.Service, principal identity.Principal, item domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error) {
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vm-ssh/"+item.CreatedBy+"/"+item.ComputeRef+"/personal-runtime-"+item.OperationID)).String()
	op, task, err := service.FrozenPersonalGuestRuntime(ctx, principal, id)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) && item.ActiveTaskID == "" {
			return repo.RecoverTerminalPersonalRuntime(ctx, item)
		}
		return item, err
	}
	if !task.Terminal || op.OperationID != item.OperationID || op.Binding.AssistantID != item.AssistantID || op.Binding.ComputeGeneration != item.ComputeGeneration {
		return item, apperrors.ErrConflict
	}
	if op.Source != "user" && (item.ActiveTaskID != "" || op.Request.TaskID != "" || op.Request.Action != "inspect") {
		return item, apperrors.ErrConflict
	}
	return repo.RecoverTerminalPersonalRuntime(ctx, item)
}

func invokePersonalVM(ctx context.Context, repo *store.Repository, service *vm.Service, principal identity.Principal, item domain.PersonalEnvironmentRecord, op guest.RuntimeOperation) (guest.RuntimeResult, error) {
	registered, err := repo.CurrentPersonalGuest(ctx, item.CreatedBy, item.AssistantID)
	if err != nil {
		return guest.RuntimeResult{}, fmt.Errorf("%w: %w", domain.ErrPersonalEnvironmentNotDispatched, err)
	}
	if registered.Status != "registered" || registered.LastHeartbeatAt == nil || time.Since(*registered.LastHeartbeatAt) > time.Minute {
		return guest.RuntimeResult{}, fmt.Errorf("%w: %w", domain.ErrPersonalEnvironmentNotDispatched, apperrors.ErrServiceUnavailable)
	}
	if err := repo.CheckPersonalGuestBinding(ctx, registered); err != nil {
		return guest.RuntimeResult{}, fmt.Errorf("%w: %w", domain.ErrPersonalEnvironmentNotDispatched, err)
	}
	request := op.Request
	deadline := time.Now().UTC().Add(50 * time.Second)
	if request.Lease.ExpiresAt.Before(deadline) {
		deadline = request.Lease.ExpiresAt
	}
	if registered.Binding.ExpiresAt.Before(deadline) {
		deadline = registered.Binding.ExpiresAt
	}
	op.SchemaVersion, op.OperationID, op.Binding, op.Deadline = guest.RuntimeOperationVersion, item.OperationID, registered.Binding, deadline
	op.ArtifactSHA256 = service.PersonalRuntimeArtifact()
	result, err := service.InvokePersonalGuestRuntime(ctx, principal, op)
	if err == nil && op.Kind == "environment" && (request.Action == "inspect" || request.Action == "acquire") {
		err = validatePersonalVMRuntimeProof(result.Proof, item)

	}
	return result, err
}

func validatePersonalVMRuntimeProof(proof *guest.RuntimeProof, item domain.PersonalEnvironmentRecord) error {
	if proof == nil || !proof.Command || !proof.CredentialsHidden || !proof.NamespacesIsolated || proof.FilesystemID != item.DataRef {
		return apperrors.ErrServiceUnavailable
	}
	if item.Lease.Mode == "restricted-write" && (proof == nil || !proof.FileRead || !proof.FileWrite || !proof.Docker || !proof.Browser) {
		return apperrors.ErrServiceUnavailable
	}
	return nil
}
