package apphosting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *MemoryRepository) GetRuntimeConfig(_ context.Context, appID string) (domain.RuntimeConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneRuntimeConfig(r.runtimeConfigs[strings.TrimSpace(appID)]), nil
}

func (r *MemoryRepository) WithLockedRuntimeConfig(ctx context.Context, appID string, use func(domain.App, domain.RuntimeConfig) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	app, ok := r.appsByID[strings.TrimSpace(appID)]
	if !ok {
		return apperrors.ErrNotFound
	}
	return use(cloneApp(app), cloneRuntimeConfig(r.runtimeConfigs[app.ID]))
}

func (r *MemoryRepository) MutateRuntimeConfig(ctx context.Context, appID string, mutate func(domain.App, domain.RuntimeConfig) (domain.RuntimeConfig, error)) (domain.RuntimeConfig, error) {
	var saved domain.RuntimeConfig
	err := r.WithLockedRuntimeConfig(ctx, appID, func(app domain.App, current domain.RuntimeConfig) error {
		next, err := mutate(app, current)
		if err != nil {
			return err
		}
		if next.DataWarningBytes < 0 {
			return apperrors.ErrInvalidArgument
		}
		r.runtimeConfigs[app.ID] = cloneRuntimeConfig(next)
		saved = cloneRuntimeConfig(next)
		return nil
	})
	return saved, err
}

func cloneRuntimeConfig(config domain.RuntimeConfig) domain.RuntimeConfig {
	config.Environment = maps.Clone(config.Environment)
	config.VisitorGrant.Fields = slices.Clone(config.VisitorGrant.Fields)
	if config.VisitorRequest != nil {
		request := *config.VisitorRequest
		request.Fields = slices.Clone(request.Fields)
		config.VisitorRequest = &request
	}
	config.SensitivePublisherIDs = slices.Clone(config.SensitivePublisherIDs)
	return config
}

func (r *PostgresRepository) GetRuntimeConfig(ctx context.Context, appID string) (domain.RuntimeConfig, error) {
	return loadRuntimeConfig(r.db.WithContext(ctx), strings.TrimSpace(appID))
}

func (r *PostgresRepository) WithLockedRuntimeConfig(ctx context.Context, appID string, use func(domain.App, domain.RuntimeConfig) error) error {
	return r.withLockedRuntimeConfig(ctx, appID, func(_ *gorm.DB, app domain.App, config domain.RuntimeConfig) error {
		return use(app, config)
	})
}

func (r *PostgresRepository) withLockedRuntimeConfig(ctx context.Context, appID string, use func(*gorm.DB, domain.App, domain.RuntimeConfig) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		app, err := getAppWhere(ctx, tx, "id = ?", strings.TrimSpace(appID), true)
		if err != nil {
			return err
		}
		current, err := loadRuntimeConfig(tx, app.ID)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return use(tx, app, current)
	})
}

func (r *PostgresRepository) MutateRuntimeConfig(ctx context.Context, appID string, mutate func(domain.App, domain.RuntimeConfig) (domain.RuntimeConfig, error)) (domain.RuntimeConfig, error) {
	var saved domain.RuntimeConfig
	err := r.withLockedRuntimeConfig(ctx, appID, func(tx *gorm.DB, app domain.App, current domain.RuntimeConfig) error {
		next, err := mutate(app, current)
		if err != nil {
			return err
		}
		if next.DataWarningBytes < 0 {
			return apperrors.ErrInvalidArgument
		}
		if err := persistRuntimeConfig(tx, app.ID, next); err != nil {
			return err
		}
		saved = cloneRuntimeConfig(next)
		return nil
	})
	if err != nil {
		return domain.RuntimeConfig{}, err
	}
	return saved, nil
}

func loadRuntimeConfig(db *gorm.DB, appID string) (domain.RuntimeConfig, error) {
	var config domain.RuntimeConfig
	var envJSON, grantJSON, requestJSON, publishersJSON []byte
	err := db.Raw(`
		SELECT environment_ciphertexts, data_warning_bytes, visitor_grant, visitor_request,
			sensitive_publisher_ids, sensitive_publisher_owner_id
		FROM public.hosted_app_runtime_configs WHERE app_id = ?
	`, appID).Row().Scan(&envJSON, &config.DataWarningBytes, &grantJSON, &requestJSON, &publishersJSON, &config.SensitivePublisherOwnerID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RuntimeConfig{}, nil
	}
	if err != nil {
		return domain.RuntimeConfig{}, err
	}
	for _, field := range []struct {
		raw  []byte
		dest any
	}{
		{envJSON, &config.Environment}, {grantJSON, &config.VisitorGrant},
		{requestJSON, &config.VisitorRequest}, {publishersJSON, &config.SensitivePublisherIDs},
	} {
		if len(field.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(field.raw, field.dest); err != nil {
			return domain.RuntimeConfig{}, err
		}
	}
	return config, nil
}

func persistRuntimeConfig(tx *gorm.DB, appID string, config domain.RuntimeConfig) error {
	var fields [4]string
	for i, value := range []any{config.Environment, config.VisitorGrant, config.VisitorRequest, nonNilStrings(config.SensitivePublisherIDs)} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		fields[i] = string(encoded)
	}
	return tx.Exec(`
		INSERT INTO public.hosted_app_runtime_configs (app_id, environment_ciphertexts, data_warning_bytes,
			visitor_grant, visitor_request, sensitive_publisher_ids, sensitive_publisher_owner_id)
		VALUES (?, COALESCE(NULLIF(?::jsonb, 'null'::jsonb), '{}'::jsonb), ?, ?::jsonb, NULLIF(?::jsonb, 'null'::jsonb), ?::jsonb, ?)
		ON CONFLICT (app_id) DO UPDATE SET environment_ciphertexts = EXCLUDED.environment_ciphertexts,
			data_warning_bytes = EXCLUDED.data_warning_bytes, visitor_grant = EXCLUDED.visitor_grant,
			visitor_request = EXCLUDED.visitor_request, sensitive_publisher_ids = EXCLUDED.sensitive_publisher_ids,
			sensitive_publisher_owner_id = EXCLUDED.sensitive_publisher_owner_id
	`, appID, fields[0], config.DataWarningBytes, fields[1], fields[2], fields[3], config.SensitivePublisherOwnerID).Error
}

func pruneSensitivePublishers(tx *gorm.DB, app domain.App) error {
	maintainers, err := json.Marshal(nonNilStrings(app.MaintainerUserIDs))
	if err != nil {
		return err
	}
	return tx.Exec(`
		UPDATE public.hosted_app_runtime_configs
		SET sensitive_publisher_ids = COALESCE((
			SELECT jsonb_agg(publisher.id)
			FROM jsonb_array_elements_text(sensitive_publisher_ids) AS publisher(id)
			WHERE ?::jsonb @> jsonb_build_array(publisher.id)
		), '[]'::jsonb)
		WHERE app_id = ? AND sensitive_publisher_ids <> '[]'::jsonb
	`, string(maintainers), app.ID).Error
}
