package aigateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func appendStringFilter(query *string, args *[]any, column, value string) {
	if value == "" {
		return
	}
	clauses := map[string]string{
		"subject_type":  " AND subject_type = ?",
		"subject_id":    " AND subject_id = ?",
		"ai_client_id":  " AND ai_client_id = ?",
		"effect":        " AND effect = ?",
		"skill_id":      " AND skill_id = ?",
		"public_model":  " AND public_model = ?",
		"provider_kind": " AND provider_kind = ?",
		"upstream_id":   " AND upstream_id = ?",
		"route_group":   " AND route_group = ?",
	}
	clause, ok := clauses[column]
	if !ok {
		return
	}
	*query += clause
	*args = append(*args, value)
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
	defer func() { _ = rows.Close() }()
	return scanPersonalAccessTokenRows(rows)
}

func (r *Repository) ListAllPersonalAccessTokens(ctx context.Context) ([]domainaigateway.PersonalAccessToken, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, name, token_hash, token_prefix, scopes, permission_keys, metadata, expires_at, last_used_at, revoked_at, created_by, created_at, updated_at
		FROM personal_access_tokens
		ORDER BY created_at DESC, id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPersonalAccessTokenRows(rows)
}

func (r *Repository) CreatePersonalAccessToken(ctx context.Context, item domainaigateway.PersonalAccessToken) (domainaigateway.PersonalAccessToken, error) {
	scopes, permissionKeys, metadata, err := marshalGatewayTokenJSON(item.Scopes, item.PermissionKeys, item.Metadata)
	if err != nil {
		return domainaigateway.PersonalAccessToken{}, err
	}
	setCreateTimestamps(&item.CreatedAt, &item.UpdatedAt)
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
	defer func() { _ = rows.Close() }()
	return scanServiceAccountRows(rows)
}

func (r *Repository) CreateServiceAccount(ctx context.Context, item domainaigateway.ServiceAccount) (domainaigateway.ServiceAccount, error) {
	roleIDs, teamIDs, scopeGrantIDs, metadata, err := marshalServiceAccountJSON(item.RoleIDs, item.TeamIDs, item.ScopeGrantIDs, item.Metadata)
	if err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	setCreateTimestamps(&item.CreatedAt, &item.UpdatedAt)
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO service_accounts (id, name, description, status, owner_user_id, role_ids, team_ids, scope_grant_ids, metadata, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, nullableString(item.Description), item.Status, nullableString(item.OwnerUserID), roleIDs, teamIDs, scopeGrantIDs, metadata, item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	return item, nil
}

func setCreateTimestamps(createdAt, updatedAt *time.Time) {
	now := time.Now().UTC()
	if createdAt.IsZero() {
		*createdAt = now
	}
	if updatedAt.IsZero() {
		*updatedAt = now
	}
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

func (r *Repository) ListAllServiceAccountTokens(ctx context.Context) ([]domainaigateway.ServiceAccountToken, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, service_account_id, name, token_hash, token_prefix, scopes, permission_keys, metadata, expires_at, last_used_at, revoked_at, created_by, created_at, updated_at
		FROM service_account_tokens
		ORDER BY created_at DESC, id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanServiceAccountTokenRows(rows)
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
	defer func() { _ = rows.Close() }()
	return scanAIClientRows(rows)
}

func (r *Repository) GetAIClient(ctx context.Context, clientID string) (domainaigateway.AIClient, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, kind, status, redirect_uris, allowed_origins, metadata, created_by, created_at, updated_at
		FROM ai_clients
		WHERE id = ?
		LIMIT 1
	`, clientID).Row()
	return scanAIClient(row)
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
	return scanToolGrantRows(rows)
}

func (r *Repository) ListAccessPolicies(ctx context.Context, filter domainaigateway.AccessPolicyFilter) ([]domainaigateway.AccessPolicy, error) {
	query := `
		SELECT id, name, description, enabled, subject_type, subject_id, ai_client_id, effect, tool_patterns, skill_ids, resource_scopes, risk_levels, approval_policy, conditions, created_by, created_at, updated_at
		FROM ai_access_policies
		WHERE 1 = 1
	`
	args := make([]any, 0)
	appendStringFilter(&query, &args, "subject_type", filter.SubjectType)
	appendStringFilter(&query, &args, "subject_id", filter.SubjectID)
	appendStringFilter(&query, &args, "ai_client_id", filter.AIClientID)
	appendStringFilter(&query, &args, "effect", filter.Effect)
	if !filter.IncludeDisabled {
		query += " AND enabled = TRUE"
	}
	query += " ORDER BY updated_at DESC, id ASC"
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	appendStringFilter(&query, &args, "subject_type", filter.SubjectType)
	appendStringFilter(&query, &args, "subject_id", filter.SubjectID)
	appendStringFilter(&query, &args, "ai_client_id", filter.AIClientID)
	appendStringFilter(&query, &args, "skill_id", filter.SkillID)
	if !filter.IncludeDisabled {
		query += " AND enabled = TRUE"
	}
	query += " ORDER BY updated_at DESC, id ASC"
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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

func (r *Repository) ListAuditLogs(ctx context.Context, filter domainaigateway.AuditLogFilter) ([]domainaigateway.AuditLog, error) {
	query := `
		SELECT id, actor_type, actor_id, actor_name, ai_client_id, ai_client_name, skill_id, tool_name, risk_level, resource_scope, action, result, summary, request_id, source_ip, metadata, created_at
		FROM ai_gateway_audit_logs
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filter.ActorType != "" {
		query += " AND actor_type = ?"
		args = append(args, filter.ActorType)
	}
	if filter.ActorID != "" {
		query += " AND actor_id = ?"
		args = append(args, filter.ActorID)
	}
	if filter.AIClientID != "" {
		query += " AND ai_client_id = ?"
		args = append(args, filter.AIClientID)
	}
	if filter.SkillID != "" {
		query += " AND skill_id = ?"
		args = append(args, filter.SkillID)
	}
	if filter.ToolName != "" {
		query += " AND tool_name = ?"
		args = append(args, filter.ToolName)
	}
	if filter.ApprovalRequestID != "" {
		query += " AND (metadata::jsonb ->> 'approvalRequestId' = ? OR metadata::jsonb #>> '{relatedIds,approvalRequestId}' = ?)"
		args = append(args, filter.ApprovalRequestID, filter.ApprovalRequestID)
	}
	if filter.RiskLevel != "" {
		query += " AND risk_level = ?"
		args = append(args, string(filter.RiskLevel))
	}
	if filter.Result != "" {
		query += " AND result = ?"
		args = append(args, filter.Result)
	}
	if filter.Action != "" {
		query += " AND action = ?"
		args = append(args, filter.Action)
	}
	if filter.From != nil {
		query += " AND created_at >= ?"
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		query += " AND created_at <= ?"
		args = append(args, *filter.To)
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query += " ORDER BY created_at DESC, id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanAuditLogRows(rows)
}

func (r *Repository) IncrementRateLimitCounter(ctx context.Context, item domainaigateway.RateLimitCounter) (domainaigateway.RateLimitCounter, error) {
	metadataRaw, err := json.Marshal(emptyMap(item.Metadata))
	if err != nil {
		return domainaigateway.RateLimitCounter{}, fmt.Errorf("marshal metadata: %w", err)
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	row := r.db.WithContext(ctx).Raw(`
		INSERT INTO ai_gateway_rate_limit_counters (
			key, policy_id, scope, actor_type, actor_id, ai_client_id, tool_name, window_start, window_end, limit_value, count, metadata, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET
			count = ai_gateway_rate_limit_counters.count + 1,
			limit_value = EXCLUDED.limit_value,
			window_end = EXCLUDED.window_end,
			metadata = EXCLUDED.metadata,
			updated_at = EXCLUDED.updated_at
		RETURNING key, policy_id, scope, actor_type, actor_id, ai_client_id, tool_name, window_start, window_end, limit_value, count, metadata, created_at, updated_at
	`, item.Key, item.PolicyID, item.Scope, nullableString(item.ActorType), nullableString(item.ActorID), nullableString(item.AIClientID), nullableString(item.ToolName), item.WindowStart, item.WindowEnd, item.Limit, string(metadataRaw), item.CreatedAt, item.UpdatedAt).Row()
	return scanRateLimitCounter(row)
}

func (r *Repository) ApplyRateLimitState(ctx context.Context, item domainaigateway.RateLimitState) (domainaigateway.RateLimitState, error) {
	metadataRaw, err := json.Marshal(emptyMap(item.Metadata))
	if err != nil {
		return domainaigateway.RateLimitState{}, fmt.Errorf("marshal metadata: %w", err)
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.Burst <= 0 {
		item.Burst = 1
	}
	var state domainaigateway.RateLimitState
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockAcquired int
		if err := tx.Raw(`
			SELECT 1
			FROM (SELECT pg_advisory_xact_lock(hashtextextended(?::text, 0))) locked
		`, item.Key).Row().Scan(&lockAcquired); err != nil {
			return err
		}
		row := tx.Raw(`
			WITH params AS (
				SELECT
					?::text AS key,
					?::text AS policy_id,
					?::text AS scope,
					?::text AS actor_type,
					?::text AS actor_id,
					?::text AS ai_client_id,
					?::text AS tool_name,
					?::int AS limit_value,
					?::int AS burst_value,
					?::double precision AS interval_seconds,
					?::json AS metadata,
					?::timestamp AS now_at,
					?::timestamp AS created_at,
					?::timestamp AS updated_at
			),
			existing AS (
				SELECT state.*
				FROM ai_gateway_rate_limit_states state
				JOIN params ON state.key = params.key
				FOR UPDATE OF state
			),
			decision AS (
				SELECT
					params.*,
					COALESCE(existing.tat, params.now_at) AS current_tat,
					(COALESCE(existing.tat, params.now_at) - make_interval(secs => GREATEST(params.burst_value - 1, 0) * params.interval_seconds)) <= params.now_at AS allowed
				FROM params
				LEFT JOIN existing ON true
			),
			upserted AS (
				INSERT INTO ai_gateway_rate_limit_states (
					key, policy_id, scope, actor_type, actor_id, ai_client_id, tool_name, limit_value, burst_value, interval_seconds, tat, metadata, created_at, updated_at
				)
				SELECT
					key, policy_id, scope, actor_type, actor_id, ai_client_id, tool_name, limit_value, burst_value, interval_seconds,
					CASE WHEN allowed THEN GREATEST(current_tat, now_at) + make_interval(secs => interval_seconds) ELSE current_tat END,
					metadata, created_at, updated_at
				FROM decision
				ON CONFLICT (key) DO UPDATE SET
					policy_id = EXCLUDED.policy_id,
					scope = EXCLUDED.scope,
					actor_type = EXCLUDED.actor_type,
					actor_id = EXCLUDED.actor_id,
					ai_client_id = EXCLUDED.ai_client_id,
					tool_name = EXCLUDED.tool_name,
					limit_value = EXCLUDED.limit_value,
					burst_value = EXCLUDED.burst_value,
					interval_seconds = EXCLUDED.interval_seconds,
					tat = EXCLUDED.tat,
					metadata = EXCLUDED.metadata,
					updated_at = EXCLUDED.updated_at
				RETURNING key, policy_id, scope, actor_type, actor_id, ai_client_id, tool_name, limit_value, burst_value, interval_seconds, tat, metadata, created_at, updated_at
			)
			SELECT
				upserted.key, upserted.policy_id, upserted.scope, upserted.actor_type, upserted.actor_id, upserted.ai_client_id, upserted.tool_name,
				upserted.limit_value, upserted.burst_value, upserted.interval_seconds, upserted.tat,
				decision.allowed,
				CASE
					WHEN decision.allowed THEN 0
					ELSE EXTRACT(EPOCH FROM ((decision.current_tat - make_interval(secs => GREATEST(decision.burst_value - 1, 0) * decision.interval_seconds)) - decision.now_at))
				END AS retry_after_seconds,
				upserted.metadata, upserted.created_at, upserted.updated_at
			FROM upserted
			JOIN decision ON decision.key = upserted.key
		`, item.Key, item.PolicyID, item.Scope, nullableString(item.ActorType), nullableString(item.ActorID), nullableString(item.AIClientID), nullableString(item.ToolName), item.Limit, item.Burst, item.IntervalSeconds, string(metadataRaw), now, item.CreatedAt, item.UpdatedAt).Row()
		var scanErr error
		state, scanErr = scanRateLimitState(row)
		return scanErr
	})
	return state, err
}

func (r *Repository) CreateApprovalRequest(ctx context.Context, item domainaigateway.ApprovalRequest) (domainaigateway.ApprovalRequest, error) {
	actorRoles, actorTeams, resourceScope, toolInput, relatedIDs, output, err := marshalApprovalRequestJSON(item.ActorRoles, item.ActorTeams, item.ResourceScope, item.ToolInput, item.RelatedIDs, item.Output)
	if err != nil {
		return domainaigateway.ApprovalRequest{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_gateway_approval_requests (
			id, status, strategy, policy_id, approval_policy_ref, actor_type, actor_id, actor_name, actor_roles, actor_teams,
			ai_client_id, ai_client_name, skill_id, tool_name, risk_level, requires_approval, resource_scope, tool_input, related_ids, output,
			summary, request_id, source_ip, decided_by, decided_by_name, decided_at, decision_comment, expires_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Status, item.Strategy, nullableString(item.PolicyID), nullableString(item.ApprovalPolicyRef), item.ActorType, item.ActorID, nullableString(item.ActorName), actorRoles, actorTeams,
		nullableString(item.AIClientID), nullableString(item.AIClientName), nullableString(item.SkillID), item.ToolName, string(item.RiskLevel), item.RequiresApproval, resourceScope, toolInput, relatedIDs, output,
		item.Summary, nullableString(item.RequestID), nullableString(item.SourceIP), nullableString(item.DecidedBy), nullableString(item.DecidedByName), item.DecidedAt, nullableString(item.DecisionComment), item.ExpiresAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.ApprovalRequest{}, err
	}
	return item, nil
}

func (r *Repository) GetApprovalRequest(ctx context.Context, requestID string) (domainaigateway.ApprovalRequest, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, status, strategy, policy_id, approval_policy_ref, actor_type, actor_id, actor_name, actor_roles, actor_teams,
			ai_client_id, ai_client_name, skill_id, tool_name, risk_level, requires_approval, resource_scope, tool_input, related_ids, output,
			summary, request_id, source_ip, decided_by, decided_by_name, decided_at, decision_comment, expires_at, created_at, updated_at
		FROM ai_gateway_approval_requests
		WHERE id = ?
		LIMIT 1
	`, requestID).Row()
	return scanApprovalRequest(row)
}

func (r *Repository) ListApprovalRequests(ctx context.Context, filter domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error) {
	query := `
		SELECT id, status, strategy, policy_id, approval_policy_ref, actor_type, actor_id, actor_name, actor_roles, actor_teams,
			ai_client_id, ai_client_name, skill_id, tool_name, risk_level, requires_approval, resource_scope, tool_input, related_ids, output,
			summary, request_id, source_ip, decided_by, decided_by_name, decided_at, decision_comment, expires_at, created_at, updated_at
		FROM ai_gateway_approval_requests
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filter.ID != "" {
		query += " AND id = ?"
		args = append(args, filter.ID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.ActorType != "" {
		query += " AND actor_type = ?"
		args = append(args, filter.ActorType)
	}
	if filter.ActorID != "" {
		query += " AND actor_id = ?"
		args = append(args, filter.ActorID)
	}
	if filter.AIClientID != "" {
		query += " AND ai_client_id = ?"
		args = append(args, filter.AIClientID)
	}
	if filter.SkillID != "" {
		query += " AND skill_id = ?"
		args = append(args, filter.SkillID)
	}
	if filter.ToolName != "" {
		query += " AND tool_name = ?"
		args = append(args, filter.ToolName)
	}
	if filter.RiskLevel != "" {
		query += " AND risk_level = ?"
		args = append(args, string(filter.RiskLevel))
	}
	if filter.Strategy != "" {
		query += " AND strategy = ?"
		args = append(args, filter.Strategy)
	}
	if filter.From != nil {
		query += " AND created_at >= ?"
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		query += " AND created_at <= ?"
		args = append(args, *filter.To)
	}
	if filter.ExpiresBefore != nil {
		query += " AND expires_at IS NOT NULL AND expires_at <= ?"
		args = append(args, *filter.ExpiresBefore)
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query += " ORDER BY created_at DESC, id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanApprovalRequestRows(rows)
}

func (r *Repository) UpdateApprovalRequest(ctx context.Context, requestID string, update domainaigateway.ApprovalRequestUpdate) (domainaigateway.ApprovalRequest, error) {
	relatedIDs, output, err := marshalApprovalRequestUpdateJSON(update.RelatedIDs, update.Output)
	if err != nil {
		return domainaigateway.ApprovalRequest{}, err
	}
	if update.UpdatedAt.IsZero() {
		update.UpdatedAt = time.Now().UTC()
	}
	expectedStatus := update.ExpectedStatus
	if expectedStatus == "" {
		expectedStatus = "pending"
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_gateway_approval_requests
		SET status = ?, summary = ?, related_ids = ?, output = ?, decided_by = ?, decided_by_name = ?, decided_at = ?, decision_comment = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, update.Status, update.Summary, relatedIDs, output, nullableString(update.DecidedBy), nullableString(update.DecidedByName), update.DecidedAt, nullableString(update.DecisionComment), update.UpdatedAt, requestID, expectedStatus)
	if result.Error != nil {
		return domainaigateway.ApprovalRequest{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainaigateway.ApprovalRequest{}, apperrors.ErrInvalidArgument
	}
	return r.GetApprovalRequest(ctx, requestID)
}

func (r *Repository) ExpirePendingApprovalRequests(ctx context.Context, at time.Time) ([]domainaigateway.ApprovalRequest, error) {
	pending, err := r.ListApprovalRequests(ctx, domainaigateway.ApprovalRequestFilter{
		Status:        "pending",
		ExpiresBefore: &at,
		Limit:         500,
	})
	if err != nil {
		return nil, err
	}
	expired := make([]domainaigateway.ApprovalRequest, 0, len(pending))
	now := time.Now().UTC()
	for _, item := range pending {
		updated, err := r.UpdateApprovalRequest(ctx, item.ID, domainaigateway.ApprovalRequestUpdate{
			Status:     "timeout",
			Summary:    "AI Gateway approval request timed out",
			RelatedIDs: item.RelatedIDs,
			Output:     item.Output,
			UpdatedAt:  now,
		})
		if err != nil {
			if errors.Is(err, apperrors.ErrInvalidArgument) {
				continue
			}
			return nil, err
		}
		expired = append(expired, updated)
	}
	return expired, nil
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

func scanAuditLogRows(rows *sql.Rows) ([]domainaigateway.AuditLog, error) {
	items := make([]domainaigateway.AuditLog, 0)
	for rows.Next() {
		item, err := scanAuditLogScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanAuditLogScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.AuditLog, error) {
	var item domainaigateway.AuditLog
	var actorName, aiClientID, aiClientName, skillID, toolName, riskLevel, requestID, sourceIP sql.NullString
	var resourceScope, metadata []byte
	if err := scanner.Scan(
		&item.ID,
		&item.ActorType,
		&item.ActorID,
		&actorName,
		&aiClientID,
		&aiClientName,
		&skillID,
		&toolName,
		&riskLevel,
		&resourceScope,
		&item.Action,
		&item.Result,
		&item.Summary,
		&requestID,
		&sourceIP,
		&metadata,
		&item.CreatedAt,
	); err != nil {
		return domainaigateway.AuditLog{}, err
	}
	item.ActorName = actorName.String
	item.AIClientID = aiClientID.String
	item.AIClientName = aiClientName.String
	item.SkillID = skillID.String
	item.ToolName = toolName.String
	item.RiskLevel = domainaigateway.RiskLevel(riskLevel.String)
	item.RequestID = requestID.String
	item.SourceIP = sourceIP.String
	unmarshalJSON(resourceScope, &item.ResourceScope)
	unmarshalJSON(metadata, &item.Metadata)
	if item.ResourceScope == nil {
		item.ResourceScope = map[string]any{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanRateLimitCounter(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.RateLimitCounter, error) {
	var item domainaigateway.RateLimitCounter
	var actorType, actorID, aiClientID, toolName sql.NullString
	var metadata []byte
	if err := scanner.Scan(
		&item.Key,
		&item.PolicyID,
		&item.Scope,
		&actorType,
		&actorID,
		&aiClientID,
		&toolName,
		&item.WindowStart,
		&item.WindowEnd,
		&item.Limit,
		&item.Count,
		&metadata,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainaigateway.RateLimitCounter{}, err
	}
	item.ActorType = actorType.String
	item.ActorID = actorID.String
	item.AIClientID = aiClientID.String
	item.ToolName = toolName.String
	unmarshalJSON(metadata, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanRateLimitState(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.RateLimitState, error) {
	var item domainaigateway.RateLimitState
	var actorType, actorID, aiClientID, toolName sql.NullString
	var metadata []byte
	var retryAfterSeconds float64
	if err := scanner.Scan(
		&item.Key,
		&item.PolicyID,
		&item.Scope,
		&actorType,
		&actorID,
		&aiClientID,
		&toolName,
		&item.Limit,
		&item.Burst,
		&item.IntervalSeconds,
		&item.TAT,
		&item.Allowed,
		&retryAfterSeconds,
		&metadata,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainaigateway.RateLimitState{}, err
	}
	item.ActorType = actorType.String
	item.ActorID = actorID.String
	item.AIClientID = aiClientID.String
	item.ToolName = toolName.String
	if retryAfterSeconds > 0 {
		item.RetryAfter = time.Duration(retryAfterSeconds * float64(time.Second))
	}
	unmarshalJSON(metadata, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanApprovalRequestRows(rows *sql.Rows) ([]domainaigateway.ApprovalRequest, error) {
	items := make([]domainaigateway.ApprovalRequest, 0)
	for rows.Next() {
		item, err := scanApprovalRequestScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanApprovalRequest(row *sql.Row) (domainaigateway.ApprovalRequest, error) {
	item, err := scanApprovalRequestScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.ApprovalRequest{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanApprovalRequestScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.ApprovalRequest, error) {
	var item domainaigateway.ApprovalRequest
	var policyID, approvalPolicyRef, actorName, aiClientID, aiClientName, skillID, requestID, sourceIP, decidedBy, decidedByName, decisionComment sql.NullString
	var decidedAt, expiresAt sql.NullTime
	var actorRoles, actorTeams, resourceScope, toolInput, relatedIDs, output []byte
	var riskLevel string
	if err := scanner.Scan(
		&item.ID,
		&item.Status,
		&item.Strategy,
		&policyID,
		&approvalPolicyRef,
		&item.ActorType,
		&item.ActorID,
		&actorName,
		&actorRoles,
		&actorTeams,
		&aiClientID,
		&aiClientName,
		&skillID,
		&item.ToolName,
		&riskLevel,
		&item.RequiresApproval,
		&resourceScope,
		&toolInput,
		&relatedIDs,
		&output,
		&item.Summary,
		&requestID,
		&sourceIP,
		&decidedBy,
		&decidedByName,
		&decidedAt,
		&decisionComment,
		&expiresAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainaigateway.ApprovalRequest{}, err
	}
	item.PolicyID = policyID.String
	item.ApprovalPolicyRef = approvalPolicyRef.String
	item.ActorName = actorName.String
	item.AIClientID = aiClientID.String
	item.AIClientName = aiClientName.String
	item.SkillID = skillID.String
	item.RiskLevel = domainaigateway.RiskLevel(riskLevel)
	item.RequestID = requestID.String
	item.SourceIP = sourceIP.String
	item.DecidedBy = decidedBy.String
	item.DecidedByName = decidedByName.String
	item.DecisionComment = decisionComment.String
	item.DecidedAt = nullTimePointer(decidedAt)
	item.ExpiresAt = nullTimePointer(expiresAt)
	unmarshalJSON(actorRoles, &item.ActorRoles)
	unmarshalJSON(actorTeams, &item.ActorTeams)
	unmarshalJSON(resourceScope, &item.ResourceScope)
	unmarshalJSON(toolInput, &item.ToolInput)
	unmarshalJSON(relatedIDs, &item.RelatedIDs)
	unmarshalJSON(output, &item.Output)
	if item.ActorRoles == nil {
		item.ActorRoles = []string{}
	}
	if item.ActorTeams == nil {
		item.ActorTeams = []string{}
	}
	if item.ResourceScope == nil {
		item.ResourceScope = map[string]any{}
	}
	if item.ToolInput == nil {
		item.ToolInput = map[string]any{}
	}
	if item.RelatedIDs == nil {
		item.RelatedIDs = map[string]any{}
	}
	if item.Output == nil {
		item.Output = map[string]any{}
	}
	return item, nil
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
	decodeGatewayTokenFields(scopes, permissionKeys, metadata, expiresAt, lastUsedAt, revokedAt, &item.Scopes, &item.PermissionKeys, &item.Metadata, &item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt)
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

func scanServiceAccountTokenRows(rows *sql.Rows) ([]domainaigateway.ServiceAccountToken, error) {
	items := make([]domainaigateway.ServiceAccountToken, 0)
	for rows.Next() {
		item, err := scanServiceAccountTokenScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
	decodeGatewayTokenFields(scopes, permissionKeys, metadata, expiresAt, lastUsedAt, revokedAt, &item.Scopes, &item.PermissionKeys, &item.Metadata, &item.ExpiresAt, &item.LastUsedAt, &item.RevokedAt)
	return item, nil
}

func decodeGatewayTokenFields(scopesJSON, permissionsJSON, metadataJSON []byte, expires, lastUsed, revoked sql.NullTime, scopes, permissions *[]string, metadata *map[string]any, expiresAt, lastUsedAt, revokedAt **time.Time) {
	*expiresAt = nullTimePointer(expires)
	*lastUsedAt = nullTimePointer(lastUsed)
	*revokedAt = nullTimePointer(revoked)
	unmarshalJSON(scopesJSON, scopes)
	unmarshalJSON(permissionsJSON, permissions)
	unmarshalJSON(metadataJSON, metadata)
	if *scopes == nil {
		*scopes = []string{}
	}
	if *permissions == nil {
		*permissions = []string{}
	}
	if *metadata == nil {
		*metadata = map[string]any{}
	}
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

func marshalApprovalRequestJSON(actorRoles, actorTeams []string, resourceScope, toolInput, relatedIDs map[string]any, output any) (string, string, string, string, string, string, error) {
	actorRolesRaw, err := json.Marshal(emptyStringSlice(actorRoles))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal actor roles: %w", err)
	}
	actorTeamsRaw, err := json.Marshal(emptyStringSlice(actorTeams))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal actor teams: %w", err)
	}
	resourceScopeRaw, err := json.Marshal(emptyMap(resourceScope))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal resource scope: %w", err)
	}
	toolInputRaw, err := json.Marshal(emptyMap(toolInput))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal tool input: %w", err)
	}
	relatedIDsRaw, err := json.Marshal(emptyMap(relatedIDs))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal related ids: %w", err)
	}
	outputRaw, err := json.Marshal(emptyAnyMap(output))
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("marshal output: %w", err)
	}
	return string(actorRolesRaw), string(actorTeamsRaw), string(resourceScopeRaw), string(toolInputRaw), string(relatedIDsRaw), string(outputRaw), nil
}

func marshalApprovalRequestUpdateJSON(relatedIDs map[string]any, output any) (string, string, error) {
	relatedIDsRaw, err := json.Marshal(emptyMap(relatedIDs))
	if err != nil {
		return "", "", fmt.Errorf("marshal related ids: %w", err)
	}
	outputRaw, err := json.Marshal(emptyAnyMap(output))
	if err != nil {
		return "", "", fmt.Errorf("marshal output: %w", err)
	}
	return string(relatedIDsRaw), string(outputRaw), nil
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

func emptyAnyMap(value any) any {
	if value == nil {
		return map[string]any{}
	}
	return value
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
