package resource

import "testing"

func TestShouldUseInformerCache(t *testing.T) {
	if shouldUseInformerCache("") {
		t.Fatalf("shouldUseInformerCache(\"\") = true, want false")
	}
	if shouldUseInformerCache("   ") {
		t.Fatalf("shouldUseInformerCache(blank) = true, want false")
	}
	if !shouldUseInformerCache("erp-front") {
		t.Fatalf("shouldUseInformerCache(namespace) = false, want true")
	}
}

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
