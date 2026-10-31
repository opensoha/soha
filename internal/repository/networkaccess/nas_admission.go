package networkaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) NASAdmissionCandidates(ctx context.Context, after string, limit int) ([]string, error) {
	return queryMany(ctx, r.db, `SELECT s.id FROM network_runtime_sessions s
        JOIN network_nas_authorizations a ON a.tenant_id=s.tenant_id AND a.workspace_id=s.workspace_id AND a.session_id=s.id
        WHERE s.tenant_id='default' AND s.workspace_id='default' AND s.status NOT IN ('revoked','expired')
          AND a.authentication_method='eap-tls' AND COALESCE(a.credential_id,'')<>''
          AND s.id>? ORDER BY s.id LIMIT ?`, []any{after, limit}, func(row scanner) (string, error) {
		var id string
		err := row.Scan(&id)
		return id, err
	})
}

func (r *Repository) ReconcileNASAdmission(ctx context.Context, sessionID string, now time.Time, evaluate func(domain.NASAdmissionSnapshot) *domain.NASAdmissionMutation) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, responsePolicyLock).Error; err != nil {
			return err
		}
		repo := New(tx)
		session, err := repo.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
		for _, lock := range []string{"network-access-policy-snapshot:default:default", "network-nas-accounting:" + session.RuntimeID + ":" + session.NASID, "network-endpoint-device:" + session.DeviceID} {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, lock).Error; err != nil {
				return err
			}
		}
		snapshot, err := repo.nasAdmissionSnapshot(ctx, sessionID, now)
		if err != nil {
			return err
		}
		mutation := evaluate(snapshot)
		if mutation == nil {
			return nil
		}
		attributes, err := json.Marshal(mutation.RadiusAttributes)
		if err != nil {
			return err
		}
		commandID := ""
		if mutation.Command != nil {
			commandID = mutation.Command.ID
		}
		if err := tx.Exec(`UPDATE network_runtime_sessions SET admission_floor_profile=?, admission_inputs_hash=?, admission_radius_attributes=?::jsonb, admission_revision=admission_revision+1, policy_version=?, admission_restriction_reason=?, admission_attempts=?, admission_command_id=COALESCE(NULLIF(?,''),admission_command_id), updated_at=? WHERE tenant_id='default' AND workspace_id='default' AND id=?`, mutation.Profile, mutation.InputsHash, string(attributes), mutation.PolicyVersion, mutation.Reason, mutation.Attempts, commandID, now, sessionID).Error; err != nil {
			return err
		}
		if mutation.Command == nil {
			return nil
		}
		// Superseded acknowledgements cannot broaden a persisted restriction.
		if err := tx.Exec(`UPDATE network_nas_session_commands SET status='expired', result_reason_code='nas_admission_superseded', completed_at=?, updated_at=? WHERE tenant_id='default' AND workspace_id='default' AND session_id=? AND status IN ('pending','delivered')`, now, now, sessionID).Error; err != nil {
			return err
		}
		_, err = repo.CreateSessionCommand(ctx, *mutation.Command)
		return err
	})
}

func (r *Repository) nasAdmissionSnapshot(ctx context.Context, sessionID string, now time.Time) (domain.NASAdmissionSnapshot, error) {
	var item domain.NASAdmissionSnapshot
	var err error
	item.Session, err = r.GetSession(ctx, sessionID)
	if err != nil {
		return item, err
	}
	var credentialID, subjectID, deviceID, stationID string
	var attributes []byte
	if err := r.db.WithContext(ctx).Raw(`SELECT s.admission_floor_profile, COALESCE(NULLIF(s.admission_inputs_hash,''),a.admission_inputs_hash), COALESCE(s.admission_radius_attributes,a.radius_attributes,'null'::jsonb), s.admission_revision, s.admission_attempts, COALESCE(a.credential_id,''), a.subject_id, a.device_id, COALESCE(a.station_id,'')
        FROM network_runtime_sessions s JOIN network_nas_authorizations a ON a.tenant_id=s.tenant_id AND a.workspace_id=s.workspace_id AND a.session_id=s.id
        WHERE s.tenant_id='default' AND s.workspace_id='default' AND s.id=? AND a.runtime_id=s.runtime_id AND a.nas_id=s.nas_id AND a.decision='allow' AND a.authentication_method='eap-tls'`, sessionID).Row().Scan(&item.FloorProfile, &item.InputsHash, &attributes, &item.Revision, &item.Attempts, &credentialID, &subjectID, &deviceID, &stationID); err != nil {
		return item, err
	}
	if err := json.Unmarshal(attributes, &item.RadiusAttributes); err != nil {
		return item, err
	}
	item.StrongBinding = credentialID != "" && subjectID == item.Session.SubjectID && deviceID == item.Session.DeviceID && domain.ValidStationID(stationID)
	if err := r.loadNASAdmissionAttributes(ctx, &item); err != nil {
		return item, err
	}
	profiles, err := r.ListSiteProfileBindings(ctx, domain.SiteProfileBindingFilter{SiteID: item.Session.SiteID, Limit: 10})
	if err != nil {
		return item, err
	}
	item.Profiles = map[string]domain.SiteProfileBinding{}
	for _, profile := range profiles {
		item.Profiles[profile.AccessProfile] = profile
	}
	floor, err := r.EndpointRestriction(ctx, item.Session.DeviceID, item.Session.SiteID)
	if err != nil {
		return item, err
	}
	if floor.AccessProfile != "" {
		item.FloorProfile = domain.RestrictAccessProfile(domain.RestrictAccessProfile(item.Session.AccessProfile, item.FloorProfile), floor.AccessProfile)
	}
	item.CredentialActive, err = r.EndpointCredentialCurrent(ctx, credentialID, item.Session.DeviceID, item.Session.SubjectID, now)
	return item, err
}

func missingAdmissionRecord(err error) bool {
	return errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, sql.ErrNoRows)
}

func (r *Repository) loadNASAdmissionAttributes(ctx context.Context, item *domain.NASAdmissionSnapshot) error {
	var err error
	item.Subject, err = r.GetSubject(ctx, item.Session.SubjectID)
	if err != nil && !missingAdmissionRecord(err) {
		return err
	}
	item.Device, err = r.GetDevice(ctx, item.Session.DeviceID)
	if err != nil && !missingAdmissionRecord(err) {
		return err
	}
	item.Site, err = r.GetSite(ctx, item.Session.SiteID)
	if err != nil && !missingAdmissionRecord(err) {
		return err
	}
	item.Binding, err = r.FindActiveNASBinding(ctx, item.Session.RuntimeID, item.Session.NASID)
	if err != nil && !missingAdmissionRecord(err) {
		return err
	}
	item.Policies, err = r.GetPolicySnapshot(ctx)
	if err != nil && !missingAdmissionRecord(err) {
		return err
	}

	return nil
}
