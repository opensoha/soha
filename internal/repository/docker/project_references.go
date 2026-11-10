package docker

import (
	"context"

	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

func (r *Repository) CreatePortMapping(ctx context.Context, input domaindocker.PortMappingInput) (domaindocker.PortMapping, error) {
	var result domaindocker.PortMapping
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		project, err := repo.lockProjectReference(ctx, input.HostID, input.ProjectID, input.ServiceID)
		if err != nil {
			return err
		}
		if project.ID != "" {
			input.ProjectID, input.HostID = project.ID, project.HostID
		}
		result, err = repo.createPortMapping(ctx, input)
		return err
	})
	if err != nil {
		return domaindocker.PortMapping{}, err
	}
	return result, nil
}

func (r *Repository) UpdatePortMapping(ctx context.Context, id string, input domaindocker.PortMappingInput) (domaindocker.PortMapping, error) {
	var result domaindocker.PortMapping
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		project, err := repo.lockProjectReference(ctx, input.HostID, input.ProjectID, input.ServiceID)
		if err != nil {
			return err
		}
		if project.ID != "" {
			input.ProjectID, input.HostID = project.ID, project.HostID
		}
		result, err = repo.updatePortMapping(ctx, id, input)
		return err
	})
	if err != nil {
		return domaindocker.PortMapping{}, err
	}
	return result, nil
}

func (r *Repository) UpdateOperation(ctx context.Context, item domaindocker.Operation) (domaindocker.Operation, error) {
	if item.IsTerminal() || item.ProjectID == "" && item.ServiceID == "" {
		return r.updateOperation(ctx, item)
	}
	var result domaindocker.Operation
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		if item.HostID == "" {
			return apperrors.ErrConflict
		}
		project, err := repo.lockProjectReference(ctx, item.HostID, item.ProjectID, item.ServiceID)
		if err != nil {
			return err
		}
		item.ProjectID = project.ID
		result, err = repo.updateOperation(ctx, item)
		return err
	})
	if err != nil {
		return domaindocker.Operation{}, err
	}
	return result, nil
}
