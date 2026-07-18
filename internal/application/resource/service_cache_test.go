package resource

import (
	"testing"
)

func TestShouldPopulatePodUsageSummaries(t *testing.T) {
	if shouldPopulatePodUsageSummaries("") {
		t.Fatalf("shouldPopulatePodUsageSummaries(\"\") = true, want false")
	}
	if shouldPopulatePodUsageSummaries("   ") {
		t.Fatalf("shouldPopulatePodUsageSummaries(blank) = true, want false")
	}
	if !shouldPopulatePodUsageSummaries("erp-front") {
		t.Fatalf("shouldPopulatePodUsageSummaries(namespace) = false, want true")
	}
}
