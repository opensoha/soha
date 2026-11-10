package personalenvironment

import (
	"context"
	"errors"
	"testing"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type guestOwnerFake struct {
	enabled, approved bool
	userID            string
	identityReads     int
	disableAfterRead  int
	vmChecks          int
	mutationChecks    int
	mutationErr       error
	leaseReads        int
	leaseErr          error
}

func (f *guestOwnerFake) CurrentPrincipal(context.Context, string) (identity.Principal, error) {
	f.identityReads++
	if !f.enabled || f.disableAfterRead > 0 && f.identityReads > f.disableAfterRead {
		return identity.Principal{}, apperrors.ErrUnauthorized
	}
	return identity.Principal{UserID: f.userID}, nil
}
func (f *guestOwnerFake) GetPersonalAgentAccess(context.Context, identity.Principal) (domain.PersonalAgentAccess, error) {
	status := "revoked"
	if f.approved {
		status = "approved"
	}
	return domain.PersonalAgentAccess{UserID: f.userID, Status: status, Version: 2}, nil
}
func (f *guestOwnerFake) ValidatePersonalGuestVM(context.Context, identity.Principal, guest.Binding) error {
	f.vmChecks++
	return nil
}

func (f *guestOwnerFake) AuthorizePersonalGuestMutation(context.Context, identity.Principal, guest.Binding) error {
	f.mutationChecks++
	return f.mutationErr
}

func (f *guestOwnerFake) PersonalGuestDataLease(_ context.Context, binding guest.Binding) (guest.DataPlan, error) {
	f.leaseReads++
	return guest.DataPlan{Binding: binding}, f.leaseErr
}

func guestAccessForTest(t *testing.T, fake *guestOwnerFake) *GuestAccess {
	t.Helper()
	access, err := NewGuestAccess(GuestAccessDependencies{Identity: fake, Approvals: fake, VM: fake, Leases: fake})
	if err != nil {
		t.Fatal(err)
	}
	return access
}

func TestPersonalGuestOwnerReloadsCurrentAccountAndApproval(t *testing.T) {
	fake := &guestOwnerFake{enabled: true, approved: true, userID: "owner"}
	authorizer := guestAccessForTest(t, fake)
	binding := guest.Binding{OwnerID: fake.userID}
	if err := authorizer.Authorize(t.Context(), binding); err != nil || fake.vmChecks != 1 {
		t.Fatal("current approved owner rejected", err)
	}
	fake.enabled = false
	if err := authorizer.Authorize(t.Context(), binding); !errors.Is(err, apperrors.ErrUnauthorized) || fake.vmChecks != 1 {
		t.Fatal("disabled owner reached provider", err)
	}
	fake.enabled, fake.approved = true, false
	if err := authorizer.Authorize(t.Context(), binding); !errors.Is(err, apperrors.ErrAccessDenied) || fake.vmChecks != 1 {
		t.Fatal("revoked approval reached provider", err)
	}
	fake.approved, fake.userID = true, "other"
	if err := authorizer.Authorize(t.Context(), binding); !errors.Is(err, apperrors.ErrAccessDenied) || fake.vmChecks != 1 {
		t.Fatal("different principal reached provider", err)
	}
}

func TestPersonalGuestDataLeaseUsesCurrentAuthority(t *testing.T) {
	binding := guest.Binding{OwnerID: "owner"}
	tests := []struct {
		name              string
		disableAfterRead  int
		mutationErr       error
		leaseErr          error
		wantErr           error
		wantMutationCalls int
		wantLeaseReads    int
	}{
		{name: "authorized", wantMutationCalls: 1, wantLeaseReads: 1},
		{name: "identity disabled between checks", disableAfterRead: 1, wantErr: apperrors.ErrUnauthorized},
		{name: "mutation refused", mutationErr: apperrors.ErrAccessDenied, wantErr: apperrors.ErrAccessDenied, wantMutationCalls: 1},
		{name: "obsolete binding refused by lease storage", leaseErr: apperrors.ErrAccessDenied, wantErr: apperrors.ErrAccessDenied, wantMutationCalls: 1, wantLeaseReads: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &guestOwnerFake{
				enabled: true, approved: true, userID: binding.OwnerID,
				disableAfterRead: tt.disableAfterRead, mutationErr: tt.mutationErr, leaseErr: tt.leaseErr,
			}
			plan, err := guestAccessForTest(t, fake).DataLease(t.Context(), binding)
			if !errors.Is(err, tt.wantErr) || (err == nil && plan.Binding != binding) {
				t.Fatalf("lease = %+v, %v, want error %v", plan, err, tt.wantErr)
			}
			if fake.identityReads != 2 || fake.vmChecks != 1 || fake.mutationChecks != tt.wantMutationCalls || fake.leaseReads != tt.wantLeaseReads {
				t.Fatalf("identity reads = %d, VM checks = %d, mutation checks = %d, lease reads = %d", fake.identityReads, fake.vmChecks, fake.mutationChecks, fake.leaseReads)
			}
		})
	}
}

func TestPersonalGuestAccessRequiresMutationPort(t *testing.T) {
	fake := &guestOwnerFake{}
	var missing *guestOwnerFake
	if _, err := NewGuestAccess(GuestAccessDependencies{Identity: fake, Approvals: fake, VM: missing, Leases: fake}); err == nil {
		t.Fatal("typed-nil VM access accepted")
	}
}
