package aigateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) hasRuntimePermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) (bool, error) {
	if s.permissions == nil {
		return false, nil
	}
	return s.permissions.HasPermission(ctx, principal, permissionKey)
}
func (s *Service) ListPersonalAccessTokens(ctx context.Context, principal domainidentity.Principal, req domainaigateway.PersonalAccessTokenListRequest) ([]domainaigateway.PersonalAccessToken, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	userID := strings.TrimSpace(req.UserID)
	if scope == "all" || userID != "" {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
			return nil, err
		}
		items, err := s.repo.ListAllPersonalAccessTokens(ctx)
		if err != nil {
			return nil, err
		}
		if userID == "" {
			return items, nil
		}
		filtered := make([]domainaigateway.PersonalAccessToken, 0)
		for _, item := range items {
			if item.UserID == userID {
				filtered = append(filtered, item)
			}
		}
		return filtered, nil
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayView); err != nil {
		return nil, err
	}
	return s.repo.ListPersonalAccessTokens(ctx, principal.UserID)
}
func (s *Service) CreatePersonalAccessToken(ctx context.Context, principal domainidentity.Principal, input domainaigateway.PersonalAccessTokenInput) (domainaigateway.CreatedPersonalAccessToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	if s.repo == nil {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: token name is required", apperrors.ErrInvalidArgument)
	}
	permissionKeys, err := s.normalizeRequestedPermissionKeys(ctx, principal, input.PermissionKeys)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	value, prefix, err := generateOpaqueToken(domainaigateway.PersonalAccessTokenPrefix)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	now := time.Now().UTC()
	item := domainaigateway.PersonalAccessToken{
		ID:             uuid.NewString(),
		UserID:         principal.UserID,
		Name:           name,
		TokenHash:      domainaigateway.HashToken(value),
		TokenPrefix:    prefix,
		Scopes:         normalizeStringSlice(input.Scopes),
		PermissionKeys: permissionKeys,
		Metadata:       emptyMap(input.Metadata),
		ExpiresAt:      input.ExpiresAt,
		CreatedBy:      principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, err := s.repo.CreatePersonalAccessToken(ctx, item)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.personal_token.create", "success", "created personal access token", map[string]any{
		"tokenId":        created.ID,
		"tokenPrefix":    created.TokenPrefix,
		"permissionKeys": created.PermissionKeys,
		"expiresAt":      created.ExpiresAt,
	})
	return domainaigateway.CreatedPersonalAccessToken{Token: created, Value: value}, nil
}
func (s *Service) RevokePersonalAccessToken(ctx context.Context, principal domainidentity.Principal, tokenID string) error {
	if s.repo == nil {
		return fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return fmt.Errorf("%w: token ID is required", apperrors.ErrInvalidArgument)
	}
	hasManage, err := s.hasRuntimePermission(ctx, principal, appaccess.PermAIGatewayManage)
	if err != nil {
		return err
	}
	ownerID := principal.UserID
	if hasManage {
		items, listErr := s.repo.ListAllPersonalAccessTokens(ctx)
		if listErr != nil {
			return listErr
		}
		ownerID = ""
		for _, item := range items {
			if item.ID == tokenID {
				ownerID = item.UserID
				break
			}
		}
		if ownerID == "" {
			return apperrors.ErrNotFound
		}
	} else if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return err
	}
	if err := s.repo.RevokePersonalAccessToken(ctx, ownerID, tokenID); err != nil {
		return err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.personal_token.revoke", "success", "revoked personal access token", map[string]any{
		"tokenId":      tokenID,
		"tokenOwnerId": ownerID,
	})
	return nil
}
func (s *Service) RotatePersonalAccessToken(ctx context.Context, principal domainidentity.Principal, tokenID string, input domainaigateway.TokenRotationInput) (domainaigateway.CreatedPersonalAccessToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	if s.repo == nil {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: token ID is required", apperrors.ErrInvalidArgument)
	}
	items, err := s.repo.ListPersonalAccessTokens(ctx, principal.UserID)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	var previous domainaigateway.PersonalAccessToken
	for _, item := range items {
		if item.ID == tokenID {
			previous = item
			break
		}
	}
	if previous.ID == "" {
		return domainaigateway.CreatedPersonalAccessToken{}, apperrors.ErrNotFound
	}
	if previous.RevokedAt != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: token is revoked", apperrors.ErrInvalidArgument)
	}
	permissionKeys := normalizeStringSlice(previous.PermissionKeys)
	if len(permissionKeys) > 0 {
		permissionKeys, err = s.normalizeRequestedPermissionKeys(ctx, principal, permissionKeys)
		if err != nil {
			return domainaigateway.CreatedPersonalAccessToken{}, err
		}
	}
	expiresAt, err := rotatedTokenExpiresAt(input.ExpiresAt, previous.ExpiresAt, time.Now().UTC())
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	value, prefix, err := generateOpaqueToken(domainaigateway.PersonalAccessTokenPrefix)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	now := time.Now().UTC()
	replacement := domainaigateway.PersonalAccessToken{
		ID:             uuid.NewString(),
		UserID:         principal.UserID,
		Name:           previous.Name,
		TokenHash:      domainaigateway.HashToken(value),
		TokenPrefix:    prefix,
		Scopes:         normalizeStringSlice(previous.Scopes),
		PermissionKeys: permissionKeys,
		Metadata:       copyMap(previous.Metadata),
		ExpiresAt:      expiresAt,
		CreatedBy:      principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, err := s.repo.CreatePersonalAccessToken(ctx, replacement)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	if err := s.repo.RevokePersonalAccessToken(ctx, principal.UserID, previous.ID); err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.personal_token.rotate", "success", "rotated personal access token", map[string]any{
		"previousTokenId": previous.ID,
		"tokenId":         created.ID,
		"tokenPrefix":     created.TokenPrefix,
		"permissionKeys":  created.PermissionKeys,
		"expiresAt":       created.ExpiresAt,
	})
	return domainaigateway.CreatedPersonalAccessToken{Token: created, Value: value}, nil
}
func (s *Service) ListServiceAccounts(ctx context.Context, principal domainidentity.Principal) ([]domainaigateway.ServiceAccount, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	return s.repo.ListServiceAccounts(ctx)
}
func (s *Service) CreateServiceAccount(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ServiceAccountInput) (domainaigateway.ServiceAccount, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	if s.repo == nil {
		return domainaigateway.ServiceAccount{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainaigateway.ServiceAccount{}, fmt.Errorf("%w: service account name is required", apperrors.ErrInvalidArgument)
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	now := time.Now().UTC()
	item := domainaigateway.ServiceAccount{
		ID:            id,
		Name:          name,
		Description:   strings.TrimSpace(input.Description),
		Status:        status,
		OwnerUserID:   strings.TrimSpace(input.OwnerUserID),
		RoleIDs:       normalizeStringSlice(input.RoleIDs),
		TeamIDs:       normalizeStringSlice(input.TeamIDs),
		ScopeGrantIDs: normalizeStringSlice(input.ScopeGrantIDs),
		Metadata:      emptyMap(input.Metadata),
		CreatedBy:     principal.UserID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	created, err := s.repo.CreateServiceAccount(ctx, item)
	if err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.service_account.create", "success", "created service account", map[string]any{"serviceAccountId": created.ID, "roleIds": created.RoleIDs})
	return created, nil
}
func (s *Service) ListServiceAccountTokens(ctx context.Context, principal domainidentity.Principal) ([]domainaigateway.ServiceAccountToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	return s.repo.ListAllServiceAccountTokens(ctx)
}
func (s *Service) CreateServiceAccountToken(ctx context.Context, principal domainidentity.Principal, serviceAccountID string, input domainaigateway.ServiceAccountTokenInput) (domainaigateway.CreatedServiceAccountToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if s.repo == nil {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	account, err := s.repo.GetServiceAccount(ctx, strings.TrimSpace(serviceAccountID))
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if strings.TrimSpace(account.Status) != "active" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: service account is not active", apperrors.ErrInvalidArgument)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: token name is required", apperrors.ErrInvalidArgument)
	}
	servicePrincipal := domainidentity.Principal{UserID: "service_account:" + account.ID, UserName: account.Name, Roles: account.RoleIDs, Teams: account.TeamIDs}
	permissionKeys, err := s.normalizeRequestedPermissionKeys(ctx, servicePrincipal, input.PermissionKeys)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	value, prefix, err := generateOpaqueToken(domainaigateway.ServiceAccountTokenPrefix)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	now := time.Now().UTC()
	item := domainaigateway.ServiceAccountToken{
		ID:               uuid.NewString(),
		ServiceAccountID: account.ID,
		Name:             name,
		TokenHash:        domainaigateway.HashToken(value),
		TokenPrefix:      prefix,
		Scopes:           normalizeStringSlice(input.Scopes),
		PermissionKeys:   permissionKeys,
		Metadata:         emptyMap(input.Metadata),
		ExpiresAt:        input.ExpiresAt,
		CreatedBy:        principal.UserID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	created, err := s.repo.CreateServiceAccountToken(ctx, item)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.service_token.create", "success", "created service account token", map[string]any{
		"serviceAccountId": account.ID,
		"tokenId":          created.ID,
		"tokenPrefix":      created.TokenPrefix,
		"permissionKeys":   created.PermissionKeys,
	})
	return domainaigateway.CreatedServiceAccountToken{Token: created, Value: value}, nil
}
func (s *Service) RevokeServiceAccountToken(ctx context.Context, principal domainidentity.Principal, tokenID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return err
	}
	if s.repo == nil {
		return fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.RevokeServiceAccountToken(ctx, strings.TrimSpace(tokenID)); err != nil {
		return err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.service_token.revoke", "success", "revoked service account token", map[string]any{"tokenId": strings.TrimSpace(tokenID)})
	return nil
}
func (s *Service) RotateServiceAccountToken(ctx context.Context, principal domainidentity.Principal, tokenID string, input domainaigateway.TokenRotationInput) (domainaigateway.CreatedServiceAccountToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if s.repo == nil {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: token ID is required", apperrors.ErrInvalidArgument)
	}
	items, err := s.repo.ListAllServiceAccountTokens(ctx)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	var previous domainaigateway.ServiceAccountToken
	for _, item := range items {
		if item.ID == tokenID {
			previous = item
			break
		}
	}
	if previous.ID == "" {
		return domainaigateway.CreatedServiceAccountToken{}, apperrors.ErrNotFound
	}
	if previous.RevokedAt != nil {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: token is revoked", apperrors.ErrInvalidArgument)
	}
	account, err := s.repo.GetServiceAccount(ctx, previous.ServiceAccountID)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if strings.TrimSpace(account.Status) != "active" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: service account is not active", apperrors.ErrInvalidArgument)
	}
	servicePrincipal := domainidentity.Principal{UserID: "service_account:" + account.ID, UserName: account.Name, Roles: account.RoleIDs, Teams: account.TeamIDs}
	permissionKeys := normalizeStringSlice(previous.PermissionKeys)
	if len(permissionKeys) > 0 {
		permissionKeys, err = s.normalizeRequestedPermissionKeys(ctx, servicePrincipal, permissionKeys)
		if err != nil {
			return domainaigateway.CreatedServiceAccountToken{}, err
		}
	}
	expiresAt, err := rotatedTokenExpiresAt(input.ExpiresAt, previous.ExpiresAt, time.Now().UTC())
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	value, prefix, err := generateOpaqueToken(domainaigateway.ServiceAccountTokenPrefix)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	now := time.Now().UTC()
	replacement := domainaigateway.ServiceAccountToken{
		ID:               uuid.NewString(),
		ServiceAccountID: account.ID,
		Name:             previous.Name,
		TokenHash:        domainaigateway.HashToken(value),
		TokenPrefix:      prefix,
		Scopes:           normalizeStringSlice(previous.Scopes),
		PermissionKeys:   permissionKeys,
		Metadata:         copyMap(previous.Metadata),
		ExpiresAt:        expiresAt,
		CreatedBy:        principal.UserID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	created, err := s.repo.CreateServiceAccountToken(ctx, replacement)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if err := s.repo.RevokeServiceAccountToken(ctx, previous.ID); err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.service_token.rotate", "success", "rotated service account token", map[string]any{
		"serviceAccountId": account.ID,
		"previousTokenId":  previous.ID,
		"tokenId":          created.ID,
		"tokenPrefix":      created.TokenPrefix,
		"permissionKeys":   created.PermissionKeys,
		"expiresAt":        created.ExpiresAt,
	})
	return domainaigateway.CreatedServiceAccountToken{Token: created, Value: value}, nil
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
func generateOpaqueToken(prefix string) (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate token entropy: %w", err)
	}
	value := prefix + base64.RawURLEncoding.EncodeToString(raw)
	tokenPrefix := value
	if len(tokenPrefix) > 20 {
		tokenPrefix = tokenPrefix[:20]
	}
	return value, tokenPrefix, nil
}
func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || slices.Contains(out, value) {
			continue
		}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
func intersectStringSlices(left, right []string) []string {
	normalizedRight := normalizeStringSlice(right)
	out := make([]string, 0)
	for _, value := range normalizeStringSlice(left) {
		if slices.Contains(normalizedRight, value) {
			out = append(out, value)
		}
	}
	return out
}
func emptyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	return values
}
func copyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
func rotatedTokenExpiresAt(requested, previous *time.Time, now time.Time) (*time.Time, error) {
	if requested != nil {
		if !requested.After(now) {
			return nil, fmt.Errorf("%w: replacement token expiration must be in the future", apperrors.ErrInvalidArgument)
		}
		next := requested.UTC()
		return &next, nil
	}
	if previous == nil {
		return nil, nil
	}
	if previous.After(now) {
		next := previous.UTC()
		return &next, nil
	}
	next := now.Add(rotatedTokenDefaultTTL).UTC()
	return &next, nil
}
