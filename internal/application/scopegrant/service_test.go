package scopegrant

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type stubScopeGrantRepository struct {
	items        []domainscopegrant.Record
	getItem      domainscopegrant.Record
	updatedID    string
	updatedInput domainscopegrant.Input
	deletedID    string
}

func (s *stubScopeGrantRepository) List(context.Context) ([]domainscopegrant.Record, error) {
	return s.items, nil
}
func (s *stubScopeGrantRepository) ListBySubject(_ context.Context, subjectType, subjectID string) ([]domainscopegrant.Record, error) {
	items := make([]domainscopegrant.Record, 0)
	for _, item := range s.items {
		if item.SubjectType == subjectType && item.SubjectID == subjectID {
			items = append(items, item)
		}
	}
	return items, nil
}
func (s *stubScopeGrantRepository) Get(context.Context, string) (domainscopegrant.Record, error) {
	return s.getItem, nil
}
func (s *stubScopeGrantRepository) Create(context.Context, domainscopegrant.Input) (domainscopegrant.Record, error) {
	return domainscopegrant.Record{}, nil
}
func (s *stubScopeGrantRepository) Update(_ context.Context, id string, input domainscopegrant.Input) (domainscopegrant.Record, error) {
	s.updatedID = id
	s.updatedInput = input
	return domainscopegrant.Record{ID: id, SubjectType: input.SubjectType, SubjectID: input.SubjectID}, nil
}
func (s *stubScopeGrantRepository) Delete(_ context.Context, id string) error {
	s.deletedID = id
	return nil
}

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

	_, err := service.List(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, "user", "user-1")
	if err == nil {
		t.Fatalf("List error = nil, want access denied")
	}
}

func TestLegacyScopeGrantOperationsPreserveGlobalSemantics(t *testing.T) {
	repo := &stubScopeGrantRepository{items: []domainscopegrant.Record{
		{ID: "grant-user", SubjectType: "user", SubjectID: "user-1"},
		{ID: "grant-team", SubjectType: "team", SubjectID: "team-1"},
	}}
	service := New(repo, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {
				appaccess.PermAccessScopeGrantsView,
				appaccess.ManagedActionPermission(appaccess.PermAccessScopeGrantsManage, "update"),
				appaccess.ManagedActionPermission(appaccess.PermAccessScopeGrantsManage, "delete"),
			},
		},
	}), nil, nil)
	principal := domainidentity.Principal{Roles: []string{"delegated"}}

	items, err := service.ListLegacy(context.Background(), principal)
	if err != nil || len(items) != 2 {
		t.Fatalf("ListLegacy() items = %#v, error = %v, want both subjects", items, err)
	}
	input := domainscopegrant.Input{SubjectType: "team", SubjectID: "team-1", BusinessLineID: "bl-1", Role: "developer"}
	if _, err := service.UpdateLegacy(context.Background(), principal, "grant-team", input); err != nil {
		t.Fatalf("UpdateLegacy() error = %v", err)
	}
	if repo.updatedID != "grant-team" || repo.updatedInput.SubjectID != "team-1" {
		t.Fatalf("legacy update = (%q, %#v), want body subject unchanged", repo.updatedID, repo.updatedInput)
	}
	if err := service.DeleteLegacy(context.Background(), principal, "grant-team"); err != nil {
		t.Fatalf("DeleteLegacy() error = %v", err)
	}
	if repo.deletedID != "grant-team" {
		t.Fatalf("legacy delete id = %q, want grant-team", repo.deletedID)
	}
}

func TestCreateRequiresScopeGrantManagePermission(t *testing.T) {
	service := New(&stubScopeGrantRepository{}, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {appaccess.PermAccessScopeGrantsView},
		},
	}), nil, nil)

	_, err := service.Create(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, "user", "user-1", domainscopegrant.Input{
		SubjectType:    "user",
		SubjectID:      "user-1",
		BusinessLineID: "bl-1",
		Role:           "developer",
	})
	if err == nil {
		t.Fatalf("Create error = nil, want access denied")
	}
}

func TestUpdateRejectsGrantOutsideRouteSubject(t *testing.T) {
	repo := &stubScopeGrantRepository{getItem: domainscopegrant.Record{SubjectType: "user", SubjectID: "user-2"}}
	service := New(repo, appaccess.NewPermissionResolver(stubRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {appaccess.ManagedActionPermission(appaccess.PermAccessScopeGrantsManage, "update")},
		},
	}), nil, nil)

	_, err := service.Update(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, "user", "user-1", "grant-1", domainscopegrant.Input{
		SubjectType: "user", SubjectID: "user-1", BusinessLineID: "bl-1", Role: "developer",
	})
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Update error = %v, want not found", err)
	}
}

func TestValidatePlatformScopeGrantRequiresDirectClusterScope(t *testing.T) {
	base := domainscopegrant.Input{
		SubjectType: "user", SubjectID: "user-1", ScopeType: domainscopegrant.ScopeTypePlatform,
		Role: "ops", Effect: "allow",
	}
	if err := validateInput(base); err == nil {
		t.Fatal("validateInput() error = nil, want missing clusterIds error")
	}
	base.ClusterIDs = []string{"cluster-a"}
	base.Namespaces = []string{"minio"}
	if err := validateInput(base); err != nil {
		t.Fatalf("validateInput() error = %v for valid platform grant", err)
	}
	base.NamespaceSelector = "tenant="
	if err := validateInput(base); err == nil {
		t.Fatal("validateInput() error = nil, want invalid namespaceSelector error")
	}
}

func TestValidateLegacyScopeGrantPreservesBusinessLineRequirement(t *testing.T) {
	input := domainscopegrant.Input{SubjectType: "user", SubjectID: "user-1", Role: "readonly"}
	if err := validateInput(input); err == nil {
		t.Fatal("validateInput() error = nil, want legacy businessLineId requirement")
	}
}
