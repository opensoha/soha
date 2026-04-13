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
	ApplicationID string         `json:"applicationId"`
	RefType       string         `json:"refType"`
	RefName       string         `json:"refName"`
	ImageTag      string         `json:"imageTag"`
	BuildArgs     map[string]any `json:"buildArgs,omitempty"`
}

type Filter struct {
	ApplicationID string
	Limit         int
}

type Repository interface {
	List(context.Context, Filter) ([]Record, error)
	Create(context.Context, TriggerInput, map[string]any) (Record, error)
}
