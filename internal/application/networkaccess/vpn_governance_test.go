package networkaccess

import (
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

func TestVPNGovernancePolicy(t *testing.T) {
	policy := VPNGovernancePolicy{}
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	proofExpiry := now.Add(time.Minute)
	allow := domain.Policy{Enabled: true, Effect: domain.PolicyEffectAllow, AccessProfile: domain.ProfileFull}
	base := VPNGovernanceFacts{
		Scope:       VPNGovernanceScope{SubjectID: "user", DeviceID: "device", SiteID: "site", Mode: domain.ModeExternalVPN, Profile: domain.ProfileFull},
		Subject:     domain.Subject{UserID: "user", Status: domain.StatusActive},
		Device:      domain.Device{ID: "device", OwnerUserID: "user", Status: domain.DeviceStatusActive, ManagementVerifiedUntil: &proofExpiry, ManagementIssuerKey: "issuer"},
		Site:        domain.Site{ID: "site", Status: domain.StatusActive, DeviceGovernance: domain.DeviceGovernanceVerified, ManagementIssuerKey: "issuer"},
		Snapshot:    domain.PolicySnapshot{PolicyVersion: 1, Policies: []domain.Policy{allow}},
		EvaluatedAt: now,
	}
	tests := []struct {
		name   string
		change func(*VPNGovernanceFacts)
		want   bool
	}{
		{name: "managed admission", want: true},
		{name: "explicit deny", change: func(f *VPNGovernanceFacts) {
			deny := allow
			deny.Effect = domain.PolicyEffectDeny
			f.Snapshot.Policies = []domain.Policy{allow, deny}
		}},
		{name: "missing subject", change: func(f *VPNGovernanceFacts) { f.Subject = domain.Subject{} }},
		{name: "no matching allow", change: func(f *VPNGovernanceFacts) { f.Snapshot.Policies = nil }},
		{name: "scope mismatch", change: func(f *VPNGovernanceFacts) { f.Scope.SiteID = "other" }},
		{name: "profile mismatch", change: func(f *VPNGovernanceFacts) { f.Scope.Profile = domain.ProfileRestricted }},
		{name: "proof expired at evaluation time", change: func(f *VPNGovernanceFacts) { f.EvaluatedAt = proofExpiry }},
		{name: "legacy without management selectors", want: true, change: func(f *VPNGovernanceFacts) {
			f.Site.DeviceGovernance = domain.DeviceGovernanceLegacy
			f.Subject = domain.Subject{}
			f.Snapshot.Policies = nil
		}},
		{name: "legacy with matching management selector", change: func(f *VPNGovernanceFacts) {
			f.Site.DeviceGovernance = domain.DeviceGovernanceLegacy
			f.Subject = domain.Subject{}
			managed := allow
			managed.DeviceManagementStates = []string{domain.DeviceManagementManaged}
			f.Snapshot.Policies = []domain.Policy{managed}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts := base
			if tt.change != nil {
				tt.change(&facts)
			}
			path := policy.Path(facts.Scope, facts.Site, facts.Snapshot)
			if allowed := policy.Allows(path, facts); allowed != tt.want {
				t.Fatalf("path=%v allowed=%t, want %t", path, allowed, tt.want)
			}
		})
	}
}

func TestVPNGovernanceResourceAndWorkload(t *testing.T) {
	policy := VPNGovernancePolicy{}
	facts := VPNGovernanceFacts{
		Scope:    VPNGovernanceScope{SubjectID: "user", DeviceID: "device", SiteID: "site", SpaceID: "space", Mode: domain.ModeExternalDirectZTNA, Profile: domain.ProfileFull, ResourceIDs: []string{"resource"}},
		Subject:  domain.Subject{UserID: "user", Status: domain.StatusActive},
		Device:   domain.Device{ID: "device", OwnerUserID: "user", Status: domain.DeviceStatusActive, ManagementState: domain.DeviceManagementManaged},
		Site:     domain.Site{ID: "site", Status: domain.StatusActive, DeviceGovernance: domain.DeviceGovernanceManaged},
		Space:    domain.Space{ID: "space", SiteID: "site", Status: domain.StatusActive},
		Resource: domain.Resource{ID: "resource", SpaceID: "space"},
		Snapshot: domain.PolicySnapshot{PolicyVersion: 1, Policies: []domain.Policy{{Enabled: true, Effect: domain.PolicyEffectAllow, AccessProfile: domain.ProfileFull, ResourceIDs: []string{"resource"}}}},
	}
	t.Run("user resource", func(t *testing.T) {
		path := policy.Path(facts.Scope, facts.Site, facts.Snapshot)
		if path != VPNGovernanceResource || !policy.Allows(path, facts) {
			t.Fatal("matching user resource was denied")
		}
		missing := facts
		missing.Resource = domain.Resource{}
		if policy.Allows(path, missing) {
			t.Fatal("missing resource was allowed")
		}
		wrongSpace := facts
		wrongSpace.Resource.SpaceID = "other"
		if policy.Allows(path, wrongSpace) {
			t.Fatal("resource outside the network space was allowed")
		}
	})
	t.Run("workload requires identity, binding and explicit policy", func(t *testing.T) {
		workload := facts
		workload.Scope.SubjectID = "service_account:account"
		workload.Subject.UserID = workload.Scope.SubjectID
		workload.Site.DeviceGovernance = domain.DeviceGovernanceLegacy
		path := policy.Path(workload.Scope, workload.Site, workload.Snapshot)
		if path != VPNGovernanceWorkload || policy.Allows(path, workload) {
			t.Fatal("workload fell through to user or legacy allow")
		}
		binding := domain.WorkloadBinding{DeviceID: "device", SiteID: "site", NetworkSpaceID: "space", ResourceIDs: []string{"resource"}}
		if !policy.WorkloadScopeMatches(workload.Scope, binding, workload.Subject.UserID) || !policy.WorkloadResourceMatches(binding, workload.Resource.ID) {
			t.Fatal("matching workload binding was denied")
		}
		if policy.WorkloadScopeMatches(workload.Scope, binding, "service_account:other") || policy.WorkloadResourceMatches(binding, "other-resource") {
			t.Fatal("foreign workload subject or resource was accepted")
		}
		workload.Scope.SpaceID = "other-space"
		if policy.WorkloadScopeMatches(workload.Scope, binding, workload.Subject.UserID) {
			t.Fatal("foreign workload space was accepted")
		}
		workload.Scope.SpaceID = binding.NetworkSpaceID
		allow := workload.Snapshot.Policies[0]
		allow.Subjects.Users = []string{workload.Subject.UserID}
		workload.Snapshot.Policies = []domain.Policy{allow}
		if !policy.Allows(path, workload) {
			t.Fatal("explicit workload resource policy was denied")
		}
	})
}
