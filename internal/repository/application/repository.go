package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("application not found")

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, filter domainapp.Filter) ([]domainapp.App, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, name, app_key, app_group, business_line_id, language, description, owner_team, repository_provider,
			repository_project_id, repository_path, default_branch, default_tag, build_image, build_context_dir,
			dockerfile_path, enabled, metadata, created_at, updated_at
		FROM applications
	`
	args := []any{}
	if search := strings.TrimSpace(filter.Search); search != "" {
		query += ` WHERE LOWER(name) LIKE ? OR LOWER(app_key) LIKE ? OR LOWER(app_group) LIKE ? OR LOWER(repository_path) LIKE ?`
		pattern := "%" + strings.ToLower(search) + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	query += ` ORDER BY app_group ASC, name ASC, id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query applications: %w", err)
	}
	defer rows.Close()

	items := make([]domainapp.App, 0, limit)
	for rows.Next() {
		item, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, applicationID string) (domainapp.App, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, app_key, app_group, business_line_id, language, description, owner_team, repository_provider,
			repository_project_id, repository_path, default_branch, default_tag, build_image, build_context_dir,
			dockerfile_path, enabled, metadata, created_at, updated_at
		FROM applications
		WHERE id = ?
		LIMIT 1
	`, applicationID).Row()
	return scanAppRow(row)
}

func (r *Repository) Create(ctx context.Context, input domainapp.UpsertInput) (domainapp.App, error) {
	now := time.Now().UTC()
	item := normalizeInput(input, now)
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return domainapp.App{}, fmt.Errorf("marshal application metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO applications (
			id, name, app_key, app_group, language, description, owner_team, repository_provider, repository_project_id,
			business_line_id, repository_path, default_branch, default_tag, build_image, build_context_dir, dockerfile_path, enabled, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.Key, item.Group, item.Language, nullableString(item.Description), nullableString(item.OwnerTeam), nullableString(item.RepositoryProvider),
		nullableString(item.RepositoryProjectID), nullableString(item.BusinessLineID), nullableString(item.RepositoryPath), nullableString(item.DefaultBranch), nullableString(item.DefaultTag),
		nullableString(item.BuildImage), nullableString(item.BuildContextDir), nullableString(item.DockerfilePath), item.Enabled, string(metadata), item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainapp.App{}, fmt.Errorf("create application: %w", err)
	}
	return item, nil
}

func (r *Repository) Update(ctx context.Context, applicationID string, input domainapp.UpsertInput) (domainapp.App, error) {
	now := time.Now().UTC()
	item := normalizeInput(input, now)
	item.ID = strings.TrimSpace(applicationID)
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return domainapp.App{}, fmt.Errorf("marshal application metadata: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE applications
		SET name = ?, app_key = ?, app_group = ?, language = ?, description = ?, owner_team = ?, repository_provider = ?, business_line_id = ?, repository_project_id = ?,
			repository_path = ?, default_branch = ?, default_tag = ?, build_image = ?, build_context_dir = ?, dockerfile_path = ?, enabled = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, item.Key, item.Group, item.Language, nullableString(item.Description), nullableString(item.OwnerTeam), nullableString(item.RepositoryProvider),
		nullableString(item.BusinessLineID),
		nullableString(item.RepositoryProjectID), nullableString(item.RepositoryPath), nullableString(item.DefaultBranch), nullableString(item.DefaultTag),
		nullableString(item.BuildImage), nullableString(item.BuildContextDir), nullableString(item.DockerfilePath), item.Enabled, string(metadata), item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainapp.App{}, fmt.Errorf("update application: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainapp.App{}, ErrNotFound
	}
	createdAt := fetchCreatedAt(ctx, r.db, item.ID)
	if !createdAt.IsZero() {
		item.CreatedAt = createdAt
	}
	return item, nil
}

func (r *Repository) Delete(ctx context.Context, applicationID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM applications WHERE id = ?`, strings.TrimSpace(applicationID))
	if result.Error != nil {
		return fmt.Errorf("delete application: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func scanApp(rows *sql.Rows) (domainapp.App, error) {
	var item domainapp.App
	var businessLineID sql.NullString
	var description sql.NullString
	var ownerTeam sql.NullString
	var repositoryProvider sql.NullString
	var repositoryProjectID sql.NullString
	var repositoryPath sql.NullString
	var defaultBranch sql.NullString
	var defaultTag sql.NullString
	var buildImage sql.NullString
	var buildContextDir sql.NullString
	var dockerfilePath sql.NullString
	var metadata []byte
	if err := rows.Scan(&item.ID, &item.Name, &item.Key, &item.Group, &businessLineID, &item.Language, &description, &ownerTeam, &repositoryProvider, &repositoryProjectID,
		&repositoryPath, &defaultBranch, &defaultTag, &buildImage, &buildContextDir, &dockerfilePath, &item.Enabled, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainapp.App{}, fmt.Errorf("scan application: %w", err)
	}
	item.BusinessLineID = businessLineID.String
	decodeStrings(&item, description, ownerTeam, repositoryProvider, repositoryProjectID, repositoryPath, defaultBranch, defaultTag, buildImage, buildContextDir, dockerfilePath)
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanAppRow(row *sql.Row) (domainapp.App, error) {
	var item domainapp.App
	var businessLineID sql.NullString
	var description sql.NullString
	var ownerTeam sql.NullString
	var repositoryProvider sql.NullString
	var repositoryProjectID sql.NullString
	var repositoryPath sql.NullString
	var defaultBranch sql.NullString
	var defaultTag sql.NullString
	var buildImage sql.NullString
	var buildContextDir sql.NullString
	var dockerfilePath sql.NullString
	var metadata []byte
	if err := row.Scan(&item.ID, &item.Name, &item.Key, &item.Group, &businessLineID, &item.Language, &description, &ownerTeam, &repositoryProvider, &repositoryProjectID,
		&repositoryPath, &defaultBranch, &defaultTag, &buildImage, &buildContextDir, &dockerfilePath, &item.Enabled, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainapp.App{}, ErrNotFound
		}
		return domainapp.App{}, fmt.Errorf("scan application row: %w", err)
	}
	item.BusinessLineID = businessLineID.String
	decodeStrings(&item, description, ownerTeam, repositoryProvider, repositoryProjectID, repositoryPath, defaultBranch, defaultTag, buildImage, buildContextDir, dockerfilePath)
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func decodeStrings(item *domainapp.App, description, ownerTeam, repositoryProvider, repositoryProjectID, repositoryPath, defaultBranch, defaultTag, buildImage, buildContextDir, dockerfilePath sql.NullString) {
	item.Description = description.String
	item.OwnerTeam = ownerTeam.String
	item.RepositoryProvider = repositoryProvider.String
	item.RepositoryProjectID = repositoryProjectID.String
	item.RepositoryPath = repositoryPath.String
	item.DefaultBranch = defaultBranch.String
	item.DefaultTag = defaultTag.String
	item.BuildImage = buildImage.String
	item.BuildContextDir = buildContextDir.String
	item.DockerfilePath = dockerfilePath.String
}

func normalizeInput(input domainapp.UpsertInput, now time.Time) domainapp.App {
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(input.Key), "_", "-"))
	}
	return domainapp.App{
		ID:                  id,
		Name:                strings.TrimSpace(input.Name),
		Key:                 strings.TrimSpace(input.Key),
		Group:               strings.TrimSpace(input.Group),
		BusinessLineID:      strings.TrimSpace(input.BusinessLineID),
		Language:            strings.TrimSpace(input.Language),
		Description:         strings.TrimSpace(input.Description),
		OwnerTeam:           strings.TrimSpace(input.OwnerTeam),
		RepositoryProvider:  strings.TrimSpace(input.RepositoryProvider),
		RepositoryProjectID: strings.TrimSpace(input.RepositoryProjectID),
		RepositoryPath:      strings.TrimSpace(input.RepositoryPath),
		DefaultBranch:       strings.TrimSpace(input.DefaultBranch),
		DefaultTag:          strings.TrimSpace(input.DefaultTag),
		BuildImage:          strings.TrimSpace(input.BuildImage),
		BuildContextDir:     strings.TrimSpace(input.BuildContextDir),
		DockerfilePath:      strings.TrimSpace(input.DockerfilePath),
		Enabled:             input.Enabled,
		Metadata:            metadata,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func fetchCreatedAt(ctx context.Context, db *gorm.DB, applicationID string) time.Time {
	var createdAt time.Time
	if err := db.WithContext(ctx).Raw(`SELECT created_at FROM applications WHERE id = ?`, applicationID).Row().Scan(&createdAt); err != nil {
		return time.Time{}
	}
	return createdAt
}
