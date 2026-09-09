package networkaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const nasCommandRedeliveryDelay = 10 * time.Second // ponytail: fixed retry delay; make configurable when measured NAS load needs tuning.

const sessionSelect = `SELECT s.id, s.runtime_id, s.subject_id, s.device_id, COALESCE(s.site_id, ''), COALESCE(s.gateway_id, ''), COALESCE(s.nas_id, ''), s.mode,
    CASE s.mode WHEN 'internal_direct' THEN 'site_direct' WHEN 'internal_ztna' THEN 'wireguard_ztna' WHEN 'external_vpn' THEN 'wireguard' WHEN 'external_vpn_ztna' THEN 'wireguard_ztna' WHEN 'external_direct_ztna' THEN 'wireguard_ztna' ELSE 'deny' END,
    s.access_profile, s.status, s.policy_version,
    COALESCE((SELECT jsonb_agg(l.id ORDER BY l.id) FROM network_runtime_leases l WHERE l.session_id = s.id AND l.lease_kind = 'network'), '[]'::jsonb),
    COALESCE((SELECT jsonb_agg(l.id ORDER BY l.id) FROM network_runtime_leases l WHERE l.session_id = s.id AND l.lease_kind = 'resource'), '[]'::jsonb),
    COALESCE(s.revoke_reason, ''), s.created_at, s.valid_until, s.updated_at
    FROM network_runtime_sessions s WHERE s.tenant_id = 'default' AND s.workspace_id = 'default'`

func (r *Repository) ListSessions(ctx context.Context, filter domainnetworkaccess.SessionFilter) ([]domainnetworkaccess.Session, error) {
	query, args := sessionSelect, []any{}
	query, args = addFilter(query, args, "s.site_id", filter.SiteID)
	query, args = addFilter(query, args, "s.runtime_id", filter.RuntimeID)
	query, args = addFilter(query, args, "s.subject_id", filter.SubjectID)
	query, args = addFilter(query, args, "s.device_id", filter.DeviceID)
	query, args = addFilter(query, args, "s.status", filter.Status)
	query += ` ORDER BY s.created_at DESC, s.id DESC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanSession)
}

func (r *Repository) GetSession(ctx context.Context, id string) (domainnetworkaccess.Session, error) {
	return scanOne(r.db.WithContext(ctx).Raw(sessionSelect+` AND s.id = ? LIMIT 1`, id).Row(), scanSession, "network session")
}

func scanSession(row scanner) (domainnetworkaccess.Session, error) {
	var item domainnetworkaccess.Session
	var networkLeaseIDs, resourceLeaseIDs []byte
	err := row.Scan(&item.ID, &item.RuntimeID, &item.SubjectID, &item.DeviceID, &item.SiteID, &item.GatewayID, &item.NASID, &item.Mode, &item.Path, &item.AccessProfile, &item.Status, &item.PolicyVersion, &networkLeaseIDs, &resourceLeaseIDs, &item.ReasonCode, &item.StartedAt, &item.ExpiresAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal(networkLeaseIDs, &item.NetworkLeaseIDs); err != nil {
		return item, fmt.Errorf("decode network lease IDs: %w", err)
	}
	if err := json.Unmarshal(resourceLeaseIDs, &item.ResourceLeaseIDs); err != nil {
		return item, fmt.Errorf("decode resource lease IDs: %w", err)
	}
	return item, nil
}

func (r *Repository) CreateSessionCommand(ctx context.Context, command domainnetworkaccess.SessionCommand) (domainnetworkaccess.SessionCommand, error) {
	var saved domainnetworkaccess.SessionCommand
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-session-command:"+command.PlanHash).Error; err != nil {
			return err
		}
		existing, err := getSessionCommandByPlan(tx, command.PlanHash)
		if err == nil {
			saved = existing
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		var radiusAttributes any
		if command.RadiusAttributes != nil {
			encoded, err := json.Marshal(command.RadiusAttributes)
			if err != nil {
				return err
			}
			radiusAttributes = string(encoded)
		}
		if err := tx.Exec(`INSERT INTO network_nas_session_commands (id, session_id, runtime_id, nas_id, action, target_access_profile, policy_version, status, reason_code, plan_hash, radius_attributes, effective_at, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?)`, command.ID, command.SessionID, command.RuntimeID, command.NASID, command.Action, command.TargetAccessProfile, command.PolicyVersion, command.Status, command.ReasonCode, command.PlanHash, radiusAttributes, command.EffectiveAt, command.ExpiresAt, command.CreatedAt, command.CreatedAt).Error; err != nil {
			return normalizeDatabaseError(err)
		}
		saved = command
		return nil
	})
	return saved, err
}

func (r *Repository) GetSessionCommand(ctx context.Context, id string) (domainnetworkaccess.SessionCommand, error) {
	return scanSessionCommand(r.db.WithContext(ctx).Raw(sessionCommandSelect+` AND id = ? LIMIT 1`, id).Row())
}

func (r *Repository) ClaimNASSessionCommand(ctx context.Context, runtimeID string, now time.Time) (domainnetworkaccess.SessionCommand, error) {
	var claimed domainnetworkaccess.SessionCommand
	var noCommand error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE network_nas_session_commands SET status = 'expired', result_reason_code = 'command_expired', completed_at = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status IN ('pending', 'delivered') AND expires_at <= ?`, now, now, runtimeID, now).Error; err != nil {
			return err
		}
		command, err := scanSessionCommand(tx.Raw(sessionCommandSelect+` AND runtime_id = ? AND expires_at > ? AND (status = 'pending' OR (status = 'delivered' AND delivered_at <= ?)) ORDER BY created_at ASC, id ASC LIMIT 1 FOR UPDATE SKIP LOCKED`, runtimeID, now, now.Add(-nasCommandRedeliveryDelay)).Row())
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				noCommand = err
				return nil
			}
			return err
		}
		result := tx.Exec(`UPDATE network_nas_session_commands SET status = 'delivered', delivered_at = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND status IN ('pending', 'delivered')`, now, now, command.ID, runtimeID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperrors.NewBusiness(apperrors.ErrConflict, "nas_command_claim_conflict", "The NAS session command could not be claimed.", "NAS 会话命令无法领取。")
		}
		if err := tx.Raw(`SELECT subject_id, device_id FROM network_runtime_sessions WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND nas_id = ?`, command.SessionID, runtimeID, command.NASID).Row().Scan(&command.SubjectID, &command.DeviceID); err != nil {
			return err
		}
		command.Status = domainnetworkaccess.SessionCommandDelivered
		claimed = command
		return nil
	})
	if err == nil && noCommand != nil {
		err = noCommand
	}
	return claimed, err
}

func (r *Repository) CompleteNASSessionCommand(ctx context.Context, runtimeID string, result networkprotocol.NASSessionCommandResult, now time.Time) error {
	var outcome error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		command, err := scanSessionCommand(tx.Raw(sessionCommandSelect+` AND id = ? LIMIT 1 FOR UPDATE`, result.CommandID).Row())
		if err != nil {
			return err
		}
		done, semanticOutcome, err := validateNASCommandCompletion(tx, command, runtimeID, result, now)
		if err != nil {
			return err
		}
		if semanticOutcome != nil {
			outcome = semanticOutcome
		}
		if done {
			return nil
		}
		return recordNASCommandCompletion(tx, command, runtimeID, result, now)
	})
	if err != nil {
		return err
	}
	return outcome
}

func validateNASCommandCompletion(tx *gorm.DB, command domainnetworkaccess.SessionCommand, runtimeID string, result networkprotocol.NASSessionCommandResult, now time.Time) (bool, error, error) {
	if command.RuntimeID != runtimeID || command.SessionID != result.SessionID {
		return true, nil, apperrors.NewBusiness(apperrors.ErrUnauthorized, "nas_command_identity_mismatch", "The NAS session command does not belong to the authenticated runtime.", "NAS 会话命令不属于已认证运行时。")
	}
	if terminalSessionCommandStatus(command.Status) {
		if command.Status == result.Status && command.ReasonCode == result.ReasonCode && command.CompletedAt != nil && command.CompletedAt.Equal(result.CompletedAt) {
			return true, nil, nil
		}
		return true, nil, apperrors.NewBusiness(apperrors.ErrConflict, "nas_command_already_completed", "The NAS session command already has a different terminal result.", "NAS 会话命令已有不同的终态结果。")
	}
	if !now.Before(command.ExpiresAt) {
		if err := tx.Exec(`UPDATE network_nas_session_commands SET status = 'expired', result_reason_code = 'command_expired', completed_at = ?, updated_at = ? WHERE id = ? AND status IN ('pending', 'delivered')`, now, now, command.ID).Error; err != nil {
			return true, nil, err
		}
		return true, apperrors.NewBusiness(apperrors.ErrGone, "nas_command_expired", "The NAS session command has expired.", "NAS 会话命令已过期。"), nil
	}
	if command.Status != domainnetworkaccess.SessionCommandDelivered {
		return true, nil, apperrors.NewBusiness(apperrors.ErrConflict, "nas_command_not_delivered", "The NAS session command was not delivered.", "NAS 会话命令尚未投递。")
	}
	if !validSessionCommandResultStatus(result.Status) || result.CompletedAt.After(command.ExpiresAt) {
		return true, nil, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_nas_command_result", "The NAS session command result is invalid.", "NAS 会话命令结果无效。")
	}
	return false, nil, nil
}

func recordNASCommandCompletion(tx *gorm.DB, command domainnetworkaccess.SessionCommand, runtimeID string, result networkprotocol.NASSessionCommandResult, now time.Time) error {
	updated := tx.Exec(`UPDATE network_nas_session_commands SET status = ?, result_reason_code = ?, completed_at = ?, updated_at = ? WHERE id = ? AND runtime_id = ? AND status = 'delivered'`, result.Status, result.ReasonCode, result.CompletedAt, now, command.ID, runtimeID)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return apperrors.NewBusiness(apperrors.ErrConflict, "nas_command_result_conflict", "The NAS session command result could not be recorded.", "NAS 会话命令结果无法记录。")
	}
	if result.Status != domainnetworkaccess.SessionCommandApplied {
		return nil
	}
	return applyNASSessionCommand(tx, command, runtimeID, result, now)
}

func applyNASSessionCommand(tx *gorm.DB, command domainnetworkaccess.SessionCommand, runtimeID string, result networkprotocol.NASSessionCommandResult, now time.Time) error {
	var updated *gorm.DB
	if command.Action == domainnetworkaccess.SessionActionDisconnect {
		updated = tx.Exec(`UPDATE network_runtime_sessions SET status = 'revoked', revoked_at = ?, revoke_reason = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND nas_id = ?`, result.CompletedAt, result.ReasonCode, now, command.SessionID, runtimeID, command.NASID)
	} else {
		updated = tx.Exec(`UPDATE network_runtime_sessions SET access_profile = ?, status = ?, policy_version = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND nas_id = ?`, command.TargetAccessProfile, sessionStatusForProfile(command.TargetAccessProfile), command.PolicyVersion, now, command.SessionID, runtimeID, command.NASID)
	}
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return apperrors.NewBusiness(apperrors.ErrConflict, "nas_session_update_conflict", "The NAS session could not be updated.", "NAS 会话无法更新。")
	}
	return nil
}

const sessionCommandSelect = `SELECT id, session_id, runtime_id, nas_id, action, target_access_profile, policy_version, status, COALESCE(result_reason_code, reason_code), plan_hash, COALESCE(radius_attributes, 'null'::jsonb), effective_at, expires_at, completed_at, created_at FROM network_nas_session_commands WHERE tenant_id = 'default' AND workspace_id = 'default'`

func getSessionCommandByPlan(db *gorm.DB, planHash string) (domainnetworkaccess.SessionCommand, error) {
	return scanSessionCommand(db.Raw(sessionCommandSelect+` AND plan_hash = ? LIMIT 1`, planHash).Row())
}

func scanSessionCommand(row scanner) (domainnetworkaccess.SessionCommand, error) {
	var item domainnetworkaccess.SessionCommand
	var radiusAttributes []byte
	var completedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.SessionID, &item.RuntimeID, &item.NASID, &item.Action, &item.TargetAccessProfile, &item.PolicyVersion, &item.Status, &item.ReasonCode, &item.PlanHash, &radiusAttributes, &item.EffectiveAt, &item.ExpiresAt, &completedAt, &item.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, fmt.Errorf("%w: network session command not found", apperrors.ErrNotFound)
		}
		return item, err
	}
	if string(radiusAttributes) != "null" {
		var attributes networkprotocol.RadiusAttributes
		if err := json.Unmarshal(radiusAttributes, &attributes); err != nil {
			return item, fmt.Errorf("decode network session command attributes: %w", err)
		}
		item.RadiusAttributes = &attributes
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return item, nil
}

func terminalSessionCommandStatus(status string) bool {
	switch status {
	case domainnetworkaccess.SessionCommandApplied, domainnetworkaccess.SessionCommandRejected,
		domainnetworkaccess.SessionCommandUnsupported, domainnetworkaccess.SessionCommandTimedOut,
		domainnetworkaccess.SessionCommandExpired:
		return true
	default:
		return false
	}
}

func validSessionCommandResultStatus(status string) bool {
	return status == domainnetworkaccess.SessionCommandApplied || status == domainnetworkaccess.SessionCommandRejected ||
		status == domainnetworkaccess.SessionCommandUnsupported || status == domainnetworkaccess.SessionCommandTimedOut
}

func sessionStatusForProfile(profile string) string {
	switch profile {
	case domainnetworkaccess.ProfileFull:
		return "active"
	case domainnetworkaccess.ProfileQuarantine:
		return "quarantine"
	case domainnetworkaccess.ProfileOnboarding:
		return "pending"
	default:
		return "restricted"
	}
}
