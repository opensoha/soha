package networkcontrol

import (
	"context"
	"errors"
	"slices"
	"time"

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
	var catalog *admissionCatalog
	emitted := map[string]bool{}
	now := s.now().UTC()
	for after := ""; ; {
		bindings, err := s.store.AdmissionNASBindings(ctx, after, 100)
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			if binding.SiteID != principal.device.SiteID || binding.EAPServerTrust == nil || binding.Status != access.StatusActive {
				continue
			}
			method := networkprotocol.NASAuthenticationEAPTLS
			if binding.EAPServerTrust.EapMethod == "ttls" {
				if !slices.Contains(credential.Capabilities, networkprotocol.CapabilityTTLS) {
					continue
				}
				method = networkprotocol.NASAuthenticationTTLS
			}
			if catalog == nil {
				loaded, err := s.admissionCatalog(ctx, binding.SiteID)
				if err != nil {
					return nil, err
				}
				catalog = &loaded
			}
			profiles, err := catalog.profiles(ctx, now, principal, binding, method, fingerprint, emitted, s.store.SiteProfileBinding)
			if err != nil {
				return nil, err
			}
			result.Profiles = append(result.Profiles, profiles...)
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

type admissionCatalog struct {
	site     access.Site
	networks []access.AccessNetwork
	managed  bool
}

func (s *Service) admissionCatalog(ctx context.Context, siteID string) (admissionCatalog, error) {
	site, err := s.store.NetworkSite(ctx, siteID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return admissionCatalog{}, nil
	}
	if err != nil {
		return admissionCatalog{}, err
	}
	networks, err := s.store.ListAccessNetworks(ctx, siteID)
	if err != nil {
		return admissionCatalog{}, err
	}
	_, managed := app.MatchAccessNetwork(networks, access.NASBinding{SiteID: siteID}, app.AccessAttachment{})
	return admissionCatalog{site: site, networks: networks, managed: managed}, nil
}

func (catalog admissionCatalog) profiles(ctx context.Context, now time.Time, principal policyPrincipal, binding access.NASBinding, method, fingerprint string, emitted map[string]bool, lookup app.SiteRadiusProfileLookup) ([]networkprotocol.AdmissionProfile, error) {
	if catalog.site.ID == "" {
		return nil, nil
	}
	if !catalog.managed {
		if binding.AccessMedium != access.AccessMediumWired && (binding.AccessMedium != access.AccessMediumWiFi || binding.SSID == "") {
			return nil, nil
		}
		preview := app.EvaluateAdmission(app.AdmissionInput{SubjectUserID: principal.subject.UserID, DeviceID: principal.device.ID, SiteID: catalog.site.ID, Mode: access.ModeInternalDirect}, principal.subject, principal.device, catalog.site, principal.snapshot)
		if preview.Decision != access.DecisionAllow {
			return nil, nil
		}
		profile, err := buildAdmissionProfile(principal.subject.UserID, binding, fingerprint)
		return []networkprotocol.AdmissionProfile{profile}, err
	}
	var profiles []networkprotocol.AdmissionProfile
	for _, network := range catalog.networks {
		if emitted[network.ID] || !network.ActiveNASBinding(binding) {
			continue
		}
		decision, err := app.CheckAccessNetworkProfile(ctx, app.EvaluateAccessNetwork(now, network, method, principal.subject, principal.device, catalog.site, principal.snapshot), method, catalog.site.ID, lookup)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			continue
		}
		projected := binding
		projected.ID, projected.SSID, projected.AccessMedium = network.ID, network.Configuration.SSID, network.Configuration.AccessMedium
		profile, err := buildAdmissionProfile(principal.subject.UserID, projected, fingerprint)
		if err != nil {
			return nil, err
		}
		profile.ProfileRevision = network.ProfileRevision(profile)
		profiles = append(profiles, profile)
		emitted[network.ID] = true
	}
	return profiles, nil
}

func buildAdmissionProfile(subjectID string, binding access.NASBinding, fingerprint string) (networkprotocol.AdmissionProfile, error) {
	if err := app.ValidateEAPServerTrust(binding.EAPServerTrust); err != nil {
		return networkprotocol.AdmissionProfile{}, err
	}
	profile := networkprotocol.AdmissionProfile{ID: binding.ID, SiteID: binding.SiteID, AccessMedium: binding.AccessMedium, SSID: binding.SSID,
		Identity: subjectID, ServerNames: slices.Clone(binding.EAPServerTrust.ServerNames), CACertificates: slices.Clone(binding.EAPServerTrust.CaCertificates),
		WifiSecurity: string(binding.EAPServerTrust.WifiSecurity), ClientCertificateFingerprint: fingerprint}
	profile.EAPMethod = string(binding.EAPServerTrust.EapMethod)
	if profile.EAPMethod == "" {
		profile.EAPMethod = "tls"
	}
	profile.InnerMethod, profile.CredentialSource = binding.EAPServerTrust.InnerMethod, binding.EAPServerTrust.CredentialSource
	if profile.EAPMethod == "ttls" {
		profile.ClientCertificateFingerprint = ""
	}
	profile.ProfileRevision = networkprotocol.AdmissionProfileRevision(profile)
	return profile, nil
}
