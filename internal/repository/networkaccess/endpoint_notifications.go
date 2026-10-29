package networkaccess

import (
	"context"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"gorm.io/gorm"
)

func (r *Repository) EndpointMaintenanceByID(ctx context.Context, id string) (domain.EndpointMaintenance, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT id, device_id, site_id, reason, actor_id, created_at, expires_at FROM network_endpoint_maintenance_windows WHERE tenant_id='default' AND workspace_id='default' AND id=?`, id).Row(), scanMaintenance, "endpoint maintenance")
}

func (r *Repository) ClaimEndpointNotification(ctx context.Context, now time.Time) (domain.EndpointResponseRecord, error) {
	var claimed domain.EndpointResponseRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := scanOne(tx.Raw(responseEventSelect+` AND restored_at IS NULL AND event->>'notificationState' IN ('pending','failed') AND notification_attempts<3 AND (notification_lease_until IS NULL OR notification_lease_until<=?) ORDER BY triggered_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Row(), scanResponseEvent, "pending endpoint notification")
		if err != nil {
			return err
		}
		item.NotificationAttempts++
		lease := now.Add(time.Minute)
		item.NotificationLeaseUntil = &lease
		if err := tx.Exec(`UPDATE network_endpoint_response_events SET notification_attempts=?,notification_lease_until=? WHERE tenant_id='default' AND workspace_id='default' AND id=?`, item.NotificationAttempts, lease, item.ID).Error; err != nil {
			return err
		}
		claimed = item
		return nil
	})
	return claimed, err
}

func (r *Repository) CompleteEndpointNotification(ctx context.Context, id string, attempt int, delivered bool, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := scanOne(tx.Raw(responseEventSelect+` AND id=? FOR UPDATE`, id).Row(), scanResponseEvent, "endpoint notification")
		if err != nil {
			return err
		}
		// A claimed notification can finish after recovery; delivery logs retain that attempt.
		if item.RestoredAt != nil {
			return nil
		}
		if item.NotificationAttempts != attempt || item.NotificationLeaseUntil == nil || now.After(*item.NotificationLeaseUntil) {
			return responseRevisionConflict()
		}
		if delivered {
			item.NotificationState = "delivered"
			item.NotificationReason = "notification_delivered"
		} else {
			item.NotificationState = "failed"
			item.NotificationReason = "notification_delivery_failed"
		}
		item.Revision++
		item.UpdatedAt = now
		raw, err := marshalResponseEvent(item.EndpointResponseEvent)
		if err != nil {
			return err
		}
		return tx.Exec(`UPDATE network_endpoint_response_events SET event=?::jsonb,revision=?,updated_at=? WHERE tenant_id='default' AND workspace_id='default' AND id=?`, raw, item.Revision, now, id).Error
	})
}
