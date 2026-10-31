package networkaccess

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func CanonicalStationID(value string) string {
	if address, err := net.ParseMAC(value); err == nil && len(address) == 6 {
		return address.String()
	}
	return value
}

func ValidStationID(value string) bool {
	address, err := net.ParseMAC(value)
	return err == nil && len(address) == 6
}

type EAPServerTrust = sohaapi.NetworkEAPServerTrust
type EndpointPresence = sohaapi.NetworkEndpointPresence
type EndpointResponsePolicy = sohaapi.NetworkEndpointResponsePolicy
type EndpointResponsePolicyConfig = sohaapi.NetworkEndpointResponsePolicyConfiguration
type EndpointResponsePolicyInput = sohaapi.NetworkEndpointResponsePolicyInput
type EndpointResponsePolicyRevision = sohaapi.NetworkEndpointResponsePolicyRevision
type EndpointResponseStep = sohaapi.NetworkEndpointResponseStep
type EndpointResponseEvent = sohaapi.NetworkEndpointResponseEvent
type EndpointResponsePreview = sohaapi.NetworkEndpointResponsePreview
type EndpointMaintenance = sohaapi.NetworkEndpointMaintenance
type EndpointMaintenanceInput = sohaapi.NetworkEndpointMaintenanceInput
type NASSessionEvidence = sohaapi.NetworkNASSessionEvidence

type EndpointResponseFilter struct {
	SiteID, DeviceID, Status, PolicyID string
	Limit, Offset                      int
}

type PresenceRecord struct {
	EndpointPresence
	CredentialID             string
	PayloadHash              string
	Epoch                    int
	HeartbeatIntervalSeconds int
}

type NASSessionObservation struct {
	NASSessionEvidence
	SubjectID, DeviceID, StationID string
	CredentialID                   string
	LastStatus                     string
	LastSessionSeconds             int64
	AccountingEpoch                time.Time
	VLANID                         int
	FilterID                       string
	NASPortID                      string
}

type EndpointResponseRecord struct {
	EndpointResponseEvent
	RestrictionProfile     string
	NotificationPolicyID   string
	NotificationAttempts   int
	NotificationLeaseUntil *time.Time
}

type EndpointRestriction struct {
	DeviceID, SiteID, AccessProfile string
	Epoch                           int
}

func AdmissionAttributesHash(subject Subject, device Device, site Site, binding NASBinding, profile *SiteProfileBinding) string {
	subject.Roles, subject.Teams, subject.Tags = slices.Clone(subject.Roles), slices.Clone(subject.Teams), slices.Clone(subject.Tags)
	slices.Sort(subject.Roles)
	slices.Sort(subject.Teams)
	slices.Sort(subject.Tags)
	management, governance := device.EffectiveManagementState(), site.EffectiveDeviceGovernance()
	if management == DeviceManagementUnknown && governance == DeviceGovernanceLegacy {
		management, governance = "", ""
	}
	proofContext := ""
	if site.DeviceGovernance == DeviceGovernanceVerified {
		proofContext = fmt.Sprint(site.DeviceGovernance, "/", site.ManagementIssuerKey, "/", device.ManagementIssuerKey, "/", device.ManagementVerifiedUntil)
	}
	values := struct {
		ManagementProof                                                             string `json:",omitempty"`
		Subject                                                                     Subject
		DeviceID, OwnerID, DeviceSite, DeviceStatus, Posture                        string
		ManagementState                                                             string `json:",omitempty"`
		DeviceGovernance                                                            string `json:",omitempty"`
		PostureVersion, CredentialGeneration                                        int
		SiteID, SiteStatus, BindingID, RuntimeID, NASID, BindingSite, BindingStatus string
		Profile                                                                     *SiteProfileBinding
		CoASupported, DisconnectSupported                                           bool
	}{proofContext, subject, device.ID, device.OwnerUserID, device.SiteID, device.Status, device.PostureStatus, management, governance, device.PostureVersion, device.CredentialGeneration, site.ID, site.Status, binding.ID, binding.RuntimeID, binding.NASID, binding.SiteID, binding.Status, profile, binding.CoASupported, binding.DisconnectSupported}
	raw, _ := json.Marshal(values)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}

func ResponsePolicyHash(policies []EndpointResponsePolicy) string {
	versions := make([][2]any, len(policies))
	for i, policy := range policies {
		versions[i] = [2]any{policy.ID, policy.PublishedRevision}
	}
	raw, _ := json.Marshal(versions)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}

func AdmissionDecisionVersion(inputsHash string, policyVersion int, responsePolicyHash, presenceHash string, restrictionEpoch int) string {
	raw, _ := json.Marshal([]any{inputsHash, policyVersion, responsePolicyHash, presenceHash, restrictionEpoch})
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}

type EndpointResponseSnapshot struct {
	Policy           EndpointResponsePolicy
	Presence         PresenceRecord
	Observation      NASSessionObservation
	Session          Session
	Device           Device
	Subject          Subject
	Site             Site
	Binding          NASBinding
	AccessPolicies   PolicySnapshot
	Profiles         map[string]SiteProfileBinding
	CredentialActive bool
	Maintenance      bool
	PolicySuperseded bool
	Event            *EndpointResponseRecord
	Commands         []SessionCommand
}

type EndpointResponseMutation struct {
	Event                 EndpointResponseRecord
	Command               *SessionCommand
	CancelPendingCommands bool
}

type EndpointResponseCandidate struct{ PolicyID, SessionID, EventID string }

// Onboarding is a separate low-trust network, so ambiguous intersections deny.
func RestrictAccessProfile(profile, floor string) string {
	if floor == "" || profile == floor {
		return profile
	}
	if profile == ProfileDeny || floor == ProfileDeny {
		return ProfileDeny
	}
	if profile == ProfileFull {
		return floor
	}
	if floor == ProfileFull {
		return profile
	}
	if profile == ProfileRestricted && floor == ProfileQuarantine || profile == ProfileQuarantine && floor == ProfileRestricted {
		return ProfileQuarantine
	}
	return ProfileDeny
}
