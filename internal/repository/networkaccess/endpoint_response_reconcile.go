package networkaccess

import (
	"context"
	"errors"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) EndpointResponseCandidates(ctx context.Context, after string, limit int) ([]domain.EndpointResponseCandidate, error) {
	return queryMany(ctx, r.db, `WITH candidates AS (
        SELECT p.id AS policy_id, s.id AS session_id, ''::text AS event_id FROM network_endpoint_response_policies p
        JOIN network_runtime_sessions s ON s.tenant_id=p.tenant_id AND s.workspace_id=p.workspace_id
        JOIN network_nas_authorizations a ON a.tenant_id=s.tenant_id AND a.workspace_id=s.workspace_id AND a.session_id=s.id
        WHERE p.tenant_id='default' AND p.workspace_id='default' AND p.deleted_at IS NULL
          AND p.published_revision > 0 AND p.published_configuration->>'enabled'='true'
          AND jsonb_exists(p.published_configuration->'siteIds', s.site_id)
          AND a.authentication_method='eap-tls' AND COALESCE(a.credential_id, '')<>''
		  AND s.status NOT IN ('expired', 'revoked')
          AND NOT EXISTS (SELECT 1 FROM network_endpoint_response_events e WHERE e.tenant_id=p.tenant_id AND e.workspace_id=p.workspace_id AND e.policy_id=p.id AND e.policy_revision=p.published_revision AND e.session_id=s.id AND e.restored_at IS NULL)
        UNION ALL
        SELECT e.policy_id, e.session_id, e.id FROM network_endpoint_response_events e
        WHERE e.tenant_id='default' AND e.workspace_id='default' AND e.restored_at IS NULL
        ) SELECT policy_id, session_id, event_id FROM candidates
          WHERE (policy_id || '/' || session_id || '/' || event_id) > ? ORDER BY (policy_id || '/' || session_id || '/' || event_id) LIMIT ?`, []any{after, limit}, func(row scanner) (domain.EndpointResponseCandidate, error) {
		var item domain.EndpointResponseCandidate
		err := row.Scan(&item.PolicyID, &item.SessionID, &item.EventID)
		return item, err
	})
}

// The callback evaluates a consistent snapshot while publication, NAS packets and heartbeats are locked.
func (r *Repository) ReconcileEndpointResponse(ctx context.Context, policyID, sessionID, eventID string, now time.Time, evaluate func(domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error)) (domain.EndpointResponseRecord, error) {
	var resultID string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		snapshot, err := repo.endpointResponseSnapshot(ctx, policyID, sessionID, eventID, now)
		if err != nil {
			return err
		}
		mutation, err := evaluate(snapshot)
		if err != nil {
			return err
		}
		if mutation == nil {
			if snapshot.Event != nil {
				resultID = snapshot.Event.ID
			}
			return nil
		}
		resultID = mutation.Event.ID
		return persistResponseMutation(tx, *mutation, now)
	})
	if err != nil || resultID == "" {
		return domain.EndpointResponseRecord{}, err
	}
	return r.EndpointResponseEvent(ctx, resultID)
}

func (r *Repository) EndpointResponseSnapshot(ctx context.Context, policyID, sessionID string, now time.Time) (domain.EndpointResponseSnapshot, error) {
	return r.endpointResponseSnapshot(ctx, policyID, sessionID, "", now)
}

func (r *Repository) endpointResponseSnapshot(ctx context.Context, policyID, sessionID, eventID string, now time.Time) (domain.EndpointResponseSnapshot, error) {
	var item domain.EndpointResponseSnapshot
	var err error
	item.Session, err = r.GetSession(ctx, sessionID)
	if err != nil {
		return item, err
	}
	if err := r.loadEndpointResponsePolicy(ctx, &item, policyID, sessionID, eventID); err != nil {
		return item, err
	}
	item.Presence, err = r.EndpointPresence(ctx, item.Session.DeviceID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return item, err
	}
	item.Presence.DeviceID = item.Session.DeviceID
	item.Observation, err = r.NASSessionObservation(ctx, sessionID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return item, err
	}
	item.Device, err = r.GetDevice(ctx, item.Session.DeviceID)
	if err != nil {
		return item, err
	}
	item.Subject, err = r.GetSubject(ctx, item.Session.SubjectID)
	if err != nil {
		return item, err
	}
	item.Site, err = r.GetSite(ctx, item.Session.SiteID)
	if err != nil {
		return item, err
	}
	item.Binding, err = r.FindActiveNASBinding(ctx, item.Session.RuntimeID, item.Session.NASID)
	if err != nil {
		return item, err
	}
	item.AccessPolicies, err = r.GetPolicySnapshot(ctx)
	if err != nil {
		return item, err
	}
	profiles, err := r.ListSiteProfileBindings(ctx, domain.SiteProfileBindingFilter{SiteID: item.Site.ID, Limit: 10})
	if err != nil {
		return item, err
	}
	item.Profiles = map[string]domain.SiteProfileBinding{}
	for _, profile := range profiles {
		item.Profiles[profile.AccessProfile] = profile
	}
	item.CredentialActive, err = r.EndpointCredentialCurrent(ctx, item.Presence.CredentialID, item.Session.DeviceID, item.Session.SubjectID, now)
	if err != nil {
		return item, err
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM network_endpoint_maintenance_windows WHERE tenant_id='default' AND workspace_id='default' AND device_id=? AND site_id=? AND ended_at IS NULL AND expires_at>?)`, item.Session.DeviceID, item.Session.SiteID, now).Scan(&item.Maintenance).Error; err != nil {
		return item, err
	}
	if item.Event != nil {
		item.Commands, err = queryMany(ctx, r.db, sessionCommandSelect+` AND response_event_id=? ORDER BY created_at, id LIMIT 16`, []any{item.Event.ID}, scanSessionCommand)
	}
	return item, err
}

func (r *Repository) loadEndpointResponsePolicy(ctx context.Context, item *domain.EndpointResponseSnapshot, policyID, sessionID, eventID string) error {
	var err error
	if eventID != "" {
		event, err := r.EndpointResponseEvent(ctx, eventID)
		if err != nil {
			return err
		}
		if event.SessionID != sessionID || event.PolicyID != policyID {
			return responseRevisionConflict()
		}
		item.Event = &event
		revision, err := r.EndpointResponsePolicyRevision(ctx, policyID, event.PolicyRevision)
		if err != nil {
			return err
		}
		item.Policy = domain.EndpointResponsePolicy{ID: policyID, Revision: revision.Revision, PublishedRevision: revision.Revision, Configuration: revision.Configuration, PublishedConfiguration: &revision.Configuration}
		return r.db.WithContext(ctx).Raw(`SELECT NOT EXISTS (SELECT 1 FROM network_endpoint_response_policies WHERE tenant_id='default' AND workspace_id='default' AND id=? AND deleted_at IS NULL AND published_revision=? AND published_configuration->>'enabled'='true')`, policyID, event.PolicyRevision).Scan(&item.PolicySuperseded).Error
	} else {
		item.Policy, err = r.EndpointResponsePolicy(ctx, policyID)
		if err != nil {
			return err
		}
		item.Event, err = optionalResponseEvent(r.db, policyID, item.Policy.PublishedRevision, sessionID)
		if err != nil {
			return err
		}
	}
	return err
}
