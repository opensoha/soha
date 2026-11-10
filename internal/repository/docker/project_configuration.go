package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

// Old configuration saves also stamped last_seen_at; never erase that evidence
// merely because a legacy row still says "defined".
const unobservedServiceDefinition = `docker_services.status IN ('defined', 'unknown')
	AND COALESCE(docker_services.container_id, '') = '' AND docker_services.last_seen_at IS NULL
	AND COALESCE(docker_services.image, '') = '' AND docker_services.restart_count = 0 AND docker_services.cpu_percent = 0
	AND docker_services.memory_bytes = 0 AND docker_services.network_rx_bytes = 0 AND docker_services.network_tx_bytes = 0
	AND docker_services.config = '{}'::jsonb`

func (r *Repository) CreateProjectConfiguration(ctx context.Context, input domaindocker.ProjectInput, names []string) (domaindocker.Project, error) {
	var result domaindocker.Project
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		project, err := repo.CreateProject(ctx, input)
		if err != nil {
			return err
		}
		if err := repo.syncProjectDefinitions(ctx, project, names); err != nil {
			return err
		}
		result = project
		return nil
	})
	if err != nil {
		return domaindocker.Project{}, err
	}
	return result, nil
}

func (r *Repository) UpdateProjectConfiguration(ctx context.Context, id string, input domaindocker.ProjectInput, names []string) (domaindocker.Project, error) {
	var result domaindocker.Project
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		current, err := repo.lockProject(ctx, id)
		if err != nil {
			return err
		}
		if strings.TrimSpace(input.HostID) != current.HostID {
			if err := repo.checkProjectHostMove(ctx, current); err != nil {
				return err
			}
		}
		project, err := repo.UpdateProject(ctx, id, input)
		if err != nil {
			return err
		}
		if project.HostID != current.HostID {
			// Retained definitions, including names removed from Compose, follow
			// a cold metadata move. The locked preflight excluded observations.
			if err := tx.Exec("UPDATE docker_services SET host_id = ?, updated_at = ? WHERE project_id = ? AND ("+unobservedServiceDefinition+")", project.HostID, time.Now().UTC(), project.ID).Error; err != nil {
				return fmt.Errorf("move Docker service definitions: %w", err)
			}
		}
		if err := repo.syncProjectDefinitions(ctx, project, names); err != nil {
			return err
		}
		result = project
		return nil
	})
	if err != nil {
		return domaindocker.Project{}, err
	}
	return result, nil
}

func (r *Repository) lockProject(ctx context.Context, id string) (domaindocker.Project, error) {
	return scanProjectRow(dbtx.DB(ctx, r.db).Raw(projectSelect()+" WHERE id = ? FOR UPDATE", strings.TrimSpace(id)).Row())
}

func (r *Repository) checkProjectHostMove(ctx context.Context, project domaindocker.Project) error {
	if project.LastDeployedAt != nil || project.Status != "draft" && project.Status != "defined" && project.Status != "unknown" {
		return fmt.Errorf("%w: deployed Docker project requires an explicit runtime relocation", apperrors.ErrConflict)
	}
	id := project.ID
	var blocked bool
	err := dbtx.DB(ctx, r.db).Raw(`SELECT
		EXISTS (SELECT 1 FROM docker_services WHERE project_id = ? AND NOT (`+unobservedServiceDefinition+`))
		OR EXISTS (SELECT 1 FROM docker_operations o LEFT JOIN docker_services s ON s.id = o.service_id
			WHERE o.project_id = ? OR s.project_id = ?)
		OR EXISTS (SELECT 1 FROM docker_port_mappings p LEFT JOIN docker_services s ON s.id = p.service_id WHERE p.project_id = ? OR s.project_id = ?)`, id, id, id, id, id).Scan(&blocked).Error
	if err != nil {
		return fmt.Errorf("check Docker project relocation: %w", err)
	}
	if blocked {
		return fmt.Errorf("%w: Docker project has runtime evidence, bound ports or operation history requiring explicit relocation", apperrors.ErrConflict)
	}
	return nil
}

func (r *Repository) lockProjectReference(ctx context.Context, hostID, projectID, serviceID string) (domaindocker.Project, error) {
	projectID, serviceID = strings.TrimSpace(projectID), strings.TrimSpace(serviceID)
	if serviceID != "" {
		service, err := r.GetService(ctx, serviceID)
		if err != nil {
			return domaindocker.Project{}, err
		}
		if projectID != "" && projectID != service.ProjectID {
			return domaindocker.Project{}, apperrors.ErrConflict
		}
		projectID = service.ProjectID
	}
	if projectID == "" {
		return domaindocker.Project{}, nil
	}
	project, err := r.lockProject(ctx, projectID)
	if err != nil {
		return domaindocker.Project{}, err
	}
	if requested := strings.TrimSpace(hostID); requested != "" && requested != project.HostID {
		return domaindocker.Project{}, apperrors.ErrConflict
	}
	if serviceID != "" {
		service, err := r.GetService(ctx, serviceID)
		if err != nil {
			return domaindocker.Project{}, err
		}
		if service.ProjectID != project.ID || service.HostID != project.HostID {
			return domaindocker.Project{}, apperrors.ErrConflict
		}
	}
	return project, nil
}

func (r *Repository) syncProjectDefinitions(ctx context.Context, project domaindocker.Project, names []string) error {
	if len(names) == 0 {
		return apperrors.ErrInvalidArgument
	}
	for _, name := range names {
		if name == "" || name != strings.TrimSpace(name) {
			return apperrors.ErrInvalidArgument
		}
		now := time.Now().UTC()
		// A saved definition is not a runtime observation. Existing inventory,
		// including names removed from Compose, awaits real runtime reconciliation.
		if err := dbtx.DB(ctx, r.db).Exec(`INSERT INTO docker_services
			(id, project_id, host_id, name, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'defined', ?, ?)
			ON CONFLICT (project_id, name) DO UPDATE SET host_id = EXCLUDED.host_id, updated_at = EXCLUDED.updated_at
			WHERE docker_services.host_id IS DISTINCT FROM EXCLUDED.host_id AND (`+unobservedServiceDefinition+`)`,
			uuid.NewString(), project.ID, project.HostID, name, now, now).Error; err != nil {
			return fmt.Errorf("save Docker service definition: %w", err)
		}
	}
	return nil
}
