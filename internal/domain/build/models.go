package build

import (
	"context"
	"time"
)

type Record struct {
	ID            string         `json:"id"`
	ApplicationID string         `json:"applicationId"`
	SourceSystem  string         `json:"sourceSystem"`
	Status        string         `json:"status"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	StartedAt     *time.Time     `json:"startedAt,omitempty"`
	FinishedAt    *time.Time     `json:"finishedAt,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type TriggerInput struct {
	ApplicationID            string          `json:"applicationId"`
	ServiceID                string          `json:"serviceId,omitempty"`
	RepositoryID             string          `json:"repositoryId,omitempty"`
	ApplicationEnvironmentID string          `json:"applicationEnvironmentId,omitempty"`
	BuildSourceID            string          `json:"buildSourceId,omitempty"`
	RefType                  string          `json:"refType"`
	RefName                  string          `json:"refName"`
	RepositoryRefs           []RepositoryRef `json:"repositoryRefs,omitempty"`
	ImageTag                 string          `json:"imageTag"`
	BuildArgs                map[string]any  `json:"buildArgs,omitempty"`
	Variables                map[string]any  `json:"variables,omitempty"`
	TriggeredByWorkflowRunID string          `json:"triggeredByWorkflowRunId,omitempty"`
	ResolvedCommit           string          `json:"resolvedCommit,omitempty"`
}

// Prepared is a server-owned build intent persisted inside a delivery snapshot.
// It is never accepted as an HTTP trigger input.
type Prepared struct {
	Input        TriggerInput   `json:"input"`
	ImageTag     string         `json:"imageTag"`
	ImageRef     string         `json:"imageRef"`
	SourceType   string         `json:"sourceType"`
	ProviderKind string         `json:"providerKind"`
	Metadata     map[string]any `json:"metadata"`
	Fingerprint  string         `json:"fingerprint"`
}

type RepositoryRef struct {
	RepositoryID string `json:"repositoryId"`
	RefType      string `json:"refType"`
	RefName      string `json:"refName"`
}

type Filter struct {
	BuildSourceID string
	ApplicationID string
	Limit         int
}

type Repository interface {
	List(context.Context, Filter) ([]Record, error)
	Get(context.Context, string) (Record, error)
	GetByExecutionTaskID(context.Context, string) (Record, error)
	Create(context.Context, TriggerInput, map[string]any) (Record, error)
	Update(context.Context, Record) (Record, error)
}
