package contentdlp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("content DLP database is required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Policy(ctx context.Context, id string) (domain.Policy, error) {
	p := domain.DefaultPolicy(id)
	var draft, published []byte
	err := r.db.WithContext(ctx).Raw(`SELECT revision,draft,published,published_version,updated_at FROM docker_volume_dlp_policies WHERE project_id=?`, id).Row().Scan(&p.Revision, &draft, &published, &p.PublishedVersion, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(draft, &p.Draft); err != nil {
		return p, err
	}
	if len(published) > 0 && string(published) != "null" {
		p.Published = &domain.Content{}
		err = json.Unmarshal(published, p.Published)
	}
	if err == nil && (p.Published == nil && p.PublishedVersion != 0 || p.Published != nil && p.PublishedVersion < 1 || p.PublishedVersion > p.Revision) {
		return p, apperrors.ErrServiceUnavailable
	}
	return p, err
}

func lockProject(tx *gorm.DB, id string) (time.Time, error) {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "docker-volume-dlp/"+id).Error; err != nil {
		return time.Time{}, err
	}
	var updated time.Time
	err := tx.Raw(`SELECT updated_at FROM docker_projects WHERE id=? FOR SHARE`, id).Row().Scan(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		return updated, apperrors.ErrNotFound
	}
	return updated, err
}

func addEvent(tx *gorm.DB, e domain.Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO docker_volume_dlp_events(id,project_id,actor_id,action,metadata,created_at) VALUES(?,?,?,?,?::jsonb,?)`, e.ID, e.ProjectID, e.ActorID, e.Action, string(data), e.CreatedAt).Error
}

func (r *Repository) Save(ctx context.Context, p domain.Policy, event domain.Event) (domain.Policy, error) {
	var result domain.Policy
	raw, err := json.Marshal(p.Draft)
	if err != nil {
		return domain.Policy{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if updated, err := lockProject(tx, p.ProjectID); err != nil {
			return err
		} else if !updated.Equal(event.ProjectUpdatedAt) {
			return apperrors.ErrConflict
		}
		current, err := (&Repository{db: tx}).Policy(ctx, p.ProjectID)
		if err != nil {
			return err
		}
		if current.Revision != p.Revision {
			return apperrors.ErrConflict
		}
		mutation := tx.Exec(`INSERT INTO docker_volume_dlp_policies(project_id,revision,draft,updated_at) VALUES(?,1,?::jsonb,?)
 ON CONFLICT(project_id) DO UPDATE SET draft=EXCLUDED.draft,revision=docker_volume_dlp_policies.revision+1,updated_at=EXCLUDED.updated_at WHERE docker_volume_dlp_policies.revision=?`, p.ProjectID, string(raw), event.CreatedAt, p.Revision)
		if mutation.Error != nil {
			return mutation.Error
		}
		if mutation.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		event.PolicyVersion = current.PublishedVersion
		if err := addEvent(tx, event); err != nil {
			return err
		}
		result, err = (&Repository{db: tx}).Policy(ctx, p.ProjectID)
		return err
	})
	if err != nil {
		return domain.Policy{}, err
	}
	return result, nil
}

func (r *Repository) Publish(ctx context.Context, id string, revision int64, event domain.Event) (domain.Policy, error) {
	var result domain.Policy
	var err error
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if updated, err := lockProject(tx, id); err != nil {
			return err
		} else if !updated.Equal(event.ProjectUpdatedAt) {
			return apperrors.ErrConflict
		}
		mutation := tx.Exec(`UPDATE docker_volume_dlp_policies SET published=draft,published_version=revision+1,revision=revision+1,updated_at=? WHERE project_id=? AND revision=?`, event.CreatedAt, id, revision)
		if mutation.Error != nil {
			return mutation.Error
		}
		if mutation.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		event.PolicyVersion = revision + 1
		if err := addEvent(tx, event); err != nil {
			return err
		}
		result, err = (&Repository{db: tx}).Policy(ctx, id)
		return err
	})
	if err != nil {
		return domain.Policy{}, err
	}
	return result, nil
}

func (r *Repository) Commit(ctx context.Context, e domain.Event, projectUpdated time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updated, err := lockProject(tx, e.ProjectID)
		if err != nil {
			return err
		}
		if !updated.Equal(projectUpdated) {
			return apperrors.ErrConflict
		}
		p, err := (&Repository{db: tx}).Policy(ctx, e.ProjectID)
		if err != nil {
			return err
		}
		if p.PublishedVersion != e.PolicyVersion {
			return apperrors.ErrConflict
		}
		return addEvent(tx, e)
	})
}

func (r *Repository) Events(ctx context.Context, id string) ([]domain.Event, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT metadata FROM docker_volume_dlp_events WHERE project_id=? ORDER BY created_at DESC,id DESC LIMIT 100`, id).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domain.Event, 0)
	for rows.Next() {
		var data []byte
		var e domain.Event
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
