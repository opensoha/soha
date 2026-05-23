package application

import (
	"context"
	"time"
)

type BuildSourceType string

const (
	BuildSourceTypeRepoDockerfile   BuildSourceType = "repo_dockerfile"
	BuildSourceTypePlatformTemplate BuildSourceType = "platform_build_template"
	BuildSourceTypeExternalPipeline BuildSourceType = "external_pipeline"
)

type BuildSource struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       BuildSourceType `json:"type"`
	Enabled    bool            `json:"enabled"`
	IsDefault  bool            `json:"isDefault"`
	BuildImage string          `json:"buildImage,omitempty"`
	DefaultTag string          `json:"defaultTag,omitempty"`
	Config     map[string]any  `json:"config,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

type BuildSourceInput struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       BuildSourceType `json:"type"`
	Enabled    bool            `json:"enabled"`
	IsDefault  bool            `json:"isDefault"`
	BuildImage string          `json:"buildImage,omitempty"`
	DefaultTag string          `json:"defaultTag,omitempty"`
	Config     map[string]any  `json:"config,omitempty"`
}

type ServiceKind string

const (
	ServiceKindKubernetesWorkload ServiceKind = "kubernetes_workload"
	ServiceKindHelmRelease        ServiceKind = "helm_release"
	ServiceKindExternalService    ServiceKind = "external_service"
	ServiceKindJob                ServiceKind = "job"
)

type ServiceContainer struct {
	ID                 string         `json:"id"`
	ServiceID          string         `json:"serviceId,omitempty"`
	Name               string         `json:"name"`
	ImageRepository    string         `json:"imageRepository,omitempty"`
	DefaultTagTemplate string         `json:"defaultTagTemplate,omitempty"`
	DockerfilePath     string         `json:"dockerfilePath,omitempty"`
	BuildContextDir    string         `json:"buildContextDir,omitempty"`
	RuntimePorts       []int          `json:"runtimePorts,omitempty"`
	EnvSchema          map[string]any `json:"envSchema,omitempty"`
	ResourceProfile    map[string]any `json:"resourceProfile,omitempty"`
	HealthCheck        map[string]any `json:"healthCheck,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

type ServiceContainerInput struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	ImageRepository    string         `json:"imageRepository,omitempty"`
	DefaultTagTemplate string         `json:"defaultTagTemplate,omitempty"`
	DockerfilePath     string         `json:"dockerfilePath,omitempty"`
	BuildContextDir    string         `json:"buildContextDir,omitempty"`
	RuntimePorts       []int          `json:"runtimePorts,omitempty"`
	EnvSchema          map[string]any `json:"envSchema,omitempty"`
	ResourceProfile    map[string]any `json:"resourceProfile,omitempty"`
	HealthCheck        map[string]any `json:"healthCheck,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type Service struct {
	ID                  string             `json:"id"`
	ApplicationID       string             `json:"applicationId"`
	Key                 string             `json:"key"`
	Name                string             `json:"name"`
	Description         string             `json:"description,omitempty"`
	ServiceKind         ServiceKind        `json:"serviceKind"`
	OwnerTeam           string             `json:"ownerTeam,omitempty"`
	RepositoryProvider  string             `json:"repositoryProvider,omitempty"`
	RepositoryProjectID string             `json:"repositoryProjectId,omitempty"`
	RepositoryPath      string             `json:"repositoryPath,omitempty"`
	DefaultBranch       string             `json:"defaultBranch,omitempty"`
	BuildSourceID       string             `json:"buildSourceId,omitempty"`
	Enabled             bool               `json:"enabled"`
	Metadata            map[string]any     `json:"metadata,omitempty"`
	Containers          []ServiceContainer `json:"containers,omitempty"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
}

type ServiceInput struct {
	ID                  string                  `json:"id"`
	Key                 string                  `json:"key"`
	Name                string                  `json:"name"`
	Description         string                  `json:"description,omitempty"`
	ServiceKind         ServiceKind             `json:"serviceKind"`
	OwnerTeam           string                  `json:"ownerTeam,omitempty"`
	RepositoryProvider  string                  `json:"repositoryProvider,omitempty"`
	RepositoryProjectID string                  `json:"repositoryProjectId,omitempty"`
	RepositoryPath      string                  `json:"repositoryPath,omitempty"`
	DefaultBranch       string                  `json:"defaultBranch,omitempty"`
	BuildSourceID       string                  `json:"buildSourceId,omitempty"`
	Enabled             bool                    `json:"enabled"`
	Metadata            map[string]any          `json:"metadata,omitempty"`
	Containers          []ServiceContainerInput `json:"containers,omitempty"`
}

type App struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	Key                 string         `json:"key"`
	Group               string         `json:"group"`
	BusinessLineID      string         `json:"businessLineId,omitempty"`
	Language            string         `json:"language"`
	Description         string         `json:"description,omitempty"`
	OwnerTeam           string         `json:"ownerTeam,omitempty"`
	RepositoryProvider  string         `json:"repositoryProvider,omitempty"`
	RepositoryProjectID string         `json:"repositoryProjectId,omitempty"`
	RepositoryPath      string         `json:"repositoryPath,omitempty"`
	DefaultBranch       string         `json:"defaultBranch,omitempty"`
	DefaultTag          string         `json:"defaultTag,omitempty"`
	BuildImage          string         `json:"buildImage,omitempty"`
	BuildContextDir     string         `json:"buildContextDir,omitempty"`
	DockerfilePath      string         `json:"dockerfilePath,omitempty"`
	Enabled             bool           `json:"enabled"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	BuildSources        []BuildSource  `json:"buildSources,omitempty"`
	EnvironmentCount    int            `json:"environmentCount,omitempty"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

type UpsertInput struct {
	ID                  string             `json:"id"`
	Name                string             `json:"name"`
	Key                 string             `json:"key"`
	Group               string             `json:"group"`
	BusinessLineID      string             `json:"businessLineId,omitempty"`
	Language            string             `json:"language"`
	Description         string             `json:"description,omitempty"`
	OwnerTeam           string             `json:"ownerTeam,omitempty"`
	RepositoryProvider  string             `json:"repositoryProvider,omitempty"`
	RepositoryProjectID string             `json:"repositoryProjectId,omitempty"`
	RepositoryPath      string             `json:"repositoryPath,omitempty"`
	DefaultBranch       string             `json:"defaultBranch,omitempty"`
	DefaultTag          string             `json:"defaultTag,omitempty"`
	BuildImage          string             `json:"buildImage,omitempty"`
	BuildContextDir     string             `json:"buildContextDir,omitempty"`
	DockerfilePath      string             `json:"dockerfilePath,omitempty"`
	Enabled             bool               `json:"enabled"`
	Metadata            map[string]any     `json:"metadata,omitempty"`
	BuildSources        []BuildSourceInput `json:"buildSources,omitempty"`
}

type Filter struct {
	Search string
	Limit  int
}

type GitRepository struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"pathWithNamespace"`
	DefaultBranch     string `json:"defaultBranch,omitempty"`
	WebURL            string `json:"webUrl,omitempty"`
}

type GitReference struct {
	Name      string    `json:"name"`
	CommitSHA string    `json:"commitSha,omitempty"`
	Protected bool      `json:"protected"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

type Repository interface {
	List(context.Context, Filter) ([]App, error)
	Get(context.Context, string) (App, error)
	Create(context.Context, UpsertInput) (App, error)
	Update(context.Context, string, UpsertInput) (App, error)
	Delete(context.Context, string) error
	ListServices(context.Context, string) ([]Service, error)
	GetService(context.Context, string, string) (Service, error)
	CreateService(context.Context, string, ServiceInput) (Service, error)
	UpdateService(context.Context, string, string, ServiceInput) (Service, error)
	DeleteService(context.Context, string, string) error
}
