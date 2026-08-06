package access

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	contractauth "github.com/opensoha/soha-contracts/auth"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var (
	permissionCatalogOnce sync.Once
	permissionCatalog     sohaapi.PermissionCatalog
	permissionCatalogErr  error
)

func loadPermissionCatalog() (sohaapi.PermissionCatalog, error) {
	permissionCatalogOnce.Do(func() {
		permissionCatalogErr = json.Unmarshal(contractauth.PermissionCatalogJSON(), &permissionCatalog)
		if permissionCatalogErr != nil {
			permissionCatalogErr = fmt.Errorf("decode embedded permission catalog: %w", permissionCatalogErr)
		}
	})
	return permissionCatalog, permissionCatalogErr
}

func permissionDefinition(permissionKey string) (sohaapi.PermissionDefinition, bool, error) {
	catalog, err := loadPermissionCatalog()
	if err != nil {
		return sohaapi.PermissionDefinition{}, false, err
	}
	permissionKey = strings.TrimSpace(permissionKey)
	for _, definition := range catalog.Permissions {
		if definition.Key == permissionKey {
			return definition, true, nil
		}
	}
	return sohaapi.PermissionDefinition{}, false, nil
}

func IsActiveAssignablePermission(permissionKey string) bool {
	definition, found, err := permissionDefinition(permissionKey)
	return err == nil && found && definition.Status == sohaapi.PermissionStatusActive && definition.Assignable
}

func expandLegacyPermissionKeys(permissionKeys []string) ([]string, error) {
	catalog, err := loadPermissionCatalog()
	if err != nil {
		return nil, err
	}
	keys := normalizePermissionKeys(permissionKeys)
	for _, definition := range catalog.Permissions {
		for _, alias := range definition.LegacyAliases {
			if slices.Contains(keys, alias) && !slices.Contains(keys, definition.Key) {
				keys = append(keys, definition.Key)
			}
		}
		if slices.Contains(keys, definition.Key) {
			for _, replacement := range definition.ReplacedBy {
				if !slices.Contains(keys, replacement) {
					keys = append(keys, replacement)
				}
			}
		}
	}
	return normalizePermissionKeys(keys), nil
}

func CanonicalAssignablePermissionKeys(permissionKeys []string) ([]string, error) {
	catalog, err := loadPermissionCatalog()
	if err != nil {
		return nil, err
	}
	for _, key := range normalizePermissionKeys(permissionKeys) {
		known := false
		for _, definition := range catalog.Permissions {
			if definition.Key == key {
				known = true
				if definition.Status == sohaapi.PermissionStatusActive && !definition.Assignable {
					return nil, fmt.Errorf("%w: permission key %q is not assignable", apperrors.ErrInvalidArgument, key)
				}
				break
			}
			if slices.Contains(definition.LegacyAliases, key) {
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("%w: unknown permission key %q", apperrors.ErrInvalidArgument, key)
		}
	}
	expanded, err := expandLegacyPermissionKeys(permissionKeys)
	if err != nil {
		return nil, err
	}
	canonical := make([]string, 0, len(expanded))
	for _, key := range expanded {
		if IsActiveAssignablePermission(key) {
			canonical = append(canonical, key)
		}
	}
	return normalizePermissionKeys(canonical), nil
}
