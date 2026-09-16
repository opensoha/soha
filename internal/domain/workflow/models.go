package workflow

import (
	"context"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
)

type Step struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

type NodeRun struct {
	DockerOperationID    string                                 `json:"dockerOperationId,omitempty"`
	ControlCall          *domainaigateway.ToolInvocationRequest `json:"controlCall,omitempty"`
	PreparedCall         *domainaigateway.ToolInvocationRequest `json:"preparedCall,omitempty"`
	Invocation           *domainaigateway.ToolInvocationResult  `json:"invocation,omitempty"`
	DispatchAttempted    bool                                   `json:"dispatchAttempted,omitempty"`
	TargetID             string                                 `json:"targetId,omitempty"`
	Stage                string                                 `json:"stage,omitempty"`
	BuildRecordID        string                                 `json:"buildRecordId,omitempty"`
	ReleaseBundleID      string                                 `json:"releaseBundleId,omitempty"`
	DeliveryPlanID       string                                 `json:"deliveryPlanId,omitempty"`
	ExecutionTaskID      string                                 `json:"executionTaskId,omitempty"`
	ManifestDeploymentID string                                 `json:"manifestDeploymentId,omitempty"`
	NodeID               string                                 `json:"nodeId"`
	Name                 string                                 `json:"name"`
	Type                 string                                 `json:"type"`
	Status               string                                 `json:"status"`
	Summary              string                                 `json:"summary,omitempty"`
	StartedAt            string                                 `json:"startedAt,omitempty"`
	FinishedAt           string                                 `json:"finishedAt,omitempty"`
}

type Run struct {
	GatewayAuthorization string         `json:"-"`
	Version              int64          `json:"-"`
	LeaseOwner           string         `json:"-"`
	LeaseUntil           *time.Time     `json:"-"`
	FencingToken         int64          `json:"-"`
	StopReason           string         `json:"-"`
	StopSummary          string         `json:"-"`
	Scope                string         `json:"scope,omitempty"`
	DeliveryBatchID      string         `json:"deliveryBatchId,omitempty"`
	ID                   string         `json:"id"`
	ApplicationID        string         `json:"applicationId"`
	WorkflowName         string         `json:"workflowName"`
	ClusterID            string         `json:"clusterId,omitempty"`
	Namespace            string         `json:"namespace,omitempty"`
	DeploymentName       string         `json:"deploymentName,omitempty"`
	Status               string         `json:"status"`
	Steps                []Step         `json:"steps"`
	NodeRuns             []NodeRun      `json:"nodeRuns,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	CreatedAt            string         `json:"createdAt"`
	UpdatedAt            string         `json:"updatedAt"`
}

type Input struct {
	ApplicationID            string                      `json:"applicationId"`
	ApplicationEnvironmentID string                      `json:"applicationEnvironmentId,omitempty"`
	WorkflowName             string                      `json:"workflowName"`
	ClusterID                string                      `json:"clusterId,omitempty"`
	Namespace                string                      `json:"namespace,omitempty"`
	DeploymentName           string                      `json:"deploymentName,omitempty"`
	BuildSourceID            string                      `json:"buildSourceId,omitempty"`
	RefType                  string                      `json:"refType,omitempty"`
	RefName                  string                      `json:"refName,omitempty"`
	RepositoryRefs           []domainbuild.RepositoryRef `json:"repositoryRefs,omitempty"`
	ImageTag                 string                      `json:"imageTag,omitempty"`
	ReleaseName              string                      `json:"releaseName,omitempty"`
	ContainerName            string                      `json:"containerName,omitempty"`
	Variables                map[string]any              `json:"variables,omitempty"`
	BuildArgs                map[string]any              `json:"buildArgs,omitempty"`
	TriggerBuild             bool                        `json:"triggerBuild"`
	TriggerRelease           bool                        `json:"triggerRelease"`
	ValidationOnly           bool                        `json:"validationOnly,omitempty"`
	RollbackOnly             bool                        `json:"rollbackOnly,omitempty"`
}

type Approval struct {
	ID            string         `json:"id"`
	WorkflowRunID string         `json:"workflowRunId"`
	NodeID        string         `json:"nodeId"`
	Action        string         `json:"action"`
	Comment       string         `json:"comment,omitempty"`
	ActorID       string         `json:"actorId"`
	ActorName     string         `json:"actorName,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type Repository interface {
	List(context.Context, string, string, int) ([]Run, error)
	Get(context.Context, string) (Run, error)
	Create(context.Context, Run) (Run, error)
	Update(context.Context, Run) (Run, error)
	CreateApproval(context.Context, Approval) error
}
