package access

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

type stubUserManager struct{}

func (stubUserManager) GetByID(context.Context, string) (userrepo.User, error) {
	return userrepo.User{ID: "user-1"}, nil
}
func (stubUserManager) CreateUser(context.Context, domainaccess.UserInput) (domainaccess.UserRecord, error) {
	return domainaccess.UserRecord{}, nil
}
func (stubUserManager) UpdateUser(context.Context, string, domainaccess.UserInput) (domainaccess.UserRecord, error) {
	return domainaccess.UserRecord{}, nil
}
func (stubUserManager) DeleteUser(context.Context, string) error                    { return nil }
func (stubUserManager) RevokeSessionsByUserID(context.Context, string) error        { return nil }
func (stubUserManager) ReplaceRoleBindings(context.Context, string, []string) error { return nil }
func (stubUserManager) ReplaceTeamBindings(context.Context, string, []string) error { return nil }
func (stubUserManager) CreateTeam(context.Context, domainaccess.TeamInput) (domainaccess.TeamRecord, error) {
	return domainaccess.TeamRecord{}, nil
}
func (stubUserManager) UpdateTeam(context.Context, string, domainaccess.TeamInput) (domainaccess.TeamRecord, error) {
	return domainaccess.TeamRecord{}, nil
}
func (stubUserManager) DeleteTeam(context.Context, string) error { return nil }

type captureUserManager struct {
	stubUserManager
	lastCreateUserInput domainaccess.UserInput
}

func (s *captureUserManager) CreateUser(_ context.Context, input domainaccess.UserInput) (domainaccess.UserRecord, error) {
	s.lastCreateUserInput = input
	return domainaccess.UserRecord{
		ID:          input.ID,
		Username:    input.Username,
		Email:       input.Email,
		DisplayName: input.DisplayName,
		Status:      input.Status,
		Roles:       input.RoleIDs,
		Teams:       input.TeamIDs,
	}, nil
}

type stubPolicyManager struct {
	createdRole domainaccess.RoleInput
}

func (s *stubPolicyManager) CreateRole(context.Context, domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	s.createdRole = domainaccess.RoleInput{}
	return domainaccess.RoleRecord{}, nil
}
func (s *stubPolicyManager) UpdateRole(context.Context, string, domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	return domainaccess.RoleRecord{}, nil
}
func (s *stubPolicyManager) DeleteRole(context.Context, string) error { return nil }
func (s *stubPolicyManager) CreatePolicy(context.Context, domainaccess.PolicyInput) (domainaccess.Policy, error) {
	return domainaccess.Policy{}, nil
}
func (s *stubPolicyManager) UpdatePolicy(context.Context, string, domainaccess.PolicyInput) (domainaccess.Policy, error) {
	return domainaccess.Policy{}, nil
}
func (s *stubPolicyManager) DeletePolicy(context.Context, string) error { return nil }

type capturePolicyManager struct {
	lastRoleInput domainaccess.RoleInput
}

func (s *capturePolicyManager) CreateRole(_ context.Context, input domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	s.lastRoleInput = input
	return domainaccess.RoleRecord{
		ID:             input.ID,
		Name:           input.Name,
		Scope:          input.Scope,
		Capabilities:   input.Capabilities,
		PermissionKeys: input.PermissionKeys,
	}, nil
}
func (s *capturePolicyManager) UpdateRole(_ context.Context, _ string, input domainaccess.RoleInput) (domainaccess.RoleRecord, error) {
	s.lastRoleInput = input
	return domainaccess.RoleRecord{
		ID:             input.ID,
		Name:           input.Name,
		Scope:          input.Scope,
		Capabilities:   input.Capabilities,
		PermissionKeys: input.PermissionKeys,
	}, nil
}
func (s *capturePolicyManager) DeleteRole(context.Context, string) error { return nil }
func (s *capturePolicyManager) CreatePolicy(context.Context, domainaccess.PolicyInput) (domainaccess.Policy, error) {
	return domainaccess.Policy{}, nil
}
func (s *capturePolicyManager) UpdatePolicy(context.Context, string, domainaccess.PolicyInput) (domainaccess.Policy, error) {
	return domainaccess.Policy{}, nil
}
func (s *capturePolicyManager) DeletePolicy(context.Context, string) error { return nil }

func TestManagementCreateRoleRequiresExplicitManagePermission(t *testing.T) {
	service := NewManagement(stubUserManager{}, &capturePolicyManager{}, NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {PermAccessRolesView},
		},
	}), nil, nil)

	_, err := service.CreateRole(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, domainaccess.RoleInput{
		Name: "Custom Role",
	})
	if err == nil {
		t.Fatalf("CreateRole error = nil, want access denied")
	}
}

func TestManagementCreateRolePersistsNormalizedPermissionKeys(t *testing.T) {
	SetRolePermissionMatrix(nil)
	policies := &capturePolicyManager{}
	service := NewManagement(stubUserManager{}, policies, NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {PermAccessRolesManage},
		},
	}), nil, nil)

	item, err := service.CreateRole(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, domainaccess.RoleInput{
		Name:           "Custom Role",
		PermissionKeys: []string{" access.users.view ", PermAccessUsersView, PermAccessScopeGrantsManage},
	})
	if err != nil {
		t.Fatalf("CreateRole returned error: %v", err)
	}
	if len(policies.lastRoleInput.PermissionKeys) != 2 {
		t.Fatalf("PermissionKeys = %v, want deduplicated keys", policies.lastRoleInput.PermissionKeys)
	}
	if !HasPermission([]string{item.ID}, PermAccessScopeGrantsManage) {
		t.Fatalf("runtime permission matrix not updated for created role")
	}
}

func TestManagementCreateRoleFailsClosedWithoutRuntimeResolver(t *testing.T) {
	service := NewManagement(stubUserManager{}, &capturePolicyManager{}, nil, nil, nil)

	_, err := service.CreateRole(context.Background(), domainidentity.Principal{Roles: []string{"admin"}}, domainaccess.RoleInput{
		Name: "Custom Role",
	})
	if err == nil {
		t.Fatalf("CreateRole error = nil, want runtime resolver failure")
	}
}

func TestManagementCreateUserGeneratesUUIDWhenIDMissing(t *testing.T) {
	SetRolePermissionMatrix(nil)
	users := &captureUserManager{}
	service := NewManagement(users, &capturePolicyManager{}, NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {PermAccessUsersManage},
		},
	}), nil, nil)

	item, err := service.CreateUser(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, domainaccess.UserInput{
		Username:    " opensoha ",
		Email:       " opensoha@soha.local ",
		DisplayName: " OpenSoha ",
	})
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if users.lastCreateUserInput.ID == "" {
		t.Fatalf("CreateUser input ID = empty, want generated UUID")
	}
	if users.lastCreateUserInput.ID == "opensoha" {
		t.Fatalf("CreateUser input ID = opensoha, want generated UUID")
	}
	if _, err := uuid.Parse(users.lastCreateUserInput.ID); err != nil {
		t.Fatalf("CreateUser input ID should be a UUID: %v", err)
	}
	if item.ID != users.lastCreateUserInput.ID {
		t.Fatalf("CreateUser item ID = %q, want %q", item.ID, users.lastCreateUserInput.ID)
	}
	if users.lastCreateUserInput.Username != "opensoha" {
		t.Fatalf("CreateUser username = %q, want trimmed username", users.lastCreateUserInput.Username)
	}
}

func TestManagementCreateUserRejectsNonUUIDID(t *testing.T) {
	SetRolePermissionMatrix(nil)
	users := &captureUserManager{}
	service := NewManagement(users, &capturePolicyManager{}, NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {PermAccessUsersManage},
		},
	}), nil, nil)

	_, err := service.CreateUser(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, domainaccess.UserInput{
		ID:       "opensoha",
		Username: "opensoha",
		Email:    "opensoha@soha.local",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("CreateUser error = %v, want invalid argument", err)
	}
	if users.lastCreateUserInput.ID != "" {
		t.Fatalf("CreateUser reached repository with ID %q", users.lastCreateUserInput.ID)
	}
}

func TestNormalizeWriteErrorPreservesNotFound(t *testing.T) {
	err := normalizeWriteError(userrepo.ErrNotFound)
	if err == nil {
		t.Fatalf("normalizeWriteError = nil, want not found error")
	}
	if errors.Is(err, userrepo.ErrNotFound) {
		t.Fatalf("normalizeWriteError should wrap repository not found into app error")
	}
}
