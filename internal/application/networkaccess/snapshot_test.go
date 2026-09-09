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
			Users: []string{"user-b", "user-a"}, Teams: []string{"team-b", "team-a"}, Tags: []string{"tag-b", "tag-a"},
		},
		SiteIDs: []string{"site-b", "site-a"}, ResourceIDs: []string{"resource-b", "resource-a"},
		Modes:           []string{domainnetworkaccess.ModeExternalVPNZTNA, domainnetworkaccess.ModeInternalZTNA},
		DeviceStatuses:  []string{domainnetworkaccess.DeviceStatusQuarantined, domainnetworkaccess.DeviceStatusActive},
		PostureStatuses: []string{domainnetworkaccess.PostureUnknown, domainnetworkaccess.PostureCompliant},
		AccessProfile:   domainnetworkaccess.ProfileRestricted, Version: 3, CreatedAt: now, UpdatedAt: now,
	}
	reordered := policy
	reordered.Subjects = domainnetworkaccess.PolicySubjects{Users: []string{"user-a", "user-b"}, Teams: []string{"team-a", "team-b"}, Tags: []string{"tag-a", "tag-b"}}
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
	if policy.Subjects.Users[0] != "user-b" || policy.SiteIDs[0] != "site-b" {
		t.Fatalf("newPolicySnapshot mutated its input: %#v", policy)
	}
}
