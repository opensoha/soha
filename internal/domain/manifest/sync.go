package manifest

import "time"

const (
	SyncRunQueued    = "queued"
	SyncRunRunning   = "running"
	SyncRunSucceeded = "succeeded"
	SyncRunFailed    = "failed"
	SyncRunIgnored   = "ignored"
)

type SyncInput struct {
	ExpectedGeneration int64  `json:"expectedGeneration"`
	RequestedCommit    string `json:"requestedCommit,omitempty"`
}

type SyncWebhookInput struct {
	RepositoryID string `json:"repositoryId"`
	Ref          string `json:"ref"`
	Commit       string `json:"commit,omitempty"`
	EventID      string `json:"eventId"`
}

type SyncRun struct {
	ID               string     `json:"id"`
	SourceID         string     `json:"sourceId"`
	PackageID        string     `json:"packageId"`
	ExecutionTaskID  string     `json:"executionTaskId,omitempty"`
	SourceGeneration int64      `json:"sourceGeneration"`
	Trigger          string     `json:"trigger"`
	Status           string     `json:"status"`
	IdempotencyKey   string     `json:"idempotencyKey"`
	RequestedCommit  string     `json:"requestedCommit,omitempty"`
	ResolvedCommit   string     `json:"resolvedCommit,omitempty"`
	TreeDigest       string     `json:"treeDigest,omitempty"`
	CanonicalDigest  string     `json:"canonicalDigest,omitempty"`
	Files            []string   `json:"files"`
	Revision         int        `json:"revision,omitempty"`
	ErrorCode        string     `json:"errorCode,omitempty"`
	ErrorMessage     string     `json:"errorMessage,omitempty"`
	Actor            string     `json:"actor,omitempty"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	FinishedAt       *time.Time `json:"finishedAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}
