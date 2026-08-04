package secret

import (
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type ScopeType string

const (
	ScopeWorkspace   ScopeType = "workspace"
	ScopeProject     ScopeType = "project"
	ScopeEnvironment ScopeType = "environment"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

type VersionStatus string

const (
	VersionActive  VersionStatus = "active"
	VersionRevoked VersionStatus = "revoked"
)

type SourceType string

const (
	SourceLocal    SourceType = "local"
	SourceVaultKV2 SourceType = "vault_kv2"
)

type Binding struct {
	TargetType string `json:"targetType"`
	TargetRef  string `json:"targetRef"`
}

type Secret struct {
	ID             string
	Name           string
	Description    string
	ScopeType      ScopeType
	ScopeID        string
	Status         Status
	CurrentVersion int
	Bindings       []Binding
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Version struct {
	SecretID   string
	Version    int
	SourceType SourceType
	Ciphertext string
	VaultKV2   *VaultKV2Reference
	Status     VersionStatus
	CreatedBy  string
	CreatedAt  time.Time
	RevokedAt  *time.Time
}

type VaultKV2Reference struct {
	Mount   string `json:"mount"`
	Path    string `json:"path"`
	Key     string `json:"key"`
	Version int    `json:"version"`
}

type CreateInput struct {
	Name        string
	Description string
	Value       *string
	VaultKV2    *VaultKV2Reference
	ScopeType   ScopeType
	ScopeID     string
	Bindings    []Binding
}

type RotateInput struct {
	Value    *string
	VaultKV2 *VaultKV2Reference
}

type Filter struct {
	ScopeType ScopeType
	ScopeID   string
}

type UpdateInput struct {
	Name        *string
	Description *string
	Status      *Status
	Bindings    *[]Binding
}

type Reference struct {
	Alias    string
	SecretID string
	Version  int
	URI      string
}

type Target struct {
	Type string
	Ref  string
}

type ExecutionContext struct {
	References []Reference
	Principal  domainidentity.Principal
	Target     Target
}

type LeaseGrant struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Lease struct {
	ID          string
	TokenHash   string
	AgentID     string
	SubjectType string
	SubjectID   string
	Target      Target
	References  []Reference
	Principal   domainidentity.Principal
	ExpiresAt   time.Time
	RedeemedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}
