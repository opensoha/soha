package access

import (
	"context"
	"errors"
	domain "github.com/opensoha/soha/internal/domain/access"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"slices"
	"testing"
)

func TestManagedNASCatalogRoleAssignmentAndSnapshot(t *testing.T) {
	SetRolePermissionMatrix(nil)
	t.Cleanup(func() { SetRolePermissionMatrix(nil) })
	ctx := context.Background()
	keys := []string{PermNetworkAccessNASBindingsView, PermNetworkAccessNASBindingsCreate, PermNetworkAccessNASBindingsUpdate}
	for _, key := range keys {
		p, found, e := permissionDefinition(key)
		if e != nil || !found || !p.Assignable || p.Status != "active" {
			t.Fatalf("catalog cannot assign %s: %+v %v", key, p, e)
		}
		resourceScope := false
		for _, scope := range p.ScopeKinds {
			resourceScope = resourceScope || string(scope) == "resource"
		}
		if !resourceScope {
			t.Fatalf("no site resource scope: %+v", p)
		}
	}
	canonical, e := CanonicalAssignablePermissionKeys(keys)
	if e != nil || len(canonical) != 3 {
		t.Fatalf("canonical assignment rejected: %v", e)
	}
	if _, e := CanonicalAssignablePermissionKeys([]string{"network_access.nas_bindings.delete"}); !errors.Is(e, apperrors.ErrInvalidArgument) {
		t.Fatal("unapproved delete permission became assignable")
	}
	matrix := defaultRolePermissions()
	for _, key := range keys {
		if !slices.Contains(matrix["admin"], key) {
			t.Fatalf("default admin enumeration missing %s", key)
		}
	}
	store := &capturePolicyManager{}
	creator := NewManagement(stubUserManager{}, store, NewPermissionResolver(stubRolePermissionReader{matrix: matrix}), nil, nil)
	role, e := creator.CreateRole(ctx, identity.Principal{UserID: "admin-user", Roles: []string{"admin"}}, domain.RoleInput{ID: "radius-site-operator", Name: "Managed NAS operator", PermissionKeys: keys})
	if e != nil {
		t.Fatal(e)
	}
	if len(store.lastRoleInput.PermissionKeys) != 3 {
		t.Fatal("validated role keys not written")
	}
	matrix[role.ID] = role.PermissionKeys
	catalog := NewCatalog(nil, stubPolicyCatalogReader{roles: []domain.RoleRecord{role}}, nil, nil, NewPermissionResolver(stubRolePermissionReader{matrix: matrix}))
	snapshot, e := catalog.PermissionSnapshot(ctx, identity.Principal{UserID: "nas-user", Roles: []string{role.ID}})
	if e != nil || !slices.Equal(snapshot.PermissionKeys, canonical) {
		t.Fatalf("role snapshot lost managed NAS grant: %+v %v", snapshot, e)
	}
	for _, p := range snapshot.PermissionKeys {
		if p == PermNetworkAccessSitesUpdate || p == PermNetworkAccessSitesDelete || p == "network_access.nas_bindings.delete" {
			t.Fatalf("role gained unrelated grant %s", p)
		}
	}
	public, e := catalog.PermissionCatalog(ctx, identity.Principal{UserID: "admin-user", Roles: []string{"admin"}})
	if e != nil || len(public.Permissions) == 0 {
		t.Fatalf("real catalog unavailable: %v", e)
	}
	old, e := CanonicalAssignablePermissionKeys([]string{PermNetworkAccessSitesView, PermNetworkAccessSitesUpdate})
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range keys {
		if slices.Contains(old, key) {
			t.Fatalf("legacy sites grant silently promoted to %s", key)
		}
	}
}
