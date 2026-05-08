package catalog

import (
	"context"
	"time"
)

type BuildPolicy struct {
	SourceID         string         `json:"sourceId,omitempty"`
	RefType          string         `json:"refType,omitempty"`
	RefValue         string         `json:"refValue,omitempty"`
	ImageTagMode     string         `json:"imageTagMode,omitempty"`
	ImageTagTemplate string         `json:"imageTagTemplate,omitempty"`
	Variables        map[string]any `json:"variables,omitempty"`
	BuildArgs        map[string]any `json:"buildArgs,omitempty"`
}

type ReleasePolicy struct {
	ActionKind            string   `json:"actionKind,omitempty"`
	RequiresApproval      bool     `json:"requiresApproval"`
	ApproverRoles         []string `json:"approverRoles,omitempty"`
	AutoRollback          bool     `json:"autoRollback"`
	RolloutTimeoutSeconds int      `json:"rolloutTimeoutSeconds,omitempty"`
	VerificationMode      string   `json:"verificationMode,omitempty"`
}

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
	ID                       string         `json:"id"`
	ApplicationEnvironmentID string         `json:"applicationEnvironmentId"`
	ClusterID                string         `json:"clusterId"`
	Namespace                string         `json:"namespace"`
	TargetKind               string         `json:"targetKind,omitempty"`
	ExecutorKind             string         `json:"executorKind,omitempty"`
	GroupKey                 string         `json:"groupKey,omitempty"`
	WaveKey                  string         `json:"waveKey,omitempty"`
	RegionKey                string         `json:"regionKey,omitempty"`
	ConfigRef                string         `json:"configRef,omitempty"`
	WorkloadKind             string         `json:"workloadKind"`
	WorkloadName             string         `json:"workloadName"`
	ContainerName            string         `json:"containerName,omitempty"`
	Metadata                 map[string]any `json:"metadata,omitempty"`
	Enabled                  bool           `json:"enabled"`
	CreatedAt                time.Time      `json:"createdAt"`
	UpdatedAt                time.Time      `json:"updatedAt"`
}

type ReleaseTargetInput struct {
	ID            string         `json:"id"`
	ClusterID     string         `json:"clusterId"`
	Namespace     string         `json:"namespace"`
	TargetKind    string         `json:"targetKind,omitempty"`
	ExecutorKind  string         `json:"executorKind,omitempty"`
	GroupKey      string         `json:"groupKey,omitempty"`
	WaveKey       string         `json:"waveKey,omitempty"`
	RegionKey     string         `json:"regionKey,omitempty"`
	ConfigRef     string         `json:"configRef,omitempty"`
	WorkloadKind  string         `json:"workloadKind"`
	WorkloadName  string         `json:"workloadName"`
	ContainerName string         `json:"containerName,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Enabled       bool           `json:"enabled"`
}

type ApplicationEnvironment struct {
	ID                 string            `json:"id"`
	ApplicationID      string            `json:"applicationId"`
	BusinessLineID     string            `json:"businessLineId,omitempty"`
	EnvironmentID      string            `json:"environmentId"`
	EnvironmentKey     string            `json:"environmentKey,omitempty"`
	StrategyProfileID  string            `json:"strategyProfileId,omitempty"`
	PromotionPolicyID  string            `json:"promotionPolicyId,omitempty"`
	ApprovalPolicyID   string            `json:"approvalPolicyId,omitempty"`
	ArtifactPolicyID   string            `json:"artifactPolicyId,omitempty"`
	WorkflowTemplateID string            `json:"workflowTemplateId,omitempty"`
	WorkflowTemplate   *WorkflowTemplate `json:"workflowTemplate,omitempty"`
	BuildPolicy        BuildPolicy       `json:"buildPolicy,omitempty"`
	ReleasePolicy      ReleasePolicy     `json:"releasePolicy,omitempty"`
	Targets            []ReleaseTarget   `json:"targets,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
}

type ApplicationEnvironmentInput struct {
	ID                 string               `json:"id"`
	ApplicationID      string               `json:"applicationId"`
	EnvironmentID      string               `json:"environmentId"`
	StrategyProfileID  string               `json:"strategyProfileId,omitempty"`
	PromotionPolicyID  string               `json:"promotionPolicyId,omitempty"`
	ApprovalPolicyID   string               `json:"approvalPolicyId,omitempty"`
	ArtifactPolicyID   string               `json:"artifactPolicyId,omitempty"`
	WorkflowTemplateID string               `json:"workflowTemplateId,omitempty"`
	BuildPolicy        BuildPolicy          `json:"buildPolicy,omitempty"`
	ReleasePolicy      ReleasePolicy        `json:"releasePolicy,omitempty"`
	Targets            []ReleaseTargetInput `json:"targets,omitempty"`
}

type BuildTemplate struct {
	ID                 string         `json:"id"`
	Key                string         `json:"key"`
	Name               string         `json:"name"`
	Description        string         `json:"description,omitempty"`
	BuilderKind        string         `json:"builderKind,omitempty"`
	DockerfileTemplate string         `json:"dockerfileTemplate,omitempty"`
	BuildCommands      []string       `json:"buildCommands,omitempty"`
	VariableSchema     map[string]any `json:"variableSchema,omitempty"`
	DefaultVariables   map[string]any `json:"defaultVariables,omitempty"`
	Enabled            bool           `json:"enabled"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

type BuildTemplateInput struct {
	ID                 string         `json:"id"`
	Key                string         `json:"key"`
	Name               string         `json:"name"`
	Description        string         `json:"description,omitempty"`
	BuilderKind        string         `json:"builderKind,omitempty"`
	DockerfileTemplate string         `json:"dockerfileTemplate,omitempty"`
	BuildCommands      []string       `json:"buildCommands,omitempty"`
	VariableSchema     map[string]any `json:"variableSchema,omitempty"`
	DefaultVariables   map[string]any `json:"defaultVariables,omitempty"`
	Enabled            bool           `json:"enabled"`
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

	ListBuildTemplates(context.Context) ([]BuildTemplate, error)
	GetBuildTemplate(context.Context, string) (BuildTemplate, error)
	CreateBuildTemplate(context.Context, BuildTemplateInput) (BuildTemplate, error)
	UpdateBuildTemplate(context.Context, string, BuildTemplateInput) (BuildTemplate, error)
	DeleteBuildTemplate(context.Context, string) error

	ListWorkflowTemplates(context.Context) ([]WorkflowTemplate, error)
	GetWorkflowTemplate(context.Context, string) (WorkflowTemplate, error)
	CreateWorkflowTemplate(context.Context, WorkflowTemplateInput) (WorkflowTemplate, error)
	UpdateWorkflowTemplate(context.Context, string, WorkflowTemplateInput) (WorkflowTemplate, error)
	DeleteWorkflowTemplate(context.Context, string) error
}
