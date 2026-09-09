package networkaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
)

const mihomoProfileColumns = `id, device_id, name, mode, COALESCE(source_type, ''), status,
	COALESCE(subscription_url_ciphertext, ''), COALESCE(manual_node_ciphertext, ''), revision,
	mixed_port, controller_port, dns_mode, COALESCE(fake_ip_range::text, ''), selector_group,
	COALESCE(selected_proxy, ''), bypass_cidrs, bypass_hosts, fail_closed, created_at, updated_at`

func (r *Repository) ListMihomoProfiles(ctx context.Context, filter domainnetworkaccess.MihomoProfileFilter) ([]domainnetworkaccess.MihomoProfile, error) {
	query := `SELECT ` + mihomoProfileColumns + ` FROM network_mihomo_profiles WHERE tenant_id = 'default' AND workspace_id = 'default'`
	args := []any{}
	query, args = addSearch(query, args, filter.Search, "name", "selector_group", "selected_proxy")
	query, args = addFilter(query, args, "device_id", filter.DeviceID)
	query, args = addFilter(query, args, "mode", filter.Mode)
	query, args = addFilter(query, args, "status", filter.Status)
	query += ` ORDER BY name ASC, id ASC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanMihomoProfile)
}

func (r *Repository) GetMihomoProfile(ctx context.Context, id string) (domainnetworkaccess.MihomoProfile, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+mihomoProfileColumns+` FROM network_mihomo_profiles WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? LIMIT 1`, id).Row(), scanMihomoProfile, "mihomo profile")
}

func (r *Repository) CreateMihomoProfile(ctx context.Context, item domainnetworkaccess.MihomoProfile) (domainnetworkaccess.MihomoProfile, error) {
	bypassCIDRs, bypassHosts, err := encodeMihomoLists(item)
	if err != nil {
		return domainnetworkaccess.MihomoProfile{}, err
	}
	err = r.db.WithContext(ctx).Exec(`INSERT INTO network_mihomo_profiles
		(id, device_id, name, mode, source_type, status, subscription_url_ciphertext, manual_node_ciphertext, revision, mixed_port, controller_port,
		 dns_mode, fake_ip_range, selector_group, selected_proxy, bypass_cidrs, bypass_hosts, fail_closed, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), 1, ?, ?, ?, NULLIF(?, '')::cidr, ?, NULLIF(?, ''), ?::jsonb, ?::jsonb, ?, ?, ?)`,
		item.ID, item.DeviceID, item.Name, item.Mode, item.SourceType, item.Status, item.SubscriptionURLCiphertext, item.ManualNodeCiphertext,
		item.MixedPort, item.ControllerPort, item.DNSMode, item.FakeIPRange, item.SelectorGroup, item.SelectedProxy,
		bypassCIDRs, bypassHosts, item.FailClosed, item.CreatedAt, item.UpdatedAt).Error
	if err != nil {
		return domainnetworkaccess.MihomoProfile{}, normalizeDatabaseError(err)
	}
	return r.GetMihomoProfile(ctx, item.ID)
}

func (r *Repository) UpdateMihomoProfile(ctx context.Context, id string, item domainnetworkaccess.MihomoProfile, updatedAt time.Time) (domainnetworkaccess.MihomoProfile, error) {
	bypassCIDRs, bypassHosts, err := encodeMihomoLists(item)
	if err != nil {
		return domainnetworkaccess.MihomoProfile{}, err
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE network_mihomo_profiles SET
		device_id = ?, name = ?, mode = ?, source_type = NULLIF(?, ''), status = ?, subscription_url_ciphertext = NULLIF(?, ''), manual_node_ciphertext = NULLIF(?, ''),
		revision = revision + 1, mixed_port = ?, controller_port = ?, dns_mode = ?, fake_ip_range = NULLIF(?, '')::cidr,
		selector_group = ?, selected_proxy = NULLIF(?, ''), bypass_cidrs = ?::jsonb, bypass_hosts = ?::jsonb,
		fail_closed = ?, updated_at = ?
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`,
		item.DeviceID, item.Name, item.Mode, item.SourceType, item.Status, item.SubscriptionURLCiphertext, item.ManualNodeCiphertext,
		item.MixedPort, item.ControllerPort, item.DNSMode, item.FakeIPRange, item.SelectorGroup, item.SelectedProxy,
		bypassCIDRs, bypassHosts, item.FailClosed, updatedAt, id)
	if err := mutationError(result, "mihomo profile"); err != nil {
		return domainnetworkaccess.MihomoProfile{}, err
	}
	return r.GetMihomoProfile(ctx, id)
}

func (r *Repository) DeleteMihomoProfile(ctx context.Context, id string) error {
	return mutationError(r.db.WithContext(ctx).Exec(`DELETE FROM network_mihomo_profiles WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, id), "mihomo profile")
}

func scanMihomoProfile(row scanner) (domainnetworkaccess.MihomoProfile, error) {
	var item domainnetworkaccess.MihomoProfile
	var bypassCIDRs, bypassHosts []byte
	if err := row.Scan(&item.ID, &item.DeviceID, &item.Name, &item.Mode, &item.SourceType, &item.Status, &item.SubscriptionURLCiphertext, &item.ManualNodeCiphertext,
		&item.Revision, &item.MixedPort, &item.ControllerPort, &item.DNSMode, &item.FakeIPRange, &item.SelectorGroup,
		&item.SelectedProxy, &bypassCIDRs, &bypassHosts, &item.FailClosed, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	if err := json.Unmarshal(bypassCIDRs, &item.BypassCIDRs); err != nil {
		return item, fmt.Errorf("decode mihomo bypass CIDRs: %w", err)
	}
	if err := json.Unmarshal(bypassHosts, &item.BypassHosts); err != nil {
		return item, fmt.Errorf("decode mihomo bypass hosts: %w", err)
	}
	item.SubscriptionConfigured = item.SubscriptionURLCiphertext != ""
	item.ManualNodeConfigured = item.ManualNodeCiphertext != ""
	return item, nil
}

func encodeMihomoLists(item domainnetworkaccess.MihomoProfile) (string, string, error) {
	bypassCIDRs, err := json.Marshal(item.BypassCIDRs)
	if err != nil {
		return "", "", err
	}
	bypassHosts, err := json.Marshal(item.BypassHosts)
	if err != nil {
		return "", "", err
	}
	return string(bypassCIDRs), string(bypassHosts), nil
}
