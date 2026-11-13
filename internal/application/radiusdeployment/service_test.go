package radiusdeployment

import (
	"context"
	"errors"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/domain/identity"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

type memoryRepo struct {
	Repository
	s          domain.State
	m          Mutation
	commits    int
	binding    access.NASBinding
	bindingErr error
}

func (r *memoryRepo) Get(context.Context, string) (domain.State, error)              { return r.s, nil }
func (r *memoryRepo) Members(context.Context, string) ([]domain.MemberRecord, error) { return nil, nil }
func (r *memoryRepo) Commit(_ context.Context, m Mutation) (domain.State, error) {
	r.m = m
	r.commits++
	return m.State, nil
}
func (r *memoryRepo) Binding(context.Context, string) (access.NASBinding, error) {
	return r.binding, r.bindingErr
}
func (r *memoryRepo) SiteActive(context.Context, string) (bool, error)            { return true, nil }
func (r *memoryRepo) Reservation(context.Context, string) (string, string, error) { return "", "", nil }

type auth struct {
	deny  bool
	calls []string
}

func (a *auth) AuthorizeRADIUS(_ context.Context, _ identity.Principal, site, binding, action string) error {
	a.calls = append(a.calls, site+"/"+binding+"/"+action)
	if a.deny {
		return apperrors.ErrAccessDenied
	}
	return nil
}
func TestCreateBootstrapRequiresExplicitConsentAndScope(t *testing.T) {
	slot := domain.Slot{ID: "slot", SiteID: "site", NASBindingID: "binding", UID: "uid", Configurations: []domain.Configuration{{Material: api.NetworkRADIUSConfiguration{Reference: "approved"}}}}
	r := &memoryRepo{bindingErr: apperrors.ErrNotFound}
	a := &auth{}
	s := &Service{repo: r, authority: a, slots: map[string]domain.Slot{"slot": slot}}
	i := api.NetworkRADIUSPoolInput{SiteID: "site", NASBindingID: "binding", SlotID: "slot", ConfigurationRef: "approved", Name: "RADIUS", RequestID: "request"}
	if _, e := s.Create(context.Background(), identity.Principal{}, i); !errors.Is(e, apperrors.ErrInvalidArgument) {
		t.Fatalf("missing CAS/consent %v", e)
	}
	i.CreateApprovedBinding = true
	if _, e := s.Create(context.Background(), identity.Principal{}, i); e != nil || !r.m.CreateApprovedBinding {
		t.Fatalf("approved bootstrap %v", e)
	}
	now := time.Now()
	i.ExpectedBindingUpdatedAt = &now
	if _, e := s.Create(context.Background(), identity.Principal{}, i); e == nil {
		t.Fatal("ambiguous CAS/bootstrap accepted")
	}
	a.deny = true
	i.ExpectedBindingUpdatedAt = nil
	if _, e := s.Create(context.Background(), identity.Principal{}, i); !errors.Is(e, apperrors.ErrAccessDenied) {
		t.Fatal(e)
	}
	if r.commits != 1 {
		t.Fatal("denied wrote state")
	}
	a.deny = false
	o, e := s.Options(context.Background(), identity.Principal{})
	if e != nil || !o.Slots[0].Available || o.Slots[0].BindingExists || !o.Slots[0].CreateApprovedBindingRequired {
		t.Fatalf("bootstrap options: %+v %v", o, e)
	}
}
func TestManagedEnrollmentAndLegacyProofBoundary(t *testing.T) {
	s := &Service{repo: &memoryRepo{}}
	for _, id := range []string{"radius-pool:a", "radius-member:a"} {
		_, e := s.ManagedEnrollment(context.Background(), networkidentity.Identity{ID: id, Kind: "nas"}, networkprotocol.EnrollmentRequest{})
		if e == nil {
			t.Fatal("reserved member without approved binding accepted")
		}
	}
	if _, e := s.ManagedEnrollment(context.Background(), networkidentity.Identity{ID: "external", Kind: "nas"}, networkprotocol.EnrollmentRequest{Capabilities: []string{domain.Capability}}); e == nil {
		t.Fatal("ordinary enrollment claimed managed capability")
	}
	if _, _, _, e := s.ManagedNAS(context.Background(), networkidentity.Identity{ID: "external"}, "nas", &networkprotocol.ManagedNASProof{}, true); e == nil {
		t.Fatal("external proof accepted")
	}
}
func TestControlScaleZeroAndStoppedConfiguration(t *testing.T) {
	zero := 0
	slot := domain.Slot{ID: "slot", UID: "uid", ExecutorID: "edge", MaxReplicas: 2}
	state := domain.State{Slot: slot, SlotHash: slot.Hash(), Pool: api.NetworkRADIUSPool{SlotID: "slot", ObjectUID: "uid", State: "unknown", DesiredReplicas: 1, Revision: 1, OwnerEpoch: 1}}
	r := &memoryRepo{s: state}
	s := &Service{repo: r, authority: &auth{}, slots: map[string]domain.Slot{"slot": slot}, executors: map[string]bool{"edge": true}}
	if _, e := s.Control(context.Background(), identity.Principal{}, "pool", api.NetworkRADIUSControlInput{Action: "stop", RequestID: "r", ExpectedRevision: 1}); e == nil {
		t.Fatal("unknown stop attempted instead of readback")
	}
	if _, e := controlTarget(state, api.NetworkRADIUSControlInput{Action: "scale", Replicas: &zero}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Configure(context.Background(), identity.Principal{}, "pool", api.NetworkRADIUSConfigurationInput{ConfigurationRef: "approved"}); e == nil {
		t.Fatal("unknown/unobserved configured")
	}
}
