package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type nativeTaskGuardFixture struct {
	parent    domaincopilot.NativeTaskParent
	principal domainidentity.Principal
	stop      string
	err       error
}

func (f *nativeTaskGuardFixture) WithNativeTaskCreation(ctx context.Context, _ domaincopilot.NativeLease, register func(context.Context, domaincopilot.NativeTaskParent, domainidentity.Principal) error) error {
	if f.err != nil {
		return f.err
	}
	return register(ctx, f.parent, f.principal)
}
func (f *nativeTaskGuardFixture) NativeTaskStopReason(context.Context, domaincopilot.NativeTaskParent) (string, error) {
	return f.stop, f.err
}
func (f *nativeTaskGuardFixture) ReserveNativeTaskToolCall(ctx context.Context, _ domaincopilot.NativeTaskParent, cancellation bool) error {
	if f.err != nil {
		return f.err
	}
	if f.stop != "" && !cancellation {
		return apperrors.ErrConflict
	}
	return ctx.Err()
}
func (f *nativeTaskGuardFixture) WithNativeTaskParent(ctx context.Context, _ domaincopilot.NativeTaskParent, cancellation bool, execute func(context.Context) error) error {
	if f.err != nil {
		return f.err
	}
	if f.stop != "" && !cancellation {
		return apperrors.ErrConflict
	}
	ctx, cancel := context.WithDeadline(ctx, f.parent.Deadline)
	defer cancel()
	return execute(ctx)
}

func TestNativeCapabilityRegistrationFreezesOriginalParentAndDeadline(t *testing.T) {
	service, repo, provider := newCapabilityExecutorFixture(t, "satisfied")
	intent, _ := domainworkflow.CapabilityIntentFrom(repo.run)
	principal, _ := provider.CurrentExecutionPrincipal(t.Context(), "actor", "frozen-token")
	parent := domaincopilot.NativeTaskParent{RunID: "native", OwnerID: "actor", SessionID: "original-session", RequestHash: "original-request", Deadline: time.Now().Add(time.Minute)}
	guard := &nativeTaskGuardFixture{parent: parent, principal: principal}
	if err := service.SetNativeTaskGuard(guard); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateNativeCapabilityTask(t.Context(), domaincopilot.NativeLease{}, intent.Input); err != nil {
		t.Fatal(err)
	}
	stored, _ := domainworkflow.CapabilityIntentFrom(repo.run)
	if !nativeCapabilityParentMatches(stored.NativeParent, parent) || stored.ActorID != "actor" || stored.ActorTokenID != "frozen-token" || stored.ActorSessionID != parent.SessionID || !stored.Deadline.Equal(parent.Deadline) || len(stored.PermissionCeiling) == 0 {
		t.Fatalf("original execution context lost: %+v", stored)
	}
	if _, err := reviseCapabilityRun(repo.run, stored); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("Native parent was detached by plan revision: %v", err)
	}
	guard.principal.UserID = "another-actor"
	if _, err := service.CreateNativeCapabilityTask(t.Context(), domaincopilot.NativeLease{}, intent.Input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("cross-owner creation accepted: %v", err)
	}
	var absent *nativeTaskGuardFixture
	for _, invalid := range []NativeTaskGuard{nil, absent} {
		if err := service.SetNativeTaskGuard(invalid); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("missing guard accepted: %v", err)
		}
	}
}

func TestNativeCapabilityWorkerStopsBeforeDispatchWhenParentStops(t *testing.T) {
	for _, scenario := range []string{"active", "cancel", "evidence_revoked", "unavailable", "missing_guard"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, provider := newCapabilityExecutorFixture(t, "satisfied")
			parent := domaincopilot.NativeTaskParent{RunID: "native", OwnerID: "actor", SessionID: "session", RequestHash: "frozen", Deadline: time.Now().Add(time.Minute)}
			guard := &nativeTaskGuardFixture{parent: parent}
			intent, _ := domainworkflow.CapabilityIntentFrom(repo.run)
			intent.NativeParent = &parent
			repo.run.Metadata["capabilityIntent"] = intent
			switch scenario {
			case "cancel":
				guard.stop = "user"
			case "evidence_revoked":
				guard.stop = "failure"
			case "unavailable":
				guard.err = apperrors.ErrConflict
			}
			if scenario != "missing_guard" {
				if err := service.SetNativeTaskGuard(guard); err != nil {
					t.Fatal(err)
				}
				runtime, ok := service.capabilityRuntime.(*appaigateway.Service)
				if !ok {
					t.Fatal("Native Gateway runtime fixture is unavailable")
				}
				if err := runtime.SetNativeTaskGuard(guard); err != nil {
					t.Fatal(err)
				}
			}
			tickCapabilityFixture(t, service, repo)
			if scenario == "active" {
				tickCapabilityFixture(t, service, repo)
				if repo.run.Status != "completed" || len(provider.calls) != 2 {
					t.Fatalf("authorized child did not finish: %s %v", repo.run.Status, provider.calls)
				}
			} else if len(provider.calls) != 0 || repo.run.Status == "completed" || repo.run.Status == "running" {
				t.Fatalf("stopped parent dispatched: %s %v", repo.run.Status, provider.calls)
			}
		})
	}
}
