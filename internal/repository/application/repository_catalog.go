package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	"gorm.io/gorm"
)

func (r *Repository) ListRepositories(ctx context.Context, filter domainapp.SourceRepositoryFilter) ([]domainapp.SourceRepository, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, name, provider, url, protocol, gitlab_project_id, path, credential_ref, default_branch, created_at, updated_at FROM repositories`
	args := []any{}
	where := []string{}
	if filter.ApplicationID != "" {
		where = append(where, `id IN (SELECT repository_id FROM application_repositories WHERE application_id = ?)`)
		args = append(args, filter.ApplicationID)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		where = append(where, `(LOWER(name) LIKE ? OR LOWER(path) LIKE ? OR LOWER(url) LIKE ?)`)
		pattern := "%" + strings.ToLower(search) + "%"
		args = append(args, pattern, pattern, pattern)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY name ASC, id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query repositories: %w", err)
	}
	defer rows.Close()
	items := make([]domainapp.SourceRepository, 0, limit)
	for rows.Next() {
		var item domainapp.SourceRepository
		var projectID, path, credential, branch *string
		if err := rows.Scan(&item.ID, &item.Name, &item.Provider, &item.URL, &item.Protocol, &projectID, &path, &credential, &branch, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan repository: %w", err)
		}
		item.GitLabProjectID, item.Path, item.CredentialRef, item.DefaultBranch = value(projectID), value(path), value(credential), value(branch)
		item.ApplicationIDs, _ = r.listRepositoryApplicationIDs(ctx, item.ID)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetRepository(ctx context.Context, id string) (domainapp.SourceRepository, error) {
	var item domainapp.SourceRepository
	var projectID, path, credential, branch *string
	err := r.db.WithContext(ctx).Raw(`SELECT id,name,provider,url,protocol,gitlab_project_id,path,credential_ref,default_branch,created_at,updated_at FROM repositories WHERE id=? LIMIT 1`, strings.TrimSpace(id)).Row().Scan(&item.ID, &item.Name, &item.Provider, &item.URL, &item.Protocol, &projectID, &path, &credential, &branch, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return domainapp.SourceRepository{}, ErrNotFound
	}
	item.GitLabProjectID, item.Path, item.CredentialRef, item.DefaultBranch = value(projectID), value(path), value(credential), value(branch)
	item.ApplicationIDs, _ = r.listRepositoryApplicationIDs(ctx, item.ID)
	return item, nil
}

func (r *Repository) CreateRepository(ctx context.Context, input domainapp.SourceRepositoryInput) (domainapp.SourceRepository, error) {
	now := time.Now().UTC()
	item := sourceRepositoryFromInput(input, now)
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO repositories (id,name,provider,url,protocol,gitlab_project_id,path,credential_ref,default_branch,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Name, item.Provider, item.URL, item.Protocol, nullableString(item.GitLabProjectID), nullableString(item.Path), nullableString(item.CredentialRef), nullableString(item.DefaultBranch), item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return err
		}
		return replaceRepositoryApplicationsTx(tx, item.ID, input.ApplicationIDs)
	}); err != nil {
		return domainapp.SourceRepository{}, fmt.Errorf("create repository: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateRepository(ctx context.Context, id string, input domainapp.SourceRepositoryInput) (domainapp.SourceRepository, error) {
	now := time.Now().UTC()
	item := sourceRepositoryFromInput(input, now)
	item.ID = strings.TrimSpace(id)
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`UPDATE repositories SET name=?,provider=?,url=?,protocol=?,gitlab_project_id=?,path=?,credential_ref=?,default_branch=?,updated_at=? WHERE id=?`, item.Name, item.Provider, item.URL, item.Protocol, nullableString(item.GitLabProjectID), nullableString(item.Path), nullableString(item.CredentialRef), nullableString(item.DefaultBranch), item.UpdatedAt, item.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return replaceRepositoryApplicationsTx(tx, item.ID, input.ApplicationIDs)
	}); err != nil {
		return domainapp.SourceRepository{}, fmt.Errorf("update repository: %w", err)
	}
	return item, nil
}

func (r *Repository) DeleteRepository(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM repositories WHERE id=?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete repository: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func sourceRepositoryFromInput(input domainapp.SourceRepositoryInput, now time.Time) domainapp.SourceRepository {
	return domainapp.SourceRepository{Name: strings.TrimSpace(input.Name), Provider: strings.ToLower(strings.TrimSpace(input.Provider)), URL: strings.TrimSpace(input.URL), Protocol: strings.ToLower(strings.TrimSpace(input.Protocol)), GitLabProjectID: strings.TrimSpace(input.GitLabProjectID), Path: strings.TrimSpace(input.Path), CredentialRef: strings.TrimSpace(input.CredentialRef), DefaultBranch: strings.TrimSpace(input.DefaultBranch), ApplicationIDs: input.ApplicationIDs, CreatedAt: now, UpdatedAt: now}
}
func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (r *Repository) listRepositoryApplicationIDs(ctx context.Context, repositoryID string) ([]string, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT application_id FROM application_repositories WHERE repository_id = ? ORDER BY application_id`, repositoryID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func replaceRepositoryApplicationsTx(tx *gorm.DB, repositoryID string, applicationIDs []string) error {
	if err := tx.Exec(`DELETE FROM application_repositories WHERE repository_id = ?`, repositoryID).Error; err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, raw := range applicationIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if err := tx.Exec(`INSERT INTO application_repositories (application_id, repository_id) VALUES (?, ?)`, id, repositoryID).Error; err != nil {
			return err
		}
	}
	return nil
}
