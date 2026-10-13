package aiproduction

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPersonalVMTemplateRequiresExplicitOwnerDiskAndAssignmentScope(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*EnvironmentTemplate)
		denied bool
		want   error
	}{
		{"valid", func(*EnvironmentTemplate) {}, false, nil},
		{"scope denied", func(*EnvironmentTemplate) {}, true, apperrors.ErrAccessDenied},
		{"owner", func(p *EnvironmentTemplate) { p.PersonalVM.OwnerID = uuid.Nil }, false, apperrors.ErrInvalidArgument},
		{"wrong backend", func(p *EnvironmentTemplate) { p.Backend = "container" }, false, apperrors.ErrInvalidArgument},
		{"missing policy", func(p *EnvironmentTemplate) { p.PersonalVM = nil }, false, apperrors.ErrInvalidArgument},
		{"disk path", func(p *EnvironmentTemplate) { p.PersonalVM.DataDiskSerial = "/dev/sdb" }, false, apperrors.ErrInvalidArgument},
		{"fractional disk", func(p *EnvironmentTemplate) { p.PersonalVM.DataDiskBytes++ }, false, apperrors.ErrInvalidArgument},
		{"oversized quota", func(p *EnvironmentTemplate) { p.PersonalVM.Quota.DiskBytes = 9 << 30 }, false, apperrors.ErrInvalidArgument},
		{"direct network", func(p *EnvironmentTemplate) { p.PersonalVM.Quota.NetworkEgress = true }, false, apperrors.ErrInvalidArgument},
		{"mixed policies", func(p *EnvironmentTemplate) { p.PersonalDocker = &sohaapi.PersonalDockerEnvironmentPolicy{} }, false, apperrors.ErrInvalidArgument},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := EnvironmentTemplate{ID: "vm", Name: "Personal VM", Backend: "vm", IsolationMode: "disposable-write", PersonalVM: &sohaapi.PersonalVMEnvironmentPolicy{
				OwnerID: uuid.New(), ComputeRef: uuid.New(), Provider: sohaapi.PersonalVMEnvironmentPolicyProviderPve, DataDiskSerial: "testdata", DataDiskBytes: 8 << 30, LeaseSeconds: 600,
				Quota: sohaapi.PersonalEnvironmentQuota{DiskBytes: 1 << 30, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}}
			test.change(&item)
			store := newMemoryStore()
			service, err := New(store)
			if err != nil {
				t.Fatal(err)
			}
			checked := false
			service.SetPersonalVMValidator(func(_ context.Context, actor identity.Principal, policy domain.PersonalVMPolicy) error {
				checked = true
				if actor.UserID != "admin" || policy.OwnerID != item.PersonalVM.OwnerID.String() || policy.ComputeRef != item.PersonalVM.ComputeRef.String() {
					t.Fatal("owner or administrator scope lost")
				}
				if test.denied {
					return apperrors.ErrAccessDenied
				}
				return nil
			})
			_, err = service.PutEnvironmentTemplateForPrincipal(t.Context(), identity.Principal{UserID: "admin"}, item)
			if !errors.Is(err, test.want) || checked != (test.want == nil || test.denied) || (len(store.templates) == 1) != (test.want == nil) {
				t.Fatalf("error=%v checked=%v saved=%d", err, checked, len(store.templates))
			}
			if _, err = service.PutEnvironmentTemplate(t.Context(), item); !errors.Is(err, apperrors.ErrAccessDenied) && item.PersonalVM != nil {
				t.Fatal("unscoped writer accepted VM assignment")
			}
			if test.want == nil {
				policy, err := service.ResolvePersonalEnvironmentPolicy(t.Context(), "vm")
				if err != nil || policy.VM == nil || policy.VM.OwnerID != item.PersonalVM.OwnerID.String() || policy.AgentClusterID != "" || !domain.ValidPersonalVMPolicyRevision(policy.PolicyRevision) {
					t.Fatalf("policy lost trusted VM assignment: %+v %v", policy, err)
				}
			}
		})
	}
}
