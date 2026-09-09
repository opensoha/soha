package networkaccess

import (
	"fmt"
	"net/netip"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func analyzeConflictRanges(ranges []domainnetworkaccess.ConflictRange) (domainnetworkaccess.ConflictAnalysis, error) {
	prefixes := make([]netip.Prefix, len(ranges))
	for index, item := range ranges {
		prefix, err := netip.ParsePrefix(item.CIDR)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
			return domainnetworkaccess.ConflictAnalysis{}, fmt.Errorf("%w: invalid canonical IPv4 CIDR %q", apperrors.ErrInvalidArgument, item.CIDR)
		}
		prefixes[index] = prefix
	}

	result := domainnetworkaccess.ConflictAnalysis{Valid: true, RangesAnalyzed: len(ranges), Conflicts: []domainnetworkaccess.Conflict{}, Warnings: []string{}}
	for left := range ranges {
		for right := left + 1; right < len(ranges); right++ {
			if !prefixes[left].Overlaps(prefixes[right]) {
				continue
			}
			reason := domainnetworkaccess.ConflictOverlap
			if prefixes[left] == prefixes[right] {
				reason = domainnetworkaccess.ConflictDuplicate
			}
			result.Conflicts = append(result.Conflicts, domainnetworkaccess.Conflict{Left: ranges[left], Right: ranges[right], Reason: reason})
		}
	}
	result.Valid = len(result.Conflicts) == 0
	return result, nil
}
