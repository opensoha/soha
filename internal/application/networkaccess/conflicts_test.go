package networkaccess

import (
	"testing"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
)

func TestAnalyzeConflictRanges(t *testing.T) {
	ranges := []domainnetworkaccess.ConflictRange{
		{SourceType: domainnetworkaccess.ConflictSourceNetworkSpace, SourceID: "space-1", Name: "office", CIDR: "10.20.0.0/16"},
		{SourceType: domainnetworkaccess.ConflictSourceWireGuardOverlay, Name: "overlay", CIDR: "10.20.1.0/24"},
		{SourceType: domainnetworkaccess.ConflictSourceMihomoFakeIP, Name: "fake-ip", CIDR: "198.18.0.0/15"},
		{SourceType: domainnetworkaccess.ConflictSourceReserved, Name: "benchmark", CIDR: "198.18.0.0/15"},
		{SourceType: domainnetworkaccess.ConflictSourceContainer, Name: "adjacent", CIDR: "10.21.0.0/16"},
	}

	got, err := analyzeConflictRanges(ranges)
	if err != nil {
		t.Fatalf("analyzeConflictRanges() error = %v", err)
	}
	if got.Valid || got.RangesAnalyzed != len(ranges) || len(got.Conflicts) != 2 {
		t.Fatalf("analyzeConflictRanges() = %#v, want two conflicts", got)
	}
	if got.Conflicts[0].Reason != domainnetworkaccess.ConflictOverlap || got.Conflicts[1].Reason != domainnetworkaccess.ConflictDuplicate {
		t.Fatalf("conflict reasons = %#v, want overlap then duplicate", got.Conflicts)
	}
}

func TestAnalyzeConflictRangesRejectsNonCanonicalCIDR(t *testing.T) {
	_, err := analyzeConflictRanges([]domainnetworkaccess.ConflictRange{{SourceType: domainnetworkaccess.ConflictSourceLAN, Name: "bad", CIDR: "10.20.1.1/16"}})
	if err == nil {
		t.Fatal("analyzeConflictRanges() error = nil, want invalid CIDR")
	}
}
