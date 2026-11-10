package networkaccess

import (
	"context"
	"errors"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type AdmissionPreviewInput struct {
	DeviceID     string
	NASBindingID string
}

type admissionPreviewStore interface {
	AdmissionPreview(context.Context, string, string) (domain.AdmissionPreviewSnapshot, error)
}

func (s *Service) PreviewAdmission(ctx context.Context, principal domainidentity.Principal, input AdmissionPreviewInput) (domain.PolicyPreview, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyView); err != nil {
		return domain.PolicyPreview{}, err
	}
	input.DeviceID, input.NASBindingID = strings.TrimSpace(input.DeviceID), strings.TrimSpace(input.NASBindingID)
	if !runtimeIdentifierPattern.MatchString(input.DeviceID) || !runtimeIdentifierPattern.MatchString(input.NASBindingID) {
		return domain.PolicyPreview{}, invalid("deviceId and nasBindingId are required")
	}
	store, ok := s.store.(admissionPreviewStore)
	if !ok {
		return domain.PolicyPreview{}, apperrors.ErrServiceUnavailable
	}
	snapshot, err := store.AdmissionPreview(ctx, input.DeviceID, input.NASBindingID)
	if err != nil {
		return domain.PolicyPreview{}, err
	}
	device, binding := snapshot.Device, snapshot.Binding
	if device.ID != input.DeviceID || binding.ID != input.NASBindingID || snapshot.Site.ID != binding.SiteID {
		return domain.PolicyPreview{}, errors.New("admission preview snapshot does not match its target")
	}
	if err := s.authorizeScope(ctx, principal, appaccess.PermNetworkAccessPolicyView, resourceScope{kind: "NetworkPolicyPreview", id: binding.ID, siteID: binding.SiteID, deviceID: device.ID, ownerUserID: device.OwnerUserID}); err != nil {
		return domain.PolicyPreview{}, err
	}
	result := EvaluateAdmissionPreview(snapshot, time.Now().UTC())
	s.recordPreview(ctx, principal, PreviewInput{NASBindingID: binding.ID, SubjectUserID: device.OwnerUserID, DeviceID: device.ID, SiteID: binding.SiteID, Mode: domain.ModeInternalDirect}, result)
	return result, nil
}

func EvaluateAdmissionPreview(snapshot domain.AdmissionPreviewSnapshot, now time.Time) domain.PolicyPreview {
	device, binding := snapshot.Device, snapshot.Binding
	result := EvaluateAdmission(AdmissionInput{SubjectUserID: device.OwnerUserID, DeviceID: device.ID, SiteID: binding.SiteID, Mode: domain.ModeInternalDirect}, snapshot.Subject, device, snapshot.Site, snapshot.Policies)
	profile := domain.ProfileDeny
	if result.Decision == domain.DecisionAllow {
		profile = result.NetworkProfile
	}
	if binding.Status != domain.StatusActive || binding.SiteID != snapshot.Site.ID {
		profile, result.Reasons = domain.ProfileDeny, []string{"nas_binding_inactive"}
	}
	profile, reason := EndpointAdmissionProfile(profile, snapshot.Subject, device, binding, snapshot.ResponsePolicies, snapshot.Presence, snapshot.Restriction, result.PolicyVersion, now)
	if reason != "" {
		result.Reasons = []string{reason}
	}
	var selected *domain.SiteProfileBinding
	if profile != domain.ProfileDeny {
		current, ok := snapshot.Profiles[profile]
		if !ok || current.SiteID != binding.SiteID || current.AccessProfile != profile {
			profile, result.Reasons = domain.ProfileDeny, []string{"site_profile_binding_unavailable"}
		} else {
			selected = &current
		}
	}
	result.NetworkProfile = profile
	if profile == domain.ProfileDeny {
		result.Decision, result.Path = domain.DecisionDeny, domain.PathDeny
	}
	inputsHash := domain.AdmissionAttributesHash(snapshot.Subject, device, snapshot.Site, binding, selected)
	result.AttributeVersion = domain.AdmissionDecisionVersion(inputsHash, result.PolicyVersion, domain.ResponsePolicyHash(snapshot.ResponsePolicies), snapshot.Presence.PayloadHash, snapshot.Restriction.Epoch)
	result.EvaluatedAt = &now
	return result
}

func EndpointAdmissionProfile(profile string, subject domain.Subject, device domain.Device, binding domain.NASBinding, policies []domain.EndpointResponsePolicy, presence domain.PresenceRecord, floor domain.EndpointRestriction, policyVersion int, now time.Time) (string, string) {
	limited, reason := domain.RestrictAccessProfile(profile, floor.AccessProfile), ""
	if limited != profile {
		reason = "endpoint_response_restriction_active"
	}
	for _, policy := range policies {
		config := policy.PublishedConfiguration
		if config != nil && config.RequireHealthyForAdmission && MatchesEndpointResponse(*config, subject, device, binding) && (!EndpointRecoveryReady(*config, presence, now) || presence.PolicyVersion != policyVersion) {
			limited, reason = domain.ProfileDeny, "endpoint_health_required"
		}
	}
	return limited, reason
}
