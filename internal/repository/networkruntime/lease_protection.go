package networkruntime

import (
	"net/netip"

	domainaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

// An allow for one resource must not shadow the deny for a different protected
// resource at the same address or inside a broader authorized CIDR.
func validateResourceProtectionScope(tx *gorm.DB, snapshot domain.PolicySnapshot, targets []leasedResourceTarget) error {
	if len(targets) == 0 {
		return nil
	}
	authorized := make(map[string]bool, len(targets))
	for _, target := range targets {
		authorized[target.ResourceID] = true
	}
	unselected := make([]string, 0, len(snapshot.ProtectedResourceIDs))
	for _, id := range snapshot.ProtectedResourceIDs {
		if !authorized[id] {
			unselected = append(unselected, id)
		}
	}
	remaining := snapshot
	remaining.ProtectedResourceIDs = unselected
	denied, err := vpnProtectedDestinations(tx, remaining)
	if err != nil {
		return err
	}
	for _, target := range targets {
		prefix, err := netip.ParsePrefix(target.DestinationCIDR)
		if err != nil {
			return staleVPNResourceScope()
		}
		for _, raw := range denied {
			if prefix.Overlaps(netip.MustParsePrefix(raw)) {
				return apperrors.NewBusiness(apperrors.ErrConflict, "resource_protection_overlap", "A selected resource overlaps another protected destination without its resource authorization.", "所选资源与未取得资源授权的其他受保护目标重叠。")
			}
		}
	}
	return nil
}

func compileProtectedLeaseAccess(tx *gorm.DB, networks []networkprotocol.NetworkLease, resources []networkprotocol.ResourceLease, snapshot domain.PolicySnapshot) (domainaccess.Space, []string, []leasedResourceTarget, error) {
	space, routes, targets, err := compileLeaseAccess(tx, networks, resources)
	if err != nil {
		return space, nil, nil, err
	}
	return space, routes, targets, validateResourceProtectionScope(tx, snapshot, targets)
}
