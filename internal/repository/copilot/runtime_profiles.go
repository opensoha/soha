package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const runtimeProfileColumns = `profile_id, revision, scope, enabled, name, assets, model_preferences, max_output_tokens`

func (r *Repository) PublishRuntimeProfile(ctx context.Context, actor string, expectedRevision int64, profile domain.RuntimeProfile) (domain.RuntimeProfile, error) {
	assets, err := json.Marshal(profile.Assets)
	if err != nil {
		return domain.RuntimeProfile{}, fmt.Errorf("marshal runtime profile assets: %w", err)
	}
	preferences, err := json.Marshal(profile.ModelPreferences)
	if err != nil {
		return domain.RuntimeProfile{}, fmt.Errorf("marshal runtime profile model preferences: %w", err)
	}
	// The revision key arbitrates writers that saw the same head, including concurrent creates.
	row := r.db.WithContext(ctx).Raw(`
		WITH head AS (
			SELECT revision, scope FROM ai_workbench_runtime_profile_revisions
			WHERE profile_id = ? ORDER BY revision DESC LIMIT 1
		)
		INSERT INTO ai_workbench_runtime_profile_revisions (`+runtimeProfileColumns+`, published_by)
		SELECT ?, ?::bigint + 1, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?
		WHERE COALESCE((SELECT revision FROM head), 0) = ?
		AND NOT EXISTS (SELECT 1 FROM head WHERE scope <> ?)
		ON CONFLICT (profile_id, revision) DO NOTHING
		RETURNING `+runtimeProfileColumns,
		profile.ID, profile.ID, expectedRevision, profile.Scope, profile.Enabled, profile.Name, string(assets), string(preferences),
		profile.MaxOutputTokens, actor, expectedRevision, profile.Scope).Row()
	published, err := scanRuntimeProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RuntimeProfile{}, apperrors.NewBusiness(apperrors.ErrConflict, "runtime_profile_revision_conflict", "The runtime profile revision or scope has changed.", "运行配置版本或作用域已变化。")
	}
	return published, err
}

func (r *Repository) ListRuntimeProfileRevisions(ctx context.Context, id string) ([]domain.RuntimeProfile, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT `+runtimeProfileColumns+` FROM ai_workbench_runtime_profile_revisions
		WHERE profile_id = ? ORDER BY revision DESC`, id).Rows()
	return scanRuntimeProfiles(rows, err)
}

func (r *Repository) ListCurrentRuntimeProfiles(ctx context.Context, scopes []string) ([]domain.RuntimeProfile, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT `+runtimeProfileColumns+` FROM (
			SELECT DISTINCT ON (profile_id) `+runtimeProfileColumns+` FROM ai_workbench_runtime_profile_revisions
			WHERE scope IN ? ORDER BY profile_id, revision DESC
		) AS heads WHERE enabled ORDER BY profile_id`, scopes).Rows()
	return scanRuntimeProfiles(rows, err)
}

func (r *Repository) GetCurrentRuntimeProfile(ctx context.Context, id, scope string) (domain.RuntimeProfile, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT `+runtimeProfileColumns+` FROM (
			SELECT `+runtimeProfileColumns+` FROM ai_workbench_runtime_profile_revisions
			WHERE profile_id = ? AND scope = ? ORDER BY revision DESC LIMIT 1
		) AS head WHERE enabled`, id, scope).Row()
	item, err := scanRuntimeProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RuntimeProfile{}, apperrors.ErrNotFound
	}
	return item, err
}

func (r *Repository) RuntimeProfileRevisionAuthorized(ctx context.Context, id string, revision int64, scope string) (bool, error) {
	var allowed bool
	err := r.db.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1 FROM ai_workbench_runtime_profile_revisions
			WHERE profile_id = ? AND revision = ? AND scope = ? AND enabled
		) AND NOT EXISTS (
			SELECT 1 FROM ai_workbench_runtime_profile_revisions
			WHERE profile_id = ? AND revision > ? AND NOT enabled
		)`, id, revision, scope, id, revision).Row().Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("authorize runtime profile revision: %w", err)
	}
	return allowed, nil
}

func (r *Repository) SelectSessionRuntimeProfile(ctx context.Context, owner, sessionID, profileID string) (domain.Session, error) {
	row := r.db.WithContext(ctx).Raw(`
		UPDATE ai_sessions SET metadata = (
			CASE WHEN ? = '' THEN COALESCE(NULLIF(metadata::jsonb, 'null'::jsonb), '{}'::jsonb) - 'runtimeProfileId'
			ELSE jsonb_set(COALESCE(NULLIF(metadata::jsonb, 'null'::jsonb), '{}'::jsonb), '{runtimeProfileId}', to_jsonb(?::text)) END
		)::json, updated_at = now()
		WHERE created_by = ? AND id = ? AND deleted_at IS NULL
		AND COALESCE(metadata->>'status', '') IN ('', 'active')
		AND COALESCE(metadata->>'archivedAt', '') = ''
		RETURNING id, title, created_by, metadata, created_at, updated_at`, profileID, profileID, owner, sessionID).Row()
	return scanSessionRow(row, sessionID)
}

func scanRuntimeProfiles(rows *sql.Rows, err error) ([]domain.RuntimeProfile, error) {
	if err != nil {
		return nil, fmt.Errorf("query runtime profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domain.RuntimeProfile, 0)
	for rows.Next() {
		item, err := scanRuntimeProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanRuntimeProfile(row interface{ Scan(...any) error }) (domain.RuntimeProfile, error) {
	var item domain.RuntimeProfile
	var assets, preferences []byte
	if err := row.Scan(&item.ID, &item.Revision, &item.Scope, &item.Enabled, &item.Name, &assets, &preferences, &item.MaxOutputTokens); err != nil {
		return domain.RuntimeProfile{}, fmt.Errorf("scan runtime profile: %w", err)
	}
	if err := json.Unmarshal(assets, &item.Assets); err != nil {
		return domain.RuntimeProfile{}, fmt.Errorf("decode runtime profile assets: %w", err)
	}
	if err := json.Unmarshal(preferences, &item.ModelPreferences); err != nil {
		return domain.RuntimeProfile{}, fmt.Errorf("decode runtime profile model preferences: %w", err)
	}
	return item, nil
}
