package access

import (
	"context"
	"slices"

	domainaccess "github.com/soha/soha/internal/domain/access"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainmenu "github.com/soha/soha/internal/domain/menu"
)

type UserReader interface {
	ListUsers(context.Context) ([]domainaccess.UserRecord, error)
	ListTeamsDetailed(context.Context) ([]domainaccess.TeamRecord, error)
}

type PolicyReader interface {
	ListPolicies(context.Context) ([]domainaccess.Policy, error)
	ListRoles(context.Context) ([]domainaccess.RoleRecord, error)
}

type VisibleMenuReader interface {
	ListVisible(context.Context, domainidentity.Principal) ([]domainmenu.Record, error)
}

type CatalogService struct {
	users       UserReader
	policies    PolicyReader
	authorizer  domainaccess.Authorizer
	menus       VisibleMenuReader
	permissions *PermissionResolver
}

func NewCatalog(users UserReader, policies PolicyReader, authorizer domainaccess.Authorizer, menus VisibleMenuReader, permissions *PermissionResolver) *CatalogService {
	return &CatalogService{users: users, policies: policies, authorizer: authorizer, menus: menus, permissions: permissions}
}

func (s *CatalogService) ListUsers(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.UserRecord, error) {
	if err := s.authorizePermission(ctx, principal, PermAccessUsersView); err != nil {
		return nil, err
	}
	return s.users.ListUsers(ctx)
}

func (s *CatalogService) ListRoles(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.RoleRecord, error) {
	if err := s.authorizePermission(ctx, principal, PermAccessRolesView); err != nil {
		return nil, err
	}
	return s.policies.ListRoles(ctx)
}

func (s *CatalogService) ListTeams(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.TeamRecord, error) {
	if err := s.authorizePermission(ctx, principal, PermAccessGroupsView); err != nil {
		return nil, err
	}
	return s.users.ListTeamsDetailed(ctx)
}

func (s *CatalogService) ListPolicies(ctx context.Context, principal domainidentity.Principal) ([]domainaccess.Policy, error) {
	if err := s.authorizePermission(ctx, principal, PermAccessPoliciesView); err != nil {
		return nil, err
	}
	return s.policies.ListPolicies(ctx)
}

func (s *CatalogService) PermissionSnapshot(ctx context.Context, principal domainidentity.Principal) (domainaccess.PermissionSnapshot, error) {
	snapshot := domainaccess.PermissionSnapshot{
		PermissionKeys: []string{},
		VisibleMenuIDs: []string{},
		VisibleMenus:   []domainaccess.VisibleMenu{},
	}
	keys, err := s.permissionKeys(ctx, principal)
	if err != nil {
		return domainaccess.PermissionSnapshot{}, err
	}
	snapshot.PermissionKeys = keys
	if s.menus == nil {
		return snapshot, nil
	}
	visibleMenus, err := s.menus.ListVisible(ctx, principal)
	if err != nil {
		return domainaccess.PermissionSnapshot{}, err
	}
	flattenVisibleMenus(visibleMenus, &snapshot)
	return snapshot, nil
}

func (s *CatalogService) authorizePermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func (s *CatalogService) permissionKeys(ctx context.Context, principal domainidentity.Principal) ([]string, error) {
	return RuntimePermissionKeys(ctx, s.permissions, principal)
}

func flattenVisibleMenus(items []domainmenu.Record, snapshot *domainaccess.PermissionSnapshot) {
	for _, item := range items {
		if item.ID != "" && !slices.Contains(snapshot.VisibleMenuIDs, item.ID) {
			snapshot.VisibleMenuIDs = append(snapshot.VisibleMenuIDs, item.ID)
			snapshot.VisibleMenus = append(snapshot.VisibleMenus, domainaccess.VisibleMenu{
				ID:        item.ID,
				ParentID:  item.ParentID,
				Path:      item.Path,
				LabelZH:   item.LabelZH,
				LabelEN:   item.LabelEN,
				IconKey:   item.IconKey,
				Section:   item.Section,
				SortOrder: item.SortOrder,
				Enabled:   item.Enabled,
			})
		}
		if len(item.Children) > 0 {
			flattenVisibleMenus(item.Children, snapshot)
		}
	}
}
