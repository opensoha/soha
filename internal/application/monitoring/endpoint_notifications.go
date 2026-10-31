package monitoring

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ValidateEndpointNotification(ctx context.Context, principal domainidentity.Principal, id string) error {
	policies, err := s.ListNotificationPolicies(ctx, principal)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		if policy.ID == id && policy.Enabled && len(policy.ChannelRefs) > 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: notification policy is missing, disabled or has no channels", apperrors.ErrInvalidArgument)
}

func (s *Service) DeliverEndpointNotification(ctx context.Context, eventID, policyID string, labels map[string]string) error {
	policies, err := s.notificationPolicies.ListNotificationPolicies(ctx)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		if policy.ID != policyID || !policy.Enabled {
			continue
		}
		now := time.Now().UTC()
		event := domainalert.AlertEvent{ID: eventID, SourceType: "network_endpoint", SourceSystem: "soha", Fingerprint: eventID, Title: "Endpoint network access response", Summary: "Endpoint health requires a network access response.", Severity: "warning", Status: "firing", Labels: labels, StartsAt: now, LastSeenAt: now, CreatedAt: now, UpdatedAt: now}
		outputs := s.buildNotificationOutputs(ctx, policy, event)
		if len(outputs) == 0 {
			return fmt.Errorf("%w: notification route has no output", apperrors.ErrServiceUnavailable)
		}
		delivered := true
		for _, output := range outputs {
			status := "failed"
			if output["status"] != "preview_failed" {
				status, _, _ = s.sendNotificationOutput(ctx, output)
			}
			if status != "sent" && status != "delivered" {
				delivered = false
			}
			if err := s.deliveryLogs.CreateDeliveryLog(ctx, domainalert.DeliveryLog{ID: "delivery:" + uuid.NewString(), AlertID: eventID, ChannelID: stringValue(output["channelId"], ""), Status: status, Summary: "Endpoint response notification delivery", Metadata: map[string]any{"policyId": policyID}, CreatedAt: time.Now().UTC()}); err != nil {
				return err
			}
		}
		if !delivered {
			return fmt.Errorf("%w: endpoint notification delivery failed", apperrors.ErrServiceUnavailable)
		}
		return nil
	}
	return fmt.Errorf("%w: endpoint notification policy unavailable", apperrors.ErrServiceUnavailable)
}
