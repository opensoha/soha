package access

import (
	"context"

	domainaccess "github.com/soha/soha/internal/domain/access"
	domainidentity "github.com/soha/soha/internal/domain/identity"
)

type ConsoleService struct {
	catalog    *CatalogService
	management *ManagementService
}

func NewConsole(catalog *CatalogService, management *ManagementService) *ConsoleService {
	return &ConsoleService{catalog: catalog, management: management}
}

func (s *ConsoleService) ListUsers(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.UserRecord, error) {
	return s.catalog.ListUsers(ctx, principal)
}

func (s *ConsoleService) CreateUser(ctx context.Context, principal domainidentity.Principal, input domainaccess.UserInput) (domainaccess.UserRecord, error) {
	return s.management.CreateUser(ctx, principal, input)
}

func (s *ConsoleService) UpdateUser(ctx context.Context, principal domainidentity.Principal, userID string, input domainaccess.UserInput) (domainaccess.UserRecord, error) {
	return s.management.UpdateUser(ctx, principal, userID, input)
}

func (s *ConsoleService) DeleteUser(ctx context.Context, principal domainidentity.Principal, userID string) error {
	return s.management.DeleteUser(ctx, principal, userID)
}

func (s *ConsoleService) RevokeUserSessions(ctx context.Context, principal domainidentity.Principal, userID string) error {
	return s.management.RevokeUserSessions(ctx, principal, userID)
}

func (s *ConsoleService) ListRoles(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.RoleRecord, error) {
	return s.catalog.ListRoles(ctx, principal)
}

func (s *ConsoleService) ListTeams(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.TeamRecord, error) {
	return s.catalog.ListTeams(ctx, principal)
}

func (s *ConsoleService) ListPolicies(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.Policy, error) {
	return s.catalog.ListPolicies(ctx, principal)
}

func (s *ConsoleService) PermissionSnapshot(ctx context.Context, principal domainidentity.Principal) (domainaccess.PermissionSnapshot, error) {
	return s.catalog.PermissionSnapshot(ctx, principal)
}

func (s *ConsoleService) CreateRole(ctx context.Context, principal domainidentity.Principal, input domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	return s.management.CreateRole(ctx, principal, input)
}

func (s *ConsoleService) UpdateRole(ctx context.Context, principal domainidentity.Principal, roleID string, input domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	return s.management.UpdateRole(ctx, principal, roleID, input)
}

func (s *ConsoleService) DeleteRole(ctx context.Context, principal domainidentity.Principal, roleID string) error {
	return s.management.DeleteRole(ctx, principal, roleID)
}

func (s *ConsoleService) CreateTeam(ctx context.Context, principal domainidentity.Principal, input domainaccess.TeamInput) (domainaccess.TeamRecord, error) {
	return s.management.CreateTeam(ctx, principal, input)
}

func (s *ConsoleService) UpdateTeam(ctx context.Context, principal domainidentity.Principal, teamID string, input domainaccess.TeamInput) (domainaccess.TeamRecord, error) {
	return s.management.UpdateTeam(ctx, principal, teamID, input)
}

func (s *ConsoleService) DeleteTeam(ctx context.Context, principal domainidentity.Principal, teamID string) error {
	return s.management.DeleteTeam(ctx, principal, teamID)
}

func (s *ConsoleService) CreatePolicy(ctx context.Context, principal domainidentity.Principal, input domainaccess.PolicyInput) (domainaccess.Policy, error) {
	return s.management.CreatePolicy(ctx, principal, input)
}

func (s *ConsoleService) UpdatePolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domainaccess.PolicyInput) (domainaccess.Policy, error) {
	return s.management.UpdatePolicy(ctx, principal, policyID, input)
}

func (s *ConsoleService) DeletePolicy(ctx context.Context, principal domainidentity.Principal, policyID string) error {
	return s.management.DeletePolicy(ctx, principal, policyID)
}

func (s *ConsoleService) ReplaceUserRoles(ctx context.Context, principal domainidentity.Principal, userID string, roleIDs []string) error {
	return s.management.ReplaceUserRoles(ctx, principal, userID, roleIDs)
}

func (s *ConsoleService) ReplaceUserTeams(ctx context.Context, principal domainidentity.Principal, userID string, teamIDs []string) error {
	return s.management.ReplaceUserTeams(ctx, principal, userID, teamIDs)
}
