package virtualization

import (
	"context"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

type VMAccessRepository interface {
	GetVMAccessBinding(context.Context, string) (domain.VMAccessBinding, error)
	SaveVMAccessBinding(context.Context, domain.VMAccessBinding, int) (domain.VMAccessBinding, error)
	CreateVMSSHSession(context.Context, domain.VMSSHSession) error
	GetVMSSHSession(context.Context, string) (domain.VMSSHSession, error)
	TransitionVMSSHSession(context.Context, string, []string, string, string) error
	ActivateVMSSHSession(context.Context, domain.VMSSHSession) error
}

type SSHCredentialProvider interface {
	PinSSHCredential(context.Context, domainidentity.Principal, string, domainsecret.Target) (domainsecret.Reference, domainsecret.Kind, error)
	IssueLease(context.Context, domainidentity.Principal, []domainsecret.Reference, domainsecret.Target, string, string, string) (*domainsecret.LeaseGrant, error)
	RedeemVMSSHLease(context.Context, string, string, string, string) (sohaapi.SecretLeaseRedemption, error)
	RevokeSubjectLeases(context.Context, string, string) error
}

type SSHChannel interface {
	ReadFrame() (sohaapi.VirtualMachineSSHFrame, error)
	WriteFrame(sohaapi.VirtualMachineSSHFrame) error
	Close() error
}

type SSHExecutor interface {
	Capability(context.Context, string) (sohaapi.AgentSSHCapability, error)
	OpenSession(context.Context, string, sohaapi.AgentSSHSessionRequest) (SSHChannel, error)
}

type SSHOptions struct {
	Access                     VMAccessRepository
	Secrets                    SSHCredentialProvider
	Executor                   SSHExecutor
	ControlPlaneEndpoint       func() string
	AuthorizeExecutionCluster  func(context.Context, domainidentity.Principal, string) error
	AuthenticateExecutionAgent func(context.Context, string, string) error
	Commands                   VMSSHCommandRepository
	CancelCommand              func(context.Context, string, domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error)
	CurrentSessionPrincipal    func(context.Context, string, string) (domainidentity.Principal, error)
	WithinCommand              func(context.Context, string, func(context.Context) error) error
}

type VMSSHCommandRepository interface {
	CreateExecutionTask(context.Context, domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error)
	GetExecutionTask(context.Context, string) (domaindelivery.ExecutionTask, error)
}
