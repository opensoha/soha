package personalenvironment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	guest "github.com/opensoha/soha/contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *VM) Recover(ctx context.Context, principal identity.Principal, item domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error) {
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vm-ssh/"+item.CreatedBy+"/"+item.ComputeRef+"/personal-runtime-"+item.OperationID)).String()
	op, terminal, err := s.deps.FrozenRuntime(ctx, principal, id)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) && item.ActiveTaskID == "" {
			return s.deps.Guests.RecoverTerminalPersonalRuntime(ctx, item)
		}
		return item, err
	}
	if !terminal || op.OperationID != item.OperationID || op.Binding.AssistantID != item.AssistantID || op.Binding.ComputeGeneration != item.ComputeGeneration {
		return item, apperrors.ErrConflict
	}
	if op.Source != "user" && (item.ActiveTaskID != "" || op.Request.TaskID != "" || op.Request.Action != "inspect") {
		return item, apperrors.ErrConflict
	}
	return s.deps.Guests.RecoverTerminalPersonalRuntime(ctx, item)
}

func (s *VM) Invoke(ctx context.Context, principal identity.Principal, item domain.PersonalEnvironmentRecord, op guest.RuntimeOperation) (guest.RuntimeResult, error) {
	registered, err := s.deps.Guests.CurrentPersonalGuest(ctx, item.CreatedBy, item.AssistantID)
	if err != nil {
		return guest.RuntimeResult{}, fmt.Errorf("%w: %w", domain.ErrPersonalEnvironmentNotDispatched, err)
	}
	if registered.Status != "registered" || registered.LastHeartbeatAt == nil || s.now().Sub(*registered.LastHeartbeatAt) > time.Minute {
		return guest.RuntimeResult{}, fmt.Errorf("%w: %w", domain.ErrPersonalEnvironmentNotDispatched, apperrors.ErrServiceUnavailable)
	}
	if err := s.deps.Guests.CheckPersonalGuestBinding(ctx, registered); err != nil {
		return guest.RuntimeResult{}, fmt.Errorf("%w: %w", domain.ErrPersonalEnvironmentNotDispatched, err)
	}
	request := op.Request
	deadline := s.now().UTC().Add(50 * time.Second)
	if request.Lease.ExpiresAt.Before(deadline) {
		deadline = request.Lease.ExpiresAt
	}
	if registered.Binding.ExpiresAt.Before(deadline) {
		deadline = registered.Binding.ExpiresAt
	}
	op.SchemaVersion, op.OperationID, op.Binding, op.Deadline = guest.RuntimeOperationVersion, item.OperationID, registered.Binding, deadline
	op.ArtifactSHA256 = s.deps.Runtime.PersonalRuntimeArtifact()
	result, err := s.deps.Runtime.InvokePersonalGuestRuntime(ctx, principal, op)
	if err == nil && op.Kind == "environment" && (request.Action == "inspect" || request.Action == "acquire") {
		err = validatePersonalVMRuntimeProof(result.Proof, item)
	}
	return result, err
}

func validatePersonalVMRuntimeProof(proof *guest.RuntimeProof, item domain.PersonalEnvironmentRecord) error {
	if proof == nil || !proof.Command || !proof.CredentialsHidden || !proof.NamespacesIsolated || proof.FilesystemID != item.DataRef {
		return apperrors.ErrServiceUnavailable
	}
	if item.Lease.Mode == "restricted-write" && (!proof.FileRead || !proof.FileWrite || !proof.Docker || !proof.Browser) {
		return apperrors.ErrServiceUnavailable
	}
	return nil
}
