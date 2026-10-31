package workflow

import "time"

// Request receipts outlive their runs. They are an at-most-once submission
// fence, not permission to replay an interrupted legacy application runner.
type TriggerRequest struct {
	ID            string
	ActorID       string
	Key           string
	InputDigest   string
	ApplicationID string
	BindingID     string
	RunID         string
	Status        string
	ErrorCode     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type TriggerReceipt struct {
	ID               string `json:"id"`
	RequestKey       string `json:"requestKey"`
	RunID            string `json:"runId"`
	SubmissionStatus string `json:"submissionStatus"`
	ErrorCode        string `json:"errorCode,omitempty"`
	Run              *Run   `json:"run,omitempty"`
}
