package docker

import (
	"context"

	domaindocker "github.com/opensoha/soha/internal/domain/docker"
)

// ProjectConfigurationWriter commits configuration and desired definitions together.
type ProjectConfigurationWriter interface {
	CreateProjectConfiguration(context.Context, domaindocker.ProjectInput, []string) (domaindocker.Project, error)
	UpdateProjectConfiguration(context.Context, string, domaindocker.ProjectInput, []string) (domaindocker.Project, error)
}
