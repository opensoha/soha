package networkaccess

import (
	"slices"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

func MatchesEndpointResponse(config domain.EndpointResponsePolicyConfig, subject domain.Subject, device domain.Device, binding domain.NASBinding) bool {
	return slices.Contains(config.SiteIDs, binding.SiteID) && matchNetworkValue(config.NasBindingIDs, binding.ID) &&
		matchNetworkValue(config.DeviceIDs, device.ID) && matchNetworkValue(config.Subjects.Users, subject.UserID) && matchNetworkSlice(config.Subjects.Roles, subject.Roles) &&
		matchNetworkSlice(config.Subjects.Teams, subject.Teams) && matchNetworkSlice(config.Subjects.Tags, subject.Tags)
}

func EndpointResponseSignal(config domain.EndpointResponsePolicyConfig, presence domain.PresenceRecord, now time.Time) (bool, time.Time, string) {
	if config.Signal == sohaapi.NetworkEndpointResponseSignalUIUnavailable && (presence.UIState == sohaapi.NetworkEndpointPresenceUIStateUnknown || presence.UIState == "") {
		return false, now, "endpoint_ui_signal_unknown"
	}
	threshold := time.Duration(config.HeartbeatIntervalSeconds*config.MissedHeartbeats+config.GracePeriodSeconds) * time.Second
	if presence.LastReceivedAt == nil || presence.LastObservedAt == nil {
		return true, now, "endpoint_presence_unknown"
	}
	last := *presence.LastObservedAt
	if last.After(*presence.LastReceivedAt) {
		last = *presence.LastReceivedAt
	}
	deadline := last.Add(threshold)
	if config.Signal == sohaapi.NetworkEndpointResponseSignalUIUnavailable {
		if presence.UIState == sohaapi.NetworkEndpointPresenceUIStateUnknown || presence.UIState == "" {
			return false, now, "endpoint_ui_signal_unknown"
		}
		if presence.UIState != sohaapi.NetworkEndpointPresenceUIStateOnline || presence.UILastObservedAt == nil {
			return true, deadline, "endpoint_ui_unavailable"
		}
		deadline = presence.UILastObservedAt.Add(threshold)
	} else if presence.ServiceState != sohaapi.NetworkEndpointPresenceServiceStateHealthy {
		return true, deadline, "endpoint_service_unhealthy"
	}
	if !now.Before(deadline) {
		return true, deadline, "endpoint_presence_stale"
	}
	return false, deadline, "endpoint_healthy"
}

func EndpointRecoveryReady(config domain.EndpointResponsePolicyConfig, presence domain.PresenceRecord, now time.Time) bool {
	if presence.LastReceivedAt == nil || presence.LastObservedAt == nil || presence.HealthySince == nil ||
		presence.ServiceState != sohaapi.NetworkEndpointPresenceServiceStateHealthy || presence.HealthySamples < config.RestoreHeartbeats {
		return false
	}
	if now.Sub(*presence.LastReceivedAt) > 2*time.Duration(config.HeartbeatIntervalSeconds)*time.Second ||
		now.Sub(*presence.LastObservedAt) > 2*time.Duration(config.HeartbeatIntervalSeconds)*time.Second ||
		now.Sub(*presence.HealthySince) < time.Duration(config.RestoreObservationSeconds)*time.Second {
		return false
	}
	if config.Signal == sohaapi.NetworkEndpointResponseSignalUIUnavailable {
		return presence.UIState == sohaapi.NetworkEndpointPresenceUIStateOnline && presence.UIHealthySince != nil &&
			presence.UILastObservedAt != nil && now.Sub(*presence.UILastObservedAt) <= 2*time.Duration(config.HeartbeatIntervalSeconds)*time.Second &&
			presence.UIHealthySamples >= config.RestoreHeartbeats && now.Sub(*presence.UIHealthySince) >= time.Duration(config.RestoreObservationSeconds)*time.Second
	}
	return true
}
