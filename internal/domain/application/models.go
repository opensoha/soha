package application

import (
	"context"
	"time"
)

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
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

type UpsertInput struct {
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
}
