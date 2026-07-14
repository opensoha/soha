package knowledge

import "time"

type ConnectorValidationResult struct {
	Kind        SourceKind `json:"kind"`
	Valid       bool       `json:"valid"`
	Host        string     `json:"host,omitempty"`
	Resource    string     `json:"resource,omitempty"`
	SecretRef   string     `json:"secretRef"`
	ConfigHash  string     `json:"configHash"`
	Warnings    []string   `json:"warnings"`
	ValidatedAt time.Time  `json:"validatedAt"`
}

type ConnectorDefinition struct {
	ID              string         `json:"id"`
	KnowledgeBaseID string         `json:"knowledgeBaseId"`
	Name            string         `json:"name"`
	Kind            SourceKind     `json:"kind"`
	Version         string         `json:"version"`
	SecretRef       string         `json:"secretRef"`
	Config          map[string]any `json:"config"`
	SyncPolicy      SyncPolicy     `json:"syncPolicy"`
	Status          SourceStatus   `json:"status"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type ConnectorInput struct {
	KnowledgeBaseID string         `json:"knowledgeBaseId"`
	Name            string         `json:"name"`
	Kind            SourceKind     `json:"kind"`
	Version         string         `json:"version,omitempty"`
	SecretRef       string         `json:"secretRef"`
	Config          map[string]any `json:"config"`
	SyncPolicy      SyncPolicy     `json:"syncPolicy"`
}

type IngestionStage string

const (
	IngestionStageDiscovering IngestionStage = "discovering"
	IngestionStageFetching    IngestionStage = "fetching"
	IngestionStageParsing     IngestionStage = "parsing"
	IngestionStageChunking    IngestionStage = "chunking"
	IngestionStageEmbedding   IngestionStage = "embedding"
	IngestionStageIndexing    IngestionStage = "indexing"
	IngestionStageVerifying   IngestionStage = "verifying"
	IngestionStagePublishing  IngestionStage = "publishing"
)

type IngestionJobStatus string

const (
	IngestionJobQueued     IngestionJobStatus = "queued"
	IngestionJobRunning    IngestionJobStatus = "running"
	IngestionJobRetryWait  IngestionJobStatus = "retry_wait"
	IngestionJobCancelling IngestionJobStatus = "cancelling"
	IngestionJobCancelled  IngestionJobStatus = "cancelled"
	IngestionJobFailed     IngestionJobStatus = "failed"
	IngestionJobSucceeded  IngestionJobStatus = "succeeded"
)

type IngestionCheckpoint struct {
	Stage           IngestionStage `json:"stage"`
	Cursor          string         `json:"cursor,omitempty"`
	DocumentsSeen   int            `json:"documentsSeen"`
	DocumentsStored int            `json:"documentsStored"`
	ChunksStored    int            `json:"chunksStored"`
	ContentHash     string         `json:"contentHash,omitempty"`
	RecordedAt      time.Time      `json:"recordedAt"`
}

type IngestionPrincipalSnapshot struct {
	UserID         string   `json:"userId"`
	Roles          []string `json:"roles"`
	PermissionKeys []string `json:"permissionKeys"`
}

type IngestionJob struct {
	ID                string                     `json:"id"`
	KnowledgeBaseID   string                     `json:"knowledgeBaseId"`
	SourceID          string                     `json:"sourceId"`
	TargetRevision    int64                      `json:"targetRevision"`
	Stage             IngestionStage             `json:"stage"`
	Status            IngestionJobStatus         `json:"status"`
	Attempt           int                        `json:"attempt"`
	MaxAttempts       int                        `json:"maxAttempts"`
	CancelRequested   bool                       `json:"cancelRequested"`
	Checkpoint        IngestionCheckpoint        `json:"checkpoint"`
	PrincipalSnapshot IngestionPrincipalSnapshot `json:"-"`
	ErrorCode         string                     `json:"errorCode,omitempty"`
	Error             string                     `json:"error,omitempty"`
	NextAttemptAt     *time.Time                 `json:"nextAttemptAt,omitempty"`
	LeaseToken        string                     `json:"-"`
	LeaseExpiresAt    *time.Time                 `json:"leaseExpiresAt,omitempty"`
	CreatedAt         time.Time                  `json:"createdAt"`
	UpdatedAt         time.Time                  `json:"updatedAt"`
	CompletedAt       *time.Time                 `json:"completedAt,omitempty"`
}

type IngestionStageRecord struct {
	JobID       string              `json:"jobId"`
	Sequence    int                 `json:"sequence"`
	Stage       IngestionStage      `json:"stage"`
	Status      IngestionJobStatus  `json:"status"`
	Checkpoint  IngestionCheckpoint `json:"checkpoint"`
	ErrorCode   string              `json:"errorCode,omitempty"`
	StartedAt   time.Time           `json:"startedAt"`
	CompletedAt *time.Time          `json:"completedAt,omitempty"`
}

func (j IngestionJob) Terminal() bool {
	switch j.Status {
	case IngestionJobCancelled, IngestionJobFailed, IngestionJobSucceeded:
		return true
	default:
		return false
	}
}
