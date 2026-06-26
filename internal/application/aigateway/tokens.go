package aigateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
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

const LLMRelayTokenPurpose = "llm-relay"

type LLMRelayAccessRequest struct {
	Model        string
	ProviderKind string
	UpstreamID   string
	SourceIP     string
}

func (s *Service) hasRuntimePermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) (bool, error) {
	if s.permissions == nil {
		return false, nil
	}
	return s.permissions.HasPermission(ctx, principal, permissionKey)
}
func (s *Service) ListPersonalAccessTokens(ctx context.Context, principal domainidentity.Principal, req domainaigateway.PersonalAccessTokenListRequest) ([]domainaigateway.PersonalAccessToken, error) {
	repo := s.personalTokenRepository()
	if repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	userID := strings.TrimSpace(req.UserID)
	if scope == "all" || userID != "" {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
			return nil, err
		}
		items, err := repo.ListAllPersonalAccessTokens(ctx)
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
	return repo.ListPersonalAccessTokens(ctx, principal.UserID)
}
func (s *Service) CreatePersonalAccessToken(ctx context.Context, principal domainidentity.Principal, input domainaigateway.PersonalAccessTokenInput) (domainaigateway.CreatedPersonalAccessToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	repo := s.personalTokenRepository()
	if repo == nil {
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
	metadata, err := s.normalizeGatewayTokenMetadataForPrincipal(ctx, principal, input.Metadata)
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
		Metadata:       metadata,
		ExpiresAt:      input.ExpiresAt,
		CreatedBy:      principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	created, err := repo.CreatePersonalAccessToken(ctx, item)
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
	repo := s.personalTokenRepository()
	if repo == nil {
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
		items, listErr := repo.ListAllPersonalAccessTokens(ctx)
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
	if err := repo.RevokePersonalAccessToken(ctx, ownerID, tokenID); err != nil {
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
	repo := s.personalTokenRepository()
	if repo == nil {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return domainaigateway.CreatedPersonalAccessToken{}, fmt.Errorf("%w: token ID is required", apperrors.ErrInvalidArgument)
	}
	items, err := repo.ListPersonalAccessTokens(ctx, principal.UserID)
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
	if err := s.ensureRelayDebugTokenMetadataAllowed(ctx, principal, previous.Metadata); err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
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
	created, err := repo.CreatePersonalAccessToken(ctx, replacement)
	if err != nil {
		return domainaigateway.CreatedPersonalAccessToken{}, err
	}
	if err := repo.RevokePersonalAccessToken(ctx, principal.UserID, previous.ID); err != nil {
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
	repo := s.serviceAccountRepository()
	if repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	return repo.ListServiceAccounts(ctx)
}
func (s *Service) CreateServiceAccount(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ServiceAccountInput) (domainaigateway.ServiceAccount, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.ServiceAccount{}, err
	}
	repo := s.serviceAccountRepository()
	if repo == nil {
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
	created, err := repo.CreateServiceAccount(ctx, item)
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
	repo := s.serviceAccountRepository()
	if repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	return repo.ListAllServiceAccountTokens(ctx)
}
func (s *Service) CreateServiceAccountToken(ctx context.Context, principal domainidentity.Principal, serviceAccountID string, input domainaigateway.ServiceAccountTokenInput) (domainaigateway.CreatedServiceAccountToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	repo := s.serviceAccountRepository()
	if repo == nil {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	account, err := repo.GetServiceAccount(ctx, strings.TrimSpace(serviceAccountID))
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
	metadata, err := s.normalizeGatewayTokenMetadataForPrincipal(ctx, principal, input.Metadata)
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
		Metadata:         metadata,
		ExpiresAt:        input.ExpiresAt,
		CreatedBy:        principal.UserID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	created, err := repo.CreateServiceAccountToken(ctx, item)
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
	repo := s.serviceAccountRepository()
	if repo == nil {
		return fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := repo.RevokeServiceAccountToken(ctx, strings.TrimSpace(tokenID)); err != nil {
		return err
	}
	_ = s.recordTokenAudit(ctx, principal, "ai_gateway.service_token.revoke", "success", "revoked service account token", map[string]any{"tokenId": strings.TrimSpace(tokenID)})
	return nil
}
func (s *Service) RotateServiceAccountToken(ctx context.Context, principal domainidentity.Principal, tokenID string, input domainaigateway.TokenRotationInput) (domainaigateway.CreatedServiceAccountToken, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayManage); err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	repo := s.serviceAccountRepository()
	if repo == nil {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: AI Gateway repository is not configured", apperrors.ErrInvalidArgument)
	}
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: token ID is required", apperrors.ErrInvalidArgument)
	}
	items, err := repo.ListAllServiceAccountTokens(ctx)
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
	account, err := repo.GetServiceAccount(ctx, previous.ServiceAccountID)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if strings.TrimSpace(account.Status) != "active" {
		return domainaigateway.CreatedServiceAccountToken{}, fmt.Errorf("%w: service account is not active", apperrors.ErrInvalidArgument)
	}
	if err := s.ensureRelayDebugTokenMetadataAllowed(ctx, principal, previous.Metadata); err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
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
	created, err := repo.CreateServiceAccountToken(ctx, replacement)
	if err != nil {
		return domainaigateway.CreatedServiceAccountToken{}, err
	}
	if err := repo.RevokeServiceAccountToken(ctx, previous.ID); err != nil {
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

func (s *Service) AuthorizeLLMRelayToken(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayAccessRequest) (domainaigateway.LLMTokenMetadata, error) {
	if !isGatewayOpaqueTokenKind(accessCtx.TokenKind) {
		return domainaigateway.LLMTokenMetadata{}, fmt.Errorf("%w: llm relay requires a gateway PAT or SAT", apperrors.ErrAccessDenied)
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayInvoke); err != nil {
		return domainaigateway.LLMTokenMetadata{}, err
	}
	metadata, err := ParseLLMTokenMetadata(accessCtx.Metadata)
	if err != nil {
		return domainaigateway.LLMTokenMetadata{}, err
	}
	if !llmRelayEnabledByToken(metadata, accessCtx.Scopes) {
		return domainaigateway.LLMTokenMetadata{}, fmt.Errorf("%w: llm relay token purpose or scope is required", apperrors.ErrAccessDenied)
	}
	if strings.TrimSpace(req.Model) != "" && !relayAllowListAllows(metadata.AllowedModels, req.Model, false) {
		return domainaigateway.LLMTokenMetadata{}, fmt.Errorf("%w: model is not allowed by token metadata", apperrors.ErrAccessDenied)
	}
	if strings.TrimSpace(req.ProviderKind) != "" && !relayAllowListAllows(metadata.AllowedProviderKinds, req.ProviderKind, true) {
		return domainaigateway.LLMTokenMetadata{}, fmt.Errorf("%w: provider kind is not allowed by token metadata", apperrors.ErrAccessDenied)
	}
	if strings.TrimSpace(req.UpstreamID) != "" && !relayAllowListAllows(metadata.AllowedUpstreamIDs, req.UpstreamID, false) {
		return domainaigateway.LLMTokenMetadata{}, fmt.Errorf("%w: upstream is not allowed by token metadata", apperrors.ErrAccessDenied)
	}
	if err := authorizeRelaySourceIP(metadata.AllowedIPCIDRs, req.SourceIP); err != nil {
		return domainaigateway.LLMTokenMetadata{}, err
	}
	if !relayTeamPolicyAllows(principal.Teams, metadata.AllowedTeams, metadata.DeniedTeams) {
		return domainaigateway.LLMTokenMetadata{}, fmt.Errorf("%w: team is not allowed by token metadata", apperrors.ErrAccessDenied)
	}
	return metadata, nil
}

func ParseLLMTokenMetadata(metadata map[string]any) (domainaigateway.LLMTokenMetadata, error) {
	var out domainaigateway.LLMTokenMetadata
	var err error
	out.Purpose, err = metadataString(metadata, "purpose")
	if err != nil {
		return out, err
	}
	out.Purpose = strings.ToLower(out.Purpose)
	out.AllowedModels, err = metadataStringList(metadata, "allowedModels", false)
	if err != nil {
		return out, err
	}
	out.AllowedProviderKinds, err = metadataStringList(metadata, "allowedProviderKinds", true)
	if err != nil {
		return out, err
	}
	out.AllowedProviderKinds = normalizeRelayProviderKindList(out.AllowedProviderKinds)
	out.AllowedUpstreamIDs, err = metadataStringList(metadata, "allowedUpstreamIds", false)
	if err != nil {
		return out, err
	}
	out.AllowedIPCIDRs, err = metadataCIDRList(metadata, "allowedIPCIDRs")
	if err != nil {
		return out, err
	}
	out.AllowedTeams, err = metadataStringList(metadata, "allowedTeams", false)
	if err != nil {
		return out, err
	}
	out.DeniedTeams, err = metadataStringList(metadata, "deniedTeams", false)
	if err != nil {
		return out, err
	}
	out.RateLimitProfileID, err = metadataString(metadata, "rateLimitProfileId")
	if err != nil {
		return out, err
	}
	out.AllowRouteTrace = metadataBool(metadata, "allowRouteTrace")
	out.AllowUpstreamSelection = metadataBool(metadata, "allowUpstreamSelection")
	return out, nil
}

func (s *Service) normalizeGatewayTokenMetadataForPrincipal(ctx context.Context, principal domainidentity.Principal, metadata map[string]any) (map[string]any, error) {
	out, err := NormalizeGatewayTokenMetadata(metadata)
	if err != nil {
		return nil, err
	}
	return out, s.ensureRelayDebugTokenMetadataAllowed(ctx, principal, out)
}

func (s *Service) ensureRelayDebugTokenMetadataAllowed(ctx context.Context, principal domainidentity.Principal, metadata map[string]any) error {
	if !relayTokenMetadataHasDebugGrant(metadata) {
		return nil
	}
	hasManage, err := s.hasRuntimePermission(ctx, principal, appaccess.PermAIGatewayRelayManage)
	if err != nil {
		return err
	}
	if !hasManage {
		return fmt.Errorf("%w: relay debug token metadata requires %s", apperrors.ErrAccessDenied, appaccess.PermAIGatewayRelayManage)
	}
	return nil
}

func NormalizeGatewayTokenMetadata(metadata map[string]any) (map[string]any, error) {
	out := copyMap(metadata)
	parsed, err := ParseLLMTokenMetadata(out)
	if err != nil {
		return nil, err
	}
	if _, exists := out["purpose"]; exists {
		setStringMetadata(out, "purpose", parsed.Purpose)
	}
	if _, exists := out["allowedModels"]; exists {
		setStringSliceMetadata(out, "allowedModels", parsed.AllowedModels)
	}
	if _, exists := out["allowedProviderKinds"]; exists {
		setStringSliceMetadata(out, "allowedProviderKinds", parsed.AllowedProviderKinds)
	}
	if _, exists := out["allowedUpstreamIds"]; exists {
		setStringSliceMetadata(out, "allowedUpstreamIds", parsed.AllowedUpstreamIDs)
	}
	if _, exists := out["allowedIPCIDRs"]; exists {
		setStringSliceMetadata(out, "allowedIPCIDRs", parsed.AllowedIPCIDRs)
	}
	if _, exists := out["allowedTeams"]; exists {
		setStringSliceMetadata(out, "allowedTeams", parsed.AllowedTeams)
	}
	if _, exists := out["deniedTeams"]; exists {
		setStringSliceMetadata(out, "deniedTeams", parsed.DeniedTeams)
	}
	if _, exists := out["rateLimitProfileId"]; exists {
		setStringMetadata(out, "rateLimitProfileId", parsed.RateLimitProfileID)
	}
	if _, exists := out["allowRouteTrace"]; exists {
		setBoolMetadata(out, "allowRouteTrace", parsed.AllowRouteTrace)
	}
	if _, exists := out["allowUpstreamSelection"]; exists {
		setBoolMetadata(out, "allowUpstreamSelection", parsed.AllowUpstreamSelection)
	}
	return out, nil
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

func metadataString(metadata map[string]any, key string) (string, error) {
	raw, exists := metadata[key]
	if !exists || raw == nil {
		return "", nil
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value), nil
	case fmt.Stringer:
		return strings.TrimSpace(value.String()), nil
	case []string, []any, map[string]any, map[string]string:
		return "", fmt.Errorf("%w: token metadata %s must be a string", apperrors.ErrInvalidArgument, key)
	default:
		return strings.TrimSpace(fmt.Sprint(value)), nil
	}
}

func metadataBool(metadata map[string]any, key string) bool {
	raw, exists := metadata[key]
	if !exists || raw == nil {
		return false
	}
	return boolFromAny(raw)
}

func metadataStringList(metadata map[string]any, key string, lower bool) ([]string, error) {
	raw, exists := metadata[key]
	if !exists || raw == nil {
		return nil, nil
	}
	values := make([]string, 0)
	switch typed := raw.(type) {
	case string:
		values = append(values, splitMetadataListString(typed)...)
	case []string:
		values = append(values, typed...)
	case []any:
		for _, item := range typed {
			value, err := metadataListItemString(item, key)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
	default:
		return nil, fmt.Errorf("%w: token metadata %s must be a string array", apperrors.ErrInvalidArgument, key)
	}
	return normalizeMetadataStringList(values, lower), nil
}

func metadataListItemString(raw any, key string) (string, error) {
	if raw == nil {
		return "", nil
	}
	switch value := raw.(type) {
	case string:
		return value, nil
	case fmt.Stringer:
		return value.String(), nil
	case []string, []any, map[string]any, map[string]string:
		return "", fmt.Errorf("%w: token metadata %s must contain strings", apperrors.ErrInvalidArgument, key)
	default:
		return fmt.Sprint(value), nil
	}
}

func splitMetadataListString(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, part)
	}
	return out
}

func normalizeMetadataStringList(values []string, lower bool) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if lower {
			value = strings.ToLower(value)
		}
		if value == "" || slices.Contains(out, value) {
			continue
		}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeRelayProviderKindList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeRelayProviderKind(value)
		if normalized == "" {
			normalized = strings.ToLower(strings.TrimSpace(value))
		}
		if normalized == "" || slices.Contains(out, normalized) {
			continue
		}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func metadataCIDRList(metadata map[string]any, key string) ([]string, error) {
	values, err := metadataStringList(metadata, key, false)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid token metadata %s entry %s", apperrors.ErrInvalidArgument, key, value)
		}
		normalized := network.String()
		if !slices.Contains(out, normalized) {
			out = append(out, normalized)
		}
	}
	sort.Strings(out)
	return out, nil
}

func setStringMetadata(metadata map[string]any, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		delete(metadata, key)
		return
	}
	metadata[key] = value
}

func setStringSliceMetadata(metadata map[string]any, key string, values []string) {
	if len(values) == 0 {
		delete(metadata, key)
		return
	}
	metadata[key] = append([]string(nil), values...)
}

func setBoolMetadata(metadata map[string]any, key string, value bool) {
	if !value {
		delete(metadata, key)
		return
	}
	metadata[key] = true
}

func relayTokenMetadataHasDebugGrant(metadata map[string]any) bool {
	return metadataBool(metadata, "allowRouteTrace") || metadataBool(metadata, "allowUpstreamSelection")
}

func isGatewayOpaqueTokenKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "personal_access_token", "service_account_token":
		return true
	default:
		return false
	}
}

func llmRelayEnabledByToken(metadata domainaigateway.LLMTokenMetadata, scopes []string) bool {
	if strings.EqualFold(strings.TrimSpace(metadata.Purpose), LLMRelayTokenPurpose) {
		return true
	}
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope == "relay" || scope == LLMRelayTokenPurpose || scope == "ai_gateway.relay" || scope == "ai_gateway:relay" {
			return true
		}
	}
	return false
}

func relayAllowListAllows(allowed []string, value string, lower bool) bool {
	if len(allowed) == 0 {
		return true
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if lower {
		if normalized := normalizeRelayProviderKind(value); normalized != "" {
			value = normalized
		} else {
			value = strings.ToLower(value)
		}
	}
	return slices.Contains(allowed, value)
}

func relayTeamPolicyAllows(principalTeams, allowedTeams, deniedTeams []string) bool {
	teams := normalizeStringSlice(principalTeams)
	if len(intersectStringSlices(teams, deniedTeams)) > 0 {
		return false
	}
	if len(allowedTeams) == 0 {
		return true
	}
	return len(intersectStringSlices(teams, allowedTeams)) > 0
}

func authorizeRelaySourceIP(allowedCIDRs []string, sourceIP string) error {
	if len(allowedCIDRs) == 0 {
		return nil
	}
	ipText := normalizeSourceIP(sourceIP)
	ip := net.ParseIP(ipText)
	if ip == nil {
		return fmt.Errorf("%w: source IP is required by token metadata", apperrors.ErrAccessDenied)
	}
	for _, cidr := range allowedCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("%w: invalid token metadata allowedIPCIDRs entry %s", apperrors.ErrAccessDenied, cidr)
		}
		if network.Contains(ip) {
			return nil
		}
	}
	return fmt.Errorf("%w: source IP is not allowed by token metadata", apperrors.ErrAccessDenied)
}

func normalizeSourceIP(sourceIP string) string {
	sourceIP = strings.TrimSpace(sourceIP)
	if index := strings.Index(sourceIP, ","); index >= 0 {
		sourceIP = strings.TrimSpace(sourceIP[:index])
	}
	if host, _, err := net.SplitHostPort(sourceIP); err == nil {
		sourceIP = host
	}
	sourceIP = strings.TrimPrefix(sourceIP, "[")
	sourceIP = strings.TrimSuffix(sourceIP, "]")
	return sourceIP
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
