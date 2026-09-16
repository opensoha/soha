package deliverydocument

import (
	"time"

	contractdelivery "github.com/opensoha/soha-contracts/delivery"
	contractsapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type File = contractsapi.DeliveryDocumentFile
type PreviewInput = contractsapi.DeliveryDocumentPreviewInput
type ApplyInput = contractsapi.DeliveryDocumentApplyInput

type Candidate struct {
	Path                 string                    `json:"path"`
	Document             contractdelivery.Document `json:"document"`
	Action               string                    `json:"action"`
	TargetID             string                    `json:"targetId,omitempty"`
	ExpectedRevision     int64                     `json:"expectedRevision,omitempty"`
	SourceDigest         string                    `json:"sourceDigest"`
	NormalizedSpecDigest string                    `json:"normalizedSpecDigest"`
	ChangedPaths         []string                  `json:"changedPaths"`
}

type Preview struct {
	ID              string                        `json:"id,omitempty"`
	Valid           bool                          `json:"valid"`
	CandidateDigest string                        `json:"candidateDigest,omitempty"`
	ExpiresAt       *time.Time                    `json:"expiresAt,omitempty"`
	Candidates      []Candidate                   `json:"candidates"`
	Diagnostics     []contractdelivery.Diagnostic `json:"diagnostics"`
}

type StoredPreview struct {
	Preview
	ActorID        string
	IdempotencyKey string
	Result         *Import
}

type ImportedObject struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Action   string `json:"action"`
}

type Import struct {
	PreviewID string           `json:"previewId"`
	Objects   []ImportedObject `json:"objects"`
}

type Export struct {
	Format               string                    `json:"format"`
	Content              string                    `json:"content"`
	Document             contractdelivery.Document `json:"document"`
	NormalizedSpecDigest string                    `json:"normalizedSpecDigest"`
}

// Mutation is prepared and authorized by the application service. Persistence
// selects the existing repository write using Candidate.Document.Kind.
type Mutation struct {
	Candidate
	Build      domaincatalog.BuildTemplateInput
	Deployment domaincatalog.DeploymentTemplateInput
	Template   domaincatalog.WorkflowTemplateInput
	Workflow   domainworkflow.DeliveryWorkflowInput
}
