package dto

type CreateCopilotSessionRequest struct {
	Title string `json:"title"`
}

type SendCopilotMessageRequest struct {
	Content string `json:"content"`
}

type CreateRootCauseRunRequest struct {
	Title            string `json:"title"`
	ClusterID        string `json:"clusterId"`
	Namespace        string `json:"namespace"`
	WorkloadKind     string `json:"workloadKind"`
	WorkloadName     string `json:"workloadName"`
	AlertID          string `json:"alertId"`
	TimeRangeMinutes int    `json:"timeRangeMinutes"`
	Question         string `json:"question"`
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
