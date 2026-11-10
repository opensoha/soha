package networkaccess

import (
	"context"
	"fmt"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// GetSessionCommandForSession observes the original command. It cannot claim, execute or
// redeliver it; only the authenticated NAS runtime owns those transitions.
func (s *Service) GetSessionCommandForSession(ctx context.Context, principal domainidentity.Principal, sessionID, commandID string) (domainnetworkaccess.SessionCommand, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	if err := validateCommandCoordinates(sessionID, commandID, false); err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	if _, err := s.GetSession(ctx, principal, sessionID); err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	return s.store.GetSessionCommandForSession(ctx, sessionID, commandID)
}

// GetOriginalSessionCommand also works when the execute response was lost and
// the command ID is unknown. Never recompute the plan or resubmit a mutation.
func (s *Service) GetOriginalSessionCommand(ctx context.Context, principal domainidentity.Principal, sessionID, planHash string) (domainnetworkaccess.SessionCommand, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	if err := validateCommandCoordinates(sessionID, planHash, true); err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	if _, err := s.GetSession(ctx, principal, sessionID); err != nil {
		return domainnetworkaccess.SessionCommand{}, err
	}
	return s.store.GetSessionCommandForPlan(ctx, sessionID, planHash)
}

func validateCommandCoordinates(sessionID, key string, byPlan bool) error {
	if !runtimeIdentifierPattern.MatchString(sessionID) {
		return fmt.Errorf("%w: invalid network session ID", apperrors.ErrInvalidArgument)
	}
	if byPlan {
		if !sha256Pattern.MatchString(key) {
			return fmt.Errorf("%w: invalid original network plan hash", apperrors.ErrInvalidArgument)
		}
		return nil
	}
	if !runtimeIdentifierPattern.MatchString(key) {
		return fmt.Errorf("%w: invalid network session command ID", apperrors.ErrInvalidArgument)
	}
	return nil
}
