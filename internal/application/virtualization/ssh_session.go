package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type sshBrowserSessionKey struct{}

func WithSSHBrowserSession(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sshBrowserSessionKey{}, id)
}

func sshSessionDTO(session domain.VMSSHSession) sohaapi.VirtualMachineSSHSession {
	return sohaapi.VirtualMachineSSHSession{ID: session.ID, VMID: session.VMID, BindingRevision: session.BindingRevision, Status: sohaapi.VirtualMachineSSHSessionStatus(session.Status), ExpiresAt: session.ExpiresAt, Reason: session.Reason}
}

func (s *Service) CreateVMSSHSession(ctx context.Context, principal domainidentity.Principal, vmID string, input sohaapi.VirtualMachineSSHSessionInput) (sohaapi.VirtualMachineSSHSession, error) {
	if !s.sshConfigured(false) {
		return sohaapi.VirtualMachineSSHSession{}, apperrors.ErrUnsupportedOperation
	}
	if input.BindingRevision < 1 || input.Cols < 1 || input.Cols > 512 || input.Rows < 1 || input.Rows > 512 {
		return sohaapi.VirtualMachineSSHSession{}, apperrors.ErrInvalidArgument
	}
	binding, err := s.ssh.Access.GetVMAccessBinding(ctx, vmID)
	if err != nil {
		return sohaapi.VirtualMachineSSHSession{}, mapNotFound(err)
	}
	if binding.Revision != input.BindingRevision {
		return sohaapi.VirtualMachineSSHSession{}, apperrors.ErrConflict
	}
	if err = s.validateSSHBinding(ctx, principal, binding); err != nil {
		return sohaapi.VirtualMachineSSHSession{}, err
	}
	capability, err := s.ssh.Executor.Capability(ctx, binding.ExecutionClusterID)
	if err != nil || !supportsSSHExecution(capability, binding, false) {
		return sohaapi.VirtualMachineSSHSession{}, apperrors.ErrUnsupportedOperation
	}
	now := time.Now().UTC()
	session := domain.VMSSHSession{ID: uuid.NewString(), VMID: vmID, BindingRevision: binding.Revision, Status: "pending", Binding: binding, Principal: principal, ExecutionTokenID: principal.AccessTokenID, Cols: input.Cols, Rows: input.Rows, CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute)}
	session.Purpose = "vm_auth"
	session.BrowserSessionID, _ = ctx.Value(sshBrowserSessionKey{}).(string)
	ref, _, err := s.ssh.Secrets.PinSSHCredential(ctx, principal, binding.CredentialRef, domainsecret.Target{Type: "vm", Ref: vmID, Purpose: "vm_auth"})
	if err != nil {
		return sohaapi.VirtualMachineSSHSession{}, err
	}
	grant, err := s.ssh.Secrets.IssueLease(ctx, principal, []domainsecret.Reference{ref}, domainsecret.Target{Type: "vm", Ref: vmID, Purpose: "vm_auth"}, "vm_ssh_session", session.ID, binding.ExecutionAgentID)
	if err != nil {
		return sohaapi.VirtualMachineSSHSession{}, err
	}
	raw, err := json.Marshal(grant)
	if err == nil {
		session.LeaseCredential, err = s.encryptCredentialJSON(map[string]any{"value": string(raw)})
	}
	if err == nil {
		session.LeaseID = grant.ID
		err = s.ssh.Access.CreateVMSSHSession(ctx, session)
	}
	if err != nil {
		_ = s.ssh.Secrets.RevokeSubjectLeases(ctx, "vm_ssh_session", session.ID)
		return sohaapi.VirtualMachineSSHSession{}, err
	}
	s.recordOperation(ctx, principal, "virtualization.vm.ssh.open", vmID, binding.Username, "success", "created VM SSH session", map[string]any{"sessionId": session.ID, "bindingRevision": binding.Revision})
	return sshSessionDTO(session), nil
}

func (s *Service) ownedSSHSession(ctx context.Context, principal domainidentity.Principal, id string) (domain.VMSSHSession, error) {
	if s.ssh.Access == nil {
		return domain.VMSSHSession{}, apperrors.ErrUnsupportedOperation
	}
	session, err := s.ssh.Access.GetVMSSHSession(ctx, id)
	if err != nil {
		return session, mapNotFound(err)
	}
	if session.Principal.UserID != principal.UserID {
		return session, apperrors.ErrAccessDenied
	}
	if _, _, err = s.accessVM(ctx, principal, session.VMID, vmConnectPermission); err != nil {
		return session, err
	}
	return session, nil
}

func (s *Service) GetVMSSHSession(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.VirtualMachineSSHSession, error) {
	session, err := s.ownedSSHSession(ctx, principal, id)
	return sshSessionDTO(session), err
}

func (s *Service) CloseVMSSHSession(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.VirtualMachineSSHSession, error) {
	if s.ssh.Access == nil {
		return sohaapi.VirtualMachineSSHSession{}, apperrors.ErrUnsupportedOperation
	}
	session, err := s.ssh.Access.GetVMSSHSession(ctx, id)
	if err != nil {
		return sohaapi.VirtualMachineSSHSession{}, err
	}
	if session.Principal.UserID != principal.UserID {
		return sohaapi.VirtualMachineSSHSession{}, apperrors.ErrAccessDenied
	}
	if session.Status != "pending" && session.Status != "active" {
		return sshSessionDTO(session), nil
	}
	status, reason := "closed", "closed before dispatch"
	if session.Status == "active" {
		status, reason = "unknown", "connection canceled; guest side effects require verification"
	}
	if err = s.ssh.Access.TransitionVMSSHSession(ctx, id, []string{session.Status}, status, reason); err != nil {
		return sohaapi.VirtualMachineSSHSession{}, err
	}
	_ = s.ssh.Secrets.RevokeSubjectLeases(ctx, "vm_ssh_session", id)
	session.Status = status
	session.Reason = reason
	s.recordOperation(ctx, principal, "virtualization.vm.ssh.close", session.VMID, session.Binding.Username, "success", reason, map[string]any{"sessionId": id, "status": status})
	return sshSessionDTO(session), nil
}

func (s *Service) OpenVMSSHStream(ctx context.Context, principal domainidentity.Principal, id string) (SSHChannel, error) {
	session, err := s.ownedSSHSession(ctx, principal, id)
	if err != nil {
		return nil, err
	}
	if session.Purpose != "vm_auth" || session.Status != "pending" || !session.ExpiresAt.After(time.Now()) {
		return nil, apperrors.ErrConflict
	}
	if err = s.validateSSHBinding(ctx, principal, session.Binding); err != nil {
		return nil, err
	}
	if err = s.ssh.Access.TransitionVMSSHSession(ctx, id, []string{"pending"}, "active", ""); err != nil {
		return nil, err
	}
	channel, err := s.openAgentSSH(ctx, session)
	if err != nil {
		_ = s.ssh.Access.TransitionVMSSHSession(ctx, id, []string{"active"}, "unknown", "SSH dispatch failed; guest effects are unconfirmed")
		_ = s.ssh.Secrets.RevokeSubjectLeases(ctx, "vm_ssh_session", id)
		return nil, apperrors.ErrUnsupportedOperation
	}
	wrapped := &managedSSHChannel{SSHChannel: channel, done: make(chan struct{}), service: s, sessionID: id}
	go s.watchSSHSession(ctx, wrapped, session)
	return wrapped, nil
}

func (s *Service) openAgentSSH(ctx context.Context, session domain.VMSSHSession) (SSHChannel, error) {
	values, err := s.decryptCredentialJSON(session.LeaseCredential)
	if err != nil {
		return nil, err
	}
	var grant domainsecret.LeaseGrant
	if err = json.Unmarshal([]byte(stringValue(values, "value")), &grant); err != nil {
		return nil, err
	}
	if !grant.ExpiresAt.After(time.Now()) {
		return nil, apperrors.ErrConflict
	}
	binding := session.Binding
	return s.ssh.Executor.OpenSession(ctx, binding.ExecutionClusterID, sohaapi.AgentSSHSessionRequest{SessionID: session.ID, AgentID: binding.ExecutionAgentID, ExecutionClusterID: binding.ExecutionClusterID, Address: binding.Address, Port: binding.Port, Username: binding.Username, TrustedHostKey: binding.TrustedHostKey, CredentialKind: sohaapi.SecretKind(binding.CredentialKind), ControlPlaneEndpoint: s.controlPlaneEndpoint(), ExpiresAt: session.ExpiresAt, Cols: session.Cols, Rows: session.Rows, Lease: sohaapi.SecretLeaseGrant{ID: grant.ID, Token: grant.Token, ExpiresAt: grant.ExpiresAt}})
}

func (s *Service) RedeemVMSSHCredentials(ctx context.Context, id, agentToken string, input sohaapi.VMSSHCredentialRedemptionInput) (sohaapi.SecretLeaseRedemption, error) {
	if s.ssh.AuthenticateExecutionAgent == nil || s.ssh.Access == nil {
		return sohaapi.SecretLeaseRedemption{}, apperrors.ErrUnsupportedOperation
	}
	if err := s.ssh.AuthenticateExecutionAgent(ctx, input.ClusterID, agentToken); err != nil {
		return sohaapi.SecretLeaseRedemption{}, apperrors.ErrAccessDenied
	}
	session, err := s.ssh.Access.GetVMSSHSession(ctx, id)
	if err != nil {
		return sohaapi.SecretLeaseRedemption{}, apperrors.ErrAccessDenied
	}
	if session.Status != "active" || !session.ExpiresAt.After(time.Now()) || session.Binding.ExecutionClusterID != input.ClusterID || session.Binding.ExecutionAgentID != input.AgentID || session.LeaseID != input.LeaseID {
		return sohaapi.SecretLeaseRedemption{}, apperrors.ErrAccessDenied
	}
	if session.Purpose == "vm_exec" {
		if _, err := s.CheckVMSSHCommandAuthorization(ctx, id, agentToken, input); err != nil {
			return sohaapi.SecretLeaseRedemption{}, apperrors.ErrAccessDenied
		}
	}
	principal, err := s.currentSSHPrincipal(ctx, session)
	if err == nil {
		err = s.validateSSHBinding(ctx, principal, session.Binding)
	}
	if err != nil {
		return sohaapi.SecretLeaseRedemption{}, apperrors.ErrAccessDenied
	}
	return s.ssh.Secrets.RedeemVMSSHLease(ctx, input.LeaseID, input.Token, input.AgentID, id)
}

func (s *Service) currentSSHPrincipal(ctx context.Context, session domain.VMSSHSession) (domainidentity.Principal, error) {
	if session.BrowserSessionID != "" {
		if s.ssh.CurrentSessionPrincipal == nil {
			return domainidentity.Principal{}, apperrors.ErrAccessDenied
		}
		return s.ssh.CurrentSessionPrincipal(ctx, session.Principal.UserID, session.BrowserSessionID)
	}
	if s.executionPrincipals == nil {
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	return s.executionPrincipals.CurrentExecutionPrincipal(ctx, session.Principal.UserID, session.ExecutionTokenID)
}

func (s *Service) watchSSHSession(ctx context.Context, channel *managedSSHChannel, session domain.VMSSHSession) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-channel.done:
			return
		case <-ctx.Done():
			_ = channel.Close()
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := s.checkActiveSSHSession(checkCtx, session)
			cancel()
			if err != nil {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				_ = s.ssh.Access.TransitionVMSSHSession(cleanup, session.ID, []string{"active"}, "revoked", "session authorization or VM identity changed; guest effects require verification")
				_ = s.ssh.Secrets.RevokeSubjectLeases(cleanup, "vm_ssh_session", session.ID)
				stop()
				_ = channel.Close()
				return
			}
		}
	}
}

func (s *Service) checkActiveSSHSession(ctx context.Context, session domain.VMSSHSession) error {
	if !session.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("session expired")
	}
	current, err := s.ssh.Access.GetVMSSHSession(ctx, session.ID)
	if err != nil {
		return err
	}
	if current.Status != "active" {
		return apperrors.ErrConflict
	}
	principal, err := s.currentSSHPrincipal(ctx, session)
	if err != nil {
		return err
	}
	return s.validateSSHBinding(ctx, principal, session.Binding)
}

type managedSSHChannel struct {
	SSHChannel
	done      chan struct{}
	service   *Service
	sessionID string
	once      sync.Once
}

func (channel *managedSSHChannel) Close() error {
	var err error
	channel.once.Do(func() {
		close(channel.done)
		err = channel.SSHChannel.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = channel.service.ssh.Access.TransitionVMSSHSession(ctx, channel.sessionID, []string{"active"}, "unknown", "SSH connection ended without a terminal receipt; guest effects require verification")
		_ = channel.service.ssh.Secrets.RevokeSubjectLeases(ctx, "vm_ssh_session", channel.sessionID)
	})
	return err
}

func (channel *managedSSHChannel) ReadFrame() (sohaapi.VirtualMachineSSHFrame, error) {
	frame, err := channel.SSHChannel.ReadFrame()
	if err == nil && frame.Type == sohaapi.VirtualMachineSSHFrameTypeClose {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		status, reason := "closed", "SSH shell exited"
		if frame.ExitCode == nil {
			status, reason = "unknown", "SSH connection ended without an exit status; guest effects require verification"
		}
		_ = channel.service.ssh.Access.TransitionVMSSHSession(ctx, channel.sessionID, []string{"active"}, status, reason)
	}
	if errors.Is(err, context.Canceled) {
		_ = channel.Close()
	}
	return frame, err
}

func (s *Service) sshConfigured(commands bool) bool {
	if s.ssh.Access == nil || s.ssh.Executor == nil || s.ssh.Secrets == nil || s.controlPlaneEndpoint() == "" {
		return false
	}
	return !commands || (s.ssh.Commands != nil && s.ssh.WithinCommand != nil)
}

func (s *Service) controlPlaneEndpoint() string {
	if s.ssh.ControlPlaneEndpoint == nil {
		return ""
	}
	return s.ssh.ControlPlaneEndpoint()
}
func supportsSSHExecution(capability sohaapi.AgentSSHCapability, binding domain.VMAccessBinding, commands bool) bool {
	return capability.Available && capability.ProtocolVersion == 1 && capability.AgentID == binding.ExecutionAgentID && (!commands || capability.CommandsAvailable)
}
