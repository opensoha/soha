package catalog

import (
	"context"
	"time"
)

type BusinessLine struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Owners      []string  `json:"owners,omitempty"`
	SortOrder   int       `json:"sortOrder"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type BusinessLineInput struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Owners      []string `json:"owners,omitempty"`
	SortOrder   int      `json:"sortOrder"`
	Enabled     bool     `json:"enabled"`
}

type Environment struct {
	ID               string    `json:"id"`
	Key              string    `json:"key"`
	Name             string    `json:"name"`
	Tier             string    `json:"tier,omitempty"`
	StageLevel       int       `json:"stageLevel"`
	SortOrder        int       `json:"sortOrder"`
	IsProduction     bool      `json:"isProduction"`
	RequiresApproval bool      `json:"requiresApproval"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type EnvironmentInput struct {
	ID               string `json:"id"`
	Key              string `json:"key"`
	Name             string `json:"name"`
	Tier             string `json:"tier,omitempty"`
	StageLevel       int    `json:"stageLevel"`
	SortOrder        int    `json:"sortOrder"`
	IsProduction     bool   `json:"isProduction"`
	RequiresApproval bool   `json:"requiresApproval"`
	Enabled          bool   `json:"enabled"`
}

type ReleaseTarget struct {
	ID                       string    `json:"id"`
	ApplicationEnvironmentID string    `json:"applicationEnvironmentId"`
	ClusterID                string    `json:"clusterId"`
	Namespace                string    `json:"namespace"`
	WorkloadKind             string    `json:"workloadKind"`
	WorkloadName             string    `json:"workloadName"`
	ContainerName            string    `json:"containerName,omitempty"`
	Enabled                  bool      `json:"enabled"`
	CreatedAt                time.Time `json:"createdAt"`
	UpdatedAt                time.Time `json:"updatedAt"`
}

type ReleaseTargetInput struct {
	ID            string `json:"id"`
	ClusterID     string `json:"clusterId"`
	Namespace     string `json:"namespace"`
	WorkloadKind  string `json:"workloadKind"`
	WorkloadName  string `json:"workloadName"`
	ContainerName string `json:"containerName,omitempty"`
	Enabled       bool   `json:"enabled"`
}

type ApplicationEnvironment struct {
	ID                 string          `json:"id"`
	ApplicationID      string          `json:"applicationId"`
	BusinessLineID     string          `json:"businessLineId,omitempty"`
	EnvironmentID      string          `json:"environmentId"`
	EnvironmentKey     string          `json:"environmentKey,omitempty"`
	WorkflowTemplateID string          `json:"workflowTemplateId,omitempty"`
	WorkflowTemplate   *WorkflowTemplate `json:"workflowTemplate,omitempty"`
	BuildPolicy        map[string]any  `json:"buildPolicy,omitempty"`
	ReleasePolicy      map[string]any  `json:"releasePolicy,omitempty"`
	Targets            []ReleaseTarget `json:"targets,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
}

type ApplicationEnvironmentInput struct {
	ID                 string               `json:"id"`
	ApplicationID      string               `json:"applicationId"`
	EnvironmentID      string               `json:"environmentId"`
	WorkflowTemplateID string               `json:"workflowTemplateId,omitempty"`
	BuildPolicy        map[string]any       `json:"buildPolicy,omitempty"`
	ReleasePolicy      map[string]any       `json:"releasePolicy,omitempty"`
	Targets            []ReleaseTargetInput `json:"targets,omitempty"`
}

type WorkflowTemplate struct {
	ID          string         `json:"id"`
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	Definition  map[string]any `json:"definition,omitempty"`
	Enabled     bool           `json:"enabled"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type WorkflowTemplateInput struct {
	ID          string         `json:"id"`
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	Definition  map[string]any `json:"definition,omitempty"`
	Enabled     bool           `json:"enabled"`
}

type Repository interface {
	ListBusinessLines(context.Context) ([]BusinessLine, error)
	GetBusinessLine(context.Context, string) (BusinessLine, error)
	CreateBusinessLine(context.Context, BusinessLineInput) (BusinessLine, error)
	UpdateBusinessLine(context.Context, string, BusinessLineInput) (BusinessLine, error)
	DeleteBusinessLine(context.Context, string) error

	ListEnvironments(context.Context) ([]Environment, error)
	GetEnvironment(context.Context, string) (Environment, error)
	CreateEnvironment(context.Context, EnvironmentInput) (Environment, error)
	UpdateEnvironment(context.Context, string, EnvironmentInput) (Environment, error)
	DeleteEnvironment(context.Context, string) error

	ListApplicationEnvironments(context.Context) ([]ApplicationEnvironment, error)
	GetApplicationEnvironment(context.Context, string) (ApplicationEnvironment, error)
	CreateApplicationEnvironment(context.Context, ApplicationEnvironmentInput) (ApplicationEnvironment, error)
	UpdateApplicationEnvironment(context.Context, string, ApplicationEnvironmentInput) (ApplicationEnvironment, error)
	DeleteApplicationEnvironment(context.Context, string) error

	ListWorkflowTemplates(context.Context) ([]WorkflowTemplate, error)
	GetWorkflowTemplate(context.Context, string) (WorkflowTemplate, error)
	CreateWorkflowTemplate(context.Context, WorkflowTemplateInput) (WorkflowTemplate, error)
	UpdateWorkflowTemplate(context.Context, string, WorkflowTemplateInput) (WorkflowTemplate, error)
	DeleteWorkflowTemplate(context.Context, string) error
}
