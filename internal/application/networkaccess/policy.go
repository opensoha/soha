package networkaccess

import domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"

type PreviewInput struct {
	SubjectUserID string `json:"subjectUserId"`
	DeviceID      string `json:"deviceId"`
	ResourceID    string `json:"resourceId"`
	SiteID        string `json:"siteId,omitempty"`
	Mode          string `json:"mode"`
}

type AdmissionInput struct {
	SubjectUserID string
	DeviceID      string
	SiteID        string
	Mode          string
}

func EvaluatePolicy(input PreviewInput, subject domainnetworkaccess.Subject, device domainnetworkaccess.Device, site domainnetworkaccess.Site, space domainnetworkaccess.Space, resource domainnetworkaccess.Resource, snapshot domainnetworkaccess.PolicySnapshot) domainnetworkaccess.PolicyPreview {
	result := domainnetworkaccess.PolicyPreview{
		Decision:      domainnetworkaccess.DecisionDeny,
		Path:          domainnetworkaccess.PathDeny,
		PolicyVersion: snapshot.PolicyVersion,
		Protected:     resource.Protected,
		Reasons:       []string{},
	}
	deny := func(reason string) domainnetworkaccess.PolicyPreview {
		result.Reasons = append(result.Reasons, reason)
		return result
	}

	if reason := admissionDenyReason(AdmissionInput{SubjectUserID: input.SubjectUserID, DeviceID: input.DeviceID, SiteID: input.SiteID, Mode: input.Mode}, subject, device, site, snapshot); reason != "" {
		return deny(reason)
	}
	if space.Status != domainnetworkaccess.StatusActive {
		return deny("network_scope_not_active")
	}
	if space.SiteID != site.ID || resource.SpaceID != space.ID {
		return deny("network_scope_mismatch")
	}
	path, networkLease, resourceLease, ok := accessModePath(input.Mode)
	if !ok {
		return deny("unsupported_access_mode")
	}

	if resource.Protected && (path == domainnetworkaccess.PathSiteDirect || path == domainnetworkaccess.PathWireGuard) {
		return deny("protected_resource_requires_ztna")
	}
	if resource.PathMode != "" && resource.PathMode != domainnetworkaccess.PathAutomatic && resource.PathMode != path {
		return deny("resource_path_mode_mismatch")
	}

	allow, denyReason := selectResourcePolicy(snapshot.Policies, input, subject, device, site, resource)
	if denyReason != "" {
		return deny(denyReason)
	}
	if allow == nil {
		return deny("no_matching_allow_policy")
	}
	if allow.AccessProfile == domainnetworkaccess.ProfileDeny {
		return deny("invalid_allow_policy_profile")
	}

	result.Decision = domainnetworkaccess.DecisionAllow
	result.Path = path
	result.NetworkProfile = allow.AccessProfile
	result.NetworkLeaseRequired = networkLease
	result.ResourceLeaseRequired = resourceLease
	result.Reasons = append(result.Reasons, "policy_allow:"+allow.ID)
	return result
}

func selectResourcePolicy(policies []domainnetworkaccess.Policy, input PreviewInput, subject domainnetworkaccess.Subject, device domainnetworkaccess.Device, site domainnetworkaccess.Site, resource domainnetworkaccess.Resource) (*domainnetworkaccess.Policy, string) {
	var allow *domainnetworkaccess.Policy
	for index := range policies {
		policy := &policies[index]
		if !policy.Enabled || !matchesNetworkPolicy(*policy, input, subject, device, site, resource) {
			continue
		}
		if policy.Effect == domainnetworkaccess.PolicyEffectDeny {
			return nil, "policy_deny:" + policy.ID
		}
		if policy.Effect == domainnetworkaccess.PolicyEffectAllow && (allow == nil || policy.Priority < allow.Priority) {
			allow = policy
		}
	}
	return allow, ""
}

func EvaluateAdmission(input AdmissionInput, subject domainnetworkaccess.Subject, device domainnetworkaccess.Device, site domainnetworkaccess.Site, snapshot domainnetworkaccess.PolicySnapshot) domainnetworkaccess.PolicyPreview {
	result := domainnetworkaccess.PolicyPreview{Decision: domainnetworkaccess.DecisionDeny, Path: domainnetworkaccess.PathDeny, PolicyVersion: snapshot.PolicyVersion, Reasons: []string{}}
	deny := func(reason string) domainnetworkaccess.PolicyPreview {
		result.Reasons = append(result.Reasons, reason)
		return result
	}
	if reason := admissionDenyReason(input, subject, device, site, snapshot); reason != "" {
		return deny(reason)
	}

	var allow *domainnetworkaccess.Policy
	for index := range snapshot.Policies {
		policy := &snapshot.Policies[index]
		if !policy.Enabled || len(policy.ResourceIDs) != 0 || !matchesPolicySelectors(*policy, input.SubjectUserID, input.Mode, subject, device, site) {
			continue
		}
		if policy.Effect == domainnetworkaccess.PolicyEffectDeny {
			return deny("policy_deny:" + policy.ID)
		}
		if policy.Effect == domainnetworkaccess.PolicyEffectAllow && (allow == nil || policy.Priority < allow.Priority) {
			allow = policy
		}
	}
	if allow == nil {
		return deny("no_matching_allow_policy")
	}
	if allow.AccessProfile == domainnetworkaccess.ProfileDeny {
		return deny("invalid_allow_policy_profile")
	}
	path, networkLease, resourceLease, ok := accessModePath(input.Mode)
	if !ok {
		return deny("unsupported_access_mode")
	}
	result.Decision, result.Path, result.NetworkProfile = domainnetworkaccess.DecisionAllow, path, allow.AccessProfile
	result.NetworkLeaseRequired, result.ResourceLeaseRequired = networkLease, resourceLease
	result.Reasons = append(result.Reasons, "policy_allow:"+allow.ID)
	return result
}

func accessModePath(mode string) (path string, networkLease, resourceLease, ok bool) {
	switch mode {
	case domainnetworkaccess.ModeInternalDirect:
		return domainnetworkaccess.PathSiteDirect, false, false, true
	case domainnetworkaccess.ModeInternalZTNA:
		return domainnetworkaccess.PathWireGuardZTNA, false, true, true
	case domainnetworkaccess.ModeExternalVPN:
		return domainnetworkaccess.PathWireGuard, true, false, true
	case domainnetworkaccess.ModeExternalVPNZTNA:
		return domainnetworkaccess.PathWireGuardZTNA, true, true, true
	case domainnetworkaccess.ModeExternalDirectZTNA:
		return domainnetworkaccess.PathWireGuardZTNA, false, true, true
	default:
		return domainnetworkaccess.PathDeny, false, false, false
	}
}

func admissionDenyReason(input AdmissionInput, subject domainnetworkaccess.Subject, device domainnetworkaccess.Device, site domainnetworkaccess.Site, snapshot domainnetworkaccess.PolicySnapshot) string {
	if snapshot.PolicyVersion < 1 {
		return "policy_snapshot_unavailable"
	}
	if subject.UserID != input.SubjectUserID || subject.Status != domainnetworkaccess.StatusActive {
		return "subject_not_active"
	}
	if device.ID != input.DeviceID || device.OwnerUserID != input.SubjectUserID {
		return "device_owner_mismatch"
	}
	if device.Status != domainnetworkaccess.DeviceStatusActive {
		return "device_not_active"
	}
	if site.Status != domainnetworkaccess.StatusActive {
		return "network_scope_not_active"
	}
	if input.SiteID != "" && input.SiteID != site.ID {
		return "requested_site_mismatch"
	}
	if (input.Mode == domainnetworkaccess.ModeInternalDirect || input.Mode == domainnetworkaccess.ModeInternalZTNA) && device.SiteID != site.ID {
		return "device_site_mismatch"
	}
	return ""
}

func matchesNetworkPolicy(policy domainnetworkaccess.Policy, input PreviewInput, subject domainnetworkaccess.Subject, device domainnetworkaccess.Device, site domainnetworkaccess.Site, resource domainnetworkaccess.Resource) bool {
	return matchesPolicySelectors(policy, input.SubjectUserID, input.Mode, subject, device, site) &&
		matchNetworkValue(policy.ResourceIDs, resource.ID)
}

func matchesPolicySelectors(policy domainnetworkaccess.Policy, subjectUserID, mode string, subject domainnetworkaccess.Subject, device domainnetworkaccess.Device, site domainnetworkaccess.Site) bool {
	return subject.UserID == subjectUserID &&
		matchNetworkValue(policy.Subjects.Users, subject.UserID) &&
		matchNetworkSlice(policy.Subjects.Teams, subject.Teams) &&
		matchNetworkSlice(policy.Subjects.Tags, subject.Tags) &&
		matchNetworkValue(policy.SiteIDs, site.ID) &&
		matchNetworkValue(policy.Modes, mode) &&
		matchNetworkValue(policy.DeviceStatuses, device.Status) &&
		matchNetworkValue(policy.PostureStatuses, device.PostureStatus)
}

func matchNetworkValue(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, value := range allowed {
		if value == actual {
			return true
		}
	}
	return false
}

func matchNetworkSlice(allowed, actual []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, value := range actual {
		if matchNetworkValue(allowed, value) {
			return true
		}
	}
	return false
}
