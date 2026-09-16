package networkaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

const gatewayColumns = `g.id, COALESCE(g.runtime_id, ''), g.site_id, g.name, g.administrative_status, g.status,
	g.public_endpoint_host, g.public_endpoint_port, COALESCE(g.overlay_cidr::text, ''), g.routing_mode, g.mtu,
	g.persistent_keepalive_seconds, g.dns_servers, COALESCE(g.hub_gateway_id, ''), g.advertised_cidrs,
	COALESCE((SELECT c.wireguard_public_key FROM network_runtime_credentials AS c
		WHERE c.tenant_id = g.tenant_id AND c.workspace_id = g.workspace_id AND c.runtime_id = g.runtime_id
		  AND c.runtime_kind = 'gateway' AND c.status = 'active' ORDER BY c.generation DESC LIMIT 1), ''),
	g.version, g.capabilities, g.policy_version, g.applied_at, g.last_heartbeat_at, g.created_at, g.updated_at,
	g.region, g.provider_code, g.provider_name, g.selection_priority, g.accept_new_connections, g.max_sessions, g.probe_url`

const deviceColumns = `id, owner_user_id, name, hostname, platform, device_type, ownership_type,
	COALESCE(site_id, ''), status, posture_status, posture_version, credential_generation, last_seen_at,
	reported_facts, created_at, updated_at`

func (r *Repository) ListDevices(ctx context.Context, filter domainnetworkaccess.DeviceFilter) ([]domainnetworkaccess.Device, error) {
	query := `SELECT ` + deviceColumns + ` FROM network_access_devices WHERE tenant_id = 'default' AND workspace_id = 'default'`
	args := []any{}
	query, args = addSearch(query, args, filter.Search, "name", "hostname", "platform", "device_type", "ownership_type")
	query, args = addFilter(query, args, "owner_user_id", filter.OwnerUserID)
	query, args = addFilter(query, args, "site_id", filter.SiteID)
	query, args = addFilter(query, args, "status", filter.Status)
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanDevice)
}

func (r *Repository) GetDevice(ctx context.Context, id string) (domainnetworkaccess.Device, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+deviceColumns+` FROM network_access_devices WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? LIMIT 1`, id).Row(), scanDevice, "endpoint device")
}

func (r *Repository) RegisterDevice(ctx context.Context, item domainnetworkaccess.Device) (domainnetworkaccess.Device, error) {
	if item.DeviceType == "" {
		item.DeviceType = domainnetworkaccess.DeviceTypeUnknown
	}
	if item.OwnershipType == "" {
		item.OwnershipType = domainnetworkaccess.DeviceOwnershipUnassigned
	}
	reportedFacts := ""
	if item.ReportedFacts != nil {
		raw, err := json.Marshal(item.ReportedFacts)
		if err != nil {
			return domainnetworkaccess.Device{}, fmt.Errorf("encode endpoint device facts: %w", err)
		}
		reportedFacts = string(raw)
	}
	result := r.db.WithContext(ctx).Exec(`INSERT INTO network_access_devices
		(id, owner_user_id, name, hostname, platform, device_type, ownership_type, status, posture_status, last_seen_at, reported_facts, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 'unknown', ?, NULLIF(?, '')::jsonb, ?, ?)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, hostname = EXCLUDED.hostname,
			platform = EXCLUDED.platform,
			device_type = CASE WHEN network_access_devices.device_type = 'unknown' THEN EXCLUDED.device_type ELSE network_access_devices.device_type END,
			last_seen_at = EXCLUDED.last_seen_at,
			reported_facts = COALESCE(EXCLUDED.reported_facts, network_access_devices.reported_facts), updated_at = EXCLUDED.updated_at
		WHERE network_access_devices.tenant_id = 'default'
			AND network_access_devices.workspace_id = 'default'
			AND network_access_devices.owner_user_id = EXCLUDED.owner_user_id`,
		item.ID, item.OwnerUserID, item.Name, item.Hostname, item.Platform, item.DeviceType, item.OwnershipType, item.LastSeenAt, reportedFacts, item.CreatedAt, item.UpdatedAt)
	if result.Error != nil {
		return domainnetworkaccess.Device{}, normalizeDatabaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return domainnetworkaccess.Device{}, fmt.Errorf("%w: endpoint device belongs to another user", apperrors.ErrConflict)
	}
	return r.GetDevice(ctx, item.ID)
}

func (r *Repository) UpdateDevice(ctx context.Context, id string, input domainnetworkaccess.DeviceInput, updatedAt time.Time) (domainnetworkaccess.Device, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE network_access_devices SET name = ?, site_id = NULLIF(?, ''), status = ?, posture_status = COALESCE(NULLIF(?, ''), posture_status), device_type = COALESCE(NULLIF(?, ''), device_type), ownership_type = COALESCE(NULLIF(?, ''), ownership_type), updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, input.Name, input.SiteID, input.Status, input.PostureStatus, input.DeviceType, input.OwnershipType, updatedAt, id)
	if err := mutationError(result, "endpoint device"); err != nil {
		return domainnetworkaccess.Device{}, err
	}
	return r.GetDevice(ctx, id)
}

func (r *Repository) ListSites(ctx context.Context, filter domainnetworkaccess.SiteFilter) ([]domainnetworkaccess.Site, error) {
	query := `SELECT id, name, description, location, status, created_at, updated_at FROM network_access_sites WHERE tenant_id = 'default' AND workspace_id = 'default'`
	args := []any{}
	query, args = addSearch(query, args, filter.Search, "name", "description", "location")
	query, args = addFilter(query, args, "status", filter.Status)
	query += ` ORDER BY name ASC, id ASC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanSite)
}

func (r *Repository) GetSite(ctx context.Context, id string) (domainnetworkaccess.Site, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT id, name, description, location, status, created_at, updated_at FROM network_access_sites WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? LIMIT 1`, id).Row(), scanSite, "network site")
}

func (r *Repository) CreateSite(ctx context.Context, item domainnetworkaccess.Site) (domainnetworkaccess.Site, error) {
	err := r.db.WithContext(ctx).Exec(`INSERT INTO network_access_sites (id, name, description, location, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, item.ID, item.Name, item.Description, item.Location, item.Status, item.CreatedAt, item.UpdatedAt).Error
	if err != nil {
		return domainnetworkaccess.Site{}, normalizeDatabaseError(err)
	}
	return r.GetSite(ctx, item.ID)
}

func (r *Repository) UpdateSite(ctx context.Context, id string, input domainnetworkaccess.SiteInput, updatedAt time.Time) (domainnetworkaccess.Site, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE network_access_sites SET name = ?, description = ?, location = ?, status = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, input.Name, input.Description, input.Location, input.Status, updatedAt, id)
	if err := mutationError(result, "network site"); err != nil {
		return domainnetworkaccess.Site{}, err
	}
	return r.GetSite(ctx, id)
}

func (r *Repository) DeleteSite(ctx context.Context, id string) error {
	return mutationError(r.db.WithContext(ctx).Exec(`DELETE FROM network_access_sites WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, id), "network site")
}

func (r *Repository) ListSpaces(ctx context.Context, filter domainnetworkaccess.SpaceFilter) ([]domainnetworkaccess.Space, error) {
	query := `SELECT id, site_id, name, cidrs, status, created_at, updated_at FROM network_access_spaces WHERE tenant_id = 'default' AND workspace_id = 'default'`
	args := []any{}
	query, args = addSearch(query, args, filter.Search, "name")
	query, args = addFilter(query, args, "site_id", filter.SiteID)
	query, args = addFilter(query, args, "status", filter.Status)
	query += ` ORDER BY name ASC, id ASC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanSpace)
}

func (r *Repository) GetSpace(ctx context.Context, id string) (domainnetworkaccess.Space, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT id, site_id, name, cidrs, status, created_at, updated_at FROM network_access_spaces WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? LIMIT 1`, id).Row(), scanSpace, "network space")
}

func (r *Repository) CreateSpace(ctx context.Context, item domainnetworkaccess.Space) (domainnetworkaccess.Space, error) {
	cidrs, err := json.Marshal(item.CIDRs)
	if err != nil {
		return domainnetworkaccess.Space{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if item.Status == domainnetworkaccess.StatusActive {
			if err := ensureSpaceCIDRsAvailable(tx, item.ID, string(cidrs)); err != nil {
				return err
			}
		}
		return tx.Exec(`INSERT INTO network_access_spaces (id, site_id, name, cidrs, status, created_at, updated_at) VALUES (?, ?, ?, ?::jsonb, ?, ?, ?)`, item.ID, item.SiteID, item.Name, string(cidrs), item.Status, item.CreatedAt, item.UpdatedAt).Error
	})
	if err != nil {
		return domainnetworkaccess.Space{}, normalizeDatabaseError(err)
	}
	return r.GetSpace(ctx, item.ID)
}

func (r *Repository) UpdateSpace(ctx context.Context, id string, input domainnetworkaccess.SpaceInput, updatedAt time.Time) (domainnetworkaccess.Space, error) {
	cidrs, err := json.Marshal(input.CIDRs)
	if err != nil {
		return domainnetworkaccess.Space{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.Status == domainnetworkaccess.StatusActive {
			if err := ensureSpaceCIDRsAvailable(tx, id, string(cidrs)); err != nil {
				return err
			}
		}
		return mutationError(tx.Exec(`UPDATE network_access_spaces SET site_id = ?, name = ?, cidrs = ?::jsonb, status = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, input.SiteID, input.Name, string(cidrs), input.Status, updatedAt, id), "network space")
	})
	if err != nil {
		return domainnetworkaccess.Space{}, err
	}
	return r.GetSpace(ctx, id)
}

func (r *Repository) DeleteSpace(ctx context.Context, id string) error {
	return mutationError(r.db.WithContext(ctx).Exec(`DELETE FROM network_access_spaces WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, id), "network space")
}

func (r *Repository) ListResources(ctx context.Context, filter domainnetworkaccess.ResourceFilter) ([]domainnetworkaccess.Resource, error) {
	query := `SELECT id, space_id, name, kind, target, protocol, ports, protected, path_mode, created_at, updated_at FROM network_access_resources WHERE tenant_id = 'default' AND workspace_id = 'default'`
	args := []any{}
	query, args = addSearch(query, args, filter.Search, "name", "target")
	query, args = addFilter(query, args, "space_id", filter.SpaceID)
	query, args = addFilter(query, args, "kind", filter.Kind)
	if filter.Protected != nil {
		query += " AND protected = ?"
		args = append(args, *filter.Protected)
	}
	query += ` ORDER BY name ASC, id ASC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanResource)
}

func (r *Repository) GetResource(ctx context.Context, id string) (domainnetworkaccess.Resource, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT id, space_id, name, kind, target, protocol, ports, protected, path_mode, created_at, updated_at FROM network_access_resources WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? LIMIT 1`, id).Row(), scanResource, "network resource")
}

func (r *Repository) CreateResource(ctx context.Context, item domainnetworkaccess.Resource) (domainnetworkaccess.Resource, error) {
	ports, err := json.Marshal(item.Ports)
	if err != nil {
		return domainnetworkaccess.Resource{}, err
	}
	err = r.db.WithContext(ctx).Exec(`INSERT INTO network_access_resources (id, space_id, name, kind, target, protocol, ports, protected, path_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?)`, item.ID, item.SpaceID, item.Name, item.Kind, item.Target, item.Protocol, string(ports), item.Protected, item.PathMode, item.CreatedAt, item.UpdatedAt).Error
	if err != nil {
		return domainnetworkaccess.Resource{}, normalizeDatabaseError(err)
	}
	return r.GetResource(ctx, item.ID)
}

func (r *Repository) UpdateResource(ctx context.Context, id string, input domainnetworkaccess.ResourceInput, updatedAt time.Time) (domainnetworkaccess.Resource, error) {
	ports, err := json.Marshal(input.Ports)
	if err != nil {
		return domainnetworkaccess.Resource{}, err
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE network_access_resources SET space_id = ?, name = ?, kind = ?, target = ?, protocol = ?, ports = ?::jsonb, protected = ?, path_mode = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, input.SpaceID, input.Name, input.Kind, input.Target, input.Protocol, string(ports), input.Protected, input.PathMode, updatedAt, id)
	if err := mutationError(result, "network resource"); err != nil {
		return domainnetworkaccess.Resource{}, err
	}
	return r.GetResource(ctx, id)
}

func (r *Repository) DeleteResource(ctx context.Context, id string) error {
	return mutationError(r.db.WithContext(ctx).Exec(`DELETE FROM network_access_resources WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, id), "network resource")
}

func (r *Repository) ListGateways(ctx context.Context, filter domainnetworkaccess.GatewayFilter) ([]domainnetworkaccess.Gateway, error) {
	query := `SELECT ` + gatewayColumns + ` FROM network_access_gateways AS g WHERE g.tenant_id = 'default' AND g.workspace_id = 'default'`
	args := []any{}
	query, args = addSearch(query, args, filter.Search, "g.name", "g.runtime_id", "g.version")
	query, args = addFilter(query, args, "g.site_id", filter.SiteID)
	query, args = addFilter(query, args, "g.status", filter.Status)
	query += ` ORDER BY g.name ASC, g.id ASC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanGateway)
}

func (r *Repository) GetGateway(ctx context.Context, id string) (domainnetworkaccess.Gateway, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+gatewayColumns+` FROM network_access_gateways AS g WHERE g.tenant_id = 'default' AND g.workspace_id = 'default' AND g.id = ? LIMIT 1`, id).Row(), scanGateway, "network gateway")
}

func (r *Repository) CreateGateway(ctx context.Context, item domainnetworkaccess.Gateway) (domainnetworkaccess.Gateway, error) {
	dnsServers, err := json.Marshal(append([]string{}, item.DNSServers...))
	if err != nil {
		return domainnetworkaccess.Gateway{}, err
	}
	advertisedCIDRs, err := json.Marshal(append([]string{}, item.AdvertisedCIDRs...))
	if err != nil {
		return domainnetworkaccess.Gateway{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if item.AdministrativeStatus == domainnetworkaccess.StatusActive {
			if err := ensureGatewayOverlayAvailable(tx, item.ID, item.OverlayCIDR); err != nil {
				return err
			}
			if err := ensureGatewayTopology(tx, item.ID, item.SiteID, item.AdministrativeStatus, item.RoutingMode, item.HubGatewayID, string(advertisedCIDRs)); err != nil {
				return err
			}
		}
		return tx.Exec(`INSERT INTO network_access_gateways
			(id, runtime_id, site_id, name, administrative_status, status, public_endpoint_host, public_endpoint_port,
			 overlay_cidr, routing_mode, mtu, persistent_keepalive_seconds, dns_servers, hub_gateway_id, advertised_cidrs, capabilities, created_at, updated_at, region, provider_code, provider_name, selection_priority, accept_new_connections, max_sessions, probe_url)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::cidr, ?, ?, ?, ?::jsonb, NULLIF(?, ''), ?::jsonb, '[]'::jsonb, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.ID, item.RuntimeID, item.SiteID, item.Name, item.AdministrativeStatus, item.Status,
			item.PublicEndpointHost, item.PublicEndpointPort, item.OverlayCIDR, item.RoutingMode, item.MTU,
			item.PersistentKeepaliveSeconds, string(dnsServers), item.HubGatewayID, string(advertisedCIDRs), item.CreatedAt, item.UpdatedAt, item.Region, item.ProviderCode, item.ProviderName, item.SelectionPriority, item.AcceptNewConnections, item.MaxSessions, item.ProbeURL).Error
	})
	if err != nil {
		return domainnetworkaccess.Gateway{}, normalizeDatabaseError(err)
	}
	return r.GetGateway(ctx, item.ID)
}

func (r *Repository) UpdateGateway(ctx context.Context, id string, input domainnetworkaccess.GatewayInput, updatedAt time.Time) (domainnetworkaccess.Gateway, error) {
	dnsServers, err := json.Marshal(append([]string{}, input.DNSServers...))
	if err != nil {
		return domainnetworkaccess.Gateway{}, err
	}
	advertisedCIDRs, err := json.Marshal(append([]string{}, input.AdvertisedCIDRs...))
	if err != nil {
		return domainnetworkaccess.Gateway{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.AdministrativeStatus == domainnetworkaccess.StatusActive {
			if err := ensureGatewayOverlayAvailable(tx, id, input.OverlayCIDR); err != nil {
				return err
			}
		}
		if err := ensureGatewayTopology(tx, id, input.SiteID, input.AdministrativeStatus, input.RoutingMode, input.HubGatewayID, string(advertisedCIDRs)); err != nil {
			return err
		}
		return mutationError(tx.Exec(`UPDATE network_access_gateways SET runtime_id = ?, site_id = ?, name = ?, administrative_status = ?,
			public_endpoint_host = ?, public_endpoint_port = ?, overlay_cidr = ?::cidr, routing_mode = ?, mtu = ?,
			persistent_keepalive_seconds = ?, dns_servers = ?::jsonb, hub_gateway_id = NULLIF(?, ''), advertised_cidrs = ?::jsonb, updated_at = ?,
			region = COALESCE(?, region), provider_code = COALESCE(?, provider_code), provider_name = COALESCE(?, provider_name),
			selection_priority = COALESCE(?, selection_priority), accept_new_connections = COALESCE(?, accept_new_connections),
			max_sessions = COALESCE(?, max_sessions), probe_url = COALESCE(?, probe_url)
			WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`,
			input.RuntimeID, input.SiteID, input.Name, input.AdministrativeStatus, input.PublicEndpointHost,
			input.PublicEndpointPort, input.OverlayCIDR, input.RoutingMode, input.MTU,
			input.PersistentKeepaliveSeconds, string(dnsServers), input.HubGatewayID, string(advertisedCIDRs), updatedAt, input.Region, input.ProviderCode, input.ProviderName, input.SelectionPriority, input.AcceptNewConnections, input.MaxSessions, input.ProbeURL, id), "network gateway")
	})
	if err != nil {
		return domainnetworkaccess.Gateway{}, normalizeDatabaseError(err)
	}
	return r.GetGateway(ctx, id)
}

func (r *Repository) GetSubject(ctx context.Context, userID string) (domainnetworkaccess.Subject, error) {
	var subject domainnetworkaccess.Subject
	var tags []byte
	if err := r.db.WithContext(ctx).Raw(`SELECT id::text, status, tags FROM users WHERE id = ?::uuid LIMIT 1`, userID).Row().Scan(&subject.UserID, &subject.Status, &tags); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return subject, fmt.Errorf("%w: network access subject not found", apperrors.ErrNotFound)
		}
		return subject, err
	}
	if err := json.Unmarshal(tags, &subject.Tags); err != nil {
		return subject, fmt.Errorf("decode network access subject tags: %w", err)
	}
	teams, err := queryMany(ctx, r.db, `SELECT team_id FROM user_team_bindings WHERE user_id = ?::uuid ORDER BY team_id ASC`, []any{userID}, func(row scanner) (string, error) {
		var teamID string
		err := row.Scan(&teamID)
		return teamID, err
	})
	if err != nil {
		return subject, err
	}
	subject.Teams = teams
	if subject.Tags == nil {
		subject.Tags = []string{}
	}
	return subject, nil
}

const policyColumns = `id, name, enabled, priority, effect, subjects, site_ids, resource_ids, modes, device_statuses, posture_statuses, access_profile, version, created_at, updated_at`

func (r *Repository) ListPolicies(ctx context.Context, filter domainnetworkaccess.PolicyFilter) ([]domainnetworkaccess.Policy, error) {
	query := `SELECT ` + policyColumns + ` FROM network_access_policies WHERE tenant_id = 'default' AND workspace_id = 'default'`
	args := []any{}
	query, args = addSearch(query, args, filter.Search, "name")
	query, args = addFilter(query, args, "effect", filter.Effect)
	if filter.Enabled != nil {
		query += ` AND enabled = ?`
		args = append(args, *filter.Enabled)
	}
	query += ` ORDER BY priority ASC, id ASC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanPolicy)
}

func (r *Repository) GetPolicy(ctx context.Context, id string) (domainnetworkaccess.Policy, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+policyColumns+` FROM network_access_policies WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? LIMIT 1`, id).Row(), scanPolicy, "network access policy")
}

func (r *Repository) CreatePolicy(ctx context.Context, item domainnetworkaccess.Policy) (domainnetworkaccess.Policy, error) {
	values, err := encodePolicyValues(item.Subjects, item.SiteIDs, item.ResourceIDs, item.Modes, item.DeviceStatuses, item.PostureStatuses)
	if err != nil {
		return domainnetworkaccess.Policy{}, err
	}
	err = r.db.WithContext(ctx).Exec(`INSERT INTO network_access_policies (id, name, enabled, priority, effect, subjects, site_ids, resource_ids, modes, device_statuses, posture_statuses, access_profile, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?, ?, ?, ?)`, item.ID, item.Name, item.Enabled, item.Priority, item.Effect, values[0], values[1], values[2], values[3], values[4], values[5], item.AccessProfile, item.Version, item.CreatedAt, item.UpdatedAt).Error
	if err != nil {
		return domainnetworkaccess.Policy{}, normalizeDatabaseError(err)
	}
	return r.GetPolicy(ctx, item.ID)
}

func (r *Repository) UpdatePolicy(ctx context.Context, id string, input domainnetworkaccess.PolicyInput, updatedAt time.Time) (domainnetworkaccess.Policy, error) {
	values, err := encodePolicyValues(input.Subjects, input.SiteIDs, input.ResourceIDs, input.Modes, input.DeviceStatuses, input.PostureStatuses)
	if err != nil {
		return domainnetworkaccess.Policy{}, err
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE network_access_policies SET name = ?, enabled = ?, priority = ?, effect = ?, subjects = ?::jsonb, site_ids = ?::jsonb, resource_ids = ?::jsonb, modes = ?::jsonb, device_statuses = ?::jsonb, posture_statuses = ?::jsonb, access_profile = ?, version = version + 1, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, input.Name, input.Enabled, input.Priority, input.Effect, values[0], values[1], values[2], values[3], values[4], values[5], input.AccessProfile, updatedAt, id)
	if err := mutationError(result, "network access policy"); err != nil {
		return domainnetworkaccess.Policy{}, err
	}
	return r.GetPolicy(ctx, id)
}

func (r *Repository) DeletePolicy(ctx context.Context, id string) error {
	return mutationError(r.db.WithContext(ctx).Exec(`DELETE FROM network_access_policies WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, id), "network access policy")
}

func (r *Repository) ListPoliciesForSnapshot(ctx context.Context) ([]domainnetworkaccess.Policy, error) {
	return queryMany(ctx, r.db, `SELECT `+policyColumns+` FROM network_access_policies WHERE tenant_id = 'default' AND workspace_id = 'default' AND enabled = true ORDER BY priority ASC, id ASC`, nil, scanPolicy)
}

func (r *Repository) ListProtectedResourceIDs(ctx context.Context) ([]string, error) {
	return queryMany(ctx, r.db, `SELECT id FROM network_access_resources WHERE tenant_id = 'default' AND workspace_id = 'default' AND protected = true ORDER BY id ASC`, nil, func(row scanner) (string, error) {
		var id string
		err := row.Scan(&id)
		return id, err
	})
}

func (r *Repository) GetPolicySnapshot(ctx context.Context) (domainnetworkaccess.PolicySnapshot, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT policy_version, content_hash, policies, protected_resource_ids, policy_count, protected_resource_count, published_at FROM network_access_policy_snapshots WHERE tenant_id = 'default' AND workspace_id = 'default' ORDER BY policy_version DESC LIMIT 1`).Row(), scanPolicySnapshot, "network policy snapshot")
}

func (r *Repository) PublishPolicySnapshot(ctx context.Context, snapshot domainnetworkaccess.PolicySnapshot) (domainnetworkaccess.PolicySnapshot, error) {
	var published domainnetworkaccess.PolicySnapshot
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-access-policy-snapshot:default:default").Error; err != nil {
			return err
		}
		current, err := scanPolicySnapshot(tx.Raw(`SELECT policy_version, content_hash, policies, protected_resource_ids, policy_count, protected_resource_count, published_at FROM network_access_policy_snapshots WHERE tenant_id = 'default' AND workspace_id = 'default' ORDER BY policy_version DESC LIMIT 1 FOR UPDATE`).Row())
		if err == nil && current.ContentHash == snapshot.ContentHash {
			published = current
			return nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		policies, err := json.Marshal(snapshot.Policies)
		if err != nil {
			return err
		}
		protectedResourceIDs, err := json.Marshal(snapshot.ProtectedResourceIDs)
		if err != nil {
			return err
		}
		published, err = scanPolicySnapshot(tx.Raw(`INSERT INTO network_access_policy_snapshots (content_hash, policies, protected_resource_ids, policy_count, protected_resource_count, published_at) VALUES (?, ?::jsonb, ?::jsonb, ?, ?, ?) RETURNING policy_version, content_hash, policies, protected_resource_ids, policy_count, protected_resource_count, published_at`, snapshot.ContentHash, string(policies), string(protectedResourceIDs), snapshot.PolicyCount, snapshot.ProtectedResourceCount, snapshot.PublishedAt).Row())
		return err
	})
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, normalizeDatabaseError(err)
	}
	return published, nil
}

func (r *Repository) ListConflictRanges(ctx context.Context) ([]domainnetworkaccess.ConflictRange, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT id, name, cidrs FROM network_access_spaces WHERE tenant_id = 'default' AND workspace_id = 'default' AND status = ? ORDER BY id ASC`, domainnetworkaccess.StatusActive).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ranges := make([]domainnetworkaccess.ConflictRange, 0)
	for rows.Next() {
		var id, name string
		var encoded []byte
		if err := rows.Scan(&id, &name, &encoded); err != nil {
			return nil, err
		}
		var cidrs []string
		if err := json.Unmarshal(encoded, &cidrs); err != nil {
			return nil, fmt.Errorf("decode network space CIDRs: %w", err)
		}
		for _, cidr := range cidrs {
			ranges = append(ranges, domainnetworkaccess.ConflictRange{SourceType: domainnetworkaccess.ConflictSourceNetworkSpace, SourceID: id, Name: name, CIDR: cidr})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	gatewayRows, err := r.db.WithContext(ctx).Raw(`SELECT id, name, overlay_cidr::text FROM network_access_gateways WHERE tenant_id = 'default' AND workspace_id = 'default' AND administrative_status = ? AND overlay_cidr IS NOT NULL ORDER BY id ASC`, domainnetworkaccess.StatusActive).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = gatewayRows.Close() }()
	for gatewayRows.Next() {
		var id, name, cidr string
		if err := gatewayRows.Scan(&id, &name, &cidr); err != nil {
			return nil, err
		}
		ranges = append(ranges, domainnetworkaccess.ConflictRange{SourceType: domainnetworkaccess.ConflictSourceWireGuardOverlay, SourceID: id, Name: name, CIDR: cidr})
	}
	return ranges, gatewayRows.Err()
}

func ensureSpaceCIDRsAvailable(tx *gorm.DB, excludeID, cidrs string) error {
	// ponytail: default/default uses one lock; scope the key when self-hosted multi-tenancy becomes real.
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-access-ranges:default:default").Error; err != nil {
		return err
	}
	var candidate, existingID, existingName, configured string
	err := tx.Raw(`
		SELECT candidate.cidr, existing.id, existing.name, configured.cidr
		FROM jsonb_array_elements_text(?::jsonb) AS candidate(cidr)
		CROSS JOIN network_access_spaces AS existing
		CROSS JOIN LATERAL jsonb_array_elements_text(existing.cidrs) AS configured(cidr)
		WHERE existing.tenant_id = 'default'
		  AND existing.workspace_id = 'default'
		  AND existing.status = ?
		  AND existing.id <> ?
		  AND candidate.cidr::cidr && configured.cidr::cidr
		LIMIT 1`, cidrs, domainnetworkaccess.StatusActive, excludeID).Row().Scan(&candidate, &existingID, &existingName, &configured)
	if !errors.Is(err, sql.ErrNoRows) {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: CIDR %s overlaps network space %s (%s, %s)", apperrors.ErrConflict, candidate, existingName, existingID, configured)
	}
	err = tx.Raw(`
		SELECT candidate.cidr, gateway.id, gateway.name, gateway.overlay_cidr::text
		FROM jsonb_array_elements_text(?::jsonb) AS candidate(cidr)
		CROSS JOIN network_access_gateways AS gateway
		WHERE gateway.tenant_id = 'default'
		  AND gateway.workspace_id = 'default'
		  AND gateway.administrative_status = ?
		  AND gateway.overlay_cidr IS NOT NULL
		  AND candidate.cidr::cidr && gateway.overlay_cidr
		LIMIT 1`, cidrs, domainnetworkaccess.StatusActive).Row().Scan(&candidate, &existingID, &existingName, &configured)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: CIDR %s overlaps WireGuard gateway %s (%s, %s)", apperrors.ErrConflict, candidate, existingName, existingID, configured)
}

func ensureGatewayOverlayAvailable(tx *gorm.DB, excludeID, overlayCIDR string) error {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-access-ranges:default:default").Error; err != nil {
		return err
	}
	var existingID, existingName, configured string
	err := tx.Raw(`
		SELECT space.id, space.name, configured.cidr
		FROM network_access_spaces AS space
		CROSS JOIN LATERAL jsonb_array_elements_text(space.cidrs) AS configured(cidr)
		WHERE space.tenant_id = 'default'
		  AND space.workspace_id = 'default'
		  AND space.status = ?
		  AND ?::cidr && configured.cidr::cidr
		LIMIT 1`, domainnetworkaccess.StatusActive, overlayCIDR).Row().Scan(&existingID, &existingName, &configured)
	if !errors.Is(err, sql.ErrNoRows) {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: WireGuard overlay %s overlaps network space %s (%s, %s)", apperrors.ErrConflict, overlayCIDR, existingName, existingID, configured)
	}
	err = tx.Raw(`
		SELECT id, name, overlay_cidr::text
		FROM network_access_gateways
		WHERE tenant_id = 'default'
		  AND workspace_id = 'default'
		  AND administrative_status = ?
		  AND id <> ?
		  AND overlay_cidr IS NOT NULL
		  AND ?::cidr && overlay_cidr
		LIMIT 1`, domainnetworkaccess.StatusActive, excludeID, overlayCIDR).Row().Scan(&existingID, &existingName, &configured)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: WireGuard overlay %s overlaps gateway %s (%s, %s)", apperrors.ErrConflict, overlayCIDR, existingName, existingID, configured)
}

func ensureGatewayTopology(tx *gorm.DB, id, siteID, administrativeStatus, routingMode, hubGatewayID, advertisedCIDRs string) error {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-gateway-topology:default:default").Error; err != nil {
		return err
	}
	if administrativeStatus != domainnetworkaccess.StatusActive {
		var childID string
		err := tx.Raw(`SELECT id FROM network_access_gateways WHERE tenant_id = 'default' AND workspace_id = 'default'
			AND hub_gateway_id = ? AND administrative_status = 'active' LIMIT 1`, id).Row().Scan(&childID)
		if err == nil {
			return fmt.Errorf("%w: gateway %s is the hub of active spoke %s", apperrors.ErrConflict, id, childID)
		}
		return normalizeNoRows(err)
	}
	if hubGatewayID != "" {
		if hubGatewayID == id {
			return fmt.Errorf("%w: a gateway cannot be its own hub", apperrors.ErrConflict)
		}
		var hubSiteID, hubParent, hubRoutingMode, hubStatus string
		err := tx.Raw(`SELECT site_id, COALESCE(hub_gateway_id, ''), routing_mode, administrative_status
			FROM network_access_gateways WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? FOR SHARE`, hubGatewayID).
			Row().Scan(&hubSiteID, &hubParent, &hubRoutingMode, &hubStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: hub gateway %s not found", apperrors.ErrConflict, hubGatewayID)
		}
		if err != nil {
			return err
		}
		if hubParent != "" || hubSiteID == siteID || hubRoutingMode != domainnetworkaccess.GatewayRoutingRouted || hubStatus != domainnetworkaccess.StatusActive || routingMode != domainnetworkaccess.GatewayRoutingRouted {
			return fmt.Errorf("%w: hub-spoke gateways must be active routed gateways in different sites", apperrors.ErrConflict)
		}
	}
	var childID string
	err := tx.Raw(`SELECT id FROM network_access_gateways WHERE tenant_id = 'default' AND workspace_id = 'default'
		AND hub_gateway_id = ? AND administrative_status = 'active' LIMIT 1`, id).Row().Scan(&childID)
	if err == nil && (hubGatewayID != "" || routingMode != domainnetworkaccess.GatewayRoutingRouted) {
		return fmt.Errorf("%w: active hub gateway %s cannot become a spoke or use SNAT", apperrors.ErrConflict, id)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return ensureGatewayAdvertisedCIDRs(tx, id, siteID, advertisedCIDRs)
}

func ensureGatewayAdvertisedCIDRs(tx *gorm.DB, id, siteID, advertisedCIDRs string) error {
	var uncovered string
	err := tx.Raw(`SELECT candidate.cidr
		FROM jsonb_array_elements_text(?::jsonb) AS candidate(cidr)
		WHERE NOT EXISTS (
			SELECT 1 FROM network_access_spaces AS space
			CROSS JOIN LATERAL jsonb_array_elements_text(space.cidrs) AS configured(cidr)
				WHERE space.tenant_id = 'default' AND space.workspace_id = 'default' AND space.status = 'active'
				AND space.site_id = ?
				AND candidate.cidr::cidr <<= configured.cidr::cidr
			) LIMIT 1`, advertisedCIDRs, siteID).Row().Scan(&uncovered)
	if err == nil {
		return fmt.Errorf("%w: advertised CIDR %s is not covered by an active network space", apperrors.ErrConflict, uncovered)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var candidate, existingID, existingCIDR string
	err = tx.Raw(`SELECT candidate.cidr, gateway.id, configured.cidr
		FROM jsonb_array_elements_text(?::jsonb) AS candidate(cidr)
		CROSS JOIN network_access_gateways AS gateway
		CROSS JOIN LATERAL jsonb_array_elements_text(gateway.advertised_cidrs) AS configured(cidr)
		WHERE gateway.tenant_id = 'default' AND gateway.workspace_id = 'default' AND gateway.administrative_status = 'active'
		AND gateway.id <> ? AND candidate.cidr::cidr && configured.cidr::cidr LIMIT 1`, advertisedCIDRs, id).
		Row().Scan(&candidate, &existingID, &existingCIDR)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: advertised CIDR %s overlaps gateway %s (%s)", apperrors.ErrConflict, candidate, existingID, existingCIDR)
}

func normalizeNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

type scanner interface{ Scan(...any) error }

func scanDevice(row scanner) (domainnetworkaccess.Device, error) {
	var item domainnetworkaccess.Device
	var reportedFacts []byte
	if err := row.Scan(&item.ID, &item.OwnerUserID, &item.Name, &item.Hostname, &item.Platform, &item.DeviceType, &item.OwnershipType, &item.SiteID, &item.Status, &item.PostureStatus, &item.PostureVersion, &item.CredentialGeneration, &item.LastSeenAt, &reportedFacts, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	if len(reportedFacts) > 0 && string(reportedFacts) != "null" {
		var facts domainnetworkaccess.DeviceReportedFacts
		if err := json.Unmarshal(reportedFacts, &facts); err != nil {
			return item, fmt.Errorf("decode endpoint device facts: %w", err)
		}
		item.ReportedFacts = &facts
	}
	return item, nil
}

func scanSite(row scanner) (domainnetworkaccess.Site, error) {
	var item domainnetworkaccess.Site
	err := row.Scan(&item.ID, &item.Name, &item.Description, &item.Location, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanSpace(row scanner) (domainnetworkaccess.Space, error) {
	var item domainnetworkaccess.Space
	var cidrs []byte
	if err := row.Scan(&item.ID, &item.SiteID, &item.Name, &cidrs, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	if err := json.Unmarshal(cidrs, &item.CIDRs); err != nil {
		return item, fmt.Errorf("decode network space CIDRs: %w", err)
	}
	return item, nil
}

func scanResource(row scanner) (domainnetworkaccess.Resource, error) {
	var item domainnetworkaccess.Resource
	var ports []byte
	if err := row.Scan(&item.ID, &item.SpaceID, &item.Name, &item.Kind, &item.Target, &item.Protocol, &ports, &item.Protected, &item.PathMode, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	if err := json.Unmarshal(ports, &item.Ports); err != nil {
		return item, fmt.Errorf("decode network resource ports: %w", err)
	}
	return item, nil
}

func scanGateway(row scanner) (domainnetworkaccess.Gateway, error) {
	var item domainnetworkaccess.Gateway
	var dnsServers, advertisedCIDRs, capabilities []byte
	if err := row.Scan(&item.ID, &item.RuntimeID, &item.SiteID, &item.Name, &item.AdministrativeStatus, &item.Status,
		&item.PublicEndpointHost, &item.PublicEndpointPort, &item.OverlayCIDR, &item.RoutingMode, &item.MTU,
		&item.PersistentKeepaliveSeconds, &dnsServers, &item.HubGatewayID, &advertisedCIDRs, &item.WireGuardPublicKey, &item.Version, &capabilities,
		&item.PolicyVersion, &item.AppliedAt, &item.LastHeartbeatAt, &item.CreatedAt, &item.UpdatedAt,
		&item.Region, &item.ProviderCode, &item.ProviderName, &item.SelectionPriority, &item.AcceptNewConnections, &item.MaxSessions, &item.ProbeURL); err != nil {
		return item, err
	}
	if err := json.Unmarshal(dnsServers, &item.DNSServers); err != nil {
		return item, fmt.Errorf("decode network gateway DNS servers: %w", err)
	}
	if err := json.Unmarshal(advertisedCIDRs, &item.AdvertisedCIDRs); err != nil {
		return item, fmt.Errorf("decode network gateway advertised CIDRs: %w", err)
	}
	if err := json.Unmarshal(capabilities, &item.Capabilities); err != nil {
		return item, fmt.Errorf("decode network gateway capabilities: %w", err)
	}
	return item, nil
}

func scanPolicy(row scanner) (domainnetworkaccess.Policy, error) {
	var item domainnetworkaccess.Policy
	var subjects, siteIDs, resourceIDs, modes, deviceStatuses, postureStatuses []byte
	if err := row.Scan(&item.ID, &item.Name, &item.Enabled, &item.Priority, &item.Effect, &subjects, &siteIDs, &resourceIDs, &modes, &deviceStatuses, &postureStatuses, &item.AccessProfile, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	for _, value := range []struct {
		name string
		raw  []byte
		dst  any
	}{
		{"subjects", subjects, &item.Subjects}, {"site IDs", siteIDs, &item.SiteIDs}, {"resource IDs", resourceIDs, &item.ResourceIDs},
		{"modes", modes, &item.Modes}, {"device statuses", deviceStatuses, &item.DeviceStatuses}, {"posture statuses", postureStatuses, &item.PostureStatuses},
	} {
		if err := json.Unmarshal(value.raw, value.dst); err != nil {
			return item, fmt.Errorf("decode network policy %s: %w", value.name, err)
		}
	}
	return item, nil
}

func scanPolicySnapshot(row scanner) (domainnetworkaccess.PolicySnapshot, error) {
	var item domainnetworkaccess.PolicySnapshot
	var policies, protectedResourceIDs []byte
	if err := row.Scan(&item.PolicyVersion, &item.ContentHash, &policies, &protectedResourceIDs, &item.PolicyCount, &item.ProtectedResourceCount, &item.PublishedAt); err != nil {
		return item, err
	}
	if err := json.Unmarshal(policies, &item.Policies); err != nil {
		return item, fmt.Errorf("decode network policy snapshot policies: %w", err)
	}
	if err := json.Unmarshal(protectedResourceIDs, &item.ProtectedResourceIDs); err != nil {
		return item, fmt.Errorf("decode network policy snapshot protected resources: %w", err)
	}
	return item, nil
}

func encodePolicyValues(values ...any) ([6]string, error) {
	var encoded [6]string
	for index, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return encoded, err
		}
		encoded[index] = string(raw)
	}
	return encoded, nil
}

func queryMany[T any](ctx context.Context, db *gorm.DB, query string, args []any, scan func(scanner) (T, error)) ([]T, error) {
	rows, err := db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanOne[T any](row *sql.Row, scan func(scanner) (T, error), kind string) (T, error) {
	item, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		var zero T
		return zero, fmt.Errorf("%w: %s not found", apperrors.ErrNotFound, kind)
	}
	return item, err
}

func addFilter(query string, args []any, column, value string) (string, []any) {
	if strings.TrimSpace(value) == "" {
		return query, args
	}
	return query + " AND " + column + " = ?", append(args, strings.TrimSpace(value))
}

func addSearch(query string, args []any, search string, columns ...string) (string, []any) {
	search = strings.TrimSpace(search)
	if search == "" || len(columns) == 0 {
		return query, args
	}
	parts := make([]string, len(columns))
	for index, column := range columns {
		parts[index] = column + " ILIKE ?"
		args = append(args, "%"+search+"%")
	}
	return query + " AND (" + strings.Join(parts, " OR ") + ")", args
}

func mutationError(result *gorm.DB, kind string) error {
	if result.Error != nil {
		return normalizeDatabaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %s not found", apperrors.ErrNotFound, kind)
	}
	return nil
}

func normalizeDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		switch state.SQLState() {
		case "23503", "23505":
			return fmt.Errorf("%w: network access resource conflicts with existing state", apperrors.ErrConflict)
		case "23514":
			return fmt.Errorf("%w: network access resource violates a data constraint", apperrors.ErrInvalidArgument)
		}
	}
	return err
}
