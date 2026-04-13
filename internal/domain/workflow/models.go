package workflow

import "context"

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
	ApplicationID  string `json:"applicationId"`
	WorkflowName   string `json:"workflowName"`
	ClusterID      string `json:"clusterId,omitempty"`
	Namespace      string `json:"namespace,omitempty"`
	DeploymentName string `json:"deploymentName,omitempty"`
	TriggerBuild   bool   `json:"triggerBuild"`
	TriggerRelease bool   `json:"triggerRelease"`
}

type Repository interface {
	List(context.Context, string, int) ([]Run, error)
	Create(context.Context, Run) (Run, error)
}
