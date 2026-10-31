package identity

import (
	"context"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// CurrentGatewayMutationPrincipal rechecks the trusted original credential.
// SessionID is server provenance (ToolInvocationRequest excludes it from JSON),
// not a browser access token or a client-selected replacement session.
func (s *Service) CurrentGatewayMutationPrincipal(ctx context.Context, original domainidentity.Principal, sessionID string) (domainidentity.Principal, error) {
	if original.AccessTokenID != "" {
		return s.CurrentDelegatedExecutionPrincipal(ctx, original)
	}
	if original.UserID == "" || original.GovernedAI != nil || sessionID == "" || s.sessions == nil {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	session, err := s.sessions.GetAuthSessionByID(ctx, sessionID)
	if err != nil || session.UserID != original.UserID || session.Status != "active" || !session.ExpiresAt.After(time.Now()) || session.AuthzVersion < 1 {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	state, err := s.requireActiveAuthzState(ctx, original.UserID)
	if err != nil || state.AuthzVersion != session.AuthzVersion {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	var current domainidentity.Principal
	if session.ProviderType == "cli_v2" {
		current, err = s.cliSessionPrincipal(ctx, session)
	} else {
		current, err = s.CurrentPrincipal(ctx, original.UserID)
	}
	if err != nil {
		return current, err
	}
	ceiling, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, original)
	if err != nil {
		return current, err
	}
	return appaccess.RuntimePrincipalWithin(ctx, s.permissions, current, ceiling)
}
