package access

import (
	"context"
	"fmt"
	"slices"
	"strings"

	domainidentity "github.com/soha/soha/internal/domain/identity"
	"github.com/soha/soha/internal/platform/apperrors"
)

type RolePermissionReader interface {
	ListRolePermissions(context.Context) (map[string][]string, error)
}

type PermissionResolver struct {
	roles RolePermissionReader
}

func NewPermissionResolver(roles RolePermissionReader) *PermissionResolver {
	return &PermissionResolver{roles: roles}
}

func runtimeResolverUnavailable(permissionKey string) error {
	permissionKey = strings.TrimSpace(permissionKey)
	if permissionKey == "" {
		return fmt.Errorf("%w: runtime permission resolver unavailable", apperrors.ErrAccessDenied)
	}
	return fmt.Errorf("%w: runtime permission resolver unavailable for %s", apperrors.ErrAccessDenied, permissionKey)
}

func AuthorizeRuntimePermission(ctx context.Context, resolver *PermissionResolver, principal domainidentity.Principal, permissionKey string) error {
	if resolver == nil || resolver.roles == nil {
		return runtimeResolverUnavailable(permissionKey)
	}
	return resolver.Authorize(ctx, principal, permissionKey)
}

func RuntimePermissionKeys(ctx context.Context, resolver *PermissionResolver, principal domainidentity.Principal) ([]string, error) {
	if resolver == nil || resolver.roles == nil {
		return nil, runtimeResolverUnavailable("")
	}
	return resolver.PermissionKeys(ctx, principal)
}

func (r *PermissionResolver) PermissionKeys(ctx context.Context, principal domainidentity.Principal) ([]string, error) {
	if r == nil || r.roles == nil {
		return nil, runtimeResolverUnavailable("")
	}
	matrix, err := r.roles.ListRolePermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("load role permissions: %w", err)
	}
	if len(matrix) == 0 {
		return PermissionKeysForRoles(principal.Roles), nil
	}
	SetRolePermissionMatrix(matrix)
	keys := make([]string, 0)
	for _, roleID := range principal.Roles {
		for _, permissionKey := range matrix[strings.TrimSpace(roleID)] {
			if !slices.Contains(keys, permissionKey) {
				keys = append(keys, permissionKey)
			}
		}
	}
	keys = normalizePermissionKeys(keys)
	if len(principal.PermissionKeys) == 0 {
		return keys, nil
	}
	capped := make([]string, 0, len(keys))
	allowedCaps := normalizePermissionKeys(principal.PermissionKeys)
	for _, key := range keys {
		if slices.Contains(allowedCaps, key) {
			capped = append(capped, key)
		}
	}
	return normalizePermissionKeys(capped), nil
}

func (r *PermissionResolver) HasPermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) (bool, error) {
	if strings.TrimSpace(permissionKey) == "" {
		return true, nil
	}
	keys, err := r.PermissionKeys(ctx, principal)
	if err != nil {
		return false, err
	}
	return slices.Contains(keys, strings.TrimSpace(permissionKey)), nil
}

func (r *PermissionResolver) Authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	allowed, err := r.HasPermission(ctx, principal, permissionKey)
	if err != nil {
		return err
	}
	if allowed {
		return nil
	}
	return fmt.Errorf("%w: missing permission %s", apperrors.ErrAccessDenied, strings.TrimSpace(permissionKey))
}
