package copilot

import (
	"math"
	"testing"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
)

func TestModelContextUsagePreservesUnknownZeroAndEstimatedMeasurements(t *testing.T) {
	if usage := modelContextUsage(appaigateway.WorkbenchRelayResponse{}); usage.Measurement != "unavailable" || usage.InputTokens != nil {
		t.Fatal("missing measurement became zero")
	}
	usage := modelContextUsage(appaigateway.WorkbenchRelayResponse{Usage: map[string]float64{"inputTokens": 0, "outputTokens": 4, "totalTokens": 4}, UsageEstimated: true, ContextWindowTokens: 128000})
	if usage.Measurement != "estimated" || usage.InputTokens == nil || *usage.InputTokens != 0 || usage.ContextWindowTokens != 128000 {
		t.Fatalf("estimate provenance lost: %+v", usage)
	}
	for _, value := range []float64{-1, 0.5, math.NaN(), math.Inf(1), 9007199254740992} {
		if modelUsageCount(map[string]float64{"inputTokens": value}, "inputTokens") != nil {
			t.Fatalf("invalid token count accepted: %v", value)
		}
	}
}
