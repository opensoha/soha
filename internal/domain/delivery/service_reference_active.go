package delivery

import (
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"slices"
	"strings"
	"time"
)

type ServiceReferenceInput struct {
	ExpectedCandidateID       string    `json:"expectedCandidateId"`
	ExpectedCandidateRevision int64     `json:"expectedCandidateRevision"`
	ExpectedConsumerUpdatedAt time.Time `json:"expectedConsumerUpdatedAt"`
	ExpectedSourceUpdatedAt   time.Time `json:"expectedSourceUpdatedAt"`
	ExpectedSourceRevision    string    `json:"expectedSourceRevision"`
	ExpectedRevision          *int64    `json:"expectedRevision"`
	ExpectedReferenceID       string    `json:"expectedReferenceId,omitempty"`
}

type ServiceReferenceDeleteInput struct {
	ExpectedReferenceID       string    `json:"expectedReferenceId"`
	ExpectedRevision          int64     `json:"expectedRevision"`
	ExpectedConsumerUpdatedAt time.Time `json:"expectedConsumerUpdatedAt"`
}

type ServiceReference struct {
	ID                     string                  `json:"id"`
	Revision               int64                   `json:"revision"`
	State                  string                  `json:"state"`
	SourceIdentityRevision string                  `json:"sourceIdentityRevision"`
	CurrentSourceRevision  string                  `json:"currentSourceRevision"`
	Preview                ServiceReferencePreview `json:"preview"`
	CreatedAt              time.Time               `json:"createdAt"`
	UpdatedAt              time.Time               `json:"updatedAt"`
}

// Record is private persistence/impact data; it never grants source execution.
type ServiceReferenceRecord struct {
	Reference                     ServiceReference
	ApplicationID                 string
	ConsumerBindingID             string
	ServiceID                     string
	SourceBindingID               string
	SourceTargetIDs               []string
	ControlRevision               string
	ConsumerConfigurationRevision string
	ConsumerEnvironment           domaincatalog.Environment `json:"-"`
}

type ServiceReferenceWrite struct {
	Record                    ServiceReferenceRecord
	ExpectedCandidateID       string
	ExpectedCandidateRevision int64
	ExpectedReferenceID       string
	ExpectedRevision          int64
	ConfigurationRevision     string
}

type ServiceReferenceSummary struct {
	ID        string    `json:"id"`
	ServiceID string    `json:"serviceId"`
	Revision  int64     `json:"revision"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ServiceReferencePage struct {
	Items      []ServiceReferenceSummary `json:"items"`
	NextCursor string                    `json:"nextCursor,omitempty"`
}

type ServiceReferenceResolvedTarget struct {
	ReferenceID       string                 `json:"referenceId"`
	ReferenceRevision int64                  `json:"referenceRevision"`
	ApplicationID     string                 `json:"applicationId"`
	ServiceID         string                 `json:"serviceId"`
	View              ServiceReferenceScope  `json:"view"`
	Origin            ServiceReferenceScope  `json:"origin"`
	Target            ServiceReferenceTarget `json:"target"`
	ObservedAt        time.Time              `json:"observedAt"`
}

// Control identity excludes ordinary Helm/Manifest/configuration versions.
func ServiceReferenceControlRevision(binding domaincatalog.ApplicationEnvironment, serviceID string) (string, error) {
	type control struct {
		ID, ClusterID, Namespace, TargetKind, ExecutorKind, ConfigRef, GroupKey, WaveKey, RegionKey, Kind, Name, ContainerName, OwnershipMode string
		Enabled                                                                                                                               bool
	}
	items := make([]control, 0)
	for _, t := range binding.Targets {
		if t.ResolvedServiceID() != serviceID {
			continue
		}
		mode, _ := t.Metadata["ownershipMode"].(string)
		items = append(items, control{t.ID, t.ClusterID, t.Namespace, t.TargetKind, t.ExecutorKind, t.ConfigRef, t.GroupKey, t.WaveKey, t.RegionKey, t.WorkloadKind, t.WorkloadName, t.ContainerName, mode, t.Enabled})
	}
	slices.SortFunc(items, func(a, b control) int { return strings.Compare(a.ID, b.ID) })
	return referenceDigest(items)
}
