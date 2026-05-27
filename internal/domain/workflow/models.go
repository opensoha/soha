package workflow

import (
	"context"
	"time"
)

type Step struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

type NodeRun struct {
	NodeID     string `json:"nodeId"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Summary    string `json:"summary,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

type Run struct {
	ID             string         `json:"id"`
	ApplicationID  string         `json:"applicationId"`
	WorkflowName   string         `json:"workflowName"`
	ClusterID      string         `json:"clusterId,omitempty"`
	Namespace      string         `json:"namespace,omitempty"`
	DeploymentName string         `json:"deploymentName,omitempty"`
	Status         string         `json:"status"`
	Steps          []Step         `json:"steps"`
	NodeRuns       []NodeRun      `json:"nodeRuns,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      string         `json:"createdAt"`
	UpdatedAt      string         `json:"updatedAt"`
}

type Input struct {
	ApplicationID            string         `json:"applicationId"`
	ApplicationEnvironmentID string         `json:"applicationEnvironmentId,omitempty"`
	WorkflowName             string         `json:"workflowName"`
	ClusterID                string         `json:"clusterId,omitempty"`
	Namespace                string         `json:"namespace,omitempty"`
	DeploymentName           string         `json:"deploymentName,omitempty"`
	BuildSourceID            string         `json:"buildSourceId,omitempty"`
	RefType                  string         `json:"refType,omitempty"`
	RefName                  string         `json:"refName,omitempty"`
	ImageTag                 string         `json:"imageTag,omitempty"`
	ReleaseName              string         `json:"releaseName,omitempty"`
	ContainerName            string         `json:"containerName,omitempty"`
	Variables                map[string]any `json:"variables,omitempty"`
	BuildArgs                map[string]any `json:"buildArgs,omitempty"`
	TriggerBuild             bool           `json:"triggerBuild"`
	TriggerRelease           bool           `json:"triggerRelease"`
	ValidationOnly           bool           `json:"validationOnly,omitempty"`
}

type Approval struct {
	ID            string    `json:"id"`
	WorkflowRunID string    `json:"workflowRunId"`
	NodeID        string    `json:"nodeId"`
	Action        string    `json:"action"`
	Comment       string    `json:"comment,omitempty"`
	ActorID       string    `json:"actorId"`
	ActorName     string    `json:"actorName,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Repository interface {
	List(context.Context, string, int) ([]Run, error)
	Get(context.Context, string) (Run, error)
	Create(context.Context, Run) (Run, error)
	Update(context.Context, Run) (Run, error)
	CreateApproval(context.Context, Approval) error
}
