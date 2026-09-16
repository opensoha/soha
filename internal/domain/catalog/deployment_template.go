package catalog

import (
	"time"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

// ParameterSchema is a bounded subset of JSON Schema, without external references.
type ParameterSchema struct {
	Type        string                     `json:"type"`
	Description string                     `json:"description,omitempty"`
	Properties  map[string]ParameterSchema `json:"properties,omitempty"`
	Required    []string                   `json:"required,omitempty"`
	Items       *ParameterSchema           `json:"items,omitempty"`
	MapValues   *ParameterSchema           `json:"mapValues,omitempty"`
	Enum        []any                      `json:"enum,omitempty"`
	Minimum     *float64                   `json:"minimum,omitempty"`
	Maximum     *float64                   `json:"maximum,omitempty"`
	MinLength   *int                       `json:"minLength,omitempty"`
	MaxLength   *int                       `json:"maxLength,omitempty"`
	MinItems    *int                       `json:"minItems,omitempty"`
	MaxItems    *int                       `json:"maxItems,omitempty"`
}

type DeploymentTemplateGit struct {
	RepositoryID string `json:"repositoryId"`
	Commit       string `json:"commit"`
	Path         string `json:"path"`
}

type DeploymentTemplateHelm struct {
	SecretRefs    map[string]string `json:"secretRefs,omitempty"`
	RepositoryURL string            `json:"repositoryUrl"`
	Chart         string            `json:"chart"`
	Version       string            `json:"version"`
	ConnectionID  string            `json:"connectionId,omitempty"`
	Digest        string            `json:"digest,omitempty"`
	Values        map[string]any    `json:"values"`
}

type DeploymentTemplateSource struct {
	Renderer  string                           `json:"renderer"`
	Files     []domainmanifest.File            `json:"files,omitempty"`
	Kustomize *domainmanifest.KustomizeOptions `json:"kustomize,omitempty"`
	Git       *DeploymentTemplateGit           `json:"git,omitempty"`
	Helm      *DeploymentTemplateHelm          `json:"helm,omitempty"`
}

type DeploymentTemplateHealth struct {
	Mode           string `json:"mode"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

type DeploymentTemplateSpec struct {
	Key                  string                   `json:"key"`
	Name                 string                   `json:"name"`
	Description          string                   `json:"description,omitempty"`
	Source               DeploymentTemplateSource `json:"source"`
	ParameterSchema      ParameterSchema          `json:"parameterSchema"`
	Defaults             map[string]any           `json:"defaults"`
	EnvironmentOverrides []string                 `json:"environmentOverrides,omitempty"`
	Artifacts            map[string]string        `json:"artifacts,omitempty"`
	Health               DeploymentTemplateHealth `json:"health"`
	Enabled              bool                     `json:"enabled"`
}

type DeploymentTemplateInput struct {
	CopiedFrom *TemplateCopyOrigin `json:"copiedFrom,omitempty"`
	DeploymentTemplateSpec
	ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
}

type DeploymentTemplate struct {
	DeploymentTemplateSpec
	ID               string    `json:"id"`
	Revision         int64     `json:"revision"`
	PublishedVersion int64     `json:"publishedVersion"`
	PublicationState string    `json:"publicationState"`
	ContentDigest    string    `json:"contentDigest,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type DeploymentTemplateBinding struct {
	TemplateID        string              `json:"templateId"`
	Version           int64               `json:"version"`
	Parameters        map[string]any      `json:"parameters"`
	Detached          bool                `json:"detached,omitempty"`
	DetachedTemplate  *DeploymentTemplate `json:"detachedTemplate,omitempty"`
	ManifestPackageID string              `json:"manifestPackageId,omitempty"`
}

type DeploymentTemplatePreviewInput struct {
	TemplateID               string         `json:"templateId"`
	Version                  int64          `json:"version"`
	ServiceKey               string         `json:"serviceKey"`
	ApplicationEnvironmentID string         `json:"applicationEnvironmentId,omitempty"`
	Parameters               map[string]any `json:"parameters"`
	Overrides                map[string]any `json:"overrides,omitempty"`
}

type DeploymentTemplatePreview struct {
	TemplateID        string                   `json:"templateId"`
	Version           int64                    `json:"version"`
	Source            DeploymentTemplateSource `json:"source"`
	Parameters        map[string]any           `json:"parameters"`
	Digest            string                   `json:"digest"`
	ConfigurationOnly bool                     `json:"configurationOnly"`
	Diagnostics       []string                 `json:"diagnostics"`
}
