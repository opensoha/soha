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

const responseEventSelect = `SELECT event, COALESCE(restriction_profile, ''), COALESCE(notification_policy_id, ''), notification_attempts, notification_lease_until FROM network_endpoint_response_events WHERE tenant_id = 'default' AND workspace_id = 'default'`

func scanResponseEvent(row scanner) (domain.EndpointResponseRecord, error) {
	var item domain.EndpointResponseRecord
	var raw []byte
	if err := row.Scan(&raw, &item.RestrictionProfile, &item.NotificationPolicyID, &item.NotificationAttempts, &item.NotificationLeaseUntil); err != nil {
		return item, err
	}
	return item, json.Unmarshal(raw, &item.EndpointResponseEvent)
}

func (r *Repository) EndpointResponseEvent(ctx context.Context, id string) (domain.EndpointResponseRecord, error) {
	return scanOne(r.db.WithContext(ctx).Raw(responseEventSelect+` AND id = ?`, id).Row(), scanResponseEvent, "endpoint response event")
}

func (r *Repository) EndpointResponseEvents(ctx context.Context, filter domain.EndpointResponseFilter) ([]domain.EndpointResponseRecord, error) {
	query, args := responseEventSelect, []any{}
	query, args = addFilter(query, args, "site_id", filter.SiteID)
	query, args = addFilter(query, args, "device_id", filter.DeviceID)
	query, args = addFilter(query, args, "status", filter.Status)
	query, args = addFilter(query, args, "policy_id", filter.PolicyID)
	query += ` ORDER BY triggered_at DESC, id DESC LIMIT ? OFFSET ?`
	return queryMany(ctx, r.db, query, append(args, filter.Limit, filter.Offset), scanResponseEvent)
}

func persistResponseMutation(tx *gorm.DB, mutation domain.EndpointResponseMutation, now time.Time) error {
	event := mutation.Event
	if event.ID == "" {
		return nil
	}
	var existingRevision int
	var existingProfile string
	if err := tx.Raw(`SELECT COALESCE(MAX(revision), 0) FROM network_endpoint_response_events WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, event.ID).Scan(&existingRevision).Error; err != nil {
		return err
	}
	if event.Revision != existingRevision+1 {
		return responseRevisionConflict()
	}
	if existingRevision > 0 {
		if err := tx.Raw(`SELECT COALESCE(restriction_profile, '') FROM network_endpoint_response_events WHERE tenant_id='default' AND workspace_id='default' AND id=?`, event.ID).Scan(&existingProfile).Error; err != nil {
			return err
		}
	}
	if event.RestrictionProfile != existingProfile || mutation.Command != nil || event.RestoredAt != nil {
		epoch, err := nextRestrictionEpoch(tx, event.DeviceID, event.SiteID)
		if err != nil {
			return err
		}
		event.RestrictionEpoch = epoch
	}
	if err := expireResponseMutationCommands(tx, mutation, now); err != nil {
		return err
	}
	raw, err := marshalResponseEvent(event.EndpointResponseEvent)
	if err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO network_endpoint_response_events (id, policy_id, policy_revision, device_id, site_id, session_id, revision, status, restriction_profile, event, notification_policy_id, triggered_at, updated_at, restored_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?::jsonb, NULLIF(?, ''), ?, ?, ?)
        ON CONFLICT (tenant_id, workspace_id, id) DO UPDATE SET revision=EXCLUDED.revision, status=EXCLUDED.status, restriction_profile=EXCLUDED.restriction_profile, event=EXCLUDED.event, notification_policy_id=EXCLUDED.notification_policy_id, updated_at=EXCLUDED.updated_at, restored_at=EXCLUDED.restored_at`,
		event.ID, event.PolicyID, event.PolicyRevision, event.DeviceID, event.SiteID, event.SessionID, event.Revision, event.Status, event.RestrictionProfile, raw, event.NotificationPolicyID, event.TriggeredAt, now, event.RestoredAt).Error; err != nil {
		return err
	}
	if event.RestoredAt != nil {
		// Recovery clears the floor; old NAS sessions must authenticate again.
		if err := tx.Exec(`UPDATE network_runtime_sessions SET status='expired', revoke_reason='endpoint_response_restored_reauthenticate', updated_at=? WHERE tenant_id='default' AND workspace_id='default' AND id=?`, now, event.SessionID).Error; err != nil {
			return err
		}
	}
	if mutation.Command != nil {
		command := *mutation.Command
		var attributes any
		if command.RadiusAttributes != nil {
			encoded, err := json.Marshal(command.RadiusAttributes)
			if err != nil {
				return err
			}
			attributes = string(encoded)
		}
		return tx.Exec(`INSERT INTO network_nas_session_commands (id, session_id, runtime_id, nas_id, action, target_access_profile, policy_version, status, reason_code, plan_hash, radius_attributes, effective_at, expires_at, created_at, updated_at, response_event_id, response_epoch)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?)`, command.ID, command.SessionID, command.RuntimeID, command.NASID, command.Action, command.TargetAccessProfile, command.PolicyVersion, command.Status, command.ReasonCode, command.PlanHash, attributes, command.EffectiveAt, command.ExpiresAt, command.CreatedAt, now, event.ID, event.RestrictionEpoch).Error
	}
	return nil
}

func expireResponseMutationCommands(tx *gorm.DB, mutation domain.EndpointResponseMutation, now time.Time) error {
	if mutation.Event.RestoredAt != nil {
		if err := expireRestoredResponseCommands(tx, mutation.Event.ID, now); err != nil {
			return err
		}
	}
	if !mutation.CancelPendingCommands {
		return nil
	}
	return tx.Exec(`UPDATE network_nas_session_commands SET status='expired', result_reason_code='endpoint_response_policy_changed', completed_at=?, updated_at=? WHERE tenant_id='default' AND workspace_id='default' AND response_event_id=? AND status='pending'`, now, now, mutation.Event.ID).Error
}

func (r *Repository) EndpointMaintenance(ctx context.Context, filter domain.EndpointResponseFilter, now time.Time) ([]domain.EndpointMaintenance, error) {
	query := `SELECT id, device_id, site_id, reason, actor_id, created_at, expires_at FROM network_endpoint_maintenance_windows WHERE tenant_id='default' AND workspace_id='default' AND ended_at IS NULL AND expires_at > ?`
	args := []any{now}
	query, args = addFilter(query, args, "device_id", filter.DeviceID)
	query, args = addFilter(query, args, "site_id", filter.SiteID)
	query += ` ORDER BY created_at DESC, id LIMIT ? OFFSET ?`
	return queryMany(ctx, r.db, query, append(args, filter.Limit, filter.Offset), scanMaintenance)
}

func scanMaintenance(row scanner) (domain.EndpointMaintenance, error) {
	var item domain.EndpointMaintenance
	err := row.Scan(&item.ID, &item.DeviceID, &item.SiteID, &item.Reason, &item.ActorID, &item.CreatedAt, &item.ExpiresAt)
	return item, err
}

func (r *Repository) SaveEndpointMaintenance(ctx context.Context, item domain.EndpointMaintenance) error {
	return r.db.WithContext(ctx).Exec(`INSERT INTO network_endpoint_maintenance_windows (id, device_id, site_id, reason, actor_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, item.ID, item.DeviceID, item.SiteID, item.Reason, item.ActorID, item.CreatedAt, item.ExpiresAt).Error
}

func (r *Repository) EndEndpointMaintenance(ctx context.Context, id string, now time.Time) (domain.EndpointMaintenance, error) {
	item, err := scanOne(r.db.WithContext(ctx).Raw(`SELECT id, device_id, site_id, reason, actor_id, created_at, expires_at FROM network_endpoint_maintenance_windows WHERE tenant_id='default' AND workspace_id='default' AND id=?`, id).Row(), scanMaintenance, "endpoint maintenance")
	if err != nil {
		return item, err
	}
	err = r.db.WithContext(ctx).Exec(`UPDATE network_endpoint_maintenance_windows SET ended_at=COALESCE(ended_at, ?) WHERE tenant_id='default' AND workspace_id='default' AND id=?`, now, id).Error
	return item, err
}

func optionalResponseEvent(tx *gorm.DB, policyID string, revision int, sessionID string) (*domain.EndpointResponseRecord, error) {
	item, err := scanOne(tx.Raw(responseEventSelect+` AND policy_id=? AND policy_revision=? AND session_id=? AND restored_at IS NULL FOR UPDATE`, policyID, revision, sessionID).Row(), scanResponseEvent, "active endpoint response")
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, nil
	}
	return &item, err
}

func expireRestoredResponseCommands(tx *gorm.DB, eventID string, now time.Time) error {
	var outstanding bool
	if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM network_nas_session_commands WHERE tenant_id = 'default' AND workspace_id = 'default' AND response_event_id = ? AND status = 'delivered' AND expires_at > ?)`, eventID, now).Scan(&outstanding).Error; err != nil {
		return err
	}
	if outstanding {
		return apperrors.NewBusiness(apperrors.ErrConflict, "endpoint_response_command_in_flight", "Wait for the delivered NAS command to finish before restoring access.", "请等待已发送的 NAS 命令完成后再恢复。")
	}
	if err := tx.Exec(`UPDATE network_nas_session_commands SET status='expired', result_reason_code='endpoint_response_restored', completed_at=?, updated_at=? WHERE tenant_id='default' AND workspace_id='default' AND response_event_id=? AND status IN ('pending', 'delivered')`, now, now, eventID).Error; err != nil {
		return err
	}
	return nil
}
