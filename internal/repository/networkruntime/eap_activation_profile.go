package networkruntime

import (
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func validateEAPActivationProfile(tx *gorm.DB, parent domain.Credential, request networkprotocol.EAPCertificateActivateRequest) error {
	var id string
	if err := tx.Raw(`SELECT id FROM network_access_nas_bindings WHERE id=? AND tenant_id='default' AND workspace_id='default' FOR SHARE`, request.ProfileID).Row().Scan(&id); err != nil {
		return staleEAPCertificate()
	}
	binding, err := accessrepo.New(tx).GetNASBinding(tx.Statement.Context, id)
	if err != nil {
		return err
	}
	trust := binding.EAPServerTrust
	if binding.Status != access.StatusActive || trust == nil || trust.EapMethod != "" && trust.EapMethod != "tls" {
		return staleEAPCertificate()
	}
	profile := networkprotocol.AdmissionProfile{ID: binding.ID, SiteID: binding.SiteID, AccessMedium: binding.AccessMedium, SSID: binding.SSID, Identity: parent.SubjectID, ServerNames: trust.ServerNames, CACertificates: trust.CaCertificates, WifiSecurity: string(trust.WifiSecurity), EAPMethod: "tls"}
	if networkprotocol.AdmissionProfileRevision(profile) != request.ProfileRevision {
		return staleEAPCertificate()
	}
	return nil
}
