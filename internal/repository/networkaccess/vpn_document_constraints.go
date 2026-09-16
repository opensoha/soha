package networkaccess

import (
	"encoding/json"
	"reflect"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func lockVPNPolicyReference(tx *gorm.DB, raw []byte, publishing bool) error {
	var config domain.VPNProfileConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	var revision int
	err := tx.Raw(`SELECT published_revision FROM network_vpn_documents WHERE id = ? AND kind = 'selection-policy' AND deleted_at IS NULL AND tenant_id = 'default' AND workspace_id = 'default' FOR SHARE`, config.SelectionPolicyID).Row().Scan(&revision)
	if err != nil {
		return apperrors.NewBusiness(apperrors.ErrConflict, "vpn_policy_unavailable", "The referenced selection policy is no longer available.", "引用的选择策略已不可用，请刷新后重试。")
	}
	if publishing && config.Enabled && revision < 1 {
		return apperrors.ErrConflict
	}
	return nil
}

func vpnProfileScopeChanged(previous, next []byte) bool {
	if len(previous) == 0 {
		return false
	}
	var a, b domain.VPNProfileConfig
	if json.Unmarshal(previous, &a) != nil || json.Unmarshal(next, &b) != nil {
		return true
	}
	return a.Enabled != b.Enabled || a.SiteID != b.SiteID || a.NetworkSpaceID != b.NetworkSpaceID || a.Mode != b.Mode || !reflect.DeepEqual(a.ResourceIDs, b.ResourceIDs) || !reflect.DeepEqual(a.Assignments, b.Assignments)
}

func revokeVPNProfileSessions(tx *gorm.DB, id string, now time.Time) error {
	if err := tx.Exec(`UPDATE network_runtime_leases SET status = 'revoked', revoked_at = ?, revoke_reason = 'vpn_profile_changed', updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND status IN ('issued', 'active', 'revoking') AND session_id IN (SELECT id FROM network_runtime_sessions WHERE vpn_profile_id = ? AND tenant_id = 'default' AND workspace_id = 'default')`, now, now, id).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE network_wireguard_peers SET status = 'revoked', revoked_at = ?, revoke_reason = 'vpn_profile_changed', updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND status = 'active' AND session_id IN (SELECT id FROM network_runtime_sessions WHERE vpn_profile_id = ? AND tenant_id = 'default' AND workspace_id = 'default')`, now, now, id).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE network_runtime_sessions SET status = 'revoked', revoked_at = ?, revoke_reason = 'vpn_profile_changed', updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND vpn_profile_id = ? AND status IN ('pending', 'active', 'restricted', 'quarantine')`, now, now, id).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_vpn_connection_intents SET status = 'rejected', token_hash = NULL WHERE tenant_id = 'default' AND workspace_id = 'default' AND profile_id = ? AND status = 'issued'`, id).Error
}
