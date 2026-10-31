package networkaccess

import "time"

const (
	DeviceManagementManaged    = "managed"
	DeviceManagementUnmanaged  = "unmanaged"
	DeviceManagementUnknown    = "unknown"
	DeviceGovernanceLegacy     = "legacy"
	DeviceGovernanceVerified   = "verified_managed_only"
	DeviceGovernanceManaged    = "managed_only"
	DeviceGovernanceRestricted = "restricted_unmanaged"
)

func (device Device) EffectiveManagementState() string {
	if device.ManagementVerifiedUntil != nil && device.ManagementVerifiedUntil.After(time.Now()) {
		return DeviceManagementManaged
	}
	switch device.ManagementState {
	case DeviceManagementManaged, DeviceManagementUnmanaged:
		return device.ManagementState
	default:
		return DeviceManagementUnknown
	}
}

func (site Site) EffectiveDeviceGovernance() string {
	switch site.DeviceGovernance {
	case "", DeviceGovernanceLegacy:
		return DeviceGovernanceLegacy
	case DeviceGovernanceRestricted:
		return DeviceGovernanceRestricted
	default:
		return DeviceGovernanceManaged
	}
}
