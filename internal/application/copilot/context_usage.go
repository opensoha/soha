package copilot

import (
	"math"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

func modelContextUsage(response appaigateway.WorkbenchRelayResponse) *domaincopilot.ModelContextUsage {
	usage := &domaincopilot.ModelContextUsage{Measurement: "unavailable", Model: response.PublicModel, ContextWindowTokens: response.ContextWindowTokens}
	usage.InputTokens = modelUsageCount(response.Usage, "inputTokens")
	usage.OutputTokens = modelUsageCount(response.Usage, "outputTokens")
	usage.TotalTokens = modelUsageCount(response.Usage, "totalTokens")
	if usage.InputTokens != nil || usage.OutputTokens != nil || usage.TotalTokens != nil {
		usage.Measurement = "reported"
		if response.UsageEstimated {
			usage.Measurement = "estimated"
		}
	}
	return usage
}

func modelUsageCount(usage map[string]float64, key string) *int64 {
	value, exists := usage[key]
	if !exists || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 9007199254740991 || math.Trunc(value) != value {
		return nil
	}
	tokens := int64(value)
	return &tokens
}
