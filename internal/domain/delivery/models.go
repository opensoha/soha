package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
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
	WorkflowRunID            string         `json:"workflowRunId,omitempty"`
	WorkflowNodeID           string         `json:"workflowNodeId,omitempty"`
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
	RetentionUntil           *time.Time     `json:"retentionUntil,omitempty"`
	CreatedAt                time.Time      `json:"createdAt,omitempty"`
	UpdatedAt                time.Time      `json:"updatedAt,omitempty"`
	ModifiedAt               *time.Time     `json:"modifiedAt,omitempty"`
}

type BlueprintFileTemplate struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Content  string `json:"content"`
	Required bool   `json:"required"`
	Purpose  string `json:"purpose,omitempty"`
}

type BlueprintApplicationDraft struct {
	ID                  string         `json:"id,omitempty"`
	Name                string         `json:"name"`
	Key                 string         `json:"key"`
	Group               string         `json:"group"`
	BusinessLineID      string         `json:"businessLineId,omitempty"`
	Language            string         `json:"language"`
	Description         string         `json:"description,omitempty"`
	OwnerTeam           string         `json:"ownerTeam,omitempty"`
	RepositoryProvider  string         `json:"repositoryProvider,omitempty"`
	RepositoryProjectID string         `json:"repositoryProjectId,omitempty"`
	RepositoryPath      string         `json:"repositoryPath,omitempty"`
	DefaultBranch       string         `json:"defaultBranch,omitempty"`
	DefaultTag          string         `json:"defaultTag,omitempty"`
	BuildImage          string         `json:"buildImage,omitempty"`
	BuildContextDir     string         `json:"buildContextDir,omitempty"`
	DockerfilePath      string         `json:"dockerfilePath,omitempty"`
	Enabled             bool           `json:"enabled"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

type BlueprintEnvironmentBindingTemplate struct {
	EnvironmentID      string                             `json:"environmentId,omitempty"`
	EnvironmentKey     string                             `json:"environmentKey,omitempty"`
	BusinessLineID     string                             `json:"businessLineId,omitempty"`
	StrategyProfileID  string                             `json:"strategyProfileId,omitempty"`
	PromotionPolicyID  string                             `json:"promotionPolicyId,omitempty"`
	ArtifactPolicyID   string                             `json:"artifactPolicyId,omitempty"`
	WorkflowTemplateID string                             `json:"workflowTemplateId,omitempty"`
	BuildPolicy        domaincatalog.BuildPolicy          `json:"buildPolicy,omitempty"`
	ReleasePolicy      domaincatalog.ReleasePolicy        `json:"releasePolicy,omitempty"`
	ResourceSelector   domaincatalog.ResourceSelector     `json:"resourceSelector,omitempty"`
	Targets            []domaincatalog.ReleaseTargetInput `json:"targets,omitempty"`
}

type DeliveryBlueprint struct {
	ID                  string                                `json:"id"`
	Key                 string                                `json:"key"`
	Name                string                                `json:"name"`
	Description         string                                `json:"description,omitempty"`
	ApplicationDraft    BlueprintApplicationDraft             `json:"applicationDraft"`
	BuildSources        []domainapp.BuildSourceInput          `json:"buildSources,omitempty"`
	EnvironmentBindings []BlueprintEnvironmentBindingTemplate `json:"environmentBindings,omitempty"`
	Files               []BlueprintFileTemplate               `json:"files,omitempty"`
	ExecutionHints      map[string]any                        `json:"executionHints,omitempty"`
	PostCreateActions   []string                              `json:"postCreateActions,omitempty"`
	Enabled             bool                                  `json:"enabled"`
	CreatedAt           time.Time                             `json:"createdAt"`
	UpdatedAt           time.Time                             `json:"updatedAt"`
}

type DeliveryBlueprintInput struct {
	ID                  string                                `json:"id"`
	Key                 string                                `json:"key"`
	Name                string                                `json:"name"`
	Description         string                                `json:"description,omitempty"`
	ApplicationDraft    BlueprintApplicationDraft             `json:"applicationDraft"`
	BuildSources        []domainapp.BuildSourceInput          `json:"buildSources,omitempty"`
	EnvironmentBindings []BlueprintEnvironmentBindingTemplate `json:"environmentBindings,omitempty"`
	Files               []BlueprintFileTemplate               `json:"files,omitempty"`
	ExecutionHints      map[string]any                        `json:"executionHints,omitempty"`
	PostCreateActions   []string                              `json:"postCreateActions,omitempty"`
	Enabled             bool                                  `json:"enabled"`
}

const (
	DeliveryDraftSourceManual    = "manual"
	DeliveryDraftSourceAI        = "ai"
	DeliveryDraftSourceBlueprint = "blueprint"

	DeliveryDraftStatusDraft      = "draft"
	DeliveryDraftStatusConfirming = "confirming"
	DeliveryDraftStatusConfirmed  = "confirmed"
)

type DeliveryDraftService struct {
	ID                  string                            `json:"id,omitempty"`
	Key                 string                            `json:"key"`
	Name                string                            `json:"name"`
	Description         string                            `json:"description,omitempty"`
	ServiceKind         domainapp.ServiceKind             `json:"serviceKind"`
	OwnerTeam           string                            `json:"ownerTeam,omitempty"`
	RepositoryProvider  string                            `json:"repositoryProvider,omitempty"`
	RepositoryProjectID string                            `json:"repositoryProjectId,omitempty"`
	RepositoryPath      string                            `json:"repositoryPath,omitempty"`
	DefaultBranch       string                            `json:"defaultBranch,omitempty"`
	BuildSourceID       string                            `json:"buildSourceId,omitempty"`
	Enabled             bool                              `json:"enabled"`
	Metadata            map[string]any                    `json:"metadata,omitempty"`
	Containers          []domainapp.ServiceContainerInput `json:"containers,omitempty"`
}

type DeliveryDraft struct {
	ID                  string                                `json:"id"`
	Source              string                                `json:"source"`
	Status              string                                `json:"status"`
	ApplicationDraft    BlueprintApplicationDraft             `json:"applicationDraft"`
	Services            []DeliveryDraftService                `json:"services,omitempty"`
	BuildSources        []domainapp.BuildSourceInput          `json:"buildSources,omitempty"`
	EnvironmentBindings []BlueprintEnvironmentBindingTemplate `json:"environmentBindings,omitempty"`
	Files               []BlueprintFileTemplate               `json:"files,omitempty"`
	ExecutionHints      map[string]any                        `json:"executionHints,omitempty"`
	PostCreateActions   []string                              `json:"postCreateActions,omitempty"`
	CreatedBy           string                                `json:"createdBy,omitempty"`
	ConfirmedAt         *time.Time                            `json:"confirmedAt,omitempty"`
	CreatedAt           time.Time                             `json:"createdAt"`
	UpdatedAt           time.Time                             `json:"updatedAt"`
}

type DeliveryDraftInput struct {
	ID                  string                                `json:"id"`
	Source              string                                `json:"source"`
	ApplicationDraft    BlueprintApplicationDraft             `json:"applicationDraft"`
	Services            []DeliveryDraftService                `json:"services,omitempty"`
	BuildSources        []domainapp.BuildSourceInput          `json:"buildSources,omitempty"`
	EnvironmentBindings []BlueprintEnvironmentBindingTemplate `json:"environmentBindings,omitempty"`
	Files               []BlueprintFileTemplate               `json:"files,omitempty"`
	ExecutionHints      map[string]any                        `json:"executionHints,omitempty"`
	PostCreateActions   []string                              `json:"postCreateActions,omitempty"`
}

type RenderedDeliverySpec struct {
	ApplicationDraft    BlueprintApplicationDraft             `json:"applicationDraft"`
	Services            []DeliveryDraftService                `json:"services,omitempty"`
	BuildSources        []domainapp.BuildSourceInput          `json:"buildSources,omitempty"`
	EnvironmentBindings []BlueprintEnvironmentBindingTemplate `json:"environmentBindings,omitempty"`
	Files               []BlueprintFileTemplate               `json:"files,omitempty"`
	ExecutionHints      map[string]any                        `json:"executionHints,omitempty"`
	PostCreateActions   []string                              `json:"postCreateActions,omitempty"`
}

type BlueprintBootstrapResult struct {
	Application         domainapp.App                          `json:"application"`
	Services            []domainapp.Service                    `json:"services,omitempty"`
	EnvironmentBindings []domaincatalog.ApplicationEnvironment `json:"environmentBindings,omitempty"`
	Spec                RenderedDeliverySpec                   `json:"spec"`
}

type DeliveryDraftConfirmResult struct {
	Draft               DeliveryDraft                          `json:"draft"`
	Application         domainapp.App                          `json:"application"`
	Services            []domainapp.Service                    `json:"services,omitempty"`
	EnvironmentBindings []domaincatalog.ApplicationEnvironment `json:"environmentBindings,omitempty"`
	Spec                RenderedDeliverySpec                   `json:"spec"`
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
	OperationState           *OperationState     `json:"operationState,omitempty" gorm:"-"`
	Artifacts                []ExecutionArtifact `json:"artifacts,omitempty"`
	StartedAt                *time.Time          `json:"startedAt,omitempty"`
	LastHeartbeatAt          *time.Time          `json:"lastHeartbeatAt,omitempty"`
	LastRuntimeSeenAt        *time.Time          `json:"lastRuntimeSeenAt,omitempty"`
	FinishedAt               *time.Time          `json:"finishedAt,omitempty"`
	CreatedAt                time.Time           `json:"createdAt"`
	UpdatedAt                time.Time           `json:"updatedAt"`
}

type OperationState struct {
	Phase                  string    `json:"phase"`
	Status                 string    `json:"status"`
	Terminal               bool      `json:"terminal"`
	Cancelable             bool      `json:"cancelable"`
	Retryable              bool      `json:"retryable"`
	HeartbeatRequired      bool      `json:"heartbeatRequired"`
	TimeoutSeconds         int       `json:"timeoutSeconds"`
	HeartbeatStale         bool      `json:"heartbeatStale"`
	LastHeartbeatAt        time.Time `json:"lastHeartbeatAt,omitempty"`
	NextHeartbeatDeadline  time.Time `json:"nextHeartbeatDeadline,omitempty"`
	FailureReason          string    `json:"failureReason,omitempty"`
	FailureMessage         string    `json:"failureMessage,omitempty"`
	FinalStateRecordedAt   time.Time `json:"finalStateRecordedAt,omitempty"`
	LastRuntimeSeenAt      time.Time `json:"lastRuntimeSeenAt,omitempty"`
	ClaimedByAgentID       string    `json:"claimedByAgentId,omitempty"`
	RuntimeEndpointPresent bool      `json:"runtimeEndpointPresent"`
	RecommendedNextAction  string    `json:"recommendedNextAction,omitempty"`
}

type ExecutionTaskFilter struct {
	ApplicationID            string
	ApplicationEnvironmentID string
	ReleaseBundleID          string
	Status                   string
	ProviderKind             string
	Limit                    int
}

type ArtifactFilter struct {
	ApplicationID            string
	ApplicationEnvironmentID string
	WorkflowRunID            string
	WorkflowNodeID           string
	ReleaseBundleID          string
	ExecutionTaskID          string
	Kind                     string
	Status                   string
	Limit                    int
}

const defaultOperationTimeoutSeconds = 300

func WithOperationState(task ExecutionTask, now time.Time) ExecutionTask {
	task.OperationState = BuildOperationState(task, now)
	return task
}

func WithOperationStates(tasks []ExecutionTask, now time.Time) []ExecutionTask {
	out := make([]ExecutionTask, len(tasks))
	for index, task := range tasks {
		out[index] = WithOperationState(task, now)
	}
	return out
}

func BuildOperationState(task ExecutionTask, now time.Time) *OperationState {
	status := strings.TrimSpace(task.Status)
	timeoutSeconds := task.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultOperationTimeoutSeconds
	}
	terminal := operationStatusTerminal(status)
	heartbeatRequired := status == "dispatching" || status == "running"
	heartbeatReference := operationHeartbeatReference(task)
	nextDeadline := time.Time{}
	heartbeatStale := false
	if heartbeatRequired {
		nextDeadline = heartbeatReference.Add(time.Duration(timeoutSeconds) * time.Second)
		heartbeatStale = now.After(nextDeadline)
	}

	state := &OperationState{
		Phase:                 operationPhase(status),
		Status:                status,
		Terminal:              terminal,
		Cancelable:            status == "queued" || status == "dispatching" || status == "running",
		Retryable:             status == "failed" || status == "callback_timeout" || status == "canceled",
		HeartbeatRequired:     heartbeatRequired,
		TimeoutSeconds:        timeoutSeconds,
		HeartbeatStale:        heartbeatStale,
		RecommendedNextAction: operationRecommendedNextAction(status, heartbeatStale),
	}
	state.RuntimeEndpointPresent = strings.TrimSpace(task.RuntimeEndpoint) != ""
	if !state.RuntimeEndpointPresent {
		if value, ok := task.Result["runtimeEndpoint"]; ok {
			runtimeEndpoint := strings.TrimSpace(fmt.Sprint(value))
			state.RuntimeEndpointPresent = runtimeEndpoint != "" && runtimeEndpoint != "<nil>"
		}
	}
	if task.LastHeartbeatAt != nil && !task.LastHeartbeatAt.IsZero() {
		state.LastHeartbeatAt = task.LastHeartbeatAt.UTC()
	}
	if !nextDeadline.IsZero() {
		state.NextHeartbeatDeadline = nextDeadline.UTC()
	}
	if task.FinishedAt != nil && !task.FinishedAt.IsZero() {
		state.FinalStateRecordedAt = task.FinishedAt.UTC()
	}
	if task.LastRuntimeSeenAt != nil && !task.LastRuntimeSeenAt.IsZero() {
		state.LastRuntimeSeenAt = task.LastRuntimeSeenAt.UTC()
	}
	state.ClaimedByAgentID = strings.TrimSpace(task.ClaimedByAgentID)
	if terminal && status != "completed" {
		state.FailureReason = firstNonEmptyResultString(task.Result, "failureReason", "reason")
		if state.FailureReason == "" {
			state.FailureReason = status
		}
		state.FailureMessage = firstNonEmptyResultString(task.Result, "error", "message", "cancelReason")
	}
	return state
}

func operationHeartbeatReference(task ExecutionTask) time.Time {
	if task.LastHeartbeatAt != nil && !task.LastHeartbeatAt.IsZero() {
		return task.LastHeartbeatAt.UTC()
	}
	if task.StartedAt != nil && !task.StartedAt.IsZero() {
		return task.StartedAt.UTC()
	}
	return task.CreatedAt.UTC()
}

func operationStatusTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "canceled", "callback_timeout":
		return true
	default:
		return false
	}
}

func operationPhase(status string) string {
	switch strings.TrimSpace(status) {
	case "queued":
		return "pending"
	case "dispatching":
		return "dispatching"
	case "running":
		return "running"
	case "completed":
		return "succeeded"
	case "failed", "callback_timeout":
		return "failed"
	case "canceled":
		return "canceled"
	default:
		return "unknown"
	}
}

func operationRecommendedNextAction(status string, heartbeatStale bool) string {
	switch strings.TrimSpace(status) {
	case "queued":
		return "wait_for_runner_claim"
	case "dispatching", "running":
		if heartbeatStale {
			return "inspect_runtime_or_cancel"
		}
		return "wait_for_heartbeat"
	case "failed", "callback_timeout":
		return "inspect_failure_or_retry"
	case "canceled":
		return "retry_or_close"
	case "completed":
		return "inspect_artifacts"
	default:
		return "inspect_status"
	}
}

func firstNonEmptyResultString(result map[string]any, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(result[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
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

type ApplicationDeliveryActionKind string

const (
	ApplicationDeliveryActionBuild       ApplicationDeliveryActionKind = "build"
	ApplicationDeliveryActionDeploy      ApplicationDeliveryActionKind = "deploy"
	ApplicationDeliveryActionBuildDeploy ApplicationDeliveryActionKind = "build_deploy"
	ApplicationDeliveryActionWorkflow    ApplicationDeliveryActionKind = "workflow"
	ApplicationDeliveryActionVerify      ApplicationDeliveryActionKind = "verify"
	ApplicationDeliveryActionRollback    ApplicationDeliveryActionKind = "rollback"
)

type ApplicationDeliveryActionInput struct {
	Action                   ApplicationDeliveryActionKind `json:"action"`
	ApplicationEnvironmentID string                        `json:"applicationEnvironmentId"`
	TargetID                 string                        `json:"targetId,omitempty"`
	BuildSourceID            string                        `json:"buildSourceId,omitempty"`
	ReleaseBundleID          string                        `json:"releaseBundleId,omitempty"`
	RefType                  string                        `json:"refType,omitempty"`
	RefName                  string                        `json:"refName,omitempty"`
	ImageTag                 string                        `json:"imageTag,omitempty"`
	ReleaseName              string                        `json:"releaseName,omitempty"`
	ContainerName            string                        `json:"containerName,omitempty"`
	Variables                map[string]any                `json:"variables,omitempty"`
	BuildArgs                map[string]any                `json:"buildArgs,omitempty"`
}

type ApplicationDeliveryActionRelatedIDs struct {
	ReleaseBundleID string `json:"releaseBundleId,omitempty"`
	ExecutionTaskID string `json:"executionTaskId,omitempty"`
	WorkflowRunID   string `json:"workflowRunId,omitempty"`
}

type ApplicationDeliveryActionResult struct {
	Action                   ApplicationDeliveryActionKind       `json:"action"`
	ApplicationID            string                              `json:"applicationId"`
	ApplicationEnvironmentID string                              `json:"applicationEnvironmentId"`
	Target                   *domaincatalog.ReleaseTarget        `json:"target,omitempty"`
	Build                    *domainbuild.Record                 `json:"build,omitempty"`
	Workflow                 *domainworkflow.Run                 `json:"workflow,omitempty"`
	Release                  *domainrelease.Record               `json:"release,omitempty"`
	RelatedIDs               ApplicationDeliveryActionRelatedIDs `json:"relatedIds,omitempty"`
}

const (
	DeliveryPlanSourceManual = "manual"
	DeliveryPlanSourceAI     = "ai"

	DeliveryPlanStatusDraft      = "draft"
	DeliveryPlanStatusConfirming = "confirming"
	DeliveryPlanStatusConfirmed  = "confirmed"
)

type DeliveryPlan struct {
	ID                       string                        `json:"id"`
	Source                   string                        `json:"source"`
	Status                   string                        `json:"status"`
	ApplicationID            string                        `json:"applicationId"`
	ApplicationName          string                        `json:"applicationName,omitempty"`
	ApplicationEnvironmentID string                        `json:"applicationEnvironmentId"`
	EnvironmentKey           string                        `json:"environmentKey,omitempty"`
	Action                   ApplicationDeliveryActionKind `json:"action"`
	TargetID                 string                        `json:"targetId,omitempty"`
	TargetSummary            string                        `json:"targetSummary,omitempty"`
	BuildSourceID            string                        `json:"buildSourceId,omitempty"`
	ReleaseBundleID          string                        `json:"releaseBundleId,omitempty"`
	RefType                  string                        `json:"refType,omitempty"`
	RefName                  string                        `json:"refName,omitempty"`
	ImageTag                 string                        `json:"imageTag,omitempty"`
	ReleaseName              string                        `json:"releaseName,omitempty"`
	ContainerName            string                        `json:"containerName,omitempty"`
	Reason                   string                        `json:"reason,omitempty"`
	RiskLevel                string                        `json:"riskLevel,omitempty"`
	RequiresApproval         bool                          `json:"requiresApproval"`
	Impact                   map[string]any                `json:"impact,omitempty"`
	RollbackStrategy         string                        `json:"rollbackStrategy,omitempty"`
	Variables                map[string]any                `json:"variables,omitempty"`
	BuildArgs                map[string]any                `json:"buildArgs,omitempty"`
	CreatedBy                string                        `json:"createdBy,omitempty"`
	ConfirmedAt              *time.Time                    `json:"confirmedAt,omitempty"`
	CreatedAt                time.Time                     `json:"createdAt"`
	UpdatedAt                time.Time                     `json:"updatedAt"`
}

type DeliveryPlanInput struct {
	ID                       string                        `json:"id"`
	Source                   string                        `json:"source"`
	ApplicationID            string                        `json:"applicationId"`
	ApplicationName          string                        `json:"applicationName,omitempty"`
	ApplicationEnvironmentID string                        `json:"applicationEnvironmentId"`
	EnvironmentKey           string                        `json:"environmentKey,omitempty"`
	Action                   ApplicationDeliveryActionKind `json:"action"`
	TargetID                 string                        `json:"targetId,omitempty"`
	TargetSummary            string                        `json:"targetSummary,omitempty"`
	BuildSourceID            string                        `json:"buildSourceId,omitempty"`
	ReleaseBundleID          string                        `json:"releaseBundleId,omitempty"`
	RefType                  string                        `json:"refType,omitempty"`
	RefName                  string                        `json:"refName,omitempty"`
	ImageTag                 string                        `json:"imageTag,omitempty"`
	ReleaseName              string                        `json:"releaseName,omitempty"`
	ContainerName            string                        `json:"containerName,omitempty"`
	Reason                   string                        `json:"reason,omitempty"`
	RiskLevel                string                        `json:"riskLevel,omitempty"`
	RequiresApproval         bool                          `json:"requiresApproval"`
	Impact                   map[string]any                `json:"impact,omitempty"`
	RollbackStrategy         string                        `json:"rollbackStrategy,omitempty"`
	Variables                map[string]any                `json:"variables,omitempty"`
	BuildArgs                map[string]any                `json:"buildArgs,omitempty"`
}

type DeliveryPlanConfirmResult struct {
	Plan   DeliveryPlan                    `json:"plan"`
	Result ApplicationDeliveryActionResult `json:"result"`
}

type ApplicationBindingSummary struct {
	ApplicationEnvironmentID string                          `json:"applicationEnvironmentId"`
	EnvironmentID            string                          `json:"environmentId"`
	EnvironmentName          string                          `json:"environmentName,omitempty"`
	EnvironmentKey           string                          `json:"environmentKey,omitempty"`
	ActionKind               string                          `json:"actionKind,omitempty"`
	RequiresApproval         bool                            `json:"requiresApproval"`
	WorkflowTemplateID       string                          `json:"workflowTemplateId,omitempty"`
	WorkflowTemplateName     string                          `json:"workflowTemplateName,omitempty"`
	WorkflowTemplate         *domaincatalog.WorkflowTemplate `json:"workflowTemplate,omitempty"`
	TargetCount              int                             `json:"targetCount"`
	Targets                  []domaincatalog.ReleaseTarget   `json:"targets,omitempty"`
	BuildSourceID            string                          `json:"buildSourceId,omitempty"`
	BuildSource              *domainapp.BuildSource          `json:"buildSource,omitempty"`
	BuildPolicy              domaincatalog.BuildPolicy       `json:"buildPolicy,omitempty"`
	LatestBundle             *ReleaseBundle                  `json:"latestBundle,omitempty"`
	LatestExecutionTask      *ExecutionTask                  `json:"latestExecutionTask,omitempty"`
	LatestBuild              *domainbuild.Record             `json:"latestBuild,omitempty"`
	LatestWorkflow           *domainworkflow.Run             `json:"latestWorkflow,omitempty"`
	LatestRelease            *domainrelease.Record           `json:"latestRelease,omitempty"`
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
	ApplicationEnvironmentID string                 `json:"applicationEnvironmentId"`
	ClusterID                string                 `json:"clusterId"`
	Namespace                string                 `json:"namespace"`
	WorkloadKind             string                 `json:"workloadKind"`
	WorkloadName             string                 `json:"workloadName"`
	Labels                   map[string]string      `json:"labels,omitempty"`
	Selector                 map[string]string      `json:"selector,omitempty"`
	DesiredReplicas          int32                  `json:"desiredReplicas"`
	ReadyReplicas            int32                  `json:"readyReplicas"`
	UpdatedReplicas          int32                  `json:"updatedReplicas"`
	AvailableReplicas        int32                  `json:"availableReplicas"`
	BuildSource              *domainapp.BuildSource `json:"buildSource,omitempty"`
	LatestBundle             *ReleaseBundle         `json:"latestBundle,omitempty"`
	LatestExecutionTask      *ExecutionTask         `json:"latestExecutionTask,omitempty"`
	LatestBuild              *domainbuild.Record    `json:"latestBuild,omitempty"`
	LatestWorkflow           *domainworkflow.Run    `json:"latestWorkflow,omitempty"`
	LatestRelease            *domainrelease.Record  `json:"latestRelease,omitempty"`
}

type ApplicationRuntimeEnvironment struct {
	ApplicationEnvironmentID string                         `json:"applicationEnvironmentId"`
	EnvironmentID            string                         `json:"environmentId"`
	EnvironmentName          string                         `json:"environmentName,omitempty"`
	EnvironmentKey           string                         `json:"environmentKey,omitempty"`
	ActionKind               string                         `json:"actionKind,omitempty"`
	RequiresApproval         bool                           `json:"requiresApproval"`
	ResourceSelector         domaincatalog.ResourceSelector `json:"resourceSelector,omitempty"`
	Targets                  []domaincatalog.ReleaseTarget  `json:"targets,omitempty"`
	Workloads                []ApplicationRuntimeWorkload   `json:"workloads,omitempty"`
}

type ApplicationRuntimeDetail struct {
	Application  domainapp.App                   `json:"application"`
	Environments []ApplicationRuntimeEnvironment `json:"environments,omitempty"`
}

type ApplicationWorkloadRuntimeDetail struct {
	Application domainapp.App                        `json:"application"`
	Binding     domaincatalog.ApplicationEnvironment `json:"binding"`
	Environment *domaincatalog.Environment           `json:"environment,omitempty"`
	Workload    ApplicationRuntimeWorkload           `json:"workload"`
	Deployment  domainresource.DeploymentDetailView  `json:"deployment"`
	Pods        []domainresource.PodView             `json:"pods,omitempty"`
	Services    []domainresource.ServiceView         `json:"services,omitempty"`
	Ingresses   []domainresource.IngressView         `json:"ingresses,omitempty"`
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

type RuntimeObjectLinks struct {
	Application string `json:"application,omitempty"`
	Audit       string `json:"audit,omitempty"`
	Operations  string `json:"operations,omitempty"`
	Artifacts   string `json:"artifacts,omitempty"`
}

type RuntimeObjectPermissions struct {
	CanViewArtifacts  bool `json:"canViewArtifacts"`
	CanViewAudit      bool `json:"canViewAudit"`
	CanViewOperations bool `json:"canViewOperations"`
	CanRetry          bool `json:"canRetry,omitempty"`
	CanCancel         bool `json:"canCancel,omitempty"`
}

type RuntimeObjectDetail struct {
	Kind             string                                `json:"kind"`
	ID               string                                `json:"id"`
	Object           any                                   `json:"object"`
	Application      *domainapp.App                        `json:"application,omitempty"`
	Binding          *domaincatalog.ApplicationEnvironment `json:"binding,omitempty"`
	Environment      *domaincatalog.Environment            `json:"environment,omitempty"`
	BuildSource      *domainapp.BuildSource                `json:"buildSource,omitempty"`
	WorkflowTemplate *domaincatalog.WorkflowTemplate       `json:"workflowTemplate,omitempty"`
	Evidence         map[string]any                        `json:"evidence,omitempty"`
	Artifacts        []ExecutionArtifact                   `json:"artifacts,omitempty"`
	Links            RuntimeObjectLinks                    `json:"links"`
	Permissions      RuntimeObjectPermissions              `json:"permissions"`
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
	BuildPolicy              domaincatalog.BuildPolicy     `json:"buildPolicy,omitempty"`
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
	ListArtifacts(context.Context, ArtifactFilter) ([]ExecutionArtifact, error)
	UpsertExecutionArtifact(context.Context, ExecutionArtifact) (ExecutionArtifact, error)

	ListDeliveryBlueprints(context.Context) ([]DeliveryBlueprint, error)
	GetDeliveryBlueprint(context.Context, string) (DeliveryBlueprint, error)
	CreateDeliveryBlueprint(context.Context, DeliveryBlueprintInput) (DeliveryBlueprint, error)
	UpdateDeliveryBlueprint(context.Context, string, DeliveryBlueprintInput) (DeliveryBlueprint, error)

	CreateDeliveryDraft(context.Context, DeliveryDraftInput, string) (DeliveryDraft, error)
	GetDeliveryDraft(context.Context, string) (DeliveryDraft, error)
	UpdateDeliveryDraft(context.Context, DeliveryDraft) (DeliveryDraft, error)

	CreateDeliveryPlan(context.Context, DeliveryPlanInput, string) (DeliveryPlan, error)
	GetDeliveryPlan(context.Context, string) (DeliveryPlan, error)
	UpdateDeliveryPlan(context.Context, DeliveryPlan) (DeliveryPlan, error)
}

type TargetCandidate struct {
	ClusterID    string            `json:"clusterId"`
	Namespace    string            `json:"namespace"`
	WorkloadKind string            `json:"workloadKind"`
	WorkloadName string            `json:"workloadName"`
	Containers   []string          `json:"containers,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}
