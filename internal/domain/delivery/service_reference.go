package delivery

import (
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

// ServiceReferencePreviewInput describes a read-only comparison, not a write grant.
type ServiceReferencePreviewInput struct {
	ServiceID                      string    `json:"serviceId"`
	SourceApplicationEnvironmentID string    `json:"sourceApplicationEnvironmentId"`
	ExpectedConsumerUpdatedAt      time.Time `json:"expectedConsumerUpdatedAt"`
	ExpectedSourceUpdatedAt        time.Time `json:"expectedSourceUpdatedAt"`
	SourceTargetIDs                []string  `json:"sourceTargetIds,omitempty"`
	ExpectedSourceRevision         string    `json:"expectedSourceRevision,omitempty"`
}

type ServiceReferenceScope struct {
	ApplicationEnvironmentID string    `json:"applicationEnvironmentId"`
	EnvironmentID            string    `json:"environmentId"`
	EnvironmentKey           string    `json:"environmentKey"`
	Tier                     string    `json:"tier"`
	StageLevel               int       `json:"stageLevel"`
	IsProduction             bool      `json:"isProduction"`
	UpdatedAt                time.Time `json:"updatedAt"`
}

type ServiceReferenceResource struct {
	Resource   domainresource.ResourceRef `json:"resource"`
	SpecDigest string                     `json:"specDigest"`
	CRDName    string                     `json:"crdName,omitempty"`
	Plural     string                     `json:"plural,omitempty"`
}

type ServiceReferenceTarget struct {
	TargetID      string                     `json:"targetId"`
	ClusterID     string                     `json:"clusterId"`
	Namespace     string                     `json:"namespace"`
	WorkloadKind  string                     `json:"workloadKind"`
	WorkloadName  string                     `json:"workloadName"`
	OwnershipMode string                     `json:"ownershipMode"`
	HelmRevision  int                        `json:"helmRevision,omitempty"`
	Resources     []ServiceReferenceResource `json:"resources"`
}

type ServiceReferencePreview struct {
	ApplicationID         string                   `json:"applicationId"`
	ServiceID             string                   `json:"serviceId"`
	Consumer              ServiceReferenceScope    `json:"consumer"`
	Source                ServiceReferenceScope    `json:"source"`
	SourceRevision        string                   `json:"sourceRevision"`
	Targets               []ServiceReferenceTarget `json:"targets"`
	CurrentLocalTargetIDs []string                 `json:"currentLocalTargetIds"`
	ObservedAt            time.Time                `json:"observedAt"`
}
