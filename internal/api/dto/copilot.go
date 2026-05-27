package dto

import domaincopilot "github.com/kubecrux/kubecrux/internal/domain/copilot"

type CreateCopilotSessionRequest struct {
	Title           string         `json:"title"`
	Mode            string         `json:"mode"`
	AgentProviderID string         `json:"agentProviderId"`
	Scope           map[string]any `json:"scope"`
	Tags            []string       `json:"tags"`
	AlertID         string         `json:"alertId"`
	Workload        string         `json:"workload"`
}

type UpdateCopilotSessionRequest struct {
	Title           string         `json:"title"`
	Mode            string         `json:"mode"`
	AgentProviderID string         `json:"agentProviderId"`
	Status          string         `json:"status"`
	Scope           map[string]any `json:"scope"`
	Toolset         map[string]any `json:"toolset"`
	Tags            []string       `json:"tags"`
	Summary         string         `json:"summary"`
	Archived        bool           `json:"archived"`
}

type SendCopilotMessageRequest struct {
	Content string `json:"content"`
}

type CreateRootCauseRunRequest struct {
	Title             string `json:"title"`
	Kind              string `json:"kind"`
	SessionID         string `json:"sessionId"`
	AnalysisProfileID string `json:"analysisProfileId"`
	AgentProviderID   string `json:"agentProviderId"`
	TriggerType       string `json:"triggerType"`
	ClusterID         string `json:"clusterId"`
	Namespace         string `json:"namespace"`
	WorkloadKind      string `json:"workloadKind"`
	WorkloadName      string `json:"workloadName"`
	AlertID           string `json:"alertId"`
	TimeRangeMinutes  int    `json:"timeRangeMinutes"`
	Question          string `json:"question"`
}

type AnalyzeSessionRequest struct {
	Mode              string         `json:"mode"`
	AnalysisProfileID string         `json:"analysisProfileId"`
	AgentProviderID   string         `json:"agentProviderId"`
	TriggerType       string         `json:"triggerType"`
	Question          string         `json:"question"`
	Scope             map[string]any `json:"scope"`
}

type AgentRunClaimRequest struct {
	AgentID     string   `json:"agentId"`
	ProviderIDs []string `json:"providerIds"`
	Kinds       []string `json:"kinds"`
}

type AgentRunCallbackRequest struct {
	RunID             string                           `json:"runId"`
	CallbackToken     string                           `json:"callbackToken"`
	AgentID           string                           `json:"agentId"`
	Status            string                           `json:"status"`
	Payload           map[string]any                   `json:"payload"`
	ToolExecutions    []domaincopilot.ToolExecution    `json:"toolExecutions"`
	AnalysisArtifacts []domaincopilot.AnalysisArtifact `json:"analysisArtifacts"`
	ExternalRunID     string                           `json:"externalRunId"`
	ErrorMessage      string                           `json:"errorMessage"`
}

type AgentToolCallRequest struct {
	RunID         string         `json:"runId"`
	CallbackToken string         `json:"callbackToken"`
	AgentID       string         `json:"agentId"`
	ToolBindingID string         `json:"toolBindingId"`
	AdapterID     string         `json:"adapterId"`
	ToolName      string         `json:"toolName"`
	Input         map[string]any `json:"input"`
}

type DataSourceRequest struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	SourceKind      string         `json:"sourceKind"`
	BackendType     string         `json:"backendType"`
	Enabled         bool           `json:"enabled"`
	CredentialRef   string         `json:"credentialRef"`
	Scope           map[string]any `json:"scope"`
	QueryBudget     map[string]any `json:"queryBudget"`
	RedactionPolicy map[string]any `json:"redactionPolicy"`
	MCPAdapter      string         `json:"mcpAdapter"`
	Config          map[string]any `json:"config"`
}

type AnalysisProfileRequest struct {
	ID                      string         `json:"id"`
	Name                    string         `json:"name"`
	Mode                    string         `json:"mode"`
	EnabledSources          []string       `json:"enabledSources"`
	EnabledPlaybooks        []string       `json:"enabledPlaybooks"`
	QueryBudgets            map[string]any `json:"queryBudgets"`
	OutputStyle             map[string]any `json:"outputStyle"`
	RemediationPolicy       string         `json:"remediationPolicy"`
	DefaultTimeRangeMinutes int            `json:"defaultTimeRangeMinutes"`
	TimeoutSeconds          int            `json:"timeoutSeconds"`
	Enabled                 bool           `json:"enabled"`
}

type AutomationPolicyRequest struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Enabled            bool           `json:"enabled"`
	TriggerType        string         `json:"triggerType"`
	AnalysisKinds      []string       `json:"analysisKinds"`
	AgentProviderID    string         `json:"agentProviderId"`
	TriggerConditions  map[string]any `json:"triggerConditions"`
	DedupWindowSeconds int            `json:"dedupWindowSeconds"`
	AnalysisProfileID  string         `json:"analysisProfileId"`
	RemediationPolicy  string         `json:"remediationPolicy"`
	ApprovalPolicy     map[string]any `json:"approvalPolicy"`
	CooldownSeconds    int            `json:"cooldownSeconds"`
}

type CreateInspectionTaskRequest struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	ScopeType       string         `json:"scopeType"`
	ClusterID       string         `json:"clusterId"`
	Namespace       string         `json:"namespace"`
	Checks          []string       `json:"checks"`
	Enabled         bool           `json:"enabled"`
	IntervalMinutes int            `json:"intervalMinutes"`
	Metadata        map[string]any `json:"metadata"`
}
