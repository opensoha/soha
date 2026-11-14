package personalenvironment

import (
	"context"
	"errors"
	"testing"
	"time"

	contractenv "github.com/opensoha/soha/contracts/agent/environment"
	guest "github.com/opensoha/soha/contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type vmFixture struct {
	approval   domain.PersonalAgentAccess
	registered domain.PersonalGuestRecord
	bindingErr error
	result     guest.RuntimeResult
	invoked    []guest.RuntimeOperation
	recovered  []domain.PersonalEnvironmentRecord
}

func (f *vmFixture) CurrentPrincipal(_ context.Context, owner string) (identity.Principal, error) {
	return identity.Principal{UserID: owner}, nil
}

func (f *vmFixture) GetPersonalAgentAccess(context.Context, string) (domain.PersonalAgentAccess, error) {
	return f.approval, nil
}

func (f *vmFixture) AuthorizePersonalVMAssignment(context.Context, identity.Principal, string) error {
	return nil
}

func (f *vmFixture) CurrentPersonalGuest(context.Context, string, string) (domain.PersonalGuestRecord, error) {
	return f.registered, nil
}

func (f *vmFixture) CheckPersonalGuestBinding(context.Context, domain.PersonalGuestRecord) error {
	return f.bindingErr
}

func (f *vmFixture) PersonalRuntimeArtifact() string { return "runtime-artifact" }

func (f *vmFixture) InvokePersonalGuestRuntime(_ context.Context, _ identity.Principal, op guest.RuntimeOperation) (guest.RuntimeResult, error) {
	f.invoked = append(f.invoked, op)
	return f.result, nil
}

func (f *vmFixture) RecoverTerminalPersonalRuntime(_ context.Context, item domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error) {
	f.recovered = append(f.recovered, item)
	item.OperationID, item.ActiveTaskID = "", ""
	return item, nil
}

func vmTestDependencies(f *vmFixture) VMDependencies {
	return VMDependencies{
		Identity: f, Approvals: f, Assignment: f, Runtime: f, Guests: f,
		ObserveVM: func(context.Context, identity.Principal, string, string) (VMObservation, error) {
			return VMObservation{}, apperrors.ErrServiceUnavailable
		},
		FrozenRuntime: func(context.Context, identity.Principal, string) (guest.RuntimeOperation, bool, error) {
			return guest.RuntimeOperation{}, false, apperrors.ErrNotFound
		},
	}
}

func mustNewVM(t *testing.T, deps VMDependencies) *VM {
	t.Helper()
	vm, err := NewVM(deps)
	if err != nil {
		t.Fatal(err)
	}
	return vm
}

func TestPersonalVMAssignmentRequiresOwnerApprovalBeforeObservation(t *testing.T) {
	policy := domain.PersonalVMPolicy{
		OwnerID: "11111111-1111-4111-8111-111111111111", ComputeRef: "22222222-2222-4222-8222-222222222222",
		Provider: "pve", DataDiskSerial: "soha-data", DataDiskBytes: 1 << 30,
	}
	f := &vmFixture{}
	deps := vmTestDependencies(f)
	observations := 0
	deps.ObserveVM = func(_ context.Context, current identity.Principal, computeRef, serial string) (VMObservation, error) {
		observations++
		if current.UserID != policy.OwnerID || computeRef != policy.ComputeRef || serial != policy.DataDiskSerial {
			t.Fatalf("observation did not use current owner and policy target")
		}
		return VMObservation{Provider: policy.Provider, Identity: domain.PersonalVMIdentity{DataDiskSerial: serial, DataDiskBytes: policy.DataDiskBytes}}, nil
	}
	vm := mustNewVM(t, deps)
	admin := identity.Principal{UserID: "admin"}
	if err := vm.ValidateAssignment(t.Context(), admin, policy); !errors.Is(err, apperrors.ErrAccessDenied) || observations != 0 {
		t.Fatalf("unapproved assignment = %v, observations = %d", err, observations)
	}
	f.approval = domain.PersonalAgentAccess{UserID: policy.OwnerID, Status: "approved"}
	if err := vm.ValidateAssignment(t.Context(), admin, policy); err != nil || observations != 1 {
		t.Fatalf("approved assignment = %v, observations = %d", err, observations)
	}
}

func TestPersonalVMInvocationPreservesGuestAndLeaseFences(t *testing.T) {
	now := time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC)
	item := domain.PersonalEnvironmentRecord{
		CreatedBy: "owner", AssistantID: "assistant", OperationID: "current-operation", DataRef: "personal-data",
		Lease: contractenv.Lease{Mode: "restricted-write"},
	}
	tests := []struct {
		name          string
		heartbeatAge  time.Duration
		leaseTTL      time.Duration
		bindingTTL    time.Duration
		bindingErr    error
		omitFileWrite bool
		wantDeadline  time.Duration
		wantErr       error
		wantDispatch  bool
	}{
		{name: "runtime limit", leaseTTL: time.Minute, bindingTTL: time.Minute, wantDeadline: 50 * time.Second, wantDispatch: true},
		{name: "lease expires first", leaseTTL: 20 * time.Second, bindingTTL: time.Minute, wantDeadline: 20 * time.Second, wantDispatch: true},
		{name: "binding expires first", leaseTTL: time.Minute, bindingTTL: 10 * time.Second, wantDeadline: 10 * time.Second, wantDispatch: true},
		{name: "stale heartbeat", heartbeatAge: time.Minute + time.Second, wantErr: apperrors.ErrServiceUnavailable},
		{name: "outdated binding", bindingErr: apperrors.ErrAccessDenied, wantErr: apperrors.ErrAccessDenied},
		{name: "missing write proof", leaseTTL: time.Minute, bindingTTL: time.Minute, omitFileWrite: true, wantErr: apperrors.ErrServiceUnavailable, wantDeadline: 50 * time.Second, wantDispatch: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heartbeat := now.Add(-tt.heartbeatAge)
			f := &vmFixture{
				registered: domain.PersonalGuestRecord{Status: "registered", LastHeartbeatAt: &heartbeat, Binding: guest.Binding{
					OwnerID: item.CreatedBy, AssistantID: item.AssistantID, ComputeGeneration: 2, ExpiresAt: now.Add(tt.bindingTTL),
				}},
				bindingErr: tt.bindingErr,
				result: guest.RuntimeResult{Proof: &guest.RuntimeProof{
					FilesystemID: item.DataRef, Command: true, CredentialsHidden: true, NamespacesIsolated: true,
					FileRead: true, FileWrite: !tt.omitFileWrite, Docker: true, Browser: true,
				}},
			}
			vm := mustNewVM(t, vmTestDependencies(f))
			vm.now = func() time.Time { return now }
			_, err := vm.Invoke(t.Context(), identity.Principal{UserID: item.CreatedBy}, item, guest.RuntimeOperation{
				Kind: "environment", Request: contractenv.Request{Action: "acquire", Lease: contractenv.Lease{ExpiresAt: now.Add(tt.leaseTTL)}},
			})
			if !errors.Is(err, tt.wantErr) || errors.Is(err, domain.ErrPersonalEnvironmentNotDispatched) != !tt.wantDispatch {
				t.Fatalf("invocation = %v, want %v (dispatched %v)", err, tt.wantErr, tt.wantDispatch)
			}
			if !tt.wantDispatch {
				if len(f.invoked) != 0 {
					t.Fatalf("rejected call dispatched %d times", len(f.invoked))
				}
				return
			}
			if len(f.invoked) != 1 {
				t.Fatalf("runtime calls = %d, want one", len(f.invoked))
			}
			op := f.invoked[0]
			if op.SchemaVersion != guest.RuntimeOperationVersion || op.OperationID != item.OperationID || op.Binding != f.registered.Binding || op.ArtifactSHA256 != f.PersonalRuntimeArtifact() || !op.Deadline.Equal(now.Add(tt.wantDeadline)) {
				t.Fatalf("runtime received unexpected frozen operation: %+v", op)
			}
		})
	}
}

func TestPersonalVMRecoveryUsesOriginalTerminalOperation(t *testing.T) {
	item := domain.PersonalEnvironmentRecord{
		CreatedBy: "owner", AssistantID: "assistant", ComputeRef: "vm", ComputeGeneration: 3, OperationID: "operation", Version: 4,
	}
	tests := []struct {
		name       string
		terminal   bool
		readErr    error
		activeTask string
		change     func(*guest.RuntimeOperation)
		wantErr    error
	}{
		{name: "terminal native inspect", terminal: true},
		{name: "terminal user task", terminal: true, activeTask: "task", change: func(op *guest.RuntimeOperation) { op.Source, op.Request.TaskID = "user", "task" }},
		{name: "no receipt for idle reservation", readErr: apperrors.ErrNotFound},
		{name: "no receipt for active task", readErr: apperrors.ErrNotFound, activeTask: "task", wantErr: apperrors.ErrNotFound},
		{name: "different owner rejected by receipt reader", readErr: apperrors.ErrAccessDenied, wantErr: apperrors.ErrAccessDenied},
		{name: "nonterminal", wantErr: apperrors.ErrConflict},
		{name: "late operation", terminal: true, change: func(op *guest.RuntimeOperation) { op.OperationID = "old" }, wantErr: apperrors.ErrConflict},
		{name: "wrong assistant", terminal: true, change: func(op *guest.RuntimeOperation) { op.Binding.AssistantID = "foreign" }, wantErr: apperrors.ErrConflict},
		{name: "obsolete compute generation", terminal: true, change: func(op *guest.RuntimeOperation) { op.Binding.ComputeGeneration-- }, wantErr: apperrors.ErrConflict},
		{name: "active native task", terminal: true, activeTask: "task", wantErr: apperrors.ErrConflict},
		{name: "native mutation", terminal: true, change: func(op *guest.RuntimeOperation) { op.Request.Action = "acquire" }, wantErr: apperrors.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := item
			original.ActiveTaskID = tt.activeTask
			f := &vmFixture{}
			deps := vmTestDependencies(f)
			deps.FrozenRuntime = func(_ context.Context, p identity.Principal, id string) (guest.RuntimeOperation, bool, error) {
				if p.UserID != original.CreatedBy || id != "18836a79-3935-59f3-97d4-1b3f682c63a3" {
					t.Fatalf("receipt lookup got owner %q and task ID %q", p.UserID, id)
				}
				op := guest.RuntimeOperation{OperationID: original.OperationID, Source: "native",
					Binding: guest.Binding{AssistantID: original.AssistantID, ComputeGeneration: original.ComputeGeneration},
					Request: contractenv.Request{Action: "inspect"},
				}
				if tt.change != nil {
					tt.change(&op)
				}
				return op, tt.terminal, tt.readErr
			}
			recovered, err := mustNewVM(t, deps).Recover(t.Context(), identity.Principal{UserID: original.CreatedBy}, original)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("recovery = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				if len(f.recovered) != 1 || f.recovered[0].Version != original.Version || f.recovered[0].OperationID != original.OperationID || recovered.OperationID != "" || recovered.ActiveTaskID != "" {
					t.Fatalf("recovery did not release exactly the original reservation: %+v", f.recovered)
				}
			} else if len(f.recovered) != 0 || recovered.OperationID != original.OperationID || recovered.ActiveTaskID != original.ActiveTaskID {
				t.Fatalf("failed recovery changed reservation: %+v", recovered)
			}
		})
	}
}
