package virtualization

import (
	"context"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
)

type VMIdentity struct {
	ProviderVMID string
	Name         string
	Node         string
	Generation   string
	Status       string
	IPAddresses  []string
	DataDisks    []CapacityDiskAllocation
}

type VMIdentityProvider interface {
	ObserveVMIdentity(context.Context, AdapterConnection, AdapterVM) (VMIdentity, error)
}

type VMGuestBootstrapProvider interface {
	ValidateGuestBootstrap(context.Context, AdapterConnection, AdapterCreateVMInput) (VMIdentity, error)
}

type VMAccessBinding struct {
	VMID               string            `json:"vmId"`
	Revision           int               `json:"revision"`
	ProviderVMID       string            `json:"providerVmId"`
	SourceID           string            `json:"sourceId"`
	Generation         string            `json:"generation"`
	Username           string            `json:"username"`
	Port               int               `json:"port"`
	Address            string            `json:"address"`
	CredentialRef      string            `json:"credentialRef"`
	CredentialKind     domainsecret.Kind `json:"credentialKind"`
	ExecutionClusterID string            `json:"executionClusterId"`
	ExecutionAgentID   string            `json:"executionAgentId"`
	TrustedHostKey     string            `json:"trustedHostKey"`
	HostKeyFingerprint string            `json:"hostKeyFingerprint"`
	Status             string            `json:"status"`
	Reason             string            `json:"reason,omitempty"`
	UpdatedAt          time.Time         `json:"updatedAt"`
	UpdatedBy          string            `json:"updatedBy"`
}

type VMSSHSession struct {
	BrowserSessionID string                   `json:"browserSessionId,omitempty"`
	Purpose          string                   `json:"purpose"`
	ExecutionTokenID string                   `json:"executionTokenId"`
	ID               string                   `json:"id"`
	VMID             string                   `json:"vmId"`
	BindingRevision  int                      `json:"bindingRevision"`
	Status           string                   `json:"status"`
	ExpiresAt        time.Time                `json:"expiresAt"`
	Reason           string                   `json:"reason,omitempty"`
	Binding          VMAccessBinding          `json:"binding"`
	Principal        domainidentity.Principal `json:"principal"`
	LeaseCredential  string                   `json:"-"`
	LeaseID          string                   `json:"leaseId"`
	Cols             int                      `json:"cols"`
	Rows             int                      `json:"rows"`
	CreatedAt        time.Time                `json:"createdAt"`
}

// VMSecretUsage is a reference fact, not proof that a guest still accepts the key.
type VMSecretUsage struct {
	TargetID        string
	VMID            string
	Usage           string
	Version         int
	BindingRevision int
	Status          string
}
