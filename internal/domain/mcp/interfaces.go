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
}

type Adapter struct {
	ID                string   `json:"id"`
	SourceKind        string   `json:"sourceKind"`
	SupportedBackends []string `json:"supportedBackends,omitempty"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Scopes            []string `json:"scopes"`
	Tools             []Tool   `json:"tools,omitempty"`
}

type Service interface {
	ListCapabilities(context.Context) ([]Capability, error)
}
