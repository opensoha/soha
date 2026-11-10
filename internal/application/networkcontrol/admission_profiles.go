package networkcontrol

import (
	"context"
	"errors"
	"slices"

	app "github.com/opensoha/soha/internal/application/networkaccess"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) endpointAdmissionConfiguration(ctx context.Context, credential domain.Credential, snapshot domain.PolicySnapshot) (*networkprotocol.AdmissionConfiguration, error) {
	if credential.RuntimeKind != "endpoint" || !slices.Contains(credential.Capabilities, "eap-profile-v1") {
		return nil, nil
	}
	principal, err := s.loadPolicyPrincipal(ctx, credential.SubjectID, credential.DeviceID, snapshot)
	if err != nil {
		return nil, err
	}
	result := &networkprotocol.AdmissionConfiguration{Profiles: []networkprotocol.AdmissionProfile{}}
	result.RestrictionEpoch, err = s.store.EndpointAdmissionEpoch(ctx, credential.DeviceID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := s.store.EAPFingerprint(ctx, credential.ID, s.now().UTC())
	if err != nil {
		return nil, err
	}
	for after := ""; ; {
		bindings, err := s.store.AdmissionNASBindings(ctx, after, 100)
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			profile, err := s.admissionProfile(ctx, principal, binding, fingerprint)
			if err != nil {
				return nil, err
			}
			if profile != nil {
				result.Profiles = append(result.Profiles, *profile)
			}
			if len(result.Profiles) > 64 {
				return nil, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "admission_profile_limit", "More than 64 admission profiles match this endpoint.", "该终端匹配的接入配置超过 64 个。")
			}
		}
		if len(bindings) < 100 {
			break
		}
		after = bindings[len(bindings)-1].ID
	}
	return result, nil
}

func (s *Service) admissionProfile(ctx context.Context, principal policyPrincipal, binding access.NASBinding, fingerprint string) (*networkprotocol.AdmissionProfile, error) {
	if binding.Status != access.StatusActive || binding.EAPServerTrust == nil || binding.AccessMedium != access.AccessMediumWired && (binding.AccessMedium != access.AccessMediumWiFi || binding.SSID == "") {
		return nil, nil
	}
	site, err := s.store.NetworkSite(ctx, binding.SiteID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	preview := app.EvaluateAdmission(app.AdmissionInput{SubjectUserID: principal.subject.UserID, DeviceID: principal.device.ID, SiteID: site.ID, Mode: access.ModeInternalDirect}, principal.subject, principal.device, site, principal.snapshot)
	if preview.Decision != access.DecisionAllow {
		return nil, nil
	}
	if err := app.ValidateEAPServerTrust(binding.EAPServerTrust); err != nil {
		return nil, err
	}
	return &networkprotocol.AdmissionProfile{ID: binding.ID, SiteID: binding.SiteID, AccessMedium: binding.AccessMedium, SSID: binding.SSID,
		Identity: principal.subject.UserID, ServerNames: slices.Clone(binding.EAPServerTrust.ServerNames), CACertificates: slices.Clone(binding.EAPServerTrust.CaCertificates),
		WifiSecurity: string(binding.EAPServerTrust.WifiSecurity), ClientCertificateFingerprint: fingerprint}, nil
}
