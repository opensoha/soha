package outpostruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/opensoha/soha/internal/application/access"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type runtimePermissionGrants map[string][]string

func (r runtimePermissionGrants) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r, nil
}

func TestRuntimeUpdatesRequireCanonicalUpdatePermission(t *testing.T) {
	for _, test := range []struct {
		name       string
		permission string
		allowWrite bool
	}{
		{"view only", "identity.outposts.view", false},
		{"canonical update only", "identity.outposts.update", true},
		{"create only", "identity.outposts.create", false},
		{"legacy manage", "identity.outposts.manage", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := &memoryRepository{state: domain.State{OutpostID: "edge", Owner: "external"}}
			permissions := access.NewPermissionResolver(runtimePermissionGrants{"runtime-operator": {test.permission}})
			service, err := New(repo, permissions, []domain.Slot{testSlot()}, nil)
			if err != nil {
				t.Fatal(err)
			}
			service.executors["executor"] = true
			principal := identity.Principal{UserID: "operator", Roles: []string{"runtime-operator"}, PermissionKeys: []string{test.permission}}
			if test.permission == "identity.outposts.view" {
				if _, err := service.View(ctx, principal, "edge"); err != nil {
					t.Fatalf("view permission could not read runtime: %v", err)
				}
			}
			_, err = service.Bind(ctx, principal, "edge", domain.BindingInput{SlotID: "slot", ExplicitAdoption: true, ExpectedGeneration: 0, RequestID: "bind"})
			if test.allowWrite {
				if err != nil || repo.commits != 1 || repo.state.Owner != "soha" {
					t.Fatalf("permitted bind failed: commits=%d state=%+v err=%v", repo.commits, repo.state, err)
				}
			} else if !errors.Is(err, apperrors.ErrAccessDenied) || repo.commits != 0 {
				t.Fatalf("bind reached persistence without update: commits=%d err=%v", repo.commits, err)
			}
			// All Control prerequisites are present even when Bind was denied,
			// so its rejection must come from permission, not external ownership.
			repo.state = domain.State{OutpostID: "edge", Owner: "soha", OwnerEpoch: 1, SlotID: "slot", UID: "uid", Generation: 1}
			before := repo.commits
			_, err = service.Control(ctx, principal, "edge", domain.ControlInput{Action: "start", ExpectedGeneration: 1, RequestID: "start"})
			if test.allowWrite {
				if err != nil || repo.commits != before+1 || repo.mutation.Task == nil {
					t.Fatalf("permitted control failed: commits=%d err=%v", repo.commits, err)
				}
			} else if !errors.Is(err, apperrors.ErrAccessDenied) || repo.commits != before {
				t.Fatalf("control reached persistence without update: commits=%d err=%v", repo.commits, err)
			}
		})
	}
}
