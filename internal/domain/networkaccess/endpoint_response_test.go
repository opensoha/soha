package networkaccess

import "testing"

func TestAdmissionAttributesHashTracksAuthorizationInputs(t *testing.T) {
	subject := Subject{UserID: "owner", Status: StatusActive, Roles: []string{"r2", "r1"}, Teams: []string{"b", "a"}, Tags: []string{"staff"}}
	device := Device{ID: "device", OwnerUserID: "owner", SiteID: "site", Status: DeviceStatusActive, PostureStatus: PostureCompliant, CredentialGeneration: 1}
	site := Site{ID: "site", Status: StatusActive}
	binding := NASBinding{ID: "binding", RuntimeID: "nas-runtime", NASID: "nas", SiteID: "site", Status: StatusActive}
	profile := &SiteProfileBinding{ID: "profile", SiteID: "site", AccessProfile: ProfileFull, VLANID: 10, SessionTimeoutSeconds: 300}
	original := AdmissionAttributesHash(subject, device, site, binding, profile)
	reordered := subject
	reordered.Teams = []string{"a", "b"}
	reordered.Roles = []string{"r1", "r2"}
	if AdmissionAttributesHash(reordered, device, site, binding, profile) != original {
		t.Fatal("set order changed the authorization hash")
	}
	if subject.Teams[0] != "b" || subject.Roles[0] != "r2" {
		t.Fatal("hashing mutated subject teams")
	}
	revoked := subject
	revoked.Roles = []string{"r1"}
	if AdmissionAttributesHash(revoked, device, site, binding, profile) == original {
		t.Fatal("role binding removal did not invalidate authorization")
	}
	device.PostureVersion++
	if AdmissionAttributesHash(subject, device, site, binding, profile) == original {
		t.Fatal("posture version change did not invalidate authorization")
	}
	device.PostureVersion--
	profile.VLANID = 20
	if AdmissionAttributesHash(subject, device, site, binding, profile) == original {
		t.Fatal("VLAN change did not invalidate authorization")
	}
}
