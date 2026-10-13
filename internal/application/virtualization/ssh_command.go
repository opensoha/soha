package virtualization

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	ai "github.com/opensoha/soha/internal/domain/aigateway"
	delivery "github.com/opensoha/soha/internal/domain/delivery"
	identity "github.com/opensoha/soha/internal/domain/identity"
	secret "github.com/opensoha/soha/internal/domain/secret"
	vm "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const vmSSHCommandKind = "vm_ssh_command"

func (s *Service) CheckVMSSHCommandReadiness(ctx context.Context, principal identity.Principal, vmID string, revision int) (ai.CapabilityAssessment, error) {
	assessment := ai.CapabilityAssessment{Verdict: "inconclusive", Summary: "A current trusted VM login binding and SSH execution Agent are required.", Evidence: []ai.CapabilityEvidence{}}
	if revision < 1 || s.ssh.Access == nil || s.ssh.Executor == nil || s.ssh.Secrets == nil {
		return assessment, apperrors.ErrInvalidArgument
	}
	binding, err := s.ssh.Access.GetVMAccessBinding(ctx, vmID)
	if err != nil {
		return assessment, mapNotFound(err)
	}
	if binding.Revision != revision {
		return assessment, apperrors.ErrConflict
	}
	if err = s.validateSSHBinding(ctx, principal, binding); err != nil {
		return assessment, err
	}
	if _, _, err = s.ssh.Secrets.PinSSHCredential(ctx, principal, binding.CredentialRef, secret.Target{Type: "vm", Ref: vmID, Purpose: "vm_exec"}); err != nil {
		return assessment, err
	}
	capability, err := s.ssh.Executor.Capability(ctx, binding.ExecutionClusterID)
	ready := err == nil && capability.Available && capability.CommandsAvailable && capability.AgentID == binding.ExecutionAgentID && capability.ProtocolVersion == 1
	if ready {
		assessment.Verdict = "satisfied"
		assessment.Summary = "Current VM identity, credential purpose and execution Agent are available for a bounded SSH command."
	}
	now := time.Now().UTC()
	assessment.Evidence = append(assessment.Evidence, ai.CapabilityEvidence{Kind: "runtime_inventory", Source: "virtualization.ssh.agent", ObservedAt: now, DataThrough: &now, Incomplete: !ready, Summary: assessment.Summary, Resource: &ai.CapabilityResourceRef{Kind: "virtualization.vm", ID: vmID}})
	return assessment, nil
}

func sshCommandDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func commandReceipt(task delivery.ExecutionTask) sohaapi.VirtualMachineSSHCommand {
	item := sohaapi.VirtualMachineSSHCommand{ID: task.ID, VMID: stringValue(task.Payload, "vmId"), BindingRevision: payloadInt(task.Payload, "bindingRevision"), Status: task.Status, Outcome: "unknown", FinishedAt: task.FinishedAt}
	raw, _ := json.Marshal(task.Result["sshCommand"])
	var result sohaapi.AgentSSHCommandResult
	_ = json.Unmarshal(raw, &result)
	item.Stdout, item.Stderr, item.OutputDigest, item.OutputTruncated, item.ExitCode = result.Stdout, result.Stderr, result.OutputDigest, result.OutputTruncated, result.ExitCode
	switch task.Status {
	case "completed", "failed", "canceled", "callback_timeout":
		item.Terminal = true
	}
	if task.Status == "completed" && result.Effect == "exited" && result.ExitCode != nil && *result.ExitCode == 0 {
		item.Outcome = "succeeded"
	}
	if task.Status == "failed" && (result.Effect == "not_started" || result.Effect == "exited" && result.ExitCode != nil) {
		item.Outcome = "failed"
	}
	if task.Status == "canceled" && task.AttemptCount == 0 {
		item.Outcome = "canceled"
	}
	return item
}

func commandRequest(vmID string, principal identity.Principal, input sohaapi.VirtualMachineSSHCommandInput) (string, string, error) {
	if vmID == "" || principal.UserID == "" || input.BindingRevision < 1 || strings.TrimSpace(input.Command) == "" || len(input.Command) > 16384 || strings.ContainsRune(input.Command, 0) || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 || input.TimeoutSeconds < 1 || input.TimeoutSeconds > 300 {
		return "", "", apperrors.ErrInvalidArgument
	}
	raw, _ := json.Marshal(input)
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vm-ssh/"+principal.UserID+"/"+vmID+"/"+input.IdempotencyKey)).String()
	return id, sshCommandDigest(string(raw)), nil
}

func (s *Service) CreateVMSSHCommand(ctx context.Context, principal identity.Principal, vmID string, input sohaapi.VirtualMachineSSHCommandInput) (sohaapi.VirtualMachineSSHCommand, error) {
	return s.createVMSSHCommand(ctx, principal, vmID, input, nil)
}

func (s *Service) createVMSSHCommand(ctx context.Context, principal identity.Principal, vmID string, input sohaapi.VirtualMachineSSHCommandInput, runtime *guest.RuntimeOperation) (sohaapi.VirtualMachineSSHCommand, error) {
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 60
	}
	id, digest, err := commandRequest(vmID, principal, input)
	if err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, err
	}
	if !s.sshConfigured(true) {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrUnsupportedOperation
	}
	if existing, readErr := s.commandForOwner(ctx, principal, id, true); readErr == nil {
		if stringValue(existing.Payload, "requestDigest") != digest {
			return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrConflict
		}
		return commandReceipt(existing), nil
	} else if !errors.Is(readErr, apperrors.ErrNotFound) {
		return sohaapi.VirtualMachineSSHCommand{}, readErr
	}
	binding, err := s.ssh.Access.GetVMAccessBinding(ctx, vmID)
	if err == nil && binding.Revision != input.BindingRevision {
		err = apperrors.ErrConflict
	}
	if err == nil {
		err = s.validateSSHBinding(ctx, principal, binding)
	}
	if err == nil {
		_, _, err = s.ssh.Secrets.PinSSHCredential(ctx, principal, binding.CredentialRef, secret.Target{Type: "vm", Ref: vmID, Purpose: "vm_exec"})
	}
	if err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, err
	}
	capability, err := s.ssh.Executor.Capability(ctx, binding.ExecutionClusterID)
	if err != nil || !supportsSSHExecution(capability, binding, true) {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrUnsupportedOperation
	}
	sealed, err := s.encryptCredentialJSON(map[string]any{"value": input.Command})
	if err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrUnsupportedOperation
	}
	now := time.Now().UTC()
	payload := map[string]any{"vmId": vmID, "actorId": principal.UserID, "bindingRevision": binding.Revision, "executionAgentId": binding.ExecutionAgentID, "requestDigest": digest, "authorizationExpiresAt": now.Add(30 * time.Minute).Format(time.RFC3339), "commandDigest": sshCommandDigest(input.Command), "sshCommandCredential": sealed}
	if runtime != nil {
		raw, marshalErr := json.Marshal(runtime)
		if marshalErr != nil {
			return sohaapi.VirtualMachineSSHCommand{}, marshalErr
		}
		sealed, sealErr := s.encryptCredentialJSON(map[string]any{"value": string(raw)})
		if sealErr != nil {
			return sohaapi.VirtualMachineSSHCommand{}, sealErr
		}
		payload["personalRuntimeCredential"] = sealed
		payload["personalRuntimeSource"] = runtime.Source
	}
	if err = s.sealGatewayExecution(ctx, payload); err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, err
	}
	session := vm.VMSSHSession{ID: id, VMID: vmID, BindingRevision: binding.Revision, Status: "pending", Binding: binding, Principal: principal, ExecutionTokenID: principal.AccessTokenID, Purpose: "vm_exec", Cols: 80, Rows: 24, CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute)}
	session.BrowserSessionID, _ = ctx.Value(sshBrowserSessionKey{}).(string)
	task, err := s.persistSSHCommand(ctx, delivery.ExecutionTask{ID: id, TaskKind: vmSSHCommandKind, ProviderKind: "vm_ssh." + binding.ExecutionClusterID, TargetKind: "vm", Status: "queued", TimeoutSeconds: input.TimeoutSeconds, CallbackToken: uuid.NewString(), RuntimeClusterID: binding.ExecutionClusterID, Payload: payload, Result: map[string]any{}, CreatedAt: now, UpdatedAt: now}, session)
	if err == nil {
		s.recordOperation(ctx, principal, "virtualization.vm.ssh.execute", vmID, binding.Username, "success", "queued SSH command", map[string]any{"executionTaskId": id, "bindingRevision": binding.Revision, "commandDigest": payload["commandDigest"]})
	}
	return commandReceipt(task), err
}

func (s *Service) commandForOwner(ctx context.Context, principal identity.Principal, id string, requireAccess bool) (delivery.ExecutionTask, error) {
	if s.ssh.Commands == nil {
		return delivery.ExecutionTask{}, apperrors.ErrUnsupportedOperation
	}
	task, err := s.ssh.Commands.GetExecutionTask(ctx, id)
	if err != nil {
		return task, mapNotFound(err)
	}
	if task.TaskKind != vmSSHCommandKind || stringValue(task.Payload, "actorId") != principal.UserID {
		return task, apperrors.ErrAccessDenied
	}
	if requireAccess {
		_, _, err = s.accessVM(ctx, principal, stringValue(task.Payload, "vmId"), vmConnectPermission)
	}
	return task, err
}

func (s *Service) GetVMSSHCommand(ctx context.Context, principal identity.Principal, id string) (sohaapi.VirtualMachineSSHCommand, error) {
	task, err := s.commandForOwner(ctx, principal, id, true)
	return commandReceipt(task), err
}

func (s *Service) FindVMSSHCommand(ctx context.Context, principal identity.Principal, vmID string, input sohaapi.VirtualMachineSSHCommandInput) (sohaapi.VirtualMachineSSHCommand, error) {
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 60
	}
	id, digest, err := commandRequest(vmID, principal, input)
	if err != nil {
		return sohaapi.VirtualMachineSSHCommand{}, err
	}
	task, err := s.commandForOwner(ctx, principal, id, true)
	if err == nil && stringValue(task.Payload, "requestDigest") != digest {
		err = apperrors.ErrConflict
	}
	return commandReceipt(task), err
}

func (s *Service) CancelVMSSHCommand(ctx context.Context, principal identity.Principal, id string) (sohaapi.VirtualMachineSSHCommand, error) {
	task, err := s.commandForOwner(ctx, principal, id, false)
	if err != nil || commandReceipt(task).Terminal {
		return commandReceipt(task), err
	}
	if s.ssh.CancelCommand == nil || s.ssh.WithinCommand == nil {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrUnsupportedOperation
	}
	err = s.ssh.WithinCommand(ctx, id, func(txCtx context.Context) error {
		_, err := s.CloseVMSSHSession(txCtx, principal, id)
		if err != nil {
			return err
		}
		task, err = s.ssh.CancelCommand(txCtx, id, delivery.ExecutionTaskActionInput{Reason: "owner canceled VM SSH command; dispatched effects remain unknown"})
		return err
	})
	return commandReceipt(task), err
}

func (s *Service) HydrateSSHExecutionTask(ctx context.Context, task delivery.ExecutionTask) (delivery.ExecutionTask, error) {
	if task.TaskKind != vmSSHCommandKind {
		return task, nil
	}
	if s.ssh.WithinCommand == nil {
		return task, apperrors.ErrAccessDenied
	}
	actorID, vmID := stringValue(task.Payload, "actorId"), stringValue(task.Payload, "vmId")
	stage := "claim"
	err := s.ssh.WithinCommand(ctx, task.ID, func(txCtx context.Context) error {
		if err := s.validateSSHCommandClaim(txCtx, task.ID, stringValue(task.Payload, "executionAgentId")); err != nil {
			return err
		}
		stage = "session"
		session, err := s.ssh.Access.GetVMSSHSession(txCtx, task.ID)
		if err != nil || session.Purpose != "vm_exec" || session.Status != "pending" || !session.ExpiresAt.After(time.Now()) || task.ProviderKind != "vm_ssh."+session.Binding.ExecutionClusterID {
			return apperrors.ErrAccessDenied
		}
		stage = "principal"
		principal, err := s.currentSSHPrincipal(txCtx, session)
		if err == nil {
			stage = "binding"
			err = s.validateSSHBinding(txCtx, principal, session.Binding)
		}
		if err == nil {
			stage = "gateway"
			err = s.checkQueuedGatewayAuthorization(txCtx, principal, vm.Task{Payload: task.Payload})
		}
		if err != nil {
			return err
		}
		stage = "credential"
		ref, _, err := s.ssh.Secrets.PinSSHCredential(txCtx, principal, session.Binding.CredentialRef, secret.Target{Type: "vm", Ref: session.VMID, Purpose: "vm_exec"})
		if err != nil {
			return err
		}
		stage = "lease"
		grant, err := s.ssh.Secrets.IssueLease(txCtx, principal, []secret.Reference{ref}, secret.Target{Type: "vm", Ref: session.VMID, Purpose: "vm_exec"}, "vm_ssh_session", session.ID, session.Binding.ExecutionAgentID)
		if err != nil {
			return err
		}
		stage = "lease_encryption"
		raw, _ := json.Marshal(grant)
		session.LeaseID = grant.ID
		session.LeaseCredential, err = s.encryptCredentialJSON(map[string]any{"value": string(raw)})
		if err != nil {
			return err
		}
		stage = "activation"
		if err = s.ssh.Access.ActivateVMSSHSession(txCtx, session); err != nil {
			return err
		}
		stage = "command"
		values, err := s.decryptCredentialJSON(stringValue(task.Payload, "sshCommandCredential"))
		command := stringValue(values, "value")
		if err != nil || sshCommandDigest(command) != stringValue(task.Payload, "commandDigest") {
			return apperrors.ErrConflict
		}
		binding := session.Binding
		request := sohaapi.AgentSSHSessionRequest{SessionID: session.ID, AgentID: binding.ExecutionAgentID, ExecutionClusterID: binding.ExecutionClusterID, Address: binding.Address, Port: binding.Port, Username: binding.Username, TrustedHostKey: binding.TrustedHostKey, CredentialKind: sohaapi.SecretKind(binding.CredentialKind), ControlPlaneEndpoint: s.controlPlaneEndpoint(), ExpiresAt: session.ExpiresAt, Cols: 80, Rows: 24, Lease: sohaapi.SecretLeaseGrant{ID: grant.ID, Token: grant.Token, ExpiresAt: grant.ExpiresAt}}
		// Hydrated command and lease exist only in the authenticated claim response.
		task.Payload = map[string]any{"sshCommand": sohaapi.AgentSSHCommandRequest{Session: request, Command: command, TimeoutSeconds: task.TimeoutSeconds}}
		stage = "commit"
		return nil
	})
	if err != nil {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		s.recordOperation(auditCtx, identity.Principal{UserID: actorID}, "virtualization.vm.ssh.prepare", vmID, "", "failure", "SSH command preparation failed", map[string]any{"executionTaskId": task.ID, "stage": stage})
	}
	return task, err
}

func (s *Service) validateSSHCommandClaim(ctx context.Context, id, agentID string) error {
	current, err := s.ssh.Commands.GetExecutionTask(ctx, id)
	if err != nil || current.Status != "dispatching" || current.ClaimedByAgentID != agentID {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (s *Service) CheckVMSSHCommandAuthorization(ctx context.Context, id, agentToken string, input sohaapi.VMSSHCredentialRedemptionInput) (sohaapi.VirtualMachineSSHCommand, error) {
	if s.ssh.AuthenticateExecutionAgent == nil || s.ssh.Commands == nil {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrAccessDenied
	}
	if s.ssh.AuthenticateExecutionAgent(ctx, input.ClusterID, agentToken) != nil {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrAccessDenied
	}
	session, err := s.ssh.Access.GetVMSSHSession(ctx, id)
	if err != nil || !liveSSHCommandSession(session, input) {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrAccessDenied
	}
	values, err := s.decryptCredentialJSON(session.LeaseCredential)
	var grant secret.LeaseGrant
	if err != nil || json.Unmarshal([]byte(stringValue(values, "value")), &grant) != nil || subtle.ConstantTimeCompare([]byte(grant.Token), []byte(input.Token)) != 1 {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrAccessDenied
	}
	task, err := s.ssh.Commands.GetExecutionTask(ctx, id)
	if err != nil || task.TaskKind != vmSSHCommandKind || task.ClaimedByAgentID != input.AgentID || (task.Status != "dispatching" && task.Status != "running") {
		return sohaapi.VirtualMachineSSHCommand{}, apperrors.ErrAccessDenied
	}
	principal, err := s.currentSSHPrincipal(ctx, session)
	if err == nil {
		err = s.validateSSHBinding(ctx, principal, session.Binding)
	}
	if err == nil {
		_, _, err = s.ssh.Secrets.PinSSHCredential(ctx, principal, session.Binding.CredentialRef, secret.Target{Type: "vm", Ref: session.VMID, Purpose: "vm_exec"})
	}
	if err == nil {
		err = s.checkQueuedGatewayAuthorization(ctx, principal, vm.Task{Payload: task.Payload})
	}
	return commandReceipt(delivery.ExecutionTask{ID: task.ID, Status: task.Status, Payload: map[string]any{"vmId": session.VMID, "bindingRevision": session.BindingRevision}}), err
}

func (s *Service) RecordExecutionTaskResult(ctx context.Context, task delivery.ExecutionTask) error {
	if task.TaskKind != vmSSHCommandKind || !commandReceipt(task).Terminal {
		return nil
	}
	session, err := s.ssh.Access.GetVMSSHSession(ctx, task.ID)
	if err != nil {
		return err
	}
	status, reason := "unknown", "SSH command ended; guest effects require verification"
	if task.AttemptCount == 0 || commandReceipt(task).Outcome == "succeeded" || commandReceipt(task).Outcome == "failed" {
		status, reason = "closed", "original SSH command has a terminal receipt"
	}
	if session.Status == "pending" || session.Status == "active" {
		err = s.ssh.Access.TransitionVMSSHSession(ctx, session.ID, []string{session.Status}, status, reason)
	}
	if err != nil {
		return err
	}
	if err := s.ssh.Secrets.RevokeSubjectLeases(ctx, "vm_ssh_session", session.ID); err != nil {
		return err
	}
	if stringValue(task.Payload, "personalRuntimeCredential") == "" || s.personalRuntimeComplete == nil {
		return nil
	}
	values, err := s.decryptCredentialJSON(stringValue(task.Payload, "personalRuntimeCredential"))
	var op guest.RuntimeOperation
	if err != nil || json.Unmarshal([]byte(stringValue(values, "value")), &op) != nil {
		return apperrors.ErrConflict
	}
	if op.Source != "user" {
		return nil
	}
	var result guest.RuntimeResult
	view := commandReceipt(task)
	confirmed := view.Outcome == "succeeded" && !view.OutputTruncated && json.Unmarshal([]byte(view.Stdout), &result) == nil && result.ValidateFor(op) == nil
	return s.personalRuntimeComplete(ctx, op, result, confirmed)
}

func (s *Service) persistSSHCommand(ctx context.Context, task delivery.ExecutionTask, session vm.VMSSHSession) (delivery.ExecutionTask, error) {
	err := s.ssh.WithinCommand(ctx, task.ID, func(txCtx context.Context) error {
		existing, err := s.ssh.Commands.GetExecutionTask(txCtx, task.ID)
		if err == nil {
			if existing.TaskKind != vmSSHCommandKind || stringValue(existing.Payload, "requestDigest") != stringValue(task.Payload, "requestDigest") {
				return apperrors.ErrConflict
			}
			task = existing
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		if err = s.ssh.Access.CreateVMSSHSession(txCtx, session); err != nil {
			return err
		}
		task, err = s.ssh.Commands.CreateExecutionTask(txCtx, task)
		return err
	})
	return task, err
}

func liveSSHCommandSession(session vm.VMSSHSession, input sohaapi.VMSSHCredentialRedemptionInput) bool {
	return session.Purpose == "vm_exec" && session.Status == "active" && session.ExpiresAt.After(time.Now()) && session.Binding.ExecutionClusterID == input.ClusterID && session.Binding.ExecutionAgentID == input.AgentID && session.LeaseID == input.LeaseID
}
