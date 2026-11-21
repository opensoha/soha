package assistantmesh

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type inheritanceRoles struct{}

func (inheritanceRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{
		"admin":  {appaccess.PermAccessUsersInheritHeyaAssets},
		"editor": {appaccess.PermAccessUsersManage},
	}, nil
}

type inheritanceRecorder struct{ called bool }

func (*inheritanceRecorder) ListInheritableAssets(context.Context, string, string) (domain.InheritancePage, error) {
	panic("test must not list assets")
}

func (s *inheritanceRecorder) DisposeInheritedAssets(ctx context.Context, input domain.InheritanceInput, record func(context.Context, []string) error) (int, error) {
	s.called = true
	if err := record(ctx, input.AssetIDs); err != nil {
		return 0, err
	}
	return len(input.AssetIDs), nil
}

func TestInheritanceRequiresUserManagementAndAuditsTransfer(t *testing.T) {
	f := newFixture(t)
	f.service.permissions = appaccess.NewPermissionResolver(inheritanceRoles{})
	store := &inheritanceRecorder{}
	service := NewInheritance(f.service, store)
	input := domain.InheritanceInput{OwnerID: receptionOwner, HeirID: receptionCaller, AssetIDs: []string{receptionReq}}
	editor := employee("user-a")
	editor.Roles = []string{"editor"}
	if _, err := service.Transfer(t.Context(), editor, input); !errors.Is(err, apperrors.ErrAccessDenied) || store.called {
		t.Fatalf("non-admin transfer reached store: %v", err)
	}
	duplicate := input
	duplicate.AssetIDs = []string{receptionReq, receptionReq}
	if _, err := service.Transfer(t.Context(), admin(), duplicate); !errors.Is(err, apperrors.ErrInvalidArgument) || store.called {
		t.Fatalf("duplicate selection reached store: %v", err)
	}
	result, err := service.Transfer(t.Context(), admin(), input)
	if err != nil || result.Transferred != 1 || len(f.audit.entries) != 1 ||
		f.audit.entries[0].Metadata["heirId"] != input.HeirID {
		t.Fatalf("missing transfer receipt: %+v, %v, %+v", result, err, f.audit.entries)
	}
	if _, err := service.Retire(t.Context(), editor, input.OwnerID, input.AssetIDs); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("user-management permission allowed direct retirement: %v", err)
	}
	if count, err := service.Retire(t.Context(), admin(), input.OwnerID, input.AssetIDs); err != nil || count != 1 ||
		len(f.audit.entries) != 2 || f.audit.entries[1].Action != "assistant_mesh.inheritance.retire" {
		t.Fatalf("missing direct retirement receipt: %d, %v, %+v", count, err, f.audit.entries)
	}
}
