package docker

import (
	"context"
	"errors"
	"time"

	dockerruntime "github.com/opensoha/soha/contracts/docker/runtime"
	domain "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) freezeRuntimeContainers(ctx context.Context, project domain.Project, operation *domain.Operation) error {
	if project.RuntimeIdentityVersion == 0 {
		return nil
	}
	if operation.OperationKind != "project_deploy" && operation.OperationKind != "container_start" && operation.OperationKind != "service_action" {
		return nil
	}
	binding, err := dockerruntime.ProjectBindingFromValue(operation.Payload["runtimeBinding"])
	if err != nil || project.RuntimeIdentityVersion != 1 || binding.ProjectUID != project.ResourceUID {
		return apperrors.ErrConflict
	}
	host, err := r.GetHost(ctx, project.HostID)
	if err != nil {
		return err
	}
	if binding.HostUID != host.ResourceUID {
		return apperrors.ErrConflict
	}
	services, err := r.ListServices(ctx, domain.ServiceFilter{ProjectID: project.ID, HostID: project.HostID, Limit: 257})
	if err != nil {
		return err
	}
	current, err := domain.RuntimeContainerSnapshot(binding, project.HostID, project.ID, services)
	if err != nil {
		return apperrors.ErrConflict
	}
	if value, present := operation.Payload["runtimeContainers"]; present {
		frozen, err := dockerruntime.ContainerSetFromValue(value, binding)
		if err != nil || !dockerruntime.SameContainerSet(frozen, current) {
			return apperrors.ErrConflict
		}
	}
	operation.Payload["runtimeContainers"] = current
	return nil
}

// ReconcileRuntimeServices applies a complete, confirmed inventory. Absent
// containers lose runtime state, never their configuration/service definition.
// The application also holds both immutable UIDs in the outer callback tx.
func (r *Repository) ReconcileRuntimeServices(ctx context.Context, hostID, projectID string, services []domain.ServiceInput) error {
	if len(services) > 256 {
		return apperrors.ErrInvalidArgument
	}
	return dbtx.Within(ctx, r.db, func(txCtx context.Context) error {
		project, err := r.lockProject(txCtx, projectID)
		if err != nil {
			return err
		}
		if project.RuntimeIdentityVersion != 1 || project.HostID != hostID {
			return apperrors.ErrConflict
		}
		names := make([]string, 0, len(services))
		for _, input := range services {
			if input.ProjectID != projectID || input.HostID != hostID {
				return apperrors.ErrConflict
			}
			current, err := r.getServiceByProjectName(txCtx, projectID, input.Name)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			// Runtime observations do not replace the service's configuration.
			if err == nil {
				input.Config = current.Config
			}
			if _, err := r.upsertServiceRuntime(txCtx, input); err != nil {
				return err
			}
			names = append(names, input.Name)
		}
		query := dbtx.DB(txCtx, r.db).Table("docker_services").Where("project_id = ? AND host_id = ?", projectID, hostID)
		if len(names) > 0 {
			query = query.Where("name NOT IN ?", names)
		}
		return query.Updates(map[string]any{"status": "removed", "container_id": nil, "restart_count": 0, "cpu_percent": 0, "memory_bytes": 0, "network_rx_bytes": 0, "network_tx_bytes": 0, "last_seen_at": time.Now().UTC(), "updated_at": time.Now().UTC()}).Error
	})
}
