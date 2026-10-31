package networkaccess

import (
	"testing"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
)

func TestNewPolicySnapshotCanonicalizesSetFields(t *testing.T) {
	now := time.Date(2026, time.September, 2, 1, 2, 3, 0, time.UTC)
	policy := domainnetworkaccess.Policy{
		ID: "policy-1", Name: "canonical", Enabled: true, Priority: 100,
		Effect: domainnetworkaccess.PolicyEffectAllow,
		Subjects: domainnetworkaccess.PolicySubjects{
			Roles: []string{"role-b", "role-a"}, Users: []string{"user-b", "user-a"}, Teams: []string{"team-b", "team-a"}, Tags: []string{"tag-b", "tag-a"},
		},
		SiteIDs: []string{"site-b", "site-a"}, ResourceIDs: []string{"resource-b", "resource-a"},
		Modes:           []string{domainnetworkaccess.ModeExternalVPNZTNA, domainnetworkaccess.ModeInternalZTNA},
		DeviceStatuses:  []string{domainnetworkaccess.DeviceStatusQuarantined, domainnetworkaccess.DeviceStatusActive},
		PostureStatuses: []string{domainnetworkaccess.PostureUnknown, domainnetworkaccess.PostureCompliant},
		AccessProfile:   domainnetworkaccess.ProfileRestricted, Version: 3, CreatedAt: now, UpdatedAt: now,
	}
	reordered := policy
	reordered.Subjects = domainnetworkaccess.PolicySubjects{Roles: []string{"role-a", "role-b"}, Users: []string{"user-a", "user-b"}, Teams: []string{"team-a", "team-b"}, Tags: []string{"tag-a", "tag-b"}}
	reordered.SiteIDs = []string{"site-a", "site-b"}
	reordered.ResourceIDs = []string{"resource-a", "resource-b"}
	reordered.Modes = []string{domainnetworkaccess.ModeInternalZTNA, domainnetworkaccess.ModeExternalVPNZTNA}
	reordered.DeviceStatuses = []string{domainnetworkaccess.DeviceStatusActive, domainnetworkaccess.DeviceStatusQuarantined}
	reordered.PostureStatuses = []string{domainnetworkaccess.PostureCompliant, domainnetworkaccess.PostureUnknown}

	first, err := newPolicySnapshot([]domainnetworkaccess.Policy{policy}, []string{"resource-b", "resource-a"}, now)
	if err != nil {
		t.Fatalf("newPolicySnapshot(first) error = %v", err)
	}
	second, err := newPolicySnapshot([]domainnetworkaccess.Policy{reordered}, []string{"resource-a", "resource-b"}, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("newPolicySnapshot(second) error = %v", err)
	}
	if first.ContentHash != second.ContentHash {
		t.Fatalf("equivalent policy hashes = %q and %q, want equal", first.ContentHash, second.ContentHash)
	}
	if policy.Subjects.Roles[0] != "role-b" || policy.Subjects.Users[0] != "user-b" || policy.SiteIDs[0] != "site-b" {
		t.Fatalf("newPolicySnapshot mutated its input: %#v", policy)
	}
}

func TestPolicySnapshotRejectsAmbiguousAllowProfiles(t *testing.T) {
	base := domainnetworkaccess.Policy{ID: "allow-full", Name: "full", Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow, Subjects: domainnetworkaccess.PolicySubjects{Users: []string{"owner"}}, SiteIDs: []string{"site"}, AccessProfile: domainnetworkaccess.ProfileFull}
	for _, test := range []struct {
		name      string
		change    func(*domainnetworkaccess.Policy)
		wantError bool
	}{
		{"overlap", func(*domainnetworkaccess.Policy) {}, true},
		{"distinct_priority", func(p *domainnetworkaccess.Policy) { p.Priority++ }, false},
		{"disabled", func(p *domainnetworkaccess.Policy) { p.Enabled = false }, false},
		{"deny_override", func(p *domainnetworkaccess.Policy) {
			p.Effect = domainnetworkaccess.PolicyEffectDeny
			p.AccessProfile = domainnetworkaccess.ProfileDeny
		}, false},
		{"disjoint_users", func(p *domainnetworkaccess.Policy) { p.Subjects.Users = []string{"other"} }, false},
		{"disjoint_sites", func(p *domainnetworkaccess.Policy) { p.SiteIDs = []string{"other"} }, false},
		{"teams_may_overlap", func(p *domainnetworkaccess.Policy) { p.Subjects.Teams = []string{"other-team"} }, true},
		{"roles_may_overlap", func(p *domainnetworkaccess.Policy) { p.Subjects.Roles = []string{"another-role"} }, true},
		{"same_profile", func(p *domainnetworkaccess.Policy) { p.AccessProfile = domainnetworkaccess.ProfileFull }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := base
			other.ID, other.Name, other.AccessProfile = "allow-restricted", "restricted", domainnetworkaccess.ProfileRestricted
			test.change(&other)
			_, err := newPolicySnapshot([]domainnetworkaccess.Policy{base, other}, nil, time.Now().UTC())
			if (err != nil) != test.wantError {
				t.Fatalf("compile error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}
