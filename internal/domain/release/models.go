package release

import (
	"context"
	"time"
)

type Record struct {
	ID             string         `json:"id"`
	ApplicationID  string         `json:"applicationId"`
	ClusterID      string         `json:"clusterId"`
	Namespace      string         `json:"namespace"`
	DeploymentName string         `json:"deploymentName"`
	Status         string         `json:"status"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	DeployedAt     *time.Time     `json:"deployedAt,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
}

type TriggerInput struct {
	ApplicationID            string `json:"applicationId"`
	ApplicationEnvironmentID string `json:"applicationEnvironmentId,omitempty"`
	ReleaseBundleID          string `json:"releaseBundleId,omitempty"`
	ClusterID                string `json:"clusterId"`
	Namespace                string `json:"namespace"`
	DeploymentName           string `json:"deploymentName"`
	ContainerName            string `json:"containerName,omitempty"`
	Image                    string `json:"image,omitempty"`
	ImageTag                 string `json:"imageTag,omitempty"`
	ReleaseName              string `json:"releaseName,omitempty"`
	ActionKind               string `json:"actionKind,omitempty"`
	WorkflowRunID            string `json:"workflowRunId,omitempty"`
}

type Filter struct {
	ApplicationID string
	ClusterID     string
	Limit         int
}

type Repository interface {
	List(context.Context, Filter) ([]Record, error)
	GetByExecutionTaskID(context.Context, string) (Record, error)
	Create(context.Context, Record) (Record, error)
	Update(context.Context, Record) (Record, error)
}
