package scopegrant

import (
	"context"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
)

type stubScopeGrantRepository struct {
	items []domainscopegrant.Record
}

func (s *stubScopeGrantRepository) List(context.Context) ([]domainscopegrant.Record, error) {
	return s.items, nil
}
func (s *stubScopeGrantRepository) Get(context.Context, string) (domainscopegrant.Record, error) {
	return domainscopegrant.Record{}, nil
}
func (s *stubScopeGrantRepository) Create(context.Context, domainscopegrant.Input) (domainscopegrant.Record, error) {
	return domainscopegrant.Record{}, nil
}
func (s *stubScopeGrantRepository) Update(context.Context, string, domainscopegrant.Input) (domainscopegrant.Record, error) {
	return domainscopegrant.Record{}, nil
}
func (s *stubScopeGrantRepository) Delete(context.Context, string) error { return nil }

type stubRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

func TestListRequiresScopeGrantViewPermission(t *testing.T) {
	service := New(&stubScopeGrantRepository{}, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {appaccess.PermAccessUsersView},
		},
	}), nil, nil)

	_, err := service.List(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}})
	if err == nil {
		t.Fatalf("List error = nil, want access denied")
	}
}

func TestCreateRequiresScopeGrantManagePermission(t *testing.T) {
	service := New(&stubScopeGrantRepository{}, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {appaccess.PermAccessScopeGrantsView},
		},
	}), nil, nil)

	_, err := service.Create(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, domainscopegrant.Input{
		SubjectType:    "user",
		SubjectID:      "user-1",
		BusinessLineID: "bl-1",
		Role:           "developer",
	})
	if err == nil {
		t.Fatalf("Create error = nil, want access denied")
	}
}
