package aigateway

import (
	"context"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) currentApprovalPrincipal(ctx context.Context, request domainaigateway.ApprovalRequest) (domainidentity.Principal, error) {
	if principal, ok := ctx.Value(capabilityPrincipalKey{}).(domainidentity.Principal); ok {
		actorType, actorID := gatewaySubject(principal)
		if actorType == request.ActorType && actorID == request.ActorID {
			return s.approvalPrincipalWithin(ctx, principal, request)
		}
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	id := strings.TrimSpace(request.ActorID)
	if request.ActorTokenID != "" {
		reader, ok := s.identity.(interface {
			CurrentExecutionPrincipal(context.Context, string, string) (domainidentity.Principal, error)
		})
		if !ok {
			return domainidentity.Principal{}, fmt.Errorf("%w: approval execution identity is unavailable", apperrors.ErrAccessDenied)
		}
		subject := id
		if request.ActorType == "service_account" {
			subject = "service_account:" + strings.TrimPrefix(id, "service_account:")
		}
		principal, err := reader.CurrentExecutionPrincipal(ctx, subject, request.ActorTokenID)
		if err != nil {
			return domainidentity.Principal{}, err
		}
		return s.approvalPrincipalWithin(ctx, principal, request)
	}
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

func (s *Service) approvalPrincipalWithin(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest) (domainidentity.Principal, error) {
	actorType, actorID := gatewaySubject(principal)
	if actorType != request.ActorType || actorID != request.ActorID || request.ActorTokenID != "" && principal.AccessTokenID != request.ActorTokenID {
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	if request.ActorTokenID != "" {
		return appaccess.RuntimePrincipalWithin(ctx, s.permissions, principal, request.ActorPermissionCeiling)
	}
	return principal, nil
}
