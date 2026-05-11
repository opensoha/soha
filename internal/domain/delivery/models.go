package delivery

import (
	"context"
	"time"

	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainbuild "github.com/kubecrux/kubecrux/internal/domain/build"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	domainrelease "github.com/kubecrux/kubecrux/internal/domain/release"
	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
	domainworkflow "github.com/kubecrux/kubecrux/internal/domain/workflow"
)

type ReleaseBundle struct {
	ID                       string              `json:"id"`
	ApplicationID            string              `json:"applicationId"`
	ApplicationEnvironmentID string              `json:"applicationEnvironmentId,omitempty"`
	Version                  string              `json:"version"`
	SourceType               string              `json:"sourceType"`
	Status                   string              `json:"status"`
	ArtifactRef              string              `json:"artifactRef,omitempty"`
	ArtifactDigest           string              `json:"artifactDigest,omitempty"`
	Metadata                 map[string]any      `json:"metadata,omitempty"`
	Artifacts                []ExecutionArtifact `json:"artifacts,omitempty"`
	CreatedAt                time.Time           `json:"createdAt"`
	UpdatedAt                time.Time           `json:"updatedAt"`
}

type ExecutionArtifact struct {
	ID                       string         `json:"id"`
	ExecutionTaskID          string         `json:"executionTaskId,omitempty"`
	ReleaseBundleID          string         `json:"releaseBundleId,omitempty"`
	ApplicationID            string         `json:"applicationId,omitempty"`
	ApplicationEnvironmentID string         `json:"applicationEnvironmentId,omitempty"`
	Kind                     string         `json:"kind"`
	Name                     string         `json:"name,omitempty"`
	Ref                      string         `json:"ref,omitempty"`
	Digest                   string         `json:"digest,omitempty"`
	Path                     string         `json:"path,omitempty"`
	Status                   string         `json:"status,omitempty"`
	SizeBytes                int64          `json:"sizeBytes,omitempty"`
	Metadata                 map[string]any `json:"metadata,omitempty"`
	ModifiedAt               *time.Time     `json:"modifiedAt,omitempty"`
}

type ReleaseBundleFilter struct {
	ApplicationID            string
	ApplicationEnvironmentID string
	Limit                    int
}

type ExecutionTask struct {
	ID                       string              `json:"id"`
	ReleaseBundleID          string              `json:"releaseBundleId,omitempty"`
	ApplicationID            string              `json:"applicationId"`
	ApplicationEnvironmentID string              `json:"applicationEnvironmentId,omitempty"`
	TaskKind                 string              `json:"taskKind"`
	ProviderKind             string              `json:"providerKind"`
	TargetKind               string              `json:"targetKind"`
	Status                   string              `json:"status"`
	QueueKey                 string              `json:"queueKey,omitempty"`
	LockKey                  string              `json:"lockKey,omitempty"`
	MaxRetries               int                 `json:"maxRetries"`
	AttemptCount             int                 `json:"attemptCount"`
	TimeoutSeconds           int                 `json:"timeoutSeconds"`
	CallbackToken            string              `json:"callbackToken,omitempty"`
	ClaimedByAgentID         string              `json:"claimedByAgentId,omitempty"`
	RuntimeEndpoint          string              `json:"runtimeEndpoint,omitempty"`
	RuntimeClusterID         string              `json:"runtimeClusterId,omitempty"`
	StopTransport            string              `json:"stopTransport,omitempty"`
	Payload                  map[string]any      `json:"payload,omitempty"`
	Result                   map[string]any      `json:"result,omitempty"`
	Artifacts                []ExecutionArtifact `json:"artifacts,omitempty"`
	StartedAt                *time.Time          `json:"startedAt,omitempty"`
	LastHeartbeatAt          *time.Time          `json:"lastHeartbeatAt,omitempty"`
	LastRuntimeSeenAt        *time.Time          `json:"lastRuntimeSeenAt,omitempty"`
	FinishedAt               *time.Time          `json:"finishedAt,omitempty"`
	CreatedAt                time.Time           `json:"createdAt"`
	UpdatedAt                time.Time           `json:"updatedAt"`
}

type ExecutionTaskFilter struct {
	ApplicationID            string
	ApplicationEnvironmentID string
	ReleaseBundleID          string
	Status                   string
	ProviderKind             string
	Limit                    int
}

type ExecutionLog struct {
	ID              string         `json:"id"`
	ExecutionTaskID string         `json:"executionTaskId"`
	LogLevel        string         `json:"logLevel"`
	Message         string         `json:"message"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
}

type ExecutionCallback struct {
	ID              string         `json:"id"`
	ExecutionTaskID string         `json:"executionTaskId"`
	ProviderKind    string         `json:"providerKind"`
	Status          string         `json:"status"`
	Payload         map[string]any `json:"payload,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
}

type ExecutionCallbackInput struct {
	CallbackToken string         `json:"callbackToken"`
	Status        string         `json:"status"`
	Payload       map[string]any `json:"payload,omitempty"`
}

type ExecutionTaskActionInput struct {
	Reason string `json:"reason,omitempty"`
}

type ApprovalPolicy struct {
	ID                string         `json:"id"`
	Key               string         `json:"key"`
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	Mode              string         `json:"mode,omitempty"`
	RequiredApprovals int            `json:"requiredApprovals"`
	SLAMinutes        int            `json:"slaMinutes"`
	ApproverRoles     []string       `json:"approverRoles,omitempty"`
	ChangeWindow      map[string]any `json:"changeWindow,omitempty"`
	Enabled           bool           `json:"enabled"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

type ApprovalPolicyInput struct {
	ID                string         `json:"id"`
	Key               string         `json:"key"`
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	Mode              string         `json:"mode,omitempty"`
	RequiredApprovals int            `json:"requiredApprovals"`
	SLAMinutes        int            `json:"slaMinutes"`
	ApproverRoles     []string       `json:"approverRoles,omitempty"`
	ChangeWindow      map[string]any `json:"changeWindow,omitempty"`
	Enabled           bool           `json:"enabled"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type ApplicationBindingSummary struct {
	ApplicationEnvironmentID string                        `json:"applicationEnvironmentId"`
	EnvironmentID            string                        `json:"environmentId"`
	EnvironmentName          string                        `json:"environmentName,omitempty"`
	EnvironmentKey           string                        `json:"environmentKey,omitempty"`
	ActionKind               string                        `json:"actionKind,omitempty"`
	RequiresApproval         bool                          `json:"requiresApproval"`
	WorkflowTemplateID       string                        `json:"workflowTemplateId,omitempty"`
	WorkflowTemplateName     string                        `json:"workflowTemplateName,omitempty"`
	TargetCount              int                           `json:"targetCount"`
	Targets                  []domaincatalog.ReleaseTarget `json:"targets,omitempty"`
	BuildSourceID            string                        `json:"buildSourceId,omitempty"`
	BuildSource              *domainapp.BuildSource        `json:"buildSource,omitempty"`
	LatestBundle             *ReleaseBundle                `json:"latestBundle,omitempty"`
	LatestExecutionTask      *ExecutionTask                `json:"latestExecutionTask,omitempty"`
	LatestBuild              *domainbuild.Record           `json:"latestBuild,omitempty"`
	LatestWorkflow           *domainworkflow.Run           `json:"latestWorkflow,omitempty"`
	LatestRelease            *domainrelease.Record         `json:"latestRelease,omitempty"`
}

type ApplicationDetail struct {
	Application         domainapp.App               `json:"application"`
	Bindings            []ApplicationBindingSummary `json:"bindings,omitempty"`
	LatestBundle        *ReleaseBundle              `json:"latestBundle,omitempty"`
	LatestExecutionTask *ExecutionTask              `json:"latestExecutionTask,omitempty"`
	LatestBuild         *domainbuild.Record         `json:"latestBuild,omitempty"`
	LatestWorkflow      *domainworkflow.Run         `json:"latestWorkflow,omitempty"`
	LatestRelease       *domainrelease.Record       `json:"latestRelease,omitempty"`
}

type ApplicationRuntimeWorkload struct {
	ApplicationEnvironmentID string                  `json:"applicationEnvironmentId"`
	ClusterID                string                  `json:"clusterId"`
	Namespace                string                  `json:"namespace"`
	WorkloadKind             string                  `json:"workloadKind"`
	WorkloadName             string                  `json:"workloadName"`
	Labels                   map[string]string       `json:"labels,omitempty"`
	Selector                 map[string]string       `json:"selector,omitempty"`
	DesiredReplicas          int32                   `json:"desiredReplicas"`
	ReadyReplicas            int32                   `json:"readyReplicas"`
	UpdatedReplicas          int32                   `json:"updatedReplicas"`
	AvailableReplicas        int32                   `json:"availableReplicas"`
	BuildSource              *domainapp.BuildSource  `json:"buildSource,omitempty"`
	LatestBundle             *ReleaseBundle          `json:"latestBundle,omitempty"`
	LatestExecutionTask      *ExecutionTask          `json:"latestExecutionTask,omitempty"`
	LatestBuild              *domainbuild.Record     `json:"latestBuild,omitempty"`
	LatestWorkflow           *domainworkflow.Run     `json:"latestWorkflow,omitempty"`
	LatestRelease            *domainrelease.Record   `json:"latestRelease,omitempty"`
}

type ApplicationRuntimeEnvironment struct {
	ApplicationEnvironmentID string                        `json:"applicationEnvironmentId"`
	EnvironmentID            string                        `json:"environmentId"`
	EnvironmentName          string                        `json:"environmentName,omitempty"`
	EnvironmentKey           string                        `json:"environmentKey,omitempty"`
	ActionKind               string                        `json:"actionKind,omitempty"`
	RequiresApproval         bool                          `json:"requiresApproval"`
	ResourceSelector         domaincatalog.ResourceSelector `json:"resourceSelector,omitempty"`
	Targets                  []domaincatalog.ReleaseTarget `json:"targets,omitempty"`
	Workloads                []ApplicationRuntimeWorkload  `json:"workloads,omitempty"`
}

type ApplicationRuntimeDetail struct {
	Application  domainapp.App                 `json:"application"`
	Environments []ApplicationRuntimeEnvironment `json:"environments,omitempty"`
}

type ApplicationWorkloadRuntimeDetail struct {
	Application         domainapp.App                  `json:"application"`
	Binding             domaincatalog.ApplicationEnvironment `json:"binding"`
	Environment         *domaincatalog.Environment     `json:"environment,omitempty"`
	Workload            ApplicationRuntimeWorkload     `json:"workload"`
	Deployment          domainresource.DeploymentDetailView `json:"deployment"`
	Pods                []domainresource.PodView       `json:"pods,omitempty"`
	Services            []domainresource.ServiceView   `json:"services,omitempty"`
	Ingresses           []domainresource.IngressView   `json:"ingresses,omitempty"`
}

type ApplicationEnvironmentDetail struct {
	Binding             domaincatalog.ApplicationEnvironment `json:"binding"`
	Application         domainapp.App                        `json:"application"`
	Environment         *domaincatalog.Environment           `json:"environment,omitempty"`
	ActionKind          string                               `json:"actionKind,omitempty"`
	RequiresApproval    bool                                 `json:"requiresApproval"`
	BuildSource         *domainapp.BuildSource               `json:"buildSource,omitempty"`
	LatestBundle        *ReleaseBundle                       `json:"latestBundle,omitempty"`
	LatestExecutionTask *ExecutionTask                       `json:"latestExecutionTask,omitempty"`
	LatestBuild         *domainbuild.Record                  `json:"latestBuild,omitempty"`
	LatestWorkflow      *domainworkflow.Run                  `json:"latestWorkflow,omitempty"`
	LatestRelease       *domainrelease.Record                `json:"latestRelease,omitempty"`
}

type ReleaseBoardEntry struct {
	ApplicationEnvironmentID string                        `json:"applicationEnvironmentId"`
	ApplicationID            string                        `json:"applicationId"`
	ApplicationName          string                        `json:"applicationName"`
	BusinessLineID           string                        `json:"businessLineId,omitempty"`
	EnvironmentID            string                        `json:"environmentId"`
	EnvironmentName          string                        `json:"environmentName,omitempty"`
	EnvironmentKey           string                        `json:"environmentKey,omitempty"`
	ActionKind               string                        `json:"actionKind,omitempty"`
	RequiresApproval         bool                          `json:"requiresApproval"`
	WorkflowTemplateID       string                        `json:"workflowTemplateId,omitempty"`
	WorkflowTemplateName     string                        `json:"workflowTemplateName,omitempty"`
	BuildSourceID            string                        `json:"buildSourceId,omitempty"`
	BuildSource              *domainapp.BuildSource        `json:"buildSource,omitempty"`
	LatestBundle             *ReleaseBundle                `json:"latestBundle,omitempty"`
	LatestExecutionTask      *ExecutionTask                `json:"latestExecutionTask,omitempty"`
	Targets                  []domaincatalog.ReleaseTarget `json:"targets,omitempty"`
	LatestBuild              *domainbuild.Record           `json:"latestBuild,omitempty"`
	LatestWorkflow           *domainworkflow.Run           `json:"latestWorkflow,omitempty"`
	LatestRelease            *domainrelease.Record         `json:"latestRelease,omitempty"`
}

type Repository interface {
	ListReleaseBundles(context.Context, ReleaseBundleFilter) ([]ReleaseBundle, error)
	GetReleaseBundle(context.Context, string) (ReleaseBundle, error)
	CreateReleaseBundle(context.Context, ReleaseBundle) (ReleaseBundle, error)
	UpdateReleaseBundle(context.Context, ReleaseBundle) (ReleaseBundle, error)

	ListExecutionTasks(context.Context, ExecutionTaskFilter) ([]ExecutionTask, error)
	GetExecutionTask(context.Context, string) (ExecutionTask, error)
	GetExecutionTaskByCallbackToken(context.Context, string) (ExecutionTask, error)
	ClaimExecutionTask(context.Context, []string, string, string) (ExecutionTask, error)
	CreateExecutionTask(context.Context, ExecutionTask) (ExecutionTask, error)
	UpdateExecutionTask(context.Context, ExecutionTask) (ExecutionTask, error)
	ListExecutionLogs(context.Context, string, int) ([]ExecutionLog, error)
	CreateExecutionLog(context.Context, ExecutionLog) error
	CreateExecutionCallback(context.Context, ExecutionCallback) error
	ListExecutionArtifacts(context.Context, string) ([]ExecutionArtifact, error)
	ListExecutionArtifactsByBundle(context.Context, string) ([]ExecutionArtifact, error)
	UpsertExecutionArtifact(context.Context, ExecutionArtifact) (ExecutionArtifact, error)

	ListApprovalPolicies(context.Context) ([]ApprovalPolicy, error)
	GetApprovalPolicy(context.Context, string) (ApprovalPolicy, error)
	CreateApprovalPolicy(context.Context, ApprovalPolicyInput) (ApprovalPolicy, error)
	UpdateApprovalPolicy(context.Context, string, ApprovalPolicyInput) (ApprovalPolicy, error)
	DeleteApprovalPolicy(context.Context, string) error
}

type TargetCandidate struct {
	ClusterID    string            `json:"clusterId"`
	Namespace    string            `json:"namespace"`
	WorkloadKind string            `json:"workloadKind"`
	WorkloadName string            `json:"workloadName"`
	Containers   []string          `json:"containers,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}
