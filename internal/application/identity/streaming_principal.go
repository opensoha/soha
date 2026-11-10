package identity

import (
	"context"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// CurrentStreamingPrincipal revalidates trusted server-side authentication
// state without retaining a bearer secret. It cannot extend the original token
// lifetime or the original permission ceiling.
func (s *Service) CurrentStreamingPrincipal(ctx context.Context, original domainidentity.Principal, access domainidentity.AccessContext) (domainidentity.Principal, error) {
	if original.UserID == "" || original.GovernedAI != nil || access.SubjectID != "" && access.SubjectID != original.UserID || !access.ExpiresAt.IsZero() && !access.ExpiresAt.After(time.Now()) {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	var current domainidentity.Principal
	var err error
	if access.SessionID != "" {
		current, err = s.currentStreamingSession(ctx, original.UserID, access)
	} else if original.AccessTokenID != "" {
		current, err = s.CurrentExecutionPrincipal(ctx, original.UserID, original.AccessTokenID)
	} else {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	if err != nil {
		return current, err
	}
	if current.GovernedAI != nil {
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	ceiling, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, original)
	if err != nil {
		return current, err
	}
	return appaccess.RuntimePrincipalWithin(ctx, s.permissions, current, ceiling)
}

func (s *Service) currentStreamingSession(ctx context.Context, userID string, access domainidentity.AccessContext) (domainidentity.Principal, error) {
	if s.sessions == nil || access.AuthzVersion < 1 || access.ExpiresAt.IsZero() {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	session, err := s.sessions.GetAuthSessionByID(ctx, access.SessionID)
	if err != nil || session.UserID != userID || session.Status != "active" || !session.ExpiresAt.After(time.Now()) || session.AuthzVersion != access.AuthzVersion {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	state, err := s.requireActiveAuthzState(ctx, userID)
	if err != nil || state.AuthzVersion != access.AuthzVersion {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	if session.ProviderType == "cli_v2" {
		return s.cliSessionPrincipal(ctx, session)
	}
	return s.CurrentPrincipal(ctx, userID)
}
