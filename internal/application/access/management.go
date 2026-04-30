package access

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	userrepo "github.com/kubecrux/kubecrux/internal/repository/user"
	"gorm.io/gorm"
)

type UserManager interface {
	GetByID(context.Context, string) (userrepo.User, error)
	CreateUser(context.Context, domainaccess.UserInput) (domainaccess.UserRecord, error)
	UpdateUser(context.Context, string, domainaccess.UserInput) (domainaccess.UserRecord, error)
	DeleteUser(context.Context, string) error
	RevokeSessionsByUserID(context.Context, string) error
	ReplaceRoleBindings(context.Context, string, []string) error
	ReplaceTeamBindings(context.Context, string, []string) error
	CreateTeam(context.Context, domainaccess.TeamInput) (domainaccess.TeamRecord, error)
	UpdateTeam(context.Context, string, domainaccess.TeamInput) (domainaccess.TeamRecord, error)
	DeleteTeam(context.Context, string) error
}

type PolicyManager interface {
	CreateRole(context.Context, domainaccess.RoleInput) (domainaccess.RoleRecord, error)
	UpdateRole(context.Context, string, domainaccess.RoleInput) (domainaccess.RoleRecord, error)
	DeleteRole(context.Context, string) error
	CreatePolicy(context.Context, domainaccess.PolicyInput) (domainaccess.Policy, error)
	UpdatePolicy(context.Context, string, domainaccess.PolicyInput) (domainaccess.Policy, error)
	DeletePolicy(context.Context, string) error
}

type ManagementService struct {
	users       UserManager
	policies    PolicyManager
	permissions *PermissionResolver
}

func NewManagement(users UserManager, policies PolicyManager, permissions *PermissionResolver) *ManagementService {
	return &ManagementService{users: users, policies: policies, permissions: permissions}
}

func (s *ManagementService) CreateRole(ctx context.Context, principal domainidentity.Principal, input domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	if err := s.ensurePermission(ctx, principal, PermAccessRolesManage); err != nil {
		return domainaccess.RoleRecord{}, err
	}
	input.ID = normalizeID(input.ID, input.Name)
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return domainaccess.RoleRecord{}, fmt.Errorf("%w: role name is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Scope) == "" {
		input.Scope = "custom"
	}
	input.PermissionKeys = normalizePermissionKeys(input.PermissionKeys)
	item, err := s.policies.CreateRole(ctx, input)
	if err == nil {
		SetRolePermissionKeys(item.ID, item.PermissionKeys)
	}
	return item, err
}

func (s *ManagementService) UpdateRole(ctx context.Context, principal domainidentity.Principal, roleID string, input domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	if err := s.ensurePermission(ctx, principal, PermAccessRolesManage); err != nil {
		return domainaccess.RoleRecord{}, err
	}
	if strings.TrimSpace(roleID) == "" {
		return domainaccess.RoleRecord{}, fmt.Errorf("%w: role id is required", apperrors.ErrInvalidArgument)
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return domainaccess.RoleRecord{}, fmt.Errorf("%w: role name is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Scope) == "" {
		input.Scope = "custom"
	}
	input.PermissionKeys = normalizePermissionKeys(input.PermissionKeys)
	item, err := s.policies.UpdateRole(ctx, roleID, input)
	if err == nil {
		SetRolePermissionKeys(item.ID, item.PermissionKeys)
	}
	return item, normalizeWriteError(err)
}

func (s *ManagementService) DeleteRole(ctx context.Context, principal domainidentity.Principal, roleID string) error {
	if err := s.ensurePermission(ctx, principal, PermAccessRolesManage); err != nil {
		return err
	}
	if strings.TrimSpace(roleID) == "" {
		return fmt.Errorf("%w: role id is required", apperrors.ErrInvalidArgument)
	}
	if err := normalizeWriteError(s.policies.DeleteRole(ctx, roleID)); err != nil {
		return err
	}
	DeleteRolePermissionKeys(roleID)
	return nil
}

func (s *ManagementService) CreateTeam(ctx context.Context, principal domainidentity.Principal, input domainaccess.TeamInput) (domainaccess.TeamRecord, error) {
	if err := s.ensurePermission(ctx, principal, PermAccessGroupsManage); err != nil {
		return domainaccess.TeamRecord{}, err
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return domainaccess.TeamRecord{}, fmt.Errorf("%w: team name is required", apperrors.ErrInvalidArgument)
	}
	input.ID = normalizeID(input.ID, input.Name)
	input.Slug = normalizeID(input.Slug, input.Name)
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	return s.users.CreateTeam(ctx, input)
}

func (s *ManagementService) UpdateTeam(ctx context.Context, principal domainidentity.Principal, teamID string, input domainaccess.TeamInput) (domainaccess.TeamRecord, error) {
	if err := s.ensurePermission(ctx, principal, PermAccessGroupsManage); err != nil {
		return domainaccess.TeamRecord{}, err
	}
	if strings.TrimSpace(teamID) == "" {
		return domainaccess.TeamRecord{}, fmt.Errorf("%w: team id is required", apperrors.ErrInvalidArgument)
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return domainaccess.TeamRecord{}, fmt.Errorf("%w: team name is required", apperrors.ErrInvalidArgument)
	}
	input.Slug = normalizeID(input.Slug, input.Name)
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	item, err := s.users.UpdateTeam(ctx, teamID, input)
	return item, normalizeWriteError(err)
}

func (s *ManagementService) DeleteTeam(ctx context.Context, principal domainidentity.Principal, teamID string) error {
	if err := s.ensurePermission(ctx, principal, PermAccessGroupsManage); err != nil {
		return err
	}
	if strings.TrimSpace(teamID) == "" {
		return fmt.Errorf("%w: team id is required", apperrors.ErrInvalidArgument)
	}
	return normalizeWriteError(s.users.DeleteTeam(ctx, teamID))
}

func (s *ManagementService) CreatePolicy(ctx context.Context, principal domainidentity.Principal, input domainaccess.PolicyInput) (domainaccess.Policy, error) {
	if err := s.ensurePermission(ctx, principal, PermAccessPoliciesManage); err != nil {
		return domainaccess.Policy{}, err
	}
	input.ID = normalizeID(input.ID, input.Name)
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return domainaccess.Policy{}, fmt.Errorf("%w: policy name is required", apperrors.ErrInvalidArgument)
	}
	if input.Effect == "" {
		input.Effect = domainaccess.EffectAllow
	}
	return s.policies.CreatePolicy(ctx, input)
}

func (s *ManagementService) UpdatePolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domainaccess.PolicyInput) (domainaccess.Policy, error) {
	if err := s.ensurePermission(ctx, principal, PermAccessPoliciesManage); err != nil {
		return domainaccess.Policy{}, err
	}
	if strings.TrimSpace(policyID) == "" {
		return domainaccess.Policy{}, fmt.Errorf("%w: policy id is required", apperrors.ErrInvalidArgument)
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return domainaccess.Policy{}, fmt.Errorf("%w: policy name is required", apperrors.ErrInvalidArgument)
	}
	if input.Effect == "" {
		input.Effect = domainaccess.EffectAllow
	}
	item, err := s.policies.UpdatePolicy(ctx, policyID, input)
	return item, normalizeWriteError(err)
}

func (s *ManagementService) DeletePolicy(ctx context.Context, principal domainidentity.Principal, policyID string) error {
	if err := s.ensurePermission(ctx, principal, PermAccessPoliciesManage); err != nil {
		return err
	}
	if strings.TrimSpace(policyID) == "" {
		return fmt.Errorf("%w: policy id is required", apperrors.ErrInvalidArgument)
	}
	return normalizeWriteError(s.policies.DeletePolicy(ctx, policyID))
}

func (s *ManagementService) ReplaceUserRoles(ctx context.Context, principal domainidentity.Principal, userID string, roleIDs []string) error {
	if err := s.ensurePermission(ctx, principal, PermAccessUsersManage); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("%w: user id is required", apperrors.ErrInvalidArgument)
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return normalizeWriteError(err)
	}
	return normalizeWriteError(s.users.ReplaceRoleBindings(ctx, userID, roleIDs))
}

func (s *ManagementService) ReplaceUserTeams(ctx context.Context, principal domainidentity.Principal, userID string, teamIDs []string) error {
	if err := s.ensurePermission(ctx, principal, PermAccessUsersManage); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("%w: user id is required", apperrors.ErrInvalidArgument)
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return normalizeWriteError(err)
	}
	return normalizeWriteError(s.users.ReplaceTeamBindings(ctx, userID, teamIDs))
}

func (s *ManagementService) CreateUser(ctx context.Context, principal domainidentity.Principal, input domainaccess.UserInput) (domainaccess.UserRecord, error) {
	if err := s.ensurePermission(ctx, principal, PermAccessUsersManage); err != nil {
		return domainaccess.UserRecord{}, err
	}
	input.ID = normalizeID(input.ID, input.Username)
	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.TrimSpace(input.Email)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.Username == "" {
		return domainaccess.UserRecord{}, fmt.Errorf("%w: username is required", apperrors.ErrInvalidArgument)
	}
	if input.Email == "" {
		return domainaccess.UserRecord{}, fmt.Errorf("%w: email is required", apperrors.ErrInvalidArgument)
	}
	if input.Status == "" {
		input.Status = "active"
	}
	input.RoleIDs = uniqueTrimmedStrings(input.RoleIDs)
	input.TeamIDs = uniqueTrimmedStrings(input.TeamIDs)
	if input.Preferences == nil {
		input.Preferences = map[string]any{}
	}
	return s.users.CreateUser(ctx, input)
}

func (s *ManagementService) UpdateUser(ctx context.Context, principal domainidentity.Principal, userID string, input domainaccess.UserInput) (domainaccess.UserRecord, error) {
	if err := s.ensurePermission(ctx, principal, PermAccessUsersManage); err != nil {
		return domainaccess.UserRecord{}, err
	}
	if strings.TrimSpace(userID) == "" {
		return domainaccess.UserRecord{}, fmt.Errorf("%w: user id is required", apperrors.ErrInvalidArgument)
	}
	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.TrimSpace(input.Email)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.Username == "" {
		return domainaccess.UserRecord{}, fmt.Errorf("%w: username is required", apperrors.ErrInvalidArgument)
	}
	if input.Email == "" {
		return domainaccess.UserRecord{}, fmt.Errorf("%w: email is required", apperrors.ErrInvalidArgument)
	}
	if input.Status == "" {
		input.Status = "active"
	}
	input.RoleIDs = uniqueTrimmedStrings(input.RoleIDs)
	input.TeamIDs = uniqueTrimmedStrings(input.TeamIDs)
	if input.Preferences == nil {
		input.Preferences = map[string]any{}
	}
	item, err := s.users.UpdateUser(ctx, userID, input)
	return item, normalizeWriteError(err)
}

func (s *ManagementService) DeleteUser(ctx context.Context, principal domainidentity.Principal, userID string) error {
	if err := s.ensurePermission(ctx, principal, PermAccessUsersManage); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("%w: user id is required", apperrors.ErrInvalidArgument)
	}
	return normalizeWriteError(s.users.DeleteUser(ctx, userID))
}

func (s *ManagementService) RevokeUserSessions(ctx context.Context, principal domainidentity.Principal, userID string) error {
	if err := s.ensurePermission(ctx, principal, PermAccessUsersManage); err != nil {
		return err
	}
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("%w: user id is required", apperrors.ErrInvalidArgument)
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return normalizeWriteError(err)
	}
	return normalizeWriteError(s.users.RevokeSessionsByUserID(ctx, userID))
}

func (s *ManagementService) ensurePermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func normalizeID(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return strings.ToLower(strings.ReplaceAll(value, " ", "-"))
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return uuid.NewString()
	}
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(fallback, " ", "-"), "_", "-"))
}

func uniqueTrimmedStrings(items []string) []string {
	if items == nil {
		return nil
	}
	unique := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" || slices.Contains(unique, value) {
			continue
		}
		unique = append(unique, value)
	}
	return unique
}

func normalizeWriteError(err error) error {
	if err == nil {
		return nil
	}
	if err == userrepo.ErrNotFound || err == gorm.ErrRecordNotFound {
		return fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
	}
	return err
}
