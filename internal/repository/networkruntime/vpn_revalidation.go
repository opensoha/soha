package networkruntime

import (
	"encoding/json"
	"errors"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

type managedSessionBinding struct {
	RuntimeID, SubjectID, DeviceID, AuthSessionID string
	Configuration                                 []byte
}

func managedSessionBindings(tx *gorm.DB, runtimeID, sessionID string) ([]managedSessionBinding, error) {
	var rows []struct {
		RuntimeID, SubjectID, DeviceID, AuthSessionID string
		Configuration                                 jsonDocument
	}
	err := tx.Raw(`SELECT s.runtime_id,s.subject_id,s.device_id,i.auth_session_id,d.published_configuration AS configuration
 FROM network_runtime_sessions s JOIN network_vpn_connection_intents i ON i.session_id=s.id AND i.status='consumed'
 LEFT JOIN network_vpn_documents d ON d.id=s.vpn_profile_id AND d.deleted_at IS NULL
 LEFT JOIN network_access_gateways g ON g.id=s.gateway_id
 WHERE s.tenant_id='default' AND s.workspace_id='default' AND s.status IN ('pending','active','restricted','quarantine')
 AND (s.runtime_id=? OR g.runtime_id=?) AND (?='' OR s.id=?) LIMIT 1025`, runtimeID, runtimeID, sessionID, sessionID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) > 1024 {
		return nil, apperrors.ErrServiceUnavailable
	}
	items := make([]managedSessionBinding, 0, len(rows))
	for _, row := range rows {
		items = append(items, managedSessionBinding{RuntimeID: row.RuntimeID, SubjectID: row.SubjectID, DeviceID: row.DeviceID, AuthSessionID: row.AuthSessionID, Configuration: []byte(row.Configuration)})
	}
	return items, nil
}

func managedSessionAllowed(tx *gorm.DB, b managedSessionBinding, now time.Time) (bool, error) {
	if err := activeVPNAuthSession(tx, b.AuthSessionID, b.SubjectID, now); errors.Is(err, apperrors.ErrAccessDenied) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var profile domain.VPNProfileConfig
	if len(b.Configuration) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(b.Configuration, &profile); err != nil {
		return false, err
	}
	if !profile.Enabled {
		return false, nil
	}
	repo := networkrepo.New(tx)
	subject, err := repo.GetSubject(tx.Statement.Context, b.SubjectID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	device, err := repo.GetDevice(tx.Statement.Context, b.DeviceID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return subject.Status == domain.StatusActive && device.Status == domain.DeviceStatusActive && profile.Assignments.Matches(subject, device), nil
}

func revokeInvalidManagedSessions(tx *gorm.DB, runtimeID string, now time.Time) error {
	bindings, err := managedSessionBindings(tx, runtimeID, "")
	if err != nil {
		return err
	}
	for _, b := range bindings {
		allowed, err := managedSessionAllowed(tx, b, now)
		if err != nil {
			return err
		}
		if !allowed {
			if err := revokeRuntimePeers(tx, b.RuntimeID, now, "managed_vpn_authorization_revoked"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateManagedRenewal(tx *gorm.DB, runtimeID, sessionID string, now time.Time) error {
	bindings, err := managedSessionBindings(tx, runtimeID, sessionID)
	if err != nil {
		return err
	}
	for _, b := range bindings {
		allowed, err := managedSessionAllowed(tx, b, now)
		if err != nil {
			return err
		}
		if !allowed {
			return invalidVPNIntent()
		}
	}
	return nil
}

func configurationLeasesCurrent(tx *gorm.DB, desired networkprotocol.ConfigurationDesired, now time.Time) (bool, error) {
	ids := make([]string, 0, len(desired.NetworkLeases)+len(desired.ResourceLeases))
	for _, lease := range desired.NetworkLeases {
		ids = append(ids, lease.ID)
	}
	for _, lease := range desired.ResourceLeases {
		ids = append(ids, lease.ID)
	}
	if len(ids) == 0 {
		return true, nil
	}
	var count int
	err := tx.Raw(`SELECT count(*) FROM network_runtime_leases l JOIN network_runtime_sessions s ON s.id=l.session_id
 WHERE l.id IN ? AND l.tenant_id='default' AND l.workspace_id='default' AND l.status IN ('issued','active') AND l.expires_at>?
 AND s.status IN ('pending','active','restricted','quarantine') AND s.valid_until>?`, ids, now, now).Row().Scan(&count)
	return count == len(ids), err
}
