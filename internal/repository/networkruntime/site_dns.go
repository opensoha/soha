package networkruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func gatewayDNSRequests(tx *gorm.DB, gateway gatewayRuntimeRow, credential credentialRow) ([]networkprotocol.DNSResolutionRequest, error) {
	if !credentialHasCapability(credential, networkprotocol.CapabilitySiteDNS) || len(gateway.DNSServers) == 0 {
		return nil, nil
	}
	var resources []struct{ ID, Target string }
	if err := tx.Raw(`SELECT r.id,r.target FROM network_access_resources r JOIN network_access_spaces s ON s.id=r.space_id
 WHERE r.tenant_id='default' AND r.workspace_id='default' AND s.tenant_id=r.tenant_id AND s.workspace_id=r.workspace_id
 AND s.site_id=? AND s.status='active' AND r.kind='fqdn' ORDER BY r.id LIMIT 129`, gateway.SiteID).Scan(&resources).Error; err != nil {
		return nil, err
	}
	if len(resources) > 128 {
		return nil, apperrors.ErrServiceUnavailable
	}
	requests := make([]networkprotocol.DNSResolutionRequest, 0, len(resources))
	for _, r := range resources {
		requests = append(requests, networkprotocol.DNSResolutionRequest{ResourceID: r.ID, SiteID: gateway.SiteID, Hostname: r.Target, Resolvers: slices.Clone(gateway.DNSServers)})
	}
	return requests, nil
}

func (r *Repository) SaveDNSObservations(ctx context.Context, runtimeID string, batch networkprotocol.DNSObservationBatch, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockWireGuardMutations(tx); err != nil {
			return err
		}
		credential, err := activeRuntimeCredential(tx, runtimeID, now)
		if err != nil {
			return err
		}
		if credential.RuntimeKind != "gateway" || !credentialHasCapability(credential, networkprotocol.CapabilitySiteDNS) {
			return apperrors.ErrAccessDenied
		}
		configuration, err := latestConfiguration(tx, runtimeID)
		if err != nil {
			return err
		}
		if configuration.ConfigurationVersion != batch.ConfigurationVersion || configuration.PolicyVersion != batch.PolicyVersion || !configuration.ValidUntil.After(now) || len(batch.Observations) > 128 {
			return apperrors.ErrConflict
		}
		requests := map[string]networkprotocol.DNSResolutionRequest{}
		for _, request := range configuration.Desired.DNSRequests {
			requests[request.ResourceID] = request
		}
		seen := map[string]bool{}
		for _, observation := range batch.Observations {
			request, ok := requests[observation.ResourceID]
			if !ok || seen[observation.ResourceID] || observation.Hostname != request.Hostname {
				return apperrors.ErrInvalidArgument
			}
			seen[observation.ResourceID] = true
			if err := saveSiteDNSObservation(tx, runtimeID, credential.ID, batch, request, observation, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func saveSiteDNSObservation(tx *gorm.DB, runtimeID, credentialID string, batch networkprotocol.DNSObservationBatch, request networkprotocol.DNSResolutionRequest, observation networkprotocol.DNSResolutionObservation, now time.Time) error {
	resource, expires, raw, fingerprint, err := validatedDNSObservation(tx, runtimeID, request, observation, now)
	if err != nil {
		return err
	}

	var previous []struct {
		Fingerprint string
		RuntimeID   string
		ObservedAt  time.Time
	}
	if err := tx.Raw(`SELECT fingerprint,runtime_id,observed_at FROM network_site_dns_mappings WHERE tenant_id='default' AND workspace_id='default' AND resource_id=? FOR UPDATE`, resource.ID).Scan(&previous).Error; err != nil {
		return err
	}
	for _, old := range previous {
		if old.RuntimeID == runtimeID && !observation.ObservedAt.After(old.ObservedAt) {
			if observation.ObservedAt.Equal(old.ObservedAt) && old.Fingerprint != fingerprint {
				return apperrors.ErrConflict
			}
			return nil
		}
	}
	for _, old := range previous {
		if old.Fingerprint != fingerprint {
			if err := revokeDNSResource(tx, resource.ID, now); err != nil {
				return err
			}
			break
		}
	}
	return tx.Exec(`INSERT INTO network_site_dns_mappings(tenant_id,workspace_id,resource_id,runtime_id,credential_id,site_id,hostname,addresses,fingerprint,policy_version,configuration_version,resolvers,observed_at,expires_at)
 VALUES('default','default',?,?,?,?,?,?::jsonb,?,?,?,?::jsonb,?,?) ON CONFLICT(tenant_id,workspace_id,resource_id,runtime_id) DO UPDATE SET credential_id=EXCLUDED.credential_id,site_id=EXCLUDED.site_id,hostname=EXCLUDED.hostname,addresses=EXCLUDED.addresses,fingerprint=EXCLUDED.fingerprint,policy_version=EXCLUDED.policy_version,configuration_version=EXCLUDED.configuration_version,resolvers=EXCLUDED.resolvers,observed_at=EXCLUDED.observed_at,expires_at=EXCLUDED.expires_at
 WHERE network_site_dns_mappings.observed_at<EXCLUDED.observed_at`, resource.ID, runtimeID, credentialID, request.SiteID, request.Hostname, string(raw), fingerprint, batch.PolicyVersion, batch.ConfigurationVersion, jsonString(request.Resolvers), observation.ObservedAt, expires).Error
}

func revokeDNSResource(tx *gorm.DB, resourceID string, now time.Time) error {
	var sessions []struct{ ID, RuntimeID string }
	if err := tx.Raw(`SELECT DISTINCT s.id,s.runtime_id FROM network_runtime_sessions s JOIN network_runtime_leases l ON l.session_id=s.id AND l.tenant_id=s.tenant_id AND l.workspace_id=s.workspace_id
 WHERE s.tenant_id='default' AND s.workspace_id='default' AND s.status IN ('pending','active','restricted','quarantine')
 AND l.status IN ('issued','active','revoking') AND l.lease_kind='resource' AND l.resource_ids @> ?::jsonb LIMIT 1025`, fmt.Sprintf("[%q]", resourceID)).Scan(&sessions).Error; err != nil {
		return err
	}
	if len(sessions) > 1024 {
		return apperrors.ErrServiceUnavailable
	}
	if len(sessions) != 0 {
		ids, runtimes := make([]string, 0, len(sessions)), make([]string, 0, len(sessions))
		for _, session := range sessions {
			ids, runtimes = append(ids, session.ID), append(runtimes, session.RuntimeID)
		}
		if err := revokeDNSResourceSessions(tx, ids, runtimes, now); err != nil {
			return err
		}
	}
	if err := tx.Exec(`UPDATE network_vpn_connection_intents i SET status='rejected',token_hash=NULL FROM network_vpn_documents p
 WHERE i.tenant_id='default' AND i.workspace_id='default' AND i.status='issued' AND p.kind='profile' AND p.id=i.profile_id
 AND p.tenant_id=i.tenant_id AND p.workspace_id=i.workspace_id AND p.published_configuration->'resourceIds' @> ?::jsonb`, fmt.Sprintf("[%q]", resourceID)).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_access_grants SET status='revoked',token_hash=NULL,revoked_at=?,reason_code='resource_dns_changed',updated_at=?
 WHERE tenant_id='default' AND workspace_id='default' AND status='issued' AND resource_ids @> ?::jsonb`, now, now, fmt.Sprintf("[%q]", resourceID)).Error
}

func revokeDNSResourceSessions(tx *gorm.DB, ids, runtimes []string, now time.Time) error {
	if err := tx.Exec(`UPDATE network_runtime_sessions SET status='revoked',revoked_at=?,revoke_reason='resource_dns_changed',updated_at=?
 WHERE tenant_id='default' AND workspace_id='default' AND id IN ? AND status IN ('pending','active','restricted','quarantine')`, now, now, ids).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE network_runtime_leases SET status='revoked',revoked_at=?,revoke_reason='resource_dns_changed',updated_at=?
 WHERE tenant_id='default' AND workspace_id='default' AND session_id IN ? AND status IN ('issued','active','revoking')`, now, now, ids).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE network_wireguard_peers SET status='revoked',revoked_at=?,revoke_reason='resource_dns_changed',updated_at=?
 WHERE tenant_id='default' AND workspace_id='default' AND session_id IN ? AND status='active'`, now, now, ids).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_runtime_configurations SET valid_until=LEAST(valid_until,?)
 WHERE tenant_id='default' AND workspace_id='default' AND (runtime_id IN ? OR runtime_id IN
 (SELECT g.runtime_id FROM network_wireguard_peers p JOIN network_access_gateways g ON g.id=p.gateway_id AND g.tenant_id=p.tenant_id AND g.workspace_id=p.workspace_id
 WHERE p.tenant_id='default' AND p.workspace_id='default' AND p.session_id IN ?))`, now, runtimes, ids).Error
}

func limitDNSConnection(tx *gorm.DB, connection *runtime.VPNConnection) error {
	until, err := resourceDNSDeadline(tx, connection.ResourceIDs, connection.ValidUntil)
	if err != nil {
		return err
	}
	until, err = managementProofDeadline(tx, connection.SiteID, connection.DeviceID, until)
	if err != nil {
		return err
	}
	connection.ValidUntil = until
	return nil
}

func resourceDNSDeadline(tx *gorm.DB, ids []string, until time.Time) (time.Time, error) {
	for _, id := range ids {
		resource, err := accessrepo.New(tx).GetResource(tx.Statement.Context, id)
		if err != nil {
			return time.Time{}, err
		}
		if resource.Kind != "fqdn" {
			continue
		}
		if len(resource.TrustedAddresses) == 0 || !resource.DNSExpiresAt.After(time.Now()) {
			return time.Time{}, staleVPNResourceScope()
		}
		until = minTime(until, resource.DNSExpiresAt)
	}
	return until, nil
}

func sessionDNSDeadline(tx *gorm.DB, sessionID string, until, now time.Time) (time.Time, error) {
	_, leases, err := activeSessionLeases(tx, sessionID, now)
	if err != nil {
		return time.Time{}, err
	}
	for _, lease := range leases {
		until, err = resourceDNSDeadline(tx, lease.ResourceIDs, until)
		if err != nil {
			return time.Time{}, err
		}
	}
	return until, nil
}

func managementProofDeadline(tx *gorm.DB, siteID, deviceID string, until time.Time) (time.Time, error) {
	site, err := accessrepo.New(tx).GetSite(tx.Statement.Context, siteID)
	if err != nil {
		return time.Time{}, err
	}
	device, err := accessrepo.New(tx).GetDevice(tx.Statement.Context, deviceID)
	if err != nil {
		return time.Time{}, err
	}
	if site.DeviceGovernance == domain.DeviceGovernanceVerified && (device.ManagementVerifiedUntil == nil || device.ManagementIssuerKey != site.ManagementIssuerKey || !device.ManagementVerifiedUntil.After(time.Now())) {
		return time.Time{}, apperrors.ErrAccessDenied
	}
	if device.ManagementVerifiedUntil != nil {
		return minTime(until, *device.ManagementVerifiedUntil), nil
	}
	return until, nil
}

func jsonString(value any) string { raw, _ := json.Marshal(value); return string(raw) }
func currentDNSResolvers(tx *gorm.DB, runtimeID string, resolvers []string) bool {
	var valid bool
	if tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_access_gateways WHERE tenant_id='default' AND workspace_id='default' AND runtime_id=? AND administrative_status='active' AND dns_servers=?::jsonb)`, runtimeID, jsonString(resolvers)).Scan(&valid).Error != nil {
		return false
	}
	return valid
}

func validatedDNSObservation(tx *gorm.DB, runtimeID string, request networkprotocol.DNSResolutionRequest, observation networkprotocol.DNSResolutionObservation, now time.Time) (domain.Resource, time.Time, []byte, string, error) {
	if observation.TTLSeconds < 1 || observation.TTLSeconds > 300 || observation.ObservedAt.After(now.Add(10*time.Second)) || observation.ObservedAt.Before(now.Add(-time.Minute)) {
		return domain.Resource{}, time.Time{}, nil, "", apperrors.ErrInvalidArgument
	}
	expires := observation.ObservedAt.Add(time.Duration(observation.TTLSeconds) * time.Second)
	if !expires.After(now) {
		return domain.Resource{}, time.Time{}, nil, "", apperrors.ErrConflict
	}
	resource, err := accessrepo.New(tx).GetResource(tx.Statement.Context, request.ResourceID)
	if err != nil {
		return domain.Resource{}, time.Time{}, nil, "", err
	}
	space, err := runtimeSpace(tx, resource.SpaceID)
	if err != nil {
		return domain.Resource{}, time.Time{}, nil, "", err
	}
	if resource.Kind != "fqdn" || resource.Target != request.Hostname || space.SiteID != request.SiteID || !currentDNSResolvers(tx, runtimeID, request.Resolvers) {
		return domain.Resource{}, time.Time{}, nil, "", apperrors.ErrConflict
	}
	resource.TrustedAddresses, resource.DNSExpiresAt = observation.Addresses, expires
	if _, valid := domain.WireGuardResourceTargets(resource, space); !valid {
		return domain.Resource{}, time.Time{}, nil, "", apperrors.ErrInvalidArgument
	}
	addresses := slices.Clone(observation.Addresses)
	slices.Sort(addresses)
	if len(addresses) != len(slices.Compact(slices.Clone(addresses))) {
		return domain.Resource{}, time.Time{}, nil, "", apperrors.ErrInvalidArgument
	}
	raw, err := json.Marshal(addresses)
	if err != nil {
		return domain.Resource{}, time.Time{}, nil, "", err
	}
	fingerprint := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	return resource, expires, raw, fingerprint, nil
}
