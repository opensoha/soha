package identity

import (
	"context"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) CurrentSessionPrincipal(ctx context.Context, userID, sessionID string) (domainidentity.Principal, error) {
	if s.sessions == nil || sessionID == "" {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	session, err := s.sessions.GetAuthSessionByID(ctx, sessionID)
	if err != nil || session.UserID != userID || session.Status != "active" || !session.ExpiresAt.After(time.Now()) {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	return s.CurrentPrincipal(ctx, userID)
}
