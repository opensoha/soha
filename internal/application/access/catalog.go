package access

import (
	"context"
	"fmt"
	"time"

	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"github.com/kubecrux/kubecrux/internal/platform/requestctx"
)

type UserReader interface {
	ListUsers(context.Context) ([]domainaccess.UserRecord, error)
	ListTeamsDetailed(context.Context) ([]domainaccess.TeamRecord, error)
}

type PolicyReader interface {
	ListPolicies(context.Context) ([]domainaccess.Policy, error)
	ListRoles(context.Context) ([]domainaccess.RoleRecord, error)
}

type CatalogService struct {
	users      UserReader
	policies   PolicyReader
	authorizer domainaccess.Authorizer
}

func NewCatalog(users UserReader, policies PolicyReader, authorizer domainaccess.Authorizer) *CatalogService {
	return &CatalogService{users: users, policies: policies, authorizer: authorizer}
}

func (s *CatalogService) ListUsers(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.UserRecord, error) {
	if err := s.authorize(ctx, principal, "users"); err != nil {
		return nil, err
	}
	return s.users.ListUsers(ctx)
}

func (s *CatalogService) ListRoles(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.RoleRecord, error) {
	if err := s.authorize(ctx, principal, "roles"); err != nil {
		return nil, err
	}
	return s.policies.ListRoles(ctx)
}

func (s *CatalogService) ListTeams(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.TeamRecord, error) {
	if err := s.authorize(ctx, principal, "teams"); err != nil {
		return nil, err
	}
	return s.users.ListTeamsDetailed(ctx)
}

func (s *CatalogService) ListPolicies(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.Policy, error) {
	if err := s.authorize(ctx, principal, "policies"); err != nil {
		return nil, err
	}
	return s.policies.ListPolicies(ctx)
}

func (s *CatalogService) authorize(ctx context.Context, principal domainidentity.Principal, resourceName string) error {
	if s.authorizer == nil {
		return nil
	}
	decision, err := s.authorizer.Authorize(ctx, domainaccess.Request{
		Principal: principal,
		Action:    domainaccess.ActionList,
		Subject: domainaccess.SubjectAttributes{
			UserID:   principal.UserID,
			Roles:    principal.Roles,
			Teams:    principal.Teams,
			Projects: principal.Projects,
			Tags:     principal.Tags,
		},
		Resource: domainaccess.ResourceAttributes{
			Kind: "Access",
			Name: resourceName,
		},
		Context: domainaccess.ContextAttributes{
			Source:     requestctx.FromContext(ctx).Source,
			OccurredAt: time.Now().UTC(),
		},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return nil
}
