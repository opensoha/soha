package networkaccess

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *EndpointResponseService) Presence(ctx context.Context, principal identity.Principal, deviceID string) (domain.EndpointPresence, error) {
	key := "network_access.endpoint_devices.view"
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return domain.EndpointPresence{}, err
	}
	device, err := s.base.store.GetDevice(ctx, deviceID)
	if err != nil {
		return domain.EndpointPresence{}, err
	}
	if err := s.base.authorizeScope(ctx, principal, key, deviceScope(device)); err != nil {
		return domain.EndpointPresence{}, err
	}
	item, err := s.store.EndpointPresence(ctx, deviceID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return domain.EndpointPresence{DeviceID: deviceID, ServiceState: sohaapi.NetworkEndpointPresenceServiceStateUnknown, UIState: sohaapi.NetworkEndpointPresenceUIStateUnknown, ReasonCode: "endpoint_presence_unknown"}, nil
	}
	if err != nil {
		return domain.EndpointPresence{}, err
	}
	if item.LastReceivedAt == nil || s.now().Sub(*item.LastReceivedAt) > 2*time.Duration(item.HeartbeatIntervalSeconds)*time.Second {
		item.ServiceState = sohaapi.NetworkEndpointPresenceServiceStateOffline
		item.ReasonCode = "endpoint_presence_stale"
	}
	return item.EndpointPresence, nil
}

func (s *EndpointResponseService) Events(ctx context.Context, principal identity.Principal, filter domain.EndpointResponseFilter) ([]domain.EndpointResponseEvent, error) {
	key := responseEventPermission("view")
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return nil, err
	}
	rows, err := collectVisible(ctx, boundedLimit(filter.Limit), func(limit, offset int) ([]domain.EndpointResponseRecord, error) {
		filter.Limit, filter.Offset = limit, offset
		return s.store.EndpointResponseEvents(ctx, filter)
	}, func(event domain.EndpointResponseRecord) error {
		return s.authorizeEvent(ctx, principal, key, event.EndpointResponseEvent)
	})
	if err != nil {
		return nil, err
	}
	items := make([]domain.EndpointResponseEvent, len(rows))
	for index, row := range rows {
		items[index] = row.EndpointResponseEvent
	}
	return items, nil
}

func (s *EndpointResponseService) authorizeEvent(ctx context.Context, principal identity.Principal, key string, event domain.EndpointResponseEvent) error {
	return s.base.authorizeScope(ctx, principal, key, resourceScope{kind: "EndpointResponseEvent", id: event.ID, siteID: event.SiteID, deviceID: event.DeviceID, ownerUserID: event.SubjectID})
}

func (s *EndpointResponseService) Event(ctx context.Context, principal identity.Principal, id string) (domain.EndpointResponseEvent, error) {
	key := responseEventPermission("view")
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return domain.EndpointResponseEvent{}, err
	}
	item, err := s.store.EndpointResponseEvent(ctx, id)
	if err != nil {
		return domain.EndpointResponseEvent{}, err
	}
	return item.EndpointResponseEvent, s.authorizeEvent(ctx, principal, key, item.EndpointResponseEvent)
}

func (s *EndpointResponseService) Restore(ctx context.Context, principal identity.Principal, id string, input sohaapi.NetworkEndpointResponseRestoreInput) (domain.EndpointResponseEvent, error) {
	key := responseEventPermission("restore")
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ExpectedRevision < 1 || input.Reason == "" || len(input.Reason) > 500 {
		return domain.EndpointResponseEvent{}, invalidResponse("restore_input")
	}
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return domain.EndpointResponseEvent{}, err
	}
	event, err := s.store.EndpointResponseEvent(ctx, id)
	if err != nil {
		return domain.EndpointResponseEvent{}, err
	}
	if err := s.authorizeEvent(ctx, principal, key, event.EndpointResponseEvent); err != nil {
		return domain.EndpointResponseEvent{}, err
	}
	now := s.now().UTC()
	restored, err := s.store.ReconcileEndpointResponse(ctx, event.PolicyID, event.SessionID, id, now, func(snapshot domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
		if snapshot.Event == nil || snapshot.Event.Revision != input.ExpectedRevision {
			return nil, responseRecoveryConflict("endpoint_response_revision_conflict")
		}
		if snapshot.Event.RestoredAt != nil {
			return nil, nil
		}
		if err := endpointRestoreAllowed(snapshot, now); err != nil {
			return nil, err
		}
		return restoreEndpointEvent(snapshot, now, "endpoint_manual_restore"), nil
	})
	if err == nil {
		s.base.recordMutation(ctx, principal, key, "EndpointResponseEvent", id, input.Reason)
	}
	return restored.EndpointResponseEvent, err
}

func (s *EndpointResponseService) Preview(ctx context.Context, principal identity.Principal, id string, input sohaapi.NetworkEndpointResponsePreviewInput) (domain.EndpointResponsePreview, error) {
	key := responsePolicyPermission("preview")
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return domain.EndpointResponsePreview{}, err
	}
	policy, err := s.store.EndpointResponsePolicy(ctx, id)
	if err != nil {
		return domain.EndpointResponsePreview{}, err
	}
	if err := s.authorizePolicy(ctx, principal, key, policy); err != nil {
		return domain.EndpointResponsePreview{}, err
	}
	device, err := s.base.store.GetDevice(ctx, input.DeviceID)
	if err != nil {
		return domain.EndpointResponsePreview{}, err
	}
	if err := s.base.authorizeScope(ctx, principal, key, deviceScope(device)); err != nil {
		return domain.EndpointResponsePreview{}, err
	}
	presence, err := s.store.EndpointPresence(ctx, device.ID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return domain.EndpointResponsePreview{}, err
	}
	if presence.DeviceID == "" {
		presence.EndpointPresence = domain.EndpointPresence{DeviceID: device.ID, ServiceState: sohaapi.NetworkEndpointPresenceServiceStateUnknown, UIState: sohaapi.NetworkEndpointPresenceUIStateUnknown}
	}
	result := domain.EndpointResponsePreview{PolicyID: id, Revision: policy.Revision, DeviceID: device.ID, Presence: presence.EndpointPresence, Steps: policy.Configuration.Steps, Sessions: []domain.NASSessionEvidence{}, UnsupportedActions: []sohaapi.NetworkEndpointResponseAction{}, ReasonCode: "nas_session_unconfirmed"}
	rows, err := s.base.store.ListSessions(ctx, domain.SessionFilter{DeviceID: device.ID, Limit: 201})
	if err != nil {
		return result, err
	}
	if len(rows) > 200 {
		return result, responseRecoveryConflict("endpoint_response_session_limit")
	}
	for _, row := range rows {
		if row.NASID == "" || input.SessionID != "" && input.SessionID != row.ID {
			continue
		}
		if err := s.base.authorizeScope(ctx, principal, key, sessionScope(row)); errors.Is(err, apperrors.ErrAccessDenied) {
			continue
		} else if err != nil {
			return result, err
		}
		snapshot, err := s.store.EndpointResponseSnapshot(ctx, id, row.ID, s.now().UTC())
		if errors.Is(err, apperrors.ErrNotFound) {
			continue
		}
		if err != nil {
			return result, err
		}
		s.previewEndpointResponseSession(&result, policy.Configuration, presence, snapshot)

	}
	return result, nil
}

func (s *EndpointResponseService) Maintenance(ctx context.Context, principal identity.Principal, filter domain.EndpointResponseFilter) ([]domain.EndpointMaintenance, error) {
	key := responseEventPermission("maintenance")
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return nil, err
	}
	return collectVisible(ctx, boundedLimit(filter.Limit), func(limit, offset int) ([]domain.EndpointMaintenance, error) {
		filter.Limit, filter.Offset = limit, offset
		return s.store.EndpointMaintenance(ctx, filter, s.now().UTC())
	}, func(item domain.EndpointMaintenance) error {
		return s.base.authorizeScope(ctx, principal, key, resourceScope{kind: "EndpointMaintenance", id: item.ID, siteID: item.SiteID, deviceID: item.DeviceID})
	})
}

func (s *EndpointResponseService) CreateMaintenance(ctx context.Context, principal identity.Principal, input domain.EndpointMaintenanceInput) (domain.EndpointMaintenance, error) {
	key := responseEventPermission("maintenance")
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return domain.EndpointMaintenance{}, err
	}
	now := s.now().UTC()
	input.Reason = strings.TrimSpace(input.Reason)
	if !input.ExpiresAt.After(now) || input.ExpiresAt.After(now.Add(4*time.Hour)) || input.Reason == "" || len(input.Reason) > 500 {
		return domain.EndpointMaintenance{}, invalidResponse("maintenance_input")
	}
	device, err := s.base.store.GetDevice(ctx, input.DeviceID)
	if err != nil {
		return domain.EndpointMaintenance{}, err
	}
	if err := s.base.authorizeScope(ctx, principal, key, deviceScope(device)); err != nil {
		return domain.EndpointMaintenance{}, err
	}
	if _, err := s.base.store.GetSite(ctx, input.SiteID); err != nil {
		return domain.EndpointMaintenance{}, err
	}
	item := domain.EndpointMaintenance{ID: uuid.NewString(), DeviceID: input.DeviceID, SiteID: input.SiteID, ExpiresAt: input.ExpiresAt, Reason: input.Reason, ActorID: principal.UserID, CreatedAt: now}
	if err := s.base.authorizeScope(ctx, principal, key, resourceScope{kind: "EndpointMaintenance", id: item.ID, siteID: item.SiteID, deviceID: item.DeviceID, ownerUserID: device.OwnerUserID}); err != nil {
		return item, err
	}
	err = s.store.SaveEndpointMaintenance(ctx, item)
	if err == nil {
		s.base.recordMutation(ctx, principal, key, "EndpointMaintenance", item.ID, item.Reason)
	}
	return item, err
}

func (s *EndpointResponseService) EndMaintenance(ctx context.Context, principal identity.Principal, id string) error {
	key := responseEventPermission("maintenance")
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return err
	}
	item, err := s.store.EndpointMaintenanceByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.base.authorizeScope(ctx, principal, key, resourceScope{kind: "EndpointMaintenance", id: item.ID, siteID: item.SiteID, deviceID: item.DeviceID}); err != nil {
		return err
	}
	_, err = s.store.EndEndpointMaintenance(ctx, id, s.now().UTC())
	if err == nil {
		s.base.recordMutation(ctx, principal, key, "EndpointMaintenance", id, item.Reason)
	}
	return err
}

func (s *EndpointResponseService) previewEndpointResponseSession(result *domain.EndpointResponsePreview, config domain.EndpointResponsePolicyConfig, presence domain.PresenceRecord, snapshot domain.EndpointResponseSnapshot) {
	if !MatchesEndpointResponse(config, snapshot.Subject, snapshot.Device, snapshot.Binding) {
		return
	}
	trigger, deadline, reason := EndpointResponseSignal(config, presence, s.now().UTC())
	if snapshot.Maintenance {
		reason = "endpoint_maintenance_active"
		trigger = false
	}
	if !freshNASEvidence(config, snapshot.Observation, s.now().UTC()) || snapshot.Observation.CredentialID == "" {
		reason = "nas_session_unconfirmed"
		trigger = false
	} else {
		result.Sessions = append(result.Sessions, snapshot.Observation.NASSessionEvidence)
	}
	result.Matched = result.Matched || trigger
	result.ReasonCode = reason
	result.TriggerAfter = &deadline
	for _, step := range config.Steps {
		if step.Action == sohaapi.NetworkEndpointResponseActionRestrict && !snapshot.Binding.CoASupported || step.Action == sohaapi.NetworkEndpointResponseActionDisconnect && !snapshot.Binding.DisconnectSupported {
			if !slices.Contains(result.UnsupportedActions, step.Action) {
				result.UnsupportedActions = append(result.UnsupportedActions, step.Action)
			}
		}
	}
}
