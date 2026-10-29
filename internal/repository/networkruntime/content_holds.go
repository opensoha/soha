package networkruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	reponetwork "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func subjectDLPRestricted(tx *gorm.DB, subject string) (bool, error) {
	var active bool
	err := tx.Raw(`SELECT network_security_subject_restricted(?)`, subject).Scan(&active).Error
	return active, err
}
func ensureSubjectNotDLPRestricted(tx *gorm.DB, subject string) error {
	active, err := subjectDLPRestricted(tx, subject)
	if err != nil {
		return err
	}
	if active {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "network_security_restricted", "Network access is restricted by a DLP event; review and fresh authorization are required.", "DLP 事件限制了网络访问，需要审查并重新授权。")
	}
	return nil
}

// The composition root acquires WG -> response-policy -> network-policy locks
// before this operation, sharing the issuance and NAS command serialization.
func (r *Repository) RestrictContentSubject(ctx context.Context, event domain.Event) (string, error) {
	tx := dbtx.DB(ctx, r.db)
	var existing string
	err := tx.Raw(`SELECT id FROM network_security_holds WHERE subject_id=? AND project_id=? AND active FOR UPDATE`, event.ActorID, event.ProjectID).Row().Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err := tx.Exec(`INSERT INTO network_security_holds(id,source_event_id,source_type,project_id,subject_id,active,revision,created_at) VALUES(?,?,'content_dlp',?,?,true,1,?)`, event.ID, event.ID, event.ProjectID, event.ActorID, event.CreatedAt).Error; err != nil {
		return "", err
	}
	if err := revokeContentVPN(tx, domain.NetworkCause{ID: event.ID, ActorID: event.ActorID, CreatedAt: event.CreatedAt}); err != nil {
		return "", err
	}
	if err := restrictContentNAS(tx, domain.NetworkCause{ID: event.ID, ActorID: event.ActorID, CreatedAt: event.CreatedAt}); err != nil {
		return "", err
	}
	if err := restrictContentProxy(tx, domain.NetworkCause{ID: event.ID, ActorID: event.ActorID, CreatedAt: event.CreatedAt}); err != nil {
		return "", err
	}
	return event.ID, nil
}
func addHoldTarget(tx *gorm.DB, hold, kind, runtime, session, command string, base int64, state string) error {
	return tx.Exec(`INSERT INTO network_security_hold_targets(hold_id,kind,runtime_id,session_id,command_id,base_version,state) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, hold, kind, runtime, session, command, base, state).Error
}
func revokeContentVPN(tx *gorm.DB, event domain.NetworkCause) error {
	var runtimes []string
	if err := tx.Raw(`SELECT DISTINCT id FROM (
 SELECT c.runtime_id AS id FROM network_runtime_credentials c WHERE c.tenant_id='default' AND c.workspace_id='default' AND c.subject_id=? AND c.runtime_kind='endpoint' AND c.status='active'
 UNION SELECT s.runtime_id FROM network_runtime_sessions s WHERE s.tenant_id='default' AND s.workspace_id='default' AND s.subject_id=? AND COALESCE(s.nas_id,'')='' AND s.status IN ('pending','active','restricted','quarantine')
 UNION SELECT g.runtime_id FROM network_wireguard_peers p JOIN network_runtime_sessions s ON s.id=p.session_id JOIN network_access_gateways g ON g.id=p.gateway_id WHERE s.tenant_id='default' AND s.workspace_id='default' AND s.subject_id=? AND p.status='active'
 ) r WHERE id<>'' ORDER BY id LIMIT 1025`, event.ActorID, event.ActorID, event.ActorID).Scan(&runtimes).Error; err != nil {
		return err
	}
	if len(runtimes) > 1024 {
		return apperrors.ErrServiceUnavailable
	}
	for _, id := range runtimes {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+id).Error; err != nil {
			return err
		}
		var base int64
		if err := tx.Raw(`SELECT COALESCE(MAX(configuration_version),0) FROM network_runtime_configurations WHERE tenant_id='default' AND workspace_id='default' AND runtime_id=?`, id).Scan(&base).Error; err != nil {
			return err
		}
		if err := addHoldTarget(tx, event.ID, "configuration", id, "", "", base, "pending"); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE network_runtime_configurations SET valid_until=LEAST(valid_until,?) WHERE tenant_id='default' AND workspace_id='default' AND runtime_id=?`, event.CreatedAt, id).Error; err != nil {
			return err
		}
	}
	for _, table := range []string{"network_runtime_leases", "network_wireguard_peers"} {
		if err := tx.Table(table).Where(`tenant_id='default' AND workspace_id='default' AND session_id IN (SELECT id FROM network_runtime_sessions WHERE tenant_id='default' AND workspace_id='default' AND subject_id=? AND COALESCE(nas_id,'')='') AND status IN ('issued','active','revoking')`, event.ActorID).Updates(map[string]any{"status": "revoked", "revoked_at": event.CreatedAt, "revoke_reason": "content_dlp", "updated_at": event.CreatedAt}).Error; err != nil {
			return err
		}
	}
	if err := tx.Exec(`UPDATE network_runtime_sessions SET status='revoked',revoked_at=?,revoke_reason='content_dlp',updated_at=? WHERE tenant_id='default' AND workspace_id='default' AND subject_id=? AND COALESCE(nas_id,'')='' AND status IN ('pending','active','restricted','quarantine')`, event.CreatedAt, event.CreatedAt, event.ActorID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE network_access_grants SET status='revoked',token_hash=NULL,revoked_at=?,reason_code='content_dlp',updated_at=? WHERE tenant_id='default' AND workspace_id='default' AND subject_id=? AND status='issued'`, event.CreatedAt, event.CreatedAt, event.ActorID).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_vpn_connection_intents SET status='rejected',token_hash=NULL WHERE tenant_id='default' AND workspace_id='default' AND subject_id=? AND status='issued'`, event.ActorID).Error
}
func restrictContentNAS(tx *gorm.DB, event domain.NetworkCause) error {
	var sessions []struct {
		ID, RuntimeID, NASID, CredentialID, DeviceID string
		PolicyVersion                                int
		Actionable                                   bool
	}
	err := tx.Raw(`SELECT s.id,s.runtime_id,s.nas_id,s.policy_version,COALESCE(a.credential_id,'') AS credential_id,s.device_id,
 COALESCE(b.disconnect_supported AND b.status='active' AND e.state='active' AND e.stopped_at IS NULL AND e.observed_at>? AND e.received_at>? AND e.observed_at<=? AND e.received_at<=? AND e.runtime_id=s.runtime_id AND e.nas_id=s.nas_id AND e.accounting_epoch>=COALESCE(ep.observed_at,e.accounting_epoch) AND e.subject_id=s.subject_id AND e.device_id=s.device_id AND e.credential_id=a.credential_id AND e.station_id=a.station_id AND a.authentication_method=?,false) AS actionable
 FROM network_runtime_sessions s LEFT JOIN network_access_nas_bindings b ON b.runtime_id=s.runtime_id AND b.nas_id=s.nas_id AND b.site_id=s.site_id AND b.tenant_id=s.tenant_id AND b.workspace_id=s.workspace_id
 LEFT JOIN network_nas_session_evidence e ON e.session_id=s.id AND e.tenant_id=s.tenant_id AND e.workspace_id=s.workspace_id
 LEFT JOIN network_nas_accounting_epochs ep ON ep.runtime_id=s.runtime_id AND ep.nas_id=s.nas_id AND ep.tenant_id=s.tenant_id AND ep.workspace_id=s.workspace_id
 LEFT JOIN network_nas_authorizations a ON a.session_id=s.id AND a.tenant_id=s.tenant_id AND a.workspace_id=s.workspace_id
 WHERE s.tenant_id='default' AND s.workspace_id='default' AND s.subject_id=? AND COALESCE(s.nas_id,'')<>'' AND s.status NOT IN ('revoked','expired') ORDER BY s.id LIMIT 1025`, event.CreatedAt.Add(-2*time.Minute), event.CreatedAt.Add(-2*time.Minute), event.CreatedAt.Add(30*time.Second), event.CreatedAt, networkprotocol.NASAuthenticationEAPTLS, event.ActorID).Scan(&sessions).Error
	if err != nil {
		return err
	}
	if len(sessions) > 1024 {
		return apperrors.ErrServiceUnavailable
	}
	if err := tx.Exec(`UPDATE network_nas_session_commands SET status='expired',completed_at=?,updated_at=?,result_reason_code='content_dlp' WHERE action<>'disconnect' AND status IN ('pending','delivered') AND session_id IN (SELECT id FROM network_runtime_sessions WHERE subject_id=? AND tenant_id='default' AND workspace_id='default')`, event.CreatedAt, event.CreatedAt, event.ActorID).Error; err != nil {
		return err
	}
	for _, s := range sessions {
		if err := tx.Exec(`UPDATE network_runtime_sessions SET admission_floor_profile='deny',admission_restriction_reason='content_dlp',updated_at=? WHERE id=? AND tenant_id='default' AND workspace_id='default'`, event.CreatedAt, s.ID).Error; err != nil {
			return err
		}
		command, state := "", "enforcement_unknown"
		if s.Actionable {
			current, err := reponetwork.New(tx).EndpointCredentialCurrent(tx.Statement.Context, s.CredentialID, s.DeviceID, event.ActorID, event.CreatedAt)
			if err != nil {
				return err
			}
			s.Actionable = current
		}
		if s.Actionable {
			command = uuid.NewString()
			state = "pending"
			err = tx.Exec(`INSERT INTO network_nas_session_commands(id,session_id,runtime_id,nas_id,action,target_access_profile,policy_version,status,reason_code,plan_hash,effective_at,expires_at,created_at,updated_at) VALUES(?,?,?,?,'disconnect','deny',?,'pending','content_dlp',?,?,?,?,?)`, command, s.ID, s.RuntimeID, s.NASID, s.PolicyVersion, contentCommandHash(event.ID, s.ID), event.CreatedAt, event.CreatedAt.Add(2*time.Minute), event.CreatedAt, event.CreatedAt).Error
			if err != nil {
				return err
			}
		}
		if err := addHoldTarget(tx, event.ID, "nas", s.RuntimeID, s.ID, command, 0, state); err != nil {
			return err
		}
	}
	return nil
}
func restrictContentProxy(tx *gorm.DB, event domain.NetworkCause) error {
	var proxies []struct {
		ID              string
		DesiredRevision int64
	}
	if err := tx.Raw(`SELECT p.id,p.desired_revision FROM network_proxy_instances p JOIN network_access_devices d ON d.id=p.binding->>'deviceId' AND d.tenant_id=p.tenant_id AND d.workspace_id=p.workspace_id WHERE p.tenant_id='default' AND p.workspace_id='default' AND d.owner_user_id=? ORDER BY p.id LIMIT 1025 FOR UPDATE OF p`, event.ActorID).Scan(&proxies).Error; err != nil {
		return err
	}
	if len(proxies) > 1024 {
		return apperrors.ErrServiceUnavailable
	}
	for _, p := range proxies {
		if err := addHoldTarget(tx, event.ID, "proxy", p.ID, "", "", p.DesiredRevision, "pending"); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE network_proxy_instances SET security_restricted=true,desired_revision=desired_revision+1,reason_code='content_dlp',updated_at=? WHERE id=? AND security_restricted=false`, event.CreatedAt, p.ID).Error; err != nil {
			return err
		}
	}
	return addHoldTarget(tx, event.ID, "unattributed_proxy", "", "", "", 0, "unsupported")
}
func (r *Repository) ReleaseContentSubject(ctx context.Context, eventID, reviewer string) error {
	tx := dbtx.DB(ctx, r.db)
	now := time.Now().UTC()
	var actor string
	// A coalesced hold represents every linked matched denial. Approving only
	// its oldest event cannot clear newer, unresolved or rejected findings.
	err := tx.Raw(`UPDATE network_security_holds h SET active=false,revision=h.revision+1,released_at=?,released_by=? WHERE h.id=? AND h.source_type='content_dlp' AND h.active AND NOT EXISTS (
 SELECT 1 FROM docker_volume_dlp_events e LEFT JOIN docker_volume_dlp_appeals a ON a.event_id=e.id
 WHERE e.action='transfer' AND e.metadata->>'networkHoldId'=h.id AND COALESCE(a.status,'pending')<>'approved'
 ) RETURNING h.subject_id`, now, reviewer, eventID).Row().Scan(&actor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return releaseContentEffects(tx, eventID, actor, now)
}
func releaseContentEffects(tx *gorm.DB, eventID, actor string, now time.Time) error {
	if err := tx.Exec(`UPDATE network_nas_session_commands SET status='expired',completed_at=?,updated_at=?,result_reason_code='content_dlp_released_reauthenticate' WHERE id IN(SELECT command_id FROM network_security_hold_targets WHERE hold_id=?) AND status IN ('pending','delivered')`, now, now, eventID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE network_runtime_configurations SET valid_until=LEAST(valid_until,?) WHERE runtime_id IN(SELECT runtime_id FROM network_security_hold_targets WHERE hold_id=? AND kind='configuration') AND tenant_id='default' AND workspace_id='default'`, now, eventID).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_proxy_instances p SET security_restricted=network_security_subject_restricted(d.owner_user_id::text),desired_revision=desired_revision+1,updated_at=? FROM network_access_devices d WHERE p.binding->>'deviceId'=d.id AND d.owner_user_id=? AND p.tenant_id=d.tenant_id AND p.workspace_id=d.workspace_id AND p.tenant_id='default' AND p.workspace_id='default' AND p.security_restricted<>network_security_subject_restricted(d.owner_user_id::text)`, now, actor).Error
}
func (r *Repository) ContentSubjectState(ctx context.Context, id string) (domain.NetworkHold, error) {
	tx := dbtx.DB(ctx, r.db)
	var h domain.NetworkHold
	err := tx.Raw(`SELECT id,COALESCE(source_event_id,source_transfer_id),COALESCE(project_id,''),subject_id,active,revision,created_at,released_at FROM network_security_holds WHERE id=?`, id).Row().Scan(&h.ID, &h.SourceEventID, &h.ProjectID, &h.SubjectID, &h.Active, &h.Revision, &h.CreatedAt, &h.ReleasedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return h, apperrors.ErrNotFound
	}
	if err != nil {
		return h, err
	}
	h.Targets = []domain.NetworkTarget{}
	err = tx.Raw(`SELECT t.kind,t.runtime_id,t.session_id,t.command_id,t.base_version,
 COALESCE(CASE WHEN t.kind='configuration' THEN cfg.configuration_version WHEN t.kind='proxy' THEN p.observed_revision ELSE 0 END,0) AS observed_version,
 CASE WHEN t.kind='configuration' THEN CASE WHEN cfg.configuration_version>t.base_version AND cfg.apply_status='applied' THEN 'configuration_applied' ELSE 'pending' END
 WHEN t.kind='proxy' THEN CASE WHEN p.security_restricted AND p.observed_revision=p.desired_revision AND p.desired_revision>t.base_version THEN 'configuration_applied' ELSE 'pending' END
 WHEN t.kind='nas' AND t.command_id<>'' THEN CASE WHEN c.status='applied' AND e.state='stopped' AND e.stopped_at>=? THEN 'verified_stopped' WHEN c.status='applied' THEN 'applied_unverified' ELSE COALESCE(c.status,'enforcement_unknown') END ELSE t.state END AS status
 FROM network_security_hold_targets t LEFT JOIN LATERAL(SELECT configuration_version,apply_status FROM network_runtime_configurations WHERE runtime_id=t.runtime_id AND tenant_id='default' AND workspace_id='default' ORDER BY configuration_version DESC LIMIT 1) cfg ON t.kind='configuration'
 LEFT JOIN network_proxy_instances p ON p.id=t.runtime_id AND t.kind='proxy' AND p.tenant_id='default' AND p.workspace_id='default'
 LEFT JOIN network_nas_session_commands c ON c.id=t.command_id LEFT JOIN network_nas_session_evidence e ON e.session_id=t.session_id AND e.tenant_id='default' AND e.workspace_id='default'
 WHERE t.hold_id=? ORDER BY t.kind,t.runtime_id,t.session_id LIMIT 4096`, h.CreatedAt, id).Scan(&h.Targets).Error
	return h, err
}

func contentCommandHash(event, session string) string {
	sum := sha256.Sum256([]byte("content_dlp\x00" + event + "\x00" + session))
	return "sha256:" + hex.EncodeToString(sum[:])
}
