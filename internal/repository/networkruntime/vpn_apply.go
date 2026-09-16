package networkruntime

import (
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
	"gorm.io/gorm"
)

func recordManagedVPNApply(tx *gorm.DB, runtimeID string, applied networkprotocol.ConfigurationApplied, now time.Time) error {
	if applied.Status != "applied" {
		return tx.Exec(`UPDATE network_vpn_decisions d SET state='failed', updated_at=?, payload=jsonb_set(d.payload,'{reasonCode}',to_jsonb(?::text))
 FROM network_runtime_sessions s WHERE d.session_id=s.id AND s.runtime_id=? AND s.configuration_version=?
 AND d.tenant_id='default' AND d.workspace_id='default' AND d.state='connecting'`, now, "configuration_apply_failed", runtimeID, applied.ConfigurationVersion).Error
	}
	// Both endpoint and gateway must acknowledge a live configuration containing
	// this endpoint's lease/peer. Issuing a session alone is not a successful tunnel.
	return tx.Exec(`UPDATE network_vpn_decisions d SET state='connected', established_at=?, updated_at=?
 FROM network_runtime_sessions s JOIN network_access_gateways g ON g.id=s.gateway_id
 WHERE d.session_id=s.id AND d.tenant_id='default' AND d.workspace_id='default' AND d.state='connecting'
 AND s.status IN ('active','restricted','quarantine') AND s.valid_until>?
 AND (s.runtime_id=? OR g.runtime_id=?)
 AND EXISTS (SELECT 1 FROM network_runtime_configurations e WHERE e.runtime_id=s.runtime_id
   AND e.configuration_version=(SELECT max(configuration_version) FROM network_runtime_configurations WHERE runtime_id=s.runtime_id)
   AND e.apply_status='applied' AND e.readback_hash IS NOT NULL AND e.valid_until>?
   AND EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(e.desired_payload->'networkLeases','[]'::jsonb)||COALESCE(e.desired_payload->'resourceLeases','[]'::jsonb)) l WHERE l->>'sessionId'=s.id))
 AND EXISTS (SELECT 1 FROM network_runtime_configurations c WHERE c.runtime_id=g.runtime_id
   AND c.configuration_version=(SELECT max(configuration_version) FROM network_runtime_configurations WHERE runtime_id=g.runtime_id)
   AND c.apply_status='applied' AND c.readback_hash IS NOT NULL AND c.valid_until>?
   AND EXISTS (SELECT 1 FROM jsonb_array_elements(c.desired_payload->'wireguard'->'peers') p WHERE p->>'runtimeId'=s.runtime_id))`, now, now, now, runtimeID, runtimeID, now, now).Error
}
