package networkaccess

import "slices"

// Password admission requires an explicit unknown-posture/unmanaged selector;
// a legacy wildcard policy cannot silently enroll password-only stations.
func PasswordAdmissionPolicies(snapshot PolicySnapshot) PolicySnapshot {
	filtered := snapshot
	filtered.Policies = nil
	for _, policy := range snapshot.Policies {
		if policy.Effect == PolicyEffectDeny || slices.Contains(policy.DeviceManagementStates, DeviceManagementUnmanaged) && slices.Contains(policy.PostureStatuses, PostureUnknown) {
			filtered.Policies = append(filtered.Policies, policy)
		}
	}
	return filtered
}
