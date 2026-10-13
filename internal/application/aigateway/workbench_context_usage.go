package aigateway

import (
	"encoding/json"
	"math"
	"net/http"
)

// Resolve the successful fallback route, not the initially preferred candidate.
func workbenchResolvedSelection(headers http.Header, selections []relaySelection) relaySelection {
	for _, selection := range selections {
		if selection.route.ID == headers.Get("X-Soha-Route-ID") && selection.upstream.ID == headers.Get("X-Soha-Upstream-ID") {
			return selection
		}
	}
	return relaySelection{}
}

func workbenchContextWindow(metadata map[string]any) int64 {
	var tokens float64
	switch value := metadata["contextWindowTokens"].(type) {
	case float64:
		tokens = value
	case int:
		tokens = float64(value)
	case int64:
		tokens = float64(value)
	case json.Number:
		tokens, _ = value.Float64()
	default:
		return 0
	}
	if math.IsNaN(tokens) || math.IsInf(tokens, 0) || tokens <= 0 || tokens > 9007199254740991 || math.Trunc(tokens) != tokens {
		return 0
	}
	return int64(tokens)
}

func workbenchCatalogContextWindow(selections []relaySelection) int64 {
	var capacity int64
	for _, selection := range selections {
		tokens := workbenchContextWindow(selection.route.Metadata)
		if tokens == 0 {
			return 0
		}
		if capacity == 0 || tokens < capacity {
			capacity = tokens
		}
	}
	return capacity
}
