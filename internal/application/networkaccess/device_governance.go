package networkaccess

import (
	"slices"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func networkUserSubject(id string) error {
	if strings.HasPrefix(strings.TrimSpace(id), "service_account:") {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "network_user_subject_required", "This network path requires a user-owned device; service accounts require a separate workload identity.", "该网络入口要求用户所属设备；服务账户需要独立的工作负载身份。")
	}
	return nil
}

func governedUnmanaged(site domain.Site, device domain.Device) bool {
	if site.DeviceGovernance == domain.DeviceGovernanceVerified && (device.ManagementVerifiedUntil == nil || !device.ManagementVerifiedUntil.After(time.Now()) || device.ManagementIssuerKey != site.ManagementIssuerKey) {
		return true
	}
	return site.EffectiveDeviceGovernance() != domain.DeviceGovernanceLegacy && device.EffectiveManagementState() != domain.DeviceManagementManaged
}

func resourceGovernanceReason(site domain.Site, device domain.Device, resource domain.Resource, networkLease, resourceLease bool) string {
	if !governedUnmanaged(site, device) {
		return ""
	}
	if site.EffectiveDeviceGovernance() == domain.DeviceGovernanceManaged || resource.Protected {
		return "managed_device_required"
	}
	if networkLease || !resourceLease {
		return "unmanaged_device_requires_resource_ztna"
	}
	return ""
}

func governancePolicyAllows(site domain.Site, device domain.Device, policy domain.Policy, resourceID string) bool {
	if !governedUnmanaged(site, device) {
		return true
	}
	if site.EffectiveDeviceGovernance() != domain.DeviceGovernanceRestricted || !slices.Contains(policy.DeviceManagementStates, device.EffectiveManagementState()) {
		return false
	}
	if resourceID != "" && policy.AccessProfile != domain.ProfileRestricted {
		return false
	}
	if policy.AccessProfile != domain.ProfileRestricted && policy.AccessProfile != domain.ProfileQuarantine && policy.AccessProfile != domain.ProfileOnboarding {
		return false
	}
	return resourceID == "" || slices.Contains(policy.ResourceIDs, resourceID)
}

func admissionGovernanceReason(site domain.Site, device domain.Device, networkLease, resourceLease bool) string {
	if !governedUnmanaged(site, device) {
		return ""
	}
	if site.EffectiveDeviceGovernance() == domain.DeviceGovernanceManaged {
		return "managed_device_required"
	}
	if networkLease || resourceLease {
		return "unmanaged_device_requires_resource_ztna"
	}
	return ""
}
