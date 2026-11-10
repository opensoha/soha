package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
)

type ServiceReferenceCandidateInput struct {
	SourceApplicationEnvironmentID string    `json:"sourceApplicationEnvironmentId"`
	ExpectedConsumerUpdatedAt      time.Time `json:"expectedConsumerUpdatedAt"`
	ExpectedSourceUpdatedAt        time.Time `json:"expectedSourceUpdatedAt"`
	SourceTargetIDs                []string  `json:"sourceTargetIds"`
	ExpectedSourceRevision         string    `json:"expectedSourceRevision"`
	ExpectedCandidateID            string    `json:"expectedCandidateId,omitempty"`
	ExpectedRevision               *int64    `json:"expectedRevision"`
}

func (i ServiceReferenceCandidateInput) PreviewInput(serviceID string) ServiceReferencePreviewInput {
	return ServiceReferencePreviewInput{ServiceID: serviceID, SourceApplicationEnvironmentID: i.SourceApplicationEnvironmentID,
		ExpectedConsumerUpdatedAt: i.ExpectedConsumerUpdatedAt, ExpectedSourceUpdatedAt: i.ExpectedSourceUpdatedAt,
		SourceTargetIDs: i.SourceTargetIDs, ExpectedSourceRevision: i.ExpectedSourceRevision}
}

type ServiceReferenceCandidateDeleteInput struct {
	ExpectedCandidateID       string    `json:"expectedCandidateId"`
	ExpectedRevision          int64     `json:"expectedRevision"`
	ExpectedConsumerUpdatedAt time.Time `json:"expectedConsumerUpdatedAt"`
}

// Candidates are configuration proposals, never runtime targets or write permits.
type ServiceReferenceCandidate struct {
	ID                     string                  `json:"id"`
	Revision               int64                   `json:"revision"`
	State                  string                  `json:"state"`
	Status                 string                  `json:"status"`
	SourceIdentityRevision string                  `json:"sourceIdentityRevision"`
	CurrentSourceRevision  string                  `json:"currentSourceRevision"`
	Preview                ServiceReferencePreview `json:"preview"`
	CreatedAt              time.Time               `json:"createdAt"`
	UpdatedAt              time.Time               `json:"updatedAt"`
	ContextRevision        string                  `json:"-"`
}

type ServiceReferenceCandidateWrite struct {
	Candidate           ServiceReferenceCandidate
	ExpectedCandidateID string
	ExpectedRevision    int64
}

type ServiceReferenceCandidateSummary struct {
	ID        string    `json:"id"`
	ServiceID string    `json:"serviceId"`
	Revision  int64     `json:"revision"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ServiceReferenceCandidatePage struct {
	Items      []ServiceReferenceCandidateSummary `json:"items"`
	NextCursor string                             `json:"nextCursor,omitempty"`
}

type ServiceReferenceConfiguration struct {
	Consumer            domaincatalog.ApplicationEnvironment
	Source              domaincatalog.ApplicationEnvironment
	Service             domainapp.Service
	ConsumerEnvironment domaincatalog.Environment
	SourceEnvironment   domaincatalog.Environment
}

func (c ServiceReferenceConfiguration) Revision() (string, error) { return referenceDigest(c) }

// Identity comparison deliberately excludes ordinary configuration/runtime versions.
func ServiceReferenceIdentityRevision(preview ServiceReferencePreview) (string, error) {
	type resourceIdentity struct{ ClusterID, Namespace, APIVersion, Kind, Name, UID, ScopeMode, CRDName, Plural string }
	type targetIdentity struct {
		TargetID, ClusterID, Namespace, Kind, Name, OwnershipMode string
		Resources                                                 []resourceIdentity
	}
	targets := make([]targetIdentity, 0, len(preview.Targets))
	for _, target := range preview.Targets {
		item := targetIdentity{TargetID: target.TargetID, ClusterID: target.ClusterID, Namespace: target.Namespace,
			Kind: target.WorkloadKind, Name: target.WorkloadName, OwnershipMode: target.OwnershipMode, Resources: make([]resourceIdentity, 0, len(target.Resources))}
		for _, source := range target.Resources {
			r := source.Resource
			item.Resources = append(item.Resources, resourceIdentity{ClusterID: r.ClusterID, Namespace: r.Namespace,
				APIVersion: r.APIVersion, Kind: r.Kind, Name: r.Name, UID: r.UID, ScopeMode: string(r.ScopeMode), CRDName: source.CRDName, Plural: source.Plural})
		}
		slices.SortFunc(item.Resources, func(a, b resourceIdentity) int {
			return strings.Compare(a.APIVersion+"/"+a.Kind+"/"+a.Name+"/"+a.UID, b.APIVersion+"/"+b.Kind+"/"+b.Name+"/"+b.UID)
		})
		targets = append(targets, item)
	}
	slices.SortFunc(targets, func(a, b targetIdentity) int { return strings.Compare(a.TargetID, b.TargetID) })
	return referenceDigest(struct {
		ApplicationID, ServiceID, SourceBindingID string
		Targets                                   []targetIdentity
	}{ApplicationID: preview.ApplicationID, ServiceID: preview.ServiceID, SourceBindingID: preview.Source.ApplicationEnvironmentID, Targets: targets})
}

func referenceDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
