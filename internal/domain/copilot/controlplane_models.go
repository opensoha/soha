package copilot

import (
	"time"

	domainmcp "github.com/soha/soha/internal/domain/mcp"
)

type DataSource struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	SourceKind        string         `json:"sourceKind"`
	BackendType       string         `json:"backendType"`
	Enabled           bool           `json:"enabled"`
	CredentialRef     string         `json:"credentialRef,omitempty"`
	Scope             map[string]any `json:"scope,omitempty"`
	QueryBudget       map[string]any `json:"queryBudget,omitempty"`
	RedactionPolicy   map[string]any `json:"redactionPolicy,omitempty"`
	MCPAdapter        string         `json:"mcpAdapter"`
	Config            map[string]any `json:"config,omitempty"`
	ValidationStatus  string         `json:"validationStatus,omitempty"`
	ValidationMessage string         `json:"validationMessage,omitempty"`
	LastValidatedAt   *time.Time     `json:"lastValidatedAt,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

type DataSourceInput struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	SourceKind      string         `json:"sourceKind"`
	BackendType     string         `json:"backendType"`
	Enabled         bool           `json:"enabled"`
	CredentialRef   string         `json:"credentialRef,omitempty"`
	Scope           map[string]any `json:"scope,omitempty"`
	QueryBudget     map[string]any `json:"queryBudget,omitempty"`
	RedactionPolicy map[string]any `json:"redactionPolicy,omitempty"`
	MCPAdapter      string         `json:"mcpAdapter"`
	Config          map[string]any `json:"config,omitempty"`
}

type AnalysisProfile struct {
	ID                      string         `json:"id"`
	Name                    string         `json:"name"`
	Mode                    string         `json:"mode"`
	EnabledSources          []string       `json:"enabledSources,omitempty"`
	EnabledPlaybooks        []string       `json:"enabledPlaybooks,omitempty"`
	QueryBudgets            map[string]any `json:"queryBudgets,omitempty"`
	OutputStyle             map[string]any `json:"outputStyle,omitempty"`
	RemediationPolicy       string         `json:"remediationPolicy"`
	DefaultTimeRangeMinutes int            `json:"defaultTimeRangeMinutes"`
	TimeoutSeconds          int            `json:"timeoutSeconds"`
	Enabled                 bool           `json:"enabled"`
	CreatedAt               time.Time      `json:"createdAt"`
	UpdatedAt               time.Time      `json:"updatedAt"`
}

type AnalysisProfileInput struct {
	ID                      string         `json:"id"`
	Name                    string         `json:"name"`
	Mode                    string         `json:"mode"`
	EnabledSources          []string       `json:"enabledSources,omitempty"`
	EnabledPlaybooks        []string       `json:"enabledPlaybooks,omitempty"`
	QueryBudgets            map[string]any `json:"queryBudgets,omitempty"`
	OutputStyle             map[string]any `json:"outputStyle,omitempty"`
	RemediationPolicy       string         `json:"remediationPolicy"`
	DefaultTimeRangeMinutes int            `json:"defaultTimeRangeMinutes"`
	TimeoutSeconds          int            `json:"timeoutSeconds"`
	Enabled                 bool           `json:"enabled"`
}

type AutomationPolicy struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Enabled            bool           `json:"enabled"`
	TriggerType        string         `json:"triggerType"`
	AnalysisKinds      []string       `json:"analysisKinds,omitempty"`
	AgentProviderID    string         `json:"agentProviderId,omitempty"`
	TriggerConditions  map[string]any `json:"triggerConditions,omitempty"`
	DedupWindowSeconds int            `json:"dedupWindowSeconds"`
	AnalysisProfileID  string         `json:"analysisProfileId"`
	RemediationPolicy  string         `json:"remediationPolicy"`
	ApprovalPolicy     map[string]any `json:"approvalPolicy,omitempty"`
	CooldownSeconds    int            `json:"cooldownSeconds"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

type AutomationPolicyInput struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	Enabled            bool           `json:"enabled"`
	TriggerType        string         `json:"triggerType"`
	AnalysisKinds      []string       `json:"analysisKinds,omitempty"`
	AgentProviderID    string         `json:"agentProviderId,omitempty"`
	TriggerConditions  map[string]any `json:"triggerConditions,omitempty"`
	DedupWindowSeconds int            `json:"dedupWindowSeconds"`
	AnalysisProfileID  string         `json:"analysisProfileId"`
	RemediationPolicy  string         `json:"remediationPolicy"`
	ApprovalPolicy     map[string]any `json:"approvalPolicy,omitempty"`
	CooldownSeconds    int            `json:"cooldownSeconds"`
}

type WorkbenchSkill struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Category       string   `json:"category,omitempty"`
	OwnerModule    string   `json:"ownerModule,omitempty"`
	Description    string   `json:"description,omitempty"`
	Enabled        bool     `json:"enabled"`
	Scopes         []string `json:"scopes,omitempty"`
	CapabilityRefs []string `json:"capabilityRefs,omitempty"`
	BlueprintRefs  []string `json:"blueprintRefs,omitempty"`
	ScopeRules     []string `json:"scopeRules,omitempty"`
}

type WorkbenchAnalysisProfile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Enabled bool   `json:"enabled"`
}

type WorkbenchDataSource struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	SourceKind        string `json:"sourceKind"`
	BackendType       string `json:"backendType"`
	Enabled           bool   `json:"enabled"`
	MCPAdapter        string `json:"mcpAdapter"`
	ValidationStatus  string `json:"validationStatus,omitempty"`
	ValidationMessage string `json:"validationMessage,omitempty"`
}

type WorkbenchCatalog struct {
	Adapters         []domainmcp.Adapter        `json:"adapters"`
	DataSources      []WorkbenchDataSource      `json:"dataSources"`
	AnalysisProfiles []WorkbenchAnalysisProfile `json:"analysisProfiles"`
	SkillsRegistry   []WorkbenchSkill           `json:"skillsRegistry,omitempty"`
	AgentProviders   []AgentProvider            `json:"agentProviders,omitempty"`
	Capabilities     []AgentCapability          `json:"capabilities,omitempty"`
	ToolBindings     []AgentToolBinding         `json:"toolBindings,omitempty"`
	SkillBindings    []AgentSkillBinding        `json:"skillBindings,omitempty"`
}
