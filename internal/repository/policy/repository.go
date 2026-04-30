package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListPolicies(ctx context.Context) ([]domainaccess.Policy, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, effect, priority, subjects, targets, actions, conditions, reason
		FROM policies
		ORDER BY priority DESC, id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	policies := make([]domainaccess.Policy, 0)
	for rows.Next() {
		policy, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (r *Repository) ListRoleCapabilities(ctx context.Context) (map[string][]domainaccess.Action, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, capabilities
		FROM roles
		ORDER BY id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	matrix := map[string][]domainaccess.Action{}
	for rows.Next() {
		var roleID string
		var capabilities []byte
		if err := rows.Scan(&roleID, &capabilities); err != nil {
			return nil, err
		}
		var actions []domainaccess.Action
		if len(capabilities) > 0 {
			if err := json.Unmarshal(capabilities, &actions); err != nil {
				return nil, fmt.Errorf("unmarshal role capabilities for %s: %w", roleID, err)
			}
		}
		matrix[roleID] = actions
	}
	return matrix, rows.Err()
}

func (r *Repository) ListRolePermissions(ctx context.Context) (map[string][]string, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, permission_keys
		FROM roles
		ORDER BY id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	matrix := map[string][]string{}
	for rows.Next() {
		var roleID string
		var permissionKeys []byte
		if err := rows.Scan(&roleID, &permissionKeys); err != nil {
			return nil, err
		}
		var keys []string
		if len(permissionKeys) > 0 {
			if err := json.Unmarshal(permissionKeys, &keys); err != nil {
				return nil, fmt.Errorf("unmarshal role permission keys for %s: %w", roleID, err)
			}
		}
		matrix[roleID] = keys
	}
	return matrix, rows.Err()
}

func (r *Repository) ListRoles(ctx context.Context) ([]domainaccess.RoleRecord, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT
			r.id,
			r.name,
			r.scope,
			r.capabilities,
			r.permission_keys,
			COUNT(DISTINCT urb.user_id) AS user_count
		FROM roles r
		LEFT JOIN user_role_bindings urb ON urb.role_id = r.id
		GROUP BY r.id, r.name, r.scope, r.capabilities, r.permission_keys
		ORDER BY r.name ASC, r.id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domainaccess.RoleRecord, 0)
	for rows.Next() {
		var item domainaccess.RoleRecord
		var capabilities []byte
		var permissionKeys []byte
		if err := rows.Scan(&item.ID, &item.Name, &item.Scope, &capabilities, &permissionKeys, &item.UserCount); err != nil {
			return nil, err
		}
		if len(capabilities) > 0 {
			if err := json.Unmarshal(capabilities, &item.Capabilities); err != nil {
				return nil, fmt.Errorf("unmarshal role capabilities for %s: %w", item.ID, err)
			}
		}
		if len(permissionKeys) > 0 {
			if err := json.Unmarshal(permissionKeys, &item.PermissionKeys); err != nil {
				return nil, fmt.Errorf("unmarshal role permission keys for %s: %w", item.ID, err)
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateRole(ctx context.Context, input domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	now := time.Now().UTC()
	capabilities, err := json.Marshal(input.Capabilities)
	if err != nil {
		return domainaccess.RoleRecord{}, fmt.Errorf("marshal role capabilities: %w", err)
	}
	permissionKeys, err := json.Marshal(input.PermissionKeys)
	if err != nil {
		return domainaccess.RoleRecord{}, fmt.Errorf("marshal role permission keys: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO roles (id, name, scope, capabilities, permission_keys, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, strings.TrimSpace(input.ID), strings.TrimSpace(input.Name), strings.TrimSpace(input.Scope), string(capabilities), string(permissionKeys), now, now).Error; err != nil {
		return domainaccess.RoleRecord{}, err
	}
	return domainaccess.RoleRecord{
		ID:             strings.TrimSpace(input.ID),
		Name:           strings.TrimSpace(input.Name),
		Scope:          strings.TrimSpace(input.Scope),
		Capabilities:   input.Capabilities,
		PermissionKeys: input.PermissionKeys,
		UserCount:      0,
	}, nil
}

func (r *Repository) UpdateRole(ctx context.Context, roleID string, input domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	capabilities, err := json.Marshal(input.Capabilities)
	if err != nil {
		return domainaccess.RoleRecord{}, fmt.Errorf("marshal role capabilities: %w", err)
	}
	permissionKeys, err := json.Marshal(input.PermissionKeys)
	if err != nil {
		return domainaccess.RoleRecord{}, fmt.Errorf("marshal role permission keys: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE roles
		SET name = ?, scope = ?, capabilities = ?, permission_keys = ?, updated_at = ?
		WHERE id = ?
	`, strings.TrimSpace(input.Name), strings.TrimSpace(input.Scope), string(capabilities), string(permissionKeys), time.Now().UTC(), strings.TrimSpace(roleID))
	if result.Error != nil {
		return domainaccess.RoleRecord{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainaccess.RoleRecord{}, gorm.ErrRecordNotFound
	}
	items, err := r.ListRoles(ctx)
	if err != nil {
		return domainaccess.RoleRecord{}, err
	}
	for _, item := range items {
		if item.ID == strings.TrimSpace(roleID) {
			return item, nil
		}
	}
	return domainaccess.RoleRecord{}, gorm.ErrRecordNotFound
}

func (r *Repository) DeleteRole(ctx context.Context, roleID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM roles WHERE id = ?`, strings.TrimSpace(roleID))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) CreatePolicy(ctx context.Context, input domainaccess.PolicyInput) (domainaccess.Policy, error) {
	return r.upsertPolicy(ctx, strings.TrimSpace(input.ID), input, true)
}

func (r *Repository) UpdatePolicy(ctx context.Context, policyID string, input domainaccess.PolicyInput) (domainaccess.Policy, error) {
	return r.upsertPolicy(ctx, strings.TrimSpace(policyID), input, false)
}

func (r *Repository) DeletePolicy(ctx context.Context, policyID string) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := tx.Exec(`DELETE FROM policy_bindings WHERE policy_id = ?`, strings.TrimSpace(policyID)).Error; err != nil {
		tx.Rollback()
		return err
	}
	result := tx.Exec(`DELETE FROM policies WHERE id = ?`, strings.TrimSpace(policyID))
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		return gorm.ErrRecordNotFound
	}
	return tx.Commit().Error
}

func (r *Repository) upsertPolicy(ctx context.Context, policyID string, input domainaccess.PolicyInput, create bool) (domainaccess.Policy, error) {
	subjects, err := json.Marshal(input.Subjects)
	if err != nil {
		return domainaccess.Policy{}, fmt.Errorf("marshal policy subjects: %w", err)
	}
	targets, err := json.Marshal(map[string]any{
		"clusters":   input.Clusters,
		"namespaces": input.Namespaces,
		"resources":  input.Resources,
	})
	if err != nil {
		return domainaccess.Policy{}, fmt.Errorf("marshal policy targets: %w", err)
	}
	actions, err := json.Marshal(input.Actions)
	if err != nil {
		return domainaccess.Policy{}, fmt.Errorf("marshal policy actions: %w", err)
	}
	conditions, err := json.Marshal(input.Conditions)
	if err != nil {
		return domainaccess.Policy{}, fmt.Errorf("marshal policy conditions: %w", err)
	}

	now := time.Now().UTC()
	if create {
		if err := r.db.WithContext(ctx).Exec(`
			INSERT INTO policies (id, name, effect, priority, subjects, targets, actions, conditions, reason, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, policyID, strings.TrimSpace(input.Name), string(input.Effect), input.Priority, string(subjects), string(targets), string(actions), string(conditions), strings.TrimSpace(input.Reason), now, now).Error; err != nil {
			return domainaccess.Policy{}, err
		}
	} else {
		result := r.db.WithContext(ctx).Exec(`
			UPDATE policies
			SET name = ?, effect = ?, priority = ?, subjects = ?, targets = ?, actions = ?, conditions = ?, reason = ?, updated_at = ?
			WHERE id = ?
		`, strings.TrimSpace(input.Name), string(input.Effect), input.Priority, string(subjects), string(targets), string(actions), string(conditions), strings.TrimSpace(input.Reason), now, policyID)
		if result.Error != nil {
			return domainaccess.Policy{}, result.Error
		}
		if result.RowsAffected == 0 {
			return domainaccess.Policy{}, gorm.ErrRecordNotFound
		}
	}

	items, err := r.ListPolicies(ctx)
	if err != nil {
		return domainaccess.Policy{}, err
	}
	for _, item := range items {
		if item.ID == policyID {
			return item, nil
		}
	}
	return domainaccess.Policy{}, gorm.ErrRecordNotFound
}

func scanPolicy(rows *sql.Rows) (domainaccess.Policy, error) {
	var policy domainaccess.Policy
	var effect string
	var subjects []byte
	var targets []byte
	var actions []byte
	var conditions []byte
	if err := rows.Scan(&policy.ID, &policy.Name, &effect, &policy.Priority, &subjects, &targets, &actions, &conditions, &policy.Reason); err != nil {
		return domainaccess.Policy{}, err
	}
	policy.Effect = domainaccess.PolicyEffect(effect)
	_ = json.Unmarshal(subjects, &policy.Subjects)
	_ = json.Unmarshal(actions, &policy.Actions)
	_ = json.Unmarshal(conditions, &policy.Conditions)
	var targetEnvelope struct {
		Clusters   domainaccess.ClusterMatcher   `json:"clusters"`
		Namespaces domainaccess.NamespaceMatcher `json:"namespaces"`
		Resources  domainaccess.ResourceMatcher  `json:"resources"`
	}
	if len(targets) > 0 {
		if err := json.Unmarshal(targets, &targetEnvelope); err != nil {
			return domainaccess.Policy{}, fmt.Errorf("unmarshal policy targets: %w", err)
		}
		policy.Clusters = targetEnvelope.Clusters
		policy.Namespaces = targetEnvelope.Namespaces
		policy.Resources = targetEnvelope.Resources
	}
	return policy, nil
}
