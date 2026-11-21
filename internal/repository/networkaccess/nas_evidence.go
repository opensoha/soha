package networkaccess

import (
	"context"
	"database/sql"
	"errors"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const nasEvidenceColumns = `session_id, runtime_id, nas_id, credential_id, subject_id, device_id, station_id,
    accounting_session_id, state, started_at, stopped_at, observed_at, received_at, last_status,
    last_session_seconds, COALESCE(observed_vlan_id, 0), COALESCE(observed_filter_id, ''), accounting_epoch, COALESCE(nas_port_id, ''), input_octets, output_octets`

func scanNASObservation(row scanner) (domain.NASSessionObservation, error) {
	var item domain.NASSessionObservation
	err := row.Scan(&item.SessionID, &item.RuntimeID, &item.NasID, &item.CredentialID, &item.SubjectID, &item.DeviceID, &item.StationID,
		&item.AccountingSessionID, &item.State, &item.StartedAt, &item.StoppedAt, &item.LastObservedAt, &item.LastReceivedAt,
		&item.LastStatus, &item.LastSessionSeconds, &item.VLANID, &item.FilterID, &item.AccountingEpoch, &item.NASPortID, &item.InputOctets, &item.OutputOctets)
	item.VerificationState = sohaapi.NetworkNASSessionEvidenceVerificationStateObserved
	return item, err
}

func (r *Repository) NASSessionObservation(ctx context.Context, sessionID string) (domain.NASSessionObservation, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+nasEvidenceColumns+` FROM network_nas_session_evidence WHERE tenant_id = 'default' AND workspace_id = 'default' AND session_id = ?`, sessionID).Row(), scanNASObservation, "NAS session evidence")
}

func (r *Repository) LatestAccessNetworkNASSessions(ctx context.Context, device domain.Device, networkIDs []string) (map[string]domain.ConnectionNASSession, error) {
	var rows []struct {
		AccessNetworkID string
		State           string
		LastReceivedAt  *time.Time
	}
	// Select the latest authorization before interpreting accounting. An older
	// active receipt must not conceal a newer authorization awaiting its Start.
	err := r.db.WithContext(ctx).Raw(`
SELECT DISTINCT ON (a.access_network_id) a.access_network_id,
       CASE WHEN s.status IN ('revoked', 'expired') OR s.valid_until <= CURRENT_TIMESTAMP
                 OR e.stopped_at IS NOT NULL OR b.status IS DISTINCT FROM 'active' THEN 'ended'
            WHEN e.session_id IS NULL THEN 'pending'
            WHEN e.state = 'active' AND e.started_at IS NOT NULL THEN 'observed'
            ELSE 'unknown' END AS state,
       e.received_at AS last_received_at
FROM network_nas_authorizations a
JOIN network_runtime_sessions s ON s.id = a.session_id AND s.tenant_id = a.tenant_id AND s.workspace_id = a.workspace_id
    AND s.subject_id = a.subject_id AND s.device_id = a.device_id AND s.runtime_id = a.runtime_id AND s.nas_id = a.nas_id
JOIN network_access_devices d ON d.id = a.device_id AND d.tenant_id = a.tenant_id AND d.workspace_id = a.workspace_id
    AND d.owner_user_id::text = a.subject_id AND d.site_id = s.site_id AND d.status = 'active'
LEFT JOIN network_access_nas_bindings b ON b.id = a.admission_binding_id AND b.tenant_id = a.tenant_id AND b.workspace_id = a.workspace_id
    AND b.site_id = s.site_id AND b.nas_id = a.nas_id
LEFT JOIN network_nas_session_evidence e ON e.session_id = a.session_id AND e.tenant_id = a.tenant_id AND e.workspace_id = a.workspace_id
    AND e.subject_id = a.subject_id AND e.device_id = a.device_id AND e.runtime_id = a.runtime_id AND e.nas_id = a.nas_id
    AND e.credential_id = a.credential_id AND e.station_id = a.station_id
WHERE a.tenant_id = 'default' AND a.workspace_id = 'default' AND a.subject_id = ? AND a.device_id = ?
    AND s.site_id = ? AND a.access_network_id IN (?) AND a.access_network_id <> ''
    AND a.authentication_method = 'eap-tls' AND a.decision = 'allow'
ORDER BY a.access_network_id, a.created_at DESC, a.session_id DESC`, device.OwnerUserID, device.ID, device.SiteID, networkIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]domain.ConnectionNASSession, len(rows))
	for _, row := range rows {
		result[row.AccessNetworkID] = domain.ConnectionNASSession{State: row.State, LastReceivedAt: row.LastReceivedAt}
	}
	return result, nil
}

func (r *Repository) RecordNASAccounting(ctx context.Context, runtimeID string, request networkprotocol.RadiusAccountingRequest, receivedAt time.Time, skew time.Duration) error {
	observedAt := time.Unix(request.EventTimestamp, 0).UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-nas-accounting:"+runtimeID+":"+request.NASID).Error; err != nil {
			return err
		}
		binding, err := New(tx).FindActiveNASBinding(ctx, runtimeID, request.NASID)
		if err != nil || binding.ID == "" {
			return accountingBindingError()
		}
		if request.StatusType == "accounting-on" || request.StatusType == "accounting-off" {
			return recordNASReset(tx, runtimeID, request.NASID, request.StatusType, observedAt, receivedAt)
		}
		authorization, err := accountingAuthorization(tx, runtimeID, binding.ID, request, observedAt, skew)
		if err != nil {
			return err
		}
		current, err := scanNASObservation(tx.Raw(`SELECT `+nasEvidenceColumns+` FROM network_nas_session_evidence WHERE tenant_id = 'default' AND workspace_id = 'default' AND session_id = ? FOR UPDATE`, request.SessionID).Row())
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		ignore, err := ignoreNASAccounting(current, authorization, request, observedAt)
		if err != nil || ignore {
			return err
		}
		incoming, record := nextNASAccountingObservation(authorization, current, request, observedAt, receivedAt)
		if !record {
			return persistSameSecondCounters(tx, current, request, observedAt)
		}
		if err := applyNASAccountingEpoch(tx, &incoming, current); err != nil {
			return err
		}
		return persistNASAccounting(tx, incoming, receivedAt)
	})
}

func accountingAuthorization(tx *gorm.DB, runtimeID, bindingID string, request networkprotocol.RadiusAccountingRequest, observedAt time.Time, skew time.Duration) (domain.NASSessionObservation, error) {
	item := domain.NASSessionObservation{NASSessionEvidence: domain.NASSessionEvidence{SessionID: request.SessionID, RuntimeID: runtimeID, NasID: request.NASID}}
	var createdAt time.Time
	var method, outerIdentity string
	if err := tx.Raw(`SELECT a.subject_id, a.device_id, COALESCE(a.station_id, ''), COALESCE(a.credential_id, ''), s.created_at, a.authentication_method, a.outer_identity
  FROM network_nas_authorizations a JOIN network_runtime_sessions s ON s.id = a.session_id
  JOIN network_access_nas_bindings b ON b.id = ? AND b.nas_id = a.nas_id AND b.site_id = s.site_id
  WHERE a.tenant_id = 'default' AND a.workspace_id = 'default' AND a.session_id = ? AND a.runtime_id = ? AND a.nas_id = ?
   AND a.decision = 'allow' AND (a.admission_binding_id='' OR a.admission_binding_id=b.id) AND b.tenant_id = 'default' AND b.workspace_id = 'default' AND b.status = 'active'`, bindingID, request.SessionID, runtimeID, request.NASID).Row().Scan(&item.SubjectID, &item.DeviceID, &item.StationID, &item.CredentialID, &createdAt, &method, &outerIdentity); err != nil {
		return item, accountingBindingError()
	}
	identityMatches := item.SubjectID == request.UserID || method == networkprotocol.NASAuthenticationTTLS && outerIdentity != "" && request.UserID == outerIdentity
	if request.SessionID == "" || item.StationID == "" || !identityMatches || item.StationID != domain.CanonicalStationID(request.DeviceID) || observedAt.Before(createdAt.Add(-skew)) {
		return item, accountingBindingError()
	}
	return item, nil
}

func ignoreNASAccounting(current, authorization domain.NASSessionObservation, request networkprotocol.RadiusAccountingRequest, observedAt time.Time) (bool, error) {
	if current.SessionID != "" && (current.AccountingSessionID != request.AccountingSessionID || current.RuntimeID != authorization.RuntimeID || current.NasID != authorization.NasID || current.SubjectID != authorization.SubjectID || current.DeviceID != authorization.DeviceID || current.StationID != authorization.StationID) {
		return false, accountingBindingError()
	}
	if current.StoppedAt != nil {
		return true, nil
	} // Stop is terminal; late packets cannot refresh liveness.
	return request.StatusType == "stop" && current.LastObservedAt != nil && observedAt.Before(*current.LastObservedAt), nil
}

func nextNASAccountingObservation(authorization, current domain.NASSessionObservation, request networkprotocol.RadiusAccountingRequest, observedAt, receivedAt time.Time) (domain.NASSessionObservation, bool) {
	incoming := authorization
	incoming.AccountingSessionID, incoming.State = request.AccountingSessionID, sohaapi.NetworkNASSessionEvidenceStateUnknown
	incoming.LastObservedAt, incoming.LastReceivedAt = &observedAt, &receivedAt
	incoming.StartedAt, incoming.StoppedAt = current.StartedAt, current.StoppedAt
	incoming.LastStatus, incoming.LastSessionSeconds = request.StatusType, request.SessionTimeSeconds
	incoming.InputOctets = max(current.InputOctets, request.InputOctets)
	incoming.OutputOctets = max(current.OutputOctets, request.OutputOctets)
	incoming.VLANID, incoming.FilterID, incoming.NASPortID = request.ObservedVLANID, request.ObservedFilterID, request.NASPortID
	if request.StatusType == "start" && incoming.StartedAt == nil {
		incoming.StartedAt = &observedAt
	}
	if request.StatusType == "stop" {
		incoming.StoppedAt, incoming.State = &observedAt, sohaapi.NetworkNASSessionEvidenceStateStopped
	} else if current.LastObservedAt != nil && !observedAt.After(*current.LastObservedAt) {
		if request.StatusType != "start" || current.StartedAt != nil {
			return incoming, false
		}
		incoming.LastObservedAt, incoming.LastReceivedAt = current.LastObservedAt, current.LastReceivedAt
		incoming.LastSessionSeconds, incoming.LastStatus = current.LastSessionSeconds, current.LastStatus
		incoming.InputOctets, incoming.OutputOctets = current.InputOctets, current.OutputOctets
		incoming.VLANID, incoming.FilterID, incoming.NASPortID = current.VLANID, current.FilterID, current.NASPortID
	}
	if incoming.StartedAt != nil && incoming.StoppedAt == nil && current.LastStatus != "accounting_regressed" {
		incoming.State = sohaapi.NetworkNASSessionEvidenceStateActive
	}
	if current.LastStatus == "accounting_regressed" && incoming.StoppedAt == nil {
		incoming.LastStatus = current.LastStatus
	}
	if current.LastObservedAt != nil && observedAt.After(*current.LastObservedAt) && request.StatusType != "stop" && request.SessionTimeSeconds < current.LastSessionSeconds {
		incoming.State, incoming.LastStatus = sohaapi.NetworkNASSessionEvidenceStateUnknown, "accounting_regressed"
	}
	return incoming, true
}

func persistSameSecondCounters(tx *gorm.DB, current domain.NASSessionObservation, request networkprotocol.RadiusAccountingRequest, observedAt time.Time) error {
	if current.LastObservedAt == nil || !observedAt.Equal(*current.LastObservedAt) ||
		request.InputOctets <= current.InputOctets && request.OutputOctets <= current.OutputOctets {
		return nil
	}
	return tx.Exec(`UPDATE network_nas_session_evidence SET input_octets=GREATEST(input_octets, ?), output_octets=GREATEST(output_octets, ?)
		WHERE tenant_id='default' AND workspace_id='default' AND session_id=?`, request.InputOctets, request.OutputOctets, current.SessionID).Error
}

func applyNASAccountingEpoch(tx *gorm.DB, incoming *domain.NASSessionObservation, current domain.NASSessionObservation) error {
	var resetAt *time.Time
	if err := tx.Raw(`SELECT observed_at FROM network_nas_accounting_epochs WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND nas_id = ?`, incoming.RuntimeID, incoming.NasID).Row().Scan(&resetAt); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if resetAt != nil && incoming.StartedAt != nil && !incoming.StartedAt.After(*resetAt) && incoming.StoppedAt == nil {
		incoming.State, incoming.LastStatus = sohaapi.NetworkNASSessionEvidenceStateUnknown, "nas_accounting_restarted"
	}
	incoming.AccountingEpoch = time.Unix(0, 0).UTC()
	if resetAt != nil {
		incoming.AccountingEpoch = *resetAt
	}
	if current.SessionID != "" {
		incoming.AccountingEpoch = current.AccountingEpoch
	}
	return nil
}

func persistNASAccounting(tx *gorm.DB, incoming domain.NASSessionObservation, receivedAt time.Time) error {
	if err := tx.Exec(`INSERT INTO network_nas_session_evidence
            (session_id, runtime_id, nas_id, credential_id, subject_id, device_id, station_id,
             accounting_session_id, state, started_at, stopped_at, observed_at, received_at, last_status,
             last_session_seconds, observed_vlan_id, observed_filter_id, accounting_epoch, nas_port_id, input_octets, output_octets)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, 0), NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?)
            ON CONFLICT (tenant_id, workspace_id, session_id) DO UPDATE SET
             state=EXCLUDED.state, started_at=EXCLUDED.started_at, stopped_at=EXCLUDED.stopped_at,
             observed_at=EXCLUDED.observed_at, received_at=EXCLUDED.received_at, last_status=EXCLUDED.last_status,
             last_session_seconds=EXCLUDED.last_session_seconds, input_octets=EXCLUDED.input_octets, output_octets=EXCLUDED.output_octets,
             observed_vlan_id=EXCLUDED.observed_vlan_id, observed_filter_id=EXCLUDED.observed_filter_id, nas_port_id=EXCLUDED.nas_port_id`,
		incoming.SessionID, incoming.RuntimeID, incoming.NasID, incoming.CredentialID, incoming.SubjectID, incoming.DeviceID, incoming.StationID,
		incoming.AccountingSessionID, incoming.State, incoming.StartedAt, incoming.StoppedAt, incoming.LastObservedAt, incoming.LastReceivedAt,
		incoming.LastStatus, incoming.LastSessionSeconds, incoming.VLANID, incoming.FilterID, incoming.AccountingEpoch, incoming.NASPortID, incoming.InputOctets, incoming.OutputOctets).Error; err != nil {
		return normalizeDatabaseError(err)
	}
	if incoming.StoppedAt != nil {
		return tx.Exec(`UPDATE network_runtime_sessions SET status = 'expired', revoke_reason = 'nas_accounting_stopped', updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND status NOT IN ('revoked', 'expired')`, receivedAt, incoming.SessionID).Error
	}
	return nil
}

func recordNASReset(tx *gorm.DB, runtimeID, nasID, status string, observedAt, receivedAt time.Time) error {
	result := tx.Exec(`INSERT INTO network_nas_accounting_epochs (runtime_id, nas_id, observed_at, received_at, status)
        VALUES (?, ?, ?, ?, ?) ON CONFLICT (tenant_id, workspace_id, runtime_id, nas_id) DO UPDATE SET
         observed_at=EXCLUDED.observed_at, received_at=EXCLUDED.received_at, status=EXCLUDED.status
        WHERE EXCLUDED.observed_at > network_nas_accounting_epochs.observed_at`, runtimeID, nasID, observedAt, receivedAt, status)
	if result.Error != nil || result.RowsAffected == 0 {
		return result.Error
	}
	if err := tx.Exec(`UPDATE network_runtime_sessions SET status = 'expired', revoke_reason = 'nas_accounting_restarted', updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND nas_id = ? AND created_at <= ? AND status NOT IN ('revoked', 'expired')`, receivedAt, runtimeID, nasID, observedAt).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_nas_session_evidence SET state = 'unknown', last_status = 'nas_accounting_restarted'
        WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND nas_id = ? AND stopped_at IS NULL AND started_at <= ?`, runtimeID, nasID, observedAt).Error
}

func accountingBindingError() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "nas_accounting_binding_mismatch", "The Accounting packet does not match a unique authorized NAS session.", "Accounting 数据包不能匹配唯一的已授权 NAS 会话。")
}
