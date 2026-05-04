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
		item.BuildSources, err = r.listBuildSources(ctx, item.ID, item)
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
	item, err := scanAppRow(row)
	if err != nil {
		return domainapp.App{}, err
	}
	item.BuildSources, err = r.listBuildSources(ctx, item.ID, item)
	if err != nil {
		return domainapp.App{}, err
	}
	return item, nil
}

func (r *Repository) Create(ctx context.Context, input domainapp.UpsertInput) (domainapp.App, error) {
	now := time.Now().UTC()
	item := normalizeInput(input, now)
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return domainapp.App{}, fmt.Errorf("marshal application metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO applications (
				id, name, app_key, app_group, language, description, owner_team, repository_provider, repository_project_id,
				business_line_id, repository_path, default_branch, default_tag, build_image, build_context_dir, dockerfile_path, enabled, metadata, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.Name, item.Key, item.Group, item.Language, nullableString(item.Description), nullableString(item.OwnerTeam), nullableString(item.RepositoryProvider),
			nullableString(item.RepositoryProjectID), nullableString(item.BusinessLineID), nullableString(item.RepositoryPath), nullableString(item.DefaultBranch), nullableString(item.DefaultTag),
			nullableString(item.BuildImage), nullableString(item.BuildContextDir), nullableString(item.DockerfilePath), item.Enabled, string(metadata), item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return fmt.Errorf("create application: %w", err)
		}
		return replaceBuildSourcesTx(tx, item.ID, resolveBuildSources(item, input.BuildSources), item.CreatedAt)
	}); err != nil {
		return domainapp.App{}, err
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
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			UPDATE applications
			SET name = ?, app_key = ?, app_group = ?, language = ?, description = ?, owner_team = ?, repository_provider = ?, business_line_id = ?, repository_project_id = ?,
				repository_path = ?, default_branch = ?, default_tag = ?, build_image = ?, build_context_dir = ?, dockerfile_path = ?, enabled = ?, metadata = ?, updated_at = ?
			WHERE id = ?
		`, item.Name, item.Key, item.Group, item.Language, nullableString(item.Description), nullableString(item.OwnerTeam), nullableString(item.RepositoryProvider),
			nullableString(item.BusinessLineID),
			nullableString(item.RepositoryProjectID), nullableString(item.RepositoryPath), nullableString(item.DefaultBranch), nullableString(item.DefaultTag),
			nullableString(item.BuildImage), nullableString(item.BuildContextDir), nullableString(item.DockerfilePath), item.Enabled, string(metadata), item.UpdatedAt, item.ID)
		if result.Error != nil {
			return fmt.Errorf("update application: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return replaceBuildSourcesTx(tx, item.ID, resolveBuildSources(item, input.BuildSources), item.UpdatedAt)
	}); err != nil {
		return domainapp.App{}, err
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

func (r *Repository) listBuildSources(ctx context.Context, applicationID string, app domainapp.App) ([]domainapp.BuildSource, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, source_name, source_type, enabled, is_default, build_image, default_tag, config, created_at, updated_at
		FROM application_build_sources
		WHERE application_id = ?
		ORDER BY is_default DESC, created_at ASC
	`, applicationID).Rows()
	if err != nil {
		return nil, fmt.Errorf("query application build sources: %w", err)
	}
	defer rows.Close()

	items := make([]domainapp.BuildSource, 0)
	for rows.Next() {
		item, scanErr := scanBuildSource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if len(items) > 0 {
		return items, rows.Err()
	}
	if legacy := legacyBuildSource(app); legacy != nil {
		return []domainapp.BuildSource{*legacy}, nil
	}
	return items, rows.Err()
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

func scanBuildSource(rows *sql.Rows) (domainapp.BuildSource, error) {
	var item domainapp.BuildSource
	var sourceType string
	var buildImage sql.NullString
	var defaultTag sql.NullString
	var config []byte
	if err := rows.Scan(&item.ID, &item.Name, &sourceType, &item.Enabled, &item.IsDefault, &buildImage, &defaultTag, &config, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainapp.BuildSource{}, fmt.Errorf("scan application build source: %w", err)
	}
	item.Type = domainapp.BuildSourceType(sourceType)
	item.BuildImage = buildImage.String
	item.DefaultTag = defaultTag.String
	_ = json.Unmarshal(config, &item.Config)
	if item.Config == nil {
		item.Config = map[string]any{}
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
	item := domainapp.App{
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
	for _, source := range resolveBuildSources(item, input.BuildSources) {
		if source.IsDefault {
			item.BuildImage = strings.TrimSpace(source.BuildImage)
			item.DefaultTag = strings.TrimSpace(source.DefaultTag)
			if contextDir := strings.TrimSpace(fmt.Sprint(source.Config["contextDir"])); contextDir != "" {
				item.BuildContextDir = contextDir
			}
			if dockerfilePath := strings.TrimSpace(fmt.Sprint(source.Config["dockerfilePath"])); dockerfilePath != "" {
				item.DockerfilePath = dockerfilePath
			}
			break
		}
	}
	return item
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

func resolveBuildSources(app domainapp.App, inputs []domainapp.BuildSourceInput) []domainapp.BuildSource {
	if len(inputs) == 0 {
		if legacy := legacyBuildSource(app); legacy != nil {
			return []domainapp.BuildSource{*legacy}
		}
		return nil
	}

	now := time.Now().UTC()
	items := make([]domainapp.BuildSource, 0, len(inputs))
	defaultSeen := false
	for index, input := range inputs {
		item := domainapp.BuildSource{
			ID:         strings.TrimSpace(input.ID),
			Name:       strings.TrimSpace(input.Name),
			Type:       input.Type,
			Enabled:    input.Enabled,
			IsDefault:  input.IsDefault,
			BuildImage: strings.TrimSpace(input.BuildImage),
			DefaultTag: strings.TrimSpace(input.DefaultTag),
			Config:     input.Config,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if item.ID == "" {
			if index == 0 {
				item.ID = "default:" + app.ID
			} else {
				item.ID = fmt.Sprintf("%s:%d", app.ID, index)
			}
		}
		if item.Name == "" {
			item.Name = strings.ReplaceAll(string(item.Type), "_", " ")
		}
		if item.Type == "" {
			item.Type = domainapp.BuildSourceTypeRepoDockerfile
		}
		if item.Config == nil {
			item.Config = map[string]any{}
		}
		if item.IsDefault {
			defaultSeen = true
		}
		items = append(items, item)
	}
	if !defaultSeen && len(items) > 0 {
		items[0].IsDefault = true
	}
	return items
}

func legacyBuildSource(app domainapp.App) *domainapp.BuildSource {
	if strings.TrimSpace(app.BuildImage) == "" && strings.TrimSpace(app.DockerfilePath) == "" && strings.TrimSpace(app.BuildContextDir) == "" {
		return nil
	}
	now := time.Now().UTC()
	return &domainapp.BuildSource{
		ID:         "default:" + app.ID,
		Name:       "Repository Dockerfile",
		Type:       domainapp.BuildSourceTypeRepoDockerfile,
		Enabled:    app.Enabled,
		IsDefault:  true,
		BuildImage: strings.TrimSpace(app.BuildImage),
		DefaultTag: strings.TrimSpace(app.DefaultTag),
		Config: map[string]any{
			"contextDir":     firstNonEmpty(strings.TrimSpace(app.BuildContextDir), "."),
			"dockerfilePath": firstNonEmpty(strings.TrimSpace(app.DockerfilePath), "Dockerfile"),
			"builderKind":    "docker",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func replaceBuildSourcesTx(tx *gorm.DB, applicationID string, items []domainapp.BuildSource, now time.Time) error {
	if err := tx.Exec(`DELETE FROM application_build_sources WHERE application_id = ?`, applicationID).Error; err != nil {
		return fmt.Errorf("delete application build sources: %w", err)
	}
	for _, item := range items {
		config, err := json.Marshal(item.Config)
		if err != nil {
			return fmt.Errorf("marshal application build source config: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO application_build_sources (id, application_id, source_name, source_type, enabled, is_default, build_image, default_tag, config, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, applicationID, item.Name, string(item.Type), item.Enabled, item.IsDefault, nullableString(item.BuildImage), nullableString(item.DefaultTag), string(config), now, now).Error; err != nil {
			return fmt.Errorf("create application build source: %w", err)
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
