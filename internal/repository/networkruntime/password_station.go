package networkruntime

import (
	"time"

	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
	"strings"
)

func validatePasswordNASBinding(tx *gorm.DB, proof domain.PasswordReceipt) error {
	var id string
	if strings.HasPrefix(proof.RuntimeID, "radius-member:") {
		b, e := accessrepo.New(tx).FindActiveNASBinding(tx.Statement.Context, proof.RuntimeID, proof.NASID)
		if e != nil {
			return e
		}
		id = b.ID
	}
	query := `SELECT n.id FROM network_access_nas_bindings n JOIN network_access_sites s ON s.id=n.site_id AND s.tenant_id=n.tenant_id AND s.workspace_id=n.workspace_id WHERE n.tenant_id='default' AND n.workspace_id='default' AND n.runtime_id=? AND n.nas_id=? AND n.site_id=? AND n.status='active' AND s.status='active' FOR SHARE OF n,s`
	scope := proof.RuntimeID
	if id != "" {
		query = strings.Replace(query, "n.runtime_id=?", "n.id=?", 1)
		scope = id
	}
	err := tx.Raw(query, scope, proof.NASID, proof.SiteID).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.ErrNotFound
	}
	if err != nil {
		return err
	}
	binding, err := accessrepo.New(tx).GetNASBinding(tx.Statement.Context, id)
	if err != nil {
		return err
	}
	trust := binding.EAPServerTrust
	if trust == nil || trust.EapMethod != "ttls" || trust.InnerMethod != "pap" || trust.CredentialSource != "user-input" {
		return passwordReceiptDenied()
	}
	return nil
}

func ensurePasswordStation(tx *gorm.DB, receipt domain.PasswordReceipt, deviceID string, now time.Time) error {
	digest := sha256.Sum256([]byte(receipt.RuntimeID + "\x00" + receipt.NASID + "\x00" + receipt.StationID + "\x00" + receipt.SubjectID))
	if deviceID != fmt.Sprintf("nas-station:%x", digest) {
		return passwordReceiptDenied()
	}
	if err := tx.Exec(`INSERT INTO network_access_devices(id,owner_user_id,name,platform,site_id,status,posture_status,management_state,device_type,ownership_type,created_at,updated_at) VALUES (?,?,'Password network station','unknown',?,'active','unknown','unmanaged','unknown','unassigned',?,?) ON CONFLICT DO NOTHING`, deviceID, receipt.SubjectID, receipt.SiteID, now, now).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO network_password_stations(device_id,runtime_id,nas_id,site_id,station_id,subject_id) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, deviceID, receipt.RuntimeID, receipt.NASID, receipt.SiteID, receipt.StationID, receipt.SubjectID).Error; err != nil {
		return err
	}
	var current bool
	err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_password_stations s JOIN network_access_devices d ON d.id=s.device_id WHERE s.device_id=? AND s.runtime_id=? AND s.nas_id=? AND s.site_id=? AND s.station_id=? AND s.subject_id=? AND d.owner_user_id=s.subject_id AND d.site_id=s.site_id AND d.status='active' AND d.posture_status='unknown' AND d.management_state='unmanaged')`, deviceID, receipt.RuntimeID, receipt.NASID, receipt.SiteID, receipt.StationID, receipt.SubjectID).Scan(&current).Error
	if err != nil {
		return err
	}
	if !current {
		return passwordReceiptDenied()
	}
	return nil
}

func preparePasswordStationAuthorization(tx *gorm.DB, authorization domain.NASAuthorization) error {
	proof := authorization.PasswordProof
	if proof == nil {
		return nil
	}
	if err := consumePasswordReceipt(tx, *proof, authorization.RequestID, authorization.CreatedAt); err != nil {
		return err
	}
	if err := validatePasswordNASBinding(tx, *proof); err != nil {
		return err
	}
	return ensurePasswordStation(tx, *proof, authorization.DeviceID, authorization.CreatedAt)
}
