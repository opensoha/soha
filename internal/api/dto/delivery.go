package dto

import sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"

type BuildRepositoryRefRequest struct {
	RepositoryID string `json:"repositoryId"`
	RefType      string `json:"refType"`
	RefName      string `json:"refName"`
}

type ApplicationDeliveryActionRequest struct {
	Action                   string                      `json:"action"`
	ApplicationEnvironmentID string                      `json:"applicationEnvironmentId"`
	TargetID                 string                      `json:"targetId"`
	TargetIDs                []string                    `json:"targetIds"`
	BuildSourceID            string                      `json:"buildSourceId"`
	RefType                  string                      `json:"refType"`
	RefName                  string                      `json:"refName"`
	RepositoryRefs           []BuildRepositoryRefRequest `json:"repositoryRefs"`
	ImageTag                 string                      `json:"imageTag"`
	ReleaseName              string                      `json:"releaseName"`
	ContainerName            string                      `json:"containerName"`
	ValuesContent            string                      `json:"valuesContent"`
	Variables                map[string]any              `json:"variables"`
	BuildArgs                map[string]any              `json:"buildArgs"`
}

type ExecutionCallbackRequest struct {
	CallbackToken string         `json:"callbackToken"`
	Status        string         `json:"status"`
	Payload       map[string]any `json:"payload"`
}

type ClaimExecutionTaskRequest struct {
	AgentID         string   `json:"agentId"`
	ProviderKinds   []string `json:"providerKinds"`
	RuntimeEndpoint string   `json:"runtimeEndpoint"`
}

type ExecutionTaskActionRequest struct {
	Reason string `json:"reason"`
}

type DeliveryBlueprintFileRequest struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Content  string `json:"content"`
	Required bool   `json:"required"`
	Purpose  string `json:"purpose"`
}

type DeliveryBlueprintRequest = sohaapi.DeliveryBlueprintInput

type DeliveryDraftRequest struct {
	IdempotencyKey      string                         `json:"idempotencyKey" binding:"omitempty,min=8,max=128"`
	ID                  string                         `json:"id"`
	Source              string                         `json:"source"`
	ApplicationDraft    map[string]any                 `json:"applicationDraft"`
	Services            []map[string]any               `json:"services"`
	BuildSources        []map[string]any               `json:"buildSources"`
	EnvironmentBindings []map[string]any               `json:"environmentBindings"`
	Files               []DeliveryBlueprintFileRequest `json:"files"`
	ExecutionHints      map[string]any                 `json:"executionHints"`
	PostCreateActions   []string                       `json:"postCreateActions"`
}

type DeliveryPlanRequest struct {
	ManifestRevision         *int           `json:"manifestRevision" binding:"omitempty,min=1"`
	HelmRevision             *int           `json:"helmRevision" binding:"omitempty,min=1"`
	ID                       string         `json:"id"`
	Source                   string         `json:"source"`
	ApplicationID            string         `json:"applicationId"`
	ApplicationEnvironmentID string         `json:"applicationEnvironmentId"`
	Action                   string         `json:"action"`
	TargetID                 string         `json:"targetId"`
	TargetIDs                []string       `json:"targetIds"`
	BuildSourceID            string         `json:"buildSourceId"`
	ReleaseBundleID          string         `json:"releaseBundleId"`
	RefType                  string         `json:"refType"`
	RefName                  string         `json:"refName"`
	ImageTag                 string         `json:"imageTag"`
	ReleaseName              string         `json:"releaseName"`
	ContainerName            string         `json:"containerName"`
	Reason                   string         `json:"reason"`
	Variables                map[string]any `json:"variables"`
	BuildArgs                map[string]any `json:"buildArgs"`
}

type DeliveryPlanApprovalRequest struct {
	Action  string `json:"action" binding:"required"`
	Comment string `json:"comment,omitempty"`
}
