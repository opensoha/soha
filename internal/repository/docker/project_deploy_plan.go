package docker

import (
	"context"
	"fmt"

	domain "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

// Keep configuration writers and deletion blocked until the receipt commits.
// NO KEY UPDATE also permits the KEY SHARE taken by legacy service-definition
// foreign keys while their writer holds the project row; UPDATE would deadlock.
func (r *Repository) WithinProjectDeployPlan(ctx context.Context, id string, expected domain.ProjectDeployPreconditions, apply func(context.Context) error) error {
	if apply == nil {
		return apperrors.ErrInvalidArgument
	}
	return dbtx.Within(ctx, r.db, func(txCtx context.Context) error {
		host, err := scanHostRow(dbtx.DB(txCtx, r.db).Raw(hostSelect()+" WHERE id=? FOR NO KEY UPDATE", expected.HostID).Row())
		if err != nil {
			return err
		}
		project, err := r.lockProject(txCtx, id)
		if err != nil {
			return err
		}
		if project.HostID != host.ID || host.ResourceUID != expected.Host.ExpectedUID.String() || host.ConfigurationRevision != expected.Host.ExpectedConfigurationRevision.String() ||
			project.ResourceUID != expected.Project.ExpectedUID.String() || project.ConfigurationRevision != expected.Project.ExpectedConfigurationRevision.String() {
			return fmt.Errorf("%w: reviewed Docker deployment configuration changed; obtain and review a new plan", apperrors.ErrConflict)
		}
		return apply(txCtx)
	})
}
