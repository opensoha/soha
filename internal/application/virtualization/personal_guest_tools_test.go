package virtualization

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	appaccess "github.com/opensoha/soha/internal/application/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPersonalGuestToolsRefuseForeignOwnerAndOriginalDispatchReplay(t *testing.T) {
	s, _, principal, binding := personalGuestVMFixture(t)
	tasks, ok := s.tasks.(*memoryRepo)
	if !ok {
		t.Fatal("unexpected task repository fixture")
	}
	s.permissions = appaccess.NewPermissionResolver(testRoleReader{matrix: map[string][]string{"admin": {appaccess.PermVirtualizationVMsView, appaccess.PermVirtualizationVMsResize, vmConnectPermission}}})
	s.personalGuestAuthorize = func(context.Context, guest.Binding) error { return nil }
	s.personalGuestToolsCheckpoint = func(context.Context, guest.ToolsPlan) error { return nil }
	s.personalGuestTransaction = func(ctx context.Context, f func(context.Context) error) error { return f(ctx) }
	s.executionPrincipals = executionPrincipalFunc(func(context.Context, string, string) (identity.Principal, error) { return principal, nil })
	plan := guest.ToolsPlan{SchemaVersion: guest.ToolsPlanVersion, OperationID: uuid.NewString(), Binding: binding, ArtifactSHA256: strings.Repeat("a", 64), DiskQuotaBytes: 6 << 30, CPUQuotaMilli: 1000, MemoryBytes: 2 << 30, PidsLimit: 64, DockerImage: "docker.io/library/debian@sha256:" + strings.Repeat("b", 64), BubblewrapVersion: "0.12.0-1", DockerVersion: "26.1.5-1", ChromiumVersion: "154.0.1-1"}
	other := principal
	other.UserID = uuid.NewString()
	if _, err := s.QueuePersonalGuestTools(t.Context(), other, plan, 5); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("foreign tools owner accepted", err)
	}
	task := domain.Task{ID: plan.OperationID, VMID: binding.ComputeRef, ConnectionID: "guest-source", Provider: ProviderPVE, RequestedBy: principal.UserID, TaskKind: TaskKindPersonalGuestTools, Status: TaskStatusRunning, Payload: map[string]any{"personalGuestToolsPlan": plan, "providerDispatchStarted": true, "executionActorId": principal.UserID, "environmentVersion": "5"}}
	tasks.tasks[task.ID] = task
	if err := s.dispatchGuestTools(t.Context(), task); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("original tools dispatch repeated", err)
	}
	if _, err := s.FindPersonalGuestTools(t.Context(), other, task.ID, 5); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("foreign original tools receipt read", err)
	}
	if _, err := s.FindPersonalGuestTools(t.Context(), principal, task.ID, 6); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("changed tools input accepted", err)
	}
	task.Payload["providerDispatchStarted"] = false
	tasks.tasks[task.ID] = task
	if _, err := s.PersonalGuestToolsGrant(t.Context(), binding, task.ID); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("undispatched root operation authorized", err)
	}
	plan.Binding.ExpiresAt = time.Now().Add(-time.Second)
	if plan.Validate(time.Now()) == nil {
		t.Fatal("expired tools plan accepted")
	}
}
