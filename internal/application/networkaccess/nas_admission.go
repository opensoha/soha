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

func evaluateNASAdmission(snapshot domain.NASAdmissionSnapshot, now time.Time) domain.NASAdmissionMutation {
	session := snapshot.Session
	preview := EvaluateAdmission(AdmissionInput{SubjectUserID: session.SubjectID, DeviceID: session.DeviceID, SiteID: session.SiteID, Mode: domain.ModeInternalDirect}, snapshot.Subject, snapshot.Device, snapshot.Site, snapshot.Policies)
	profile, reason := preview.NetworkProfile, "nas_admission_revalidated"
	if preview.Decision != domain.DecisionAllow {
		profile = domain.ProfileDeny
		if len(preview.Reasons) > 0 {
			reason = preview.Reasons[0]
		}
	}
	if session.ExpiresAt.Sub(now) < time.Second {
		profile, reason = domain.ProfileDeny, "nas_authorization_expired"
	} else if !snapshot.CredentialActive {
		profile, reason = domain.ProfileDeny, "endpoint_credential_inactive"
	} else if snapshot.Binding.Status != domain.StatusActive || snapshot.Binding.SiteID != session.SiteID {
		profile, reason = domain.ProfileDeny, "nas_binding_inactive"
	}
	profile = domain.RestrictAccessProfile(profile, domain.RestrictAccessProfile(session.AccessProfile, snapshot.FloorProfile))
	var attributes *networkprotocol.RadiusAttributes
	var binding *domain.SiteProfileBinding
	if profile != domain.ProfileDeny {
		current, ok := snapshot.Profiles[profile]
		if !ok {
			profile, reason = domain.ProfileDeny, "nas_profile_unavailable"
		} else {
			binding = &current
			attributes = &networkprotocol.RadiusAttributes{VLANID: current.VLANID, FilterID: current.FilterID, SessionTimeoutSeconds: current.SessionTimeoutSeconds}
		}
	}

	return domain.NASAdmissionMutation{Profile: profile, InputsHash: domain.AdmissionAttributesHash(snapshot.Subject, snapshot.Device, snapshot.Site, snapshot.Binding, binding), Reason: reason, PolicyVersion: max(preview.PolicyVersion, session.PolicyVersion), RadiusAttributes: attributes, Attempts: snapshot.Attempts}
}

func nasAdmissionMappingChanged(snapshot domain.NASAdmissionSnapshot, attributes *networkprotocol.RadiusAttributes, profile, oldProfile string) bool {
	// A mapping change may broaden an ACL within the same profile; require fresh authentication.
	return profile == oldProfile && attributes != nil && snapshot.RadiusAttributes != nil && (attributes.VLANID != snapshot.RadiusAttributes.VLANID || attributes.FilterID != snapshot.RadiusAttributes.FilterID || snapshot.Revision > 0 && attributes.SessionTimeoutSeconds != snapshot.RadiusAttributes.SessionTimeoutSeconds)
}

func nasAdmissionCommand(snapshot domain.NASAdmissionSnapshot, mutation *domain.NASAdmissionMutation, mappingChanged, retry bool, now time.Time) domain.SessionCommand {
	session := snapshot.Session
	action := domain.SessionActionCoA
	if mutation.Profile == domain.ProfileDeny || mappingChanged || !snapshot.Binding.CoASupported || retry && session.LatestCommand.Action == domain.SessionActionCoA {
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
