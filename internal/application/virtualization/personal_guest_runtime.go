package virtualization

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ConfigurePersonalRuntime(artifact string, authorize func(context.Context, guest.RuntimeOperation) error) {
	s.personalRuntimeArtifact, s.personalRuntimeAuthorize = artifact, authorize
}

func (s *Service) PersonalRuntimeArtifact() string { return s.personalRuntimeArtifact }

func (s *Service) ConfigurePersonalRuntimeCompletion(complete func(context.Context, guest.RuntimeOperation, guest.RuntimeResult, bool) error) {
	s.personalRuntimeComplete = complete
}

func personalRuntimeSSHID(binding guest.Binding, id string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("vm-ssh/"+binding.OwnerID+"/"+binding.ComputeRef+"/personal-runtime-"+id)).String()
}

func (s *Service) InvokePersonalGuestRuntime(ctx context.Context, principal identity.Principal, op guest.RuntimeOperation) (guest.RuntimeResult, error) {
	var result guest.RuntimeResult
	queued, err := s.QueuePersonalGuestRuntime(ctx, principal, op)
	if err != nil {
		return result, err
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if queued.Terminal {
			if queued.Outcome != "succeeded" || queued.OutputTruncated || len(queued.Stdout) > 320<<10 || json.Unmarshal([]byte(queued.Stdout), &result) != nil {
				return result, apperrors.ErrConflict
			}
			return result, result.ValidateFor(op)
		}
		select {
		case <-ctx.Done():
			cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_, _ = s.CancelVMSSHCommand(cancelCtx, principal, queued.ID)
			cancel()
			return result, ctx.Err()
		case <-ticker.C:
		}
		queued, err = s.GetVMSSHCommand(ctx, principal, queued.ID)
		if err != nil {
			return result, err
		}
	}
}

func (s *Service) QueuePersonalGuestRuntime(ctx context.Context, principal identity.Principal, op guest.RuntimeOperation) (sohaapi.VirtualMachineSSHCommand, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, err
	}
	if op.Validate(time.Now()) != nil || s.personalRuntimeArtifact == "" || s.personalRuntimeAuthorize == nil {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrServiceUnavailable
	}
	if err := s.personalRuntimeAuthorize(ctx, op); err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, err
	}
	if principal.UserID != op.Binding.OwnerID {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrAccessDenied
	}
	if err := s.ValidatePersonalGuestVM(ctx, principal, op.Binding); err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, err
	}
	binding, err := s.ssh.Access.GetVMAccessBinding(ctx, op.Binding.ComputeRef)
	if err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, err
	}
	digest, err := op.Digest()
	if err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, err
	}
	command := fmt.Sprintf("/usr/local/libexec/soha-personal-guest/%s/personal-guest --runtime=%s --operation=%s --digest=%s", s.personalRuntimeArtifact, op.Binding.DataRef, op.OperationID, digest)
	input := sohaapi.VirtualMachineSSHCommandInput{BindingRevision: binding.Revision, Command: command, IdempotencyKey: "personal-runtime-" + op.OperationID, TimeoutSeconds: 60}
	return s.createVMSSHCommand(ctx, principal, op.Binding.ComputeRef, input, &op)
}

func (s *Service) ReadPersonalGuestRuntime(ctx context.Context, principal identity.Principal, id string) (guest.RuntimeOperation, sohaapi.VirtualMachineSSHCommand, error) {
	op, task, err := s.FrozenPersonalGuestRuntime(ctx, principal, id)
	if err == nil && op.Source != "user" {
		err = apperrors.ErrAccessDenied
	}
	return op, task, err
}

func (s *Service) FrozenPersonalGuestRuntime(ctx context.Context, principal identity.Principal, id string) (guest.RuntimeOperation, sohaapi.VirtualMachineSSHCommand, error) {
	var op guest.RuntimeOperation
	if err := s.authorize(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return op, sohaapi.VirtualMachineSSHCommand{}, err
	}
	task, err := s.commandForOwner(ctx, principal, id, true)
	if err != nil {
		return op, sohaapi.VirtualMachineSSHCommand{}, err
	}
	op, err = s.decryptPersonalRuntime(stringValue(task.Payload, "personalRuntimeCredential"))
	if err != nil || op.Binding.OwnerID != principal.UserID || op.Validate(op.Deadline.Add(-time.Second)) != nil {
		return op, sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrAccessDenied
	}
	return op, commandReceipt(task), nil
}

func (s *Service) PersonalGuestRuntimeGrant(ctx context.Context, binding guest.Binding, id string) (guest.RuntimeOperation, error) {
	var op guest.RuntimeOperation
	if s.personalRuntimeAuthorize == nil || s.ssh.Commands == nil {
		return op, apperrors.ErrAccessDenied
	}
	task, err := s.ssh.Commands.GetExecutionTask(ctx, personalRuntimeSSHID(binding, id))
	if err != nil {
		return op, err
	}
	if task.TaskKind != vmSSHCommandKind || (task.Status != "running" && task.Status != "dispatching") || task.AttemptCount != 1 || task.ClaimedByAgentID == "" || stringValue(task.Payload, "actorId") != binding.OwnerID {
		return op, apperrors.ErrAccessDenied
	}
	session, err := s.ssh.Access.GetVMSSHSession(ctx, task.ID)
	if err != nil || session.Status != "active" || !session.ExpiresAt.After(time.Now()) {
		return op, apperrors.ErrAccessDenied
	}
	principal, err := s.currentSSHPrincipal(ctx, session)
	if err == nil {
		err = s.validateSSHBinding(ctx, principal, session.Binding)
	}
	if err == nil {
		err = s.checkQueuedGatewayAuthorization(ctx, principal, domain.Task{Payload: task.Payload})
	}
	if err != nil {
		return op, err
	}
	op, err = s.decryptPersonalRuntime(stringValue(task.Payload, "personalRuntimeCredential"))
	if err != nil || op.OperationID != id || !op.Binding.SameWorkspace(binding) || op.Validate(time.Now()) != nil {
		return op, apperrors.ErrAccessDenied
	}
	return op, s.personalRuntimeAuthorize(ctx, op)
}

func (s *Service) decryptPersonalRuntime(encrypted string) (guest.RuntimeOperation, error) {
	var op guest.RuntimeOperation
	values, err := s.decryptCredentialJSON(encrypted)
	if err != nil || json.Unmarshal([]byte(stringValue(values, "value")), &op) != nil {
		return op, apperrors.ErrConflict
	}
	return op, nil
}
