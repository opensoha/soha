package networkaccess

import (
	"context"
	"errors"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type vpnConnectionScopeStore struct {
	Store
	VPNConnectionStore
	space      domain.Space
	restricted bool
	deny       bool
}

func (s *vpnConnectionScopeStore) GetSite(context.Context, string) (domain.Site, error) {
	return domain.Site{ID: "site-1", Status: domain.StatusActive}, nil
}

func (s *vpnConnectionScopeStore) GetSpace(context.Context, string) (domain.Space, error) {
	return s.space, nil
}

func (s *vpnConnectionScopeStore) GetPolicySnapshot(context.Context) (domain.PolicySnapshot, error) {
	effect := domain.PolicyEffectAllow
	if s.deny {
		effect = domain.PolicyEffectDeny
	}
	return domain.PolicySnapshot{PolicyVersion: 7, Policies: []domain.Policy{{
		ID: "vpn-scope", Enabled: true, Effect: effect, Subjects: domain.PolicySubjects{Users: []string{"user-1"}},
		SiteIDs: []string{"site-1"}, Modes: []string{domain.ModeExternalVPN}, AccessProfile: domain.ProfileFull,
	}}}, nil
}

func (s *vpnConnectionScopeStore) VPNRestrictionActive(context.Context, string, string, string) (bool, error) {
	return s.restricted, nil
}

func TestExternalVPNAuthorizesBothAddressFamiliesWithoutBypassingScope(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cidrs      []string
		restricted bool
		deny       bool
		unassigned bool
		wantAllow  bool
	}{
		{name: "IPv4", cidrs: []string{"10.53.0.0/16"}, wantAllow: true},
		{name: "IPv6", cidrs: []string{"fd00:53::/64"}, wantAllow: true},
		{name: "dual stack", cidrs: []string{"10.53.0.0/16", "fd00:53::/64"}, wantAllow: true},
		{name: "IPv6 default route", cidrs: []string{"::/0"}},
		{name: "IPv6 host bits", cidrs: []string{"fd00:53::1/64"}},
		{name: "IPv6 link local", cidrs: []string{"fe80::/64"}},
		{name: "IPv4 mapped", cidrs: []string{"::ffff:10.53.0.0/112"}},
		{name: "mixed invalid", cidrs: []string{"fd00:53::/64", "garbage"}},
		{name: "revoked IPv6", cidrs: []string{"fd00:53::/64"}, restricted: true},
		{name: "policy denied IPv6", cidrs: []string{"fd00:53::/64"}, deny: true},
		{name: "unassigned IPv6", cidrs: []string{"fd00:53::/64"}, unassigned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &vpnConnectionScopeStore{space: domain.Space{ID: "space-1", SiteID: "site-1", Status: domain.StatusActive, CIDRs: tc.cidrs}, restricted: tc.restricted, deny: tc.deny}
			service := &VPNService{base: &Service{store: store}, connections: store}
			subject := domain.Subject{UserID: "user-1", Status: domain.StatusActive}
			device := domain.Device{ID: "device-1", OwnerUserID: subject.UserID, Status: domain.DeviceStatusActive, PostureStatus: domain.PostureCompliant}
			config := domain.VPNProfileConfig{Enabled: true, SiteID: "site-1", NetworkSpaceID: "space-1", Mode: domain.ModeExternalVPN, Assignments: domain.VPNAssignment{UserIDs: []string{subject.UserID}}}
			if tc.unassigned {
				config.Assignments.UserIDs = []string{"user-2"}
			}
			snapshot, err := service.authorizeConnection(context.Background(), subject, device, config)
			if tc.wantAllow {
				if err != nil || snapshot.PolicyVersion != 7 {
					t.Fatalf("authorizeConnection = %#v, %v", snapshot, err)
				}
			} else if !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("authorizeConnection error = %v, want access denied", err)
			}
		})
	}
}
