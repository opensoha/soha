package virtualization

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/crypto/ssh"
)

const vmConnectPermission = "virtualization.vms.connect"
const vmAccessPermission = "virtualization.vm_access.update"

func bindingDTO(binding domain.VMAccessBinding) sohaapi.VirtualMachineAccessBinding {
	var result sohaapi.VirtualMachineAccessBinding
	raw, _ := json.Marshal(binding)
	_ = json.Unmarshal(raw, &result)
	return result
}

func (s *Service) accessVM(ctx context.Context, principal domainidentity.Principal, id, permission string) (domain.VM, domain.Connection, error) {
	if err := s.authorize(ctx, principal, permission); err != nil {
		return domain.VM{}, domain.Connection{}, err
	}
	vm, err := s.vms.GetVM(ctx, strings.TrimSpace(id))
	if err != nil {
		return vm, domain.Connection{}, mapNotFound(err)
	}
	connection, err := s.connections.GetConnection(ctx, vm.ConnectionID)
	if err != nil {
		return vm, connection, mapNotFound(err)
	}
	scope := connectionCapabilityScope(connection, vm.Namespace)
	scope["vmId"] = vm.ID
	if err = domain.CheckScope(ctx, scope); err != nil {
		return vm, connection, err
	}
	if !connection.Enabled || vm.Status == "deleted" {
		return vm, connection, apperrors.ErrUnsupportedOperation
	}
	return vm, connection, nil
}

func (s *Service) observeVMIdentity(ctx context.Context, vm domain.VM, connection domain.Connection) (domain.VMIdentity, string, error) {
	adapter, conn, err := s.adapterForConnection(ctx, connection)
	if err != nil {
		return domain.VMIdentity{}, "", err
	}
	observer, ok := adapter.(domain.VMIdentityProvider)
	if !ok {
		return domain.VMIdentity{}, "", apperrors.ErrUnsupportedOperation
	}
	identity, err := observer.ObserveVMIdentity(ctx, conn, domain.AdapterVM{ID: vm.ExternalID, Name: vm.Name, Node: vm.NodeName, Namespace: vm.Namespace, Metadata: adapterVMMetadata(vm)})
	if err != nil {
		return identity, "", err
	}
	if identity.ProviderVMID != vm.ExternalID || identity.Name != vm.Name || identity.Generation == "" {
		return identity, "", fmt.Errorf("%w: provider VM identity is unavailable or changed; synchronize before binding", apperrors.ErrConflict)
	}
	policy, err := s.capacityPolicy(ctx, connection)
	return identity, policy.SourceID, err
}

func (s *Service) GetVMAccessBinding(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.VirtualMachineAccessBinding, error) {
	if _, _, err := s.accessVM(ctx, principal, id, appaccess.PermVirtualizationVMsView); err != nil {
		return sohaapi.VirtualMachineAccessBinding{}, err
	}
	if s.ssh.Access == nil {
		return sohaapi.VirtualMachineAccessBinding{}, apperrors.ErrUnsupportedOperation
	}
	binding, err := s.ssh.Access.GetVMAccessBinding(ctx, id)
	return bindingDTO(binding), mapNotFound(err)
}

func (s *Service) SaveVMAccessBinding(ctx context.Context, principal domainidentity.Principal, id string, input sohaapi.VirtualMachineAccessBindingInput) (sohaapi.VirtualMachineAccessBinding, error) {
	vm, connection, err := s.accessVM(ctx, principal, id, vmAccessPermission)
	if err != nil {
		return sohaapi.VirtualMachineAccessBinding{}, err
	}
	if s.ssh.Access == nil || s.ssh.Secrets == nil || s.ssh.Executor == nil {
		return sohaapi.VirtualMachineAccessBinding{}, apperrors.ErrUnsupportedOperation
	}
	if err = validateVMAccessInput(input); err != nil {
		return sohaapi.VirtualMachineAccessBinding{}, err
	}
	if err = s.checkSSHExecutionCluster(ctx, principal, input.ExecutionClusterID); err != nil {
		return sohaapi.VirtualMachineAccessBinding{}, err
	}
	capability, err := s.ssh.Executor.Capability(ctx, input.ExecutionClusterID)
	if err != nil || !capability.Available || capability.AgentID != input.ExecutionAgentID || capability.ProtocolVersion != 1 {
		return sohaapi.VirtualMachineAccessBinding{}, fmt.Errorf("%w: requested SSH Agent is unavailable", apperrors.ErrUnsupportedOperation)
	}
	identity, source, err := s.observeVMIdentity(ctx, vm, connection)
	if err != nil {
		return sohaapi.VirtualMachineAccessBinding{}, err
	}
	if !identityContainsAddress(identity, input.Address) {
		return sohaapi.VirtualMachineAccessBinding{}, fmt.Errorf("%w: address is not observed on this VM", apperrors.ErrInvalidArgument)
	}
	ref, kind, err := s.ssh.Secrets.PinSSHCredential(ctx, principal, input.CredentialRef, domainsecret.Target{Type: "vm", Ref: vm.ID, Purpose: "vm_auth"})
	if err != nil {
		return sohaapi.VirtualMachineAccessBinding{}, err
	}
	key, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(input.TrustedHostKey))
	binding := domain.VMAccessBinding{VMID: vm.ID, ProviderVMID: identity.ProviderVMID, SourceID: source, Generation: identity.Generation, Username: input.Username, Address: netip.MustParseAddr(input.Address).Unmap().String(), Port: input.Port, CredentialRef: ref.URI, CredentialKind: kind, ExecutionClusterID: input.ExecutionClusterID, ExecutionAgentID: input.ExecutionAgentID, TrustedHostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), HostKeyFingerprint: ssh.FingerprintSHA256(key), Status: "ready", UpdatedAt: time.Now().UTC(), UpdatedBy: principal.UserID}
	binding, err = s.ssh.Access.SaveVMAccessBinding(ctx, binding, input.ExpectedRevision)
	if err == nil {
		s.recordOperation(ctx, principal, "virtualization.vm_access.update", vm.ID, vm.Name, "success", "updated VM login binding", map[string]any{"revision": binding.Revision, "hostKeyFingerprint": binding.HostKeyFingerprint, "credentialRef": binding.CredentialRef})
	}
	return bindingDTO(binding), err
}

func validateVMAccessInput(input sohaapi.VirtualMachineAccessBindingInput) error {
	address, err := netip.ParseAddr(input.Address)
	key, _, options, rest, keyErr := ssh.ParseAuthorizedKey([]byte(input.TrustedHostKey))
	if err != nil || address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() || address.IsLinkLocalUnicast() || input.ExpectedRevision < 0 || input.Port < 1 || input.Port > 65535 || !guestUsernamePattern.MatchString(input.Username) || input.Username == "root" || input.ExecutionClusterID == "" || input.ExecutionAgentID == "" || len(input.TrustedHostKey) > 16384 || keyErr != nil || key == nil || len(options) != 0 || strings.TrimSpace(string(rest)) != "" {
		return fmt.Errorf("%w: invalid VM login binding or trusted host key", apperrors.ErrInvalidArgument)
	}
	return nil
}

func identityContainsAddress(identity domain.VMIdentity, address string) bool {
	parsed, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(identity.IPAddresses, func(value string) bool {
		candidate, err := netip.ParseAddr(value)
		return err == nil && candidate.Unmap() == parsed.Unmap()
	})
}

func (s *Service) checkSSHExecutionCluster(ctx context.Context, principal domainidentity.Principal, id string) error {
	if s.ssh.AuthorizeExecutionCluster == nil {
		return apperrors.ErrUnsupportedOperation
	}
	return s.ssh.AuthorizeExecutionCluster(ctx, principal, id)
}

func (s *Service) validateSSHBinding(ctx context.Context, principal domainidentity.Principal, binding domain.VMAccessBinding) error {
	vm, connection, err := s.accessVM(ctx, principal, binding.VMID, vmConnectPermission)
	if err != nil {
		return err
	}
	current, err := s.ssh.Access.GetVMAccessBinding(ctx, binding.VMID)
	if err != nil {
		return err
	}
	if current.Revision != binding.Revision {
		return apperrors.ErrConflict
	}
	if err = s.checkSSHExecutionCluster(ctx, principal, binding.ExecutionClusterID); err != nil {
		return err
	}
	identity, source, err := s.observeVMIdentity(ctx, vm, connection)
	if err != nil {
		return err
	}
	if source != binding.SourceID || identity.Generation != binding.Generation || identity.ProviderVMID != binding.ProviderVMID || !identityContainsAddress(identity, binding.Address) || !strings.EqualFold(identity.Status, "running") {
		return fmt.Errorf("%w: VM identity, endpoint or running state changed", apperrors.ErrConflict)
	}
	ref, _, err := s.ssh.Secrets.PinSSHCredential(ctx, principal, binding.CredentialRef, domainsecret.Target{Type: "vm", Ref: vm.ID, Purpose: "vm_auth"})
	if err == nil && ref.URI != binding.CredentialRef {
		return apperrors.ErrConflict
	}
	return err
}

func (s *Service) ListCredentialReferences(ctx context.Context, principal domainidentity.Principal, secretID string) ([]sohaapi.SecretReferenceUsage, error) {
	reader, ok := s.ssh.Access.(interface {
		ListVMSecretUsages(context.Context, string) ([]domain.VMSecretUsage, error)
	})
	if !ok {
		return nil, apperrors.ErrUnsupportedOperation
	}
	usages, err := reader.ListVMSecretUsages(ctx, secretID)
	if err != nil {
		return nil, err
	}
	result := make([]sohaapi.SecretReferenceUsage, 0, len(usages))
	for _, usage := range usages {
		if usage.Usage == "cloud_init_operation" {
			if _, err = s.GetOperation(ctx, principal, usage.TargetID); err != nil {
				continue
			}
		} else if _, _, err = s.accessVM(ctx, principal, usage.VMID, "virtualization.vms.view"); err != nil {
			continue
		}
		result = append(result, sohaapi.SecretReferenceUsage{TargetID: usage.TargetID, VMID: usage.VMID, Usage: usage.Usage, Version: usage.Version, BindingRevision: usage.BindingRevision, Status: usage.Status})
	}
	return result, nil
}
