package docker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) FindProjectCreation(ctx context.Context, id, digest string) (domaindocker.Project, error) {
	var stored string
	var raw []byte
	err := r.db.WithContext(ctx).Raw(`SELECT request_digest, receipt FROM docker_project_creations WHERE id = ?`, id).Row().Scan(&stored, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domaindocker.Project{}, ErrNotFound
	}
	if err != nil {
		return domaindocker.Project{}, err
	}
	if stored != digest {
		return domaindocker.Project{}, apperrors.ErrConflict
	}
	var item domaindocker.Project
	err = json.Unmarshal(raw, &item)
	return item, err
}

func (r *Repository) CreateProjectIdempotent(ctx context.Context, input domaindocker.ProjectInput, digest string, services []string) (domaindocker.Project, error) {
	var receipt domaindocker.Project
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.ID == "" || digest == "" {
			return apperrors.ErrInvalidArgument
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "docker.project.create:"+input.ID).Error; err != nil {
			return err
		}
		repo := New(tx)
		found, err := repo.FindProjectCreation(ctx, input.ID, digest)
		if err == nil {
			receipt = found
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		item, err := repo.CreateProject(ctx, input)
		if err != nil {
			return err
		}
		for _, name := range services {
			if _, err := repo.UpsertService(ctx, domaindocker.ServiceInput{ProjectID: item.ID, HostID: item.HostID, Name: name, Status: "defined"}); err != nil {
				return err
			}
		}
		// Keep only the creation identity; secrets are not duplicated in receipts.
		receipt = domaindocker.Project{ID: item.ID, HostID: item.HostID, Name: item.Name, Slug: item.Slug, SourceKind: item.SourceKind, Status: item.Status, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
		raw, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO docker_project_creations (id, request_digest, receipt) VALUES (?, ?, ?::jsonb)`, item.ID, digest, string(raw)).Error
	})
	return receipt, err
}
