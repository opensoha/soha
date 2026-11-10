package networkaccess

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) EndpointRestriction(ctx context.Context, deviceID, siteID string) (domain.EndpointRestriction, error) {
	result := domain.EndpointRestriction{DeviceID: deviceID, SiteID: siteID}
	var profiles []string
	if err := r.db.WithContext(ctx).Raw(`SELECT restriction_profile FROM network_endpoint_response_events WHERE tenant_id = 'default' AND workspace_id = 'default' AND device_id = ? AND site_id = ? AND restored_at IS NULL AND restriction_profile IS NOT NULL ORDER BY id`, deviceID, siteID).Scan(&profiles).Error; err != nil {
		return result, err
	}
	for _, profile := range profiles {
		if result.AccessProfile == "" {
			result.AccessProfile = profile
		} else {
			result.AccessProfile = domain.RestrictAccessProfile(result.AccessProfile, profile)
		}
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT COALESCE(MAX(epoch), 0) FROM network_endpoint_response_epochs WHERE tenant_id = 'default' AND workspace_id = 'default' AND device_id = ? AND site_id = ?`, deviceID, siteID).Scan(&result.Epoch).Error; err != nil {
		return result, err
	}
	return result, nil
}

func enforceNASCommandRestriction(tx *gorm.DB, command domain.SessionCommand) error {
	var deviceID, siteID, admissionFloor, subjectID string
	if err := tx.Raw(`SELECT device_id, COALESCE(site_id, ''), admission_floor_profile,subject_id FROM network_runtime_sessions WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND nas_id = ?`, command.SessionID, command.RuntimeID, command.NASID).Row().Scan(&deviceID, &siteID, &admissionFloor, &subjectID); err != nil {
		return err
	}
	if command.Action == domain.SessionActionDisconnect {
		return nil
	}
	var restricted bool
	if err := tx.Raw(`SELECT network_security_subject_restricted(?)`, subjectID).Scan(&restricted).Error; err != nil {
		return err
	}
	if restricted {
		return apperrors.NewBusiness(apperrors.ErrConflict, "network_security_restricted", "A DLP hold prevents widening this NAS session.", "DLP 限制禁止扩大该 NAS 会话权限。")
	}

	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-endpoint-device:"+deviceID).Error; err != nil {
		return err
	}
	floor, err := New(tx).EndpointRestriction(tx.Statement.Context, deviceID, siteID)
	if err != nil {
		return err
	}
	if domain.RestrictAccessProfile(command.TargetAccessProfile, floor.AccessProfile) != command.TargetAccessProfile || domain.RestrictAccessProfile(command.TargetAccessProfile, admissionFloor) != command.TargetAccessProfile {
		return apperrors.NewBusiness(apperrors.ErrConflict, "endpoint_response_restriction_active", "An active security restriction prevents broadening this NAS session.", "活动安全限制禁止扩大该 NAS 会话权限。")
	}
	return nil
}

func currentCommandResponse(tx *gorm.DB, commandID string, now time.Time) (bool, error) {
	var active bool
	if err := tx.Raw(`SELECT NOT EXISTS (
        SELECT 1 FROM network_nas_session_commands c LEFT JOIN network_endpoint_response_events e
        ON e.tenant_id = c.tenant_id AND e.workspace_id = c.workspace_id AND e.id = c.response_event_id
        WHERE c.id = ? AND c.response_event_id IS NOT NULL
          AND (e.id IS NULL OR e.restored_at IS NOT NULL OR (e.event->>'restrictionEpoch')::bigint <> c.response_epoch)
    )`, commandID).Scan(&active).Error; err != nil {
		return false, err
	}
	if active {
		command, err := scanSessionCommand(tx.Raw(sessionCommandSelect+` AND id=?`, commandID).Row())
		if err != nil {
			return false, err
		}
		if err := enforceNASCommandRestriction(tx, command); err != nil {
			if !errors.Is(err, apperrors.ErrConflict) {
				return false, err
			}
			active = false
		}
	}
	if !active {
		if err := tx.Exec(`UPDATE network_nas_session_commands SET status = 'expired', result_reason_code = 'endpoint_response_superseded', completed_at = ?, updated_at = ? WHERE id = ? AND status IN ('pending', 'delivered')`, now, now, commandID).Error; err != nil {
			return false, err
		}
	}
	return active, nil
}

func nextRestrictionEpoch(tx *gorm.DB, deviceID, siteID string) (int, error) {
	var epoch int
	err := tx.Raw(`INSERT INTO network_endpoint_response_epochs (device_id, site_id, epoch) VALUES (?, ?, 1)
        ON CONFLICT (tenant_id, workspace_id, device_id, site_id) DO UPDATE SET epoch = network_endpoint_response_epochs.epoch + 1 RETURNING epoch`, deviceID, siteID).Row().Scan(&epoch)
	return epoch, err
}

func marshalResponseEvent(event domain.EndpointResponseEvent) (string, error) {
	raw, err := json.Marshal(event)
	return string(raw), err
}
