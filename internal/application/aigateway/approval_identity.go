package aigateway

import (
	"context"
	"fmt"
	"strings"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) currentApprovalPrincipal(ctx context.Context, request domainaigateway.ApprovalRequest) (domainidentity.Principal, error) {
	if principal, ok := ctx.Value(capabilityPrincipalKey{}).(domainidentity.Principal); ok {
		actorType, actorID := gatewaySubject(principal)
		if actorType == request.ActorType && actorID == request.ActorID {
			return principal, nil
		}
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	id := strings.TrimSpace(request.ActorID)
	if request.ActorType == "user" && id != "" && s.identity != nil {
		principal, err := s.identity.CurrentPrincipal(ctx, id)
		if err != nil {
			return domainidentity.Principal{}, err
		}
		if principal.UserID == id {
			return principal, nil
		}
	}
	if request.ActorType == "service_account" && id != "" && s.serviceAccounts != nil {
		id = strings.TrimPrefix(id, "service_account:")
		account, err := s.serviceAccounts.GetServiceAccount(ctx, id)
		if err != nil {
			return domainidentity.Principal{}, err
		}
		if account.ID == id && account.Status == "active" {
			return domainidentity.Principal{UserID: "service_account:" + id, UserName: account.Name, Roles: account.RoleIDs, Teams: account.TeamIDs}, nil
		}
	}
	return domainidentity.Principal{}, fmt.Errorf("%w: approval actor is no longer available", apperrors.ErrAccessDenied)
}
