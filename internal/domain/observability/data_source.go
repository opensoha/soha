package observability

import (
	"strings"
	"time"
)

// DataSource is the shared durable telemetry source persisted in ai_data_sources.
type DataSource struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	SourceKind        string         `json:"sourceKind"`
	BackendType       string         `json:"backendType"`
	Enabled           bool           `json:"enabled"`
	CredentialRef     string         `json:"-"`
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

// RetiredBackend reports a historical built-in source that Core can no longer query.
func (source DataSource) RetiredBackend() bool {
	return IsRetiredBackend(source.SourceKind, source.BackendType)
}

func IsRetiredBackend(kind, backend string) bool {
	backend = strings.ToLower(strings.TrimSpace(backend))
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "logs":
		return backend == "es" || backend == "elasticsearch" || backend == "clickhouse"
	case "traces":
		return backend == "jaeger" || backend == "skywalking"
	default:
		return false
	}
}
