package aigateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	"github.com/soha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListPersonalAccessTokens(ctx context.Context, userID string) ([]domainaigateway.PersonalAccessToken, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, name, token_hash, token_prefix, scopes, permission_keys, metadata, expires_at, last_used_at, revoked_at, created_by, created_at, updated_at
		FROM personal_access_tokens
		WHERE user_id = ?
		ORDER BY created_at DESC, id ASC
	`, userID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPersonalAccessTokenRows(rows)
}

func (r *Repository) CreatePersonalAccessToken(ctx context.Context, item domainaigateway.PersonalAccessToken) (domainaigateway.PersonalAccessToken, error) {
	scopes, permissionKeys, metadata, err := marshalGatewayTokenJSON(item.Scopes, item.PermissionKeys, item.Metadata)
	if err != nil {
		return domainaigateway.PersonalAccessToken{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO personal_access_tokens (id, user_id, name, token_hash, token_prefix, scopes, permission_keys, metadata, expires_at, last_used_at, revoked_at, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.UserID, item.Name, item.TokenHash, item.TokenPrefix, scopes, permissionKeys, metadata, item.ExpiresAt, item.LastUsedAt, item.RevokedAt, item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.PersonalAccessToken{}, err
	}
	return item, nil
}

func (r *Repository) GetPersonalAccessTokenByHash(ctx context.Context, tokenHash string) (domainaigateway.PersonalAccessToken, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, name, token_hash, token_prefix, scopes, permission_keys, metadata, expires_at, last_used_at, revoked_at, created_by, created_at, updated_at
		FROM personal_access_tokens
		WHERE token_hash = ?
		LIMIT 1
	`, tokenHash).Row()
	return scanPersonalAccessToken(row)
}

func (r *Repository) TouchPersonalAccessToken(ctx context.Context, tokenID string, at time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE personal_access_tokens
		SET last_used_at = ?, updated_at = ?
		WHERE id = ?
	`, at, time.Now().UTC(), tokenID).Error
}

func (r *Repository) RevokePersonalAccessToken(ctx context.Context, userID, tokenID string) error {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE personal_access_tokens
		SET revoked_at = COALESCE(revoked_at, ?), updated_at = ?
		WHERE id = ? AND user_id = ?
	`, time.Now().UTC(), time.Now().UTC(), tokenID, userID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *Repository) ListServiceAccounts(ctx context.Context) ([]domainaigateway.ServiceAccount, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, description, status, owner_user_id, role_ids, team_ids, scope_grant_ids, metadata, created_by, created_at, updated_at
		FROM service_accounts
		ORDER BY name ASC, id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanServiceAccountRows(rows)
}

func (r *Repository) CreateServiceAccount(ctx context.Context, item domainaigateway.ServiceAccount) (domainaigateway.ServiceAccount, error) {
	roleIDs, teamIDs, scopeGrantIDs, metadata, err := marshalServiceAccountJSON(item.RoleIDs, item.TeamIDs, item.ScopeGrantIDs, item.Metadata)
	if err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO service_accounts (id, name, description, status, owner_user_id, role_ids, team_ids, scope_grant_ids, metadata, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, nullableString(item.Description), item.Status, nullableString(item.OwnerUserID), roleIDs, teamIDs, scopeGrantIDs, metadata, item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	return item, nil
}

func (r *Repository) GetServiceAccount(ctx context.Context, serviceAccountID string) (domainaigateway.ServiceAccount, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, description, status, owner_user_id, role_ids, team_ids, scope_grant_ids, metadata, created_by, created_at, updated_at
		FROM service_accounts
		WHERE id = ?
		LIMIT 1
	`, serviceAccountID).Row()
	return scanServiceAccount(row)
}

func (r *Repository) CreateServiceAccountToken(ctx context.Context, item domainaigateway.ServiceAccountToken) (domainaigateway.ServiceAccountToken, error) {
	scopes, permissionKeys, metadata, err := marshalGatewayTokenJSON(item.Scopes, item.PermissionKeys, item.Metadata)
	if err != nil {
		return domainaigateway.ServiceAccountToken{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO service_account_tokens (id, service_account_id, name, token_hash, token_prefix, scopes, permission_keys, metadata, expires_at, last_used_at, revoked_at, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ServiceAccountID, item.Name, item.TokenHash, item.TokenPrefix, scopes, permissionKeys, metadata, item.ExpiresAt, item.LastUsedAt, item.RevokedAt, item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.ServiceAccountToken{}, err
	}
	return item, nil
}

func (r *Repository) GetServiceAccountTokenByHash(ctx context.Context, tokenHash string) (domainaigateway.ServiceAccountToken, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, service_account_id, name, token_hash, token_prefix, scopes, permission_keys, metadata, expires_at, last_used_at, revoked_at, created_by, created_at, updated_at
		FROM service_account_tokens
		WHERE token_hash = ?
		LIMIT 1
	`, tokenHash).Row()
	return scanServiceAccountToken(row)
}

func (r *Repository) TouchServiceAccountToken(ctx context.Context, tokenID string, at time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE service_account_tokens
		SET last_used_at = ?, updated_at = ?
		WHERE id = ?
	`, at, time.Now().UTC(), tokenID).Error
}

func (r *Repository) RevokeServiceAccountToken(ctx context.Context, tokenID string) error {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE service_account_tokens
		SET revoked_at = COALESCE(revoked_at, ?), updated_at = ?
		WHERE id = ?
	`, time.Now().UTC(), time.Now().UTC(), tokenID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *Repository) ListAIClients(ctx context.Context) ([]domainaigateway.AIClient, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, kind, status, redirect_uris, allowed_origins, metadata, created_by, created_at, updated_at
		FROM ai_clients
		ORDER BY name ASC, id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAIClientRows(rows)
}

func (r *Repository) CreateAIClient(ctx context.Context, item domainaigateway.AIClient) (domainaigateway.AIClient, error) {
	redirectURIs, allowedOrigins, metadata, err := marshalAIClientJSON(item.RedirectURIs, item.AllowedOrigins, item.Metadata)
	if err != nil {
		return domainaigateway.AIClient{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_clients (id, name, kind, status, redirect_uris, allowed_origins, metadata, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.Kind, item.Status, redirectURIs, allowedOrigins, metadata, item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.AIClient{}, err
	}
	return item, nil
}

func (r *Repository) UpdateAIClient(ctx context.Context, item domainaigateway.AIClient) (domainaigateway.AIClient, error) {
	redirectURIs, allowedOrigins, metadata, err := marshalAIClientJSON(item.RedirectURIs, item.AllowedOrigins, item.Metadata)
	if err != nil {
		return domainaigateway.AIClient{}, err
	}
	item.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_clients
		SET name = ?, kind = ?, status = ?, redirect_uris = ?, allowed_origins = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, item.Kind, item.Status, redirectURIs, allowedOrigins, metadata, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainaigateway.AIClient{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainaigateway.AIClient{}, apperrors.ErrNotFound
	}
	return r.getAIClient(ctx, item.ID)
}

func (r *Repository) getAIClient(ctx context.Context, clientID string) (domainaigateway.AIClient, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, kind, status, redirect_uris, allowed_origins, metadata, created_by, created_at, updated_at
		FROM ai_clients
		WHERE id = ?
		LIMIT 1
	`, clientID).Row()
	return scanAIClient(row)
}

func (r *Repository) ListToolGrants(ctx context.Context, filter domainaigateway.ToolGrantFilter) ([]domainaigateway.ToolGrant, error) {
	query := `
		SELECT id, subject_type, subject_id, ai_client_id, tool_name, effect, risk_level, permission_keys, resource_scopes, requires_approval, expires_at, created_by, created_at, updated_at
		FROM mcp_tool_grants
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filter.SubjectType != "" {
		query += " AND subject_type = ?"
		args = append(args, filter.SubjectType)
	}
	if filter.SubjectID != "" {
		query += " AND subject_id = ?"
		args = append(args, filter.SubjectID)
	}
	if filter.AIClientID != "" {
		query += " AND ai_client_id = ?"
		args = append(args, filter.AIClientID)
	}
	if filter.ToolName != "" {
		query += " AND tool_name = ?"
		args = append(args, filter.ToolName)
	}
	if !filter.IncludeExpired {
		query += " AND (expires_at IS NULL OR expires_at > ?)"
		args = append(args, time.Now().UTC())
	}
	query += " ORDER BY created_at DESC, id ASC"
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanToolGrantRows(rows)
}

func (r *Repository) CreateToolGrant(ctx context.Context, item domainaigateway.ToolGrant) (domainaigateway.ToolGrant, error) {
	permissionKeys, resourceScopes, err := marshalToolGrantJSON(item.PermissionKeys, item.ResourceScopes)
	if err != nil {
		return domainaigateway.ToolGrant{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO mcp_tool_grants (id, subject_type, subject_id, ai_client_id, tool_name, effect, risk_level, permission_keys, resource_scopes, requires_approval, expires_at, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.SubjectType, item.SubjectID, nullableString(item.AIClientID), item.ToolName, item.Effect, item.RiskLevel, permissionKeys, resourceScopes, item.RequiresApproval, item.ExpiresAt, item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.ToolGrant{}, err
	}
	return item, nil
}

func (r *Repository) DeleteToolGrant(ctx context.Context, grantID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM mcp_tool_grants WHERE id = ?`, grantID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *Repository) ListActiveToolGrants(ctx context.Context, subjectType, subjectID, aiClientID string, at time.Time) ([]domainaigateway.ToolGrant, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, subject_type, subject_id, ai_client_id, tool_name, effect, risk_level, permission_keys, resource_scopes, requires_approval, expires_at, created_by, created_at, updated_at
		FROM mcp_tool_grants
		WHERE subject_type = ?
			AND subject_id = ?
			AND (ai_client_id IS NULL OR ai_client_id = '' OR ai_client_id = ?)
			AND (expires_at IS NULL OR expires_at > ?)
		ORDER BY created_at DESC, id ASC
	`, subjectType, subjectID, aiClientID, at).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanToolGrantRows(rows)
}

func (r *Repository) ListAccessPolicies(ctx context.Context, filter domainaigateway.AccessPolicyFilter) ([]domainaigateway.AccessPolicy, error) {
	query := `
		SELECT id, name, description, enabled, subject_type, subject_id, ai_client_id, effect, tool_patterns, skill_ids, resource_scopes, risk_levels, approval_policy, conditions, created_by, created_at, updated_at
		FROM ai_access_policies
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filter.SubjectType != "" {
		query += " AND subject_type = ?"
		args = append(args, filter.SubjectType)
	}
	if filter.SubjectID != "" {
		query += " AND subject_id = ?"
		args = append(args, filter.SubjectID)
	}
	if filter.AIClientID != "" {
		query += " AND ai_client_id = ?"
		args = append(args, filter.AIClientID)
	}
	if filter.Effect != "" {
		query += " AND effect = ?"
		args = append(args, filter.Effect)
	}
	if !filter.IncludeDisabled {
		query += " AND enabled = TRUE"
	}
	query += " ORDER BY updated_at DESC, id ASC"
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAccessPolicyRows(rows)
}

func (r *Repository) CreateAccessPolicy(ctx context.Context, item domainaigateway.AccessPolicy) (domainaigateway.AccessPolicy, error) {
	toolPatterns, skillIDs, resourceScopes, riskLevels, approvalPolicy, conditions, err := marshalAccessPolicyJSON(item.ToolPatterns, item.SkillIDs, item.ResourceScopes, item.RiskLevels, item.ApprovalPolicy, item.Conditions)
	if err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_access_policies (id, name, description, enabled, subject_type, subject_id, ai_client_id, effect, tool_patterns, skill_ids, resource_scopes, risk_levels, approval_policy, conditions, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, nullableString(item.Description), item.Enabled, item.SubjectType, item.SubjectID, nullableString(item.AIClientID), item.Effect, toolPatterns, skillIDs, resourceScopes, riskLevels, approvalPolicy, conditions, item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	return item, nil
}

func (r *Repository) UpdateAccessPolicy(ctx context.Context, item domainaigateway.AccessPolicy) (domainaigateway.AccessPolicy, error) {
	toolPatterns, skillIDs, resourceScopes, riskLevels, approvalPolicy, conditions, err := marshalAccessPolicyJSON(item.ToolPatterns, item.SkillIDs, item.ResourceScopes, item.RiskLevels, item.ApprovalPolicy, item.Conditions)
	if err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	item.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_access_policies
		SET name = ?, description = ?, enabled = ?, subject_type = ?, subject_id = ?, ai_client_id = ?, effect = ?, tool_patterns = ?, skill_ids = ?, resource_scopes = ?, risk_levels = ?, approval_policy = ?, conditions = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, nullableString(item.Description), item.Enabled, item.SubjectType, item.SubjectID, nullableString(item.AIClientID), item.Effect, toolPatterns, skillIDs, resourceScopes, riskLevels, approvalPolicy, conditions, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainaigateway.AccessPolicy{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainaigateway.AccessPolicy{}, apperrors.ErrNotFound
	}
	return r.getAccessPolicy(ctx, item.ID)
}

func (r *Repository) DeleteAccessPolicy(ctx context.Context, policyID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM ai_access_policies WHERE id = ?`, policyID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *Repository) ListActiveAccessPolicies(ctx context.Context, subjectType, subjectID, aiClientID string) ([]domainaigateway.AccessPolicy, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, description, enabled, subject_type, subject_id, ai_client_id, effect, tool_patterns, skill_ids, resource_scopes, risk_levels, approval_policy, conditions, created_by, created_at, updated_at
		FROM ai_access_policies
		WHERE enabled = TRUE
			AND subject_type = ?
			AND subject_id = ?
			AND (ai_client_id IS NULL OR ai_client_id = '' OR ai_client_id = ?)
		ORDER BY updated_at DESC, id ASC
	`, subjectType, subjectID, aiClientID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAccessPolicyRows(rows)
}

func (r *Repository) getAccessPolicy(ctx context.Context, policyID string) (domainaigateway.AccessPolicy, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, description, enabled, subject_type, subject_id, ai_client_id, effect, tool_patterns, skill_ids, resource_scopes, risk_levels, approval_policy, conditions, created_by, created_at, updated_at
		FROM ai_access_policies
		WHERE id = ?
		LIMIT 1
	`, policyID).Row()
	return scanAccessPolicy(row)
}

func (r *Repository) ListSkillBindings(ctx context.Context, filter domainaigateway.SkillBindingFilter) ([]domainaigateway.SkillBinding, error) {
	query := `
		SELECT id, subject_type, subject_id, ai_client_id, skill_id, capability_refs, enabled, metadata, created_by, created_at, updated_at
		FROM ai_gateway_skill_bindings
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filter.SubjectType != "" {
		query += " AND subject_type = ?"
		args = append(args, filter.SubjectType)
	}
	if filter.SubjectID != "" {
		query += " AND subject_id = ?"
		args = append(args, filter.SubjectID)
	}
	if filter.AIClientID != "" {
		query += " AND ai_client_id = ?"
		args = append(args, filter.AIClientID)
	}
	if filter.SkillID != "" {
		query += " AND skill_id = ?"
		args = append(args, filter.SkillID)
	}
	if !filter.IncludeDisabled {
		query += " AND enabled = TRUE"
	}
	query += " ORDER BY updated_at DESC, id ASC"
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSkillBindingRows(rows)
}

func (r *Repository) CreateSkillBinding(ctx context.Context, item domainaigateway.SkillBinding) (domainaigateway.SkillBinding, error) {
	capabilityRefs, metadata, err := marshalSkillBindingJSON(item.CapabilityRefs, item.Metadata)
	if err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_gateway_skill_bindings (id, subject_type, subject_id, ai_client_id, skill_id, capability_refs, enabled, metadata, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.SubjectType, item.SubjectID, nullableString(item.AIClientID), item.SkillID, capabilityRefs, item.Enabled, metadata, item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	return item, nil
}

func (r *Repository) UpdateSkillBinding(ctx context.Context, item domainaigateway.SkillBinding) (domainaigateway.SkillBinding, error) {
	capabilityRefs, metadata, err := marshalSkillBindingJSON(item.CapabilityRefs, item.Metadata)
	if err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	item.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_gateway_skill_bindings
		SET subject_type = ?, subject_id = ?, ai_client_id = ?, skill_id = ?, capability_refs = ?, enabled = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, item.SubjectType, item.SubjectID, nullableString(item.AIClientID), item.SkillID, capabilityRefs, item.Enabled, metadata, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainaigateway.SkillBinding{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainaigateway.SkillBinding{}, apperrors.ErrNotFound
	}
	return r.getSkillBinding(ctx, item.ID)
}

func (r *Repository) DeleteSkillBinding(ctx context.Context, bindingID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM ai_gateway_skill_bindings WHERE id = ?`, bindingID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *Repository) ListActiveSkillBindings(ctx context.Context, subjectType, subjectID, aiClientID string) ([]domainaigateway.SkillBinding, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, subject_type, subject_id, ai_client_id, skill_id, capability_refs, enabled, metadata, created_by, created_at, updated_at
		FROM ai_gateway_skill_bindings
		WHERE enabled = TRUE
			AND subject_type = ?
			AND subject_id = ?
			AND (ai_client_id IS NULL OR ai_client_id = '' OR ai_client_id = ?)
		ORDER BY updated_at DESC, id ASC
	`, subjectType, subjectID, aiClientID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSkillBindingRows(rows)
}

func (r *Repository) getSkillBinding(ctx context.Context, bindingID string) (domainaigateway.SkillBinding, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, subject_type, subject_id, ai_client_id, skill_id, capability_refs, enabled, metadata, created_by, created_at, updated_at
		FROM ai_gateway_skill_bindings
		WHERE id = ?
		LIMIT 1
	`, bindingID).Row()
	return scanSkillBinding(row)
}

func (r *Repository) CreateAuditLog(ctx context.Context, item domainaigateway.AuditLog) error {
	resourceScope, metadata, err := marshalAuditLogJSON(item.ResourceScope, item.Metadata)
	if err != nil {
		return err
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_gateway_audit_logs (id, actor_type, actor_id, actor_name, ai_client_id, ai_client_name, skill_id, tool_name, risk_level, resource_scope, action, result, summary, request_id, source_ip, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ActorType, item.ActorID, nullableString(item.ActorName), nullableString(item.AIClientID), nullableString(item.AIClientName), nullableString(item.SkillID), nullableString(item.ToolName), nullableString(string(item.RiskLevel)), resourceScope, item.Action, item.Result, item.Summary, nullableString(item.RequestID), nullableString(item.SourceIP), metadata, item.CreatedAt).Error
}

func scanPersonalAccessTokenRows(rows *sql.Rows) ([]domainaigateway.PersonalAccessToken, error) {
	items := make([]domainaigateway.PersonalAccessToken, 0)
	for rows.Next() {
		item, err := scanPersonalAccessTokenScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanAIClientRows(rows *sql.Rows) ([]domainaigateway.AIClient, error) {
	items := make([]domainaigateway.AIClient, 0)
	for rows.Next() {
		item, err := scanAIClientScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanAIClient(row *sql.Row) (domainaigateway.AIClient, error) {
	item, err := scanAIClientScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.AIClient{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanAIClientScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.AIClient, error) {
	var item domainaigateway.AIClient
	var redirectURIs, allowedOrigins, metadata []byte
	if err := scanner.Scan(&item.ID, &item.Name, &item.Kind, &item.Status, &redirectURIs, &allowedOrigins, &metadata, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainaigateway.AIClient{}, err
	}
	unmarshalJSON(redirectURIs, &item.RedirectURIs)
	unmarshalJSON(allowedOrigins, &item.AllowedOrigins)
	unmarshalJSON(metadata, &item.Metadata)
	if item.RedirectURIs == nil {
		item.RedirectURIs = []string{}
	}
	if item.AllowedOrigins == nil {
		item.AllowedOrigins = []string{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanPersonalAccessToken(row *sql.Row) (domainaigateway.PersonalAccessToken, error) {
	item, err := scanPersonalAccessTokenScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.PersonalAccessToken{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanPersonalAccessTokenScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.PersonalAccessToken, error) {
	var item domainaigateway.PersonalAccessToken
	var scopes, permissionKeys, metadata []byte
	var expiresAt, lastUsedAt, revokedAt sql.NullTime
	if err := scanner.Scan(&item.ID, &item.UserID, &item.Name, &item.TokenHash, &item.TokenPrefix, &scopes, &permissionKeys, &metadata, &expiresAt, &lastUsedAt, &revokedAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainaigateway.PersonalAccessToken{}, err
	}
	item.ExpiresAt = nullTimePointer(expiresAt)
	item.LastUsedAt = nullTimePointer(lastUsedAt)
	item.RevokedAt = nullTimePointer(revokedAt)
	unmarshalJSON(scopes, &item.Scopes)
	unmarshalJSON(permissionKeys, &item.PermissionKeys)
	unmarshalJSON(metadata, &item.Metadata)
	if item.Scopes == nil {
		item.Scopes = []string{}
	}
	if item.PermissionKeys == nil {
		item.PermissionKeys = []string{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanServiceAccountRows(rows *sql.Rows) ([]domainaigateway.ServiceAccount, error) {
	items := make([]domainaigateway.ServiceAccount, 0)
	for rows.Next() {
		item, err := scanServiceAccountScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanServiceAccount(row *sql.Row) (domainaigateway.ServiceAccount, error) {
	item, err := scanServiceAccountScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.ServiceAccount{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanServiceAccountScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.ServiceAccount, error) {
	var item domainaigateway.ServiceAccount
	var description, ownerUserID sql.NullString
	var roleIDs, teamIDs, scopeGrantIDs, metadata []byte
	if err := scanner.Scan(&item.ID, &item.Name, &description, &item.Status, &ownerUserID, &roleIDs, &teamIDs, &scopeGrantIDs, &metadata, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	item.Description = description.String
	item.OwnerUserID = ownerUserID.String
	unmarshalJSON(roleIDs, &item.RoleIDs)
	unmarshalJSON(teamIDs, &item.TeamIDs)
	unmarshalJSON(scopeGrantIDs, &item.ScopeGrantIDs)
	unmarshalJSON(metadata, &item.Metadata)
	if item.RoleIDs == nil {
		item.RoleIDs = []string{}
	}
	if item.TeamIDs == nil {
		item.TeamIDs = []string{}
	}
	if item.ScopeGrantIDs == nil {
		item.ScopeGrantIDs = []string{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanServiceAccountToken(row *sql.Row) (domainaigateway.ServiceAccountToken, error) {
	item, err := scanServiceAccountTokenScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.ServiceAccountToken{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanServiceAccountTokenScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.ServiceAccountToken, error) {
	var item domainaigateway.ServiceAccountToken
	var scopes, permissionKeys, metadata []byte
	var expiresAt, lastUsedAt, revokedAt sql.NullTime
	if err := scanner.Scan(&item.ID, &item.ServiceAccountID, &item.Name, &item.TokenHash, &item.TokenPrefix, &scopes, &permissionKeys, &metadata, &expiresAt, &lastUsedAt, &revokedAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainaigateway.ServiceAccountToken{}, err
	}
	item.ExpiresAt = nullTimePointer(expiresAt)
	item.LastUsedAt = nullTimePointer(lastUsedAt)
	item.RevokedAt = nullTimePointer(revokedAt)
	unmarshalJSON(scopes, &item.Scopes)
	unmarshalJSON(permissionKeys, &item.PermissionKeys)
	unmarshalJSON(metadata, &item.Metadata)
	if item.Scopes == nil {
		item.Scopes = []string{}
	}
	if item.PermissionKeys == nil {
		item.PermissionKeys = []string{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanToolGrantRows(rows *sql.Rows) ([]domainaigateway.ToolGrant, error) {
	items := make([]domainaigateway.ToolGrant, 0)
	for rows.Next() {
		item, err := scanToolGrantScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanToolGrantScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.ToolGrant, error) {
	var item domainaigateway.ToolGrant
	var aiClientID sql.NullString
	var permissionKeys, resourceScopes []byte
	var expiresAt sql.NullTime
	var riskLevel string
	if err := scanner.Scan(&item.ID, &item.SubjectType, &item.SubjectID, &aiClientID, &item.ToolName, &item.Effect, &riskLevel, &permissionKeys, &resourceScopes, &item.RequiresApproval, &expiresAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainaigateway.ToolGrant{}, err
	}
	item.AIClientID = aiClientID.String
	item.RiskLevel = domainaigateway.RiskLevel(riskLevel)
	item.ExpiresAt = nullTimePointer(expiresAt)
	unmarshalJSON(permissionKeys, &item.PermissionKeys)
	unmarshalJSON(resourceScopes, &item.ResourceScopes)
	if item.PermissionKeys == nil {
		item.PermissionKeys = []string{}
	}
	if item.ResourceScopes == nil {
		item.ResourceScopes = map[string]any{}
	}
	return item, nil
}

func scanAccessPolicyRows(rows *sql.Rows) ([]domainaigateway.AccessPolicy, error) {
	items := make([]domainaigateway.AccessPolicy, 0)
	for rows.Next() {
		item, err := scanAccessPolicyScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanAccessPolicy(row *sql.Row) (domainaigateway.AccessPolicy, error) {
	item, err := scanAccessPolicyScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.AccessPolicy{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanAccessPolicyScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.AccessPolicy, error) {
	var item domainaigateway.AccessPolicy
	var description, aiClientID sql.NullString
	var toolPatterns, skillIDs, resourceScopes, riskLevels, approvalPolicy, conditions []byte
	var rawRiskLevels []string
	if err := scanner.Scan(&item.ID, &item.Name, &description, &item.Enabled, &item.SubjectType, &item.SubjectID, &aiClientID, &item.Effect, &toolPatterns, &skillIDs, &resourceScopes, &riskLevels, &approvalPolicy, &conditions, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainaigateway.AccessPolicy{}, err
	}
	item.Description = description.String
	item.AIClientID = aiClientID.String
	unmarshalJSON(toolPatterns, &item.ToolPatterns)
	unmarshalJSON(skillIDs, &item.SkillIDs)
	unmarshalJSON(resourceScopes, &item.ResourceScopes)
	unmarshalJSON(riskLevels, &rawRiskLevels)
	unmarshalJSON(approvalPolicy, &item.ApprovalPolicy)
	unmarshalJSON(conditions, &item.Conditions)
	item.RiskLevels = make([]domainaigateway.RiskLevel, 0, len(rawRiskLevels))
	for _, value := range rawRiskLevels {
		item.RiskLevels = append(item.RiskLevels, domainaigateway.RiskLevel(value))
	}
	if item.ToolPatterns == nil {
		item.ToolPatterns = []string{}
	}
	if item.SkillIDs == nil {
		item.SkillIDs = []string{}
	}
	if item.ResourceScopes == nil {
		item.ResourceScopes = map[string]any{}
	}
	if item.RiskLevels == nil {
		item.RiskLevels = []domainaigateway.RiskLevel{}
	}
	if item.ApprovalPolicy == nil {
		item.ApprovalPolicy = map[string]any{}
	}
	if item.Conditions == nil {
		item.Conditions = map[string]any{}
	}
	return item, nil
}

func scanSkillBindingRows(rows *sql.Rows) ([]domainaigateway.SkillBinding, error) {
	items := make([]domainaigateway.SkillBinding, 0)
	for rows.Next() {
		item, err := scanSkillBindingScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanSkillBinding(row *sql.Row) (domainaigateway.SkillBinding, error) {
	item, err := scanSkillBindingScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.SkillBinding{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanSkillBindingScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.SkillBinding, error) {
	var item domainaigateway.SkillBinding
	var aiClientID sql.NullString
	var capabilityRefs, metadata []byte
	if err := scanner.Scan(&item.ID, &item.SubjectType, &item.SubjectID, &aiClientID, &item.SkillID, &capabilityRefs, &item.Enabled, &metadata, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainaigateway.SkillBinding{}, err
	}
	item.AIClientID = aiClientID.String
	unmarshalJSON(capabilityRefs, &item.CapabilityRefs)
	unmarshalJSON(metadata, &item.Metadata)
	if item.CapabilityRefs == nil {
		item.CapabilityRefs = []string{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func marshalGatewayTokenJSON(scopes, permissionKeys []string, metadata map[string]any) (string, string, string, error) {
	scopesRaw, err := json.Marshal(emptyStringSlice(scopes))
	if err != nil {
		return "", "", "", fmt.Errorf("marshal scopes: %w", err)
	}
	permissionKeysRaw, err := json.Marshal(emptyStringSlice(permissionKeys))
	if err != nil {
		return "", "", "", fmt.Errorf("marshal permission keys: %w", err)
	}
	metadataRaw, err := json.Marshal(emptyMap(metadata))
	if err != nil {
		return "", "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(scopesRaw), string(permissionKeysRaw), string(metadataRaw), nil
}

func marshalAIClientJSON(redirectURIs, allowedOrigins []string, metadata map[string]any) (string, string, string, error) {
	redirectRaw, err := json.Marshal(emptyStringSlice(redirectURIs))
	if err != nil {
		return "", "", "", fmt.Errorf("marshal redirect uris: %w", err)
	}
	originsRaw, err := json.Marshal(emptyStringSlice(allowedOrigins))
	if err != nil {
		return "", "", "", fmt.Errorf("marshal allowed origins: %w", err)
	}
	metadataRaw, err := json.Marshal(emptyMap(metadata))
	if err != nil {
		return "", "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(redirectRaw), string(originsRaw), string(metadataRaw), nil
}

func marshalToolGrantJSON(permissionKeys []string, resourceScopes map[string]any) (string, string, error) {
	permissionRaw, err := json.Marshal(emptyStringSlice(permissionKeys))
	if err != nil {
		return "", "", fmt.Errorf("marshal permission keys: %w", err)
	}
	scopeRaw, err := json.Marshal(emptyMap(resourceScopes))
	if err != nil {
		return "", "", fmt.Errorf("marshal resource scopes: %w", err)
	}
	return string(permissionRaw), string(scopeRaw), nil
}

func marshalAccessPolicyJSON(toolPatterns, skillIDs []string, resourceScopes map[string]any, riskLevels []domainaigateway.RiskLevel, approvalPolicy, conditions map[string]any) (string, string, string, string, string, string, error) {
	toolPatternRaw, err := json.Marshal(emptyStringSlice(toolPatterns))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal tool patterns: %w", err)
	}
	skillRaw, err := json.Marshal(emptyStringSlice(skillIDs))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal skill ids: %w", err)
	}
	scopeRaw, err := json.Marshal(emptyMap(resourceScopes))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal resource scopes: %w", err)
	}
	riskValues := make([]string, 0, len(riskLevels))
	for _, riskLevel := range riskLevels {
		riskValues = append(riskValues, string(riskLevel))
	}
	riskRaw, err := json.Marshal(emptyStringSlice(riskValues))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal risk levels: %w", err)
	}
	approvalRaw, err := json.Marshal(emptyMap(approvalPolicy))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal approval policy: %w", err)
	}
	conditionRaw, err := json.Marshal(emptyMap(conditions))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal conditions: %w", err)
	}
	return string(toolPatternRaw), string(skillRaw), string(scopeRaw), string(riskRaw), string(approvalRaw), string(conditionRaw), nil
}

func marshalSkillBindingJSON(capabilityRefs []string, metadata map[string]any) (string, string, error) {
	capabilityRaw, err := json.Marshal(emptyStringSlice(capabilityRefs))
	if err != nil {
		return "", "", fmt.Errorf("marshal capability refs: %w", err)
	}
	metadataRaw, err := json.Marshal(emptyMap(metadata))
	if err != nil {
		return "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(capabilityRaw), string(metadataRaw), nil
}

func marshalServiceAccountJSON(roleIDs, teamIDs, scopeGrantIDs []string, metadata map[string]any) (string, string, string, string, error) {
	roleRaw, err := json.Marshal(emptyStringSlice(roleIDs))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal role ids: %w", err)
	}
	teamRaw, err := json.Marshal(emptyStringSlice(teamIDs))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal team ids: %w", err)
	}
	scopeRaw, err := json.Marshal(emptyStringSlice(scopeGrantIDs))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal scope grant ids: %w", err)
	}
	metadataRaw, err := json.Marshal(emptyMap(metadata))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(roleRaw), string(teamRaw), string(scopeRaw), string(metadataRaw), nil
}

func marshalAuditLogJSON(resourceScope, metadata map[string]any) (string, string, error) {
	resourceScopeRaw, err := json.Marshal(emptyMap(resourceScope))
	if err != nil {
		return "", "", fmt.Errorf("marshal resource scope: %w", err)
	}
	metadataRaw, err := json.Marshal(emptyMap(metadata))
	if err != nil {
		return "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(resourceScopeRaw), string(metadataRaw), nil
}

func unmarshalJSON(raw []byte, out any) {
	if len(raw) == 0 {
		return
	}
	_ = json.Unmarshal(raw, out)
}

func emptyStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func emptyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	return values
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
