package networkaccess

import (
	"context"
	"encoding/json"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

// DNS is accepted only from a current, site-bound gateway credential. Conflicting
// fresh gateway answers are unusable rather than selected by arrival order.
func (r *Repository) ResolveResourceDNS(ctx context.Context, resource *domain.Resource) error {
	if resource.Kind != "fqdn" {
		return nil
	}
	var rows []struct {
		Addresses   []byte
		Fingerprint string
		ExpiresAt   time.Time
	}
	err := r.db.WithContext(ctx).Raw(`SELECT m.addresses, m.fingerprint, m.expires_at FROM network_site_dns_mappings m
 JOIN network_runtime_credentials c ON c.id=m.credential_id AND c.runtime_id=m.runtime_id AND c.runtime_kind='gateway'
 JOIN network_access_gateways g ON g.runtime_id=m.runtime_id AND g.site_id=m.site_id AND g.administrative_status='active' AND g.dns_servers=m.resolvers
 JOIN network_access_spaces s ON s.id=? AND s.site_id=m.site_id
 WHERE m.tenant_id='default' AND m.workspace_id='default' AND m.resource_id=? AND m.hostname=?
 AND c.tenant_id=m.tenant_id AND c.workspace_id=m.workspace_id AND c.status='active' AND c.revoked_at IS NULL
 AND c.not_before<=CURRENT_TIMESTAMP AND c.expires_at>CURRENT_TIMESTAMP AND m.expires_at>CURRENT_TIMESTAMP
 ORDER BY m.runtime_id LIMIT 33`, resource.SpaceID, resource.ID, resource.Target).Scan(&rows).Error
	if err != nil {
		return err
	}
	resource.TrustedAddresses, resource.DNSExpiresAt = nil, time.Time{}
	if len(rows) == 0 || len(rows) > 32 {
		return nil
	}
	for _, row := range rows {
		if row.Fingerprint != rows[0].Fingerprint {
			return nil
		}
		if resource.DNSExpiresAt.IsZero() || row.ExpiresAt.Before(resource.DNSExpiresAt) {
			resource.DNSExpiresAt = row.ExpiresAt
		}
	}
	return json.Unmarshal(rows[0].Addresses, &resource.TrustedAddresses)
}
