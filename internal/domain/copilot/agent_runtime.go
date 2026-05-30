package copilot

import "time"

const (
	AgentRunStatusQueued          = "queued"
	AgentRunStatusRunning         = "running"
	AgentRunStatusCompleted       = "completed"
	AgentRunStatusFailed          = "failed"
	AgentRunStatusCanceled        = "canceled"
	AgentRunStatusCallbackTimeout = "callback_timeout"
)

type AgentProvider struct {
	ID               string                      `json:"id"`
	Kind             string                      `json:"kind"`
	Name             string                      `json:"name"`
	Description      string                      `json:"description,omitempty"`
	Enabled          bool                        `json:"enabled"`
	Default          bool                        `json:"default,omitempty"`
	Capabilities     []string                    `json:"capabilities,omitempty"`
	SupportedModes   []string                    `json:"supportedModes,omitempty"`
	SupportsAsync    bool                        `json:"supportsAsync"`
	SupportsSkills   bool                        `json:"supportsSkills"`
	SupportsToolsets bool                        `json:"supportsToolsets"`
	Config           map[string]any              `json:"config,omitempty"`
	RuntimeStatus    *AgentProviderRuntimeStatus `json:"runtimeStatus,omitempty"`
}

type AgentProviderRuntimeStatus struct {
	State           string     `json:"state"`
	Reason          string     `json:"reason,omitempty"`
	QueuedRuns      int        `json:"queuedRuns"`
	RunningRuns     int        `json:"runningRuns"`
	RecentFailures  int        `json:"recentFailures"`
	LastRunID       string     `json:"lastRunId,omitempty"`
	LastRunStatus   string     `json:"lastRunStatus,omitempty"`
	LastAgentID     string     `json:"lastAgentId,omitempty"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastCompletedAt *time.Time `json:"lastCompletedAt,omitempty"`
	ObservedAt      time.Time  `json:"observedAt"`
}

type AgentToolBinding struct {
	ID            string         `json:"id"`
	CapabilityID  string         `json:"capabilityId"`
	ProviderID    string         `json:"providerId,omitempty"`
	ProviderKind  string         `json:"providerKind,omitempty"`
	ToolKind      string         `json:"toolKind"`
	AdapterID     string         `json:"adapterId,omitempty"`
	ToolName      string         `json:"toolName,omitempty"`
	PermissionKey string         `json:"permissionKey,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
}

type AgentSkillBinding struct {
	ID               string         `json:"id"`
	SkillID          string         `json:"skillId"`
	ProviderID       string         `json:"providerId,omitempty"`
	ProviderKind     string         `json:"providerKind,omitempty"`
	ProviderSkillRef string         `json:"providerSkillRef,omitempty"`
	CapabilityRefs   []string       `json:"capabilityRefs,omitempty"`
	PromptTemplateID string         `json:"promptTemplateId,omitempty"`
	Config           map[string]any `json:"config,omitempty"`
}

type AgentCapability struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Category       string              `json:"category,omitempty"`
	Description    string              `json:"description,omitempty"`
	AnalysisKinds  []string            `json:"analysisKinds,omitempty"`
	RequiredScopes []string            `json:"requiredScopes,omitempty"`
	ToolRefs       []string            `json:"toolRefs,omitempty"`
	ToolBindings   []AgentToolBinding  `json:"toolBindings,omitempty"`
	SkillBindings  []AgentSkillBinding `json:"skillBindings,omitempty"`
}

type AgentRun struct {
	ID                string              `json:"id"`
	ProviderID        string              `json:"providerId"`
	ProviderKind      string              `json:"providerKind"`
	CapabilityID      string              `json:"capabilityId"`
	SkillIDs          []string            `json:"skillIds,omitempty"`
	SessionID         string              `json:"sessionId,omitempty"`
	RootCauseRunID    string              `json:"rootCauseRunId,omitempty"`
	CreatedBy         string              `json:"createdBy"`
	Status            string              `json:"status"`
	Scope             SessionScope        `json:"scope,omitempty"`
	Toolset           SessionToolset      `json:"toolset,omitempty"`
	ToolBindings      []AgentToolBinding  `json:"toolBindings,omitempty"`
	SkillBindings     []AgentSkillBinding `json:"skillBindings,omitempty"`
	Input             map[string]any      `json:"input,omitempty"`
	Output            map[string]any      `json:"output,omitempty"`
	ToolExecutions    []ToolExecution     `json:"toolExecutions,omitempty"`
	AnalysisArtifacts []AnalysisArtifact  `json:"analysisArtifacts,omitempty"`
	CallbackToken     string              `json:"callbackToken,omitempty"`
	ClaimedByAgentID  string              `json:"claimedByAgentId,omitempty"`
	ExternalRunID     string              `json:"externalRunId,omitempty"`
	ErrorMessage      string              `json:"errorMessage,omitempty"`
	TimeoutSeconds    int                 `json:"timeoutSeconds"`
	QueuedAt          time.Time           `json:"queuedAt"`
	StartedAt         *time.Time          `json:"startedAt,omitempty"`
	LastHeartbeatAt   *time.Time          `json:"lastHeartbeatAt,omitempty"`
	CompletedAt       *time.Time          `json:"completedAt,omitempty"`
	CreatedAt         time.Time           `json:"createdAt"`
	UpdatedAt         time.Time           `json:"updatedAt"`
}

type AgentRunInput struct {
	ProviderID     string              `json:"providerId,omitempty"`
	CapabilityID   string              `json:"capabilityId"`
	SkillIDs       []string            `json:"skillIds,omitempty"`
	SessionID      string              `json:"sessionId,omitempty"`
	RootCauseRunID string              `json:"rootCauseRunId,omitempty"`
	CreatedBy      string              `json:"createdBy,omitempty"`
	Scope          SessionScope        `json:"scope,omitempty"`
	Toolset        SessionToolset      `json:"toolset,omitempty"`
	ToolBindings   []AgentToolBinding  `json:"toolBindings,omitempty"`
	SkillBindings  []AgentSkillBinding `json:"skillBindings,omitempty"`
	Input          map[string]any      `json:"input,omitempty"`
	TimeoutSeconds int                 `json:"timeoutSeconds,omitempty"`
}

type GatewayAnalysisArtifactInput struct {
	CapabilityID       string                `json:"capabilityId"`
	Title              string                `json:"title,omitempty"`
	Summary            string                `json:"summary"`
	SkillIDs           []string              `json:"skillIds,omitempty"`
	Scope              SessionScope          `json:"scope,omitempty"`
	Toolset            SessionToolset        `json:"toolset,omitempty"`
	Input              map[string]any        `json:"input,omitempty"`
	Output             map[string]any        `json:"output,omitempty"`
	Evidence           []RootCauseEvidence   `json:"evidence,omitempty"`
	Hypotheses         []RootCauseHypothesis `json:"hypotheses,omitempty"`
	Recommendations    []string              `json:"recommendations,omitempty"`
	ToolExecutions     []ToolExecution       `json:"toolExecutions,omitempty"`
	Graph              *AnalysisGraph        `json:"graph,omitempty"`
	DataSourceSnapshot map[string]any        `json:"dataSourceSnapshot,omitempty"`
}

type GatewayAnalysisAgentRunInput struct {
	GatewayAnalysisArtifactInput
	AgentProviderID string `json:"agentProviderId,omitempty"`
	TimeoutSeconds  int    `json:"timeoutSeconds,omitempty"`
}

type AgentRunFilter struct {
	CreatedBy      string
	Status         string
	ProviderID     string
	CapabilityID   string
	TriggerType    string
	DedupKey       string
	DedupKeyPrefix string
	Limit          int
}

type AgentRunClaimInput struct {
	AgentID     string   `json:"agentId"`
	ProviderIDs []string `json:"providerIds,omitempty"`
	Kinds       []string `json:"kinds,omitempty"`
}

type AgentRunCallbackInput struct {
	RunID             string             `json:"runId"`
	CallbackToken     string             `json:"callbackToken"`
	AgentID           string             `json:"agentId,omitempty"`
	Status            string             `json:"status"`
	Payload           map[string]any     `json:"payload,omitempty"`
	ToolExecutions    []ToolExecution    `json:"toolExecutions,omitempty"`
	AnalysisArtifacts []AnalysisArtifact `json:"analysisArtifacts,omitempty"`
	ExternalRunID     string             `json:"externalRunId,omitempty"`
	ErrorMessage      string             `json:"errorMessage,omitempty"`
}

type AgentToolCallInput struct {
	RunID         string         `json:"runId"`
	CallbackToken string         `json:"callbackToken"`
	AgentID       string         `json:"agentId,omitempty"`
	ToolBindingID string         `json:"toolBindingId,omitempty"`
	AdapterID     string         `json:"adapterId,omitempty"`
	ToolName      string         `json:"toolName,omitempty"`
	Input         map[string]any `json:"input,omitempty"`
}

type AgentToolCallResult struct {
	RunID         string         `json:"runId"`
	ToolExecution ToolExecution  `json:"toolExecution"`
	Output        map[string]any `json:"output,omitempty"`
}
