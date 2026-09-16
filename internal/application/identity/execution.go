package identity

import (
	"context"
	"fmt"
	"strings"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// CurrentExecutionPrincipal restores a previously authenticated execution subject.
// Token IDs are trusted server state, never a substitute for HTTP authentication.
func (s *Service) CurrentExecutionPrincipal(ctx context.Context, subjectID, tokenID string) (domainidentity.Principal, error) {
	serviceAccount := strings.HasPrefix(subjectID, "service_account:")
	if tokenID == "" {
		if serviceAccount {
			return domainidentity.Principal{}, fmt.Errorf("%w: service account execution requires a token", apperrors.ErrUnauthorized)
		}
		return s.CurrentPrincipal(ctx, subjectID)
	}
	if s.gateway == nil {
		return domainidentity.Principal{}, fmt.Errorf("%w: gateway token store is unavailable", apperrors.ErrUnauthorized)
	}
	if serviceAccount {
		item, err := s.gateway.GetServiceAccountTokenByID(ctx, tokenID)
		if err != nil || "service_account:"+item.ServiceAccountID != subjectID {
			return domainidentity.Principal{}, fmt.Errorf("%w: execution token does not belong to subject", apperrors.ErrUnauthorized)
		}
		principal, _, err := s.serviceAccountTokenPrincipal(ctx, item)
		return principal, err
	}
	item, err := s.gateway.GetPersonalAccessTokenByID(ctx, tokenID)
	if err != nil || item.UserID != subjectID {
		return domainidentity.Principal{}, fmt.Errorf("%w: execution token does not belong to subject", apperrors.ErrUnauthorized)
	}
	principal, _, err := s.personalAccessTokenPrincipal(ctx, item)
	return principal, err
}
