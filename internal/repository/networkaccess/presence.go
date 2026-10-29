package networkaccess

import (
	"context"
	"database/sql"
	"errors"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const presenceColumns = `device_id, runtime_id, credential_id, boot_id, sequence, payload_hash, presence_epoch,
    service_state, ui_state, configuration_version, policy_version, observed_at, received_at,
    healthy_since, healthy_samples, ui_observed_at, ui_healthy_since, ui_healthy_samples, heartbeat_interval_seconds`

func scanPresence(row scanner) (domain.PresenceRecord, error) {
	var item domain.PresenceRecord
	err := row.Scan(&item.DeviceID, &item.RuntimeID, &item.CredentialID, &item.BootID, &item.Sequence, &item.PayloadHash, &item.Epoch,
		&item.ServiceState, &item.UIState, &item.ConfigurationVersion, &item.PolicyVersion, &item.LastObservedAt, &item.LastReceivedAt,
		&item.HealthySince, &item.HealthySamples, &item.UILastObservedAt, &item.UIHealthySince, &item.UIHealthySamples, &item.HeartbeatIntervalSeconds)
	return item, err
}

func (r *Repository) EndpointPresence(ctx context.Context, deviceID string) (domain.PresenceRecord, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+presenceColumns+` FROM network_endpoint_presence WHERE tenant_id = 'default' AND workspace_id = 'default' AND device_id = ?`, deviceID).Row(), scanPresence, "endpoint presence")
}

func (r *Repository) SaveEndpointPresence(ctx context.Context, incoming domain.PresenceRecord) (domain.PresenceRecord, error) {
	var saved domain.PresenceRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-endpoint-device:"+incoming.DeviceID).Error; err != nil {
			return err
		}
		current, err := scanPresence(tx.Raw(`SELECT `+presenceColumns+` FROM network_endpoint_presence WHERE tenant_id = 'default' AND workspace_id = 'default' AND device_id = ? FOR UPDATE`, incoming.DeviceID).Row())
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		newBoot := errors.Is(err, sql.ErrNoRows) || current.CredentialID != incoming.CredentialID || current.BootID != incoming.BootID
		if !newBoot && incoming.Sequence == current.Sequence && incoming.PayloadHash == current.PayloadHash {
			saved = current
			return nil
		}
		if err == nil && (!incoming.LastObservedAt.After(*current.LastObservedAt) || !incoming.LastReceivedAt.After(*current.LastReceivedAt) || !newBoot && incoming.Sequence <= current.Sequence) {
			return stalePresence()
		}
		incoming.Epoch = current.Epoch
		if newBoot {
			if err := recordPresenceBoot(tx, incoming, current); err != nil {
				return err
			}
			incoming.Epoch++
		}
		advancePresenceHealth(&incoming, current, newBoot)
		if err := tx.Exec(`INSERT INTO network_endpoint_presence
            (device_id, runtime_id, credential_id, boot_id, sequence, payload_hash, presence_epoch,
             service_state, ui_state, configuration_version, policy_version, observed_at, received_at,
             healthy_since, healthy_samples, ui_observed_at, ui_healthy_since, ui_healthy_samples, heartbeat_interval_seconds)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT (tenant_id, workspace_id, device_id) DO UPDATE SET
             runtime_id=EXCLUDED.runtime_id, credential_id=EXCLUDED.credential_id, boot_id=EXCLUDED.boot_id,
             sequence=EXCLUDED.sequence, payload_hash=EXCLUDED.payload_hash, presence_epoch=EXCLUDED.presence_epoch,
             service_state=EXCLUDED.service_state, ui_state=EXCLUDED.ui_state,
             configuration_version=EXCLUDED.configuration_version, policy_version=EXCLUDED.policy_version,
             observed_at=EXCLUDED.observed_at, received_at=EXCLUDED.received_at,
             healthy_since=EXCLUDED.healthy_since, healthy_samples=EXCLUDED.healthy_samples,
             ui_observed_at=EXCLUDED.ui_observed_at, ui_healthy_since=EXCLUDED.ui_healthy_since, ui_healthy_samples=EXCLUDED.ui_healthy_samples,
             heartbeat_interval_seconds=EXCLUDED.heartbeat_interval_seconds`,
			incoming.DeviceID, incoming.RuntimeID, incoming.CredentialID, incoming.BootID, incoming.Sequence, incoming.PayloadHash, incoming.Epoch,
			incoming.ServiceState, incoming.UIState, incoming.ConfigurationVersion, incoming.PolicyVersion, incoming.LastObservedAt, incoming.LastReceivedAt,
			incoming.HealthySince, incoming.HealthySamples, incoming.UILastObservedAt, incoming.UIHealthySince, incoming.UIHealthySamples, incoming.HeartbeatIntervalSeconds).Error; err != nil {
			return err
		}
		saved = incoming
		return nil
	})
	return saved, err
}

func recordPresenceBoot(tx *gorm.DB, incoming, current domain.PresenceRecord) error {
	var exists bool
	if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM network_endpoint_presence_boots WHERE tenant_id = 'default' AND workspace_id = 'default' AND credential_id = ? AND boot_id = ?)`, incoming.CredentialID, incoming.BootID).Scan(&exists).Error; err != nil {
		return err
	}
	if exists {
		return stalePresence()
	}
	var bootCount int64
	if err := tx.Raw(`SELECT count(*) FROM network_endpoint_presence_boots WHERE tenant_id = 'default' AND workspace_id = 'default' AND credential_id = ?`, incoming.CredentialID).Scan(&bootCount).Error; err != nil {
		return err
	}
	if bootCount >= 128 {
		return apperrors.NewBusiness(apperrors.ErrConflict, "endpoint_presence_boot_limit", "Re-enroll the endpoint after excessive service restarts.", "保护服务重启次数超限，请重新登记终端。")
	}
	if err := tx.Exec(`UPDATE network_endpoint_presence_boots SET retired_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND credential_id = ? AND boot_id = ? AND retired_at IS NULL`, incoming.LastReceivedAt, current.CredentialID, current.BootID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO network_endpoint_presence_boots (credential_id, boot_id) VALUES (?, ?)`, incoming.CredentialID, incoming.BootID).Error; err != nil {
		return err
	}
	return nil
}

func advancePresenceHealth(incoming *domain.PresenceRecord, current domain.PresenceRecord, newBoot bool) {
	interval := time.Duration(incoming.HeartbeatIntervalSeconds) * time.Second
	continuous := !newBoot && current.LastReceivedAt != nil && incoming.LastReceivedAt.Sub(*current.LastReceivedAt) <= 2*interval && incoming.LastObservedAt.Sub(*current.LastObservedAt) <= 2*interval && incoming.PolicyVersion == current.PolicyVersion && incoming.ConfigurationVersion == current.ConfigurationVersion
	if incoming.ServiceState == sohaapi.NetworkEndpointPresenceServiceStateHealthy {
		incoming.HealthySince, incoming.HealthySamples = incoming.LastReceivedAt, 1
		if continuous && current.ServiceState == sohaapi.NetworkEndpointPresenceServiceStateHealthy {
			incoming.HealthySince, incoming.HealthySamples = current.HealthySince, current.HealthySamples
			if incoming.LastReceivedAt.Sub(*current.LastReceivedAt) >= interval/2 {
				incoming.HealthySamples++
			}
		}
	}
	if incoming.UIState == sohaapi.NetworkEndpointPresenceUIStateOnline && incoming.UILastObservedAt != nil {
		incoming.UIHealthySince, incoming.UIHealthySamples = incoming.LastReceivedAt, 1
		if continuous && current.UIState == sohaapi.NetworkEndpointPresenceUIStateOnline && current.UILastObservedAt != nil && incoming.UILastObservedAt.After(*current.UILastObservedAt) {
			incoming.UIHealthySince, incoming.UIHealthySamples = current.UIHealthySince, current.UIHealthySamples
			if incoming.LastReceivedAt.Sub(*current.LastReceivedAt) >= interval/2 {
				incoming.UIHealthySamples++
			}
		}
	}
}

func stalePresence() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "endpoint_presence_stale", "The endpoint presence is stale or belongs to a retired service incarnation.", "终端心跳已陈旧或来自已退役的保护服务实例。")
}
