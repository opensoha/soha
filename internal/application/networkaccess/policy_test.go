package networkaccess

import (
	"testing"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
)

func TestEvaluatePolicyPathsAndProtectedSet(t *testing.T) {
	subject := domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive, Teams: []string{"team-engineering"}, Tags: []string{"employee"}}
	device := domainnetworkaccess.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site-1", Status: domainnetworkaccess.DeviceStatusActive}
	site := domainnetworkaccess.Site{ID: "site-1", Status: domainnetworkaccess.StatusActive}
	space := domainnetworkaccess.Space{ID: "space-1", SiteID: site.ID, Status: domainnetworkaccess.StatusActive}
	snapshot := domainnetworkaccess.PolicySnapshot{PolicyVersion: 7, Policies: []domainnetworkaccess.Policy{{ID: "allow-all", Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow, AccessProfile: domainnetworkaccess.ProfileFull}}}

	tests := []struct {
		name         string
		mode         string
		pathMode     string
		protected    bool
		wantDecision string
		wantPath     string
		wantNetwork  bool
		wantResource bool
		wantReason   string
	}{
		{name: "internal direct", mode: domainnetworkaccess.ModeInternalDirect, pathMode: domainnetworkaccess.PathAutomatic, wantDecision: domainnetworkaccess.DecisionAllow, wantPath: domainnetworkaccess.PathSiteDirect},
		{name: "protected blocks internal direct", mode: domainnetworkaccess.ModeInternalDirect, pathMode: domainnetworkaccess.PathAutomatic, protected: true, wantDecision: domainnetworkaccess.DecisionDeny, wantPath: domainnetworkaccess.PathDeny, wantReason: "protected_resource_requires_ztna"},
		{name: "external vpn", mode: domainnetworkaccess.ModeExternalVPN, pathMode: domainnetworkaccess.PathAutomatic, wantDecision: domainnetworkaccess.DecisionAllow, wantPath: domainnetworkaccess.PathWireGuard, wantNetwork: true},
		{name: "protected blocks broad external vpn", mode: domainnetworkaccess.ModeExternalVPN, pathMode: domainnetworkaccess.PathAutomatic, protected: true, wantDecision: domainnetworkaccess.DecisionDeny, wantPath: domainnetworkaccess.PathDeny, wantReason: "protected_resource_requires_ztna"},
		{name: "internal ztna", mode: domainnetworkaccess.ModeInternalZTNA, pathMode: domainnetworkaccess.PathAutomatic, protected: true, wantDecision: domainnetworkaccess.DecisionAllow, wantPath: domainnetworkaccess.PathWireGuardZTNA, wantResource: true},
		{name: "external vpn ztna", mode: domainnetworkaccess.ModeExternalVPNZTNA, pathMode: domainnetworkaccess.PathAutomatic, protected: true, wantDecision: domainnetworkaccess.DecisionAllow, wantPath: domainnetworkaccess.PathWireGuardZTNA, wantNetwork: true, wantResource: true},
		{name: "external direct ztna", mode: domainnetworkaccess.ModeExternalDirectZTNA, pathMode: domainnetworkaccess.PathAutomatic, protected: true, wantDecision: domainnetworkaccess.DecisionAllow, wantPath: domainnetworkaccess.PathWireGuardZTNA, wantResource: true},
		{name: "explicit path cannot bypass selected mode", mode: domainnetworkaccess.ModeExternalVPNZTNA, pathMode: domainnetworkaccess.PathWireGuard, protected: true, wantDecision: domainnetworkaccess.DecisionDeny, wantPath: domainnetworkaccess.PathDeny, wantReason: "resource_path_mode_mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := domainnetworkaccess.Resource{ID: "resource-1", SpaceID: space.ID, PathMode: tt.pathMode, Protected: tt.protected}
			got := EvaluatePolicy(PreviewInput{SubjectUserID: "user-1", DeviceID: device.ID, ResourceID: resource.ID, SiteID: site.ID, Mode: tt.mode}, subject, device, site, space, resource, snapshot)
			if got.Decision != tt.wantDecision || got.Path != tt.wantPath || got.NetworkLeaseRequired != tt.wantNetwork || got.ResourceLeaseRequired != tt.wantResource {
				t.Fatalf("evaluatePolicy() = decision %q path %q networkLease %v resourceLease %v, want %q %q %v %v", got.Decision, got.Path, got.NetworkLeaseRequired, got.ResourceLeaseRequired, tt.wantDecision, tt.wantPath, tt.wantNetwork, tt.wantResource)
			}
			if tt.wantReason != "" && !contains(got.Reasons, tt.wantReason) {
				t.Fatalf("evaluatePolicy() reasons = %v, want %q", got.Reasons, tt.wantReason)
			}
			if got.PolicyVersion != 7 {
				t.Fatalf("evaluatePolicy() policy version = %d, want 7", got.PolicyVersion)
			}
		})
	}
}

func TestEvaluatePolicyUsesUserTeamTagEntitlementsAndDenyPrecedence(t *testing.T) {
	subject := domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive, Teams: []string{"team-engineering"}, Tags: []string{"employee"}}
	device := domainnetworkaccess.Device{ID: "device-1", OwnerUserID: subject.UserID, SiteID: "site-1", Status: domainnetworkaccess.DeviceStatusActive, PostureStatus: domainnetworkaccess.PostureCompliant}
	site := domainnetworkaccess.Site{ID: "site-1", Status: domainnetworkaccess.StatusActive}
	space := domainnetworkaccess.Space{ID: "space-1", SiteID: site.ID, Status: domainnetworkaccess.StatusActive}
	resource := domainnetworkaccess.Resource{ID: "resource-1", SpaceID: space.ID, PathMode: domainnetworkaccess.PathAutomatic, Protected: true}
	input := PreviewInput{SubjectUserID: subject.UserID, DeviceID: device.ID, ResourceID: resource.ID, SiteID: site.ID, Mode: domainnetworkaccess.ModeInternalZTNA}
	allow := domainnetworkaccess.Policy{ID: "team-tag-allow", Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow, Subjects: domainnetworkaccess.PolicySubjects{Teams: []string{"team-engineering"}, Tags: []string{"employee"}}, SiteIDs: []string{site.ID}, ResourceIDs: []string{resource.ID}, Modes: []string{input.Mode}, DeviceStatuses: []string{domainnetworkaccess.DeviceStatusActive}, PostureStatuses: []string{domainnetworkaccess.PostureCompliant}, AccessProfile: domainnetworkaccess.ProfileFull}

	got := EvaluatePolicy(input, subject, device, site, space, resource, domainnetworkaccess.PolicySnapshot{PolicyVersion: 9, Policies: []domainnetworkaccess.Policy{allow}})
	if got.Decision != domainnetworkaccess.DecisionAllow || got.NetworkProfile != domainnetworkaccess.ProfileFull || !contains(got.Reasons, "policy_allow:team-tag-allow") {
		t.Fatalf("team/tag policy result = %#v, want full allow", got)
	}

	deny := allow
	deny.ID, deny.Priority, deny.Effect, deny.AccessProfile = "explicit-deny", 900, domainnetworkaccess.PolicyEffectDeny, domainnetworkaccess.ProfileDeny
	got = EvaluatePolicy(input, subject, device, site, space, resource, domainnetworkaccess.PolicySnapshot{PolicyVersion: 10, Policies: []domainnetworkaccess.Policy{allow, deny}})
	if got.Decision != domainnetworkaccess.DecisionDeny || !contains(got.Reasons, "policy_deny:explicit-deny") {
		t.Fatalf("deny precedence result = %#v, want explicit deny", got)
	}

	allow.Subjects.Teams = []string{"team-finance"}
	got = EvaluatePolicy(input, subject, device, site, space, resource, domainnetworkaccess.PolicySnapshot{PolicyVersion: 11, Policies: []domainnetworkaccess.Policy{allow}})
	if got.Decision != domainnetworkaccess.DecisionDeny || !contains(got.Reasons, "no_matching_allow_policy") {
		t.Fatalf("non-matching policy result = %#v, want deny", got)
	}
}

func TestEvaluatePolicyReturnsConfiguredAccessProfile(t *testing.T) {
	subject := domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive}
	device := domainnetworkaccess.Device{ID: "device-1", OwnerUserID: subject.UserID, SiteID: "site-1", Status: domainnetworkaccess.DeviceStatusActive}
	site := domainnetworkaccess.Site{ID: "site-1", Status: domainnetworkaccess.StatusActive}
	space := domainnetworkaccess.Space{ID: "space-1", SiteID: site.ID, Status: domainnetworkaccess.StatusActive}
	resource := domainnetworkaccess.Resource{ID: "resource-1", SpaceID: space.ID, PathMode: domainnetworkaccess.PathAutomatic}
	input := PreviewInput{SubjectUserID: subject.UserID, DeviceID: device.ID, ResourceID: resource.ID, SiteID: site.ID, Mode: domainnetworkaccess.ModeInternalDirect}

	for _, profile := range []string{domainnetworkaccess.ProfileOnboarding, domainnetworkaccess.ProfileFull, domainnetworkaccess.ProfileRestricted, domainnetworkaccess.ProfileQuarantine} {
		t.Run(profile, func(t *testing.T) {
			snapshot := domainnetworkaccess.PolicySnapshot{PolicyVersion: 1, Policies: []domainnetworkaccess.Policy{{ID: "allow-" + profile, Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow, AccessProfile: profile}}}
			got := EvaluatePolicy(input, subject, device, site, space, resource, snapshot)
			if got.Decision != domainnetworkaccess.DecisionAllow || got.NetworkProfile != profile {
				t.Fatalf("evaluatePolicy() = %#v, want allow profile %q", got, profile)
			}
		})
	}
}

func TestEvaluateAdmissionUsesNetworkWidePoliciesAndFailsClosed(t *testing.T) {
	subject := domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive, Teams: []string{"team-engineering"}}
	device := domainnetworkaccess.Device{ID: "device-1", OwnerUserID: subject.UserID, SiteID: "site-1", Status: domainnetworkaccess.DeviceStatusActive, PostureStatus: domainnetworkaccess.PostureCompliant}
	site := domainnetworkaccess.Site{ID: "site-1", Status: domainnetworkaccess.StatusActive}
	resourceOnly := domainnetworkaccess.Policy{ID: "resource-only", Enabled: true, Priority: 1, Effect: domainnetworkaccess.PolicyEffectAllow, ResourceIDs: []string{"resource-1"}, AccessProfile: domainnetworkaccess.ProfileFull}
	networkWide := domainnetworkaccess.Policy{ID: "network-wide", Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow, Subjects: domainnetworkaccess.PolicySubjects{Teams: []string{"team-engineering"}}, SiteIDs: []string{site.ID}, Modes: []string{domainnetworkaccess.ModeInternalDirect}, DeviceStatuses: []string{domainnetworkaccess.DeviceStatusActive}, PostureStatuses: []string{domainnetworkaccess.PostureCompliant}, AccessProfile: domainnetworkaccess.ProfileRestricted}

	got := EvaluateAdmission(AdmissionInput{SubjectUserID: subject.UserID, DeviceID: device.ID, SiteID: site.ID, Mode: domainnetworkaccess.ModeInternalDirect}, subject, device, site, domainnetworkaccess.PolicySnapshot{PolicyVersion: 12, Policies: []domainnetworkaccess.Policy{resourceOnly, networkWide}})
	if got.Decision != domainnetworkaccess.DecisionAllow || got.NetworkProfile != domainnetworkaccess.ProfileRestricted || !contains(got.Reasons, "policy_allow:network-wide") {
		t.Fatalf("EvaluateAdmission() = %#v, want restricted network-wide allow", got)
	}

	device.Status = domainnetworkaccess.DeviceStatusQuarantined
	got = EvaluateAdmission(AdmissionInput{SubjectUserID: subject.UserID, DeviceID: device.ID, SiteID: site.ID, Mode: domainnetworkaccess.ModeInternalDirect}, subject, device, site, domainnetworkaccess.PolicySnapshot{PolicyVersion: 12, Policies: []domainnetworkaccess.Policy{networkWide}})
	if got.Decision != domainnetworkaccess.DecisionDeny || !contains(got.Reasons, "device_not_active") {
		t.Fatalf("EvaluateAdmission(quarantined) = %#v, want fail-closed deny", got)
	}
}

func TestEvaluateAdmissionReturnsVPNPathAndLeaseRequirement(t *testing.T) {
	subject := domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive, Teams: []string{"team-engineering"}}
	device := domainnetworkaccess.Device{ID: "device-1", OwnerUserID: subject.UserID, Status: domainnetworkaccess.DeviceStatusActive, PostureStatus: domainnetworkaccess.PostureCompliant}
	site := domainnetworkaccess.Site{ID: "site-1", Status: domainnetworkaccess.StatusActive}
	policy := domainnetworkaccess.Policy{
		ID: "vpn-allow", Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow,
		Subjects: domainnetworkaccess.PolicySubjects{Teams: []string{"team-engineering"}}, SiteIDs: []string{site.ID},
		Modes: []string{domainnetworkaccess.ModeExternalVPN}, DeviceStatuses: []string{domainnetworkaccess.DeviceStatusActive},
		PostureStatuses: []string{domainnetworkaccess.PostureCompliant}, AccessProfile: domainnetworkaccess.ProfileFull,
	}

	got := EvaluateAdmission(AdmissionInput{SubjectUserID: subject.UserID, DeviceID: device.ID, SiteID: site.ID, Mode: domainnetworkaccess.ModeExternalVPN}, subject, device, site, domainnetworkaccess.PolicySnapshot{PolicyVersion: 13, Policies: []domainnetworkaccess.Policy{policy}})
	if got.Decision != domainnetworkaccess.DecisionAllow || got.Path != domainnetworkaccess.PathWireGuard || !got.NetworkLeaseRequired || got.ResourceLeaseRequired {
		t.Fatalf("EvaluateAdmission(external VPN) = %#v, want WireGuard network lease", got)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
