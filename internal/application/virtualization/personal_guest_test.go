package virtualization

import (
	"context"
	"encoding/json"
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

type personalGuestDispatchCounter struct {
	*sshIdentityAdapter
	calls int
}

func (a *personalGuestDispatchCounter) ProvisionPersonalGuest(context.Context, domain.AdapterConnection, domain.AdapterVM, guest.Provisioning) (guest.DataResult, error) {
	a.calls++
	return guest.DataResult{}, nil
}

func TestPersonalGuestReconciliationKeepsOriginalOperationAndWorkspace(t *testing.T) {
	s, adapter, principal, binding := personalGuestVMFixture(t)
	s.permissions = appaccess.NewPermissionResolver(testRoleReader{matrix: map[string][]string{"admin": {appaccess.PermVirtualizationVMsView, appaccess.PermVirtualizationVMsResize, vmConnectPermission}}})
	plan := guest.DataPlan{SchemaVersion: guest.DataPlanVersion, OperationID: uuid.NewString(), Binding: binding, DiskQuotaBytes: 6 << 30}
	old := domain.Task{ID: plan.OperationID, VMID: binding.ComputeRef, Provider: ProviderPVE, ConnectionID: "guest-source", RequestedBy: principal.UserID, TaskKind: TaskKindPersonalGuest, Status: TaskStatusFailed, AttemptCount: 1, Payload: map[string]any{"personalGuestPlan": plan, "providerDispatchStarted": true}}
	repo, ok := s.tasks.(*memoryRepo)
	if !ok {
		t.Fatal("expected test repository")
	}
	repo.tasks[old.ID] = old
	for _, edit := range []func(*domain.Task){
		func(v *domain.Task) { v.RequestedBy = uuid.NewString() }, func(v *domain.Task) { v.VMID = uuid.NewString() },
		func(v *domain.Task) { v.Status = TaskStatusRunning }, func(v *domain.Task) { v.AttemptCount = 3 },
	} {
		changed := old
		edit(&changed)
		repo.tasks[old.ID] = changed
		if err := s.AuthorizePersonalGuestReconciliation(t.Context(), principal, old.ID, old.VMID); err == nil {
			t.Fatal("invalid original operation reconciled")
		}
	}
	repo.tasks[old.ID] = old
	for _, edit := range []func(*guest.DataPlan){func(p *guest.DataPlan) { p.Binding.DataRef = uuid.NewString() }, func(p *guest.DataPlan) { p.DiskQuotaBytes -= 1024 }} {
		changed := plan
		edit(&changed)
		if _, err := s.savePersonalGuestTask(t.Context(), principal, old, changed); err == nil {
			t.Fatal("reconciliation changed data identity or quota")
		}
	}
	fresh := plan
	fresh.Binding.ID = uuid.NewString()
	fresh.Binding.Version++
	next := old
	next.Payload = map[string]any{"personalGuestPlan": fresh}
	updated, err := s.savePersonalGuestTask(t.Context(), principal, next, fresh)
	if err != nil || updated.ID != old.ID || updated.VMID != old.VMID || updated.RequestedBy != old.RequestedBy || updated.Status != TaskStatusQueued || updated.AttemptCount != 1 || boolValue(updated.Payload, "providerDispatchStarted") {
		t.Fatal("explicit reconcile lost original operation", err)
	}
	count := &personalGuestDispatchCounter{sshIdentityAdapter: adapter}
	s.adapters[ProviderPVE] = count
	s.executionPrincipals = executionPrincipalFunc(func(context.Context, string, string) (identity.Principal, error) { return principal, nil })
	s.personalGuestAuthorize = func(context.Context, guest.Binding) error { return nil }
	old.Status = TaskStatusRunning
	repo.tasks[old.ID] = old
	if _, err := s.executePersonalGuest(t.Context(), old); err == nil || count.calls != 0 {
		t.Fatal("unknown prior dispatch was repeated", err)
	}
}

func personalGuestVMFixture(t *testing.T) (*Service, *sshIdentityAdapter, identity.Principal, guest.Binding) {
	t.Helper()
	repo := newMemoryRepo()
	connection := domain.Connection{ID: "guest-source", Provider: ProviderPVE, Enabled: true}
	repo.connections[connection.ID] = connection
	id := uuid.NewString()
	repo.vms[id] = domain.VM{ID: id, ConnectionID: connection.ID, Provider: ProviderPVE, ExternalID: "51049", Name: "personal-test", Status: "running"}
	adapter := &sshIdentityAdapter{identity: domain.VMIdentity{ProviderVMID: "51049", Name: "personal-test", Generation: "incarnation-one", Status: "running",
		DataDisks: []domain.CapacityDiskAllocation{{ID: "scsi1", Serial: "testdata01", ProviderRef: "local:vm-51049-disk-1", SizeGiB: 8}}}}
	service := MustNew(testDependencies(repo), map[string]Adapter{ProviderPVE: adapter}, testPermissions(), nil, Options{})
	principal := testPrincipal()
	principal.UserID = uuid.NewString()
	proof, err := service.ObservePersonalGuestVM(t.Context(), principal, id, "testdata01")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	binding := guest.Binding{SchemaVersion: guest.BindingVersion, ID: uuid.NewString(), Version: 1, AssistantID: uuid.NewString(), OwnerID: principal.UserID,
		Provider: ProviderPVE, ComputeRef: id, ComputeGeneration: 1, ProviderIdentityHash: proof.ProviderIdentityHash, DataRef: uuid.NewString(), DataDiskSerial: "testdata01", DataDiskBytes: 8 << 30,
		PolicyRevision: "sha256:" + strings.Repeat("a", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	return service, adapter, principal, binding
}

func TestPersonalGuestVMRequiresCurrentIncarnationAndUniqueDataDisk(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*domain.VMIdentity)
	}{
		{"recreated same VM ID", func(v *domain.VMIdentity) { v.Generation = "incarnation-two" }},
		{"different provider VM", func(v *domain.VMIdentity) { v.ProviderVMID = "51050" }},
		{"stopped VM", func(v *domain.VMIdentity) { v.Status = "stopped" }},
		{"missing data disk", func(v *domain.VMIdentity) { v.DataDisks = nil }},
		{"ambiguous serial", func(v *domain.VMIdentity) { v.DataDisks = append(v.DataDisks, v.DataDisks[0]) }},
		{"changed disk capacity", func(v *domain.VMIdentity) { v.DataDisks[0].SizeGiB++ }},
		{"replaced data volume", func(v *domain.VMIdentity) { v.DataDisks[0].ProviderRef = "local:other-volume" }},
		{"unknown disk capacity", func(v *domain.VMIdentity) { v.DataDisks[0].SizeGiB = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, adapter, principal, binding := personalGuestVMFixture(t)
			if err := service.ValidatePersonalGuestVM(t.Context(), principal, binding); err != nil {
				t.Fatal(err)
			}
			test.edit(&adapter.identity)
			if err := service.ValidatePersonalGuestVM(t.Context(), principal, binding); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("changed provider/data identity accepted: %v", err)
			}
		})
	}
}

func TestPersonalGuestVMRejectsDifferentOwnerAndCurrentPermissionLoss(t *testing.T) {
	service, _, principal, binding := personalGuestVMFixture(t)
	other := principal
	other.UserID = uuid.NewString()
	if err := service.ValidatePersonalGuestVM(t.Context(), other, binding); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("different owner accepted", err)
	}
	principal.Roles = nil
	if err := service.ValidatePersonalGuestVM(t.Context(), principal, binding); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("current permission loss accepted", err)
	}
}

func TestPersonalGuestProviderDigestSeparatesSourcesAndIncarnations(t *testing.T) {
	one, err := personalGuestProviderIdentityHash("pve", "source-one", "51049", "generation-one", "local:volume-one")
	if err != nil {
		t.Fatal(err)
	}
	for _, parts := range [][5]string{{"pve", "source-two", "51049", "generation-one", "local:volume-one"}, {"pve", "source-one", "51049", "generation-two", "local:volume-one"}, {"pve", "source-one", "51050", "generation-one", "local:volume-one"}, {"pve", "source-one", "51049", "generation-one", "local:volume-two"}} {
		other, err := personalGuestProviderIdentityHash(parts[0], parts[1], parts[2], parts[3], parts[4])
		if err != nil || other == one {
			t.Fatal("different physical identity shares digest", err)
		}
	}
	if _, err := personalGuestProviderIdentityHash("pve", "", "51049", "generation-one", "local:volume-one"); err == nil {
		t.Fatal("unknown physical source accepted")
	}
}

type guestFailureDiagnostic struct{}

func (guestFailureDiagnostic) Error() string             { return "private provider credential" }
func (guestFailureDiagnostic) ProvisioningStage() string { return "runtime_scope" }

func TestPersonalGuestFailureReceiptDropsPrivateProviderOutput(t *testing.T) {
	for _, err := range []error{errors.New("private provider credential"), guestFailureDiagnostic{}} {
		raw, marshalErr := json.Marshal(personalGuestFailureResult(err))
		if marshalErr != nil || strings.Contains(string(raw), "private provider credential") {
			t.Fatal("provider output leaked into operation receipt")
		}
	}
	if personalGuestFailureResult(guestFailureDiagnostic{})["stage"] != "runtime_scope" {
		t.Fatal("safe stage missing")
	}
}
