package networkruntime

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const vpnIntentColumns = `id, runtime_id, credential_id, subject_id, device_id, auth_session_id, profile_id, profile_revision,
 selection_policy_id, selection_policy_revision, selection, COALESCE(requested_gateway_id, ''), COALESCE(token_hash, ''), status,
 expires_at, created_at, consumed_at, COALESCE(session_id, ''), COALESCE(request_id, ''), COALESCE(request_hash, ''), managed_result`

func scanVPNIntent(row rowScanner) (domain.VPNIntent, error) {
	var item domain.VPNIntent
	var managedResult []byte
	err := row.Scan(&item.ID, &item.RuntimeID, &item.CredentialID, &item.SubjectID, &item.DeviceID, &item.AuthSessionID, &item.ProfileID, &item.ProfileRevision,
		&item.SelectionPolicyID, &item.SelectionPolicyRevision, &item.Selection, &item.RequestedGatewayID, &item.TokenHash, &item.Status, &item.ExpiresAt, &item.CreatedAt, &item.ConsumedAt, &item.SessionID, &item.RequestID, &item.RequestHash, &managedResult)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	item.ManagedResult = managedResult
	return item, err
}

func (r *Repository) CreateVPNIntent(ctx context.Context, item domain.VPNIntent) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-vpn-intent:"+item.RuntimeID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE network_vpn_connection_intents SET status = 'expired', token_hash = NULL WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status = 'issued' AND expires_at <= ?`, item.RuntimeID, item.CreatedAt).Error; err != nil {
			return err
		}
		var count int
		if err := tx.Raw(`SELECT count(*) FROM network_vpn_connection_intents WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status = 'issued'`, item.RuntimeID).Row().Scan(&count); err != nil {
			return err
		}
		if count >= 32 {
			return apperrors.NewBusiness(apperrors.ErrConflict, "vpn_intent_limit", "Too many pending VPN connection requests.", "待处理的 VPN 连接请求过多，请稍后重试。")
		}
		if err := activeVPNAuthSession(tx, item.AuthSessionID, item.SubjectID, item.CreatedAt); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO network_vpn_connection_intents (id, runtime_id, credential_id, subject_id, device_id, auth_session_id,
			profile_id, profile_revision, selection_policy_id, selection_policy_revision, selection, requested_gateway_id, token_hash, status, expires_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, 'issued', ?, ?)`, item.ID, item.RuntimeID, item.CredentialID, item.SubjectID, item.DeviceID, item.AuthSessionID,
			item.ProfileID, item.ProfileRevision, item.SelectionPolicyID, item.SelectionPolicyRevision, item.Selection, item.RequestedGatewayID, item.TokenHash, item.ExpiresAt, item.CreatedAt).Error
	})
}

func (r *Repository) GetVPNIntent(ctx context.Context, id string) (domain.VPNIntent, error) {
	return scanVPNIntent(r.db.WithContext(ctx).Raw("SELECT "+vpnIntentColumns+" FROM network_vpn_connection_intents WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?", id).Row())
}

func (r *Repository) RejectVPNIntent(ctx context.Context, id, runtimeID string, now time.Time) error {
	return r.db.WithContext(ctx).Exec(`UPDATE network_vpn_connection_intents SET status = 'rejected', token_hash = NULL WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND status = 'issued'`, id, runtimeID).Error
}

func (r *Repository) VPNAuthSessionActive(ctx context.Context, sessionID, subjectID string, now time.Time) error {
	return activeVPNAuthSession(r.db.WithContext(ctx), sessionID, subjectID, now)
}

func activeVPNAuthSession(tx *gorm.DB, sessionID, subjectID string, now time.Time) error {
	var id string
	err := tx.Raw(`SELECT id FROM sessions WHERE id = ? AND user_id::text = ? AND status = 'active' AND expires_at > ? FOR SHARE`, sessionID, subjectID, now).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return invalidVPNIntent()
	}
	return err
}

func validVPNIntent(item domain.VPNIntent, runtimeID, credentialID, subjectID, deviceID, hash string, now time.Time) bool {
	return item.Status == "issued" && item.RuntimeID == runtimeID && item.CredentialID == credentialID && item.SubjectID == subjectID && item.DeviceID == deviceID && item.ExpiresAt.After(now) && len(hash) == 71 && subtle.ConstantTimeCompare([]byte(item.TokenHash), []byte(hash)) == 1
}

func invalidVPNIntent() error {
	return apperrors.NewBusiness(apperrors.ErrAccessDenied, "vpn_intent_invalid", "The VPN connection request expired, changed, or is no longer authorized.", "VPN 连接请求已过期、发生变化或不再被授权，请重新连接。")
}
