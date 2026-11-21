package networkaccess

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func reconcileNASAdmission(snapshot domain.NASAdmissionSnapshot, now time.Time) *domain.NASAdmissionMutation {
	session := snapshot.Session
	if session.Status == "expired" || session.Status == "revoked" {
		return nil
	}
	decision := evaluateNASAdmission(snapshot, now)
	unchanged := decision.InputsHash == snapshot.InputsHash && decision.Profile == snapshot.FloorProfile && decision.PolicyVersion == session.PolicyVersion
	retry := retryNASAdmissionCommand(session.LatestCommand, snapshot.Attempts, now)
	if unchanged && !retry {
		return nil
	}
	mutation := &decision
	oldProfile := domain.RestrictAccessProfile(session.AccessProfile, snapshot.FloorProfile)
	mappingChanged := nasAdmissionMappingChanged(snapshot, mutation.RadiusAttributes, mutation.Profile, oldProfile)
	retryUnsupported := !unchanged && session.LatestCommand != nil && session.LatestCommand.Status == domain.SessionCommandUnsupported
	if mutation.Profile == oldProfile && !mappingChanged && !retry && !retryUnsupported {
		return mutation
	}
	mutation.Attempts = 1
	if unchanged {
		mutation.Attempts = snapshot.Attempts + 1
	}
	command := nasAdmissionCommand(snapshot, mutation, mappingChanged, retry, now)
	mutation.Command = &command
	return mutation
}

// EvaluateNASNetwork is shared by fresh RADIUS authorization and live-session revalidation.
func EvaluateNASNetwork(now time.Time, snapshot domain.NASAdmissionSnapshot) AccessNetworkDecision {
	matched, managed := MatchAccessNetwork(snapshot.AccessNetworks, snapshot.Binding, AccessAttachment(snapshot.Attachment))
	if snapshot.AccessNetworkID != "" && (matched == nil || matched.ID != snapshot.AccessNetworkID) ||
		snapshot.Session.ID != "" && managed && snapshot.AccessNetworkID == "" {
		return AccessNetworkDecision{ReasonCode: "access_network_reauthentication_required"}
	}
	if matched != nil {
		if snapshot.AuthenticationMethod == networkprotocol.NASAuthenticationPasswordCompatible {
			return AccessNetworkDecision{AccessNetworkID: matched.ID, ReasonCode: "access_network_authentication_method_denied"}
		}
		return evaluateAccessNetwork(now, *matched, snapshot.AuthenticationMethod, snapshot.Subject, snapshot.Device, snapshot.Site, snapshot.Policies, snapshot.Binding.SupportsStrictOnline())
	}
	if managed {
		return AccessNetworkDecision{ReasonCode: "access_network_attachment_unmatched"}
	}
	preview := evaluateAdmissionAt(now, AdmissionInput{SubjectUserID: snapshot.Subject.UserID, DeviceID: snapshot.Device.ID, SiteID: snapshot.Site.ID, Mode: domain.ModeInternalDirect}, snapshot.Subject, snapshot.Device, snapshot.Site, snapshot.Policies)
	reason := "authorization_denied"
	if preview.Decision == domain.DecisionAllow {
		reason = "policy_allowed"
	} else if len(preview.Reasons) != 0 {
		reason = preview.Reasons[0]
	}
	return AccessNetworkDecision{Allowed: preview.Decision == domain.DecisionAllow, ReasonCode: reason, AccessProfile: preview.NetworkProfile}
}

func NASInitialProfile(decision AccessNetworkDecision, method string) (profile, reason string) {
	if !decision.Allowed {
		return domain.ProfileDeny, decision.ReasonCode
	}
	if decision.AccessProfile == domain.ProfileFull && (method == networkprotocol.NASAuthenticationTTLS || method == networkprotocol.NASAuthenticationPasswordCompatible) {
		return domain.ProfileRestricted, "password_compatible_profile_capped"
	}
	return decision.AccessProfile, decision.ReasonCode
}

func NASNetworkAttributes(decision AccessNetworkDecision) *networkprotocol.RadiusAttributes {
	if !decision.Allowed || decision.AccessNetworkID == "" {
		return nil
	}
	attributes := &networkprotocol.RadiusAttributes{VLANID: decision.VLANID, FilterID: decision.FilterID, SessionTimeoutSeconds: decision.SessionTimeoutSeconds}
	if decision.StrictOnline {
		if attributes.SessionTimeoutSeconds == 0 || attributes.SessionTimeoutSeconds > domain.StrictNASReauthenticationSeconds {
			attributes.SessionTimeoutSeconds = domain.StrictNASReauthenticationSeconds
		}
		attributes.Reauthenticate = true
	}
	return attributes
}

func evaluateNASAdmission(snapshot domain.NASAdmissionSnapshot, now time.Time) domain.NASAdmissionMutation {
	session := snapshot.Session
	if snapshot.AuthenticationMethod == networkprotocol.NASAuthenticationTTLS {
		snapshot.Policies = domain.PasswordAdmissionPolicies(snapshot.Policies)
		if snapshot.Binding.EAPServerTrust == nil || snapshot.Binding.EAPServerTrust.EapMethod != "ttls" {
			snapshot.CredentialActive = false
		}
	}
	decision := EvaluateNASNetwork(now, snapshot)
	profile, reason := NASInitialProfile(decision, snapshot.AuthenticationMethod)
	if snapshot.StrictUIEpoch != "" || decision.StrictOnline {
		if !decision.StrictOnline || snapshot.StrictUIEpoch != snapshot.StrictUI.Epoch || !snapshot.StrictUI.CurrentFor(session.SubjectID, now) {
			profile, reason = domain.ProfileDeny, "strict_ui_lease_expired"
		}
	}
	if decision.Allowed && decision.AccessNetworkID == "" {
		reason = "nas_admission_revalidated"
	}
	if session.ExpiresAt.Sub(now) < time.Second {
		profile, reason = domain.ProfileDeny, "nas_authorization_expired"
	} else if !snapshot.CredentialActive {
		profile, reason = domain.ProfileDeny, "endpoint_credential_inactive"
	} else if snapshot.Binding.Status != domain.StatusActive || snapshot.Binding.SiteID != session.SiteID || snapshot.BindingID != "" && snapshot.Binding.ID != snapshot.BindingID {
		profile, reason = domain.ProfileDeny, "nas_binding_inactive"
	}
	profile = domain.RestrictAccessProfile(profile, domain.RestrictAccessProfile(session.AccessProfile, snapshot.FloorProfile))
	var attributes *networkprotocol.RadiusAttributes
	var binding *domain.SiteProfileBinding
	if profile != domain.ProfileDeny {
		var siteProfile *domain.SiteProfileBinding
		if current, ok := snapshot.Profiles[profile]; ok {
			siteProfile = &current
		}
		binding = domain.NASRadiusProfile(decision.AccessNetworkID, snapshot.Site.ID, profile, NASNetworkAttributes(decision), siteProfile)
		if binding == nil {
			profile, reason = domain.ProfileDeny, "nas_profile_unavailable"
		} else {
			timeout := binding.SessionTimeoutSeconds
			if decision.AccessNetworkID != "" && snapshot.Site.DeviceGovernance == domain.DeviceGovernanceVerified {
				timeout = min(timeout, int(session.ExpiresAt.Sub(session.StartedAt)/time.Second))
			}
			if decision.StrictOnline {
				timeout = min(domain.StrictNASReauthenticationSeconds, timeout, int(session.ExpiresAt.Sub(session.StartedAt)/time.Second))
			}
			attributes = &networkprotocol.RadiusAttributes{VLANID: binding.VLANID, FilterID: binding.FilterID, SessionTimeoutSeconds: timeout, Reauthenticate: decision.StrictOnline}
		}
	}

	return domain.NASAdmissionMutation{Profile: profile, InputsHash: domain.AdmissionAttributesHash(snapshot.Subject, snapshot.Device, snapshot.Site, snapshot.Binding, binding), Reason: reason, PolicyVersion: max(snapshot.Policies.PolicyVersion, session.PolicyVersion), RadiusAttributes: attributes, Attempts: snapshot.Attempts}
}

func nasAdmissionMappingChanged(snapshot domain.NASAdmissionSnapshot, attributes *networkprotocol.RadiusAttributes, profile, oldProfile string) bool {
	// A mapping change may broaden an ACL within the same profile; require fresh authentication.
	return profile == oldProfile && attributes != nil && snapshot.RadiusAttributes != nil && (attributes.VLANID != snapshot.RadiusAttributes.VLANID || attributes.FilterID != snapshot.RadiusAttributes.FilterID || (snapshot.Revision > 0 || snapshot.AccessNetworkID != "") && attributes.SessionTimeoutSeconds != snapshot.RadiusAttributes.SessionTimeoutSeconds)
}

func nasAdmissionCommand(snapshot domain.NASAdmissionSnapshot, mutation *domain.NASAdmissionMutation, mappingChanged, retry bool, now time.Time) domain.SessionCommand {
	session := snapshot.Session
	action := domain.SessionActionCoA
	if snapshot.StrictUIEpoch != "" || mutation.Profile == domain.ProfileDeny || mappingChanged || !snapshot.Binding.CoASupported || retry && session.LatestCommand.Action == domain.SessionActionCoA {
		action = domain.SessionActionDisconnect
	}
	if mappingChanged {
		mutation.Profile, mutation.Reason = domain.ProfileDeny, "nas_mapping_changed_reauthenticate"
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("nas-admission:%s:%d:%s:%s:%s", session.ID, snapshot.Revision+1, action, mutation.Profile, mutation.InputsHash)))
	command := domain.SessionCommand{ID: uuid.NewString(), SessionID: session.ID, RuntimeID: session.RuntimeID, NASID: session.NASID, Action: action, TargetAccessProfile: mutation.Profile, PolicyVersion: mutation.PolicyVersion, Status: domain.SessionCommandPending, ReasonCode: mutation.Reason, PlanHash: fmt.Sprintf("sha256:%x", digest), EffectiveAt: now, ExpiresAt: now.Add(time.Minute), CreatedAt: now}
	if action == domain.SessionActionCoA {
		copy := *mutation.RadiusAttributes
		copy.SessionTimeoutSeconds = min(copy.SessionTimeoutSeconds, int(session.ExpiresAt.Sub(now)/time.Second))
		command.RadiusAttributes = &copy
	}
	if !snapshot.StrongBinding || action == domain.SessionActionDisconnect && !snapshot.Binding.DisconnectSupported {
		command.Status, command.ReasonCode = domain.SessionCommandUnsupported, "nas_enforcement_unsupported"
		command.CompletedAt = &now
	}
	if action == domain.SessionActionDisconnect {
		command.RadiusAttributes = nil
	}
	return command
}

func retryNASAdmissionCommand(command *domain.SessionCommand, attempts int, now time.Time) bool {
	if command == nil || attempts >= 3 || command.Status == domain.SessionCommandApplied || command.Status == domain.SessionCommandUnsupported {
		return false
	}
	failedAt := command.ExpiresAt
	if command.CompletedAt != nil {
		failedAt = *command.CompletedAt
	}
	if now.Before(failedAt.Add(time.Duration(max(attempts, 1)) * 30 * time.Second)) {
		return false
	}
	return command.Status == domain.SessionCommandExpired || command.Status == domain.SessionCommandRejected || command.Status == domain.SessionCommandTimedOut || !now.Before(command.ExpiresAt)
}
