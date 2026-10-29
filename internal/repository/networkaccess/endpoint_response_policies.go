package networkaccess

import (
	"context"
	"encoding/json"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const responsePolicyColumns = `id, revision, published_revision, configuration, published_configuration, created_at, updated_at, published_at`
const responsePolicyLock = "network-endpoint-response-policies:default:default"

func scanResponsePolicy(row scanner) (domain.EndpointResponsePolicy, error) {
	var item domain.EndpointResponsePolicy
	var draft, published []byte
	if err := row.Scan(&item.ID, &item.Revision, &item.PublishedRevision, &draft, &published, &item.CreatedAt, &item.UpdatedAt, &item.PublishedAt); err != nil {
		return item, err
	}
	if err := json.Unmarshal(draft, &item.Configuration); err != nil {
		return item, err
	}
	if len(published) > 0 {
		if err := json.Unmarshal(published, &item.PublishedConfiguration); err != nil {
			return item, err
		}
	}
	return item, nil
}

func (r *Repository) EndpointResponsePolicy(ctx context.Context, id string) (domain.EndpointResponsePolicy, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+responsePolicyColumns+` FROM network_endpoint_response_policies WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND deleted_at IS NULL`, id).Row(), scanResponsePolicy, "endpoint response policy")
}

func (r *Repository) EndpointResponsePolicies(ctx context.Context, filter domain.EndpointResponseFilter) ([]domain.EndpointResponsePolicy, error) {
	query := `SELECT ` + responsePolicyColumns + ` FROM network_endpoint_response_policies WHERE tenant_id = 'default' AND workspace_id = 'default' AND deleted_at IS NULL`
	args := []any{}
	if filter.SiteID != "" {
		query += ` AND (jsonb_exists(configuration->'siteIds', ?) OR jsonb_exists(published_configuration->'siteIds', ?))`
		args = append(args, filter.SiteID, filter.SiteID)
	}
	if filter.DeviceID != "" {
		query += ` AND (configuration->'deviceIds' = '[]'::jsonb OR jsonb_exists(configuration->'deviceIds', ?) OR published_configuration->'deviceIds' = '[]'::jsonb OR jsonb_exists(published_configuration->'deviceIds', ?))`
		args = append(args, filter.DeviceID, filter.DeviceID)
	}
	query += ` ORDER BY id LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)
	return queryMany(ctx, r.db, query, args, scanResponsePolicy)
}

func (r *Repository) PublishedEndpointResponsePolicies(ctx context.Context) ([]domain.EndpointResponsePolicy, error) {
	items, err := queryMany(ctx, r.db, `SELECT `+responsePolicyColumns+` FROM network_endpoint_response_policies WHERE tenant_id = 'default' AND workspace_id = 'default' AND deleted_at IS NULL AND published_revision > 0 AND published_configuration->>'enabled' = 'true' ORDER BY id LIMIT 1001`, nil, scanResponsePolicy)
	if err == nil && len(items) > 1000 {
		return nil, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "endpoint_response_policy_limit", "The active endpoint response policy set exceeds the bounded evaluator.", "活动终端联动策略超过评估上限。")
	}
	return items, err
}

func (r *Repository) SaveEndpointResponsePolicy(ctx context.Context, id string, expected int, config domain.EndpointResponsePolicyConfig, actor string, now time.Time) (domain.EndpointResponsePolicy, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return domain.EndpointResponsePolicy{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, responsePolicyLock).Error; err != nil {
			return err
		}
		if expected == 0 {
			if err := tx.Exec(`INSERT INTO network_endpoint_response_policies (id, revision, configuration, created_at, updated_at) VALUES (?, 1, ?::jsonb, ?, ?)`, id, string(raw), now, now).Error; err != nil {
				return normalizeDatabaseError(err)
			}
		} else if err := responseRevisionMutation(tx.Exec(`UPDATE network_endpoint_response_policies SET revision = revision + 1, configuration = ?::jsonb, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND revision = ? AND deleted_at IS NULL`, string(raw), now, id, expected)); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO network_endpoint_response_policy_revisions (policy_id, revision, configuration, actor_id, created_at) VALUES (?, ?, ?::jsonb, ?, ?)`, id, expected+1, string(raw), actor, now).Error
	})
	if err != nil {
		return domain.EndpointResponsePolicy{}, err
	}
	return r.EndpointResponsePolicy(ctx, id)
}

func (r *Repository) PublishEndpointResponsePolicy(ctx context.Context, id string, expected, target int, actor string, now time.Time) (domain.EndpointResponsePolicy, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, responsePolicyLock).Error; err != nil {
			return err
		}
		item, err := scanOne(tx.Raw(`SELECT `+responsePolicyColumns+` FROM network_endpoint_response_policies WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND deleted_at IS NULL FOR UPDATE`, id).Row(), scanResponsePolicy, "endpoint response policy")
		if err != nil {
			return err
		}
		if item.Revision != expected {
			return responseRevisionConflict()
		}
		config, revision := item.Configuration, item.Revision
		if target != 0 {
			history, err := scanResponsePolicyRevision(tx.Raw(`SELECT revision, configuration, actor_id, created_at FROM network_endpoint_response_policy_revisions WHERE tenant_id = 'default' AND workspace_id = 'default' AND policy_id = ? AND revision = ?`, id, target).Row())
			if err != nil {
				return err
			}
			config, revision = history.Configuration, item.Revision+1
		}
		raw, err := json.Marshal(config)
		if err != nil {
			return err
		}
		if target != 0 {
			if err := tx.Exec(`INSERT INTO network_endpoint_response_policy_revisions (policy_id, revision, configuration, actor_id, created_at) VALUES (?, ?, ?::jsonb, ?, ?)`, id, revision, string(raw), actor, now).Error; err != nil {
				return err
			}
		}
		return tx.Exec(`UPDATE network_endpoint_response_policies SET revision = ?, configuration = ?::jsonb, published_revision = ?, published_configuration = ?::jsonb, published_at = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, revision, string(raw), revision, string(raw), now, now, id).Error
	})
	if err != nil {
		return domain.EndpointResponsePolicy{}, err
	}
	return r.EndpointResponsePolicy(ctx, id)
}

func (r *Repository) DeleteEndpointResponsePolicy(ctx context.Context, id string, expected int, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, responsePolicyLock).Error; err != nil {
			return err
		}
		return responseRevisionMutation(tx.Exec(`UPDATE network_endpoint_response_policies SET deleted_at = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND revision = ? AND deleted_at IS NULL`, now, now, id, expected))
	})
}

func (r *Repository) EndpointResponsePolicyRevision(ctx context.Context, id string, revision int) (domain.EndpointResponsePolicyRevision, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT revision, configuration, actor_id, created_at FROM network_endpoint_response_policy_revisions WHERE tenant_id = 'default' AND workspace_id = 'default' AND policy_id = ? AND revision = ?`, id, revision).Row(), scanResponsePolicyRevision, "endpoint response policy revision")
}

func (r *Repository) EndpointResponsePolicyRevisions(ctx context.Context, id string) ([]domain.EndpointResponsePolicyRevision, error) {
	return queryMany(ctx, r.db, `SELECT revision, configuration, actor_id, created_at FROM network_endpoint_response_policy_revisions WHERE tenant_id = 'default' AND workspace_id = 'default' AND policy_id = ? ORDER BY revision DESC LIMIT 100`, []any{id}, scanResponsePolicyRevision)
}

func scanResponsePolicyRevision(row scanner) (domain.EndpointResponsePolicyRevision, error) {
	var item domain.EndpointResponsePolicyRevision
	var raw []byte
	if err := row.Scan(&item.Revision, &raw, &item.ActorID, &item.CreatedAt); err != nil {
		return item, err
	}
	return item, json.Unmarshal(raw, &item.Configuration)
}

func responseRevisionMutation(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return responseRevisionConflict()
	}
	return nil
}

func responseRevisionConflict() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "endpoint_response_revision_conflict", "The endpoint response changed. Reload it before retrying.", "终端联动配置已变化，请刷新后重试。")
}
