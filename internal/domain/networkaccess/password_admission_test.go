package networkaccess

import "testing"

func TestPasswordAdmissionRequiresExplicitUnmanagedUnknown(t *testing.T) {
	snapshot := PolicySnapshot{PolicyVersion: 1, Policies: []Policy{{ID: "wildcard", Effect: PolicyEffectAllow}, {ID: "managed", Effect: PolicyEffectAllow, DeviceManagementStates: []string{DeviceManagementManaged}, PostureStatuses: []string{PostureUnknown}}, {ID: "password", Effect: PolicyEffectAllow, DeviceManagementStates: []string{DeviceManagementUnmanaged}, PostureStatuses: []string{PostureUnknown}}, {ID: "deny", Effect: PolicyEffectDeny}}}
	filtered := PasswordAdmissionPolicies(snapshot)
	if len(filtered.Policies) != 2 || filtered.Policies[0].ID != "password" || filtered.Policies[1].ID != "deny" || len(snapshot.Policies) != 4 {
		t.Fatalf("unexpected policies: %+v", filtered)
	}
}
