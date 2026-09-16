package identityprovider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// GetProviderUserMetadata previews mapped attributes without issuing credentials or authorizing a login.
func (s *Service) GetProviderUserMetadata(ctx context.Context, actor domainidentity.Principal, providerID, userID, clientID string) (sohaapi.IdentityProviderUserMetadata, error) {
	result := sohaapi.IdentityProviderUserMetadata{}
	provider, err := s.GetProvider(ctx, actor, providerID)
	if err != nil {
		return result, err
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, actor, appaccess.PermAccessUsersView); err != nil {
		return result, err
	}
	userID, clientID = strings.TrimSpace(userID), strings.TrimSpace(clientID)
	if userID == "" {
		return result, fmt.Errorf("%w: user ID is required", apperrors.ErrInvalidArgument)
	}
	principal, err := s.loadPrincipal(ctx, userID)
	if errors.Is(err, apperrors.ErrUnauthorized) {
		return result, fmt.Errorf("%w: selected user is unavailable or inactive", apperrors.ErrInvalidArgument)
	}
	if err != nil {
		return result, err
	}
	result = sohaapi.IdentityProviderUserMetadata{
		ProviderID: provider.ID, UserID: principal.UserID,
		Protocol: sohaapi.IdentityProviderUserMetadataProtocol(provider.Type), Attributes: map[string][]string{},
	}
	var client domainprovider.OIDCClient
	switch provider.Type {
	case domainprovider.ProviderTypeOIDC:
		if clientID == "" {
			return result, fmt.Errorf("%w: select an OIDC client", apperrors.ErrInvalidArgument)
		}
		client, err = s.repo.GetOIDCClient(ctx, clientID)
		if err != nil {
			return result, err
		}
		if client.ProviderID != provider.ID {
			return result, fmt.Errorf("%w: select a client belonging to this provider", apperrors.ErrInvalidArgument)
		}
		result.ClientID, result.Scopes = client.ID, client.AllowedScopes
		result.Attributes = previewOIDCAttributes(principal, client.AllowedScopes)
	case domainprovider.ProviderTypeSAML:
		repository, ok := s.repo.(samlProviderRepository)
		if !ok {
			return result, fmt.Errorf("%w: SAML provider repository is not configured", apperrors.ErrUnsupportedOperation)
		}
		serviceProvider, err := repository.GetSAMLServiceProvider(ctx, provider.ID)
		if err != nil {
			return result, err
		}
		result.Subject = samlNameID(principal, serviceProvider.NameIDFormat)
		result.Attributes = samlAttributes(principal, serviceProvider.AttributeMappings)
		for key, values := range result.Attributes {
			if values == nil {
				result.Attributes[key] = []string{}
			}
		}
	case domainprovider.ProviderTypeProxy:
		for key, value := range proxyIdentityHeaders(principal, provider) {
			result.Attributes[key] = []string{value}
		}
	default:
		return result, fmt.Errorf("%w: unsupported provider type", apperrors.ErrInvalidArgument)
	}
	s.recordAudit(ctx, actor, "identity.provider.user_metadata", "success", provider, client, map[string]any{"targetUserId": principal.UserID})
	return result, nil
}

func previewOIDCAttributes(principal domainidentity.Principal, scopes []string) map[string][]string {
	var claims oidcTokenClaims
	applyOIDCScopedClaims(&claims, principal, scopes)
	attributes := map[string][]string{"sub": {principal.UserID}}
	for key, value := range map[string]string{"name": claims.UserName, "email": claims.Email} {
		if value != "" {
			attributes[key] = []string{value}
		}
	}
	for key, values := range map[string][]string{"roles": claims.Roles, "teams": claims.Teams, "projects": claims.Projects, "tags": claims.Tags} {
		if len(values) > 0 {
			attributes[key] = values
		}
	}
	return attributes
}
