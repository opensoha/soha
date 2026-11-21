package networkruntime

import (
	"context"
	"errors"
	"slices"

	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func validateEAPActivationProfile(tx *gorm.DB, parent domain.Credential, request networkprotocol.EAPCertificateActivateRequest) error {
	ctx := tx.Statement.Context
	repo := accessrepo.New(tx)
	device, err := repo.GetDevice(ctx, parent.DeviceID)
	if err != nil {
		return err
	}
	if err := repo.LockAccessNetworkSite(ctx, device.SiteID); err != nil {
		return staleEAPCertificate()
	}
	networks, err := repo.ListAccessNetworks(ctx, device.SiteID)
	if err != nil {
		return err
	}
	managed := slices.ContainsFunc(networks, access.AccessNetwork.ManagesNASAdmission)
	if !managed {
		binding, err := lockEAPNASBinding(ctx, tx, repo, request.ProfileID)
		if err != nil {
			return err
		}
		profile, ok := eapTLSProfile(parent.SubjectID, binding)
		if ok && binding.SiteID == device.SiteID && networkprotocol.AdmissionProfileRevision(profile) == request.ProfileRevision {
			return nil
		}
		return staleEAPCertificate()
	}
	for _, network := range networks {
		if network.ID == request.ProfileID {
			return validateManagedEAPProfile(ctx, tx, repo, parent.SubjectID, request.ProfileRevision, network)
		}
	}
	return staleEAPCertificate()
}

func validateManagedEAPProfile(ctx context.Context, tx *gorm.DB, repo *accessrepo.Repository, subjectID, revision string, network access.AccessNetwork) error {
	for _, bindingID := range network.Configuration.NASBindingIDs {
		binding, err := lockEAPNASBinding(ctx, tx, repo, bindingID)
		if errors.Is(err, apperrors.ErrConflict) {
			continue
		}
		if err != nil {
			return err
		}
		if !network.ActiveNASBinding(binding) {
			continue
		}
		binding.ID, binding.SSID, binding.AccessMedium = network.ID, network.Configuration.SSID, network.Configuration.AccessMedium
		profile, ok := eapTLSProfile(subjectID, binding)
		if ok && network.ProfileRevision(profile) == revision {
			return nil
		}
	}
	return staleEAPCertificate()
}

func lockEAPNASBinding(ctx context.Context, tx *gorm.DB, repo *accessrepo.Repository, bindingID string) (access.NASBinding, error) {
	var id string
	if err := tx.Raw(`SELECT id FROM network_access_nas_bindings WHERE id=? AND tenant_id='default' AND workspace_id='default' FOR SHARE`, bindingID).Row().Scan(&id); err != nil {
		return access.NASBinding{}, staleEAPCertificate()
	}
	return repo.GetNASBinding(ctx, id)
}

func eapTLSProfile(subjectID string, binding access.NASBinding) (networkprotocol.AdmissionProfile, bool) {
	trust := binding.EAPServerTrust
	if binding.Status != access.StatusActive || trust == nil || trust.EapMethod != "" && trust.EapMethod != "tls" {
		return networkprotocol.AdmissionProfile{}, false
	}
	return networkprotocol.AdmissionProfile{
		ID: binding.ID, SiteID: binding.SiteID, AccessMedium: binding.AccessMedium, SSID: binding.SSID,
		Identity: subjectID, ServerNames: trust.ServerNames, CACertificates: trust.CaCertificates, WifiSecurity: string(trust.WifiSecurity),
		EAPMethod: "tls", InnerMethod: trust.InnerMethod, CredentialSource: trust.CredentialSource,
	}, true
}
