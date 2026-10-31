package identity

import (
	"context"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// CurrentDelegatedExecutionPrincipal revalidates an accepted, credential-bound
// execution without manufacturing a browser AccessContext. Callers must also
// recheck the original domain/Gateway authorization. Native streaming retains
// its separate credential expiry and governed-AI rejection rules.
func (s *Service) CurrentDelegatedExecutionPrincipal(ctx context.Context, original domainidentity.Principal) (domainidentity.Principal, error) {
	if original.UserID == "" || original.AccessTokenID == "" {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	current, err := s.CurrentExecutionPrincipal(ctx, original.UserID, original.AccessTokenID)
	if err != nil {
		return domainidentity.Principal{}, err
	}
	if current.UserID != original.UserID || current.AccessTokenID != original.AccessTokenID || (current.GovernedAI == nil) != (original.GovernedAI == nil) {
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	ceiling, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, original)
	if err != nil {
		return domainidentity.Principal{}, err
	}
	return appaccess.RuntimePrincipalWithin(ctx, s.permissions, current, ceiling)
}
