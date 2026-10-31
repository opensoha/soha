package networkaccess

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *EndpointResponseService) Run(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	after := ""
	nasAfter := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		rows, err := s.store.EndpointResponseCandidates(ctx, after, 200)
		if err != nil {
			report(err)
			continue
		}
		for _, row := range rows {
			if ctx.Err() != nil {
				return
			}
			now := s.now().UTC()
			_, err := s.store.ReconcileEndpointResponse(ctx, row.PolicyID, row.SessionID, row.EventID, now, func(snapshot domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
				return reconcileEndpointResponse(snapshot, now)
			})
			if err != nil && !errors.Is(err, apperrors.ErrNotFound) && !errors.Is(err, apperrors.ErrConflict) {
				report(err)
			}
			after = row.PolicyID + "/" + row.SessionID + "/" + row.EventID
		}
		if len(rows) < 200 {
			after = ""
		}
		nasRows, err := s.store.NASAdmissionCandidates(ctx, nasAfter, 200)
		if err != nil {
			report(err)
			continue
		}
		for _, sessionID := range nasRows {
			if ctx.Err() != nil {
				return
			}
			now := s.now().UTC()
			if err := s.store.ReconcileNASAdmission(ctx, sessionID, now, func(snapshot domain.NASAdmissionSnapshot) *domain.NASAdmissionMutation {
				return reconcileNASAdmission(snapshot, now)
			}); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
				report(err)
			}
			nasAfter = sessionID
		}
		if len(nasRows) < 200 {
			nasAfter = ""
		}
	}
}

// A slow notification receiver must not delay admission revocation.
func (s *EndpointResponseService) RunNotifications(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.deliverNotifications(ctx); err != nil && ctx.Err() == nil {
				report(err)
			}
		}
	}
}

func reconcileEndpointResponse(snapshot domain.EndpointResponseSnapshot, now time.Time) (*domain.EndpointResponseMutation, error) {
	if snapshot.Event != nil && snapshot.PolicySuperseded {
		return reconcileSupersededEndpointResponse(snapshot, now), nil
	}
	config := snapshot.Policy.PublishedConfiguration
	if config == nil || !config.Enabled || !MatchesEndpointResponse(*config, snapshot.Subject, snapshot.Device, snapshot.Binding) {
		return nil, nil
	}
	if snapshot.Observation.CredentialID == "" || snapshot.Observation.DeviceID != snapshot.Device.ID || snapshot.Observation.SubjectID != snapshot.Subject.UserID {
		return nil, nil
	}
	triggered, deadline, reason := EndpointResponseSignal(*config, snapshot.Presence, now)
	if snapshot.Event == nil && (!triggered || snapshot.Maintenance || !freshNASEvidence(*config, snapshot.Observation, now)) {
		return nil, nil
	}
	if snapshot.Event != nil && !triggered {
		return recoverEndpointResponse(snapshot, now), nil
	}
	event := newEndpointEvent(snapshot, deadline, reason, now)
	if snapshot.Event != nil {
		event = *snapshot.Event
		event.Revision++
		event.UpdatedAt = now
	}
	if now.Before(event.TriggeredAt) {
		if snapshot.Event == nil {
			return &domain.EndpointResponseMutation{Event: event}, nil
		}
		return nil, nil
	}
	if snapshot.Maintenance && event.RestrictionProfile == "" {
		return nil, nil
	}
	return advanceEndpointResponse(snapshot, event, now), nil
}

// Policy changes retire old actions without losing their floor or NAS receipts.
func reconcileSupersededEndpointResponse(snapshot domain.EndpointResponseSnapshot, now time.Time) *domain.EndpointResponseMutation {
	if snapshot.Policy.PublishedConfiguration == nil {
		return nil
	}
	if snapshot.Policy.PublishedConfiguration.AutoRestore && endpointRestoreAllowed(snapshot, now) == nil {
		return restoreEndpointEvent(snapshot, now, "endpoint_healthy")
	}
	status, _ := endpointCommandProgress(snapshot, now)
	cancelPending := false
	for _, command := range snapshot.Commands {
		cancelPending = cancelPending || command.Status == domain.SessionCommandPending
	}
	if status == "" || cancelPending {
		status = sohaapi.NetworkEndpointResponseEventStatusRecovering
	}
	if !cancelPending && snapshot.Event.Status == status && snapshot.Event.ReasonCode == "endpoint_response_policy_changed" {
		return nil
	}
	event := *snapshot.Event
	event.Revision++
	event.UpdatedAt, event.Status, event.ReasonCode = now, status, "endpoint_response_policy_changed"
	evidence := snapshot.Observation.NASSessionEvidence
	event.NasEvidence = &evidence
	return &domain.EndpointResponseMutation{Event: event, CancelPendingCommands: cancelPending}
}

func recoverEndpointResponse(snapshot domain.EndpointResponseSnapshot, now time.Time) *domain.EndpointResponseMutation {
	if snapshot.Policy.PublishedConfiguration.AutoRestore && endpointRestoreAllowed(snapshot, now) == nil {
		return restoreEndpointEvent(snapshot, now, "endpoint_healthy")
	}
	if snapshot.Event.Status == sohaapi.NetworkEndpointResponseEventStatusRecovering {
		return nil
	}
	event := *snapshot.Event
	event.Revision++
	event.UpdatedAt = now
	event.Status = sohaapi.NetworkEndpointResponseEventStatusRecovering
	return &domain.EndpointResponseMutation{Event: event}
}

func advanceEndpointResponse(snapshot domain.EndpointResponseSnapshot, event domain.EndpointResponseRecord, now time.Time) *domain.EndpointResponseMutation {
	config := snapshot.Policy.PublishedConfiguration
	next := event.Stage + 1
	if snapshot.Event == nil || event.Status == sohaapi.NetworkEndpointResponseEventStatusObserving {
		next = 0
	}
	if next < len(config.Steps) && !now.Before(event.TriggeredAt.Add(time.Duration(config.Steps[next].AfterSeconds)*time.Second)) {
		return applyEndpointStep(snapshot, event, next, config.Steps[next], now)
	}
	status, progressReason := endpointCommandProgress(snapshot, now)
	if (status == sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown || event.Status == sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown) && config.FailureAction == sohaapi.NetworkEndpointResponsePolicyConfigurationFailureActionDisconnect && event.Action != sohaapi.NetworkEndpointResponseActionDisconnect {
		mutation := applyEndpointStep(snapshot, event, event.Stage, domain.EndpointResponseStep{Action: sohaapi.NetworkEndpointResponseActionDisconnect}, now)
		return mutation
	}
	if status != "" && (event.Status != status || event.ReasonCode != progressReason) {
		event.Status, event.ReasonCode = status, progressReason
		return &domain.EndpointResponseMutation{Event: event}
	}
	return nil
}

func newEndpointEvent(snapshot domain.EndpointResponseSnapshot, deadline time.Time, reason string, now time.Time) domain.EndpointResponseRecord {
	config := snapshot.Policy.PublishedConfiguration
	if snapshot.Presence.LastReceivedAt == nil {
		deadline = snapshot.Session.StartedAt.Add(time.Duration(config.HeartbeatIntervalSeconds*config.MissedHeartbeats+config.GracePeriodSeconds) * time.Second)
	}
	presence, evidence := snapshot.Presence.EndpointPresence, snapshot.Observation.NASSessionEvidence
	return domain.EndpointResponseRecord{EndpointResponseEvent: domain.EndpointResponseEvent{
		ID: uuid.NewString(), PolicyID: snapshot.Policy.ID, PolicyRevision: snapshot.Policy.PublishedRevision, Revision: 1,
		DeviceID: snapshot.Device.ID, SubjectID: snapshot.Subject.UserID, SiteID: snapshot.Site.ID, SessionID: snapshot.Session.ID, NasID: snapshot.Binding.NASID,
		Status: sohaapi.NetworkEndpointResponseEventStatusObserving, ReasonCode: reason, Signal: config.Signal, Stage: 0, Action: sohaapi.NetworkEndpointResponseActionRecord,
		CommandIDs: []string{}, NotificationState: sohaapi.NetworkEndpointResponseNotificationStateNotRequested, TriggeredAt: deadline, UpdatedAt: now, Presence: &presence, NasEvidence: &evidence,
	}}
}

func applyEndpointStep(snapshot domain.EndpointResponseSnapshot, event domain.EndpointResponseRecord, stage int, step domain.EndpointResponseStep, now time.Time) *domain.EndpointResponseMutation {
	event.Stage, event.Action, event.Status = stage, step.Action, sohaapi.NetworkEndpointResponseEventStatusActive
	presence, evidence := snapshot.Presence.EndpointPresence, snapshot.Observation.NASSessionEvidence
	event.Presence, event.NasEvidence = &presence, &evidence
	mutation := &domain.EndpointResponseMutation{Event: event}
	switch step.Action {
	case sohaapi.NetworkEndpointResponseActionNotify:
		mutation.Event.NotificationPolicyID = snapshot.Policy.PublishedConfiguration.NotificationPolicyID
		if event.NotificationState == sohaapi.NetworkEndpointResponseNotificationStateNotRequested {
			mutation.Event.NotificationState = sohaapi.NetworkEndpointResponseNotificationStatePending
		}
	case sohaapi.NetworkEndpointResponseActionRestrict, sohaapi.NetworkEndpointResponseActionDisconnect:
		profile, action := string(step.TargetAccessProfile), domain.SessionActionCoA
		if step.Action == sohaapi.NetworkEndpointResponseActionDisconnect {
			profile, action = domain.ProfileDeny, domain.SessionActionDisconnect
		}
		mutation.Event.RestrictionProfile = domain.RestrictAccessProfile(profile, event.RestrictionProfile)
		if !freshNASEvidence(*snapshot.Policy.PublishedConfiguration, snapshot.Observation, now) {
			mutation.Event.Status = sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown
			mutation.Event.ReasonCode = "nas_accounting_stale"
			return mutation
		}
		if action == domain.SessionActionDisconnect && !snapshot.Binding.DisconnectSupported || action != domain.SessionActionDisconnect && !snapshot.Binding.CoASupported {
			mutation.Event.Status = sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown
			mutation.Event.ReasonCode = "nas_action_unsupported"
			return mutation
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("endpoint-response:%s:%d:%s:%s", event.ID, stage, action, mutation.Event.RestrictionProfile)))
		command := domain.SessionCommand{ID: uuid.NewString(), SessionID: snapshot.Session.ID, RuntimeID: snapshot.Session.RuntimeID, NASID: snapshot.Session.NASID, Action: action, TargetAccessProfile: mutation.Event.RestrictionProfile, PolicyVersion: snapshot.AccessPolicies.PolicyVersion, Status: domain.SessionCommandPending, ReasonCode: "endpoint_response", PlanHash: fmt.Sprintf("sha256:%x", digest), EffectiveAt: now, ExpiresAt: now.Add(time.Minute), CreatedAt: now}
		if action != domain.SessionActionDisconnect {
			binding, ok := snapshot.Profiles[command.TargetAccessProfile]
			if !ok {
				mutation.Event.Status = sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown
				mutation.Event.ReasonCode = "repair_profile_unavailable"
				return mutation
			}
			command.RadiusAttributes = &networkprotocol.RadiusAttributes{VLANID: binding.VLANID, FilterID: binding.FilterID, SessionTimeoutSeconds: binding.SessionTimeoutSeconds}
		}
		mutation.Command = &command
		mutation.Event.CommandIDs = append(mutation.Event.CommandIDs, command.ID)
		mutation.Event.Status = sohaapi.NetworkEndpointResponseEventStatusEnforcing
	}
	return mutation
}

func freshNASEvidence(config domain.EndpointResponsePolicyConfig, evidence domain.NASSessionObservation, now time.Time) bool {
	return evidence.State == sohaapi.NetworkNASSessionEvidenceStateActive && evidence.StartedAt != nil && evidence.StoppedAt == nil && evidence.LastReceivedAt != nil && evidence.LastObservedAt != nil &&
		now.Sub(*evidence.LastReceivedAt) <= time.Duration(config.AccountingFreshnessSeconds)*time.Second && now.Sub(*evidence.LastObservedAt) <= time.Duration(config.AccountingFreshnessSeconds)*time.Second
}

func endpointCommandProgress(snapshot domain.EndpointResponseSnapshot, now time.Time) (sohaapi.NetworkEndpointResponseEventStatus, string) {
	if len(snapshot.Commands) == 0 {
		return "", ""
	}
	command := snapshot.Commands[len(snapshot.Commands)-1]
	if command.Status == domain.SessionCommandPending || command.Status == domain.SessionCommandDelivered {
		if now.Before(command.ExpiresAt) {
			return sohaapi.NetworkEndpointResponseEventStatusEnforcing, "nas_command_pending"
		}
		return sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown, "nas_command_expired"
	}
	if command.Status != domain.SessionCommandApplied || command.CompletedAt == nil {
		return sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown, "nas_command_failed"
	}
	evidence := snapshot.Observation
	if command.Action == domain.SessionActionDisconnect {
		if nasSessionStoppedAfter(evidence, command.CreatedAt) {
			return sohaapi.NetworkEndpointResponseEventStatusDisconnected, "nas_disconnect_verified"
		}
	} else if freshNASEvidence(*snapshot.Policy.PublishedConfiguration, evidence, now) && evidence.LastObservedAt.After(*command.CompletedAt) && command.RadiusAttributes != nil && evidence.VLANID == command.RadiusAttributes.VLANID && evidence.FilterID == command.RadiusAttributes.FilterID {
		return sohaapi.NetworkEndpointResponseEventStatusRestricted, "nas_restriction_verified"
	}
	return sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown, "nas_command_unverified"
}

func endpointRestoreAllowed(snapshot domain.EndpointResponseSnapshot, now time.Time) error {
	config := snapshot.Policy.PublishedConfiguration
	if config == nil || !EndpointRecoveryReady(*config, snapshot.Presence, now) || !snapshot.CredentialActive || snapshot.Presence.PolicyVersion != snapshot.AccessPolicies.PolicyVersion || snapshot.Device.OwnerUserID != snapshot.Subject.UserID {
		return responseRecoveryConflict("endpoint_recovery_not_ready")
	}
	preview := EvaluateAdmission(AdmissionInput{SubjectUserID: snapshot.Subject.UserID, DeviceID: snapshot.Device.ID, SiteID: snapshot.Site.ID, Mode: domain.ModeInternalDirect}, snapshot.Subject, snapshot.Device, snapshot.Site, snapshot.AccessPolicies)
	if preview.Decision != domain.DecisionAllow {
		return responseRecoveryConflict("endpoint_recovery_authorization_denied")
	}
	if snapshot.Event != nil && snapshot.Event.RestrictionProfile != "" && len(snapshot.Commands) == 0 &&
		!nasSessionStoppedAfter(snapshot.Observation, snapshot.Event.TriggeredAt) {
		return responseRecoveryConflict("endpoint_recovery_nas_unverified")
	}
	for _, command := range snapshot.Commands {
		if command.Status == domain.SessionCommandDelivered && now.Before(command.ExpiresAt) {
			return responseRecoveryConflict("endpoint_response_command_in_flight")
		}
		if command.Status != domain.SessionCommandPending {
			// A later, strongly bound Stop proves that this old session ended,
			// including restrictions applied by earlier stages of the same event.
			if nasSessionStoppedAfter(snapshot.Observation, command.CreatedAt) {
				continue
			}
			status, _ := endpointCommandProgress(domain.EndpointResponseSnapshot{Commands: []domain.SessionCommand{command}, Observation: snapshot.Observation, Policy: snapshot.Policy}, now)
			if status != sohaapi.NetworkEndpointResponseEventStatusRestricted && status != sohaapi.NetworkEndpointResponseEventStatusDisconnected {
				return responseRecoveryConflict("endpoint_recovery_nas_unverified")
			}
		}
	}
	return nil
}

func nasSessionStoppedAfter(evidence domain.NASSessionObservation, after time.Time) bool {
	return evidence.State == sohaapi.NetworkNASSessionEvidenceStateStopped && evidence.StoppedAt != nil && !evidence.StoppedAt.Before(after)
}

func restoreEndpointEvent(snapshot domain.EndpointResponseSnapshot, now time.Time, reason string) *domain.EndpointResponseMutation {
	event := *snapshot.Event
	event.Revision++
	event.Status = sohaapi.NetworkEndpointResponseEventStatusRestored
	event.ReasonCode = reason
	event.RestoredAt = &now
	event.UpdatedAt = now
	event.RestrictionProfile = ""
	if event.NotificationState == sohaapi.NetworkEndpointResponseNotificationStatePending {
		event.NotificationState = sohaapi.NetworkEndpointResponseNotificationStateFailed
		event.NotificationReason = "notification_cancelled_on_restore"
	}
	return &domain.EndpointResponseMutation{Event: event}
}

func responseRecoveryConflict(reason string) error {
	return apperrors.NewBusiness(apperrors.ErrConflict, reason, "Recovery requires current authorization, continuous endpoint health and verified NAS execution.", "恢复需要当前授权、连续健康心跳和已验证的 NAS 执行结果。")
}

func (s *EndpointResponseService) deliverNotifications(ctx context.Context) error {
	for range 20 {
		item, err := s.store.ClaimEndpointNotification(ctx, s.now().UTC())
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		deliveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = s.notifications.DeliverEndpointNotification(deliveryCtx, item.ID, item.NotificationPolicyID, map[string]string{"source": "network_endpoint", "deviceId": item.DeviceID, "siteId": item.SiteID, "sessionId": item.SessionID, "reasonCode": item.ReasonCode})
		cancel()
		if completeErr := s.store.CompleteEndpointNotification(ctx, item.ID, item.NotificationAttempts, err == nil, s.now().UTC()); completeErr != nil {
			return completeErr
		}
	}
	return nil
}
