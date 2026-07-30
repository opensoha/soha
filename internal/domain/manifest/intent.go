package manifest

import "time"

const (
	IntentStatusDraft    = "draft"
	IntentStatusAccepted = "accepted"
	IntentStatusRejected = "rejected"
)

type DeliveryIntentInput struct {
	BindingID             string   `json:"bindingId,omitempty"`
	Files                 []File   `json:"files"`
	Provider              string   `json:"provider"`
	Model                 string   `json:"model"`
	PromptTemplateVersion string   `json:"promptTemplateVersion"`
	RequestID             string   `json:"requestId,omitempty"`
	EvidenceDigest        string   `json:"evidenceDigest"`
	EvidenceRefs          []string `json:"evidenceRefs,omitempty"`
	Rationale             string   `json:"rationale"`
	Risk                  string   `json:"risk"`
}

type DeliveryIntentDecisionInput struct {
	ExpectedCurrentRevision  int       `json:"expectedCurrentRevision"`
	ExpectedPackageUpdatedAt time.Time `json:"expectedPackageUpdatedAt"`
	Comment                  string    `json:"comment,omitempty"`
}

type DeliveryIntent struct {
	ID                    string          `json:"id"`
	PackageID             string          `json:"packageId"`
	BindingID             string          `json:"bindingId,omitempty"`
	Status                string          `json:"status"`
	Files                 []File          `json:"files"`
	Provider              string          `json:"provider"`
	Model                 string          `json:"model"`
	PromptTemplateVersion string          `json:"promptTemplateVersion"`
	RequestID             string          `json:"requestId,omitempty"`
	EvidenceDigest        string          `json:"evidenceDigest"`
	EvidenceRefs          []string        `json:"evidenceRefs"`
	ProposalDigest        string          `json:"proposalDigest"`
	Rationale             string          `json:"rationale"`
	Risk                  string          `json:"risk"`
	Validation            PreflightResult `json:"validation"`
	DecisionComment       string          `json:"decisionComment,omitempty"`
	CreatedBy             string          `json:"createdBy"`
	DecidedBy             string          `json:"decidedBy,omitempty"`
	DecidedAt             *time.Time      `json:"decidedAt,omitempty"`
	CreatedAt             time.Time       `json:"createdAt"`
	UpdatedAt             time.Time       `json:"updatedAt"`
}
