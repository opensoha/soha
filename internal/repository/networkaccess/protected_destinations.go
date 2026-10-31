package networkaccess

import (
	"context"
	"encoding/json"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/netip"
	"slices"
	"strings"
)

func (r *Repository) CompileProtectedDestinations(ctx context.Context, version int, protectedIDs []string) (*domainnetworkaccess.ProtectedDestinations, error) {
	tx := r.db.WithContext(ctx)
	protection := &domainnetworkaccess.ProtectedDestinations{PolicyVersion: version, CIDRs: []string{}, Hosts: []string{}}
	if version < 1 {
		return nil, invalidProtectedSet()
	}
	if len(protectedIDs) == 0 {
		return protection, nil
	}
	rows, err := tx.Raw(`SELECT r.id, r.kind, r.target, s.cidrs FROM network_access_resources r
		JOIN network_access_spaces s ON s.tenant_id = r.tenant_id AND s.workspace_id = r.workspace_id AND s.id = r.space_id
		WHERE r.tenant_id = 'default' AND r.workspace_id = 'default' AND r.protected = true AND r.id IN ?
		ORDER BY r.id`, protectedIDs).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	seen := map[string]bool{}
	dnsResources := []dnsResource{}
	for rows.Next() {
		var id, kind, target string
		var rawCIDRs []byte
		if err := rows.Scan(&id, &kind, &target, &rawCIDRs); err != nil {
			return nil, err
		}
		var spaceCIDRs []string
		if err := json.Unmarshal(rawCIDRs, &spaceCIDRs); err != nil || len(spaceCIDRs) == 0 {
			return nil, invalidProtectedSet()
		}
		seen[id] = true
		if kind == "fqdn" {
			dnsResources = append(dnsResources, dnsResource{resource: domainnetworkaccess.Resource{ID: id, Kind: kind, Target: target}, cidrs: spaceCIDRs})
		}
		if err := projectProtectedResource(protection, kind, target, spaceCIDRs); err != nil {
			return nil, err
		}

		if len(protection.CIDRs) > 4096 || len(protection.Hosts) > 2048 {
			return nil, invalidProtectedSet()
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := r.expandProtectedDNS(ctx, protection, dnsResources); err != nil {
		return nil, err
	}

	if len(protection.CIDRs) > 4096 {
		return nil, invalidProtectedSet()
	}

	for _, id := range protectedIDs {
		if !seen[id] {
			return nil, invalidProtectedSet()
		}
	}
	if err := validateProtectedPrefixes(protection.CIDRs); err != nil {
		return nil, err
	}
	slices.Sort(protection.CIDRs)
	protection.CIDRs = slices.Compact(protection.CIDRs)
	slices.Sort(protection.Hosts)
	protection.Hosts = slices.Compact(protection.Hosts)
	return protection, nil
}

func invalidProtectedSet() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "protected_set_invalid", "The protected destination set is missing, invalid or exceeds its limit.", "受保护目标清单缺失、无效或超过限制。")
}
func validateProtectedPrefixes(values []string) error {
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Is4In6() || !prefix.Addr().IsGlobalUnicast() || prefix != prefix.Masked() || prefix.Bits() == 0 || prefix.String() != value {
			return invalidProtectedSet()
		}
	}
	return nil
}

type dnsResource struct {
	resource domainnetworkaccess.Resource
	cidrs    []string
}

func (r *Repository) expandProtectedDNS(ctx context.Context, protection *domainnetworkaccess.ProtectedDestinations, dnsResources []dnsResource) error {
	for _, pending := range dnsResources {
		resource, err := r.GetResource(ctx, pending.resource.ID)
		if err != nil {
			return err
		}
		if len(resource.TrustedAddresses) == 0 {
			protection.CIDRs = append(protection.CIDRs, pending.cidrs...)
			continue
		}
		for _, raw := range resource.TrustedAddresses {
			address, err := netip.ParseAddr(raw)
			if err != nil {
				return invalidProtectedSet()
			}
			protection.CIDRs = append(protection.CIDRs, netip.PrefixFrom(address, address.BitLen()).String())
		}
	}
	return nil
}

func projectProtectedResource(protection *domainnetworkaccess.ProtectedDestinations, kind, target string, spaceCIDRs []string) error {
	switch kind {
	case "ip":
		address, err := netip.ParseAddr(target)
		if err != nil || address.Is4In6() || !address.IsGlobalUnicast() {
			return invalidProtectedSet()
		}
		protection.CIDRs = append(protection.CIDRs, netip.PrefixFrom(address, address.BitLen()).String())
	case "cidr":
		if target == "0.0.0.0/0" || target == "::/0" {
			protection.CIDRs = append(protection.CIDRs, spaceCIDRs...)
		} else {
			protection.CIDRs = append(protection.CIDRs, target)
		}
	case "fqdn":
		protection.Hosts = append(protection.Hosts, strings.ToLower(target))
	default:
		return invalidProtectedSet()
	}
	return nil
}
