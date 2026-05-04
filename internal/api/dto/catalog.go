package dto

type BusinessLineRequest struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Owners      []string `json:"owners"`
	SortOrder   int      `json:"sortOrder"`
	Enabled     bool     `json:"enabled"`
}

type DeliveryEnvironmentRequest struct {
	ID               string `json:"id"`
	Key              string `json:"key"`
	Name             string `json:"name"`
	Tier             string `json:"tier"`
	StageLevel       int    `json:"stageLevel"`
	SortOrder        int    `json:"sortOrder"`
	IsProduction     bool   `json:"isProduction"`
	RequiresApproval bool   `json:"requiresApproval"`
	Enabled          bool   `json:"enabled"`
}

type ReleaseTargetRequest struct {
	ID            string `json:"id"`
	ClusterID     string `json:"clusterId"`
	Namespace     string `json:"namespace"`
	WorkloadKind  string `json:"workloadKind"`
	WorkloadName  string `json:"workloadName"`
	ContainerName string `json:"containerName"`
	Enabled       bool   `json:"enabled"`
}

type ApplicationEnvironmentRequest struct {
	ID                 string                 `json:"id"`
	ApplicationID      string                 `json:"applicationId"`
	EnvironmentID      string                 `json:"environmentId"`
	WorkflowTemplateID string                 `json:"workflowTemplateId"`
	BuildPolicy        BuildPolicyRequest     `json:"buildPolicy"`
	ReleasePolicy      ReleasePolicyRequest   `json:"releasePolicy"`
	Targets            []ReleaseTargetRequest `json:"targets"`
}

type BuildPolicyRequest struct {
	SourceID         string         `json:"sourceId"`
	RefType          string         `json:"refType"`
	RefValue         string         `json:"refValue"`
	ImageTagMode     string         `json:"imageTagMode"`
	ImageTagTemplate string         `json:"imageTagTemplate"`
	Variables        map[string]any `json:"variables"`
	BuildArgs        map[string]any `json:"buildArgs"`
}

type ReleasePolicyRequest struct {
	ActionKind            string   `json:"actionKind"`
	RequiresApproval      bool     `json:"requiresApproval"`
	ApproverRoles         []string `json:"approverRoles"`
	AutoRollback          bool     `json:"autoRollback"`
	RolloutTimeoutSeconds int      `json:"rolloutTimeoutSeconds"`
	VerificationMode      string   `json:"verificationMode"`
}

type BuildTemplateRequest struct {
	ID                 string         `json:"id"`
	Key                string         `json:"key"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	BuilderKind        string         `json:"builderKind"`
	DockerfileTemplate string         `json:"dockerfileTemplate"`
	BuildCommands      []string       `json:"buildCommands"`
	VariableSchema     map[string]any `json:"variableSchema"`
	DefaultVariables   map[string]any `json:"defaultVariables"`
	Enabled            bool           `json:"enabled"`
}

type WorkflowTemplateRequest struct {
	ID          string         `json:"id"`
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Category    string         `json:"category"`
	Definition  map[string]any `json:"definition"`
	Enabled     bool           `json:"enabled"`
}
