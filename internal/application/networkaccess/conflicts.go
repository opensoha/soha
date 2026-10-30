package networkaccess

import (
	"fmt"
	"net/netip"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func analyzeConflictRanges(ranges []domainnetworkaccess.ConflictRange) (domainnetworkaccess.ConflictAnalysis, error) {
	type parsedRange struct {
		value  domainnetworkaccess.ConflictRange
		prefix netip.Prefix
	}
	parsed := make([]parsedRange, 0, len(ranges))
	for _, item := range ranges {
		prefix, err := netip.ParsePrefix(item.CIDR)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
			return domainnetworkaccess.ConflictAnalysis{}, fmt.Errorf("%w: invalid canonical IPv4 CIDR %q", apperrors.ErrInvalidArgument, item.CIDR)
		}
		parsed = append(parsed, parsedRange{value: item, prefix: prefix})
	}

	result := domainnetworkaccess.ConflictAnalysis{Valid: true, RangesAnalyzed: len(ranges), Conflicts: []domainnetworkaccess.Conflict{}, Warnings: []string{}}
	for left, item := range parsed {
		for right := left + 1; right < len(parsed); right++ {
			if !item.prefix.Overlaps(parsed[right].prefix) {
				continue
			}
			reason := domainnetworkaccess.ConflictOverlap
			if item.prefix == parsed[right].prefix {
				reason = domainnetworkaccess.ConflictDuplicate
			}
			result.Conflicts = append(result.Conflicts, domainnetworkaccess.Conflict{Left: item.value, Right: parsed[right].value, Reason: reason})
		}
	}
	result.Valid = len(result.Conflicts) == 0
	return result, nil
}
