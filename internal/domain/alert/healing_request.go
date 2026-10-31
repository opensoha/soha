package alert

import "time"

// HealingRequest is a tombstone as well as a request binding. It deliberately
// has no cascading foreign key: removing a run must never free the UUID.
type HealingRequest struct {
	ID, ActorID, CredentialHash, Key, EventID, PolicyID, InputDigest, RunID string
	CreatedAt                                                               time.Time
}

type HealingOriginalRun struct {
	ID                  string    `json:"id"`
	EventID             string    `json:"eventId"`
	PolicyID            string    `json:"policyId"`
	Status              string    `json:"status"`
	ApprovalStatus      string    `json:"approvalStatus"`
	ExecutionGeneration int64     `json:"executionGeneration"`
	SubmissionStatus    string    `json:"submissionStatus"`
	WorkflowRunID       string    `json:"workflowRunId,omitempty"`
	IntentDigest        string    `json:"intentDigest,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type HealingOriginalReceipt struct {
	Protocol       string             `json:"protocol"`
	IdempotencyKey string             `json:"idempotencyKey"`
	Run            HealingOriginalRun `json:"run"`
}
