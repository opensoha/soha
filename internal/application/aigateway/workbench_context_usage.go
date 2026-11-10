package aigateway

import (
	"encoding/json"
	"math"
)

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
