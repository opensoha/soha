package resource

import "time"

type ResourceCreateBatchStatus string

const (
	ResourceCreateBatchRunning   ResourceCreateBatchStatus = "running"
	ResourceCreateBatchSucceeded ResourceCreateBatchStatus = "succeeded"
	ResourceCreateBatchFailed    ResourceCreateBatchStatus = "failed"
)

func (s ResourceCreateBatchStatus) Terminal() bool {
	return s == ResourceCreateBatchSucceeded || s == ResourceCreateBatchFailed
}

type ResourceCreateBatch struct {
	ID             string
	ActorID        string
	ClusterID      string
	IdempotencyKey string
	ContentHash    string
	Status         ResourceCreateBatchStatus
	Documents      []ResourceCreateExecutionDocument
	CreatedAt      time.Time
	UpdatedAt      time.Time
	FinishedAt     *time.Time
}

type ResourceCreateBatchClaim struct {
	Batch   ResourceCreateBatch
	Created bool
}
