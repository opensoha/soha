package deliverydocument

import (
	"time"

	contractdelivery "github.com/opensoha/soha-contracts/delivery"
	contractsapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type Source = contractsapi.DeliveryTemplateSource
type SourceInput = contractsapi.DeliveryTemplateSourceInput
type SourceRemoveInput = contractsapi.DeliveryTemplateSourceRemoveInput
type SyncInput = contractsapi.DeliveryTemplateSyncInput
type SyncApplyInput = contractsapi.DeliveryTemplateSyncApplyInput
type Association = contractsapi.DeliveryTemplateSourceAssociation
type Provenance = contractsapi.DeliveryTemplateProvenance

type StoredAssociation struct {
	Association
	Document contractdelivery.Document `json:"-"`
}

type SourceInfo struct {
	Repository  *contractsapi.DeliveryDocumentRepository `json:"repository,omitempty"`
	Association *Association                             `json:"association,omitempty"`
	Provenance  *Provenance                              `json:"provenance,omitempty"`
}

type SyncRun struct {
	ID               string        `json:"id"`
	SourceID         string        `json:"sourceId"`
	SourceGeneration int           `json:"sourceGeneration"`
	Status           string        `json:"status"`
	ActorID          string        `json:"actorId"`
	RequestedCommit  string        `json:"requestedCommit,omitempty"`
	ResolvedCommit   string        `json:"resolvedCommit,omitempty"`
	TreeDigest       string        `json:"treeDigest,omitempty"`
	Preview          *Preview      `json:"preview,omitempty"`
	Removed          []Association `json:"removed,omitempty"`
	Result           *Import       `json:"result,omitempty"`
	ErrorCode        string        `json:"errorCode,omitempty"`
	ErrorMessage     string        `json:"errorMessage,omitempty"`
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
}

type StoredSyncRun struct {
	SyncRun
	IdempotencyKey string
	ApplyKey       string
	RepositoryID   string
}

type GitDocuments struct {
	ResolvedCommit string
	TreeDigest     string
	Files          []File
}
