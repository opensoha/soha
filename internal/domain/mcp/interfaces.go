package mcp

import "context"

type Capability struct {
	AdapterID   string   `json:"adapterID"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Scopes      []string `json:"scopes"`
}

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SchemaHint  string `json:"schemaHint,omitempty"`
}

type Adapter struct {
	ID                string   `json:"id"`
	SourceKind        string   `json:"sourceKind"`
	SupportedBackends []string `json:"supportedBackends,omitempty"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Category          string   `json:"category,omitempty"`
	RequiresConfig    bool     `json:"requiresConfig"`
	SupportsSessionOverride bool `json:"supportsSessionOverride"`
	Scopes            []string `json:"scopes"`
	Tools             []Tool   `json:"tools,omitempty"`
	DefaultBudget     map[string]any `json:"defaultBudget,omitempty"`
	ToolSchemaSummary map[string]string `json:"toolSchemaSummary,omitempty"`
}

type Service interface {
	ListCapabilities(context.Context) ([]Capability, error)
}
