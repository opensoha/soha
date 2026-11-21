package networkaccess

import (
	"context"
	"testing"
	"time"

	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAccessNetworkAdmissionSelectorsAndPrecedence(t *testing.T) {
	subject := domain.Subject{UserID: "user", Status: domain.StatusActive, Teams: []string{"it"}, Roles: []string{"employee"}}
	device := domain.Device{ID: "device", OwnerUserID: subject.UserID, SiteID: "site", Status: domain.DeviceStatusActive}
	site := domain.Site{ID: device.SiteID, Status: domain.StatusActive}
	snapshot := domain.PolicySnapshot{PolicyVersion: 1, Policies: []domain.Policy{
		{Enabled: true, Effect: domain.PolicyEffectAllow, AccessProfile: domain.ProfileFull},
	}}
	wifi := domain.AccessNetwork{ID: "corp", Configuration: domain.AccessNetworkInput{
		SiteID: site.ID, Kind: domain.AccessNetworkStandard, Enabled: true, SSID: "Corp Wi-Fi",
		NASBindingIDs: []string{"wlc"}, AccessMedium: domain.AccessMediumWiFi, BSSIDs: []string{"00:11:22:33:44:55"}, VLANID: 30,
		Rules: []domain.AccessNetworkRule{
			{Effect: domain.PolicyEffectAllow, Subjects: domain.PolicySubjects{Teams: []string{"it"}}, AuthenticationMethods: []string{"eap_tls"}, SessionTimeoutSeconds: 900},
			{Effect: domain.PolicyEffectAllow, Subjects: domain.PolicySubjects{Roles: []string{"employee"}}, SessionTimeoutSeconds: 300},
			{Effect: domain.PolicyEffectDeny, Subjects: domain.PolicySubjects{Roles: []string{"contractor"}}},
		},
	}}
	port := int64(0)
	wired := domain.AccessNetwork{ID: "port", Configuration: domain.AccessNetworkInput{
		SiteID: site.ID, Kind: domain.AccessNetworkStandard, Enabled: true, AccessMedium: domain.AccessMediumWired,
		NASBindingIDs: []string{"switch"}, PortStart: &port, PortEnd: &port,
	}}
	networks := []domain.AccessNetwork{wifi, wired}
	now := time.Unix(0, 0)
	wireless := domain.NASBinding{ID: "wlc", SiteID: site.ID, AccessMedium: domain.AccessMediumWiFi}
	for _, tc := range []struct {
		name       string
		binding    domain.NASBinding
		attachment AccessAttachment
		want       string
		managed    bool
	}{
		{"known SSID and BSSID", wireless, AccessAttachment{CalledStationID: "00-11-22-33-44-55:Corp Wi-Fi"}, "corp", true},
		{"unknown SSID", wireless, AccessAttachment{CalledStationID: "00-11-22-33-44-55:Guest"}, "", true},
		{"wrong BSSID", wireless, AccessAttachment{CalledStationID: "00-11-22-33-44-AA:Corp Wi-Fi"}, "", true},
		{"missing wireless evidence", wireless, AccessAttachment{NASPort: &port}, "", true},
		{"cross site", domain.NASBinding{ID: "wlc", SiteID: "another"}, AccessAttachment{CalledStationID: "00-11-22-33-44-55:Corp Wi-Fi"}, "", false},
		{"physical port zero", domain.NASBinding{ID: "switch", SiteID: site.ID, AccessMedium: domain.AccessMediumWired}, AccessAttachment{NASPort: &port}, "port", true},
		{"unknown NAS", domain.NASBinding{ID: "unknown", SiteID: site.ID}, AccessAttachment{NASPort: &port}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, managed := MatchAccessNetwork(networks, tc.binding, tc.attachment)
			if managed != tc.managed || (got == nil && tc.want != "") || (got != nil && got.ID != tc.want) {
				t.Fatalf("matched %#v, managed=%v", got, managed)
			}
		})
	}
	decision := EvaluateAccessNetwork(now, wifi, "eap-tls", subject, device, site, snapshot)
	if !decision.Allowed || decision.AccessProfile != domain.ProfileFull || decision.VLANID != 30 || decision.SessionTimeoutSeconds != 300 {
		t.Fatalf("unexpected authorized attributes %#v", decision)
	}
	if got := EvaluateAccessNetwork(now, wifi, "eap-ttls-pap", subject, device, site, snapshot); got.Allowed || got.ReasonCode != "site_admission_denied" {
		t.Fatalf("password method did not default deny: %#v", got)
	}
	subject.Roles = append(subject.Roles, "contractor")
	if got := EvaluateAccessNetwork(now, wifi, "eap-tls", subject, device, site, snapshot); got.Allowed || got.ReasonCode != "access_network_explicit_deny" {
		t.Fatalf("explicit deny did not override allow: %#v", got)
	}
	draft := wifi
	draft.Configuration.Enabled = false
	if got, managed := MatchAccessNetwork([]domain.AccessNetwork{draft}, wireless, AccessAttachment{}); got != nil || managed {
		t.Fatalf("a disabled draft opted the site into managed admission: %v, %v", got, managed)
	}
	wifi.Deleted = true
	wifi.Activated = true
	if got, managed := MatchAccessNetwork([]domain.AccessNetwork{wifi}, wireless, AccessAttachment{CalledStationID: "00-11-22-33-44-55:Corp Wi-Fi"}); got != nil || !managed {
		t.Fatalf("retirement restored legacy admission: %v, %v", got, managed)
	}
}

func TestAccessNetworkMutuallyExclusiveRuleMappings(t *testing.T) {
	input := domain.AccessNetworkInput{Rules: []domain.AccessNetworkRule{
		{Effect: domain.PolicyEffectAllow, Subjects: domain.PolicySubjects{Teams: []string{"engineering"}}, VLANID: 10},
		{Effect: domain.PolicyEffectAllow, Subjects: domain.PolicySubjects{Teams: []string{"support"}}, VLANID: 20},
	}}
	if err := validateAccessNetworkRuleConflicts(input); err == nil {
		t.Fatal("overlap via multi-team membership was incorrectly allowed")
	}
	input.Rules[0].Subjects.Users = []string{"one"}
	input.Rules[1].Subjects.Users = []string{"two"}
	if err := validateAccessNetworkRuleConflicts(input); err != nil {
		t.Fatal(err)
	}
}

type accessNetworkChoiceStore struct {
	connectionOptionsStore
}

func (s *accessNetworkChoiceStore) GetSite(context.Context, string) (domain.Site, error) {
	return s.sites[0], nil
}

func (s *accessNetworkChoiceStore) FindSiteProfileBinding(_ context.Context, siteID, profile string) (domain.SiteProfileBinding, error) {
	return domain.SiteProfileBinding{SiteID: siteID, AccessProfile: profile, VLANID: 12, SessionTimeoutSeconds: 60}, nil
}

func TestAccessNetworkTTLSDoesNotIgnoreUnverifiedDeviceDenies(t *testing.T) {
	subject := domain.Subject{UserID: "user-1", Status: domain.StatusActive}
	device := domain.Device{ID: "registered", OwnerUserID: subject.UserID, SiteID: "site", Status: domain.DeviceStatusActive, PostureStatus: domain.PostureCompliant}
	site := domain.Site{ID: device.SiteID, Status: domain.StatusActive}
	snapshot := domain.PolicySnapshot{PolicyVersion: 1, Policies: []domain.Policy{{
		Enabled: true, Effect: domain.PolicyEffectAllow, AccessProfile: domain.ProfileFull,
		DeviceManagementStates: []string{domain.DeviceManagementUnmanaged}, PostureStatuses: []string{domain.PostureUnknown},
	}}}
	network := domain.AccessNetwork{ID: "wifi", Configuration: domain.AccessNetworkInput{
		SiteID: site.ID, Kind: domain.AccessNetworkStandard, Enabled: true,
		Rules: []domain.AccessNetworkRule{{Effect: domain.PolicyEffectAllow}, {Effect: domain.PolicyEffectDeny, DeviceIDs: []string{device.ID}}},
	}}
	rule := &network.Configuration.Rules[1]
	rule.Subjects = domain.PolicySubjects{Users: []string{}, Teams: []string{}, Tags: []string{}}
	if err := validateAccessNetworkRule(*rule); err == nil {
		t.Fatal("device-specific rule permits unverifiable TTLS")
	}
	for _, id := range []string{"registered", "nas-station:example"} {
		device.ID = id
		got := EvaluateAccessNetwork(time.Unix(0, 0), network, "eap-ttls-pap", subject, device, site, snapshot)
		if got.Allowed || got.ReasonCode != "access_network_device_proof_required" {
			t.Fatalf("device %s: %+v", id, got)
		}
	}
	rule.AuthenticationMethods = []string{"eap_tls"}
	decision := EvaluateAccessNetwork(time.Unix(0, 0), network, "eap-ttls-pap", subject, device, site, snapshot)
	decision, err := CheckAccessNetworkProfile(context.Background(), decision, "eap_ttls_pap", site.ID, func(_ context.Context, _, profile string) (domain.SiteProfileBinding, error) {
		if profile != domain.ProfileRestricted {
			t.Fatalf("password profile = %s", profile)
		}
		return domain.SiteProfileBinding{}, apperrors.ErrNotFound
	})
	if err != nil || decision.Allowed || decision.ReasonCode != "site_profile_binding_unavailable" {
		t.Fatalf("missing RADIUS mapping was accepted: %+v %v", decision, err)
	}
}

type accessNetworkChoiceCatalog struct {
	AccessNetworkCatalog
	items []domain.AccessNetwork
}

func (c accessNetworkChoiceCatalog) ListAccessNetworks(context.Context, string) ([]domain.AccessNetwork, error) {
	return c.items, nil
}

func TestAccessNetworkChoicesKeepGuestSeparate(t *testing.T) {
	store := &accessNetworkChoiceStore{connectionOptionsStore{
		device:   domain.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site", Status: domain.DeviceStatusActive},
		subject:  domain.Subject{UserID: "user-1", Status: domain.StatusActive},
		snapshot: domain.PolicySnapshot{PolicyVersion: 1, Policies: []domain.Policy{{Enabled: true, Effect: domain.PolicyEffectAllow, AccessProfile: domain.ProfileFull}}},
		bindings: []domain.NASBinding{{ID: "wlc", SiteID: "site", Status: domain.StatusActive, AccessMedium: domain.AccessMediumWiFi, SSID: "legacy"}},
		sites:    []domain.Site{{ID: "site", Name: "Office", Status: domain.StatusActive}},
	}}
	managed := func(id string, priority int, allowed bool) domain.AccessNetwork {
		kind := domain.PolicyEffectDeny
		if allowed {
			kind = domain.PolicyEffectAllow
		}
		return domain.AccessNetwork{ID: id, Configuration: domain.AccessNetworkInput{
			SiteID: "site", Kind: domain.AccessNetworkStandard, Enabled: true, AccessMedium: domain.AccessMediumWiFi,
			NASBindingIDs: []string{"wlc"}, SSID: id, Name: id, Priority: priority, Rules: []domain.AccessNetworkRule{{Effect: kind}},
		}}
	}
	catalog := accessNetworkChoiceCatalog{items: []domain.AccessNetwork{
		managed("second", 20, true), managed("denied", 1, false), managed("first", 10, true),
		{ID: "guest", Configuration: domain.AccessNetworkInput{SiteID: "site", Kind: domain.AccessNetworkBootstrap, Enabled: true, SSID: "guest"}},
	}}
	service := &Service{store: store, accessNetworks: catalog}
	ctx := context.Background()
	principal := identity.Principal{UserID: "user-1"}
	items, guest, err := service.ListAccessNetworkChoices(ctx, principal, "device-1", true)
	if err != nil || len(items) != 2 || items[0].AccessNetworkID != "first" || items[1].AccessNetworkID != "second" || len(guest) != 1 || guest[0].AccessNetworkID != "guest" {
		t.Fatalf("got %#v, %#v, %v", items, guest, err)
	}
	_, guest, err = service.ListAccessNetworkChoices(ctx, principal, "device-1", false)
	if err != nil || len(guest) != 0 {
		t.Fatalf("legacy caller received guest info: %#v, %v", guest, err)
	}
}
